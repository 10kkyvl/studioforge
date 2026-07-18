package claudecode

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/providers"
)

func TestRealClaudeSmoke(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_CLAUDE") != "1" {
		t.Skip("set STUDIOFORGE_REAL_CLAUDE=1 for an authenticated, billable smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	provider := New("")
	diagnostics := provider.Diagnose(ctx)
	if !diagnostics.Available || !diagnostics.Authenticated {
		t.Skipf("Claude is not ready: %s", diagnostics.Message)
	}
	handle, err := provider.Start(ctx, providers.RunRequest{RunID: "286fbd5e-6cde-4bac-a57d-31045c34e571", ProjectID: "smoke", AgentID: "smoke", WorkingDirectory: t.TempDir(), Prompt: "Reply with exactly: StudioForge Claude adapter smoke passed", MaxTurns: 1, MaxBudget: .10, PermissionProfile: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	events := 0
	lastType := ""
	lastRawType := ""
	lastError := ""
	summaries := []string{}
	for event := range handle.Events() {
		events++
		lastType, lastRawType, lastError = event.Type, event.RawType, event.Error
		payload, _ := event.Payload.(map[string]any)
		summaries = append(summaries, fmt.Sprintf("%s/%s subtype=%v", event.Type, event.RawType, payload["subtype"]))
	}
	result := handle.Wait()
	if result.Err != nil {
		if strings.Contains(strings.ToLower(result.Err.Error()), "organization has disabled") || strings.Contains(strings.ToLower(result.Err.Error()), "oauth_org_not_allowed") {
			t.Skipf("Claude login is present but the organization denied model access: %v", result.Err)
		}
		t.Fatalf("Claude smoke failed after %d events (last=%s/%s error=%q, summaries=%v): %v", events, lastType, lastRawType, lastError, summaries, result.Err)
	}
	if events == 0 || result.SessionID == "" {
		t.Fatalf("events=%d result=%+v", events, result)
	}
}
