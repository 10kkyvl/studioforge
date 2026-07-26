// Package attachments stores the images that ride along with a chat thread —
// the ones an operator pastes into the composer, and the ones an agent captures
// from Roblox Studio while it works.
//
// It is a domain package rather than part of internal/api because both of those
// producers need it and only one of them is an HTTP handler: a provider running
// an agent's tool loop has an image in hand and no business importing the API
// layer to save it.
package attachments

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dir is where chat images live, relative to a project's root. Inside
// .studioforge/ — StudioForge's own bookkeeping directory, not the Rojo-managed
// tree the agent edits — so an image never gets synced into Studio or shipped as
// a place asset.
const Dir = ".studioforge/attachments"

// MaxBytes caps a single image at 10 MB: comfortably more than a
// full-resolution Studio screenshot needs, small enough that a mis-paste or a
// runaway tool can't quietly balloon a prompt or the project's disk footprint.
const MaxBytes = 10 << 20

// PromptHeader marks the block Block writes. web/src/lib/attachments.ts's
// parseAttachments splits it back out of a message to render thumbnails — the
// two must agree on this exact string.
const PromptHeader = "## Attached images"

// extensions is the MIME allowlist, checked against bytes actually sniffed off
// the image (http.DetectContentType) rather than any declared type — a browser
// paste always sends the true type, but nothing stops a scripted client, or a
// tool result from a Studio plugin, from claiming otherwise.
var extensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// contentTypes is extensions inverted, for replying to a GET. Built from our own
// extension map rather than mime.TypeByExtension: this package chose every
// extension it writes, so it does not need the OS mime registry (which may not
// even know ".webp") to say what they mean.
var contentTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// ErrUnsupportedType is returned for bytes that are not one of the four image
// formats a chat thread can render.
var ErrUnsupportedType = errors.New("only PNG, JPEG, GIF, or WebP images are accepted")

// ErrTooLarge is returned for an image past MaxBytes.
var ErrTooLarge = errors.New("images are capped at 10 MB")

// ContentTypeFor reports the Content-Type to serve a stored attachment with.
func ContentTypeFor(path string) (string, bool) {
	ct, ok := contentTypes[strings.ToLower(filepath.Ext(path))]
	return ct, ok
}

// Save stores an image content-addressed under the project's attachments
// directory and returns its path relative to the project root — the shape Block
// folds into a message and Resolve turns back into a file.
//
// Content-addressed means the same screenshot saved twice hashes to the same
// name, so the second save finds the file already there and costs no extra
// write. An agent that captures the same unchanged screen on three consecutive
// turns leaves one file behind, not three.
func Save(projectPath string, data []byte) (string, error) {
	if len(data) > MaxBytes {
		return "", ErrTooLarge
	}
	ext, ok := extensions[http.DetectContentType(data)]
	if !ok {
		return "", ErrUnsupportedType
	}
	dir := filepath.Join(projectPath, filepath.FromSlash(Dir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("prepare the attachments directory: %w", err)
	}
	sum := sha256.Sum256(data)
	name := time.Now().UTC().Format("2006-01-02") + "-" + hex.EncodeToString(sum[:])[:12] + ext
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			return "", fmt.Errorf("save the attachment: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("inspect the attachments directory: %w", err)
	}
	return Dir + "/" + name, nil
}

// SaveDataURL stores the image carried by a `data:` URL, which is the form a
// Studio screenshot arrives in on the in-process agent path: the MCP result
// carries base64 in an image content block and the bridge turns it into a data
// URL for the model. Anything that is not a base64 data URL is refused rather
// than guessed at.
func SaveDataURL(projectPath, dataURL string) (string, error) {
	_, payload, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(dataURL, "data:") {
		return "", errors.New("not a data URL")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("decode the image: %w", err)
	}
	return Save(projectPath, data)
}

// Resolve turns a supplied attachment name into an absolute path inside a
// project's attachments directory, refusing anything that would land outside it.
//
// filepath.Abs + strings.HasPrefix is not a sufficient containment check on its
// own — "<root>/attachments-evil" has "<root>/attachments" as a string prefix
// without being inside it — so this instead joins and asks filepath.Rel what the
// relationship actually is: the only acceptable answer is "the name itself,
// unchanged". The upfront character checks reject the only inputs ("..", ".", a
// path with a separator) that could ever make Join produce something outside
// dir; the filepath.Rel check below is a second, independent line of defence
// that still holds even if those checks are ever loosened.
func Resolve(projectPath, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", errors.New("attachment name must be a single path segment")
	}
	dir := filepath.Join(projectPath, filepath.FromSlash(Dir))
	target := filepath.Join(dir, name)
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return "", err
	}
	if rel != name || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("attachment name escapes the attachments directory")
	}
	return target, nil
}

// ValidRef reports whether ref is exactly "<Dir>/<name>" for some name that
// resolves inside the project's attachments directory.
func ValidRef(projectPath, ref string) bool {
	name, ok := strings.CutPrefix(ref, Dir+"/")
	if !ok {
		return false
	}
	_, err := Resolve(projectPath, name)
	return err == nil
}

// MaxPublishedPerRun bounds how many screenshots one run may put in front of
// the operator.
//
// An agent should capture as often as it needs to see something — that is what
// the tool is for, and the image costs whoever is running the model either way.
// Publishing is the separate question: every published image is re-read by the
// operator's browser and, on a provider that bills for them, is not free to
// keep in the conversation. So the first few reach the chat and the rest do
// not, which keeps a run that photographs each of its twenty steps from
// flooding a thread while leaving "show me two screenshots" working exactly as
// asked.
const MaxPublishedPerRun = 3

// Publisher decides which of a run's screenshots reach the operator. The zero
// value is ready to use and is not safe for concurrent use by several
// goroutines; one run's tool loop is single-threaded, which is where it lives.
type Publisher struct {
	published map[string]bool
	count     int
}

// Allow reports whether this image should be put in front of the operator, and
// records it if so.
//
// A repeat of an image already shown is refused whatever the count: paths are
// content-addressed, so the same unchanged screen captured three times in a row
// is the same path, and showing it again tells the operator nothing.
func (p *Publisher) Allow(path string) bool {
	if path == "" {
		return false
	}
	if p.published[path] {
		return false
	}
	if p.count >= MaxPublishedPerRun {
		return false
	}
	if p.published == nil {
		p.published = map[string]bool{}
	}
	p.published[path] = true
	p.count++
	return true
}

// Block renders image paths as the section the chat renders thumbnails from.
// Returns "" for no paths, so a caller can append it unconditionally.
func Block(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(PromptHeader)
	for _, path := range paths {
		b.WriteString("\n- ")
		b.WriteString(path)
	}
	return b.String()
}
