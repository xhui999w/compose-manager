package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testPassword = "correct-horse-battery"

func testSecret() []byte { return []byte("0123456789abcdef0123456789abcdef") }

func newTestAuthenticator(t *testing.T, clock func() time.Time) *Authenticator {
	t.Helper()
	instance, err := New(Config{Username: "admin", Password: testPassword, Secret: testSecret(), Now: clock})
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}
	return instance
}

// 部署文档推荐用 CM_AUTH_PASSWORD_HASH 而非明文口令，这条路径必须真的能登录。
func TestAuthenticateWithPrecomputedHash(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword() 失败: %v", err)
	}
	instance, err := New(Config{Username: "admin", PasswordHash: hash, Secret: testSecret()})
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}
	if !instance.Enabled() {
		t.Fatal("提供哈希后应启用认证")
	}
	if err := instance.Authenticate("1.1.1.1", "admin", testPassword); err != nil {
		t.Fatalf("哈希模式登录应成功，得到 %v", err)
	}
	if err := instance.Authenticate("1.1.1.1", "admin", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("期望 ErrInvalidCredentials，得到 %v", err)
	}
}

// 哈希优先于明文：两者同时配置时，以哈希为准，明文不应被采纳。
func TestPasswordHashTakesPrecedence(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword() 失败: %v", err)
	}
	instance, err := New(Config{Username: "admin", Password: "another-password-entirely", PasswordHash: hash, Secret: testSecret()})
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}
	if err := instance.Authenticate("1.1.1.1", "admin", testPassword); err != nil {
		t.Fatalf("应以哈希为准，得到 %v", err)
	}
	if err := instance.Authenticate("1.1.1.1", "admin", "another-password-entirely"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("明文口令不应被采纳，得到 %v", err)
	}
}

func TestNewDisabledWithoutPassword(t *testing.T) {
	instance, err := New(Config{Secret: testSecret()})
	if err != nil {
		t.Fatalf("未配置口令时不应报错，得到 %v", err)
	}
	if instance.Enabled() {
		t.Fatal("未配置口令时 Enabled() 应为 false")
	}
	if err := instance.Authenticate("1.2.3.4", "admin", "whatever"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("期望 ErrNotConfigured，得到 %v", err)
	}
	// 未启用时令牌校验必须放行，否则降级模式下所有请求都会被拦。
	if err := instance.Verify("随便什么值"); err != nil {
		t.Fatalf("未启用时应放行，得到 %v", err)
	}
	if _, _, err := instance.Issue(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("未启用时 Issue() 应返回 ErrNotConfigured，得到 %v", err)
	}
}

func TestNewRejectsShortPassword(t *testing.T) {
	if _, err := New(Config{Password: "short", Secret: testSecret()}); err == nil {
		t.Fatal("过短的口令应被拒绝")
	}
}

func TestNewRejectsInvalidHash(t *testing.T) {
	if _, err := New(Config{PasswordHash: "not-a-bcrypt-hash", Secret: testSecret()}); err == nil {
		t.Fatal("非法 bcrypt 哈希应被拒绝")
	}
}

func TestNewRequiresSecretWhenEnabled(t *testing.T) {
	if _, err := New(Config{Password: testPassword, Secret: []byte("too-short")}); err == nil {
		t.Fatal("启用认证但密钥过短时应被拒绝")
	}
}

func TestNewDefaultsUsernameAndTTL(t *testing.T) {
	instance, err := New(Config{Password: testPassword, Secret: testSecret()})
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}
	if instance.Username() != "admin" {
		t.Fatalf("默认用户名应为 admin，得到 %q", instance.Username())
	}
	if instance.TTL() != DefaultTTL {
		t.Fatalf("默认 TTL 应为 %v，得到 %v", DefaultTTL, instance.TTL())
	}
}

func TestAuthenticateSuccessAndFailure(t *testing.T) {
	instance := newTestAuthenticator(t, nil)

	if err := instance.Authenticate("1.1.1.1", "admin", testPassword); err != nil {
		t.Fatalf("正确凭据应通过，得到 %v", err)
	}
	if err := instance.Authenticate("1.1.1.1", "admin", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("错误口令应返回 ErrInvalidCredentials，得到 %v", err)
	}
	if err := instance.Authenticate("1.1.1.1", "root", testPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("错误用户名应返回 ErrInvalidCredentials，得到 %v", err)
	}
	// 成功一次之后失败计数应清零：上面第二次失败后，再成功一次即可复位。
	if err := instance.Authenticate("1.1.1.1", "admin", testPassword); err != nil {
		t.Fatalf("复位后正确凭据应通过，得到 %v", err)
	}
}

func TestAuthenticateRateLimited(t *testing.T) {
	instance := newTestAuthenticator(t, nil)
	for attempt := 0; attempt < maxLoginFailures; attempt++ {
		if err := instance.Authenticate("9.9.9.9", "admin", "bad-password"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("第 %d 次失败应返回 ErrInvalidCredentials，得到 %v", attempt+1, err)
		}
	}
	if err := instance.Authenticate("9.9.9.9", "admin", testPassword); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("超过失败上限后即使口令正确也应被限流，得到 %v", err)
	}
	// 其它客户端不受影响。
	if err := instance.Authenticate("8.8.8.8", "admin", testPassword); err != nil {
		t.Fatalf("其它客户端不应被牵连，得到 %v", err)
	}
}

func TestRateLimitWindowExpires(t *testing.T) {
	current := time.Now()
	instance := newTestAuthenticator(t, func() time.Time { return current })
	for attempt := 0; attempt < maxLoginFailures; attempt++ {
		_ = instance.Authenticate("9.9.9.9", "admin", "bad-password")
	}
	if err := instance.Authenticate("9.9.9.9", "admin", testPassword); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("窗口内应被限流，得到 %v", err)
	}
	current = current.Add(failureWindow + time.Second)
	if err := instance.Authenticate("9.9.9.9", "admin", testPassword); err != nil {
		t.Fatalf("窗口过期后应恢复，得到 %v", err)
	}
}

func TestIssueAndVerify(t *testing.T) {
	current := time.Now()
	instance := newTestAuthenticator(t, func() time.Time { return current })

	token, expires, err := instance.Issue()
	if err != nil {
		t.Fatalf("Issue() 失败: %v", err)
	}
	if !expires.Equal(current.Add(DefaultTTL)) {
		t.Fatalf("过期时间应为 %v，得到 %v", current.Add(DefaultTTL), expires)
	}
	if err := instance.Verify(token); err != nil {
		t.Fatalf("刚签发的 token 应通过校验，得到 %v", err)
	}

	current = current.Add(DefaultTTL + time.Second)
	if err := instance.Verify(token); !errors.Is(err, ErrExpiredSession) {
		t.Fatalf("过期 token 应返回 ErrExpiredSession，得到 %v", err)
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	instance := newTestAuthenticator(t, nil)
	token, _, err := instance.Issue()
	if err != nil {
		t.Fatalf("Issue() 失败: %v", err)
	}
	parts := strings.Split(token, ".")

	tampered := []string{
		"",
		"garbage",
		"v1",
		"v2." + parts[1] + "." + parts[2],
		"v1.not-a-number." + parts[2],
		"v1." + parts[1] + ".not-base64!!",
		// 把过期时间往后改，签名对不上
		"v1." + strings.TrimSpace("9999999999") + "." + parts[2],
	}
	for _, candidate := range tampered {
		if err := instance.Verify(candidate); err == nil {
			t.Fatalf("被篡改的 token %q 不应通过校验", candidate)
		}
	}

	// 换一个密钥后，旧 token 必须失效。
	other, err := New(Config{Username: "admin", Password: testPassword, Secret: []byte("ffffffffffffffffffffffffffffffff")})
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}
	if err := other.Verify(token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("换密钥后旧 token 应失效，得到 %v", err)
	}

	// 换用户名后，旧 token 也必须失效。
	renamed, err := New(Config{Username: "someone-else", Password: testPassword, Secret: testSecret()})
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}
	if err := renamed.Verify(token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("换用户名后旧 token 应失效，得到 %v", err)
	}
}

func TestParseSecret(t *testing.T) {
	hexValue := strings.Repeat("ab", 32)
	decoded, err := ParseSecret(hexValue)
	if err != nil {
		t.Fatalf("64 位十六进制应可解析，得到 %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("十六进制应解码为 32 字节，得到 %d", len(decoded))
	}

	raw, err := ParseSecret("a-plain-secret-value")
	if err != nil {
		t.Fatalf("原始字符串应可解析，得到 %v", err)
	}
	if string(raw) != "a-plain-secret-value" {
		t.Fatalf("原始字符串应原样返回，得到 %q", string(raw))
	}

	if value, err := ParseSecret("   "); err != nil || value != nil {
		t.Fatalf("空白输入应返回 nil, nil，得到 %v, %v", value, err)
	}
	if _, err := ParseSecret("short"); err == nil {
		t.Fatal("过短的密钥应被拒绝")
	}
}

func TestLoadOrCreateSecretPersistsAndReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".session-secret")

	first, reused, warning := LoadOrCreateSecret(path)
	if warning != nil {
		t.Fatalf("首次生成不应有警告，得到 %v", warning)
	}
	if reused {
		t.Fatal("首次生成时 reused 应为 false")
	}
	if len(first) < MinSecretLength {
		t.Fatalf("生成的密钥过短: %d", len(first))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("密钥应已落盘: %v", err)
	}

	second, reused, warning := LoadOrCreateSecret(path)
	if warning != nil {
		t.Fatalf("二次读取不应有警告，得到 %v", warning)
	}
	if !reused {
		t.Fatal("二次读取时 reused 应为 true")
	}
	if string(second) != string(first) {
		t.Fatal("二次读取应拿到同一个密钥")
	}
}

func TestLoadOrCreateSecretFallsBackWhenUnwritable(t *testing.T) {
	// 用一个「父路径是文件」的位置，确保落盘一定失败。
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("准备失败: %v", err)
	}
	secret, _, warning := LoadOrCreateSecret(filepath.Join(blocker, "sub", ".session-secret"))
	if warning == nil {
		t.Fatal("落盘失败时应返回警告")
	}
	if len(secret) < MinSecretLength {
		t.Fatalf("落盘失败时仍应返回可用的内存密钥，得到 %d 字节", len(secret))
	}
}

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword() 失败: %v", err)
	}
	instance, err := New(Config{PasswordHash: hash, Secret: testSecret()})
	if err != nil {
		t.Fatalf("用生成的哈希构造应成功，得到 %v", err)
	}
	if err := instance.Authenticate("1.1.1.1", "admin", testPassword); err != nil {
		t.Fatalf("生成的哈希应能校验通过，得到 %v", err)
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("过短口令应被拒绝")
	}
}

func TestSetAndClearCookie(t *testing.T) {
	instance, err := New(Config{Password: testPassword, Secret: testSecret(), CookieSecure: true, TTL: time.Hour})
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}

	recorder := httptest.NewRecorder()
	instance.SetCookie(recorder, "token-value", time.Now().Add(time.Hour))
	cookie := readCookie(t, recorder)
	if cookie.Name != CookieName || cookie.Value != "token-value" {
		t.Fatalf("Cookie 名/值不对: %+v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure {
		t.Fatal("会话 Cookie 必须是 HttpOnly + Secure")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite 应为 Lax，得到 %v", cookie.SameSite)
	}
	if cookie.MaxAge != int(time.Hour/time.Second) {
		t.Fatalf("MaxAge 应为 3600，得到 %d", cookie.MaxAge)
	}

	cleared := httptest.NewRecorder()
	instance.ClearCookie(cleared)
	if got := readCookie(t, cleared); got.MaxAge != -1 || got.Value != "" {
		t.Fatalf("清理 Cookie 应立即失效，得到 %+v", got)
	}
}

func TestTokenFromRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	if TokenFromRequest(request) != "" {
		t.Fatal("没有 Cookie 时应返回空串")
	}
	request.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})
	if TokenFromRequest(request) != "abc" {
		t.Fatal("应取出 Cookie 值")
	}
}

func TestClientKey(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "192.168.1.20:54321"
	if got := ClientKey(request); got != "192.168.1.20" {
		t.Fatalf("应剥离端口，得到 %q", got)
	}
	// 伪造的 X-Forwarded-For 不得影响限流标识。
	request.Header.Set("X-Forwarded-For", "10.0.0.9")
	if got := ClientKey(request); got != "192.168.1.20" {
		t.Fatalf("不应信任 X-Forwarded-For，得到 %q", got)
	}
	request.RemoteAddr = "没有端口"
	if got := ClientKey(request); got != "没有端口" {
		t.Fatalf("无法解析时原样返回，得到 %q", got)
	}
}

func readCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("响应里没有 Set-Cookie")
	}
	return cookies[0]
}
