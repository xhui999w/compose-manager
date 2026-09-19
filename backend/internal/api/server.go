package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/app"
	authsvc "github.com/compose-manager/compose-manager/backend/internal/auth"
	"github.com/compose-manager/compose-manager/backend/internal/compose"
	"github.com/compose-manager/compose-manager/backend/internal/store"
)

type Server struct {
	service        *app.Service
	auth           *authsvc.Manager
	logger         *slog.Logger
	mux            *http.ServeMux
	ipLimiter      *authsvc.Limiter
	accountLimiter *authsvc.Limiter
}

func New(service *app.Service, auth *authsvc.Manager, logger *slog.Logger, static http.Handler) http.Handler {
	server := &Server{service: service, auth: auth, logger: logger, mux: http.NewServeMux(), ipLimiter: authsvc.NewLimiter(30, 15*time.Minute), accountLimiter: authsvc.NewLimiter(8, 15*time.Minute)}
	server.routes(static)
	return server.security(server.recover(server.log(server.authenticate(server.mux))))
}

func (s *Server) routes(static http.Handler) {
	s.mux.HandleFunc("GET /api/v1/health", s.health)
	s.mux.HandleFunc("GET /api/v1/auth/status", s.authStatus)
	s.mux.HandleFunc("POST /api/v1/auth/setup", s.authSetup)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.authLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.authLogout)
	s.mux.HandleFunc("GET /api/v1/overview", s.overview)
	s.mux.HandleFunc("GET /api/v1/compose/projects", s.projects)
	s.mux.HandleFunc("POST /api/v1/compose/projects/{key}/actions", s.projectAction)
	s.mux.HandleFunc("GET /api/v1/compose/projects/{key}/logs", s.projectLogs)
	s.mux.HandleFunc("GET /api/v1/compose/projects/{key}/file", s.getFile)
	s.mux.HandleFunc("POST /api/v1/compose/projects/{key}/file/validate", s.validateFile)
	s.mux.HandleFunc("PUT /api/v1/compose/projects/{key}/file", s.saveFile)
	s.mux.HandleFunc("GET /api/v1/compose/projects/{key}/versions", s.versions)
	s.mux.HandleFunc("POST /api/v1/compose/projects/{key}/versions/{id}/restore", s.restore)
	s.mux.HandleFunc("GET /api/v1/containers", s.containers)
	s.mux.HandleFunc("POST /api/v1/containers/{id}/actions", s.containerAction)
	s.mux.HandleFunc("GET /api/v1/containers/{id}/inspect", s.inspectContainer)
	s.mux.HandleFunc("GET /api/v1/containers/{id}/logs", s.containerLogs)
	s.mux.HandleFunc("GET /api/v1/images", s.images)
	s.mux.HandleFunc("POST /api/v1/images/check-updates", s.checkImageUpdates)
	s.mux.HandleFunc("DELETE /api/v1/images/{id}", s.deleteImage)
	s.mux.HandleFunc("GET /api/v1/updates", s.updates)
	s.mux.HandleFunc("POST /api/v1/updates/run", s.runUpdate)
	s.mux.HandleFunc("GET /api/v1/settings", s.settings)
	s.mux.HandleFunc("PUT /api/v1/settings", s.putSettings)
	s.mux.Handle("/", static)
}

type authStatusResponse struct {
	SetupRequired bool   `json:"setupRequired"`
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
	CSRFToken     string `json:"csrfToken,omitempty"`
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	setupRequired, err := s.auth.SetupRequired(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "AUTH_STATUS_FAILED", err)
		return
	}
	status := authStatusResponse{SetupRequired: setupRequired}
	if setupRequired {
		writeJSON(w, http.StatusOK, map[string]any{"data": status})
		return
	}
	cookie, err := r.Cookie(s.auth.CookieName())
	if err == nil {
		if session, authErr := s.auth.Authenticate(r.Context(), cookie.Value); authErr == nil {
			status.Authenticated = true
			status.Username = session.Username
			status.CSRFToken = session.CSRFToken
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status})
}

func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "ORIGIN_REJECTED", errors.New("request origin is not allowed"))
		return
	}
	var request struct {
		SetupToken string `json:"setupToken"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := s.auth.Setup(r.Context(), request.SetupToken, request.Username, request.Password)
	if err != nil {
		status, code := http.StatusBadRequest, "SETUP_FAILED"
		if errors.Is(err, authsvc.ErrInvalidSetupToken) {
			status, code = http.StatusForbidden, "INVALID_SETUP_TOKEN"
		} else if errors.Is(err, store.ErrAlreadyInitialized) {
			status, code = http.StatusConflict, "ALREADY_INITIALIZED"
		}
		writeError(w, status, code, err)
		return
	}
	s.setSessionCookie(w, result)
	writeJSON(w, http.StatusCreated, map[string]any{"data": authStatusResponse{Authenticated: true, Username: result.Username, CSRFToken: result.CSRFToken}})
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !sameOrigin(r) {
		writeError(w, http.StatusForbidden, "ORIGIN_REJECTED", errors.New("request origin is not allowed"))
		return
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	ipKey, accountKey := loginKeys(r, request.Username)
	ipAllowed, ipRetry := s.ipLimiter.Allow(ipKey)
	accountAllowed, accountRetry := s.accountLimiter.Allow(accountKey)
	if !ipAllowed || !accountAllowed {
		retryAfter := max(ipRetry, accountRetry)
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", errors.New("too many login attempts; try again later"))
		return
	}
	result, err := s.auth.Login(r.Context(), request.Username, request.Password)
	if err != nil {
		s.ipLimiter.Failure(ipKey)
		s.accountLimiter.Failure(accountKey)
		if errors.Is(err, authsvc.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", err)
			return
		}
		writeError(w, http.StatusInternalServerError, "LOGIN_FAILED", err)
		return
	}
	s.ipLimiter.Success(ipKey)
	s.accountLimiter.Success(accountKey)
	s.setSessionCookie(w, result)
	writeJSON(w, http.StatusOK, map[string]any{"data": authStatusResponse{Authenticated: true, Username: result.Username, CSRFToken: result.CSRFToken}})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if cookie, err := r.Cookie(s.auth.CookieName()); err == nil {
		_ = s.auth.Logout(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: s.auth.CookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.auth.SecureCookie(), SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, result authsvc.Result) {
	http.SetCookie(w, &http.Cookie{Name: s.auth.CookieName(), Value: result.SessionToken, Path: "/", HttpOnly: true, Secure: s.auth.SecureCookie(), SameSite: http.SameSiteStrictMode, MaxAge: int(s.auth.SessionTTL().Seconds()), Expires: result.ExpiresAt})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC(), "version": "0.2.0"})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.Overview(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": result, "warning": publicError(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.Projects(r.Context())
	if err != nil && len(result) == 0 {
		writeError(w, http.StatusServiceUnavailable, "DISCOVERY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result, "warning": optionalError(err)})
}

func (s *Server) projectAction(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Action  string `json:"action"`
		Service string `json:"service"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	output, err := s.service.ProjectAction(r.Context(), r.PathValue("key"), request.Action, request.Service)
	if err != nil {
		writeError(w, http.StatusBadRequest, "COMPOSE_ACTION_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": output})
}

func (s *Server) projectLogs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	output, err := s.service.ProjectLogs(r.Context(), r.PathValue("key"), r.URL.Query().Get("service"), tail)
	if err != nil {
		writeError(w, http.StatusBadRequest, "LOGS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": output})
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.File(r.Context(), r.PathValue("key"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "FILE_READ_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) validateFile(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	output, diff, err := s.service.ValidateFile(r.Context(), r.PathValue("key"), request.Content)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "COMPOSE_INVALID", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "output": output, "diff": diff})
}

func (s *Server) saveFile(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Content string `json:"content"`
		BaseSHA string `json:"baseSha"`
		Apply   bool   `json:"apply"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := s.service.SaveFile(r.Context(), r.PathValue("key"), request.Content, request.BaseSHA, request.Apply)
	if err != nil {
		status, code := http.StatusUnprocessableEntity, "COMPOSE_SAVE_FAILED"
		if errors.Is(err, compose.ErrConflict) {
			status, code = http.StatusConflict, "FILE_CONFLICT"
		}
		writeError(w, status, code, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) versions(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.Versions(r.Context(), r.PathValue("key"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VERSIONS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_VERSION", errors.New("invalid version id"))
		return
	}
	var request struct {
		BaseSHA string `json:"baseSha"`
		Apply   bool   `json:"apply"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := s.service.Restore(r.Context(), r.PathValue("key"), id, request.BaseSHA, request.Apply)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "RESTORE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) containers(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.Containers(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "DOCKER_UNAVAILABLE", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) containerAction(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := s.service.ContainerAction(r.Context(), r.PathValue("id"), request.Action); err != nil {
		writeError(w, http.StatusBadRequest, "CONTAINER_ACTION_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) inspectContainer(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.InspectContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "CONTAINER_INSPECT_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	result, err := s.service.ContainerLogs(r.Context(), r.PathValue("id"), tail)
	if err != nil {
		writeError(w, http.StatusBadRequest, "CONTAINER_LOGS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": result})
}

func (s *Server) images(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.Images(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "IMAGES_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) checkImageUpdates(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.CheckImageUpdates(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "UPDATE_CHECK_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) deleteImage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("confirm") != "true" {
		writeError(w, http.StatusBadRequest, "CONFIRMATION_REQUIRED", errors.New("explicit confirmation is required"))
		return
	}
	if err := s.service.DeleteImage(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusConflict, "IMAGE_REFERENCED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updates(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.Updates(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "UPDATES_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) runUpdate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Project string `json:"project"`
		Service string `json:"service"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := s.service.RunUpdate(r.Context(), request.Project, request.Service)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "UPDATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	result, err := s.service.Settings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SETTINGS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	values := map[string]any{}
	if !decodeJSON(w, r, &values) {
		return
	}
	if err := s.service.PutSettings(r.Context(), values); err != nil {
		writeError(w, http.StatusInternalServerError, "SETTINGS_SAVE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", errors.New("request must contain a single JSON value"))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string, err error) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": publicError(err)}})
}

func publicError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}

func optionalError(err error) any {
	if err == nil {
		return nil
	}
	return publicError(err)
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	publicPaths := map[string]bool{
		"/api/v1/health":      true,
		"/api/v1/auth/status": true,
		"/api/v1/auth/setup":  true,
		"/api/v1/auth/login":  true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") || publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(s.auth.CookieName())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", errors.New("authentication required"))
			return
		}
		session, err := s.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			http.SetCookie(w, &http.Cookie{Name: s.auth.CookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.auth.SecureCookie(), SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", errors.New("session expired or invalid"))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && !s.auth.ValidCSRF(session, r.Header.Get("X-CSRF-Token")) {
			writeError(w, http.StatusForbidden, "CSRF_FAILED", errors.New("CSRF token is missing or invalid"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func loginKeys(r *http.Request, username string) (string, string) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host, host + "\x00" + strings.ToLower(strings.TrimSpace(username))
}

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
		if s.auth.SecureCookie() {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				s.logger.Error("panic recovered", "error", fmt.Sprint(value))
				writeError(w, http.StatusInternalServerError, "INTERNAL", errors.New("internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}
