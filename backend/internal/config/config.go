package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr          string
	DataDir             string
	BackupDir           string
	ComposeRoots        []string
	DockerHost          string
	NASIP               string
	DefaultScheme       string
	ProxyURL            string
	DemoMode            bool
	OperationTimeout    time.Duration
	UpdateCheckInterval time.Duration
	SessionTTL          time.Duration
	SecureCookie        bool
	SetupToken          string
}

func Load() Config {
	dataDir := env("CM_DATA_DIR", "./data")
	return Config{
		ListenAddr:          env("CM_LISTEN_ADDR", ":8080"),
		DataDir:             dataDir,
		BackupDir:           env("CM_BACKUP_DIR", "./backups"),
		ComposeRoots:        splitPaths(env("CM_COMPOSE_ROOTS", "./compose")),
		DockerHost:          env("CM_DOCKER_HOST", "unix:///var/run/docker.sock"),
		NASIP:               env("CM_NAS_IP", "127.0.0.1"),
		DefaultScheme:       env("CM_DEFAULT_SCHEME", "http"),
		ProxyURL:            strings.TrimSpace(os.Getenv("CM_PROXY_URL")),
		DemoMode:            envBool("CM_DEMO_MODE", false),
		OperationTimeout:    envDuration("CM_OPERATION_TIMEOUT", 15*time.Minute),
		UpdateCheckInterval: envDuration("CM_UPDATE_CHECK_INTERVAL", 24*time.Hour),
		SessionTTL:          envDuration("CM_SESSION_TTL", 7*24*time.Hour),
		SecureCookie:        envBool("CM_SECURE_COOKIE", false),
		SetupToken:          strings.TrimSpace(os.Getenv("CM_SETUP_TOKEN")),
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
	if value, ok := values["proxyURL"].(string); ok {
		c.ProxyURL = strings.TrimSpace(value)
	}
	if value, ok := settingNumber(values["operationTimeoutMinutes"]); ok && value >= 2 && value <= 60 {
		c.OperationTimeout = time.Duration(value) * time.Minute
	}
	return c
}

func settingNumber(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		return int(number), number == float64(int(number))
	case int:
		return number, true
	default:
		return 0, false
	}
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
