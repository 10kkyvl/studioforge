//go:build windows

package processes

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// BenchmarkAttachJob isolates the Attach step of confinement (job assignment
// plus resuming the suspended main thread), which is dominated by the
// Toolhelp thread snapshot in resumeMainThread and so scales with
// system-wide thread count rather than with our own process. Spawning the
// child and reaping it happen outside the timed section.
func BenchmarkAttachJob(b *testing.B) {
	tmp := b.TempDir()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=TestHelperProcess")
		cmd.Env = append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1")
		configureProcessTree(cmd)
		conf, err := applyConfinement(cmd, Spec{Kind: "bench", Confine: ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{tmp}}})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := cmd.StdoutPipe(); err != nil {
			b.Fatal(err)
		}
		if _, err := cmd.StderrPipe(); err != nil {
			b.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if err := conf.Attach(cmd); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		_ = cmd.Wait()
		_ = conf.Close()
	}
}
