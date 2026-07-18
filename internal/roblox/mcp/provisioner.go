package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dialer opens a transport to the Studio MCP launcher.
type Dialer func(context.Context, LaunchConfig) (Transport, error)

// launcherTimeout bounds the whole gate check. A run's context has no deadline
// of its own, and this runs before the agent starts while a scheduler slot and
// a project write lease are held, so a Studio that accepts the connection but
// never answers must cost the run a few seconds, not block it forever.
const launcherTimeout = 20 * time.Second

// Grant is the Studio access handed to a single run. An empty ConfigPath means
// the run gets no Studio access; Notice, when set, explains why in terms the
// operator can act on.
type Grant struct {
	ConfigPath   string
	AllowedTools []string
	Notice       string
	Context      string // a snapshot of the open place, for the run's prompt
	Release      func()
}

// Provisioner decides whether a run may reach Roblox Studio and, if so, writes
// the MCP config that grants it.
//
// Access is the only control that can be enforced here. Claude Code runs its
// own MCP client, so set_active_studio on our connection cannot pin the
// instance on the agent's connection, and the launcher accepts no
// instance-selection argument. Ambiguity is therefore refused rather than
// guessed at.
type Provisioner struct {
	Dir      string        // directory for generated per-run configs
	Override func() string // configured studio_mcp_path, may be nil
	Dial     Dialer        // defaults to the stdio launcher transport
	Timeout  time.Duration // defaults to launcherTimeout
	Exe      func() (string, error)
	// AutoOpen reports the studio_auto_open setting. A nil func opens Studio,
	// which is the default the operator sees.
	AutoOpen func() bool
}

func (p *Provisioner) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return launcherTimeout
}

func (p *Provisioner) dial(ctx context.Context, launch LaunchConfig) (Transport, error) {
	if p.Dial != nil {
		return p.Dial(ctx, launch)
	}
	return NewStdioTransport(ctx, launch)
}

// Target names the project a run is for. PlaceName is the file name that
// project's place is built under, which is how an open Studio is recognised as
// holding this project rather than another — an instance reports its place's
// file name and nothing else about which project it belongs to. Open, when set,
// builds and launches that place.
//
// A zero Target falls back to the older rule, where a single open Studio is
// taken to be the right one.
type Target struct {
	PlaceName string
	Open      func(context.Context) error
}

// openWait bounds how long a run waits for a Studio it asked for. Studio builds
// the place and paints a window first, so this is slow by nature; a run that
// waited longer would be better served proceeding without Studio.
const openWait = 45 * time.Second

// Provision returns the Studio access for a run. It never fails a run: when
// Studio is absent or ambiguous the run simply proceeds without it.
func (p *Provisioner) Provision(ctx context.Context, runID, permissionProfile string, target Target) Grant {
	tools := AllowedTools(permissionProfile)
	if len(tools) == 0 {
		return Grant{Notice: fmt.Sprintf("Studio MCP withheld: permission profile %q grants no Studio tools", permissionProfile)}
	}
	override := ""
	if p.Override != nil {
		override = p.Override()
	}
	launch, err := DetectLauncher(override)
	if err != nil {
		// Studio not installed or MCP not enabled is an ordinary local setup, not
		// a run failure.
		return Grant{}
	}
	instances, state, err := p.probe(ctx, launch)
	if err != nil {
		return Grant{Notice: "Studio MCP withheld: " + err.Error()}
	}
	instances, state, notice := p.selectForTarget(ctx, launch, target, instances, state)
	if notice != "" {
		return Grant{Notice: notice}
	}
	if len(instances) == 0 {
		return Grant{}
	}
	path := filepath.Join(p.Dir, runID+".json")
	if err := WriteConfig(path, p.agentLaunch(launch)); err != nil {
		return Grant{Notice: "Studio MCP withheld: " + err.Error()}
	}
	return Grant{
		ConfigPath:   path,
		AllowedTools: tools,
		Context:      state,
		Release:      func() { _ = os.Remove(path) },
	}
}

// selectForTarget narrows the open instances to the one holding this project's
// place, opening Studio if asked to and nothing is open yet. It returns the
// instances to grant on, the state snapshot to carry into the prompt, and a
// notice that, when set, means no access is granted and why.
func (p *Provisioner) selectForTarget(ctx context.Context, launch LaunchConfig, target Target, instances []Instance, state string) ([]Instance, string, string) {
	// Without an expected place name nothing can be matched, so fall back to the
	// older rule: one open Studio is unambiguous, several cannot be pinned.
	if target.PlaceName == "" {
		if len(instances) > 1 {
			return nil, "", fmt.Sprintf("Studio MCP withheld: %d Studio instances are open and StudioForge cannot pin one for the agent's own MCP connection; leave a single Studio open", len(instances))
		}
		return instances, state, ""
	}

	matched := matching(instances, target.PlaceName)
	switch {
	case len(matched) == 1:
		return matched, state, ""
	case len(matched) > 1:
		return nil, "", fmt.Sprintf("Studio MCP withheld: %d Studio instances hold %s and StudioForge cannot pin one for the agent's own MCP connection; leave a single one open", len(matched), target.PlaceName)
	}

	// Nothing of this project's is open. Opening it is the whole point of the
	// setting, so a run that wanted Studio gets it rather than silently going
	// without.
	if target.Open == nil || !p.autoOpen() {
		if len(instances) > 0 {
			return nil, "", fmt.Sprintf("Studio MCP withheld: the open Studio does not hold this project's place (%s); open the project's place, or turn on opening it automatically", target.PlaceName)
		}
		return nil, "", ""
	}
	if err := target.Open(ctx); err != nil {
		return nil, "", "Studio MCP withheld: opening this project's place failed: " + err.Error()
	}
	opened, state, err := p.waitForPlace(ctx, launch, target.PlaceName)
	if err != nil {
		return nil, "", "Studio MCP withheld: " + err.Error()
	}
	if len(opened) != 1 {
		return nil, "", fmt.Sprintf("Studio MCP withheld: %s did not finish opening within %s; the run continues without Studio", target.PlaceName, openWait)
	}
	return opened, state, ""
}

func (p *Provisioner) autoOpen() bool {
	return p.AutoOpen == nil || p.AutoOpen()
}

// matching returns the instances holding the named place. Studio reports the
// file name it opened, so this is a plain comparison — case-insensitively,
// because Windows paths are.
func matching(instances []Instance, placeName string) []Instance {
	var out []Instance
	for _, instance := range instances {
		if strings.EqualFold(instance.Name, placeName) {
			out = append(out, instance)
		}
	}
	return out
}

// waitForPlace polls for a Studio holding the named place over a single
// launcher connection. Re-probing would spawn a launcher process per attempt,
// and each of those competes for the WS host port that decides who is told
// about Studio's tools.
func (p *Provisioner) waitForPlace(ctx context.Context, launch LaunchConfig, placeName string) ([]Instance, string, error) {
	ctx, cancel := context.WithTimeout(ctx, openWait)
	defer cancel()
	transport, err := p.dial(ctx, launch)
	if err != nil {
		return nil, "", fmt.Errorf("open Studio MCP launcher: %w", err)
	}
	client := NewClient(transport)
	defer func() { _ = client.Close() }()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		instances, err := client.ListStudios(ctx)
		if err == nil {
			if matched := matching(instances, placeName); len(matched) > 0 {
				state := ""
				if raw, callErr := client.Call(ctx, "get_studio_state", nil); callErr == nil {
					state = studioStateText(raw)
				}
				return matched, state, nil
			}
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			// A place that never appeared is not an error to report upwards; the
			// caller turns an empty result into its own notice.
			return nil, "", nil
		}
	}
}

// agentLaunch is the command the agent runs to reach Studio: StudioForge in
// shim mode, wrapping the launcher.
//
// Pointing the agent straight at the launcher is what leaves it blind whenever
// another MCP client holds the WS host port, because the tool list is pushed
// only to the host and Claude Code builds its toolset from that list. If the
// executable cannot be located the raw launcher is used after all — that is the
// old behaviour, so the run is no worse off than before the shim existed.
func (p *Provisioner) agentLaunch(launch LaunchConfig) LaunchConfig {
	self := p.Exe
	if self == nil {
		self = os.Executable
	}
	exe, err := self()
	if err != nil {
		return launch
	}
	args := []string{"mcp-shim", "--launcher", launch.Command}
	for _, arg := range launch.Args {
		args = append(args, "--launcher-arg", arg)
	}
	if p.Dir != "" {
		args = append(args, "--tool-cache", filepath.Join(p.Dir, toolCacheName))
	}
	return LaunchConfig{Command: exe, Args: args}
}

// Status is what the UI needs to say about Studio: how many are open at all,
// and how many hold the project being looked at.
type Status struct {
	Open    int `json:"open"`
	Matched int `json:"matched"`
}

// Status reports the open Studio instances and how many hold the named place.
// An empty placeName reports only the total, which is what a caller with no
// project in hand can be told.
//
// It spawns the launcher, so callers should cache it rather than poll it.
func (p *Provisioner) Status(ctx context.Context, placeName string) (Status, error) {
	override := ""
	if p.Override != nil {
		override = p.Override()
	}
	launch, err := DetectLauncher(override)
	if err != nil {
		return Status{}, nil
	}
	instances, _, err := p.probe(ctx, launch)
	if err != nil {
		return Status{}, err
	}
	status := Status{Open: len(instances)}
	if placeName != "" {
		status.Matched = len(matching(instances, placeName))
	}
	return status, nil
}

// CountOpen reports how many Roblox Studio instances are open, so the UI can
// show whether an agent will reach Studio. It spawns the launcher, so it is for
// on-demand checks, not tight polling. A missing launcher counts as zero.
func (p *Provisioner) CountOpen(ctx context.Context) (int, error) {
	override := ""
	if p.Override != nil {
		override = p.Override()
	}
	launch, err := DetectLauncher(override)
	if err != nil {
		return 0, nil
	}
	instances, _, err := p.probe(ctx, launch)
	if err != nil {
		return 0, err
	}
	return len(instances), nil
}

// probe opens one launcher connection and reports the open Studio instances and,
// when exactly one is open, a snapshot of its place (via get_studio_state) so
// the run's prompt can carry the current state instead of the agent re-exploring
// it. The snapshot is best-effort: any failure yields an empty string.
func (p *Provisioner) probe(ctx context.Context, launch LaunchConfig) ([]Instance, string, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()
	transport, err := p.dial(ctx, launch)
	if err != nil {
		return nil, "", fmt.Errorf("open Studio MCP launcher: %w", err)
	}
	client := NewClient(transport)
	defer func() { _ = client.Close() }()
	// tools/list is deliberately not consulted. Only the launcher that won the
	// WS host port is pushed the tool list; every other client is advertised
	// zero tools for as long as it lives, yet its calls still succeed through
	// the host. Asking anyway would also cost ten seconds per probe, because the
	// launcher waits that long for a push that never comes.
	instances, err := client.ListStudios(ctx)
	if err != nil {
		if IsMethodNotFound(err) {
			return nil, "", fmt.Errorf("Studio MCP exposes no instance listing; update Roblox Studio")
		}
		return nil, "", err
	}
	state := ""
	if len(instances) == 1 {
		if raw, callErr := client.Call(ctx, "get_studio_state", nil); callErr == nil {
			state = studioStateText(raw)
		}
	}
	return instances, state, nil
}

// studioStateText pulls the human-readable text out of an MCP tool result and
// caps it so a large place tree cannot bloat every prompt.
func studioStateText(raw json.RawMessage) string {
	var body struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	var parts []string
	for _, c := range body.Content {
		if strings.TrimSpace(c.Text) != "" {
			parts = append(parts, c.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if len(text) > 4000 {
		text = text[:4000] + "\n…(truncated)"
	}
	return text
}
