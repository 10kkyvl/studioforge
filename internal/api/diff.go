package api

import "net/http"

func (s *Server) runDiff(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, 404, "not_found", "Run not found", err)
		return
	}
	project, err := s.store.Project(r.Context(), run.ProjectID)
	if err != nil {
		writeError(w, r, 404, "not_found", "Project not found", err)
		return
	}
	if s.differ == nil {
		writeJSON(w, 200, map[string]string{"diff": "", "note": "Diffing is not available"})
		return
	}
	diff, err := s.differ.DiffHead(r.Context(), project.Path)
	if err != nil {
		writeJSON(w, 200, map[string]string{"diff": "", "note": "Unable to compute diff: " + err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"diff": diff})
}
