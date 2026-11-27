package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Batch 对应 import_batches。
type Batch struct {
	ID         int64   `json:"id"`
	SourcePath string  `json:"source_path"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Status     string  `json:"status"`
	StatsJSON  *string `json:"stats_json,omitempty"`
	ItemCount  int64   `json:"item_count"`
	// Source 为 local 或 phone；旧批次默认 local。
	Source string `json:"source"`
	// DeviceID 仅手机导入批次有值。
	DeviceID *int64 `json:"device_id,omitempty"`
}

// ImportItem 对应 import_items。
type ImportItem struct {
	ID         int64  `json:"id"`
	BatchID    int64  `json:"batch_id"`
	SourcePath string `json:"source_path"`
	Hash       string `json:"hash,omitempty"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
}

// DuplicateItem 是“哈希相同、文件名不同”的待处理项，附带库内已有图片信息。
type DuplicateItem struct {
	ID            int64   `json:"id"`
	SourcePath    string  `json:"source_path"`
	Hash          string  `json:"hash"`
	ExistingName  string  `json:"existing_name"`
	ExistingSize  int64   `json:"existing_size"`
	ExistingTaken *string `json:"existing_taken_at"`
	IncomingSize  int64   `json:"incoming_size"`
}

// ReportRow 是 CSV 报告的一行。
type ReportRow struct {
	SourcePath string
	Hash       string
	Status     string
	Message    string
}

// CreateBatch 开始一条本地导入批次。
func (s *Store) CreateBatch(ctx context.Context, sourcePath, startedAt string) (Batch, error) {
	return s.CreateBatchWithSource(ctx, sourcePath, startedAt, "local", nil)
}

// CreateBatchWithSource 开始一条指定来源的导入批次（local 或 phone）。
func (s *Store) CreateBatchWithSource(ctx context.Context, sourcePath, startedAt, source string, deviceID *int64) (Batch, error) {
	if source == "" {
		source = "local"
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO import_batches (source_path, started_at, status, source, device_id)
		 VALUES (?, ?, 'running', ?, ?)`,
		sourcePath, startedAt, source, deviceID)
	if err != nil {
		return Batch{}, fmt.Errorf("创建导入批次: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Batch{}, err
	}
	return Batch{ID: id, SourcePath: sourcePath, StartedAt: startedAt, Status: "running", Source: source, DeviceID: deviceID}, nil
}

// GetBatch 读取批次。
func (s *Store) GetBatch(ctx context.Context, id int64) (Batch, error) {
	var (
		b        Batch
		finished sql.NullString
		stats    sql.NullString
		deviceID sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, source_path, started_at, finished_at, status, stats_json, source, device_id
		 FROM import_batches WHERE id = ?`, id).
		Scan(&b.ID, &b.SourcePath, &b.StartedAt, &finished, &b.Status, &stats, &b.Source, &deviceID)
	if err != nil {
		return Batch{}, err
	}
	b.FinishedAt = nullStringPtr(finished)
	b.StatsJSON = nullStringPtr(stats)
	if deviceID.Valid {
		v := deviceID.Int64
		b.DeviceID = &v
	}
	return b, nil
}

// FinishBatch 结束批次并记录统计 JSON。
func (s *Store) FinishBatch(ctx context.Context, id int64, status, finishedAt, statsJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE import_batches SET status = ?, finished_at = ?, stats_json = ? WHERE id = ?`,
		status, finishedAt, statsJSON, id)
	if err != nil {
		return fmt.Errorf("结束导入批次: %w", err)
	}
	return nil
}

// InterruptRunningBatches 在启动时把进程崩溃遗留的 running 批次标记为 interrupted。
func (s *Store) InterruptRunningBatches(ctx context.Context, finishedAt string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE import_batches SET status = 'interrupted', finished_at = ? WHERE status = 'running'`,
		finishedAt)
	if err != nil {
		return 0, fmt.Errorf("标记中断批次: %w", err)
	}
	return res.RowsAffected()
}

// AddImportItem 记录单个源文件的处理结果。
func (s *Store) AddImportItem(ctx context.Context, batchID int64, sourcePath, hash, status, message string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO import_items (batch_id, source_path, hash, status, message) VALUES (?, ?, ?, ?, ?)`,
		batchID, sourcePath, nullIfEmpty(hash), status, message)
	if err != nil {
		return 0, fmt.Errorf("写入 import_items: %w", err)
	}
	return res.LastInsertId()
}

// BatchItemCounts 返回该批次各状态的数量。
func (s *Store) BatchItemCounts(ctx context.Context, batchID int64) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, count(*) FROM import_items WHERE batch_id = ? GROUP BY status`, batchID)
	if err != nil {
		return nil, fmt.Errorf("统计 import_items: %w", err)
	}
	defer rows.Close()

	counts := map[string]int64{}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		counts[status] = n
	}
	return counts, rows.Err()
}

// ListDuplicateItems 返回待用户处理的重复项（哈希相同、文件名不同）。
func (s *Store) ListDuplicateItems(ctx context.Context, batchID int64) ([]DuplicateItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.source_path, i.hash, a.original_name, a.size, a.taken_at
		FROM import_items i
		JOIN assets a ON a.hash = i.hash
		WHERE i.batch_id = ? AND i.status = 'duplicate_pending'
		ORDER BY i.id`, batchID)
	if err != nil {
		return nil, fmt.Errorf("查询重复项: %w", err)
	}
	defer rows.Close()

	out := make([]DuplicateItem, 0)
	for rows.Next() {
		var (
			d     DuplicateItem
			taken sql.NullString
		)
		if err := rows.Scan(&d.ID, &d.SourcePath, &d.Hash, &d.ExistingName, &d.ExistingSize, &taken); err != nil {
			return nil, err
		}
		d.ExistingTaken = nullStringPtr(taken)
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetImportItem 读取单条导入项（校验批次归属）。
func (s *Store) GetImportItem(ctx context.Context, batchID, itemID int64) (ImportItem, error) {
	var (
		item    ImportItem
		hash    sql.NullString
		message sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, batch_id, source_path, hash, status, message FROM import_items WHERE id = ? AND batch_id = ?`,
		itemID, batchID).
		Scan(&item.ID, &item.BatchID, &item.SourcePath, &hash, &item.Status, &message)
	if err != nil {
		return ImportItem{}, err
	}
	item.Hash = hash.String
	item.Message = message.String
	return item, nil
}

// SetImportItemStatus 更新导入项状态与说明。
func (s *Store) SetImportItemStatus(ctx context.Context, itemID int64, status, message string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE import_items SET status = ?, message = ? WHERE id = ?`, status, message, itemID)
	if err != nil {
		return fmt.Errorf("更新 import_items: %w", err)
	}
	return nil
}

// ReportRows 返回 CSV 导出所需的全部记录。
func (s *Store) ReportRows(ctx context.Context, batchID int64) ([]ReportRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source_path, COALESCE(hash, ''), status, COALESCE(message, '')
		FROM import_items WHERE batch_id = ? ORDER BY id`, batchID)
	if err != nil {
		return nil, fmt.Errorf("查询报告: %w", err)
	}
	defer rows.Close()

	out := make([]ReportRow, 0)
	for rows.Next() {
		var r ReportRow
		if err := rows.Scan(&r.SourcePath, &r.Hash, &r.Status, &r.Message); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CleanupImportItems 删除某批次的明细，返回删除行数。
func (s *Store) CleanupImportItems(ctx context.Context, batchID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM import_items WHERE batch_id = ?`, batchID)
	if err != nil {
		return 0, fmt.Errorf("清理 import_items: %w", err)
	}
	return res.RowsAffected()
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
