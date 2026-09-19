package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/auth"
)

const testPassword = "correct-horse-battery"

// newAuthTestServer 构造一个只关心鉴权的 Server。
//
// 这些用例只打 auth/health/static 三类不依赖 service 的路由，
// 因此不装配 app.Service，避免为安全边界测试引入整套 Docker/Compose 依赖。
func newAuthTestServer(t *testing.T, enabled bool) *Server {
	t.Helper()
	cfg := auth.Config{
		Username: "admin",
		Secret:   []byte("0123456789abcdef0123456789abcdef"),
		TTL:      time.Hour,
	}
	if enabled {
		cfg.Password = testPassword
	}
	authenticator, err := auth.New(cfg)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	server := &Server{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		mux:    http.NewServeMux(),
		authn:  authenticator,
	}
	server.routes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "spa")
	}))
	return server
}

func doRequest(handler http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	return payload.Error.Code
}

// 豁免清单是安全边界，任何一条规则被误删都会导致「要么面板打不开，要么裸奔」。
func TestAuthExemptPaths(t *testing.T) {
	exempt := []string{
		"/",
		"/index.html",
		"/assets/index-abc123.js",
		"/some/spa/route",
		"/api/v1/health",
		"/api/v1/auth/login",
		"/api/v1/auth/logout",
		"/api/v1/auth/session",
	}
	for _, path := range exempt {
		if !authExempt(path) {
			t.Errorf("authExempt(%q) = false, want true", path)
		}
	}
	protected := []string{
		"/api/v1/overview",
		"/api/v1/compose/projects",
		"/api/v1/containers",
		"/api/v1/images",
		"/api/v1/settings",
		"/api/v1/updates/run",
		"/api/v1/auth/session/",
		"/api/v2/health",
	}
	for _, path := range protected {
		if authExempt(path) {
			t.Errorf("authExempt(%q) = true, want false", path)
		}
	}
}

func TestAuthGuardBlocksProtectedAPIWithoutSession(t *testing.T) {
	server := newAuthTestServer(t, true)
	handler := server.authGuard(server.mux)

	for _, path := range []string{"/api/v1/overview", "/api/v1/compose/projects", "/api/v1/settings"} {
		recorder := doRequest(handler, http.MethodGet, path, "")
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, recorder.Code)
		}
		if code := errorCode(t, recorder); code != "UNAUTHENTICATED" {
			t.Errorf("%s: code = %q, want UNAUTHENTICATED", path, code)
		}
	}
}

func TestAuthGuardRejectsTamperedAndMissingSession(t *testing.T) {
	server := newAuthTestServer(t, true)
	handler := server.authGuard(server.mux)

	cases := []struct {
		name   string
		cookie *http.Cookie
	}{
		{"missing", nil},
		{"empty", &http.Cookie{Name: auth.CookieName, Value: ""}},
		{"garbage", &http.Cookie{Name: auth.CookieName, Value: "not-a-token"}},
		{"unsigned-token", &http.Cookie{Name: auth.CookieName, Value: "v1.99999999999.AAAA"}},
		{"expired", &http.Cookie{Name: auth.CookieName, Value: "v1.1.AAAA.AAAA"}},
	}
	for _, item := range cases {
		recorder := doRequest(handler, http.MethodGet, "/api/v1/overview", "", item.cookie)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", item.name, recorder.Code)
		}
	}
}

// 静态资源匿名可达是硬要求：否则浏览器拿不到前端代码，登录页永远渲染不出来。
func TestAuthGuardAllowsStaticAssets(t *testing.T) {
	server := newAuthTestServer(t, true)
	handler := server.authGuard(server.mux)

	for _, path := range []string{"/", "/index.html", "/assets/app.js"} {
		recorder := doRequest(handler, http.MethodGet, path, "")
		if recorder.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, recorder.Code)
		}
		if recorder.Body.String() != "spa" {
			t.Errorf("%s: body = %q, want spa", path, recorder.Body.String())
		}
	}
}

func TestAuthGuardAllowsExemptAuthEndpoints(t *testing.T) {
	server := newAuthTestServer(t, true)
	handler := server.authGuard(server.mux)

	// 无 Cookie 的会话探测必须得到 200（而不是被守卫拦成 401），前端才能区分「未登录」与「接口故障」。
	recorder := doRequest(handler, http.MethodGet, "/api/v1/auth/session", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("session probe: status = %d, want 200", recorder.Code)
	}

	// 登录接口必须匿名可达，否则第一次登录就被自己挡住。
	recorder = doRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"wrong-password"}`)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("login: status = %d, want 401", recorder.Code)
	}
	if code := errorCode(t, recorder); code != "INVALID_CREDENTIALS" {
		t.Fatalf("login: code = %q, want INVALID_CREDENTIALS（而不是 UNAUTHENTICATED）", code)
	}

	// 登出幂等：没有会话也应能清 Cookie。
	recorder = doRequest(handler, http.MethodPost, "/api/v1/auth/logout", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("logout: status = %d, want 200", recorder.Code)
	}
}

func TestLoginIssuesSessionAcceptedByGuard(t *testing.T) {
	server := newAuthTestServer(t, true)
	handler := server.authGuard(server.mux)

	recorder := doRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+testPassword+`"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login: status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var session *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == auth.CookieName {
			session = cookie
		}
	}
	if session == nil {
		t.Fatalf("login 未下发 %s Cookie", auth.CookieName)
	}
	if !session.HttpOnly {
		t.Error("会话 Cookie 必须为 HttpOnly")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", session.SameSite)
	}

	// 拿到会话后，探测接口应回报已登录。
	recorder = doRequest(handler, http.MethodGet, "/api/v1/auth/session", "", session)
	if recorder.Code != http.StatusOK {
		t.Fatalf("session: status = %d, want 200", recorder.Code)
	}
	var payload struct {
		Data struct {
			Required      bool   `json:"required"`
			Authenticated bool   `json:"authenticated"`
			Username      string `json:"username"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if !payload.Data.Required || !payload.Data.Authenticated || payload.Data.Username != "admin" {
		t.Fatalf("session = %+v, want required+authenticated+admin", payload.Data)
	}

	// 该 Cookie 必须能真正通过守卫，否则登录等于白登。
	recorder = doRequest(handler, http.MethodGet, "/api/v1/auth/session", "", session)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reused session: status = %d, want 200", recorder.Code)
	}
}

func TestLoginRejectsWrongCredentials(t *testing.T) {
	server := newAuthTestServer(t, true)
	handler := server.authGuard(server.mux)

	recorder := doRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"definitely-wrong"}`)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if code := errorCode(t, recorder); code != "INVALID_CREDENTIALS" {
		t.Errorf("code = %q, want INVALID_CREDENTIALS", code)
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Error("登录失败不得下发会话 Cookie")
	}

	// 用户名错误与口令错误必须返回同一个错误码，避免枚举用户名。
	recorder = doRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"username":"root","password":"`+testPassword+`"}`)
	if code := errorCode(t, recorder); code != "INVALID_CREDENTIALS" {
		t.Errorf("错误用户名: code = %q, want INVALID_CREDENTIALS", code)
	}
}

func TestLoginRateLimited(t *testing.T) {
	server := newAuthTestServer(t, true)
	handler := server.authGuard(server.mux)

	for attempt := 0; attempt < 5; attempt++ {
		recorder := doRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"bad-password"}`)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", attempt, recorder.Code)
		}
	}
	// 第 6 次即使口令正确也应被限流挡住。
	recorder := doRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+testPassword+`"}`)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if code := errorCode(t, recorder); code != "TOO_MANY_ATTEMPTS" {
		t.Errorf("code = %q, want TOO_MANY_ATTEMPTS", code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	server := newAuthTestServer(t, true)
	handler := server.authGuard(server.mux)

	login := doRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+testPassword+`"}`)
	var session *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == auth.CookieName {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("登录未下发会话 Cookie")
	}

	recorder := doRequest(handler, http.MethodPost, "/api/v1/auth/logout", "", session)
	if recorder.Code != http.StatusOK {
		t.Fatalf("logout: status = %d, want 200", recorder.Code)
	}
	var cleared *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == auth.CookieName {
			cleared = cookie
		}
	}
	if cleared == nil || cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Fatalf("logout 未清除 Cookie: %+v", cleared)
	}
}

// 无状态会话的固有代价：登出只负责让浏览器丢掉 Cookie，服务端不保存会话表，
// 因此已被复制的 token 在有效期内依然可验证通过。
//
// 本用例把这个已知行为钉死：如果将来引入服务端吊销（黑名单 / 会话表），
// 这里会失败，从而强制做出一次有意识的取舍，而不是悄悄改变安全语义。
// 当前缓解手段是「更换 CM_SESSION_SECRET 即吊销全部会话」。
func TestLogoutOnlyClearsCookieNotServerSideToken(t *testing.T) {
	server := newAuthTestServer(t, true)
	handler := server.authGuard(server.mux)

	login := doRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+testPassword+`"}`)
	var session *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == auth.CookieName {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("登录未下发会话 Cookie")
	}

	if recorder := doRequest(handler, http.MethodPost, "/api/v1/auth/logout", "", session); recorder.Code != http.StatusOK {
		t.Fatalf("logout: status = %d, want 200", recorder.Code)
	}

	// 浏览器已丢弃 Cookie，但持有一份副本的客户端仍能通过守卫——这是预期行为，不是缺陷。
	recorder := doRequest(handler, http.MethodGet, "/api/v1/auth/session", "", session)
	if recorder.Code != http.StatusOK {
		t.Fatalf("session: status = %d, want 200", recorder.Code)
	}
	var payload struct {
		Data struct {
			Authenticated bool `json:"authenticated"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if !payload.Data.Authenticated {
		t.Fatal("无状态会话下，登出后旧 token 仍应可验证（如需真正吊销请更换 CM_SESSION_SECRET）")
	}
}

// 未配置口令时必须整体降级为不鉴权，否则既有部署升级后会把自己锁在门外。
func TestAuthDisabledFallsOpen(t *testing.T) {
	server := newAuthTestServer(t, false)
	handler := server.authGuard(server.mux)

	recorder := doRequest(handler, http.MethodGet, "/api/v1/auth/session", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("session: status = %d, want 200", recorder.Code)
	}
	var payload struct {
		Data struct {
			Required      bool `json:"required"`
			Authenticated bool `json:"authenticated"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if payload.Data.Required || !payload.Data.Authenticated {
		t.Fatalf("session = %+v, want required=false authenticated=true", payload.Data)
	}

	// 未启用认证时不允许调用登录接口，避免误配后产生「假登录」。
	recorder = doRequest(handler, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+testPassword+`"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("login: status = %d, want 400", recorder.Code)
	}
	if code := errorCode(t, recorder); code != "AUTH_DISABLED" {
		t.Errorf("code = %q, want AUTH_DISABLED", code)
	}
}
