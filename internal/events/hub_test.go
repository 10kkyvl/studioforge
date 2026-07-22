package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/10kkyvl/studioforge/internal/models"
)

type fakeStore struct {
	mu    sync.Mutex
	next  int64
	calls int
}

func (s *fakeStore) AppendEvents(_ context.Context, events []models.RunEvent) ([]models.RunEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	for i := range events {
		s.next++
		events[i].ID = s.next
	}
	return events, nil
}

func TestTransientPublishSkipsPersistence(t *testing.T) {
	store := &fakeStore{}
	hub := NewHub(store)
	defer hub.Close()
	stream, cancel := hub.Subscribe(1)
	defer cancel()

	hub.PublishTransient(models.RunEvent{RunID: "run-1", Type: "message", RawType: "openrouter.message.partial", Payload: map[string]any{"text": "delta"}})

	event := <-stream
	if event.ID != 0 || event.RawType != "openrouter.message.partial" {
		t.Fatalf("transient event = %+v", event)
	}
	if store.calls != 0 {
		t.Fatalf("transient event caused %d persistence calls", store.calls)
	}
}

func TestSlowSubscriberDropsTransientWithoutDisconnecting(t *testing.T) {
	hub := NewHub(&fakeStore{})
	defer hub.Close()
	stream, cancel := hub.Subscribe(1)
	defer cancel()

	hub.PublishTransient(models.RunEvent{RawType: "openrouter.message.partial"})
	hub.PublishTransient(models.RunEvent{RawType: "openrouter.message.partial"})
	if len(hub.subs) != 1 {
		t.Fatal("transient backpressure disconnected subscriber")
	}
	<-stream
	persisted, err := hub.Publish(context.Background(), models.RunEvent{Type: "message"})
	if err != nil || len(persisted) != 1 {
		t.Fatalf("publish=%+v err=%v", persisted, err)
	}
	select {
	case event := <-stream:
		if event.ID == 0 {
			t.Fatalf("event=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive persisted event")
	}
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
