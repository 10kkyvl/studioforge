package processes

import (
	"context"
	"fmt"
	"os"
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
