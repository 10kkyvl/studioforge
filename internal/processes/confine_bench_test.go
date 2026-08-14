package processes

import (
	"context"
	"os"
	"testing"
)

// BenchmarkStartUnconfined and BenchmarkStartConfined measure Start->Wait for
// a helper process that exits immediately, with and without ConfineAgent, so
// the confinement startup cost (an acceptance criterion of issue #27) can be
// read off as the delta between the two.

func BenchmarkStartUnconfined(b *testing.B) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	spec := Spec{
		ID:          "bench-unconfined",
		Kind:        "bench",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineNone},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		process, err := supervisor.Start(context.Background(), spec)
		if err != nil {
			b.Fatal(err)
		}
		process.Wait()
	}
}

func BenchmarkStartConfined(b *testing.B) {
	supervisor := NewSupervisor()
	defer supervisor.Close(context.Background())
	root := b.TempDir()
	spec := Spec{
		ID:          "bench-confined",
		Kind:        "bench",
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestHelperProcess"},
		Environment: append(MinimalEnvironment(nil), "STUDIOFORGE_HELPER=1"),
		Confine:     ConfinementPolicy{Mode: ConfineAgent, WritableRoots: []string{root}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		process, err := supervisor.Start(context.Background(), spec)
		if err != nil {
			b.Fatal(err)
		}
		process.Wait()
	}
}
