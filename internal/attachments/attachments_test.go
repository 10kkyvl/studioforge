package attachments

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A one-pixel PNG, so the sniffed type is genuinely image/png.
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

func TestSaveWritesInsideTheAttachmentsDirectory(t *testing.T) {
	root := t.TempDir()
	path, err := Save(root, tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, Dir+"/") {
		t.Fatalf("path=%q, want it under %q", path, Dir)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Fatalf("path=%q, want a .png extension from the sniffed type", path)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, tinyPNG) {
		t.Fatal("the stored bytes are not the ones handed over")
	}
}

// Content addressing is what keeps an agent that captures the same unchanged
// screen on three turns from leaving three copies behind.
func TestSaveIsContentAddressed(t *testing.T) {
	root := t.TempDir()
	first, err := Save(root, tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Save(root, tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("the same image produced %q then %q", first, second)
	}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(Dir)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the same image was stored %d times", len(entries))
	}
}

func TestSaveRefusesWhatTheChatCannotRender(t *testing.T) {
	root := t.TempDir()
	if _, err := Save(root, []byte("#!/bin/sh\nrm -rf /\n")); err != ErrUnsupportedType {
		t.Fatalf("err=%v, want ErrUnsupportedType", err)
	}
	// The type is sniffed off the bytes, so a PNG name cannot smuggle a script.
	if _, err := Save(root, []byte("<html><body>hi</body></html>")); err != ErrUnsupportedType {
		t.Fatalf("err=%v, want ErrUnsupportedType", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, filepath.FromSlash(Dir))); len(entries) != 0 {
		t.Fatalf("a refused image left %d files behind", len(entries))
	}
}

func TestSaveRefusesAnOversizedImage(t *testing.T) {
	root := t.TempDir()
	oversized := append(append([]byte{}, tinyPNG...), make([]byte, MaxBytes)...)
	if _, err := Save(root, oversized); err != ErrTooLarge {
		t.Fatalf("err=%v, want ErrTooLarge", err)
	}
}

func TestSaveDataURL(t *testing.T) {
	root := t.TempDir()
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNG)
	path, err := SaveDataURL(root, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"not a data url": "https://example.invalid/a.png",
		"no comma":       "data:image/png;base64",
		"not base64":     "data:image/png;base64,%%%%",
	} {
		if _, err := SaveDataURL(root, bad); err == nil {
			t.Errorf("SaveDataURL(%s) accepted %q", name, bad)
		}
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(Dir)), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"..", "../secret.txt", "../../secret.txt", "a/../../secret.txt", "..\\secret.txt", ".", "", "/etc/passwd"} {
		if resolved, err := Resolve(root, name); err == nil {
			t.Errorf("Resolve(%q) = %q, want an error", name, resolved)
		}
	}
	legit, err := Resolve(root, "2026-07-19-abc123def456.png")
	if err != nil {
		t.Fatalf("a plain filename must resolve: %v", err)
	}
	if !strings.HasSuffix(legit, filepath.Join(filepath.FromSlash(Dir), "2026-07-19-abc123def456.png")) {
		t.Errorf("resolved=%q", legit)
	}
}

func TestValidRef(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(Dir)), 0o700); err != nil {
		t.Fatal(err)
	}
	if !ValidRef(root, Dir+"/2026-07-19-abc123def456.png") {
		t.Error("a well-formed reference must be accepted")
	}
	for _, ref := range []string{"a.png", "../a.png", Dir + "/../a.png", Dir + "/", "/etc/passwd"} {
		if ValidRef(root, ref) {
			t.Errorf("ValidRef accepted %q", ref)
		}
	}
}

// The block is the contract web/src/lib/attachments.ts parses back out to
// render thumbnails, so its exact shape matters.
func TestBlock(t *testing.T) {
	if got := Block(nil); got != "" {
		t.Errorf("Block(nil) = %q, want empty so callers can append it blind", got)
	}
	got := Block([]string{Dir + "/a.png", Dir + "/b.png"})
	want := PromptHeader + "\n- " + Dir + "/a.png" + "\n- " + Dir + "/b.png"
	if got != want {
		t.Errorf("Block() = %q, want %q", got, want)
	}
}
