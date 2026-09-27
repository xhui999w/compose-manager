package api

import (
	"github.com/compose-manager/compose-manager/backend/internal/access"
	"net/http"
)

func (s *Server) projectWebAccess(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.ProjectWebAccess(r.Context(), r.PathValue("key"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "WEB_ACCESS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) saveProjectWebAccess(w http.ResponseWriter, r *http.Request) {
	var cfg access.WebConfig
	if !decodeJSON(w, r, &cfg) {
		return
	}
	if err := s.service.SaveProjectWebAccess(r.Context(), r.PathValue("key"), cfg); err != nil {
		writeError(w, http.StatusBadRequest, "WEB_ACCESS_SAVE_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
