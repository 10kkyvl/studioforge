package orclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

type wireFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type wireToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function wireFunctionDelta `json:"function,omitempty"`
}

type wireDelta struct {
	Role      string              `json:"role,omitempty"`
	Content   string              `json:"content,omitempty"`
	Reasoning string              `json:"reasoning,omitempty"`
	ToolCalls []wireToolCallDelta `json:"tool_calls,omitempty"`
}

type wireChoice struct {
	Index        int       `json:"index"`
	Delta        wireDelta `json:"delta"`
	FinishReason string    `json:"finish_reason,omitempty"`
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type wireChunk struct {
	ID      string       `json:"id,omitempty"`
	Model   string       `json:"model,omitempty"`
	Choices []wireChoice `json:"choices,omitempty"`
	Usage   *Usage       `json:"usage,omitempty"`
	Error   *wireError   `json:"error,omitempty"`
}

func basicReq(model string) ChatRequest {
	return ChatRequest{Model: model, Messages: []Message{{Role: "user", Content: "hi"}}}
}

func mustSSELine(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return fmt.Sprintf("data: %s\n\n", b)
}

func writeSSE(t *testing.T, w io.Writer, flusher http.Flusher, v any) {
	t.Helper()
	fmt.Fprint(w, mustSSELine(t, v))
	flusher.Flush()
}

func writeDone(w io.Writer, flusher http.Flusher) {
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func TestStreamChat_Success(t *testing.T) {
	type captured struct {
		auth    string
		referer string
		title   string
		body    map[string]any
	}
	capCh := make(chan captured, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		var bodyMap map[string]any
		_ = json.Unmarshal(bodyBytes, &bodyMap)
		capCh <- captured{
			auth:    r.Header.Get("Authorization"),
			referer: r.Header.Get("HTTP-Referer"),
			title:   r.Header.Get("X-Title"),
			body:    bodyMap,
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		writeSSE(t, w, flusher, wireChunk{ID: "1", Model: "test-model", Choices: []wireChoice{{Delta: wireDelta{Content: "Hello"}}}})
		writeSSE(t, w, flusher, wireChunk{ID: "1", Model: "test-model", Choices: []wireChoice{{Delta: wireDelta{Content: " world"}}}})
		writeSSE(t, w, flusher, wireChunk{
			ID:      "1",
			Model:   "test-model",
			Choices: []wireChoice{{Delta: wireDelta{}, FinishReason: "stop"}},
			Usage:   &Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, Cost: 0.002},
		})
		writeDone(w, flusher)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	var deltas []Delta
	sink := func(d Delta) { deltas = append(deltas, d) }

	res, err := c.StreamChat(context.Background(), basicReq("test-model"), sink)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if res.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", res.Content, "Hello world")
	}
	if res.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", res.FinishReason)
	}
	if res.Model != "test-model" {
		t.Errorf("Model = %q, want test-model", res.Model)
	}
	if res.Usage.Cost != 0.002 || res.Usage.TotalTokens != 15 {
		t.Errorf("Usage = %+v, unexpected", res.Usage)
	}
	if len(deltas) != 2 {
		t.Errorf("sink called %d times, want 2", len(deltas))
	}

	cap := <-capCh
	if cap.auth != "Bearer test-key" {
		t.Errorf("Authorization = %q", cap.auth)
	}
	if cap.referer == "" {
		t.Errorf("HTTP-Referer missing")
	}
	if cap.title == "" {
		t.Errorf("X-Title missing")
	}
	if cap.body["stream"] != true {
		t.Errorf("body stream = %v, want true", cap.body["stream"])
	}
	so, ok := cap.body["stream_options"].(map[string]any)
	if !ok || so["include_usage"] != true {
		t.Errorf("body stream_options = %v, want include_usage true", cap.body["stream_options"])
	}
}

func TestStreamChat_KeepaliveComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, ": OPENROUTER PROCESSING\n\n")
		flusher.Flush()
		writeSSE(t, w, flusher, wireChunk{Model: "m", Choices: []wireChoice{{Delta: wireDelta{Content: "Hello"}}}})
		fmt.Fprint(w, ": OPENROUTER PROCESSING\n\n")
		flusher.Flush()
		writeSSE(t, w, flusher, wireChunk{Model: "m", Choices: []wireChoice{{FinishReason: "stop"}}})
		writeDone(w, flusher)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := c.StreamChat(context.Background(), basicReq("m"), nil)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if res.Content != "Hello" {
		t.Errorf("Content = %q, want Hello", res.Content)
	}
}

func TestStreamChat_FragmentedLines(t *testing.T) {
	full := mustSSELine(t, wireChunk{
		Model:   "m",
		Choices: []wireChoice{{Delta: wireDelta{Content: "Hello world"}, FinishReason: "stop"}},
		Usage:   &Usage{TotalTokens: 3},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		mid := len(full) / 2
		parts := []string{full[:5], full[5:mid], full[mid:]}
		for _, p := range parts {
			fmt.Fprint(w, p)
			flusher.Flush()
			time.Sleep(2 * time.Millisecond)
		}
		writeDone(w, flusher)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := c.StreamChat(context.Background(), basicReq("m"), nil)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if res.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", res.Content, "Hello world")
	}
	if res.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", res.FinishReason)
	}
}

func TestStreamChat_ToolCallSingle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
			{Index: 0, ID: "call_1", Type: "function", Function: wireFunctionDelta{Name: "read_file"}},
		}}}}})
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
			{Index: 0, Function: wireFunctionDelta{Arguments: `{"path":`}},
		}}}}})
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
			{Index: 0, Function: wireFunctionDelta{Arguments: `"a.lua"}`}},
		}}}}})
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{FinishReason: "tool_calls"}}})
		writeDone(w, flusher)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := c.StreamChat(context.Background(), basicReq("m"), nil)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(res.ToolCalls))
	}
	tc := res.ToolCalls[0]
	if tc.ID != "call_1" || tc.Type != "function" || tc.Function.Name != "read_file" {
		t.Errorf("ToolCall = %+v, unexpected", tc)
	}
	if tc.Function.Arguments != `{"path":"a.lua"}` {
		t.Errorf("Arguments = %q, want %q", tc.Function.Arguments, `{"path":"a.lua"}`)
	}
	if res.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", res.FinishReason)
	}
}

func TestStreamChat_ToolCallMultiple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
			{Index: 0, ID: "call_0", Type: "function", Function: wireFunctionDelta{Name: "read_file", Arguments: `{"path":"a"}`}},
		}}}}})
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{Delta: wireDelta{ToolCalls: []wireToolCallDelta{
			{Index: 1, ID: "call_1", Type: "function", Function: wireFunctionDelta{Name: "write_file", Arguments: `{"path":"b"}`}},
		}}}}})
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{FinishReason: "tool_calls"}}})
		writeDone(w, flusher)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := c.StreamChat(context.Background(), basicReq("m"), nil)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if len(res.ToolCalls) != 2 {
		t.Fatalf("ToolCalls = %d, want 2", len(res.ToolCalls))
	}
	if res.ToolCalls[0].ID != "call_0" || res.ToolCalls[1].ID != "call_1" {
		t.Errorf("ToolCalls not in index order: %+v", res.ToolCalls)
	}
}

func TestStreamChat_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {not json\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := c.StreamChat(context.Background(), basicReq("m"), nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v (%T)", err, err)
	}
	if apiErr.Kind != KindMalformedResponse {
		t.Errorf("Kind = %v, want %v", apiErr.Kind, KindMalformedResponse)
	}
}

func TestStreamChat_PreStreamErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		maxRetries int
		wantKind   Kind
	}{
		{"unauthorized", 401, `{"error":{"code":401,"message":"invalid api key"}}`, 3, KindAuthentication},
		{"insufficient credits", 402, `{"error":{"code":402,"message":"insufficient credits"}}`, 3, KindInsufficientCredits},
		{"rate limited no retries", 429, `{"error":{"code":429,"message":"rate limited"}}`, 0, KindRateLimited},
		{"model unavailable", 502, `{"error":{"code":502,"message":"bad gateway"}}`, 3, KindModelUnavailable},
		{"context overflow", 400, `{"error":{"code":400,"message":"This model's maximum context length is 4096 tokens"}}`, 3, KindContextOverflow},
		{"unsupported parameter", 400, `{"error":{"code":400,"message":"Unsupported parameter: response_format is not supported"}}`, 3, KindUnsupportedParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: tc.maxRetries})
			_, err := c.StreamChat(context.Background(), basicReq("m"), nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *APIError, got %v (%T)", err, err)
			}
			if apiErr.Kind != tc.wantKind {
				t.Errorf("Kind = %v, want %v", apiErr.Kind, tc.wantKind)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
		})
	}
}

func TestStreamChat_MidStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "partial"}}}})
		writeSSE(t, w, flusher, wireChunk{
			Choices: []wireChoice{{FinishReason: "error"}},
			Error:   &wireError{Code: 502, Message: "upstream"},
		})
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	var got []Delta
	sink := func(d Delta) { got = append(got, d) }
	_, err := c.StreamChat(context.Background(), basicReq("m"), sink)

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v (%T)", err, err)
	}
	if apiErr.Kind != KindModelUnavailable {
		t.Errorf("Kind = %v, want %v", apiErr.Kind, KindModelUnavailable)
	}
	if len(got) == 0 || got[0].Text != "partial" {
		t.Errorf("partial content not delivered to sink before error: %+v", got)
	}
}

func TestStreamChat_Cancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "partial"}}}})
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := c.StreamChat(ctx, basicReq("m"), nil)
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("StreamChat did not return promptly on cancellation: %v", elapsed)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v (%T)", err, err)
	}
	if apiErr.Kind != KindCancelled {
		t.Errorf("Kind = %v, want %v", apiErr.Kind, KindCancelled)
	}
}

func TestStreamChat_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.StreamChat(ctx, basicReq("m"), nil)
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("StreamChat did not return promptly on deadline: %v", elapsed)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v (%T)", err, err)
	}
	if apiErr.Kind != KindCancelled {
		t.Errorf("Kind = %v, want %v", apiErr.Kind, KindCancelled)
	}
}

func TestStreamChat_RetryAfterRateLimit(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"message":"rate limited"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		writeSSE(t, w, flusher, wireChunk{Model: "m", Choices: []wireChoice{{Delta: wireDelta{Content: "ok"}, FinishReason: "stop"}}})
		writeDone(w, flusher)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: 2})
	res, err := c.StreamChat(context.Background(), basicReq("m"), nil)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if res.Content != "ok" {
		t.Errorf("Content = %q, want ok", res.Content)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("handler hit %d times, want 2", got)
	}
}

func TestStreamChat_RetryAfterIsFloor(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"message":"rate limited"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		writeSSE(t, w, flusher, wireChunk{Model: "m", Choices: []wireChoice{{Delta: wireDelta{Content: "ok"}, FinishReason: "stop"}}})
		writeDone(w, flusher)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: 2})
	start := time.Now()
	res, err := c.StreamChat(context.Background(), basicReq("m"), nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if res.Content != "ok" {
		t.Errorf("Content = %q, want ok", res.Content)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, want >= ~1s (Retry-After should be a floor on backoff)", elapsed)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("handler hit %d times, want 2", got)
	}
}

func TestStreamChat_NoRetryOn400(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad request"}}`))
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: 3})
	_, err := c.StreamChat(context.Background(), basicReq("m"), nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("handler hit %d times, want 1", got)
	}
}

type closeTrackingBody struct {
	io.ReadCloser
	closed *atomic.Bool
}

func (b *closeTrackingBody) Close() error {
	b.closed.Store(true)
	return b.ReadCloser.Close()
}

type trackingTransport struct {
	base   http.RoundTripper
	closed *atomic.Bool
}

func (t *trackingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return resp, err
	}
	resp.Body = &closeTrackingBody{ReadCloser: resp.Body, closed: t.closed}
	return resp, nil
}

func TestStreamChat_BodyClosed(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "hi"}, FinishReason: "stop"}}})
			writeDone(w, flusher)
		}))
		defer srv.Close()

		var closed atomic.Bool
		httpClient := &http.Client{Transport: &trackingTransport{base: http.DefaultTransport, closed: &closed}}
		c := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: httpClient})
		_, err := c.StreamChat(context.Background(), basicReq("m"), nil)
		if err != nil {
			t.Fatalf("StreamChat: %v", err)
		}
		if !closed.Load() {
			t.Errorf("response body was not closed")
		}
	})

	t.Run("error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":401,"message":"bad key"}}`))
		}))
		defer srv.Close()

		var closed atomic.Bool
		httpClient := &http.Client{Transport: &trackingTransport{base: http.DefaultTransport, closed: &closed}}
		c := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: httpClient})
		_, err := c.StreamChat(context.Background(), basicReq("m"), nil)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !closed.Load() {
			t.Errorf("response body was not closed")
		}
	})
}

func TestStreamChat_NoGoroutineLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		writeSSE(t, w, flusher, wireChunk{Choices: []wireChoice{{Delta: wireDelta{Content: "hi"}, FinishReason: "stop"}}})
		writeDone(w, flusher)
	}))
	defer srv.Close()

	httpClient := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	c := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: httpClient})

	for i := 0; i < 5; i++ {
		if _, err := c.StreamChat(context.Background(), basicReq("m"), nil); err != nil {
			t.Fatalf("StreamChat warmup: %v", err)
		}
	}
	runtime.GC()
	before := runtime.NumGoroutine()

	for i := 0; i < 50; i++ {
		if _, err := c.StreamChat(context.Background(), basicReq("m"), nil); err != nil {
			t.Fatalf("StreamChat: %v", err)
		}
	}
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+5 {
		t.Errorf("possible goroutine leak: before=%d after=%d", before, after)
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"zero seconds", "0", 0},
		{"two seconds", "2", 2 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("Retry-After", tc.value)
			got := parseRetryAfter(h)
			if got != tc.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}

	t.Run("http-date", func(t *testing.T) {
		future := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
		h := http.Header{}
		h.Set("Retry-After", future)
		got := parseRetryAfter(h)
		if got <= 0 || got > 6*time.Second {
			t.Errorf("parseRetryAfter(%q) = %v, want ~5s", future, got)
		}
	})

	t.Run("missing", func(t *testing.T) {
		h := http.Header{}
		if got := parseRetryAfter(h); got != 0 {
			t.Errorf("parseRetryAfter(missing) = %v, want 0", got)
		}
	})
}

func TestClassifyBadRequest(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    Kind
	}{
		{"unsupported context param", "Unsupported parameter: 'context-1m' is not supported", KindUnsupportedParams},
		{"context length exceeded", "maximum context length exceeded", KindContextOverflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyBadRequest(tc.message)
			if got != tc.want {
				t.Errorf("classifyBadRequest(%q) = %v, want %v", tc.message, got, tc.want)
			}
		})
	}
}
