package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureStylePacksInstallsDefaultsAndPreservesEdits(t *testing.T) {
	root := t.TempDir()
	if err := EnsureStylePacks(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range builtInStyles {
		if _, err := os.Stat(filepath.Join(root, styleRoot, name, "brief.md")); err != nil {
			t.Fatalf("default %q brief missing: %v", name, err)
		}
	}
	path := filepath.Join(root, styleRoot, "cute", "brief.md")
	if err := os.WriteFile(path, []byte("operator's version"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureStylePacks(root); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "operator's version" {
		t.Fatalf("EnsureStylePacks overwrote an operator edit: %q", body)
	}
}

func TestListStylePacksIncludesCustomPack(t *testing.T) {
	root := t.TempDir()
	if err := EnsureStylePacks(root); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(root, styleRoot, "my-farm")
	if err := os.MkdirAll(custom, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(custom, "brief.md"), []byte("my own style"), 0o600); err != nil {
		t.Fatal(err)
	}
	packs, err := ListStylePacks(root)
	if err != nil {
		t.Fatal(err)
	}
	var found StylePack
	for _, pack := range packs {
		if pack.Name == "my-farm" {
			found = pack
		}
	}
	if found.Brief != "my own style" || found.BuiltIn {
		t.Fatalf("custom pack not listed correctly: %+v", found)
	}
}

func TestLoadStyleContextContainsBriefAndTables(t *testing.T) {
	root := t.TempDir()
	if err := EnsureStylePacks(root); err != nil {
		t.Fatal(err)
	}
	got := LoadStyleContext(root, "cyber")
	for _, want := range []string{"Project UI style: cyber", "electric cyan", `"primary": "#2DE2E6"`, "Rajdhani"} {
		if !strings.Contains(got, want) {
			t.Errorf("style context missing %q: %s", want, got)
		}
	}
}

func TestValidStyleNameRejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../cute", "Cute", "cute/farm", "cute farm", "cute."} {
		if ValidStyleName(name) {
			t.Errorf("ValidStyleName(%q) = true", name)
		}
	}
	for _, name := range []string{"cute", "dark-fantasy", "my_farm2"} {
		if !ValidStyleName(name) {
			t.Errorf("ValidStyleName(%q) = false", name)
		}
	}
}

func TestStylePacksCannotEscapeRootThroughSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".agent")); err != nil {
		t.Skip(err)
	}
	if err := EnsureStylePacks(root); err == nil {
		t.Fatal("installed styles outside project")
	}
	if err := EnsureReference(root, ".agent/roblox-ui.md", "reference"); err == nil {
		t.Fatal("installed reference outside project")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatal("outside directory changed")
	}
	root = t.TempDir()
	if err := EnsureStylePacks(root); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(secret, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	brief := filepath.Join(root, styleRoot, "cyber", "brief.md")
	if err := os.Remove(brief); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, brief); err != nil {
		t.Fatal(err)
	}
	if got := LoadStyleContext(root, "cyber"); strings.Contains(got, "outside secret") {
		t.Fatal("symlink read escaped root")
	}
	if _, err := ListStylePacks(root); err == nil {
		t.Fatal("unsafe brief was accepted")
	}
}
