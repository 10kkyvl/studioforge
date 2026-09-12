package api

import (
	"database/sql"
	"errors"
	"net/http"
)

func (s *Server) projectUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.Project(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, r, 404, "project_not_found", "Project not found", err)
		} else {
			writeError(w, r, 500, "database_error", "Unable to load project", err)
		}
		return
	}
	report, err := s.store.UsageReport(r.Context(), id)
	if err != nil {
		writeError(w, r, 500, "database_error", "Unable to load usage", err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
