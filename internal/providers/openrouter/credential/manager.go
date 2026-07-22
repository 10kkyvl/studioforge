package credential

import (
	"context"
	"errors"
	"fmt"
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

type Config struct {
	Service    string
	Account    string
	EnvVar     string
	Secure     platform.SecretStore
	GetState   func(context.Context) (string, error)
	SetState   func(context.Context, string) error
	BaseURL    string
	HTTPClient *http.Client
}

type Manager struct {
	mu      sync.Mutex
	cfg     Config
	session []byte
}

func NewManager(cfg Config) *Manager {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://openrouter.ai/api/v1"
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
	m.mu.Lock()
	secure := m.cfg.Secure
	account := m.cfg.Account
	m.mu.Unlock()

	if secure != nil {
		_ = secure.Delete(ctx, account)
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
	key := m.Key()
	if key == "" {
		return m.Status(ctx), errors.New("no OpenRouter API key configured")
	}

	m.mu.Lock()
	baseURL := m.cfg.BaseURL
	client := m.cfg.HTTPClient
	m.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/key", nil)
	if err != nil {
		return m.Status(ctx), errors.New("could not build OpenRouter request")
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		_ = m.setState(ctx, string(StateUnverified))
		return m.Status(ctx), errors.New("could not reach OpenRouter")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		_ = m.setState(ctx, string(StateConfigured))
		return m.Status(ctx), nil
	case http.StatusUnauthorized:
		_ = m.setState(ctx, string(StateInvalid))
		return m.Status(ctx), nil
	default:
		_ = m.setState(ctx, string(StateUnverified))
		return m.Status(ctx), fmt.Errorf("openrouter key check failed: status %d", resp.StatusCode)
	}
}
