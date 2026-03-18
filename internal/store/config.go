package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type ConfigEntry struct {
	Key         string
	Value       string
	Description string
	UpdatedAt   time.Time
}

// GetConfig returns a single config value. Returns ("", nil) if key not found.
func (s *Store) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM system_config WHERE key = $1`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get config %s: %w", key, err)
	}
	return value, nil
}

// SetConfig updates a config value.
func (s *Store) SetConfig(ctx context.Context, key, value string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE system_config SET value = $1, updated_at = now() WHERE key = $2`, value, key)
	if err != nil {
		return fmt.Errorf("set config %s: %w", key, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("config key %q not found", key)
	}
	return nil
}

// ListConfig returns all config entries.
func (s *Store) ListConfig(ctx context.Context) ([]ConfigEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value, description, updated_at FROM system_config ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list config: %w", err)
	}
	defer rows.Close()

	var result []ConfigEntry
	for rows.Next() {
		var c ConfigEntry
		if err := rows.Scan(&c.Key, &c.Value, &c.Description, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan config: %w", err)
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
