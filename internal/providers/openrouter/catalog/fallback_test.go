package catalog

import "testing"

func TestFallbackModelsParsesToThirtyFive(t *testing.T) {
	models := FallbackModels()
	if len(models) != 35 {
		t.Fatalf("fallback models=%d, want 35", len(models))
	}
	for _, m := range models {
		if m.ID == "" {
			t.Fatalf("fallback model missing id: %+v", m)
		}
	}
}

func TestFallbackModelsReturnsAnIndependentCopy(t *testing.T) {
	first := FallbackModels()
	first[0].ID = "mutated"
	second := FallbackModels()
	if second[0].ID == "mutated" {
		t.Fatal("FallbackModels must return a copy, not shared backing data")
	}
}
