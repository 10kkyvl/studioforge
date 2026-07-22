package events

import (
	"context"
	"sync"

	"github.com/10kkyvl/studioforge/internal/models"
)

type Persister interface {
	AppendEvents(context.Context, []models.RunEvent) ([]models.RunEvent, error)
}

type Hub struct {
	store  Persister
	mu     sync.Mutex
	next   int
	subs   map[int]chan models.RunEvent
	closed bool
}

func NewHub(store Persister) *Hub { return &Hub{store: store, subs: map[int]chan models.RunEvent{}} }

func (h *Hub) Publish(ctx context.Context, input ...models.RunEvent) ([]models.RunEvent, error) {
	persisted, err := h.store.AppendEvents(ctx, input)
	if err != nil {
		return nil, err
	}
	h.broadcast(persisted)
	return persisted, nil
}

func (h *Hub) PublishTransient(input ...models.RunEvent) {
	h.broadcast(input)
}

func (h *Hub) broadcast(events []models.RunEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for _, event := range events {
		for id, ch := range h.subs {
			select {
			case ch <- event:
			default:
				if event.ID == 0 {
					continue
				}
				close(ch)
				delete(h.subs, id)
			}
		}
	}
}

func (h *Hub) Subscribe(buffer int) (<-chan models.RunEvent, func()) {
	if buffer < 1 {
		buffer = 64
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan models.RunEvent, buffer)
	if h.closed {
		close(ch)
		return ch, func() {}
	}
	id := h.next
	h.next++
	h.subs[id] = ch
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if existing, ok := h.subs[id]; ok {
				delete(h.subs, id)
				close(existing)
			}
		})
	}
	return ch, cancel
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, ch := range h.subs {
		close(ch)
		delete(h.subs, id)
	}
}
