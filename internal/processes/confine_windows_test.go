//go:build windows

package processes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsProcessInJob is not exposed by golang.org/x/sys/windows; it is declared
// here for tests only.
var procIsProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func isProcessInJob(process, job windows.Handle) (bool, error) {
	var result uint32
	r1, _, callErr := procIsProcessInJob.Call(uintptr(process), uintptr(job), uintptr(unsafe.Pointer(&result)))
	if r1 == 0 {
		return false, callErr
	}
	return result != 0, nil
}

func TestConfinedProcessJoinsJob(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "job-membership",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_HANG=1"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{t.TempDir()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Terminate(50 * time.Millisecond) }()

	jc, ok := process.confinement.(*jobConfinement)
	if !ok {
		t.Fatalf("process.confinement = %T, want *jobConfinement", process.confinement)
	}

	queryHandle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(process.PID()))
	if err != nil {
		t.Fatalf("OpenProcess: %v", err)
	}
	defer windows.CloseHandle(queryHandle)

	inJob, err := isProcessInJob(queryHandle, jc.job)
	if err != nil {
		t.Fatalf("IsProcessInJob: %v", err)
	}
	if !inJob {
		t.Fatal("expected process to be a member of its confinement job")
	}
}

func TestJobKillsOrphanedGrandchild(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "orphan-alive.txt")
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())

	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "orphan-grandchild",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_SPAWN_ORPHAN="+marker),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{t.TempDir()}},
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
			t.Fatal("orphaned grandchild never started writing its marker file")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := process.Terminate(2 * time.Second); err != nil {
		t.Logf("Terminate: %v", err)
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
		t.Fatalf("orphaned grandchild kept writing its marker after the job should have killed it: %q -> %q", first, second)
	}
}

func TestActiveProcessLimitRefusesFork(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "active-process-limit",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1", "STUDIOFORGE_HELPER_SPAWN_MANY=32"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, MaxProcesses: 8, WritableRoots: []string{t.TempDir()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout string
	for line := range process.Lines() {
		if line.Stream == "stdout" {
			stdout += line.Text
		}
	}
	result := process.Wait()
	if result.ExitCode != 0 {
		t.Fatalf("helper exited %d, want 0; stdout=%q", result.ExitCode, stdout)
	}
	m := regexp.MustCompile(`SPAWN_FAILURES=(\d+)`).FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("stdout did not contain SPAWN_FAILURES: %q", stdout)
	}
	failures, _ := strconv.Atoi(m[1])
	if failures < 1 {
		t.Fatalf("expected at least one spawn to fail under MaxProcesses=8, got 0 failures; stdout=%q", stdout)
	}
}

func TestProbeConfinementSucceedsOnWindows(t *testing.T) {
	if err := ProbeConfinement(); err != nil {
		t.Fatalf("ProbeConfinement() = %v, want nil on windows", err)
	}
}

// TestResumeMainThreadFallbackStillWorks forces the NtResumeProcess fast
// path to fail so Attach falls back to resumeMainThread (the documented
// Toolhelp path), and checks the process still joins its job and runs to
// completion. This is the safety net the fast path depends on, so it has to
// keep working on its own.
func TestResumeMainThreadFallbackStillWorks(t *testing.T) {
	original := resumeProcess
	defer func() { resumeProcess = original }()
	resumeProcess = func(windows.Handle) error {
		return errors.New("injected NtResumeProcess failure")
	}

	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "resume-fallback",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{t.TempDir()}},
	})
	if err != nil {
		t.Fatal(err)
	}

	jc, ok := process.confinement.(*jobConfinement)
	if !ok {
		t.Fatalf("process.confinement = %T, want *jobConfinement", process.confinement)
	}
	queryHandle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(process.PID()))
	if err != nil {
		t.Fatalf("OpenProcess: %v", err)
	}
	defer windows.CloseHandle(queryHandle)
	inJob, err := isProcessInJob(queryHandle, jc.job)
	if err != nil {
		t.Fatalf("IsProcessInJob: %v", err)
	}
	if !inJob {
		t.Fatal("expected process to be a member of its confinement job")
	}

	if result := process.Wait(); result.ExitCode != 7 {
		t.Fatalf("result = %+v, want ExitCode 7", result)
	}
}

func TestConfinementFailureDoesNotStart(t *testing.T) {
	original := newJobObject
	defer func() { newJobObject = original }()
	injected := errors.New("injected job object failure")
	newJobObject = func() (windows.Handle, error) { return 0, injected }

	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "confinement-fails",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent},
	})
	if err == nil {
		t.Fatal("expected Start to fail when job object creation fails")
	}
	if !strings.Contains(err.Error(), "confine") {
		t.Fatalf("err = %v, want it to mention confinement", err)
	}
	if !errors.Is(err, injected) {
		t.Fatalf("err = %v, want it to wrap the injected error", err)
	}
	if process != nil {
		t.Fatalf("expected nil process, got %+v", process)
	}
	if len(supervisor.processes) != 0 {
		t.Fatalf("supervisor.processes = %v, want empty", supervisor.processes)
	}
}

func TestStrictNetworkPolicyRefusesToStartOnWindows(t *testing.T) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	process, err := supervisor.Start(context.Background(), Spec{
		ID:          "strict-network-policy",
		Kind:        "test",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, Network: NetworkRegistryOnly, WritableRoots: []string{t.TempDir()}},
	})
	if err == nil {
		t.Fatal("expected Start to fail when a strict network policy cannot be enforced on windows")
	}
	if !errors.Is(err, ErrNetworkPolicyUnsupported) {
		t.Fatalf("err = %v, want it to wrap ErrNetworkPolicyUnsupported", err)
	}
	if process != nil {
		t.Fatalf("expected nil process, got %+v", process)
	}
	if len(supervisor.processes) != 0 {
		t.Fatalf("supervisor.processes = %v, want empty", supervisor.processes)
	}
}
