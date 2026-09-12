package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/10kkyvl/studioforge/internal/projects"
)

// A project file is deliberately small and read-only. Keeping this limit in
// the daemon means a browser cannot make it buffer a place file or an
// accidentally generated build artifact.
const maxProjectFileBytes int64 = 1 << 20

type projectFileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

type projectFileContent struct {
	Path       string    `json:"path"`
	Content    string    `json:"content"`
	Binary     bool      `json:"binary"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// resolveProjectFile performs the project lookup before resolving the
// request's relative path. Existing projects are normally registered during
// daemon startup; the lazy registration also covers a project whose directory
// was unavailable at startup and became available later.
func (s *Server) resolveProjectFile(r *http.Request) (string, error) {
	project, err := s.store.Project(r.Context(), r.PathValue("id"))
	if err != nil {
		return "", errProjectNotFound
	}
	if s.guard == nil {
		return "", errors.New("project path guard is unavailable")
	}
	relative := r.URL.Query().Get("path")
	path, err := s.guard.Resolve(project.ID, relative)
	if errors.Is(err, projects.ErrProjectRootNotRegistered) {
		if _, registerErr := s.guard.Register(project.ID, project.Path); registerErr != nil {
			return "", registerErr
		}
		path, err = s.guard.Resolve(project.ID, relative)
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

func (s *Server) projectFiles(w http.ResponseWriter, r *http.Request) {
	path, err := s.resolveProjectFile(r)
	if err != nil {
		s.writeProjectFileError(w, r, err)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "file_not_found", "Project path was not found", err)
		return
	}
	if !info.IsDir() {
		writeError(w, r, http.StatusBadRequest, "not_directory", "The requested project path is not a directory", nil)
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "directory_read_failed", "Unable to read the project directory", err)
		return
	}
	root, rootErr := s.store.Project(r.Context(), r.PathValue("id"))
	if rootErr != nil {
		writeError(w, r, http.StatusNotFound, "file_not_found", "Project path was not found", rootErr)
		return
	}
	rootPath, rootErr := s.guard.Resolve(root.ID, "")
	if rootErr != nil {
		writeError(w, r, http.StatusInternalServerError, "project_path_unavailable", "Project root is unavailable", rootErr)
		return
	}
	out := make([]projectFileEntry, 0, len(entries))
	for _, entry := range entries {
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		typeName := "file"
		if entryInfo.IsDir() {
			typeName = "directory"
		} else if entryInfo.Mode()&os.ModeSymlink != 0 {
			typeName = "symlink"
		}
		rel, relErr := filepath.Rel(rootPath, filepath.Join(path, entry.Name()))
		if relErr != nil {
			continue
		}
		out = append(out, projectFileEntry{
			Name: entry.Name(), Path: filepath.ToSlash(rel), Type: typeName,
			Size: entryInfo.Size(), ModifiedAt: entryInfo.ModTime().UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		rank := func(kind string) int {
			if kind == "directory" {
				return 0
			}
			if kind == "file" {
				return 1
			}
			return 2
		}
		if rank(out[i].Type) != rank(out[j].Type) {
			return rank(out[i].Type) < rank(out[j].Type)
		}
		left, right := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if left != right {
			return left < right
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, http.StatusOK, map[string]any{"path": r.URL.Query().Get("path"), "entries": out})
}

func (s *Server) projectFileContent(w http.ResponseWriter, r *http.Request) {
	path, err := s.resolveProjectFile(r)
	if err != nil {
		s.writeProjectFileError(w, r, err)
		return
	}
	// Lstat happens before Open so a FIFO, socket, or device can never make
	// this request block waiting for a writer. Resolve has already canonicalized
	// an in-project symlink, so this check applies to the actual target.
	info, err := os.Lstat(path)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "file_not_found", "Project file was not found", err)
		return
	}
	if !info.Mode().IsRegular() {
		writeError(w, r, http.StatusBadRequest, "unsupported_file_type", "Only regular project files can be viewed", nil)
		return
	}
	if info.Size() > maxProjectFileBytes {
		writeErrorDetails(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "Project files are capped at 1 MB", map[string]any{"maxBytes": maxProjectFileBytes, "size": info.Size()}, nil)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "file_not_found", "Project file was not found", err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxProjectFileBytes+1))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "file_read_failed", "Unable to read the project file", err)
		return
	}
	if int64(len(data)) > maxProjectFileBytes {
		writeErrorDetails(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "Project files are capped at 1 MB", map[string]any{"maxBytes": maxProjectFileBytes}, nil)
		return
	}
	binary := bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
	content := ""
	if !binary {
		content = string(data)
	}
	writeJSON(w, http.StatusOK, projectFileContent{
		Path: r.URL.Query().Get("path"), Content: content, Binary: binary,
		Size: info.Size(), ModifiedAt: info.ModTime().UTC(),
	})
}

func (s *Server) writeProjectFileError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errProjectNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "Project not found", err)
		return
	}
	if errors.Is(err, projects.ErrOutsideProject) {
		writeError(w, r, http.StatusForbidden, "path_outside_project", "The requested path is outside the project", nil)
		return
	}
	if errors.Is(err, projects.ErrProjectRootNotRegistered) {
		writeError(w, r, http.StatusConflict, "project_path_unavailable", "Project root is unavailable", err)
		return
	}
	writeError(w, r, http.StatusBadRequest, "invalid_project_path", "Unable to resolve the project path", err)
}

var errProjectNotFound = errors.New("project not found")
