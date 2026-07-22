package catalog

import (
	_ "embed"
	"encoding/json"
	"sync"
)

type Curated struct {
	ID              string `json:"id"`
	Category        string `json:"category"`
	Recommendation  string `json:"recommendation"`
	Workload        string `json:"workload"`
	SupportsTools   bool   `json:"supports_tools"`
	SupportsImages  bool   `json:"supports_images"`
	ContextLength   int    `json:"context_length"`
	PromptPrice     string `json:"prompt_price"`
	CompletionPrice string `json:"completion_price"`
	Free            bool   `json:"free"`
	LastReviewed    string `json:"last_reviewed"`
}

//go:embed curated.json
var curatedJSON []byte

var CategoryOrder = []string{
	"Free automatic",
	"Free recommended",
	"Best coding",
	"Balanced",
	"Fast and cheap",
	"Strong reasoning",
	"Large context",
}

var (
	curatedOnce   sync.Once
	curatedParsed []Curated
)

func CuratedModels() []Curated {
	curatedOnce.Do(func() {
		var parsed []Curated
		if err := json.Unmarshal(curatedJSON, &parsed); err != nil {
			panic("catalog: embedded curated.json failed to parse: " + err.Error())
		}
		curatedParsed = parsed
	})
	out := make([]Curated, len(curatedParsed))
	copy(out, curatedParsed)
	return out
}
