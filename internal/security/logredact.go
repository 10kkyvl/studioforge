package security

import (
	"context"
	"fmt"
	"log/slog"
)

type RedactingHandler struct {
	next slog.Handler
}

func NewRedactingHandler(next slog.Handler) *RedactingHandler {
	return &RedactingHandler{next: next}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, record slog.Record) error {
	out := slog.NewRecord(record.Time, record.Level, Redact(record.Message), record.PC)
	record.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &RedactingHandler{next: h.next.WithAttrs(redacted)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	a.Value = redactSlogValue(a.Key, a.Value)
	return a
}

func redactSlogValue(key string, v slog.Value) slog.Value {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		if IsSensitiveKey(key) {
			return slog.StringValue("[REDACTED]")
		}
		return slog.StringValue(Redact(v.String()))
	case slog.KindGroup:
		attrs := v.Group()
		out := make([]slog.Attr, len(attrs))
		for i, ga := range attrs {
			out[i] = redactAttr(ga)
		}
		return slog.GroupValue(out...)
	case slog.KindAny:
		any := v.Any()
		if IsSensitiveKey(key) {
			return slog.StringValue("[REDACTED]")
		}
		if err, ok := any.(error); ok {
			return slog.StringValue(Redact(err.Error()))
		}
		return slog.StringValue(Redact(fmt.Sprint(any)))
	default:
		return v
	}
}
