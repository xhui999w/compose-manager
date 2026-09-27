package api

import (
	"github.com/compose-manager/compose-manager/backend/internal/app"
	"net/http"
)

func (s *Server) previewProjectRemoval(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	plan, err := s.service.PreviewProjectRemoval(r.Context(), r.PathValue("key"))
	if err != nil {
		writeError(w, http.StatusConflict, "DELETE_PREVIEW_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": plan})
}
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	var request app.RemovalRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := s.service.DeleteProject(r.Context(), r.PathValue("key"), request)
	if err != nil {
		writeError(w, http.StatusConflict, "DELETE_REJECTED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}
