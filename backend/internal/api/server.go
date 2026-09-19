package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/app"
	"github.com/compose-manager/compose-manager/backend/internal/auth"
	"github.com/compose-manager/compose-manager/backend/internal/compose"
)

type Server struct {
	service *app.Service
	logger  *slog.Logger
	mux     *http.ServeMux
	authn   *auth.Authenticator
}

func New(service *app.Service, logger *slog.Logger, static http.Handler, authenticator *auth.Authenticator) http.Handler {
	server := &Server{service: service, logger: logger, mux: http.NewServeMux(), authn: authenticator}
	server.routes(static)
	return server.security(server.recover(server.log(server.authGuard(server.mux))))
}

func (s *Server) routes(static http.Handler) {
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	s.mux.HandleFunc("GET /api/v1/auth/session", s.session)
	s.mux.HandleFunc("GET /api/v1/health", s.health)
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

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.service.Health(r.Context()))
}

// authEnabled 表示面板是否要求登录。
func (s *Server) authEnabled() bool { return s.authn != nil && s.authn.Enabled() }

// login 校验用户名口令并下发会话 Cookie。
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.authEnabled() {
		writeError(w, http.StatusBadRequest, "AUTH_DISABLED", auth.ErrNotConfigured)
		return
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := s.authn.Authenticate(auth.ClientKey(r), request.Username, request.Password); err != nil {
		if errors.Is(err, auth.ErrTooManyAttempts) {
			writeError(w, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", err)
			return
		}
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", err)
		return
	}
	token, expires, err := s.authn.Issue()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SESSION_ISSUE_FAILED", err)
		return
	}
	s.authn.SetCookie(w, token, expires)
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"username":  s.authn.Username(),
		"expiresAt": expires.UTC().Format(time.RFC3339),
	}})
}

// logout 清除会话 Cookie；即使当前没有有效会话也返回成功，保证幂等。
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.authn != nil {
		s.authn.ClearCookie(w)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// session 供前端启动时探测登录态。
//
// 始终返回 200，用 authenticated 字段表达状态：这样前端无需把「未登录」当成接口异常处理。
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.authEnabled() {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"required": false, "authenticated": true, "username": ""}})
		return
	}
	authenticated := s.authn.Verify(auth.TokenFromRequest(r)) == nil
	username := ""
	if authenticated {
		username = s.authn.Username()
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"required": true, "authenticated": authenticated, "username": username}})
}

// authGuard 拦截未携带有效会话的 API 请求。
//
// 只保护 /api/ 前缀：静态资源必须匿名可达，否则浏览器取不到前端代码、也就渲染不出登录页。
// 面板本身不含敏感数据的接口只有 /api/v1/health（供容器健康检查）与 auth 自身三个接口。
func (s *Server) authGuard(next http.Handler) http.Handler {
	if !s.authEnabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if err := s.authn.Verify(auth.TokenFromRequest(r)); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authExempt(path string) bool {
	if !strings.HasPrefix(path, "/api/") {
		return true
	}
	switch path {
	case "/api/v1/health", "/api/v1/auth/login", "/api/v1/auth/logout", "/api/v1/auth/session":
		return true
	default:
		return false
	}
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

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
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
