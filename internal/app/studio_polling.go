package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/10kkyvl/studioforge/internal/api"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

// studioProbeCoordinator serializes launcher probes with the lifetime of every
// Studio grant. A poll which has already started is cancelled when a run asks
// for a grant; the run then waits for that probe to finish before opening its
// own launcher connection. This is deliberately conservative: if a probe does
// not observe cancellation, the run waits instead of competing for Studio's
// single WS host slot.
type studioProbeCoordinator struct {
	mu          sync.Mutex
	active      int
	pending     int
	refreshing  bool
	refreshStop context.CancelFunc
	refreshDone chan struct{}
	quietUntil  time.Time
	resumeGap   time.Duration
}

func newStudioProbeCoordinator(resumeGap time.Duration) *studioProbeCoordinator {
	if resumeGap <= 0 {
		resumeGap = 5 * time.Second
	}
	return &studioProbeCoordinator{resumeGap: resumeGap}
}

// beginGrant reserves the Studio slot for a run. It may wait for and cancel a
// refresh already in progress, but it never lets a grant and a refresh overlap.
func (c *studioProbeCoordinator) beginGrant(ctx context.Context) (func(), bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	c.mu.Lock()
	c.pending++
	c.mu.Unlock()
	for {
		c.mu.Lock()
		if !c.refreshing && ctx.Err() == nil {
			c.pending--
			c.active++
			c.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					c.mu.Lock()
					c.active--
					if c.active == 0 {
						c.quietUntil = time.Now().Add(c.resumeGap)
					}
					c.mu.Unlock()
				})
			}, true
		}
		stop := c.refreshStop
		done := c.refreshDone
		if stop != nil {
			stop()
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			c.mu.Lock()
			c.pending--
			c.mu.Unlock()
			return nil, false
		case <-done:
		}
	}
}

// beginRefresh reserves a launcher probe for a background or manual listing.
// A false result means the caller must leave the stored list untouched: a run
// is active, another refresh owns the probe, or the post-run quiet gap is in
// effect. The returned context is cancelled when a grant arrives mid-poll.
func (c *studioProbeCoordinator) beginRefresh(ctx context.Context) (context.Context, func(), bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active > 0 || c.pending > 0 || c.refreshing || time.Now().Before(c.quietUntil) {
		return nil, nil, false
	}
	probeCtx, cancel := context.WithCancel(ctx)
	c.refreshing = true
	c.refreshStop = cancel
	c.refreshDone = make(chan struct{})
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			c.mu.Lock()
			c.refreshing = false
			c.refreshStop = nil
			close(c.refreshDone)
			c.refreshDone = nil
			c.mu.Unlock()
		})
	}
	return probeCtx, release, true
}

func (c *studioProbeCoordinator) suspended() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active > 0 || c.pending > 0 || c.refreshing || time.Now().Before(c.quietUntil)
}

// studioRefreshState is the small, thread-safe state shared by the API and
// the polling loop. The API owns the JSON shape; the app only updates it.
type studioRefreshState struct {
	coordinator *studioProbeCoordinator
	enabled     atomic.Bool
	lastUnix    atomic.Int64
}

func newStudioRefreshState(c *studioProbeCoordinator, enabled bool) *studioRefreshState {
	s := &studioRefreshState{coordinator: c}
	s.enabled.Store(enabled)
	return s
}

func (s *studioRefreshState) setEnabled(enabled bool) { s.enabled.Store(enabled) }

func (s *studioRefreshState) markRefreshed() { s.lastUnix.Store(time.Now().UTC().UnixNano()) }

func (s *studioRefreshState) snapshot() api.StudioSessionsState {
	var last *time.Time
	if stamp := s.lastUnix.Load(); stamp != 0 {
		value := time.Unix(0, stamp).UTC()
		last = &value
	}
	return api.StudioSessionsState{
		Enabled:       s.enabled.Load(),
		Suspended:     s.coordinator.suspended(),
		LastRefreshed: last,
	}
}

// gatedStudioRefresh adds the safety gate to both the manual endpoint and the
// background ticker. Cancellation caused by a grant is an expected transition
// and is reported as a quiet no-op; the next safe pass will correct the list.
func gatedStudioRefresh(
	refresh func(context.Context) (bool, error),
	coordinator *studioProbeCoordinator,
	state *studioRefreshState,
) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		probeCtx, release, ok := coordinator.beginRefresh(ctx)
		if !ok {
			return true, nil
		}
		defer release()
		detected, err := refresh(probeCtx)
		if probeCtx.Err() != nil && ctx.Err() == nil {
			return detected, nil
		}
		if err == nil {
			state.markRefreshed()
		}
		return detected, err
	}
}

// gatedStudioValidation keeps the daemon's own playtest connection under the
// same grant accounting as provider connections. It is separate from a run's
// grant because OpenRouter and NVIDIA release their live client when the agent
// loop exits, before scheduler validation begins.
func gatedStudioValidation(ctx context.Context, coordinator *studioProbeCoordinator, validate func(context.Context) mcp.ValidationResult) mcp.ValidationResult {
	gateRelease, ok := coordinator.beginGrant(ctx)
	if !ok {
		return mcp.ValidationResult{Outcome: mcp.ValidationInconclusive, Notice: "Studio validation was cancelled while a Studio sessions refresh was being stopped"}
	}
	defer gateRelease()
	return validate(ctx)
}

// startStudioPoller keeps the timer out of the API package and makes its
// cancellation explicit at daemon shutdown. The interval is read each tick,
// so a settings change takes effect without restarting StudioForge.
func startStudioPoller(ctx context.Context, interval func() time.Duration, refresh func(context.Context) (bool, error), state *studioRefreshState) {
	go func() {
		for {
			delay := interval()
			if delay <= 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
				continue
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
			if interval() <= 0 {
				continue
			}
			_, _ = refresh(ctx)
		}
	}()
}
