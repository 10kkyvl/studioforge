package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

type fakeStore struct {
	mu   sync.Mutex
	next int64
}

func (s *fakeStore) AppendEvents(_ context.Context, events []models.RunEvent) ([]models.RunEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range events {
		s.next++
		events[i].ID = s.next
	}
	return events, nil
}
func TestPublishAfterPersistenceAndSlowDisconnect(t *testing.T) {
	store := &fakeStore{}
	hub := NewHub(store)
	defer hub.Close()
	fast, cancelFast := hub.Subscribe(2)
	defer cancelFast()
	slow, _ := hub.Subscribe(1)
	persisted, err := hub.Publish(context.Background(), models.RunEvent{Type: "a"}, models.RunEvent{Type: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if persisted[0].ID != 1 || persisted[1].ID != 2 {
		t.Fatalf("ids=%v", persisted)
	}
	select {
	case event := <-fast:
		if event.ID != 1 {
			t.Fatalf("first=%d", event.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	for range slow {
	}
	if len(hub.subs) != 1 {
		t.Fatalf("slow subscriber was not removed: %d", len(hub.subs))
	}
}
