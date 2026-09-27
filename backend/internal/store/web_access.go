package store

import (
	"context"
	"encoding/json"

	"github.com/compose-manager/compose-manager/backend/internal/access"
)

func (s *Store) WebAccess(ctx context.Context) (map[string]access.WebConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT project_key, config FROM project_web_access`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]access.WebConfig{}
	for rows.Next() {
		var key, raw string
		var cfg access.WebConfig
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return nil, err
		}
		result[key] = cfg
	}
	return result, rows.Err()
}

func (s *Store) PutWebAccess(ctx context.Context, key string, cfg access.WebConfig) error {
	if err := access.ValidateWebConfig(cfg); err != nil {
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO project_web_access(project_key,config) VALUES(?,?) ON CONFLICT(project_key) DO UPDATE SET config=excluded.config`, key, string(raw))
	return err
}
