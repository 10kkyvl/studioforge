package security

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	base := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(NewRedactingHandler(base))
}

func TestRedactingHandlerCategories(t *testing.T) {
	cases := []struct {
		name   string
		log    func(l *slog.Logger)
		secret string
	}{
		{"openrouter_key_in_message", func(l *slog.Logger) {
			l.Info("provider call failed with key sk-or-v1-abcdefghijklmnopqrstuvwxyz1234567890")
		}, "sk-or-v1-abcdefghijklmnopqrstuvwxyz1234567890"},
		{"nvidia_key_in_attr", func(l *slog.Logger) {
			l.Info("provider call", "detail", "nvapi-abcdefghijklmnopqrstuvwxyz_123456")
		}, "nvapi-abcdefghijklmnopqrstuvwxyz_123456"},
		{"authorization_header", func(l *slog.Logger) {
			l.Info("request", "header", "Authorization: Bearer sekret-token-value-0000")
		}, "sekret-token-value-0000"},
		{"bootstrap_token_url_attr", func(l *slog.Logger) {
			l.Warn("browser did not open automatically", "url", "http://127.0.0.1:1234/#bootstrap=q1w2e3r4t5y6u7i8o9p0asdfghjklzxcvbnmQW1")
		}, "q1w2e3r4t5y6u7i8o9p0asdfghjklzxcvbnmQW1"},
		{"cookie_attr", func(l *slog.Logger) {
			l.Info("request", "cookie", "Cookie: sessionid=abcdef1234567890")
		}, "abcdef1234567890"},
		{"sensitive_key_name_attr", func(l *slog.Logger) {
			l.Info("credential loaded", "api_key", "raw-secret-value-here")
		}, "raw-secret-value-here"},
		{"error_value_redacted", func(l *slog.Logger) {
			l.Error("request failed", "error", errors.New("upstream said Authorization: Bearer sekret-token-value-0000"))
		}, "sekret-token-value-0000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := newTestLogger(&buf)
			c.log(logger)
			out := buf.String()
			if strings.Contains(out, c.secret) {
				t.Fatalf("secret %q leaked into handler output: %s", c.secret, out)
			}
			if !strings.Contains(out, "[REDACTED]") {
				t.Fatalf("expected [REDACTED] placeholder in output: %s", out)
			}
		})
	}
}

func TestRedactingHandlerPreservesNonSecretAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	logger.Info("run started", "run_id", "abc-123", "project_id", "proj-1", "status", "running")
	out := buf.String()
	for _, want := range []string{"run started", "abc-123", "proj-1", "running"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q preserved in output: %s", want, out)
		}
	}
}

func TestRedactingHandlerRedactsNestedGroups(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	logger.Info("provider config", slog.Group("credentials",
		slog.String("provider", "openrouter"),
		slog.String("api_key", "sk-or-v1-abcdefghijklmnopqrstuvwxyz1234567890"),
	))
	out := buf.String()
	if strings.Contains(out, "sk-or-v1-abcdefghijklmnopqrstuvwxyz1234567890") {
		t.Fatalf("secret leaked through nested group: %s", out)
	}
	if !strings.Contains(out, "openrouter") {
		t.Fatalf("expected non-secret nested attr preserved: %s", out)
	}
}

func TestRedactingHandlerRedactsWithAttrsChain(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf).With("token", "chained-secret-value-1234")
	logger.Info("ready")
	out := buf.String()
	if strings.Contains(out, "chained-secret-value-1234") {
		t.Fatalf("secret leaked through With(...): %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] placeholder in output: %s", out)
	}
}

func TestRedactingHandlerMessageTextRedacted(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	logger.Info("failed request Authorization: Bearer sekret-token-value-0000 to upstream")
	out := buf.String()
	if strings.Contains(out, "sekret-token-value-0000") {
		t.Fatalf("secret leaked through message text: %s", out)
	}
	if !strings.Contains(out, "failed request") || !strings.Contains(out, "to upstream") {
		t.Fatalf("expected non-secret message text preserved: %s", out)
	}
}

func TestRedactingHandlerEnabledDelegates(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := NewRedactingHandler(base)
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("expected debug level to be disabled when base handler is configured for warn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("expected error level to be enabled")
	}
}
