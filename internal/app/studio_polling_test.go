package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

func TestStudioProbeCoordinatorGrantCancelsRefreshBeforeAcquiring(t *testing.T) {
	c := newStudioProbeCoordinator(time.Hour)
	refreshCtx, releaseRefresh, ok := c.beginRefresh(context.Background())
	if !ok {
		t.Fatal("initial refresh was refused")
	}
	grantReady := make(chan func(), 1)
	go func() {
		release, acquired := c.beginGrant(context.Background())
		if acquired {
			grantReady <- release
		}
	}()
	select {
	case <-refreshCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("grant did not cancel the in-flight refresh")
	}
	select {
	case <-grantReady:
		t.Fatal("grant acquired before the refresh was drained")
	default:
	}
	releaseRefresh()
	select {
	case releaseGrant := <-grantReady:
		if !c.suspended() {
			t.Error("active grant must suspend polling")
		}
		releaseGrant()
	case <-time.After(time.Second):
		t.Fatal("grant did not acquire after refresh drained")
	}
	if !c.suspended() {
		t.Error("post-release quiet gap must suspend polling")
	}
}

func TestStudioProbeCoordinatorPendingGrantBlocksAnotherRefresh(t *testing.T) {
	c := newStudioProbeCoordinator(time.Hour)
	refreshCtx, releaseRefresh, ok := c.beginRefresh(context.Background())
	if !ok {
		t.Fatal("initial refresh was refused")
	}
	grantReady := make(chan func(), 1)
	go func() {
		release, acquired := c.beginGrant(context.Background())
		if acquired {
			grantReady <- release
		}
	}()
	select {
	case <-refreshCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("grant did not cancel the refresh")
	}
	releaseRefresh()
	select {
	case releaseGrant := <-grantReady:
		// A grant that has acquired the reservation must block any later poll.
		if _, _, ok := c.beginRefresh(context.Background()); ok {
			t.Fatal("an active grant must prevent another refresh")
		}
		releaseGrant()
	case <-time.After(time.Second):
		t.Fatal("grant did not acquire after refresh release")
	}
}

func TestGatedStudioRefreshDoesNotClaimFailedPass(t *testing.T) {
	c := newStudioProbeCoordinator(time.Millisecond)
	state := newStudioRefreshState(c, true)
	refresh := gatedStudioRefresh(func(context.Context) (bool, error) {
		return false, errors.New("launcher failed")
	}, c, state)
	if _, err := refresh(context.Background()); err == nil {
		t.Fatal("failed pass lost its error")
	}
	if got := state.snapshot().LastRefreshed; got != nil {
		t.Fatalf("failed pass recorded last refresh at %s", got)
	}
}

func TestGatedStudioValidationWaitsForRefreshToDrain(t *testing.T) {
	c := newStudioProbeCoordinator(time.Hour)
	refreshCtx, releaseRefresh, ok := c.beginRefresh(context.Background())
	if !ok {
		t.Fatal("initial refresh was refused")
	}
	resultReady := make(chan mcp.ValidationResult, 1)
	go func() {
		resultReady <- gatedStudioValidation(context.Background(), c, func(ctx context.Context) mcp.ValidationResult {
			return mcp.ValidationResult{Outcome: mcp.ValidationPassed}
		})
	}()
	select {
	case <-refreshCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("validation did not cancel the in-flight refresh")
	}
	releaseRefresh()
	select {
	case result := <-resultReady:
		if result.Outcome != mcp.ValidationPassed {
			t.Fatalf("result=%+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("validation did not start after refresh drained")
	}
}
