package store

import (
	"context"
	"fmt"
)

// Settings 读取全部 app_settings。
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM app_settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("读取设置: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetSettings 批量写入（upsert），整体在一个事务内完成。
func (s *Store) SetSettings(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return fmt.Errorf("设置内容不能为空")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("写入设置: %w", err)
	}
	defer tx.Rollback()
	for k, v := range values {
		if err := validateSettingKey(k); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO app_settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return fmt.Errorf("写入设置: %w", err)
		}
	}
	return tx.Commit()
}

// Setting 读取单个键。
func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		if isNoRows(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("读取设置: %w", err)
	}
	return v, true, nil
}

func validateSettingKey(k string) error {
	if k == "" {
		return fmt.Errorf("设置键不能为空")
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
		default:
			return fmt.Errorf("设置键只允许字母、数字、下划线、点、短横线: %q", k)
		}
	}
	return nil
}
