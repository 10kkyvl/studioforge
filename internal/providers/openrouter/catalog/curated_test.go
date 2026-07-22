package catalog

import "testing"

func TestCuratedModelsParsesToThirteen(t *testing.T) {
	curated := CuratedModels()
	if len(curated) != 13 {
		t.Fatalf("curated models=%d, want 13", len(curated))
	}
	for _, c := range curated {
		if c.ID == "" || c.Category == "" {
			t.Fatalf("curated entry missing id/category: %+v", c)
		}
	}
}

func TestCategoryOrderCoversEveryCuratedCategory(t *testing.T) {
	order := map[string]bool{}
	for _, c := range CategoryOrder {
		order[c] = true
	}
	for _, c := range CuratedModels() {
		if !order[c.Category] {
			t.Fatalf("curated category %q missing from CategoryOrder", c.Category)
		}
	}
}
