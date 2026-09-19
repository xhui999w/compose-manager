package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/compose-manager/compose-manager/backend/internal/store"
)

const (
	insecureCookieName = "cm_session"
	secureCookieName   = "__Host-cm_session"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidSetupToken  = errors.New("invalid setup token")
	ErrInvalidUsername    = errors.New("username must be 3-32 characters using letters, numbers, dot, underscore, or hyphen")
	ErrInvalidPassword    = errors.New("password must be 12-128 characters")
	usernamePattern       = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

type Config struct {
	DataDir      string
	SessionTTL   time.Duration
	SecureCookie bool
	SetupToken   string
}

type Result struct {
	Username     string
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
}

type Manager struct {
	store        *store.Store
	logger       *slog.Logger
	ttl          time.Duration
	secureCookie bool
	setupPath    string
	dummyHash    string
	mu           sync.RWMutex
	setupToken   string
}

func New(ctx context.Context, database *store.Store, cfg Config, logger *slog.Logger) (*Manager, error) {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 7 * 24 * time.Hour
	}
	dummyHash, err := hashPassword("compose-manager-dummy-password")
	if err != nil {
		return nil, err
	}
	manager := &Manager{store: database, logger: logger, ttl: cfg.SessionTTL, secureCookie: cfg.SecureCookie, setupPath: filepath.Join(cfg.DataDir, "setup-token"), dummyHash: dummyHash}
	hasUsers, err := database.HasUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("check administrator account: %w", err)
	}
	if hasUsers {
		_ = os.Remove(manager.setupPath)
		return manager, nil
	}
	token := strings.TrimSpace(cfg.SetupToken)
	if token == "" {
		token, err = loadOrCreateSetupToken(manager.setupPath)
		if err != nil {
			return nil, err
		}
		logger.Warn("administrator setup required", "setupToken", token, "setupTokenFile", manager.setupPath)
	} else {
		logger.Warn("administrator setup required; setup token supplied by CM_SETUP_TOKEN")
	}
	manager.setupToken = token
	return manager, nil
}

func (m *Manager) CookieName() string {
	if m.secureCookie {
		return secureCookieName
	}
	return insecureCookieName
}

func (m *Manager) SecureCookie() bool        { return m.secureCookie }
func (m *Manager) SessionTTL() time.Duration { return m.ttl }

func (m *Manager) SetupRequired(ctx context.Context) (bool, error) {
	hasUsers, err := m.store.HasUsers(ctx)
	return !hasUsers, err
}

func (m *Manager) Setup(ctx context.Context, setupToken, username, password string) (Result, error) {
	m.mu.RLock()
	expectedToken := m.setupToken
	m.mu.RUnlock()
	if expectedToken == "" || !constantTimeEqual(expectedToken, strings.TrimSpace(setupToken)) {
		return Result{}, ErrInvalidSetupToken
	}
	username = strings.TrimSpace(username)
	if err := validateCredentials(username, password); err != nil {
		return Result{}, err
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return Result{}, err
	}
	user, err := m.store.CreateInitialUser(ctx, username, passwordHash)
	if err != nil {
		return Result{}, err
	}
	result, err := m.newSession(ctx, user)
	if err != nil {
		return Result{}, err
	}
	m.mu.Lock()
	m.setupToken = ""
	m.mu.Unlock()
	if err := os.Remove(m.setupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.logger.Warn("remove setup token file", "error", err)
	}
	_ = m.store.Audit(ctx, "auth.setup", "user", user.Username, "success", "initial administrator created")
	return result, nil
}

func (m *Manager) Login(ctx context.Context, username, password string) (Result, error) {
	username = strings.TrimSpace(username)
	user, err := m.store.UserByUsername(ctx, username)
	encoded := m.dummyHash
	if err == nil {
		encoded = user.PasswordHash
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	valid, verifyErr := verifyPassword(encoded, password)
	if verifyErr != nil {
		m.logger.Error("verify password hash", "error", verifyErr)
		return Result{}, ErrInvalidCredentials
	}
	if err != nil || !valid {
		return Result{}, ErrInvalidCredentials
	}
	result, err := m.newSession(ctx, user)
	if err != nil {
		return Result{}, err
	}
	_ = m.store.Audit(ctx, "auth.login", "user", user.Username, "success", "")
	return result, nil
}

func (m *Manager) Authenticate(ctx context.Context, token string) (store.Session, error) {
	if strings.TrimSpace(token) == "" {
		return store.Session{}, sql.ErrNoRows
	}
	return m.store.SessionByTokenHash(ctx, hashToken(token))
}

func (m *Manager) Logout(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	session, authErr := m.Authenticate(ctx, token)
	if err := m.store.DeleteSession(ctx, hashToken(token)); err != nil {
		return err
	}
	if authErr == nil {
		_ = m.store.Audit(ctx, "auth.logout", "user", session.Username, "success", "")
	}
	return nil
}

func (m *Manager) ValidCSRF(session store.Session, token string) bool {
	return session.CSRFToken != "" && constantTimeEqual(session.CSRFToken, token)
}

func (m *Manager) newSession(ctx context.Context, user store.User) (Result, error) {
	sessionToken, err := randomToken(32)
	if err != nil {
		return Result{}, err
	}
	csrfToken, err := randomToken(32)
	if err != nil {
		return Result{}, err
	}
	expiresAt := time.Now().UTC().Add(m.ttl)
	if err := m.store.CreateSession(ctx, user.ID, hashToken(sessionToken), csrfToken, expiresAt); err != nil {
		return Result{}, err
	}
	return Result{Username: user.Username, SessionToken: sessionToken, CSRFToken: csrfToken, ExpiresAt: expiresAt}, nil
}

func validateCredentials(username, password string) error {
	if len(username) < 3 || len(username) > 32 || !usernamePattern.MatchString(username) {
		return ErrInvalidUsername
	}
	passwordLength := utf8.RuneCountInString(password)
	if passwordLength < 12 || passwordLength > 128 || len(password) > 512 {
		return ErrInvalidPassword
	}
	return nil
}

func loadOrCreateSetupToken(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if token := strings.TrimSpace(string(raw)); len(token) >= 20 {
			return token, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read setup token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	token, err := randomToken(24)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		return strings.TrimSpace(string(raw)), nil
	}
	if err != nil {
		return "", fmt.Errorf("create setup token: %w", err)
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return token, nil
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func constantTimeEqual(left, right string) bool {
	leftDigest := sha256.Sum256([]byte(left))
	rightDigest := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftDigest[:], rightDigest[:]) == 1
}
