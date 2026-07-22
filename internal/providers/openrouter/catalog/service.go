package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type CacheStore interface {
	GetModelCache(ctx context.Context) (payload []byte, fetchedAt time.Time, err error)
	SetModelCache(ctx context.Context, payload []byte, fetchedAt time.Time) error
}

type Config struct {
	HTTPClient *http.Client
	BaseURL    string
	Cache      CacheStore
	TTL        time.Duration
	Now        func() time.Time
}

type Source string

const (
	SourceLive     Source = "live"
	SourceCache    Source = "cache"
	SourceFallback Source = "fallback"
)

const DefaultTTL = 6 * time.Hour

const fetchTimeout = 15 * time.Second

type Service struct {
	mu         sync.Mutex
	cfg        Config
	memo       []Model
	memoAt     time.Time
	memoSource Source
}

func NewService(cfg Config) *Service {
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg}
}

func (s *Service) now() time.Time { return s.cfg.Now() }

func cloneModels(models []Model) []Model {
	out := make([]Model, len(models))
	copy(out, models)
	return out
}

func (s *Service) Models(ctx context.Context) ([]Model, Source, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if !s.memoAt.IsZero() && now.Sub(s.memoAt) < s.cfg.TTL {
		return cloneModels(s.memo), s.memoSource, nil
	}

	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	models, err := Fetch(fetchCtx, s.cfg.HTTPClient, s.cfg.BaseURL)
	if err == nil {
		s.storeLocked(ctx, models, now, SourceLive, true)
		return cloneModels(models), SourceLive, nil
	}

	if s.cfg.Cache != nil {
		payload, _, cacheErr := s.cfg.Cache.GetModelCache(ctx)
		if cacheErr == nil && len(payload) > 0 {
			var cached []Model
			if jsonErr := json.Unmarshal(payload, &cached); jsonErr == nil {
				s.storeLocked(ctx, cached, now, SourceCache, false)
				return cloneModels(cached), SourceCache, nil
			}
		}
	}

	fallback := FallbackModels()
	s.storeLocked(ctx, fallback, now, SourceFallback, false)
	return cloneModels(fallback), SourceFallback, nil
}

func (s *Service) Refresh(ctx context.Context) ([]Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	models, err := Fetch(fetchCtx, s.cfg.HTTPClient, s.cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	now := s.now()
	s.storeLocked(ctx, models, now, SourceLive, true)
	return cloneModels(models), nil
}

func (s *Service) storeLocked(ctx context.Context, models []Model, now time.Time, source Source, persist bool) {
	s.memo = cloneModels(models)
	s.memoAt = now
	s.memoSource = source
	if persist && s.cfg.Cache != nil {
		if payload, err := json.Marshal(models); err == nil {
			_ = s.cfg.Cache.SetModelCache(ctx, payload, now)
		}
	}
}
