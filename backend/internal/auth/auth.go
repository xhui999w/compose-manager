// Package auth 为单机面板提供登录认证。
//
// 设计取舍：
//   - 口令只以 bcrypt 哈希形式保存在内存中（可来自 CM_AUTH_PASSWORD 或 CM_AUTH_PASSWORD_HASH）。
//   - 会话使用 HMAC-SHA256 签名的**无状态 Cookie**，服务端不存会话表：容器重启不掉登录态，
//     也无需新增数据库表。代价是无法单独吊销某个会话——更换 SessionSecret 即吊销全部。
//   - 登录失败按客户端做限流，避免面板被放在内网时被弱口令爆破。
//   - 未配置口令时整体降级为「不鉴权」，由调用方负责打印显式警告，以保持既有部署的兼容性。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// CookieName 是会话 Cookie 名。
	CookieName = "cm_session"
	// tokenVersion 参与签名计算，便于以后平滑更换 token 格式。
	tokenVersion = "v1"
	// MinPasswordLength 是明文口令的最小长度。
	MinPasswordLength = 8
	// MinSecretLength 是会话签名密钥的最小字节数。
	MinSecretLength = 16
	// DefaultTTL 是会话默认有效期。
	DefaultTTL = 12 * time.Hour

	bcryptCost       = 12
	maxLoginFailures = 5
	failureWindow    = 5 * time.Minute
)

var (
	// ErrInvalidCredentials 表示用户名或口令不正确。
	ErrInvalidCredentials = errors.New("用户名或口令不正确")
	// ErrTooManyAttempts 表示该客户端短时间内失败次数过多。
	ErrTooManyAttempts = errors.New("登录失败次数过多，请稍后再试")
	// ErrNotConfigured 表示服务端没有配置登录口令。
	ErrNotConfigured = errors.New("服务端未配置登录口令")
	// ErrInvalidSession 表示会话 Cookie 无效或已被篡改。
	ErrInvalidSession = errors.New("会话无效，请重新登录")
	// ErrExpiredSession 表示会话已过期。
	ErrExpiredSession = errors.New("会话已过期，请重新登录")
)

// Config 是构造 Authenticator 所需的配置。
type Config struct {
	Username     string
	Password     string
	PasswordHash string
	Secret       []byte
	TTL          time.Duration
	CookieSecure bool
	// Now 可注入时钟，仅用于测试。
	Now func() time.Time
}

// Authenticator 负责校验口令、签发与校验会话 Cookie。
type Authenticator struct {
	disabled     bool
	username     string
	passwordHash []byte
	secret       []byte
	ttl          time.Duration
	cookieSecure bool
	now          func() time.Time
	limiter      *limiter
}

// New 构造 Authenticator。未提供口令与哈希时返回一个「未启用」的实例（不会报错）。
func New(cfg Config) (*Authenticator, error) {
	instance := &Authenticator{
		username:     strings.TrimSpace(cfg.Username),
		ttl:          cfg.TTL,
		cookieSecure: cfg.CookieSecure,
		now:          cfg.Now,
		limiter:      newLimiter(maxLoginFailures, failureWindow),
	}
	if instance.username == "" {
		instance.username = "admin"
	}
	if instance.ttl <= 0 {
		instance.ttl = DefaultTTL
	}
	if instance.now == nil {
		instance.now = time.Now
	}

	hash := strings.TrimSpace(cfg.PasswordHash)
	switch {
	case hash != "":
		if _, err := bcrypt.Cost([]byte(hash)); err != nil {
			return nil, fmt.Errorf("CM_AUTH_PASSWORD_HASH 不是合法的 bcrypt 哈希: %w", err)
		}
		instance.passwordHash = []byte(hash)
	case cfg.Password != "":
		if len(cfg.Password) < MinPasswordLength {
			return nil, fmt.Errorf("CM_AUTH_PASSWORD 至少需要 %d 个字符", MinPasswordLength)
		}
		generated, err := bcrypt.GenerateFromPassword([]byte(cfg.Password), bcryptCost)
		if err != nil {
			return nil, fmt.Errorf("生成口令哈希失败: %w", err)
		}
		instance.passwordHash = generated
	default:
		instance.disabled = true
		return instance, nil
	}

	if len(cfg.Secret) < MinSecretLength {
		return nil, fmt.Errorf("会话签名密钥至少需要 %d 字节", MinSecretLength)
	}
	instance.secret = append([]byte(nil), cfg.Secret...)
	return instance, nil
}

// Enabled 表示是否启用了登录认证。为 false 时所有请求直接放行。
func (a *Authenticator) Enabled() bool { return !a.disabled }

// Username 返回配置的登录用户名。
func (a *Authenticator) Username() string { return a.username }

// TTL 返回会话有效期。
func (a *Authenticator) TTL() time.Duration { return a.ttl }

// Authenticate 校验用户名与口令；key 用于失败限流（通常传客户端 IP）。
//
// 无论用户名是否正确都会执行一次 bcrypt 校验，避免通过响应耗时区分「用户不存在」与「口令错误」。
func (a *Authenticator) Authenticate(key, username, password string) error {
	if a.disabled {
		return ErrNotConfigured
	}
	switch err := a.limiter.allow(key, a.now()); {
	case errors.Is(err, ErrTooManyAttempts):
		return ErrTooManyAttempts
	case err != nil:
		return err
	}
	userMatches := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(a.username)) == 1
	passwordMatches := bcrypt.CompareHashAndPassword(a.passwordHash, []byte(password)) == nil
	if !userMatches || !passwordMatches {
		a.limiter.fail(key, a.now())
		return ErrInvalidCredentials
	}
	a.limiter.reset(key)
	return nil
}

// Issue 签发新的会话 token 及其过期时间。
func (a *Authenticator) Issue() (string, time.Time, error) {
	if a.disabled {
		return "", time.Time{}, ErrNotConfigured
	}
	expires := a.now().Add(a.ttl)
	return a.sign(strconv.FormatInt(expires.Unix(), 10)), expires, nil
}

// Verify 校验会话 token 的签名与有效期。
func (a *Authenticator) Verify(token string) error {
	if a.disabled {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] != tokenVersion {
		return ErrInvalidSession
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return ErrInvalidSession
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ErrInvalidSession
	}
	if !hmac.Equal(provided, a.mac(parts[1])) {
		return ErrInvalidSession
	}
	if !a.now().Before(time.Unix(expiresAt, 0)) {
		return ErrExpiredSession
	}
	return nil
}

func (a *Authenticator) sign(expires string) string {
	return tokenVersion + "." + expires + "." + base64.RawURLEncoding.EncodeToString(a.mac(expires))
}

// mac 同时绑定用户名，用户名变更后旧会话自然失效。
func (a *Authenticator) mac(expires string) []byte {
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(tokenVersion + "|" + a.username + "|" + expires))
	return mac.Sum(nil)
}

// SetCookie 写入会话 Cookie。
//
// SameSite 固定为 Lax：所有状态变更接口都是 POST/PUT/DELETE，Lax 已能挡住跨站携带 Cookie 的写请求，
// 同时又不会像 Strict 那样影响从书签或外链直接打开面板。
func (a *Authenticator) SetCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(a.ttl / time.Second),
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie 让浏览器立即丢弃会话 Cookie。
func (a *Authenticator) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// TokenFromRequest 从请求的 Cookie 中取出会话 token，缺失时返回空串。
func TokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ClientKey 返回用于限流的客户端标识。
//
// 只信任 TCP 对端地址：若信任 X-Forwarded-For，未受控的客户端就能伪造头绕过限流。
// 代价是面板放在反向代理后面时所有请求共用同一个桶——宁可误伤，也不要让限流形同虚设。
func ClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// HashPassword 生成 bcrypt 哈希，用于写入 CM_AUTH_PASSWORD_HASH。
func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", fmt.Errorf("口令至少需要 %d 个字符", MinPasswordLength)
	}
	generated, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("生成口令哈希失败: %w", err)
	}
	return string(generated), nil
}

// ParseSecret 解析会话密钥：64 位十六进制按 hex 解码，否则按原始字节处理。
func ParseSecret(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed) == 64 {
		if decoded, err := hex.DecodeString(trimmed); err == nil {
			return decoded, nil
		}
	}
	raw := []byte(trimmed)
	if len(raw) < MinSecretLength {
		return nil, fmt.Errorf("CM_SESSION_SECRET 至少需要 %d 字节（或 64 位十六进制）", MinSecretLength)
	}
	return raw, nil
}

// LoadOrCreateSecret 读取或生成会话签名密钥。
//
// 自动生成的密钥会写入 path（0600），这样容器重启后既有登录态仍然有效。
// 落盘失败时退化为内存密钥并把原因作为 warning 返回，不阻断启动。
func LoadOrCreateSecret(path string) (secret []byte, reused bool, warning error) {
	if path != "" {
		if existing, err := readSecret(path); err == nil && len(existing) >= MinSecretLength {
			return existing, true, nil
		}
	}
	generated := make([]byte, 32)
	if _, err := rand.Read(generated); err != nil {
		return nil, false, fmt.Errorf("生成会话签名密钥失败: %w", err)
	}
	if path == "" {
		return generated, false, nil
	}
	if err := writeSecret(path, generated); err != nil {
		return generated, false, fmt.Errorf("会话密钥无法落盘，本次使用内存密钥（重启后需要重新登录）: %w", err)
	}
	return generated, false, nil
}

func writeSecret(path string, secret []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(secret)), 0o600)
}

func readSecret(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSecret(string(content))
}

// limiter 是按 key 的失败次数限流器（固定窗口）。
type limiter struct {
	mu      sync.Mutex
	entries map[string]*failureEntry
	max     int
	window  time.Duration
}

type failureEntry struct {
	count int
	reset time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{entries: make(map[string]*failureEntry), max: max, window: window}
}

func (l *limiter) allow(key string, now time.Time) error {
	if key == "" || l.max <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evictLocked(now)
	current, ok := l.entries[key]
	if ok && current.count >= l.max && now.Before(current.reset) {
		return ErrTooManyAttempts
	}
	return nil
}

func (l *limiter) fail(key string, now time.Time) {
	if key == "" || l.max <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evictLocked(now)
	current, ok := l.entries[key]
	if !ok || !now.Before(current.reset) {
		l.entries[key] = &failureEntry{count: 1, reset: now.Add(l.window)}
		return
	}
	current.count++
	current.reset = now.Add(l.window)
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// evictLocked 清理已过窗口的条目，避免长期运行后 map 无限增长。
func (l *limiter) evictLocked(now time.Time) {
	for key, item := range l.entries {
		if !now.Before(item.reset) {
			delete(l.entries, key)
		}
	}
}
