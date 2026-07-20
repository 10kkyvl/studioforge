package resources

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrLeaseLost = errors.New("resource lease is no longer owned")

type lease struct {
	owner     string
	heartbeat time.Time
	expires   time.Time
}
type Manager struct {
	mu      sync.Mutex
	leases  map[string]lease
	changed chan struct{}
	ttl     time.Duration
	stop    chan struct{}
	done    chan struct{}
}

// TTL reports the lease lifetime this manager was configured with, so a
// caller that needs to renew a lease on its own schedule (rather than via the
// regular per-run heartbeat loop) can pace itself safely inside it.
func (m *Manager) TTL() time.Duration { return m.ttl }

func NewManager(ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	m := &Manager{leases: map[string]lease{}, changed: make(chan struct{}), ttl: ttl, stop: make(chan struct{}), done: make(chan struct{})}
	go m.reap()
	return m
}

type Handle struct {
	manager *Manager
	owner   string
	keys    []string
	once    sync.Once
}

func (m *Manager) Acquire(ctx context.Context, owner string, keys []string) (*Handle, error) {
	keys = normalized(keys)
	if len(keys) == 0 {
		return &Handle{manager: m, owner: owner}, nil
	}
	for {
		m.mu.Lock()
		now := time.Now()
		available := true
		for _, key := range keys {
			if l, ok := m.leases[key]; ok && l.owner != owner && l.expires.After(now) {
				available = false
				break
			}
		}
		if available {
			for _, key := range keys {
				m.leases[key] = lease{owner: owner, heartbeat: now, expires: now.Add(m.ttl)}
			}
			m.signalLocked()
			m.mu.Unlock()
			return &Handle{manager: m, owner: owner, keys: keys}, nil
		}
		changed := m.changed
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func normalized(keys []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func (h *Handle) Heartbeat() error {
	m := h.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, key := range h.keys {
		l, ok := m.leases[key]
		if !ok || l.owner != h.owner {
			return ErrLeaseLost
		}
		l.heartbeat = now
		l.expires = now.Add(m.ttl)
		m.leases[key] = l
	}
	return nil
}
func (h *Handle) Release() {
	h.once.Do(func() {
		m := h.manager
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, key := range h.keys {
			if l, ok := m.leases[key]; ok && l.owner == h.owner {
				delete(m.leases, key)
			}
		}
		m.signalLocked()
	})
}

func (m *Manager) signalLocked() { close(m.changed); m.changed = make(chan struct{}) }
func (m *Manager) Snapshot() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for key, l := range m.leases {
		out[key] = l.owner
	}
	return out
}
func (m *Manager) reap() {
	ticker := time.NewTicker(m.ttl / 2)
	defer ticker.Stop()
	defer close(m.done)
	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			m.mu.Lock()
			changed := false
			for key, l := range m.leases {
				if !l.expires.After(now) {
					delete(m.leases, key)
					changed = true
				}
			}
			if changed {
				m.signalLocked()
			}
			m.mu.Unlock()
		}
	}
}
func (m *Manager) Close() {
	select {
	case <-m.stop:
		return
	default:
		close(m.stop)
		<-m.done
	}
}
