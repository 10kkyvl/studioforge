package processes

import (
	"context"
	"fmt"
	"os"
	"strings"
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
