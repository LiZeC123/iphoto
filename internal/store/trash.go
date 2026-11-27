package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ListTrash 分页返回回收站中的图片，按删除时间倒序。
func (s *Store) ListTrash(ctx context.Context, page, pageSize int) (AssetPage, error) {
	where := []string{"a.status = 'trashed'"}
	return s.queryAssets(ctx, where, nil, "deleted_at_desc", page, pageSize)
}

// TrashedAssets 返回全部回收站记录（清空/恢复时使用）。
func (s *Store) TrashedAssets(ctx context.Context) ([]Asset, error) {
	return s.ListAssetsByStatus(ctx, "trashed")
}

// RestoreAsset 把记录恢复为 ready。
func (s *Store) RestoreAsset(ctx context.Context, hash string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE assets SET status = 'ready', deleted_at = NULL WHERE hash = ? AND status = 'trashed'`, hash)
	if err != nil {
		return fmt.Errorf("恢复图片: %w", err)
	}
	return requireAffected(res, 1)
}

// PurgeTrash 删除全部回收站记录（物理文件由调用方处理）。
func (s *Store) PurgeTrash(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM assets WHERE status = 'trashed'`)
	if err != nil {
		return 0, fmt.Errorf("清空回收站: %w", err)
	}
	return res.RowsAffected()
}

// ListAssetsByHashes 按哈希批量读取资产（导出用）。
func (s *Store) ListAssetsByHashes(ctx context.Context, hashes []string) ([]Asset, error) {
	if len(hashes) == 0 {
		return []Asset{}, nil
	}
	out := make([]Asset, 0, len(hashes))
	for _, hash := range hashes {
		a, err := s.GetAsset(ctx, hash)
		if err != nil {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// ListBatches 返回最近的导入批次及明细数，用于设置页清理历史。
func (s *Store) ListBatches(ctx context.Context, limit int) ([]Batch, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.source_path, b.started_at, b.finished_at, b.status, b.stats_json,
			b.source, b.device_id,
			(SELECT count(*) FROM import_items i WHERE i.batch_id = b.id)
		FROM import_batches b ORDER BY b.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("查询导入批次: %w", err)
	}
	defer rows.Close()

	out := make([]Batch, 0, limit)
	for rows.Next() {
		var (
			b        Batch
			finished sql.NullString
			stats    sql.NullString
			deviceID sql.NullInt64
			items    int64
		)
		if err := rows.Scan(&b.ID, &b.SourcePath, &b.StartedAt, &finished, &b.Status, &stats,
			&b.Source, &deviceID, &items); err != nil {
			return nil, err
		}
		b.FinishedAt = nullStringPtr(finished)
		b.StatsJSON = nullStringPtr(stats)
		if deviceID.Valid {
			v := deviceID.Int64
			b.DeviceID = &v
		}
		b.ItemCount = items
		out = append(out, b)
	}
	return out, rows.Err()
}

// AllAssetRelPaths 返回所有记录引用的 rel_path，用于孤儿文件扫描。
func (s *Store) AllAssetRelPaths(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rel_path FROM assets`)
	if err != nil {
		return nil, fmt.Errorf("查询资产路径: %w", err)
	}
	defer rows.Close()

	out := make(map[string]struct{}, 1024)
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return nil, err
		}
		out[rel] = struct{}{}
	}
	return out, rows.Err()
}
