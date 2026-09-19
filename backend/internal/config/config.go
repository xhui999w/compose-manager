package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr       string
	DataDir          string
	BackupDir        string
	ComposeRoots     []string
	DockerHost       string
	NASIP            string
	DefaultScheme    string
	DemoMode         bool
	OperationTimeout time.Duration
	Auth             AuthConfig
}

// AuthConfig 描述登录认证配置。
//
// 既没给 CM_AUTH_PASSWORD 也没给 CM_AUTH_PASSWORD_HASH 时面板不启用登录，
// 保持与既有部署兼容；两种口令来源同时存在时以哈希为准。
type AuthConfig struct {
	Username          string
	Password          string
	PasswordHash      string
	SessionSecret     string
	SessionSecretPath string
	SessionTTL        time.Duration
	CookieSecure      bool
}

func Load() Config {
	dataDir := env("CM_DATA_DIR", "./data")
	return Config{
		ListenAddr:       env("CM_LISTEN_ADDR", ":8080"),
		DataDir:          dataDir,
		BackupDir:        env("CM_BACKUP_DIR", "./backups"),
		ComposeRoots:     splitPaths(env("CM_COMPOSE_ROOTS", "./compose")),
		DockerHost:       env("CM_DOCKER_HOST", "unix:///var/run/docker.sock"),
		NASIP:            env("CM_NAS_IP", "127.0.0.1"),
		DefaultScheme:    env("CM_DEFAULT_SCHEME", "http"),
		DemoMode:         envBool("CM_DEMO_MODE", false),
		OperationTimeout: envDuration("CM_OPERATION_TIMEOUT", 2*time.Minute),
		Auth: AuthConfig{
			Username: env("CM_AUTH_USER", "admin"),
			// 口令不做 TrimSpace：前后空格是口令的一部分，裁剪会与用户实际输入不一致。
			Password:          os.Getenv("CM_AUTH_PASSWORD"),
			PasswordHash:      os.Getenv("CM_AUTH_PASSWORD_HASH"),
			SessionSecret:     os.Getenv("CM_SESSION_SECRET"),
			SessionSecretPath: env("CM_SESSION_SECRET_PATH", filepath.Join(dataDir, ".session-secret")),
			SessionTTL:        envDuration("CM_SESSION_TTL", 12*time.Hour),
			// 默认 false：面板通常以 http://<内网 IP>:8080 直连，Secure Cookie 会被浏览器直接丢弃。
			CookieSecure: envBool("CM_COOKIE_SECURE", false),
		},
	}
}

func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, "compose-manager.db") }

func ApplySettings(c Config, values map[string]any) Config {
	if value, ok := values["dockerHost"].(string); ok && strings.TrimSpace(value) != "" {
		c.DockerHost = strings.TrimSpace(value)
	}
	if value, ok := values["composeRoots"].(string); ok && strings.TrimSpace(value) != "" {
		c.ComposeRoots = splitPaths(value)
	}
	if value, ok := values["nasIP"].(string); ok && strings.TrimSpace(value) != "" {
		c.NASIP = strings.TrimSpace(value)
	}
	if value, ok := values["defaultScheme"].(string); ok && (value == "http" || value == "https") {
		c.DefaultScheme = value
	}
	return c
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitPaths(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if cleaned := strings.TrimSpace(part); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}
