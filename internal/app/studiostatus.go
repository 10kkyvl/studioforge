package app

import (
	"context"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/api"
)

// studioStatusTTL is how stale the chat badge may be. Each miss spawns a Studio
// MCP launcher, so this trades a few seconds of lag for not spawning a process
// per poll — and for not competing with a running agent over the WS host port
// that decides which client is told about Studio's tools.
const studioStatusTTL = 8 * time.Second

// cachedStudioStatus memoises Studio status per project for studioStatusTTL.
// Concurrent askers about the same project share one probe rather than each
// starting their own.
func cachedStudioStatus(probe func(context.Context, string) (api.StudioStatus, error)) func(context.Context, string) (api.StudioStatus, error) {
	type entry struct {
		status api.StudioStatus
		err    error
		at     time.Time
	}
	var mu sync.Mutex
	cache := map[string]entry{}
	inflight := map[string]*sync.WaitGroup{}

	return func(ctx context.Context, projectID string) (api.StudioStatus, error) {
		for {
			mu.Lock()
			if hit, ok := cache[projectID]; ok && time.Since(hit.at) < studioStatusTTL {
				mu.Unlock()
				return hit.status, hit.err
			}
			if wait, running := inflight[projectID]; running {
				mu.Unlock()
				wait.Wait()
				continue // The probe that just finished left a fresh entry.
			}
			wait := &sync.WaitGroup{}
			wait.Add(1)
			inflight[projectID] = wait
			mu.Unlock()

			status, err := probe(ctx, projectID)

			mu.Lock()
			cache[projectID] = entry{status: status, err: err, at: time.Now()}
			delete(inflight, projectID)
			mu.Unlock()
			wait.Done()
			return status, err
		}
	}
}
