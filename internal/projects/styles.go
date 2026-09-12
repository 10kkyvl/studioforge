package projects

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StyleSettingKey is the project_settings key that selects the project's
// installed UI style pack. Keeping it in project_settings means old databases
// need no schema migration and operator choices remain project scoped.
const StyleSettingKey = "style"

// DefaultStyleName is the style used by projects created before style packs
// existed and by a new project with no explicit selection.
const DefaultStyleName = "cute"

const (
	// StylePacksDir is the project-relative directory an operator can populate
	// with custom style packs.
	StylePacksDir = ".agent/styles"
	styleRoot     = StylePacksDir
)

// StylePack is the data an agent can read for one project's visual language.
// Palette and Type are optional machine-readable tables; Brief is the required
// human-readable part that is folded into a run's project context.
type StylePack struct {
	Name    string            `json:"name"`
	Brief   string            `json:"brief"`
	Palette map[string]string `json:"palette,omitempty"`
	Type    map[string]string `json:"type,omitempty"`
	BuiltIn bool              `json:"builtIn"`
}

//go:embed stylepacks/*/*
var builtInStyleFiles embed.FS

var builtInStyles = []string{"cute", "dark-fantasy", "cyber"}

// EnsureStylePacks installs the bundled packs into a project without replacing
// anything already present. This is deliberately write-once: an operator may
// tune a shipped brief or table and an application update must leave it intact.
func EnsureStylePacks(root string) error {
	handle, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer handle.Close()
	for _, name := range builtInStyles {
		dir := filepath.Join(styleRoot, name)
		if err := handle.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create style pack directory %q: %w", name, err)
		}
		for _, rel := range []string{"brief.md", "palette.json", "type.json"} {
			body, err := fs.ReadFile(builtInStyleFiles, filepath.ToSlash(filepath.Join("stylepacks", name, rel)))
			if err != nil {
				return fmt.Errorf("read bundled style pack %q: %w", name, err)
			}
			if err := ensureRootFile(handle, filepath.Join(dir, rel), body); err != nil {
				return fmt.Errorf("install style pack %q: %w", name, err)
			}
		}
	}
	return nil
}

// ValidStyleName accepts the directory names the API may select. Listing is
// intentionally stricter too, so a dropped pack can never make a path escape
// the project's .agent/styles directory.
func ValidStyleName(name string) bool {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return false
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || (r == '-' && i > 0) {
			continue
		}
		return false
	}
	return true
}

// ListStylePacks returns packs installed in the project. It does not install
// defaults; callers that need a complete catalog should call EnsureStylePacks
// first. Malformed optional tables are ignored so a hand-written brief remains
// usable, while a missing brief makes the directory invisible to selection.
func ListStylePacks(root string) ([]StylePack, error) {
	handle, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	entries, err := fs.ReadDir(handle.FS(), styleRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []StylePack{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read style packs: %w", err)
	}
	result := make([]StylePack, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !ValidStyleName(entry.Name()) {
			continue
		}
		pack, ok, err := readStylePack(handle, entry.Name())
		if err != nil {
			return nil, err
		}
		if ok {
			pack.BuiltIn = isBuiltInStyle(entry.Name())
			result = append(result, pack)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// LoadStyleContext reads one selected style's brief and optional tables. If a
// custom pack has been removed, it falls back to the bundled default after the
// caller has ensured the defaults. The result is intentionally plain Markdown
// so every provider receives the same instructions.
func LoadStyleContext(root, selected string) string {
	handle, err := os.OpenRoot(root)
	if err != nil {
		return ""
	}
	defer handle.Close()
	if !ValidStyleName(selected) {
		selected = DefaultStyleName
	}
	pack, ok, err := readStylePack(handle, selected)
	if err != nil || !ok {
		pack, _, _ = readStylePack(handle, DefaultStyleName)
		selected = DefaultStyleName
	}
	if strings.TrimSpace(pack.Brief) == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Project UI style: %s\n\n%s", selected, strings.TrimSpace(pack.Brief))
	if len(pack.Palette) > 0 {
		encoded, _ := json.MarshalIndent(pack.Palette, "", "  ")
		b.WriteString("\n\n### Palette\n\n```json\n")
		b.Write(encoded)
		b.WriteString("\n```")
	}
	if len(pack.Type) > 0 {
		encoded, _ := json.MarshalIndent(pack.Type, "", "  ")
		b.WriteString("\n\n### Type\n\n```json\n")
		b.Write(encoded)
		b.WriteString("\n```")
	}
	return b.String()
}

func readStylePack(root *os.Root, name string) (StylePack, bool, error) {
	dir := filepath.Join(styleRoot, name)
	body, err := readStyleFile(root, filepath.Join(dir, "brief.md"))
	if errors.Is(err, os.ErrNotExist) {
		return StylePack{}, false, nil
	}
	if err != nil {
		return StylePack{}, false, fmt.Errorf("read style pack %q: %w", name, err)
	}
	pack := StylePack{Name: name, Brief: string(body)}
	pack.Palette = readTable(root, filepath.Join(dir, "palette.json"))
	pack.Type = readTable(root, filepath.Join(dir, "type.json"))
	return pack, true, nil
}

func readTable(root *os.Root, path string) map[string]string {
	body, err := readStyleFile(root, path)
	if err != nil {
		return nil
	}
	var table map[string]string
	if json.Unmarshal(body, &table) != nil {
		return nil
	}
	return table
}

func isBuiltInStyle(name string) bool {
	for _, builtIn := range builtInStyles {
		if builtIn == name {
			return true
		}
	}
	return false
}

// Read small regular data files through a rooted handle: a pack or brief
// symlink cannot disclose files outside the project, even during a rename race.
func readStyleFile(root *os.Root, path string) ([]byte, error) {
	info, err := root.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("style data must be a regular file")
	}
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	const limit = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, fmt.Errorf("style data exceeds 64 KiB")
	}
	return body, nil
}

func ensureRootFile(root *os.Root, path string, body []byte) error {
	file, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
