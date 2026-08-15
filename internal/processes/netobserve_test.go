package processes

import (
	"context"
	"net/netip"
	"runtime"
	"sync"
	"testing"
	"time"
)

type fakeNetworkObserver struct {
	mu        sync.Mutex
	supported bool
	samples   [][]netip.AddrPort
	calls     int
}

func (f *fakeNetworkObserver) Supported() bool { return f.supported }

func (f *fakeNetworkObserver) Sample() ([]netip.AddrPort, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls >= len(f.samples) {
		f.calls++
		return nil, nil
	}
	sample := f.samples[f.calls]
	f.calls++
	return sample, nil
}

func TestObserveNetworkReportsUnsupportedWhereItCannotBeDetermined(t *testing.T) {
	observer := &fakeNetworkObserver{supported: false}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observation := ObserveNetwork(ctx, observer, time.Millisecond)
	if observation.Supported {
		t.Fatalf("observation.Supported = true, want false")
	}
	if len(observation.Endpoints) != 0 {
		t.Fatalf("observation.Endpoints = %v, want empty", observation.Endpoints)
	}
}

func TestObserveNetworkFiltersLoopbackAndPrivateAddresses(t *testing.T) {
	loopback := netip.MustParseAddrPort("127.0.0.1:80")
	private := netip.MustParseAddrPort("10.0.0.5:443")
	linkLocal := netip.MustParseAddrPort("169.254.1.1:80")
	public4 := netip.MustParseAddrPort("93.184.216.34:443")
	public6 := netip.MustParseAddrPort("[2606:2800:220:1:248:1893:25c8:1946]:443")

	observer := &fakeNetworkObserver{
		supported: true,
		samples:   [][]netip.AddrPort{{loopback, private, linkLocal, public4, public6}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	observation := ObserveNetwork(ctx, observer, time.Hour)
	if !observation.Supported {
		t.Fatalf("observation.Supported = false, want true")
	}
	got := map[netip.AddrPort]bool{}
	for _, ep := range observation.Endpoints {
		got[ep] = true
	}
	if !got[public4] || !got[public6] {
		t.Fatalf("observation.Endpoints = %v, want it to contain %v and %v", observation.Endpoints, public4, public6)
	}
	if got[loopback] || got[private] || got[linkLocal] {
		t.Fatalf("observation.Endpoints = %v, want loopback/private/link-local filtered out", observation.Endpoints)
	}
}

func TestObserveNetworkStopsOnContextCancellation(t *testing.T) {
	observer := &fakeNetworkObserver{supported: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan NetworkObservation, 1)
	go func() {
		done <- ObserveNetwork(ctx, observer, time.Millisecond)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ObserveNetwork did not return after context cancellation")
	}
}

func TestObserveNetworkDoesNotLeakGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		observer := &fakeNetworkObserver{supported: true}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ObserveNetwork(ctx, observer, time.Millisecond)
	}
	deadline := time.Now().Add(time.Second)
	for {
		runtime.GC()
		after := runtime.NumGoroutine()
		if after <= before+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count grew from %d to %d after repeated ObserveNetwork calls", before, after)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
