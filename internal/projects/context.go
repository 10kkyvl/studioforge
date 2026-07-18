package projects

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadContext gathers a project's standing context — its constitution and
// requirements — so every chat run carries it without the operator re-explaining
// the project each time. Missing files are skipped; the result is empty when the
// project has none.
func LoadContext(root string) string {
	var parts []string
	for _, rel := range []string{
		filepath.Join(".agent", "constitution.yaml"),
		filepath.Join(".agent", "requirements.md"),
	} {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		if text := strings.TrimSpace(string(body)); text != "" {
			parts = append(parts, "## "+filepath.ToSlash(rel)+"\n"+text)
		}
	}
	return strings.Join(parts, "\n\n")
}
