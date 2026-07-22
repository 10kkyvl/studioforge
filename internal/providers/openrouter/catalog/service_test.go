package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fakeCacheStore struct {
	mu        sync.Mutex
	payload   []byte
	fetchedAt time.Time
}

func (f *fakeCacheStore) GetModelCache(ctx context.Context) ([]byte, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.payload) == 0 {
		return nil, time.Time{}, nil
	}
	out := make([]byte, len(f.payload))
	copy(out, f.payload)
	return out, f.fetchedAt, nil
}

func (f *fakeCacheStore) SetModelCache(ctx context.Context, payload []byte, fetchedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payload = append([]byte(nil), payload...)
	f.fetchedAt = fetchedAt
	return nil
}

func (f *fakeCacheStore) snapshot() ([]byte, time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.payload...), f.fetchedAt
}

type toggleServer struct {
	mu   sync.Mutex
	hits int
	fail bool
	body string
}

func newToggleServer(t *testing.T, body string) (*httptest.Server, *toggleServer) {
	t.Helper()
	ts := &toggleServer{body: body}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.hits++
		fail := ts.fail
		respBody := ts.body
		ts.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(server.Close)
	return server, ts
}

func (ts *toggleServer) setFail(v bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.fail = v
}

func (ts *toggleServer) hitCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.hits
}

const oneToolModelResponse = `{"data":[{"id":"vendor/x","supported_parameters":["tools"],"architecture":{"output_modalities":["text"]},"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}`

func TestServiceModelsLiveFetchSuccessCachesResult(t *testing.T) {
	server, ts := newToggleServer(t, oneToolModelResponse)
	cache := &fakeCacheStore{}
	svc := NewService(Config{HTTPClient: server.Client(), BaseURL: server.URL, Cache: cache, TTL: time.Hour})

	models, source, err := svc.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if source != SourceLive {
		t.Fatalf("source=%q, want live", source)
	}
	if len(models) != 1 || models[0].ID != "vendor/x" {
		t.Fatalf("models=%+v", models)
	}
	if ts.hitCount() != 1 {
		t.Fatalf("hits=%d, want 1", ts.hitCount())
	}
	payload, _ := cache.snapshot()
	if len(payload) == 0 {
		t.Fatal("expected live fetch to persist payload to cache")
	}
}

func TestServiceModelsFetchFailureFallsBackToPopulatedCache(t *testing.T) {
	server, ts := newToggleServer(t, oneToolModelResponse)
	ts.setFail(true)
	cache := &fakeCacheStore{}
	cachedAt := time.Now().Add(-24 * time.Hour)
	if err := cache.SetModelCache(context.Background(), []byte(`[{"id":"vendor/cached","supported_parameters":["tools"],"architecture":{"output_modalities":["text"]},"pricing":{"prompt":"0","completion":"0"}}]`), cachedAt); err != nil {
		t.Fatal(err)
	}
	svc := NewService(Config{HTTPClient: server.Client(), BaseURL: server.URL, Cache: cache, TTL: time.Hour})

	models, source, err := svc.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if source != SourceCache {
		t.Fatalf("source=%q, want cache", source)
	}
	if len(models) != 1 || models[0].ID != "vendor/cached" {
		t.Fatalf("models=%+v", models)
	}
}

func TestServiceModelsFetchFailureEmptyCacheFallsBackToFallbackModels(t *testing.T) {
	server, ts := newToggleServer(t, oneToolModelResponse)
	ts.setFail(true)
	cache := &fakeCacheStore{}
	svc := NewService(Config{HTTPClient: server.Client(), BaseURL: server.URL, Cache: cache, TTL: time.Hour})

	models, source, err := svc.Models(context.Background())
	if err != nil {
		t.Fatalf("expected no hard error on network failure with empty cache, got %v", err)
	}
	if source != SourceFallback {
		t.Fatalf("source=%q, want fallback", source)
	}
	if len(models) != 35 {
		t.Fatalf("models=%d, want 35 (bundled fallback snapshot)", len(models))
	}
}

func TestServiceModelsTTLMemoizationAvoidsRefetch(t *testing.T) {
	server, ts := newToggleServer(t, oneToolModelResponse)
	cache := &fakeCacheStore{}
	now := time.Now()
	svc := NewService(Config{HTTPClient: server.Client(), BaseURL: server.URL, Cache: cache, TTL: time.Hour, Now: func() time.Time { return now }})

	if _, _, err := svc.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ts.hitCount() != 1 {
		t.Fatalf("hits=%d after first call, want 1", ts.hitCount())
	}

	models, source, err := svc.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ts.hitCount() != 1 {
		t.Fatalf("hits=%d after second call within TTL, want still 1 (memoized)", ts.hitCount())
	}
	if source != SourceLive {
		t.Fatalf("source=%q, want live (from memo)", source)
	}
	if len(models) != 1 {
		t.Fatalf("models=%+v", models)
	}

	now = now.Add(2 * time.Hour)
	if _, _, err := svc.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ts.hitCount() != 2 {
		t.Fatalf("hits=%d after TTL expiry, want 2 (refetched)", ts.hitCount())
	}
}

func TestServiceModelsEmptyFetchIsMemoizedWithinTTL(t *testing.T) {
	server, ts := newToggleServer(t, `{"data":[]}`)
	cache := &fakeCacheStore{}
	now := time.Now()
	svc := NewService(Config{HTTPClient: server.Client(), BaseURL: server.URL, Cache: cache, TTL: time.Hour, Now: func() time.Time { return now }})

	models, source, err := svc.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if source != SourceLive {
		t.Fatalf("source=%q, want live", source)
	}
	if len(models) != 0 {
		t.Fatalf("models=%+v, want empty", models)
	}
	if ts.hitCount() != 1 {
		t.Fatalf("hits=%d after first call, want 1", ts.hitCount())
	}

	models, source, err = svc.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ts.hitCount() != 1 {
		t.Fatalf("hits=%d after second call within TTL, want still 1 (empty fetch memoized)", ts.hitCount())
	}
	if source != SourceLive {
		t.Fatalf("source=%q, want live (from memo)", source)
	}
	if len(models) != 0 {
		t.Fatalf("models=%+v, want empty", models)
	}
}

func TestServiceModelsCallerCancellationDoesNotPoisonMemo(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oneToolModelResponse))
	}))
	defer server.Close()

	cache := &fakeCacheStore{}
	svc := NewService(Config{HTTPClient: server.Client(), BaseURL: server.URL, Cache: cache, TTL: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		models []Model
		source Source
		err    error
	}
	done := make(chan result, 1)
	go func() {
		models, source, err := svc.Models(ctx)
		done <- result{models, source, err}
	}()

	<-started
	cancel()
	time.Sleep(50 * time.Millisecond)
	close(release)

	res := <-done
	if res.err != nil {
		t.Fatalf("unexpected error despite caller cancellation: %v", res.err)
	}
	if res.source != SourceLive {
		t.Fatalf("source=%q, want live (fetch must not be aborted by caller cancellation)", res.source)
	}
	if len(res.models) != 1 || res.models[0].ID != "vendor/x" {
		t.Fatalf("models=%+v", res.models)
	}

	models, source, err := svc.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if source != SourceLive {
		t.Fatalf("memoized source=%q, want live (must not be poisoned with fallback data)", source)
	}
	if len(models) != 1 || models[0].ID != "vendor/x" {
		t.Fatalf("memoized models=%+v", models)
	}
}

func TestServiceRefreshFailureKeepsOldCache(t *testing.T) {
	server, ts := newToggleServer(t, oneToolModelResponse)
	cache := &fakeCacheStore{}
	svc := NewService(Config{HTTPClient: server.Client(), BaseURL: server.URL, Cache: cache, TTL: time.Hour})

	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	beforePayload, beforeFetchedAt := cache.snapshot()
	if len(beforePayload) == 0 {
		t.Fatal("expected first refresh to populate cache")
	}

	ts.setFail(true)
	if _, err := svc.Refresh(context.Background()); err == nil {
		t.Fatal("expected Refresh to return an error on fetch failure")
	}

	afterPayload, afterFetchedAt := cache.snapshot()
	if string(afterPayload) != string(beforePayload) || !afterFetchedAt.Equal(beforeFetchedAt) {
		t.Fatalf("failed refresh must not clobber existing cache: before=%s/%v after=%s/%v", beforePayload, beforeFetchedAt, afterPayload, afterFetchedAt)
	}
	if ts.hitCount() != 2 {
		t.Fatalf("hits=%d, want 2 (one success, one failure)", ts.hitCount())
	}
}
