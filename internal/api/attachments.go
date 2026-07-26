package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/10kkyvl/studioforge/internal/attachments"
)

// The storage itself lives in internal/attachments, because an agent capturing
// a Studio screenshot mid-run needs to save one too and has no business
// importing this package to do it. What stays here is the HTTP shape: reading a
// multipart body, turning a domain error into a status code, and serving a file
// back.

// uploadAttachment stores a pasted chat image and hands back its path relative
// to the project root — the same shape appendAttachmentsBlock later folds into a
// prompt, and getAttachment later resolves back to a file.
func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, attachments.MaxBytes+1024)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, 400, "invalid_upload", "A single image file is required", err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, r, http.StatusRequestEntityTooLarge, "attachment_too_large", "Images are capped at 10 MB", err)
		return
	}
	path, err := attachments.Save(project.Path, data)
	switch {
	case errors.Is(err, attachments.ErrTooLarge):
		writeError(w, r, http.StatusRequestEntityTooLarge, "attachment_too_large", "Images are capped at 10 MB", err)
		return
	case errors.Is(err, attachments.ErrUnsupportedType):
		writeError(w, r, 400, "unsupported_type", "Only PNG, JPEG, GIF, or WebP images are accepted", err)
		return
	case err != nil:
		writeError(w, r, 500, "storage_error", "Unable to save the attachment", err)
		return
	}
	writeJSON(w, 201, map[string]string{"path": path})
}

// getAttachment serves a stored chat image back, so the message history can
// render a thumbnail for the path riding inside prompt_snapshot.
func (s *Server) getAttachment(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	path, err := attachments.Resolve(project.Path, r.PathValue("name"))
	if err != nil {
		writeError(w, r, 400, "invalid_name", "Invalid attachment name", err)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, r, 404, "not_found", "Attachment not found", err)
		return
	}
	if ct, ok := attachments.ContentTypeFor(path); ok {
		w.Header().Set("Content-Type", ct)
	}
	// Content-addressed names never change meaning once minted, so caching
	// them hard is safe and saves re-fetching the same thumbnail on every
	// thread reload.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// validAttachmentRef reports whether ref is exactly "<attachments dir>/<name>"
// for some name that resolves inside the project's attachments directory.
// createRun trusts the composer to echo back exactly what /attachments just
// handed it, but the request still arrives over HTTP from a page that could be
// scripted, so the same containment check guarding downloads also guards what
// reaches the agent's prompt.
func validAttachmentRef(projectPath, ref string) bool {
	return attachments.ValidRef(projectPath, ref)
}

// appendAttachmentsBlock folds the composer's pasted-image paths into the
// prompt as their own trailing section, the same way buildTaskPrompt
// (board.go) folds a task's context in at the front. Claude Code has no
// content-block channel for images — StudioForge shells out to the CLI with
// the prompt as a single positional argument (claudecode/claude.go:213) — so
// a path in the prompt text, plus Claude Code's own Read tool, is the whole
// mechanism.
func appendAttachmentsBlock(prompt string, paths []string) string {
	return strings.TrimRight(prompt, "\n") + "\n\n" + attachments.Block(paths)
}
