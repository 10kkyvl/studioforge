package nvidia

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// pacedTransport spaces every outbound attempt, including retries, so all
// concurrent NVIDIA agent loops share one conservative free-tier allowance.
// A burst of one keeps a new session responsive while preventing 40-agent
// spikes from all reaching the service at once.
type pacedTransport struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
	base     http.RoundTripper
}

func (t *pacedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.wait(req.Context()); err != nil {
		return nil, err
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func (t *pacedTransport) wait(ctx context.Context) error {
	t.mu.Lock()
	now := time.Now()
	slot := now
	if t.next.After(slot) {
		slot = t.next
	}
	t.next = slot.Add(t.interval)
	t.mu.Unlock()

	delay := time.Until(slot)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
