package resources

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

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
