package config

import (
	"path/filepath"
	"testing"
	"time"
)

// 环境变量名一旦写错，认证会静默退化为「不鉴权」——最危险的失败方式。
// 这些用例把变量名与解析规则钉住。
func TestLoadAuthDefaults(t *testing.T) {
	// 清空相关变量，避免本机环境（或其它用例）影响默认值断言。
	for _, key := range []string{"CM_AUTH_USER", "CM_AUTH_PASSWORD", "CM_AUTH_PASSWORD_HASH", "CM_SESSION_SECRET", "CM_SESSION_SECRET_PATH", "CM_SESSION_TTL", "CM_COOKIE_SECURE", "CM_DATA_DIR"} {
		t.Setenv(key, "")
	}
	cfg := Load()
	if cfg.Auth.Username != "admin" {
		t.Errorf("默认用户名 = %q, want admin", cfg.Auth.Username)
	}
	if cfg.Auth.Password != "" || cfg.Auth.PasswordHash != "" {
		t.Error("未设置环境变量时口令与哈希都应为空（此时不启用登录）")
	}
	if cfg.Auth.SessionSecret != "" {
		t.Error("未设置 CM_SESSION_SECRET 时 SessionSecret 应为空，交由调用方生成")
	}
	if want := filepath.Join(cfg.DataDir, ".session-secret"); cfg.Auth.SessionSecretPath != want {
		t.Errorf("SessionSecretPath = %q, want %q", cfg.Auth.SessionSecretPath, want)
	}
	if cfg.Auth.SessionTTL != 12*time.Hour {
		t.Errorf("SessionTTL = %v, want 12h", cfg.Auth.SessionTTL)
	}
	if cfg.Auth.CookieSecure {
		t.Error("CookieSecure 默认应为 false：直连 http://<内网 IP> 时 Secure Cookie 会被浏览器丢弃")
	}
}

func TestLoadAuthFromEnv(t *testing.T) {
	t.Setenv("CM_AUTH_USER", "xiangwei")
	t.Setenv("CM_AUTH_PASSWORD", "s3cret-passphrase")
	t.Setenv("CM_AUTH_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("CM_SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("CM_SESSION_TTL", "30m")
	t.Setenv("CM_COOKIE_SECURE", "true")
	t.Setenv("CM_DATA_DIR", "/srv/data")

	cfg := Load()
	if cfg.Auth.Username != "xiangwei" {
		t.Errorf("Username = %q", cfg.Auth.Username)
	}
	if cfg.Auth.Password != "s3cret-passphrase" {
		t.Errorf("Password = %q", cfg.Auth.Password)
	}
	if cfg.Auth.PasswordHash != "$2a$12$abcdefghijklmnopqrstuv" {
		t.Errorf("PasswordHash = %q", cfg.Auth.PasswordHash)
	}
	if cfg.Auth.SessionSecret != "0123456789abcdef0123456789abcdef" {
		t.Errorf("SessionSecret = %q", cfg.Auth.SessionSecret)
	}
	if cfg.Auth.SessionTTL != 30*time.Minute {
		t.Errorf("SessionTTL = %v, want 30m", cfg.Auth.SessionTTL)
	}
	if !cfg.Auth.CookieSecure {
		t.Error("CookieSecure 应解析为 true")
	}
	if want := filepath.Join("/srv/data", ".session-secret"); cfg.Auth.SessionSecretPath != want {
		t.Errorf("SessionSecretPath = %q, want %q", cfg.Auth.SessionSecretPath, want)
	}
}

// 口令前后空格是口令的一部分：做 TrimSpace 会让用户按 .env 明文登录却一直失败。
func TestLoadPasswordKeepsSurroundingSpaces(t *testing.T) {
	t.Setenv("CM_AUTH_PASSWORD", "  padded secret  ")
	if got := Load().Auth.Password; got != "  padded secret  " {
		t.Errorf("Password = %q, want 保留前后空格", got)
	}
}

// 非法 CM_SESSION_TTL 应回落到默认值而不是 0（0 会被 auth 包再兜底，但语义要稳定）。
func TestLoadInvalidSessionTTLFallsBack(t *testing.T) {
	t.Setenv("CM_SESSION_TTL", "not-a-duration")
	if got := Load().Auth.SessionTTL; got != 12*time.Hour {
		t.Errorf("SessionTTL = %v, want 12h", got)
	}
}
