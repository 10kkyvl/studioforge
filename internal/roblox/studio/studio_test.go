package studio

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeBuilder struct {
	built       bool
	pluginTried bool
}

func (f *fakeBuilder) Build(_ context.Context, _, output string) error {
	f.built = true
	return os.WriteFile(output, []byte("RBLX"), 0o600) // stand in for a real place
}
func (f *fakeBuilder) InstallPlugin(context.Context) error { f.pluginTried = true; return nil }

func TestNewestStudioPicksMostRecent(t *testing.T) {
	dir := t.TempDir()
	older := filepath.Join(dir, "old.exe")
	newer := filepath.Join(dir, "new.exe")
	for _, p := range []string{older, newer} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, old, old); err != nil {
		t.Fatal(err)
	}
	if got := newestStudio([]string{older, newer}); got != newer {
		t.Errorf("newestStudio picked %q, want the most recent %q", got, newer)
	}
	if newestStudio(nil) != "" {
		t.Error("no matches must yield an empty string")
	}
}

func TestPlaceNameSlugsTheProjectName(t *testing.T) {
	cases := []struct {
		what string
		name string
		id   string
		want string
	}{
		{"an ordinary name", "My Game", "abc123", "my-game-abc123.rbxl"},
		{"runs of punctuation and spaces collapse", "  Obby -- Tycoon!!  v2 ", "abc123", "obby-tycoon-v2-abc123.rbxl"},
		{"case is folded and digits kept", "Level42", "abc123", "level42-abc123.rbxl"},
		{"a name that is only punctuation leaves just the id", "!!! ---", "abc123", "abc123.rbxl"},
		{"an empty name leaves just the id", "", "abc123", "abc123.rbxl"},
		// Cyrillic is dropped rather than transliterated, so a wholly Cyrillic
		// name leaves just the id and a mixed one keeps only its ASCII.
		{"a Cyrillic name leaves just the id", "Моя Игра", "abc123", "abc123.rbxl"},
		{"a mixed name keeps its ASCII run", "Моя Игра 2", "abc123", "2-abc123.rbxl"},
		{"only the head of a real uuid is used", "My Game", "a3f91b2c-4d5e-6f70-8192-a3b4c5d6e7f8", "my-game-a3f91b2c.rbxl"},
		{"a short id is kept whole", "", "Proj_ID 7", "projid7.rbxl"},
		// Windows refuses DOS device names even with an extension.
		{"a reserved device name leaves just the id", "CON", "abc123", "abc123.rbxl"},
		{"nothing usable at all still yields a legal name", "", "", "place.rbxl"},
	}
	for _, tc := range cases {
		if got := PlaceName(tc.name, tc.id); got != tc.want {
			t.Errorf("%s: PlaceName(%q, %q)=%q, want %q", tc.what, tc.name, tc.id, got, tc.want)
		}
	}
}

// Names that slug to the same thing must still yield different places. Русские
// названия collapse hardest — "Моя Игра 2" and "Игра 2" both slug to "2" — and
// two projects sharing a place name would let a run edit the wrong place.
func TestPlaceNameSeparatesProjectsThatSlugAlike(t *testing.T) {
	first := PlaceName("Моя Игра 2", "a3f91b2c-4d5e-6f70-8192-a3b4c5d6e7f8")
	second := PlaceName("Игра 2", "b7c02d3e-1a2b-3c4d-5e6f-708192a3b4c5")
	if first == second {
		t.Fatalf("both projects build to %q", first)
	}
	if third := PlaceName("Моя Игра", "c1d2e3f4-5a6b-7c8d-9e0f-1a2b3c4d5e6f"); third == first || third == second {
		t.Fatalf("a wholly Cyrillic name collided: %q", third)
	}
}

func TestPlacePathIsUnderTheProject(t *testing.T) {
	want := filepath.Join("C:\\proj", ".studioforge", PlaceName("My Game", "abc123"))
	if got := PlacePath("C:\\proj", "My Game", "abc123"); got != want {
		t.Errorf("PlacePath=%q, want %q", got, want)
	}
}

func TestOpenProjectBuildsAndLaunches(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "default.project.json"), []byte(`{"name":"t","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBuilder{}
	var launchedExe, launchedPlace string
	o := &Opener{
		Rojo:         fb,
		DetectStudio: func() (string, error) { return "C:\\Studio.exe", nil },
		Launch:       func(exe, place string) error { launchedExe, launchedPlace = exe, place; return nil },
	}
	place, err := o.OpenProject(context.Background(), project, "My Game", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project, ".studioforge", PlaceName("My Game", "abc123")); place != want {
		t.Errorf("place=%q, want it named after the project %q", place, want)
	}
	if !fb.built || !fb.pluginTried {
		t.Errorf("OpenProject must build and try the plugin, built=%v plugin=%v", fb.built, fb.pluginTried)
	}
	if launchedExe != "C:\\Studio.exe" || launchedPlace != place {
		t.Errorf("launch got exe=%q place=%q, want the detected exe and built place %q", launchedExe, launchedPlace, place)
	}
	if _, err := os.Stat(place); err != nil {
		t.Errorf("the built place should exist on disk: %v", err)
	}
}

func TestOpenProjectPreservesAnExistingSavedPlace(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "default.project.json"), []byte(`{"name":"t","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	place := PlacePath(project, "My Game", "abc123")
	if err := os.MkdirAll(filepath.Dir(place), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := []byte("saved Studio place with generated meshes and terrain")
	if err := os.WriteFile(place, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBuilder{}
	var launched string
	o := &Opener{
		Rojo:         fb,
		DetectStudio: func() (string, error) { return "C:\\Studio.exe", nil },
		Launch:       func(_, path string) error { launched = path; return nil },
	}
	got, err := o.OpenProject(context.Background(), project, "My Game", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if fb.built {
		t.Fatal("an existing saved place was rebuilt and would lose Studio-only edits")
	}
	if got != place || launched != place {
		t.Fatalf("got=%q launched=%q want existing place %q", got, launched, place)
	}
	after, err := os.ReadFile(place)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(saved) {
		t.Fatalf("saved place changed while reopening: got %q want %q", after, saved)
	}
}

func TestOpenProjectRefusesWithoutProjectFile(t *testing.T) {
	o := &Opener{Rojo: &fakeBuilder{}, DetectStudio: func() (string, error) { return "x", nil }, Launch: func(string, string) error { return nil }}
	if _, err := o.OpenProject(context.Background(), t.TempDir(), "My Game", "abc123"); err == nil {
		t.Error("a project without default.project.json must be refused, not silently built")
	}
}

// A second call for the same place while the first is still within the
// pending grace window must not relaunch Studio — both the provisioner's
// auto-open and the manual "Open Studio" button funnel through the same
// *Opener, and a run's own retry (or the two racing) must not pile a second
// window onto Studio that is still booting from the first.
func TestOpenProjectDoesNotRelaunchAPendingPlace(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "default.project.json"), []byte(`{"name":"t","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	launches := 0
	o := &Opener{
		Rojo:         &fakeBuilder{},
		DetectStudio: func() (string, error) { return "C:\\Studio.exe", nil },
		Launch:       func(string, string) error { launches++; return nil },
	}
	first, err := o.OpenProject(context.Background(), project, "My Game", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	second, err := o.OpenProject(context.Background(), project, "My Game", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("place=%q on the second call, want the same %q", second, first)
	}
	if launches != 1 {
		t.Fatalf("launches=%d, want exactly 1", launches)
	}
}

// The grace window outlives the provisioner's own openWait (45s), because
// Studio may still be genuinely booting past it — a caller that gave up
// waiting must not read that as "nothing happened" and relaunch.
func TestOpenProjectStaysPendingPastOpenWaitButWithinGrace(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "default.project.json"), []byte(`{"name":"t","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	launches := 0
	now := time.Now()
	o := &Opener{
		Rojo:         &fakeBuilder{},
		DetectStudio: func() (string, error) { return "C:\\Studio.exe", nil },
		Launch:       func(string, string) error { launches++; return nil },
		clock:        func() time.Time { return now },
	}
	if _, err := o.OpenProject(context.Background(), project, "My Game", "abc123"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(50 * time.Second) // past openWait's 45s, still under the 90s grace
	if _, err := o.OpenProject(context.Background(), project, "My Game", "abc123"); err != nil {
		t.Fatal(err)
	}
	if launches != 1 {
		t.Fatalf("a second attempt within the grace window relaunched Studio, launches=%d", launches)
	}
}

// Once the grace window fully elapses the guard must not wedge shut forever —
// a place genuinely closed and reopened later is a legitimate new launch.
func TestOpenProjectRelaunchesAfterGraceExpires(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "default.project.json"), []byte(`{"name":"t","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	launches := 0
	now := time.Now()
	o := &Opener{
		Rojo:         &fakeBuilder{},
		DetectStudio: func() (string, error) { return "C:\\Studio.exe", nil },
		Launch:       func(string, string) error { launches++; return nil },
		clock:        func() time.Time { return now },
	}
	if _, err := o.OpenProject(context.Background(), project, "My Game", "abc123"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(91 * time.Second)
	if _, err := o.OpenProject(context.Background(), project, "My Game", "abc123"); err != nil {
		t.Fatal(err)
	}
	if launches != 2 {
		t.Fatalf("launches=%d, want 2 once the grace window has fully elapsed", launches)
	}
}

// A build failure must not hold the guard: nothing was actually launched, so
// an operator fixing the problem and retrying must not be blocked for the
// rest of the grace window over a launch that never happened.
func TestOpenProjectReleasesTheGuardAfterAFailedBuild(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "default.project.json"), []byte(`{"name":"t","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	launches := 0
	fb := &failingBuilder{err: os.ErrPermission}
	o := &Opener{
		Rojo:         fb,
		DetectStudio: func() (string, error) { return "C:\\Studio.exe", nil },
		Launch:       func(string, string) error { launches++; return nil },
	}
	if _, err := o.OpenProject(context.Background(), project, "My Game", "abc123"); err == nil {
		t.Fatal("expected the build failure to surface")
	}
	fb.err = nil
	if _, err := o.OpenProject(context.Background(), project, "My Game", "abc123"); err != nil {
		t.Fatal(err)
	}
	if launches != 1 {
		t.Fatalf("launches=%d, want exactly 1 after the retry succeeded", launches)
	}
}

// Two callers racing for the same place — the shape of the provisioner's
// auto-open and the manual button firing at once — must still launch Studio
// exactly once.
func TestOpenProjectConcurrentCallsLaunchExactlyOnce(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "default.project.json"), []byte(`{"name":"t","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	launches := 0
	o := &Opener{
		Rojo:         &fakeBuilder{},
		DetectStudio: func() (string, error) { return "C:\\Studio.exe", nil },
		Launch: func(string, string) error {
			mu.Lock()
			launches++
			mu.Unlock()
			return nil
		},
	}
	const callers = 8
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer done.Done()
			start.Wait()
			if _, err := o.OpenProject(context.Background(), project, "My Game", "abc123"); err != nil {
				t.Error(err)
			}
		}()
	}
	start.Done()
	done.Wait()
	mu.Lock()
	got := launches
	mu.Unlock()
	if got != 1 {
		t.Fatalf("launches=%d, want exactly 1 across %d concurrent callers", got, callers)
	}
}

// failingBuilder lets a test flip a build failure on and off, to check the
// pending guard is released rather than held for the whole grace window.
type failingBuilder struct{ err error }

func (f *failingBuilder) Build(_ context.Context, _, output string) error {
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(output, []byte("RBLX"), 0o600)
}
func (f *failingBuilder) InstallPlugin(context.Context) error { return nil }

// A missing Studio install must fail before any place is built.
func TestOpenProjectFailsFastWhenStudioMissing(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "default.project.json"), []byte(`{"name":"t","tree":{"$className":"DataModel"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBuilder{}
	o := &Opener{
		Rojo:         fb,
		DetectStudio: func() (string, error) { return "", os.ErrNotExist },
		Launch:       func(string, string) error { t.Fatal("must not launch when Studio is missing"); return nil },
	}
	if _, err := o.OpenProject(context.Background(), project, "My Game", "abc123"); err == nil {
		t.Error("a missing Studio must be reported")
	}
	if fb.built {
		t.Error("no place should be built when Studio is missing")
	}
}
