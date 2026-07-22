package credential

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/10kkyvl/studioforge/internal/platform"
)

type State string

const (
	StateNotConfigured State = "not_configured"
	StateUnverified    State = "unverified"
	StateConfigured    State = "configured"
	StateInvalid       State = "invalid"
)

type Source string

const (
	SourceNone     Source = "none"
	SourceKeychain Source = "keychain"
	SourceSession  Source = "session"
	SourceEnv      Source = "env"
)

type Status struct {
	State  State  `json:"state"`
	Source Source `json:"source"`
	Secure bool   `json:"secure"`
}

type ConnectionErrorKind string

const (
	ConnectionMissing  ConnectionErrorKind = "missing_key"
	ConnectionNetwork  ConnectionErrorKind = "network"
	ConnectionTimeout  ConnectionErrorKind = "timeout"
	ConnectionUpstream ConnectionErrorKind = "upstream"
	ConnectionInternal ConnectionErrorKind = "internal"
)

type ConnectionError struct {
	Kind       ConnectionErrorKind
	StatusCode int
	Provider   string
}

func (e *ConnectionError) Error() string {
	provider := e.Provider
	if provider == "" {
		provider = "OpenRouter"
	}
	switch e.Kind {
	case ConnectionMissing:
		return "no " + provider + " API key is configured"
	case ConnectionNetwork:
		return "could not reach " + provider
	case ConnectionTimeout:
		return provider + " connection timed out"
	case ConnectionUpstream:
		return provider + " returned an unexpected response"
	default:
		return "could not update " + provider + " key verification state"
	}
}

type Config struct {
	Service      string
	Account      string
	EnvVar       string
	Secure       platform.SecretStore
	GetState     func(context.Context) (string, error)
	SetState     func(context.Context, string) error
	BaseURL      string
	ProviderName string
	TestPath     string
	HTTPClient   *http.Client
}

type Manager struct {
	mu      sync.Mutex
	opMu    sync.Mutex
	cfg     Config
	session []byte
}

func NewManager(cfg Config) *Manager {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://openrouter.ai/api/v1"
	}
	if cfg.ProviderName == "" {
		cfg.ProviderName = "OpenRouter"
	}
	if cfg.TestPath == "" {
		cfg.TestPath = "/key"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Manager{cfg: cfg}
}

func (m *Manager) Key() string {
	m.mu.Lock()
	secure := m.cfg.Secure
	account := m.cfg.Account
	session := m.session
	envVar := m.cfg.EnvVar
	m.mu.Unlock()

	if secure != nil {
		if v, err := secure.Get(context.Background(), account); err == nil && len(v) > 0 {
			return string(v)
		}
	}
	if len(session) > 0 {
		return string(session)
	}
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return v
		}
	}
	return ""
}

func (m *Manager) Save(ctx context.Context, key string) (Status, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	key = strings.TrimSpace(key)
	if key == "" {
		return Status{}, errors.New("key must not be empty")
	}
	m.mu.Lock()
	secure := m.cfg.Secure
	account := m.cfg.Account
	m.mu.Unlock()

	if secure == nil {
		m.mu.Lock()
		m.session = []byte(key)
		m.mu.Unlock()
	} else if err := secure.Set(ctx, account, []byte(key)); err == nil {
		m.mu.Lock()
		m.session = nil
		m.mu.Unlock()
	} else {
		m.mu.Lock()
		m.session = []byte(key)
		m.mu.Unlock()
	}

	if err := m.setState(ctx, string(StateUnverified)); err != nil {
		return Status{}, err
	}
	return m.Status(ctx), nil
}

func (m *Manager) Remove(ctx context.Context) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	secure := m.cfg.Secure
	account := m.cfg.Account
	m.mu.Unlock()

	if secure != nil {
		if err := secure.Delete(ctx, account); err != nil {
			return errors.New("secure credential deletion failed")
		}
		if _, err := secure.Get(ctx, account); err == nil {
			return errors.New("secure credential remained after deletion")
		} else if !errors.Is(err, platform.ErrSecretNotFound) {
			return errors.New("secure credential deletion could not be verified")
		}
	}
	m.mu.Lock()
	m.session = nil
	m.mu.Unlock()
	return m.setState(ctx, "")
}

func (m *Manager) Status(ctx context.Context) Status {
	m.mu.Lock()
	secure := m.cfg.Secure
	account := m.cfg.Account
	session := m.session
	envVar := m.cfg.EnvVar
	m.mu.Unlock()

	source := SourceNone
	if secure != nil {
		if v, err := secure.Get(ctx, account); err == nil && len(v) > 0 {
			source = SourceKeychain
		}
	}
	if source == SourceNone && len(session) > 0 {
		source = SourceSession
	}
	if source == SourceNone && envVar != "" && os.Getenv(envVar) != "" {
		source = SourceEnv
	}

	secureFlag := source == SourceKeychain
	if source == SourceNone {
		return Status{State: StateNotConfigured, Source: SourceNone, Secure: secureFlag}
	}

	state := m.readState(ctx)
	return Status{State: state, Source: source, Secure: secureFlag}
}

func (m *Manager) readState(ctx context.Context) State {
	m.mu.Lock()
	fn := m.cfg.GetState
	m.mu.Unlock()
	if fn == nil {
		return StateUnverified
	}
	s, err := fn(ctx)
	if err != nil {
		return StateUnverified
	}
	switch State(s) {
	case StateConfigured:
		return StateConfigured
	case StateInvalid:
		return StateInvalid
	default:
		return StateUnverified
	}
}

func (m *Manager) setState(ctx context.Context, s string) error {
	m.mu.Lock()
	fn := m.cfg.SetState
	m.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, s)
}

func (m *Manager) TestConnection(ctx context.Context) (Status, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	key := m.Key()
	if key == "" {
		return m.Status(ctx), m.connectionError(ConnectionMissing, 0)
	}

	m.mu.Lock()
	baseURL := m.cfg.BaseURL
	testPath := m.cfg.TestPath
	provider := m.cfg.ProviderName
	client := m.cfg.HTTPClient
	m.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/"+strings.TrimLeft(testPath, "/"), nil)
	if err != nil {
		return m.Status(ctx), errors.New("could not build " + provider + " request")
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		if stateErr := m.setState(ctx, string(StateUnverified)); stateErr != nil {
			return m.Status(ctx), m.connectionError(ConnectionInternal, 0)
		}
		var netErr interface{ Timeout() bool }
		if errors.As(err, &netErr) && netErr.Timeout() || errors.Is(err, context.DeadlineExceeded) {
			return m.Status(ctx), m.connectionError(ConnectionTimeout, 0)
		}
		return m.Status(ctx), m.connectionError(ConnectionNetwork, 0)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		if err := m.setState(ctx, string(StateConfigured)); err != nil {
			return m.Status(ctx), m.connectionError(ConnectionInternal, 0)
		}
		return m.Status(ctx), nil
	case http.StatusUnauthorized:
		if err := m.setState(ctx, string(StateInvalid)); err != nil {
			return m.Status(ctx), m.connectionError(ConnectionInternal, 0)
		}
		return m.Status(ctx), nil
	default:
		if err := m.setState(ctx, string(StateUnverified)); err != nil {
			return m.Status(ctx), m.connectionError(ConnectionInternal, 0)
		}
		return m.Status(ctx), m.connectionError(ConnectionUpstream, resp.StatusCode)
	}
}

func (m *Manager) connectionError(kind ConnectionErrorKind, status int) *ConnectionError {
	m.mu.Lock()
	provider := m.cfg.ProviderName
	m.mu.Unlock()
	return &ConnectionError{Kind: kind, StatusCode: status, Provider: provider}
}
