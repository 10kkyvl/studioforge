package database

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/10kkyvl/studioforge/internal/models"
)

func TestAppendEventsRedactsSecretsInStoredPayload(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	fakeKey := "sk-ant-FAKEFAKEFAKEFAKEFAKE0000000000000000"
	fakeBearer := "Bearer fake-token-0000000000000000"
	payload := map[string]any{
		"type": "tool_result",
		"content": []any{
			map[string]any{
				"type": "text",
				"text": "Here is your key: " + fakeKey + "\nAuthorization: " + fakeBearer + "\nrequest ok",
			},
		},
	}
	appended, err := store.AppendEvents(ctx, []models.RunEvent{{
		ProjectID: "demo-obby",
		RunID:     "demo-obby-history",
		AgentID:   "demo-obby-orch",
		Type:      "message",
		RawType:   "assistant",
		Payload:   payload,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(appended) != 1 {
		t.Fatalf("appended = %d events, want 1", len(appended))
	}

	events, err := store.EventsAfter(ctx, appended[0].ID-1, "demo-obby", "demo-obby-history", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("read back = %d events, want 1", len(events))
	}

	readBack, err := json.Marshal(events[0].Payload)
	if err != nil {
		t.Fatalf("read-back payload is not valid JSON: %v", err)
	}
	body := string(readBack)
	if strings.Contains(body, fakeKey) {
		t.Fatalf("stored payload still contains the fake API key: %s", body)
	}
	if strings.Contains(body, fakeBearer) {
		t.Fatalf("stored payload still contains the fake bearer token: %s", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("stored payload was not redacted at all: %s", body)
	}
	if !strings.Contains(body, "request ok") {
		t.Fatalf("stored payload lost unrelated text it should have kept: %s", body)
	}
}

func TestAppendEventsRedactsSecretsByKeyName(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	fakeAWSKey := "AKIAFAKEFAKEFAKEFAKE"
	fakePassword := "fake-password-value-123"
	fakeSecretOne := "fake-secret-one"
	fakeSecretTwo := "fake-secret-two"
	payload := map[string]any{
		"input":   map[string]any{"api_key": fakeAWSKey},
		"content": map[string]any{"password": fakePassword},
		"secrets": []any{fakeSecretOne, fakeSecretTwo},
		"message": "ordinary text without a sensitive key",
	}
	appended, err := store.AppendEvents(ctx, []models.RunEvent{{
		ProjectID: "demo-obby",
		RunID:     "demo-obby-history",
		AgentID:   "demo-obby-orch",
		Type:      "tool",
		RawType:   "user",
		Payload:   payload,
	}})
	if err != nil {
		t.Fatal(err)
	}

	events, err := store.EventsAfter(ctx, appended[0].ID-1, "demo-obby", "demo-obby-history", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("read back = %d events, want 1", len(events))
	}

	readBack, err := json.Marshal(events[0].Payload)
	if err != nil {
		t.Fatalf("read-back payload is not valid JSON: %v", err)
	}
	body := string(readBack)
	for _, secret := range []string{fakeAWSKey, fakePassword, fakeSecretOne, fakeSecretTwo} {
		if strings.Contains(body, secret) {
			t.Fatalf("stored payload still contains a key-named secret %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "ordinary text without a sensitive key") {
		t.Fatalf("stored payload lost unrelated text it should have kept: %s", body)
	}
}

func TestAppendEventsPayloadWithoutSecretsRoundTripsUnchanged(t *testing.T) {
	_, store := testDB(t)
	ctx := context.Background()
	if err := store.SeedDemo(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"type":   "text",
		"text":   "no secrets here, just an ordinary status update",
		"count":  float64(3),
		"nested": map[string]any{"ok": true, "note": "still fine"},
		"tags":   []any{"a", "b", "c"},
	}
	appended, err := store.AppendEvents(ctx, []models.RunEvent{{
		ProjectID: "demo-obby",
		RunID:     "demo-obby-history",
		AgentID:   "demo-obby-orch",
		Type:      "message",
		RawType:   "assistant",
		Payload:   payload,
	}})
	if err != nil {
		t.Fatal(err)
	}

	events, err := store.EventsAfter(ctx, appended[0].ID-1, "demo-obby", "demo-obby-history", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("read back = %d events, want 1", len(events))
	}

	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(events[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("payload without secrets was mangled:\n got  = %s\n want = %s", got, want)
	}
}
