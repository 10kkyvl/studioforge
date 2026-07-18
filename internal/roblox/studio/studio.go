// Package studio launches Roblox Studio on a project's built place file, so a
// new StudioForge project can be opened for editing with one action.
package studio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// winReserved holds the DOS device names. Windows rejects them as file names
// even with an extension, so a project called "CON" must not slug to one.
var winReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// PlaceName returns the file name a project's place is built to. It is derived
// from the project name because an open Studio instance reports only the place's
// file name — naming the place after the project is what lets a caller tell
// which project an open Studio holds.
//
// Only ASCII letters and digits survive. Names in other scripts — Cyrillic is
// the common case for this project's users — are deliberately dropped rather
// than transliterated: the name has to survive a round trip through the
// filesystem, Studio and the instance listing before it can be compared, and
// non-ASCII does not do that dependably enough to match on.
//
// The name alone therefore cannot carry identity. "Моя Игра 2" and "Игра 2"
// both slug to "2", and two projects sharing a place name would let a run edit
// the wrong place — the exact confusion this naming exists to prevent. So the
// head of the project ID is always appended, and the slug is left to serve
// legibility alone. A name that slugs to nothing yields the ID head by itself.
func PlaceName(name, id string) string {
	slug := slugify(name)
	// A reserved device name is unopenable on Windows even with an extension,
	// so treat it as if the name had slugged to nothing.
	if winReserved[slug] {
		slug = ""
	}
	unique := idHead(id)
	if slug == "" {
		return unique + ".rbxl"
	}
	return slug + "-" + unique + ".rbxl"
}

// idHead is the leading field of a project ID — eight hex characters, enough to
// separate the handful of projects one machine holds. A project created without
// an ID still needs a stable file name, so it falls back to a fixed word.
func idHead(id string) string {
	head := strings.ReplaceAll(slugify(id), "-", "")
	if head == "" {
		return "place"
	}
	if len(head) > 8 {
		head = head[:8]
	}
	return head
}

// PlacePath is where OpenProject builds a project's place. Callers that need to
// recognise an already-open Studio instance derive the expected name from here
// instead of restating the rule.
func PlacePath(projectPath, name, id string) string {
	return filepath.Join(projectPath, ".studioforge", PlaceName(name, id))
}

// slugify lowercases s and collapses every run of non-alphanumerics to a single
// '-', with no leading or trailing '-'.
func slugify(s string) string {
	var b strings.Builder
	sep := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			r += 'a' - 'A'
			fallthrough
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if sep && b.Len() > 0 {
				b.WriteByte('-')
			}
			sep = false
			b.WriteRune(r)
		default:
			sep = true
		}
	}
	return b.String()
}

// Builder compiles a Rojo project into a place file and installs the plugin.
// *rojo.Manager satisfies it; tests can substitute a fake.
type Builder interface {
	Build(ctx context.Context, projectFile, output string) error
	InstallPlugin(ctx context.Context) error
}

// DetectStudioExe locates the Roblox Studio executable. It is Windows-only for
// now; other platforms report that automatic launch is unsupported.
func DetectStudioExe() (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("automatic Studio launch is only supported on Windows")
	}
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return "", fmt.Errorf("LOCALAPPDATA is not set")
	}
	matches, _ := filepath.Glob(filepath.Join(local, "Roblox", "Versions", "*", "RobloxStudioBeta.exe"))
	newest := newestStudio(matches)
	if newest == "" {
		return "", fmt.Errorf("RobloxStudioBeta.exe not found; install Roblox Studio")
	}
	return newest, nil
}

// IsRunning reports whether a Roblox Studio process exists on this machine.
//
// It is the only way to tell two situations apart that the launcher reports
// identically. Roblox hands the MCP host slot to a single client: whichever
// launcher won it is the one Studio registers with, and every later client
// still connects and still answers, but lists no instances. So "no Studio is
// open" and "Studio is open, but another MCP client owns it" both arrive as an
// empty instance list. Looking for the process itself breaks the tie.
//
// Any failure to ask counts as "not running", because callers use this only to
// sharpen a message they would otherwise word vaguely.
func IsRunning(ctx context.Context) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "tasklist", "/FI", "IMAGENAME eq RobloxStudioBeta.exe", "/NH")
	case "darwin":
		cmd = exec.CommandContext(ctx, "pgrep", "-x", "RobloxStudio")
	default:
		return false
	}
	out, err := cmd.Output()
	if err != nil {
		// pgrep exits non-zero when nothing matches, which is the common "not
		// running" answer rather than a fault worth reporting.
		return false
	}
	return processListShowsStudio(string(out), runtime.GOOS)
}

// processListShowsStudio reads a process listing the way each platform's tool
// writes it, kept apart from the command so the parsing can be tested without a
// Studio to point it at.
func processListShowsStudio(out, goos string) bool {
	if goos == "windows" {
		// tasklist exits zero whether or not the filter matched, printing
		// "INFO: No tasks are running which match the specified criteria." when
		// it did not. That line never carries the image name, so finding the
		// name is what separates a hit from a miss.
		return strings.Contains(out, "RobloxStudioBeta")
	}
	// pgrep prints matching PIDs and nothing else.
	return strings.TrimSpace(out) != ""
}

// newestStudio picks the most recently modified executable, so an upgraded
// Studio version is preferred over a stale one left behind on disk.
func newestStudio(matches []string) string {
	if len(matches) == 0 {
		return ""
	}
	sorted := append([]string(nil), matches...)
	sort.Slice(sorted, func(i, j int) bool {
		return modTime(sorted[i]).After(modTime(sorted[j]))
	})
	return sorted[0]
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// LaunchPlace opens a local place file in Studio and returns immediately.
// Studio is a long-lived GUI, so the process is deliberately detached from the
// caller's context rather than tied to the request that started it.
func LaunchPlace(studioExe, placePath string) error {
	cmd := exec.Command(studioExe, "-task", "EditFile", "-localPlaceFile", placePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Roblox Studio: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// Opener builds a project's place file and opens it in Studio. DetectStudio and
// Launch are seams so tests can avoid touching the real filesystem or spawning
// Studio; both default to the real implementations.
type Opener struct {
	Rojo         Builder
	DetectStudio func() (string, error)
	Launch       func(studioExe, placePath string) error
}

func (o *Opener) detect() (string, error) {
	if o.DetectStudio != nil {
		return o.DetectStudio()
	}
	return DetectStudioExe()
}

func (o *Opener) launch(exe, place string) error {
	if o.Launch != nil {
		return o.Launch(exe, place)
	}
	return LaunchPlace(exe, place)
}

// OpenProject builds the project's default.project.json into a place file,
// installs the Rojo plugin (best effort), and opens the place in Studio. It
// returns the built place path. The name and id decide the place's file name —
// see PlaceName — so the resulting Studio instance identifies this project.
func (o *Opener) OpenProject(ctx context.Context, projectPath, name, id string) (string, error) {
	projectFile := filepath.Join(projectPath, "default.project.json")
	if _, err := os.Stat(projectFile); err != nil {
		return "", fmt.Errorf("no default.project.json in the project; cannot build a place")
	}
	// Detect Studio before building so a missing install fails fast.
	exe, err := o.detect()
	if err != nil {
		return "", err
	}
	place := PlacePath(projectPath, name, id)
	if err := os.MkdirAll(filepath.Dir(place), 0o755); err != nil {
		return "", err
	}
	if err := o.Rojo.Build(ctx, projectFile, place); err != nil {
		return "", err
	}
	_ = o.Rojo.InstallPlugin(ctx)
	if err := o.launch(exe, place); err != nil {
		return "", err
	}
	return place, nil
}
