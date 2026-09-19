package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/compose-manager/compose-manager/backend/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

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
);`
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
