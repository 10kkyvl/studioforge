package nvidia

import (
	"context"
	"testing"
	"time"
)

func TestSupportedModelsAreVerifiedAgentModels(t *testing.T) {
	want := []string{
		"z-ai/glm-5.2",
		"nvidia/nemotron-3-ultra-550b-a55b",
		"moonshotai/kimi-k2.6",
		"deepseek-ai/deepseek-v4-pro",
	}
	models := Models()
	if len(models) != len(want) {
		t.Fatalf("Models() count = %d, want %d", len(models), len(want))
	}
	for i, id := range want {
		if models[i].ID != id {
			t.Errorf("Models()[%d].ID = %q, want %q", i, models[i].ID, id)
		}
		info, ok := modelInfo(id)
		if !ok || !info.Verified || !info.Tools || !info.PriceKnown {
			t.Errorf("modelInfo(%q) = %+v, %v; want verified free tool model", id, info, ok)
		}
	}
}

func TestPacedTransportSpacesConcurrentReservations(t *testing.T) {
	limiter := &pacedTransport{interval: 15 * time.Millisecond}
	start := time.Now()
	if err := limiter.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := limiter.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 12*time.Millisecond {
		t.Fatalf("second reservation arrived after %s; requests were not paced", elapsed)
	}
}

func TestPacedTransportWaitHonorsCancellation(t *testing.T) {
	limiter := &pacedTransport{interval: time.Hour}
	if err := limiter.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := limiter.wait(ctx); err == nil {
		t.Fatal("wait returned nil for a cancelled context")
	}
}
