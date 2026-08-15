package processes

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("STUDIOFORGE_HELPER") != "1" {
		return
	}
	fmt.Println("helper stdout")
	fmt.Fprintln(os.Stderr, "helper stderr")
	if marker := os.Getenv("STUDIOFORGE_HELPER_CHILD_MARKER"); marker != "" {
		for i := 0; ; i++ {
			_ = os.WriteFile(marker, []byte(strconv.Itoa(i)), 0o644)
			time.Sleep(20 * time.Millisecond)
		}
	}
	if marker := os.Getenv("STUDIOFORGE_HELPER_SPAWN_CHILD"); marker != "" {
		child := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
		child.Env = append(os.Environ(), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_CHILD_MARKER="+marker)
		if err := child.Start(); err != nil {
			os.Exit(9)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	if marker := os.Getenv("STUDIOFORGE_HELPER_SPAWN_ORPHAN"); marker != "" {
		child := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
		child.Env = append(os.Environ(), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_CHILD_MARKER="+marker)
		if err := child.Start(); err != nil {
			os.Exit(9)
		}
		// Give the grandchild a moment of real wall-clock time to start
		// writing before this process exits, so the test can observe it
		// alive before the job (or, in the unconfined control, nothing)
		// reaps it. It is still orphaned: once this process exits, it is no
		// longer any tracked process's child, which is what defeats
		// taskkill /T's parent-PID walk.
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}
	if raw := os.Getenv("STUDIOFORGE_HELPER_SPAWN_MANY"); raw != "" {
		n, _ := strconv.Atoi(raw)
		failures := 0
		for i := 0; i < n; i++ {
			child := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
			child.Env = append(os.Environ(), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_HANG=1")
			if err := child.Start(); err != nil {
				failures++
				continue
			}
		}
		fmt.Printf("SPAWN_FAILURES=%d\n", failures)
		os.Exit(0)
	}
	if os.Getenv("STUDIOFORGE_HELPER_HANG") == "1" {
		for {
			time.Sleep(time.Second)
		}
	}
	os.Exit(7)
}
func TestSupervisorCapturesStreamsAndExit(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{ID: "helper", Kind: "test", Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1")})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr string
	for line := range process.Lines() {
		if line.Stream == "stdout" {
			stdout += line.Text
		} else {
			stderr += line.Text
		}
	}
	result := process.Wait()
	if result.ExitCode != 7 || result.Err == nil {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(stdout, "helper stdout") || !strings.Contains(stderr, "helper stderr") {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}
func TestCollectDropsLinesUnderBackpressureAndCountsThem(t *testing.T) {
	p := &Process{lines: make(chan Line, 1), spec: Spec{ID: "test-proc", Kind: "test"}}
	p.collectors.Add(1)
	p.collect(strings.NewReader(strings.Repeat("line\n", 10)), "stdout")
	if got := p.DroppedLines(); got != 9 {
		t.Fatalf("DroppedLines() = %d, want 9", got)
	}
}
func TestStartRejectsDuplicateIDConcurrently(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	spec := Spec{ID: "dup-start", Kind: "test", Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1")}

	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	var results [2]*Process
	var errs [2]error
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		i := i
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			results[i], errs[i] = supervisor.Start(context.Background(), spec)
		}()
	}
	ready.Wait()
	close(start)
	wg.Wait()

	var winner *Process
	var loserErr error
	switch {
	case errs[0] == nil && errs[1] != nil:
		winner, loserErr = results[0], errs[1]
	case errs[1] == nil && errs[0] != nil:
		winner, loserErr = results[1], errs[0]
	default:
		t.Fatalf("expected exactly one Start to succeed, got errs=%v results=%v", errs, results)
	}
	if !strings.Contains(loserErr.Error(), "already exists") {
		t.Fatalf("loser error = %q, want it to contain %q", loserErr.Error(), "already exists")
	}
	if winner == nil {
		t.Fatal("winning Start returned a nil process")
	}
	if result := winner.Wait(); result.ExitCode != 7 {
		t.Fatalf("winner result = %+v, want ExitCode 7", result)
	}

	process, err := supervisor.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start after winner exited: %v", err)
	}
	if r := process.Wait(); r.ExitCode != 7 {
		t.Fatalf("reused id result = %+v, want ExitCode 7", r)
	}
}
func TestStartFailureLeavesNoLingeringReservation(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	id := "reserve-fail"

	if _, err := supervisor.Start(context.Background(), Spec{ID: id, Kind: "test", Executable: "studioforge-test-definitely-missing-binary"}); err == nil {
		t.Fatal("expected Start with a missing executable to fail")
	}

	process, err := supervisor.Start(context.Background(), Spec{ID: id, Kind: "test", Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1")})
	if err != nil {
		t.Fatalf("Start after failed start should succeed, got: %v", err)
	}
	if r := process.Wait(); r.ExitCode != 7 {
		t.Fatalf("result = %+v, want ExitCode 7", r)
	}
}
func TestSupervisorTerminatesProcessTree(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{ID: "hang", Kind: "test", Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_HANG=1")})
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Terminate(50 * time.Millisecond); err != nil && !strings.Contains(strings.ToLower(err.Error()), "interrupt") {
		t.Logf("graceful termination unavailable: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process was not terminated")
	}
}

// TestStartSetsWaitDelayForMaxRuntimeButNotUnconfined asserts the unconfined
// case only: WaitDelay is set when MaxRuntime > 0, and stays unset otherwise.
// Confinement also sets WaitDelay (see confine_test.go); that is intentional
// and covered separately, not by this test.
func TestStartSetsWaitDelayForMaxRuntimeButNotUnconfined(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())

	withTimeout, err := supervisor.Start(context.Background(), Spec{ID: "wire-timeout", Kind: "test", Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_HANG=1"), MaxRuntime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = withTimeout.Terminate(50 * time.Millisecond) }()
	if withTimeout.cmd.WaitDelay == 0 {
		t.Fatal("expected cmd.WaitDelay to be set when MaxRuntime > 0")
	}

	noTimeout, err := supervisor.Start(context.Background(), Spec{ID: "wire-no-timeout", Kind: "test", Executable: os.Args[0], Args: []string{"-test.run=TestHelperProcess"}, Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1")})
	if err != nil {
		t.Fatal(err)
	}
	_ = noTimeout.Wait()
	if noTimeout.cmd.WaitDelay != 0 {
		t.Fatal("expected cmd.WaitDelay to remain unset when MaxRuntime is unset")
	}
}
func TestSupervisorMaxRuntimeKillsProcessTree(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "grandchild-alive.txt")
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())

	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "maxruntime-tree",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_SPAWN_CHILD="+marker),
		MaxRuntime:  500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, statErr := os.Stat(marker); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("grandchild never started writing its marker file; skipping tree-kill assertion")
		}
		time.Sleep(10 * time.Millisecond)
	}

	done := make(chan struct{})
	go func() { _ = process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("MaxRuntime timeout did not terminate the process")
	}

	read := func() string {
		b, statErr := os.ReadFile(marker)
		if statErr != nil {
			return ""
		}
		return string(b)
	}
	time.Sleep(150 * time.Millisecond)
	first := read()
	time.Sleep(400 * time.Millisecond)
	second := read()
	if first != second {
		t.Fatalf("grandchild kept writing its marker after MaxRuntime should have killed the process tree: %q -> %q", first, second)
	}
}
func TestScrubbedEnvironmentRemovesOnlyDeniedKeys(t *testing.T) {
	t.Setenv("STUDIOFORGE_TEST_DENIED", "should-be-removed")
	t.Setenv("STUDIOFORGE_TEST_KEPT", "should-survive")

	deny := func(key string) bool { return key == "STUDIOFORGE_TEST_DENIED" }
	got := ScrubbedEnvironment(deny, []string{"STUDIOFORGE_TEST_EXTRA=added"})

	var sawDenied, sawKept, sawExtra bool
	for _, entry := range got {
		switch entry {
		case "STUDIOFORGE_TEST_DENIED=should-be-removed":
			sawDenied = true
		case "STUDIOFORGE_TEST_KEPT=should-survive":
			sawKept = true
		case "STUDIOFORGE_TEST_EXTRA=added":
			sawExtra = true
		}
	}
	if sawDenied {
		t.Fatal("a key the deny function rejects must not appear in the result")
	}
	if !sawKept {
		t.Fatal("a key the deny function does not reject must survive unchanged")
	}
	if !sawExtra {
		t.Fatal("extra entries must be appended to the result")
	}
}
func TestProxyEnvironmentOverridesTheOperatorProxyInBothCases(t *testing.T) {
	got := ProxyEnvironment("http://127.0.0.1:9999")
	want := map[string]string{
		"HTTP_PROXY":  "http://127.0.0.1:9999",
		"HTTPS_PROXY": "http://127.0.0.1:9999",
		"ALL_PROXY":   "http://127.0.0.1:9999",
		"NO_PROXY":    "",
		"http_proxy":  "http://127.0.0.1:9999",
		"https_proxy": "http://127.0.0.1:9999",
		"all_proxy":   "http://127.0.0.1:9999",
		"no_proxy":    "",
	}
	found := map[string]string{}
	for _, entry := range got {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("entry %q is not a KEY=VALUE pair", entry)
		}
		found[key] = value
	}
	for key, value := range want {
		if got, ok := found[key]; !ok || got != value {
			t.Fatalf("entry for %s = %q (present=%v), want %q", key, got, ok, value)
		}
	}
}
func TestStripEnvRemovesLowercaseProxyVariables(t *testing.T) {
	env := []string{"http_proxy=operator-proxy", "HTTPS_PROXY=operator-proxy", "PATH=/usr/bin"}
	got := StripEnv(env, ProxyEnvKeys...)
	for _, entry := range got {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY":
			t.Fatalf("StripEnv left a proxy variable behind: %q", entry)
		}
	}
	var sawPath bool
	for _, entry := range got {
		if entry == "PATH=/usr/bin" {
			sawPath = true
		}
	}
	if !sawPath {
		t.Fatal("StripEnv must not remove unrelated variables")
	}
}
