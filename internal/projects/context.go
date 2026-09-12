package projects

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadContext gathers a project's standing context — its constitution,
// requirements, and selected UI style — so every chat run carries it without
// the operator re-explaining the project each time. The optional style argument
// keeps existing callers compatible; when omitted, an installed default style
// is used. Missing files are skipped.
func LoadContext(root string, selectedStyle ...string) string {
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
	style := DefaultStyleName
	if len(selectedStyle) > 0 && strings.TrimSpace(selectedStyle[0]) != "" {
		style = selectedStyle[0]
	}
	if text := strings.TrimSpace(LoadStyleContext(root, style)); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n")
}
