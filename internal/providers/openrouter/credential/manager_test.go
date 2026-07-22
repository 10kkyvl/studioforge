package credential

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/10kkyvl/studioforge/internal/platform"
)

type fakeSecretStore struct {
	mu        sync.Mutex
	values    map[string][]byte
	setErr    error
	deleteErr error
}

func (f *fakeSecretStore) Set(_ context.Context, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	if f.values == nil {
		f.values = map[string][]byte{}
	}
	f.values[key] = append([]byte(nil), value...)
	return nil
}

func (f *fakeSecretStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.values[key]
	if !ok {
		return nil, platform.ErrSecretNotFound
	}
	return append([]byte(nil), v...), nil
}

func (f *fakeSecretStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.values, key)
	return nil
}

type fakeStateStore struct {
	mu      sync.Mutex
	state   string
	written []string
}

func (f *fakeStateStore) Get(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *fakeStateStore) Set(_ context.Context, s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
	f.written = append(f.written, s)
	return nil
}

func newTestManager(t *testing.T, secure platform.SecretStore) (*Manager, *fakeStateStore) {
	return newTestManagerWithBaseURL(t, secure, "")
}

func newTestManagerWithBaseURL(t *testing.T, secure platform.SecretStore, baseURL string) (*Manager, *fakeStateStore) {
	t.Helper()
	fs := &fakeStateStore{}
	m := NewManager(Config{
		Service:  "StudioForge-Test",
		Account:  "openrouter",
		EnvVar:   "STUDIOFORGE_TEST_OPENROUTER_KEY",
		Secure:   secure,
		GetState: fs.Get,
		SetState: fs.Set,
		BaseURL:  baseURL,
	})
	return m, fs
}

func TestSaveKeyRoundTripsAndRemoveClears(t *testing.T) {
	ctx := context.Background()
	m, fs := newTestManager(t, platform.NewMemorySecretStore())

	status, err := m.Save(ctx, "sk-or-abc123")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if status.State != StateUnverified {
		t.Errorf("state after Save = %q want unverified", status.State)
	}
	if status.Source != SourceKeychain || !status.Secure {
		t.Errorf("status after Save = %+v want keychain/secure", status)
	}
	if got := m.Key(); got != "sk-or-abc123" {
		t.Errorf("Key() = %q", got)
	}
	if fs.state != string(StateUnverified) {
		t.Errorf("fake state store = %q want unverified", fs.state)
	}

	if err := m.Remove(ctx); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := m.Key(); got != "" {
		t.Errorf("Key() after Remove = %q want empty", got)
	}
	st := m.Status(ctx)
	if st.State != StateNotConfigured {
		t.Errorf("Status after Remove = %+v want not_configured", st)
	}
	if fs.state != "" {
		t.Errorf("fake state store after Remove = %q want empty", fs.state)
	}
}

func TestSaveRejectsBlankKey(t *testing.T) {
	m, _ := newTestManager(t, platform.NewMemorySecretStore())
	if _, err := m.Save(context.Background(), ""); err == nil {
		t.Fatal("Save with blank key must error")
	}
}

func TestSaveFallsBackToSessionWhenSecureUnavailable(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t, nil)

	status, err := m.Save(ctx, "sk-or-session-only")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if status.Source != SourceSession || status.Secure {
		t.Errorf("status = %+v want session/insecure", status)
	}
	if got := m.Key(); got != "sk-or-session-only" {
		t.Errorf("Key() = %q", got)
	}
}

func TestSaveFallsBackToSessionWhenSecureSetErrors(t *testing.T) {
	ctx := context.Background()
	fake := &fakeSecretStore{setErr: errors.New("keychain locked")}
	m, _ := newTestManager(t, fake)
	const key = "sk-or-locked-store"

	status, err := m.Save(ctx, key)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if status.Source != SourceSession || status.Secure {
		t.Errorf("status = %+v want session/insecure", status)
	}
	if got := m.Key(); got != key {
		t.Errorf("Key() = %q want %q", got, key)
	}

	b, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal Status: %v", err)
	}
	if strings.Contains(string(b), key) {
		t.Fatalf("Status JSON leaked the key: %s", b)
	}
}

func TestRemoveDoesNotFailHardWhenSecureDeleteErrors(t *testing.T) {
	ctx := context.Background()
	fake := &fakeSecretStore{deleteErr: errors.New("access denied")}
	m, fs := newTestManager(t, fake)

	if _, err := m.Save(ctx, "sk-or-remove-me"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := m.Remove(ctx); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if fs.state != "" {
		t.Errorf("fake state store after Remove = %q want empty", fs.state)
	}
}

func TestKeyPrefersEnvWhenNoSecureOrSession(t *testing.T) {
	t.Setenv("STUDIOFORGE_TEST_OPENROUTER_KEY", "sk-or-from-env")
	m, _ := newTestManager(t, nil)

	if got := m.Key(); got != "sk-or-from-env" {
		t.Errorf("Key() = %q want env value", got)
	}
	status := m.Status(context.Background())
	if status.Source != SourceEnv || status.Secure {
		t.Errorf("status = %+v want env/insecure", status)
	}
}

func TestTestConnectionSuccessAndInvalid(t *testing.T) {
	ctx := context.Background()
	var gotAuth string
	statusCode := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(statusCode)
	}))
	defer srv.Close()

	m, fs := newTestManagerWithBaseURL(t, platform.NewMemorySecretStore(), srv.URL)

	if _, err := m.Save(ctx, "sk-or-secret-xyz"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	status, err := m.TestConnection(ctx)
	if err != nil {
		t.Fatalf("TestConnection (200): %v", err)
	}
	if status.State != StateConfigured {
		t.Errorf("state = %q want configured", status.State)
	}
	if gotAuth != "Bearer sk-or-secret-xyz" {
		t.Errorf("Authorization header = %q", gotAuth)
	}

	statusCode = http.StatusUnauthorized
	status, err = m.TestConnection(ctx)
	if err != nil {
		t.Fatalf("TestConnection (401): %v", err)
	}
	if status.State != StateInvalid {
		t.Errorf("state = %q want invalid", status.State)
	}

	for _, w := range fs.written {
		if strings.Contains(w, "sk-or-secret-xyz") {
			t.Fatalf("state store received the key: %q", w)
		}
	}
}

func TestTestConnectionOtherStatusReturnsError(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	m, _ := newTestManagerWithBaseURL(t, platform.NewMemorySecretStore(), srv.URL)
	if _, err := m.Save(ctx, "sk-or-secret-500"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	status, err := m.TestConnection(ctx)
	if err == nil {
		t.Fatal("expected error for non-200/401 status")
	}
	if status.State != StateUnverified {
		t.Errorf("state = %q want unverified", status.State)
	}
	if strings.Contains(err.Error(), "sk-or-secret-500") {
		t.Fatalf("error contains the key: %v", err)
	}
}

func TestNoLeak(t *testing.T) {
	ctx := context.Background()
	m, fs := newTestManager(t, platform.NewMemorySecretStore())
	const key = "sk-or-do-not-leak-me"

	if _, err := m.Save(ctx, key); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, w := range fs.written {
		if strings.Contains(w, key) {
			t.Fatalf("state store received the key: %q", w)
		}
	}

	status := m.Status(ctx)
	b, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal Status: %v", err)
	}
	if strings.Contains(string(b), key) {
		t.Fatalf("Status JSON leaked the key: %s", b)
	}

	if got := m.Key(); got != key {
		t.Fatalf("Key() = %q want %q", got, key)
	}
}
