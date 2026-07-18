package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/providers"
	"github.com/10kkyvl/studioforge/internal/providers/claudecode"
	"github.com/10kkyvl/studioforge/internal/roblox/mcp"
)

// TestLiveClaudeReachesStudio drives the assembled chain the product actually
// uses: the real provisioner detects the launcher and writes a config, and the
// real Claude adapter consumes it and reads live Studio state.
//
// The per-package smokes deliberately do not cover this. They exercise the
// Studio client and the Claude adapter in isolation, which is why both passed
// for the entire period during which no agent could reach Studio at all.
func TestLiveClaudeReachesStudio(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_STUDIO") != "1" || os.Getenv("STUDIOFORGE_REAL_CLAUDE") != "1" {
		t.Skip("set STUDIOFORGE_REAL_STUDIO=1 and STUDIOFORGE_REAL_CLAUDE=1 with Roblox Studio open for a billable live smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	provisioner := &mcp.Provisioner{Dir: t.TempDir()}
	grant := provisioner.Provision(ctx, "live-smoke", "read-only", mcp.Target{})
	if grant.ConfigPath == "" {
		t.Skipf("no unambiguous Studio available: %s", grant.Notice)
	}
	defer grant.Release()
	if _, err := os.Stat(grant.ConfigPath); err != nil {
		t.Fatalf("provisioner reported a config it did not write: %v", err)
	}

	provider := claudecode.New("")
	diag := provider.Diagnose(ctx)
	if !diag.Available || !diag.Authenticated {
		t.Skipf("Claude is not ready: %s", diag.Message)
	}

	handle, err := provider.Start(ctx, providers.RunRequest{
		RunID:             "5f3e2d1c-0b9a-4c8d-9e7f-6a5b4c3d2e1f",
		WorkingDirectory:  t.TempDir(),
		PermissionProfile: "read-only",
		MCPConfigPath:     grant.ConfigPath,
		AllowedTools:      grant.AllowedTools,
		Prompt:            "Call the get_studio_state tool and reply with its raw output verbatim. Change nothing.",
	})
	if err != nil {
		t.Fatal(err)
	}

	var transcript strings.Builder
	for event := range handle.Events() {
		if payload, ok := event.Payload.(map[string]any); ok {
			transcript.WriteString(describe(payload))
		}
	}
	result := handle.Wait()
	if result.Err != nil {
		t.Fatalf("live run failed: %v\ntranscript:\n%s", result.Err, transcript.String())
	}
	// Studio reports its mode as Edit or Play; either proves the agent reached it.
	body := transcript.String()
	if !strings.Contains(body, "Studio Mode") && !strings.Contains(body, "DataModel") {
		t.Fatalf("Claude never reported Studio state, so the MCP handoff did not reach Studio.\ntranscript:\n%s", body)
	}
	t.Logf("live chain verified: config=%s tools=%d", filepath.Base(grant.ConfigPath), len(grant.AllowedTools))
}

// TestLiveClaudePlanModeReachesStudio proves the installed Claude CLI accepts
// the PLAN value for --permission-mode and that a plan-mode run still traverses
// the whole chain. Plan mode may reason about a tool rather than calling it, so
// the assertion is that the run completes without a capability mismatch, not
// that Studio state comes back.
func TestLiveClaudePlanModeReachesStudio(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_STUDIO") != "1" || os.Getenv("STUDIOFORGE_REAL_CLAUDE") != "1" {
		t.Skip("set STUDIOFORGE_REAL_STUDIO=1 and STUDIOFORGE_REAL_CLAUDE=1 with Roblox Studio open for a billable live smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	provisioner := &mcp.Provisioner{Dir: t.TempDir()}
	grant := provisioner.Provision(ctx, "live-plan-smoke", "read-only", mcp.Target{})
	if grant.ConfigPath == "" {
		t.Skipf("no unambiguous Studio available: %s", grant.Notice)
	}
	defer grant.Release()

	provider := claudecode.New("")
	diag := provider.Diagnose(ctx)
	if !diag.Available || !diag.Authenticated {
		t.Skipf("Claude is not ready: %s", diag.Message)
	}

	handle, err := provider.Start(ctx, providers.RunRequest{
		RunID:             "7a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d",
		WorkingDirectory:  t.TempDir(),
		PermissionProfile: "read-only",
		Mode:              "plan",
		MCPConfigPath:     grant.ConfigPath,
		AllowedTools:      grant.AllowedTools,
		Prompt:            "Briefly, how would you inspect the current Studio state? Do not change anything.",
	})
	if err != nil {
		t.Fatal(err)
	}
	var transcript strings.Builder
	for event := range handle.Events() {
		if payload, ok := event.Payload.(map[string]any); ok {
			transcript.WriteString(describe(payload))
		}
	}
	result := handle.Wait()
	if result.Err != nil {
		if strings.Contains(result.Err.Error(), "capability mismatch") {
			t.Fatalf("the installed Claude CLI rejected --permission-mode plan: %v", result.Err)
		}
		t.Fatalf("plan-mode live run failed: %v\ntranscript:\n%s", result.Err, transcript.String())
	}
	t.Logf("plan-mode chain verified: run completed without a capability mismatch (transcript %d bytes)", transcript.Len())
}

// TestLiveClaudeAcceptsAgents proves the installed Claude CLI accepts the
// --agents JSON StudioForge generates for orchestrator delegation. A malformed
// agents object or an unknown flag would fail the run with a capability
// mismatch; anything else means the delegation wiring is well-formed.
func TestLiveClaudeAcceptsAgents(t *testing.T) {
	if os.Getenv("STUDIOFORGE_REAL_CLAUDE") != "1" {
		t.Skip("set STUDIOFORGE_REAL_CLAUDE=1 for a billable live smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	provider := claudecode.New("")
	diag := provider.Diagnose(ctx)
	if !diag.Available || !diag.Authenticated {
		t.Skipf("Claude is not ready: %s", diag.Message)
	}

	handle, err := provider.Start(ctx, providers.RunRequest{
		RunID:            "3c2b1a09-8f7e-6d5c-4b3a-2e1d0c9b8a76",
		WorkingDirectory: t.TempDir(),
		Subagents: []providers.Subagent{
			{Name: "Gameplay Engineer", Description: "Builds gameplay", Prompt: "You build Roblox gameplay features."},
		},
		Prompt: "List the custom subagents available to you by name, then stop.",
	})
	if err != nil {
		t.Fatal(err)
	}
	for range handle.Events() {
	}
	result := handle.Wait()
	if result.Err != nil && strings.Contains(result.Err.Error(), "capability mismatch") {
		t.Fatalf("the installed Claude CLI rejected the generated --agents JSON: %v", result.Err)
	}
	if result.Err != nil {
		t.Fatalf("agents live run failed: %v", result.Err)
	}
	t.Logf("--agents accepted by the live CLI without a capability mismatch")
}

func describe(payload map[string]any) string {
	var out strings.Builder
	for _, key := range []string{"result", "text", "content"} {
		if value, ok := payload[key]; ok {
			out.WriteString(flatten(value))
		}
	}
	if message, ok := payload["message"].(map[string]any); ok {
		out.WriteString(flatten(message["content"]))
	}
	return out.String()
}

func flatten(value any) string {
	switch v := value.(type) {
	case string:
		return v + "\n"
	case []any:
		var out strings.Builder
		for _, item := range v {
			out.WriteString(flatten(item))
		}
		return out.String()
	case map[string]any:
		var out strings.Builder
		for _, key := range []string{"text", "content"} {
			if inner, ok := v[key]; ok {
				out.WriteString(flatten(inner))
			}
		}
		return out.String()
	}
	return ""
}
