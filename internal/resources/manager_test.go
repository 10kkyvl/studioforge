package resources

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// A caller pacing its own renewal (rather than the regular per-run heartbeat
// loop) needs the manager's actual configured TTL, not a guess.
func TestTTLReportsTheConfiguredLifetime(t *testing.T) {
	if got := NewManager(7 * time.Second).TTL(); got != 7*time.Second {
		t.Errorf("TTL=%v, want 7s", got)
	}
	if got := NewManager(0).TTL(); got != 30*time.Second {
		t.Errorf("TTL=%v, want the 30s default when zero is passed", got)
	}
}

func TestSortedAtomicAcquisitionPreventsDeadlock(t *testing.T) {
	m := NewManager(time.Second)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first, err := m.Acquire(ctx, "a", []string{"project:a:write", "studio:1"})
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan *Handle, 1)
	go func() { h, _ := m.Acquire(ctx, "b", []string{"studio:1", "project:a:write"}); acquired <- h }()
	select {
	case <-acquired:
		t.Fatal("second owner acquired locked resources")
	case <-time.After(50 * time.Millisecond):
	}
	first.Release()
	select {
	case h := <-acquired:
		if h == nil {
			t.Fatal("acquisition failed")
		}
		h.Release()
	case <-ctx.Done():
		t.Fatal("deadlock")
	}
}
func TestAcquireCancellationAndHeartbeat(t *testing.T) {
	m := NewManager(80 * time.Millisecond)
	defer m.Close()
	h, err := m.Acquire(context.Background(), "a", []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = m.Acquire(ctx, "b", []string{"x"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	if err := h.Heartbeat(); err != nil {
		t.Fatal(err)
	}
	h.Release()
	if len(m.Snapshot()) != 0 {
		t.Fatal("lease was not released")
	}
}
func TestConcurrentDifferentResources(t *testing.T) {
	m := NewManager(time.Second)
	defer m.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h, err := m.Acquire(context.Background(), string(rune('a'+i)), []string{string(rune('A' + i))})
			if err != nil {
				t.Error(err)
				return
			}
			h.Release()
		}(i)
	}
	wg.Wait()
}
