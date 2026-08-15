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
func TestTransferKeepsTheLeaseHeldThroughout(t *testing.T) {
	m := NewManager(time.Second)
	defer m.Close()
	first, err := m.Acquire(context.Background(), "a", []string{"project:a:write"})
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan *Handle, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		h, _ := m.Acquire(ctx, "waiter", []string{"project:a:write"})
		acquired <- h
	}()
	select {
	case <-acquired:
		t.Fatal("waiter acquired the resource before Transfer even ran")
	case <-time.After(50 * time.Millisecond):
	}
	second, err := first.Transfer("b")
	if err != nil {
		t.Fatalf("Transfer failed: %v", err)
	}
	select {
	case h := <-acquired:
		t.Fatalf("waiter acquired the resource across the transfer: %+v", h)
	case <-time.After(50 * time.Millisecond):
	}
	owners := m.Snapshot()
	if owners["project:a:write"] != "b" {
		t.Fatalf("owners=%+v, want project:a:write owned by b after Transfer", owners)
	}
	second.Release()
	select {
	case h := <-acquired:
		if h == nil {
			t.Fatal("waiter's acquisition failed after the transferred handle released")
		}
		h.Release()
	case <-ctx.Done():
		t.Fatal("waiter never acquired the resource after it was released")
	}
}

func TestTransferMakesTheOldHandleReleaseANoOp(t *testing.T) {
	m := NewManager(time.Second)
	defer m.Close()
	first, err := m.Acquire(context.Background(), "a", []string{"k"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := first.Transfer("b")
	if err != nil {
		t.Fatal(err)
	}
	first.Release()
	owners := m.Snapshot()
	if owners["k"] != "b" {
		t.Fatalf("owners=%+v, want k still owned by b after the old handle's Release", owners)
	}
	second.Release()
	if len(m.Snapshot()) != 0 {
		t.Fatal("the transferred handle's own Release did not free the resource")
	}
}

func TestTransferAfterReleaseFails(t *testing.T) {
	m := NewManager(time.Second)
	defer m.Close()
	h, err := m.Acquire(context.Background(), "a", []string{"k"})
	if err != nil {
		t.Fatal(err)
	}
	h.Release()
	if _, err := h.Transfer("b"); !errors.Is(err, ErrHandleReleased) {
		t.Fatalf("Transfer after Release: err=%v, want ErrHandleReleased", err)
	}
	if len(m.Snapshot()) != 0 {
		t.Fatal("a failed Transfer must not resurrect the released resource")
	}
}

func TestReleaseAfterTransferIsANoOpEvenIfCalledTwice(t *testing.T) {
	m := NewManager(time.Second)
	defer m.Close()
	h, err := m.Acquire(context.Background(), "a", []string{"k"})
	if err != nil {
		t.Fatal(err)
	}
	transferred, err := h.Transfer("b")
	if err != nil {
		t.Fatal(err)
	}
	h.Release()
	h.Release()
	if owners := m.Snapshot(); owners["k"] != "b" {
		t.Fatalf("owners=%+v, want k still owned by b after repeated Release on the old handle", owners)
	}
	transferred.Release()
}

func TestTransferOnAnEmptyKeyHandleStillSwapsOwner(t *testing.T) {
	m := NewManager(time.Second)
	defer m.Close()
	h, err := m.Acquire(context.Background(), "a", nil)
	if err != nil {
		t.Fatal(err)
	}
	transferred, err := h.Transfer("b")
	if err != nil {
		t.Fatal(err)
	}
	if transferred == nil {
		t.Fatal("Transfer on a zero-key handle must still return a usable handle")
	}
	if _, err := h.Transfer("c"); !errors.Is(err, ErrHandleReleased) {
		t.Fatalf("a second Transfer on the same handle: err=%v, want ErrHandleReleased", err)
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
