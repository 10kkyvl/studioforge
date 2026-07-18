package studio

import (
	"errors"
	"testing"
)

func TestBindingRefusesAmbiguityAndCrossProjectReuse(t *testing.T) {
	s := NewService()
	s.Update([]Instance{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}})
	if _, err := s.Resolve("p"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("error=%v", err)
	}
	if err := s.Bind("p1", "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Bind("p2", "a"); err == nil {
		t.Fatal("shared mutating Studio binding accepted")
	}
	instance, err := s.Resolve("p1")
	if err != nil || instance.ID != "a" {
		t.Fatalf("instance=%+v err=%v", instance, err)
	}
	s.Update([]Instance{{ID: "b", Name: "B"}})
	if _, err := s.Resolve("p1"); err != nil {
		t.Fatalf("single remaining Studio should resolve: %v", err)
	}
}
