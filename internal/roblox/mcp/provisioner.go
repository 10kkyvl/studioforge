package mcp

import (
	"context"
	"encoding/json"
	"errors"
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
	// Studio reports whether this grant actually reaches Roblox Studio. It is
	// explicit rather than inferred from ConfigPath because a config is now
	// written for every Claude run — StudioForge's own question server lives
	// there too — so a written config no longer means Studio was granted.
	Studio bool
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
	// Running reports whether a Studio process exists, which distinguishes a
	// machine with no Studio from one whose Studio is owned by another MCP
	// client — the launcher lists no instances in both cases. A nil func means
	// the question cannot be asked and the vaguer wording is used.
	Running func(context.Context) bool
	// attachWindow and retryEvery pace the wait for the Studio plugin to attach
	// to a freshly spawned launcher; tests shrink them. See probe.
	attachWindow time.Duration
	retryEvery   time.Duration
}

// The plugin dials the WS host a beat after the launcher spawns, so the first
// listings on a fresh launcher routinely come back without a place. attachWait
// bounds how long a probe waits for the attach; past it a running Studio that
// registered nothing is out of reach for this run either way.
const attachWait = 8 * time.Second

// glanceAttachWait is the same wait for callers a person is watching rather
// than a run: the chat badge polls Status, the Open Studio button blocks on
// CheckOpen, and the sessions list refreshes on demand. Those must answer
// within a glance, and a Studio that registers nothing holds that state for as
// long as it stays open — a plugin left disabled holds it indefinitely — so
// spending the run gate's window on every poll would stall the UI outright.
// Giving up early costs a glance only the freshest answer, which the next poll
// or click corrects; a run that gives up early loses every Studio tool for its
// whole length, which is why the gate waits longer.
const glanceAttachWait = 1500 * time.Millisecond

// errWSHostUnreachable means Studio is running but never registered a place
// with our launcher within the attach window.
var errWSHostUnreachable = errors.New("Studio MCP plugin never attached to the launcher")

// notConnected recognises the launcher's tool error for a missing plugin
// connection, which arrives as a well-formed tool result rather than a distinct
// error code.
func notConnected(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Not connected to the WS host")
}

// blocked reports whether an empty instance list means "another MCP client owns
// Studio" rather than "no Studio is open".
func (p *Provisioner) blocked(ctx context.Context) bool {
	return p.Running != nil && p.Running(ctx)
}

// hostTakenNotice explains the one failure that looks like every other: the
// launcher connected, so nothing errored, yet Studio registered no place with
// it. Which cause produced that cannot be read off the wire, so the notice
// states what was observed and names both, rather than asserting the one that
// is only usually right — an operator sent to close a second MCP client that
// does not exist has nothing left to try.
const hostTakenNotice = "Studio MCP withheld: Roblox Studio is running, but it registered no open place with the MCP launcher. Either another MCP client already holds Studio's connection — Roblox grants it to one client at a time — or this Studio's MCP plugin is not enabled. Close the other client (Claude Desktop, Claude Code, Cursor or another editor with the Roblox Studio MCP server enabled), or enable the MCP plugin in Studio, then start the run again."

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

// Place is how an open Studio is recognised as holding a project's place. The
// launcher reports one name per instance and nothing else about which project
// it belongs to, and what that name is depends on where the place was opened
// from: a local file reports the file's name, while a place opened from
// roblox.com — Team Create included — reports the place's display name. A
// project is therefore recognised by whichever of these its operator works in.
type Place struct {
	// FileName is the place file StudioForge builds this project to.
	FileName string
	// CloudName is the display name of the roblox.com place this project is
	// edited as, empty for a project that only opens its built file.
	CloudName string
}

// Named reports whether the place carries anything to match an instance on. An
// unnamed place cannot be recognised, so callers fall back to the older rule of
// taking a single open Studio to be the right one.
func (p Place) Named() bool { return p.FileName != "" || p.CloudName != "" }

// Cloud reports whether this project is edited as a roblox.com place rather
// than as its own built file.
func (p Place) Cloud() bool { return p.CloudName != "" }

// matches reports whether an instance reporting name holds this place. The
// comparison is case-insensitive because Windows file names are. An instance
// that reports no name at all is mid-registration, not a match.
func (p Place) matches(name string) bool {
	if name == "" {
		return false
	}
	return (p.FileName != "" && strings.EqualFold(name, p.FileName)) ||
		(p.CloudName != "" && strings.EqualFold(name, p.CloudName))
}

// String names the place the way a notice should state it, so an operator
// reading a refusal sees what was actually looked for.
func (p Place) String() string {
	switch {
	case p.FileName != "" && p.CloudName != "":
		return fmt.Sprintf("%s or the roblox.com place %q", p.FileName, p.CloudName)
	case p.CloudName != "":
		return fmt.Sprintf("the roblox.com place %q", p.CloudName)
	default:
		return p.FileName
	}
}

// Target names the project a run is for. Place is how an open Studio is
// recognised as holding this project rather than another. Open, when set,
// builds and launches the project's own place file.
//
// A zero Target falls back to the older rule, where a single open Studio is
// taken to be the right one.
type Target struct {
	Place Place
	Open  func(context.Context) error
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
		return p.questionOnlyGrant(runID, fmt.Sprintf("Studio MCP withheld: permission profile %q grants no Studio tools", permissionProfile))
	}
	override := ""
	if p.Override != nil {
		override = p.Override()
	}
	launch, err := DetectLauncher(override)
	if err != nil {
		// Studio not installed or MCP not enabled is an ordinary local setup, not
		// a run failure.
		return p.questionOnlyGrant(runID, "")
	}
	instances, state, err := p.probe(ctx, launch)
	if errors.Is(err, errWSHostUnreachable) {
		return p.questionOnlyGrant(runID, hostTakenNotice)
	}
	if err != nil {
		return p.questionOnlyGrant(runID, "Studio MCP withheld: "+err.Error())
	}
	instances, state, notice := p.selectForTarget(ctx, launch, target, instances, state)
	if notice != "" {
		return p.questionOnlyGrant(runID, notice)
	}
	if len(instances) == 0 {
		// A machine with no Studio open stays silent: plenty of runs never want
		// Studio, and a notice on each would be noise. A Studio that is open but
		// owned by another MCP client is the opposite case — it looks identical
		// here, yet leaving it silent strips the agent of every Studio tool with
		// no stated reason, and it improvises a workaround instead.
		if p.blocked(ctx) {
			return p.questionOnlyGrant(runID, hostTakenNotice)
		}
		return p.questionOnlyGrant(runID, "")
	}
	path := filepath.Join(p.Dir, runID+".json")
	servers := map[string]LaunchConfig{ServerName: p.agentLaunch(launch)}
	if question, ok := p.questionLaunch(); ok {
		servers[QuestionServerName] = question
	}
	if err := WriteServers(path, servers); err != nil {
		return p.questionOnlyGrant(runID, "Studio MCP withheld: "+err.Error())
	}
	return Grant{
		ConfigPath:   path,
		AllowedTools: tools,
		Context:      state,
		Studio:       true,
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
	if !target.Place.Named() {
		if len(instances) > 1 {
			return nil, "", fmt.Sprintf("Studio MCP withheld: %d Studio instances are open and StudioForge cannot pin one for the agent's own MCP connection; leave a single Studio open", len(instances))
		}
		return instances, state, ""
	}

	matched := matching(instances, target.Place)
	switch {
	case len(matched) == 1:
		return matched, state, ""
	case len(matched) > 1:
		return nil, "", ambiguousMatchNotice(len(matched), target.Place)
	}

	// Nothing of this project's is open. Some OTHER Studio instance being open
	// is not the same as none being open at all: auto-opening on top of it
	// would risk piling a second window onto Studio rather than the one this
	// project wants, so this withholds even when auto-open is on — the same
	// notice the no-match/auto-open-off case already gave, now covering both.
	if len(instances) > 0 {
		return nil, "", mismatchNotice(instances, target.Place)
	}

	if p.blocked(ctx) {
		return nil, "", hostTakenNotice
	}

	// A project edited on roblox.com has no local build worth opening: the place
	// the operator and their collaborators work in lives on Roblox, and Studio
	// can only reach it by being opened from there. Building the project's own
	// file and launching that would put a second, unrelated window in front of
	// them — the very pile-on the branch above refuses — so this says what is
	// missing instead of opening the wrong thing.
	if target.Place.Cloud() {
		return nil, "", cloudPlaceNotice(target.Place)
	}

	// Opening is the whole point of the setting, so a run that wanted Studio
	// gets it rather than silently going without — but only when Studio is not
	// open at all, never as a "top up" alongside an unrelated instance.
	if target.Open == nil || !p.autoOpen() {
		return nil, "", ""
	}
	if err := target.Open(ctx); err != nil {
		return nil, "", "Studio MCP withheld: opening this project's place failed: " + err.Error()
	}
	opened, state, err := p.waitForPlace(ctx, launch, target.Place)
	if err != nil {
		return nil, "", "Studio MCP withheld: " + err.Error()
	}
	if len(opened) != 1 {
		return nil, "", fmt.Sprintf("Studio MCP withheld: %s did not finish opening within %s; the run continues without Studio", target.Place, openWait)
	}
	return opened, state, ""
}

func (p *Provisioner) autoOpen() bool {
	return p.AutoOpen == nil || p.AutoOpen()
}

// matching returns the instances holding the given place, by whichever of its
// names the launcher reports.
func matching(instances []Instance, place Place) []Instance {
	var out []Instance
	for _, instance := range instances {
		if place.matches(instance.Name) {
			out = append(out, instance)
		}
	}
	return out
}

// ambiguousMatchNotice explains a refusal when more than one open instance
// holds the same expected place. A place is meant to be unique per project, so
// this should not happen in practice, but it is still refused rather than
// picked from arbitrarily.
func ambiguousMatchNotice(count int, place Place) string {
	return fmt.Sprintf("Studio MCP withheld: %d Studio instances hold %s and StudioForge cannot pin one for the agent's own MCP connection; leave a single one open", count, place)
}

// mismatchNotice explains a refusal when Studio instances are open but none of
// them hold the expected place, naming what is actually open next to what was
// expected — an operator who opened the project's original .rbxl instead of
// its built place, say, can see exactly why from this alone.
func mismatchNotice(instances []Instance, place Place) string {
	advice := "open the project's place, or close the others and let StudioForge open it automatically"
	if place.Cloud() {
		// Telling someone working on roblox.com to let StudioForge open the
		// place sends them to a local build their collaborators are not in.
		advice = fmt.Sprintf("open %q from roblox.com, or correct the project's cloud place name", place.CloudName)
	}
	return fmt.Sprintf("Studio MCP withheld: the open Studio does not hold this project's place (expected %s, found %s); %s", place, strings.Join(instanceNames(instances), ", "), advice)
}

// cloudPlaceNotice explains a refusal when a project is edited as a roblox.com
// place and no Studio is open at all. Auto-open cannot help here — it builds
// and launches a local file, which is not the place being collaborated on — so
// the only way forward is for someone to open it from Roblox.
func cloudPlaceNotice(place Place) string {
	return fmt.Sprintf("Studio MCP withheld: no Studio holds %s, and StudioForge cannot open a roblox.com place itself; open it from Roblox and the run will find it", place)
}

func instanceNames(instances []Instance) []string {
	names := make([]string, 0, len(instances))
	for _, instance := range instances {
		name := instance.Name
		if name == "" {
			name = "(unnamed)"
		}
		names = append(names, name)
	}
	return names
}

// OpenCheck is what a probe found relative to a project's expected place, for
// a caller that wants to know before opening Studio without also requesting
// an agent's MCP grant — currently the manual "Open Studio" button, which must
// apply the same no-duplicate-launch rule selectForTarget already applies
// before auto-opening.
type OpenCheck struct {
	// Open is true when no Studio instance is open at all, so launching is
	// safe.
	Open bool
	// Matched is true when an instance already holds this project's place;
	// the caller should read this as already there and not relaunch.
	Matched bool
	// Notice explains a refusal: instances are open, but none of them hold
	// this project's place. Empty whenever Open or Matched is true.
	Notice string
}

// CheckOpen reports whether launching Studio for place is safe, already
// done, or refused, without opening anything itself. A probe that cannot be
// completed — including an absent launcher — fails open (Open: true), the
// same posture every other probe in this package takes for a machine that
// simply has no Studio MCP configured; the launch attempt that follows a
// true Open still goes through the Opener's own in-flight guard, so a probe
// failure here does not risk a duplicate launch.
func (p *Provisioner) CheckOpen(ctx context.Context, place Place) OpenCheck {
	if !place.Named() {
		return OpenCheck{Open: true}
	}
	override := ""
	if p.Override != nil {
		override = p.Override()
	}
	launch, err := DetectLauncher(override)
	if err != nil {
		return OpenCheck{Open: true}
	}
	instances, _, err := p.glance(ctx, launch)
	// A Studio that ran out the attach window without registering a place is a
	// refusal, not an inconclusive probe: launching another window on top of it
	// is exactly what this check exists to prevent. Provision and Status single
	// this error out for the same reason; failing open on it would relaunch.
	if errors.Is(err, errWSHostUnreachable) {
		return OpenCheck{Notice: hostTakenNotice}
	}
	if err != nil {
		return OpenCheck{Open: true}
	}
	if len(instances) == 0 {
		if p.blocked(ctx) {
			return OpenCheck{Notice: hostTakenNotice}
		}
		return OpenCheck{Open: true}
	}
	// Any match at all — even the ambiguous case of two instances somehow
	// reporting this project's place — means something already holds it, so a
	// launch would only add another window rather than resolve anything.
	if matched := matching(instances, place); len(matched) > 0 {
		return OpenCheck{Matched: true}
	}
	return OpenCheck{Notice: mismatchNotice(instances, place)}
}

// waitForPlace polls for a Studio holding the named place over a single
// launcher connection. Re-probing would spawn a launcher process per attempt,
// and each of those competes for the WS host port that decides who is told
// about Studio's tools.
func (p *Provisioner) waitForPlace(ctx context.Context, launch LaunchConfig, place Place) ([]Instance, string, error) {
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
			if matched := matching(instances, place); len(matched) > 0 {
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

// questionLaunch is the command behind StudioForge's own single-tool MCP
// server. It wraps no launcher and never touches Studio, so unlike agentLaunch
// there is nothing to fall back to: without the executable's own path there is
// no server, and the run keeps the text fence it has always had.
func (p *Provisioner) questionLaunch() (LaunchConfig, bool) {
	self := p.Exe
	if self == nil {
		self = os.Executable
	}
	exe, err := self()
	if err != nil {
		return LaunchConfig{}, false
	}
	return LaunchConfig{Command: exe, Args: []string{"mcp-shim", "--questions-only"}}, true
}

// questionOnlyGrant is what a run gets when Studio is not granted: StudioForge's
// own question server and nothing else.
//
// Every path through Provision that declines Studio comes through here, because
// asking the operator a question has nothing to do with whether Studio is open.
// Leaving those runs with no MCP config at all is what used to send them back to
// the text fence, which is the failure this replaces.
func (p *Provisioner) questionOnlyGrant(runID, notice string) Grant {
	launch, ok := p.questionLaunch()
	if !ok {
		return Grant{Notice: notice}
	}
	path := filepath.Join(p.Dir, runID+".json")
	if err := WriteServers(path, map[string]LaunchConfig{QuestionServerName: launch}); err != nil {
		return Grant{Notice: notice}
	}
	return Grant{
		ConfigPath:   path,
		AllowedTools: []string{QuestionToolFullName},
		Notice:       notice,
		Release:      func() { _ = os.Remove(path) },
	}
}

// Status is what the UI needs to say about Studio: how many are open at all,
// and how many hold the project being looked at.
type Status struct {
	Open    int `json:"open"`
	Matched int `json:"matched"`
	// Blocked means Studio is running but another MCP client owns its
	// connection, which is why no instance is listed. Without it the badge
	// cannot tell this apart from a machine with Studio closed.
	Blocked bool `json:"blocked"`
}

// Status reports the open Studio instances and how many hold the named place.
// An empty placeName reports only the total, which is what a caller with no
// project in hand can be told.
//
// It spawns the launcher, so callers should cache it rather than poll it.
func (p *Provisioner) Status(ctx context.Context, place Place) (Status, error) {
	override := ""
	if p.Override != nil {
		override = p.Override()
	}
	launch, err := DetectLauncher(override)
	if err != nil {
		return Status{}, nil
	}
	instances, _, err := p.glance(ctx, launch)
	if errors.Is(err, errWSHostUnreachable) {
		return Status{Blocked: true}, nil
	}
	if err != nil {
		return Status{}, err
	}
	status := Status{Open: len(instances)}
	if len(instances) == 0 {
		status.Blocked = p.blocked(ctx)
	}
	if place.Named() {
		status.Matched = len(matching(instances, place))
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
	instances, _, err := p.glance(ctx, launch)
	if err != nil {
		return 0, err
	}
	return len(instances), nil
}

func (p *Provisioner) attachWindowOr() time.Duration {
	if p.attachWindow > 0 {
		return p.attachWindow
	}
	return attachWait
}

// glanceWindow is the attach budget for a caller a person is waiting on. The
// test seam overrides both windows, so a test that shrinks the wait shrinks it
// on every path rather than only the one it named.
func (p *Provisioner) glanceWindow() time.Duration {
	if p.attachWindow > 0 {
		return p.attachWindow
	}
	return glanceAttachWait
}

func (p *Provisioner) retryInterval() time.Duration {
	if p.retryEvery > 0 {
		return p.retryEvery
	}
	return time.Second
}

// anyNamed reports whether the listing carries a place name yet. A registration
// lands in pieces, so an instance can be listed with its ID before Studio has
// filled in the place it holds.
func anyNamed(instances []Instance) bool {
	for _, instance := range instances {
		if instance.Name != "" {
			return true
		}
	}
	return false
}

// awaitAttach lists Studio instances over an already-dialled launcher, waiting
// out the attach before believing the answer.
//
// The attach lands in three steps and only the first one errors. Until the
// plugin dials the WS host the listing fails outright ("Not connected to the WS
// host"); once it has dialled but registered nothing, the listing *succeeds*
// and returns an empty list; and once the instance is registered but its place
// is not, the listing carries an instance whose Name is still empty. Every
// launcher process runs this handshake for itself, and the gate, the badge, the
// sessions refresh and the agent's own shim each spawn their own — so a Studio
// that has been open for an hour still replays all three per connection, which
// is why this surfaces after a turn or two rather than only on the first.
//
// Believing any single step let a half-finished registration through as final.
// An empty list with a Studio process running is what Provision turns into
// hostTakenNotice, so a run started a beat too early was told another MCP client
// owned Studio when nothing did. A named-less instance is worse: it is not
// empty, so it reaches matching(), fails the place comparison, and comes back as
// "the open Studio does not hold this project's place … found (unnamed)" — a
// refusal naming a place mismatch that never happened.
//
// So all three steps count as "not attached yet" and are waited out together.
// An unfinished listing only becomes an answer when no Studio process exists,
// which is the ordinary "Studio closed" case and must stay silent and immediate.
//
// A genuine mismatch is untouched: a different place that is really open is not
// mid-registration, so its name is already there on the first listing and this
// returns at once, leaving mismatchNotice to say what is open.
func (p *Provisioner) awaitAttach(ctx context.Context, client *Client, window time.Duration) ([]Instance, error) {
	var attach <-chan time.Time
	for {
		instances, err := client.ListStudios(ctx)
		switch {
		case err != nil && !notConnected(err):
			return nil, err
		case err == nil && anyNamed(instances):
			return instances, nil
		}
		if attach == nil {
			// Whether a Studio process exists is settled once, on the first
			// unattached answer, and reused for the rest of the wait. Asking
			// again per iteration read the process table once a second, and
			// IsRunning reports any failure to ask as "not running" — so a
			// single hiccup part-way through collapsed the wait back into the
			// silent "no Studio" answer it exists to avoid. A Studio that starts
			// or stops inside the window is not worth that.
			if !p.blocked(ctx) {
				return nil, nil
			}
			attach = time.After(window)
		}
		select {
		case <-time.After(p.retryInterval()):
		case <-attach:
			return nil, errWSHostUnreachable
		case <-ctx.Done():
			return nil, errWSHostUnreachable
		}
	}
}

// probe opens one launcher connection and reports the open Studio instances and,
// when exactly one is open, a snapshot of its place (via get_studio_state) so
// the run's prompt can carry the current state instead of the agent re-exploring
// it. The snapshot is best-effort: any failure yields an empty string.
// It waits out the full attach window, which is what a run wants: the agent
// keeps whatever Studio access this decides for its whole length.
func (p *Provisioner) probe(ctx context.Context, launch LaunchConfig) ([]Instance, string, error) {
	return p.probeWithin(ctx, launch, p.attachWindowOr())
}

// glance is probe on the shorter budget, for the badge, the buttons and the
// sessions list — callers that are asked again shortly and must not make a
// person wait out a Studio that may never register.
func (p *Provisioner) glance(ctx context.Context, launch LaunchConfig) ([]Instance, string, error) {
	return p.probeWithin(ctx, launch, p.glanceWindow())
}

func (p *Provisioner) probeWithin(ctx context.Context, launch LaunchConfig, window time.Duration) ([]Instance, string, error) {
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
	instances, err := p.awaitAttach(ctx, client, window)
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
