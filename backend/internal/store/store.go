package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

var ErrAlreadyInitialized = errors.New("administrator account already exists")

type User struct {
	ID           int64
	Username     string
	PasswordHash string
}

type Session struct {
	UserID    int64
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("restrict database permissions: %w", err)
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS compose_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_key TEXT NOT NULL,
  file_path TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  backup_path TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_compose_versions_project ON compose_versions(project_key, created_at DESC);
CREATE TABLE IF NOT EXISTS update_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_key TEXT NOT NULL,
  service TEXT NOT NULL,
  old_image TEXT NOT NULL DEFAULT '',
  old_digest TEXT NOT NULL DEFAULT '',
  new_image TEXT NOT NULL DEFAULT '',
  new_digest TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  result TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  csrf_token TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token_hash);
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);
`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) Settings(ctx context.Context) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]any{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			value = raw
		}
		result[key] = value
	}
	return result, rows.Err()
}

func (s *Store) PutSettings(ctx context.Context, values map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, string(raw), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AddVersion(ctx context.Context, version model.ComposeVersion) (model.ComposeVersion, error) {
	created := version.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO compose_versions(project_key,file_path,sha256,backup_path,created_at) VALUES(?,?,?,?,?)`, version.ProjectKey, version.FilePath, version.SHA256, version.BackupPath, created.Format(time.RFC3339Nano))
	if err != nil {
		return model.ComposeVersion{}, err
	}
	version.ID, _ = result.LastInsertId()
	version.CreatedAt = created
	return version, nil
}

func (s *Store) Versions(ctx context.Context, projectKey string) ([]model.ComposeVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_key, file_path, sha256, backup_path, created_at FROM compose_versions WHERE project_key=? ORDER BY created_at DESC LIMIT 100`, projectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.ComposeVersion
	for rows.Next() {
		var item model.ComposeVersion
		var created string
		if err := rows.Scan(&item.ID, &item.ProjectKey, &item.FilePath, &item.SHA256, &item.BackupPath, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Version(ctx context.Context, projectKey string, id int64) (model.ComposeVersion, error) {
	var item model.ComposeVersion
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, project_key, file_path, sha256, backup_path, created_at FROM compose_versions WHERE project_key=? AND id=?`, projectKey, id).Scan(&item.ID, &item.ProjectKey, &item.FilePath, &item.SHA256, &item.BackupPath, &created)
	if err != nil {
		return model.ComposeVersion{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return item, nil
}

func (s *Store) AddUpdate(ctx context.Context, item model.UpdateRecord) (model.UpdateRecord, error) {
	created := item.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO update_records(project_key,service,old_image,old_digest,new_image,new_digest,status,error,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, item.Project, item.Service, item.OldImage, item.OldDigest, item.NewImage, item.NewDigest, item.Status, item.Error, created.Format(time.RFC3339Nano))
	if err != nil {
		return model.UpdateRecord{}, err
	}
	item.ID, _ = result.LastInsertId()
	item.CreatedAt = created
	return item, nil
}

func (s *Store) Updates(ctx context.Context) ([]model.UpdateRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_key, service, old_image, old_digest, new_image, new_digest, status, error, created_at FROM update_records ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.UpdateRecord
	for rows.Next() {
		var item model.UpdateRecord
		var created string
		if err := rows.Scan(&item.ID, &item.Project, &item.Service, &item.OldImage, &item.OldDigest, &item.NewImage, &item.NewDigest, &item.Status, &item.Error, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Audit(ctx context.Context, action, targetType, targetID, result, detail string) error {
	if len(detail) > 2048 {
		detail = detail[:2048]
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(action,target_type,target_id,result,detail,created_at) VALUES(?,?,?,?,?,?)`, action, targetType, targetID, result, detail, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}

func (s *Store) HasUsers(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) CreateInitialUser(ctx context.Context, username, passwordHash string) (User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return User{}, err
	}
	if count != 0 {
		return User{}, ErrAlreadyInitialized
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `INSERT INTO users(username,password_hash,created_at,updated_at) VALUES(?,?,?,?)`, username, passwordHash, now, now)
	if err != nil {
		return User{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, PasswordHash: passwordHash}, nil
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash FROM users WHERE username = ? COLLATE NOCASE`, username).Scan(&user.ID, &user.Username, &user.PasswordHash)
	return user, err
}

func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash, csrfToken string, expiresAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(user_id,token_hash,csrf_token,expires_at,created_at) VALUES(?,?,?,?,?)`, userID, tokenHash, csrfToken, expiresAt.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	var session Session
	var expires string
	err := s.db.QueryRowContext(ctx, `SELECT sessions.user_id, users.username, sessions.csrf_token, sessions.expires_at
		FROM sessions JOIN users ON users.id = sessions.user_id
		WHERE sessions.token_hash = ? AND sessions.expires_at > ?`, tokenHash, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&session.UserID, &session.Username, &session.CSRFToken, &expires)
	if err != nil {
		return Session{}, err
	}
	session.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}
