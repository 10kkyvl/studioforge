package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/10kkyvl/studioforge/internal/api"
)

func TestCachedStudioStatusServesRepeatAsksFromCache(t *testing.T) {
	var probes atomic.Int64
	status := cachedStudioStatus(func(context.Context, string) (api.StudioStatus, error) {
		probes.Add(1)
		return api.StudioStatus{Open: 1, Matched: 1}, nil
	})
	for range 5 {
		got, err := status(context.Background(), "project-1")
		if err != nil || got.Matched != 1 {
			t.Fatalf("status=%+v err=%v", got, err)
		}
	}
	// Each miss spawns a launcher process, so five polls must not mean five of
	// them competing with a running agent for Studio's WS host port.
	if probes.Load() != 1 {
		t.Fatalf("probes=%d, want 1", probes.Load())
	}
}

func TestCachedStudioStatusKeepsProjectsApart(t *testing.T) {
	status := cachedStudioStatus(func(_ context.Context, projectID string) (api.StudioStatus, error) {
		if projectID == "mine" {
			return api.StudioStatus{Open: 1, Matched: 1}, nil
		}
		return api.StudioStatus{Open: 1}, nil
	})
	mine, _ := status(context.Background(), "mine")
	theirs, _ := status(context.Background(), "theirs")
	if mine.Matched != 1 || theirs.Matched != 0 {
		t.Fatalf("mine=%+v theirs=%+v", mine, theirs)
	}
}

// Concurrent askers must share one probe rather than each starting a launcher.
func TestCachedStudioStatusCollapsesConcurrentProbes(t *testing.T) {
	var probes atomic.Int64
	release := make(chan struct{})
	status := cachedStudioStatus(func(context.Context, string) (api.StudioStatus, error) {
		probes.Add(1)
		<-release
		return api.StudioStatus{Open: 1}, nil
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = status(context.Background(), "project-1")
		}()
	}
	close(release)
	wg.Wait()
	if probes.Load() != 1 {
		t.Fatalf("probes=%d, want a single shared probe", probes.Load())
	}
}

func TestCachedStudioStatusCachesFailuresToo(t *testing.T) {
	var probes atomic.Int64
	status := cachedStudioStatus(func(context.Context, string) (api.StudioStatus, error) {
		probes.Add(1)
		return api.StudioStatus{}, errors.New("launcher unavailable")
	})
	for range 3 {
		if _, err := status(context.Background(), "project-1"); err == nil {
			t.Fatal("failure not reported")
		}
	}
	// A launcher that is failing is the worst case to retry per poll.
	if probes.Load() != 1 {
		t.Fatalf("probes=%d, want 1", probes.Load())
	}
}
