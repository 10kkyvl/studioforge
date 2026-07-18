package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

type SessionManager struct {
	mu            sync.RWMutex
	bootstrap     string
	bootstrapUsed bool
	sessions      map[string]time.Time
	ttl           time.Duration
}

func NewSessionManager(ttl time.Duration) (*SessionManager, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	return &SessionManager{bootstrap: token, sessions: map[string]time.Time{}, ttl: ttl}, nil
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *SessionManager) BootstrapToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bootstrap
}
func (s *SessionManager) Exchange(token string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bootstrapUsed || len(token) != len(s.bootstrap) || subtle.ConstantTimeCompare([]byte(token), []byte(s.bootstrap)) != 1 {
		return "", errors.New("invalid or already-used bootstrap token")
	}
	session, err := randomToken()
	if err != nil {
		return "", err
	}
	s.bootstrapUsed = true
	s.sessions[session] = time.Now().Add(s.ttl)
	return session, nil
}
func (s *SessionManager) Valid(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(s.sessions, token)
		return false
	}
	s.sessions[token] = time.Now().Add(s.ttl)
	return true
}
