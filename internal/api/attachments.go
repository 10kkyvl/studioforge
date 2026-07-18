package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// chatAttachmentsDir is where pasted chat images live, relative to a
// project's root. Inside .studioforge/ — StudioForge's own bookkeeping
// directory, not the Rojo-managed tree the agent edits — so a pasted
// screenshot never gets synced into Studio or shipped as a place asset.
const chatAttachmentsDir = ".studioforge/attachments"

// maxAttachmentBytes caps a single pasted image at 10 MB: comfortably more
// than a full-resolution Studio screenshot needs, small enough that a
// mis-paste can't quietly balloon a prompt or the project's disk footprint.
const maxAttachmentBytes = 10 << 20

// attachmentExtensions is the MIME allowlist, checked against bytes actually
// sniffed off the upload (http.DetectContentType) rather than the
// client-declared Content-Type header — a browser paste always sends the
// true type, but nothing stops a scripted client from lying about it.
var attachmentExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// attachmentContentTypes is attachmentExtensions inverted, for replying to a
// GET. Built from our own extension map rather than mime.TypeByExtension:
// this package chose every extension it writes, so it doesn't need the OS
// mime registry (which may not even know ".webp") to say what they mean.
var attachmentContentTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// attachmentsPromptHeader marks the block appendAttachmentsBlock writes.
// web/src/lib/attachments.ts's parseAttachments splits it back out of
// prompt_snapshot to render thumbnails in the message list — the two must
// agree on this exact string.
const attachmentsPromptHeader = "## Attached images"

// uploadAttachment stores a pasted chat image content-addressed under the
// project's attachments directory and hands back its path relative to the
// project root — the same shape appendAttachmentsBlock later folds into a
// prompt, and getAttachment later resolves back to a file.
func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	// Refuse an oversized body before it's fully buffered, rather than reading
	// an unbounded multipart stream into memory and rejecting after the fact.
	// The slack above the real cap only has to cover multipart boundaries and
	// field headers, not another image's worth of bytes.
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentBytes+1<<16)
	if err := r.ParseMultipartForm(maxAttachmentBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "attachment_too_large", "Images are capped at 10 MB", nil)
			return
		}
		writeError(w, r, 400, "invalid_upload", "Unable to read the uploaded file", err)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, 400, "invalid_upload", "A file field is required", err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, r, 400, "invalid_upload", "Unable to read the uploaded file", err)
		return
	}
	// Belt-and-suspenders on top of MaxBytesReader above: the request body as a
	// whole could stay under that cap while still smuggling more than one
	// field, so the file's own bytes are checked again in isolation.
	if len(data) > maxAttachmentBytes {
		writeError(w, r, http.StatusRequestEntityTooLarge, "attachment_too_large", "Images are capped at 10 MB", nil)
		return
	}
	ext, ok := attachmentExtensions[http.DetectContentType(data)]
	if !ok {
		writeError(w, r, 400, "unsupported_type", "Only PNG, JPEG, GIF, or WebP images are accepted", nil)
		return
	}
	dir := filepath.Join(project.Path, chatAttachmentsDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeError(w, r, 500, "storage_error", "Unable to prepare the attachments directory", err)
		return
	}
	sum := sha256.Sum256(data)
	name := time.Now().UTC().Format("2006-01-02") + "-" + hex.EncodeToString(sum[:])[:12] + ext
	dest := filepath.Join(dir, name)
	// Content-addressed: the same screenshot pasted twice hashes to the same
	// name, so the second paste finds the file already there and costs no
	// extra write or disk space.
	if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			writeError(w, r, 500, "storage_error", "Unable to save the attachment", err)
			return
		}
	} else if err != nil {
		writeError(w, r, 500, "storage_error", "Unable to inspect the attachments directory", err)
		return
	}
	writeJSON(w, 201, map[string]string{"path": chatAttachmentsDir + "/" + name})
}

// getAttachment serves a previously uploaded chat image back, so the message
// history can render a thumbnail for the path riding inside prompt_snapshot.
func (s *Server) getAttachment(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	path, err := resolveAttachment(project.Path, r.PathValue("name"))
	if err != nil {
		writeError(w, r, 400, "invalid_name", "Invalid attachment name", err)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, r, 404, "not_found", "Attachment not found", err)
		return
	}
	if ct, ok := attachmentContentTypes[strings.ToLower(filepath.Ext(path))]; ok {
		w.Header().Set("Content-Type", ct)
	}
	// Content-addressed names never change meaning once minted, so caching
	// them hard is safe and saves re-fetching the same thumbnail on every
	// thread reload.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// resolveAttachment turns a URL-supplied attachment name into an absolute
// path inside a project's attachments directory, refusing anything that
// would land outside it. filepath.Abs + strings.HasPrefix is not a sufficient
// containment check on its own — "<root>/attachments-evil" has
// "<root>/attachments" as a string prefix without being inside it — so this
// instead joins and asks filepath.Rel what the relationship actually is: the
// only acceptable answer is "the name itself, unchanged". The upfront
// character checks reject the only inputs ("..", ".", a path with a
// separator) that could ever make Join produce something outside dir; the
// filepath.Rel check below is a second, independent line of defense that
// still holds even if those checks are ever loosened.
func resolveAttachment(projectPath, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", errors.New("attachment name must be a single path segment")
	}
	dir := filepath.Join(projectPath, chatAttachmentsDir)
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

// validAttachmentRef reports whether ref is exactly "<attachments
// dir>/<name>" for some name that resolves inside the project's attachments
// directory. createRun trusts the composer to echo back exactly what
// /attachments just handed it, but the request still arrives over HTTP from
// a page that could be scripted, so the same containment check guarding
// downloads also guards what reaches the agent's prompt.
func validAttachmentRef(projectPath, ref string) bool {
	name, ok := strings.CutPrefix(ref, chatAttachmentsDir+"/")
	if !ok {
		return false
	}
	_, err := resolveAttachment(projectPath, name)
	return err == nil
}

// appendAttachmentsBlock folds the composer's pasted-image paths into the
// prompt as their own trailing section, the same way buildTaskPrompt
// (board.go) folds a task's context in at the front. Claude Code has no
// content-block channel for images — StudioForge shells out to the CLI with
// the prompt as a single positional argument (claudecode/claude.go:213) — so
// a path in the prompt text, plus Claude Code's own Read tool, is the whole
// mechanism.
func appendAttachmentsBlock(prompt string, attachments []string) string {
	lines := make([]string, 0, len(attachments)+1)
	lines = append(lines, attachmentsPromptHeader)
	for _, path := range attachments {
		lines = append(lines, "- "+path)
	}
	return strings.TrimRight(prompt, "\n") + "\n\n" + strings.Join(lines, "\n")
}
