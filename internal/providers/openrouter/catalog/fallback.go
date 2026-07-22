package catalog

import (
	_ "embed"
	"encoding/json"
)

//go:embed fallback_models.json
var fallbackJSON []byte

const FallbackSnapshotDate = "2026-07-21"

var fallbackParsed []Model

func init() {
	if err := json.Unmarshal(fallbackJSON, &fallbackParsed); err != nil {
		panic("catalog: embedded fallback_models.json failed to parse: " + err.Error())
	}
}

func FallbackModels() []Model {
	out := make([]Model, len(fallbackParsed))
	copy(out, fallbackParsed)
	return out
}
