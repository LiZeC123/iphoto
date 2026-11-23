package store

import (
	"context"
	"fmt"
	"time"
)

// Signatures 返回某算法下已缓存的全部指纹（asset hash -> 指纹值）。
// 检测任务一次性加载到内存，避免逐张查询。
func (s *Store) Signatures(ctx context.Context, algorithm string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT hash, value FROM asset_signatures WHERE algorithm = ?`, algorithm)
	if err != nil {
		return nil, fmt.Errorf("读取指纹缓存: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string, 256)
	for rows.Next() {
		var hash, value string
		if err := rows.Scan(&hash, &value); err != nil {
			return nil, err
		}
		out[hash] = value
	}
	return out, rows.Err()
}

// SaveSignature 写入或更新一张图片的指纹。
func (s *Store) SaveSignature(ctx context.Context, hash, algorithm, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO asset_signatures (hash, algorithm, value, computed_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(hash, algorithm) DO UPDATE SET
			value = excluded.value, computed_at = excluded.computed_at`,
		hash, algorithm, value, time.Now().Format(TimeLayout))
	if err != nil {
		return fmt.Errorf("写入指纹缓存: %w", err)
	}
	return nil
}
