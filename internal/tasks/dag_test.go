package tasks

import "testing"

func TestValidateDAG(t *testing.T) {
	if err := ValidateDAG([]Node{{ID: "a"}, {ID: "b", Dependencies: []string{"a"}}, {ID: "c", Dependencies: []string{"a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDAG([]Node{{ID: "a", Dependencies: []string{"b"}}, {ID: "b", Dependencies: []string{"a"}}}); err == nil {
		t.Fatal("cycle accepted")
	}
	if err := ValidateDAG([]Node{{ID: "a", Dependencies: []string{"missing"}}}); err == nil {
		t.Fatal("missing dependency accepted")
	}
}
