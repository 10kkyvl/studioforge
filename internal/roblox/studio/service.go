package studio

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrAmbiguous = errors.New("no unambiguous Studio binding is available")

type Instance struct {
	ID, Name, PlaceID, GameID string
	Active                    bool
	LastSeen                  time.Time
}
type Binding struct{ ProjectID, InstanceID string }
type Service struct {
	mu        sync.RWMutex
	instances map[string]Instance
	bindings  map[string]string
}

func NewService() *Service {
	return &Service{instances: map[string]Instance{}, bindings: map[string]string{}}
}
func (s *Service) Update(items []Instance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]Instance{}
	for _, item := range items {
		if item.LastSeen.IsZero() {
			item.LastSeen = time.Now().UTC()
		}
		next[item.ID] = item
	}
	s.instances = next
	for project, id := range s.bindings {
		if _, ok := next[id]; !ok {
			delete(s.bindings, project)
		}
	}
}
func (s *Service) Bind(projectID, instanceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.instances[instanceID]; !ok {
		return fmt.Errorf("Studio instance %s is no longer connected", instanceID)
	}
	for project, bound := range s.bindings {
		if bound == instanceID && project != projectID {
			return fmt.Errorf("Studio instance %s is already bound to another project", instanceID)
		}
	}
	s.bindings[projectID] = instanceID
	return nil
}
func (s *Service) Resolve(projectID string) (Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id, ok := s.bindings[projectID]; ok {
		instance, exists := s.instances[id]
		if !exists {
			return Instance{}, ErrAmbiguous
		}
		return instance, nil
	}
	if len(s.instances) == 1 {
		for _, instance := range s.instances {
			return instance, nil
		}
	}
	return Instance{}, ErrAmbiguous
}
func (s *Service) List() []Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Instance, 0, len(s.instances))
	for _, v := range s.instances {
		out = append(out, v)
	}
	return out
}
