package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"iphoto/internal/search"
)

// TimeLayout 是库内存储的本地时间格式（无时区），见 dev.md 4.1。
const TimeLayout = "2006-01-02T15:04:05"

// Asset 对应 assets 表一行。可空列用指针表示。
type Asset struct {
	Hash            string  `json:"hash"`
	OriginalName    string  `json:"original_name"`
	StorageName     string  `json:"storage_name"`
	RelPath         string  `json:"rel_path"`
	Size            int64   `json:"size"`
	Mime            string  `json:"mime"`
	Width           *int    `json:"width"`
	Height          *int    `json:"height"`
	TakenAt         *string `json:"taken_at"`
	TakenAtSource   *string `json:"taken_at_source"`
	TakenAtOverride *string `json:"taken_at_override"`
	Note            *string `json:"note"`
	AddedAt         string  `json:"added_at"`
	Status          string  `json:"status"`
	DeletedAt       *string `json:"deleted_at"`
}

// EffectiveTime 返回 COALESCE(taken_at_override, taken_at) 对应的值。
func (a Asset) EffectiveTime() *string {
	if a.TakenAtOverride != nil && *a.TakenAtOverride != "" {
		return a.TakenAtOverride
	}
	return a.TakenAt
}

const assetColumns = `a.hash, a.original_name, a.storage_name, a.rel_path, a.size, a.mime,
	a.width, a.height, a.taken_at, a.taken_at_source, a.taken_at_override, a.note,
	a.added_at, a.status, a.deleted_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAsset(row rowScanner) (Asset, error) {
	var (
		a                                        Asset
		width, height                            sql.NullInt64
		takenAt, source, override, note, deleted sql.NullString
	)
	if err := row.Scan(
		&a.Hash, &a.OriginalName, &a.StorageName, &a.RelPath, &a.Size, &a.Mime,
		&width, &height, &takenAt, &source, &override, &note,
		&a.AddedAt, &a.Status, &deleted,
	); err != nil {
		return Asset{}, err
	}
	a.Width = nullIntPtr(width)
	a.Height = nullIntPtr(height)
	a.TakenAt = nullStringPtr(takenAt)
	a.TakenAtSource = nullStringPtr(source)
	a.TakenAtOverride = nullStringPtr(override)
	a.Note = nullStringPtr(note)
	a.DeletedAt = nullStringPtr(deleted)
	return a, nil
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

// GetAsset 按哈希读取单张图片，不存在时返回 sql.ErrNoRows。
func (s *Store) GetAsset(ctx context.Context, hash string) (Asset, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM assets a WHERE a.hash = ?`, hash)
	return scanAsset(row)
}

// InsertImportingAsset 先落一条 status='importing' 记录，复制完成后再更新为 ready。
// 这样崩溃后可以依据该状态做修复（dev.md 5.4）。
func (s *Store) InsertImportingAsset(ctx context.Context, a Asset) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO assets (hash, original_name, storage_name, rel_path, size, mime,
			width, height, taken_at, taken_at_source, taken_at_override, note,
			added_at, status, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, 'importing', NULL)`,
		a.Hash, a.OriginalName, a.StorageName, a.RelPath, a.Size, a.Mime,
		a.Width, a.Height, a.TakenAt, a.TakenAtSource, a.AddedAt)
	if err != nil {
		return fmt.Errorf("写入 assets: %w", err)
	}
	return nil
}

// MarkAssetReady 在复制与探测完成后补齐字段并置为 ready。
func (s *Store) MarkAssetReady(ctx context.Context, hash string, size int64, mime string, width, height *int, takenAt, takenAtSource string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE assets SET size = ?, mime = ?, width = ?, height = ?,
			taken_at = ?, taken_at_source = ?, status = 'ready'
		WHERE hash = ?`,
		size, mime, width, height, takenAt, takenAtSource, hash)
	if err != nil {
		return fmt.Errorf("更新 assets: %w", err)
	}
	return requireAffected(res, 1)
}

// AssetPatch 描述 PATCH /api/assets/{hash} 允许修改的字段。
// nil 表示不修改；指向空字符串表示清空该字段。
type AssetPatch struct {
	Note            *string
	TakenAtOverride *string
	OriginalName    *string
}

// PatchAsset 应用部分更新并返回最新记录。
func (s *Store) PatchAsset(ctx context.Context, hash string, p AssetPatch) (Asset, error) {
	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if p.Note != nil {
		sets = append(sets, "note = ?")
		args = append(args, emptyToNil(*p.Note))
	}
	if p.TakenAtOverride != nil {
		sets = append(sets, "taken_at_override = ?")
		args = append(args, emptyToNil(*p.TakenAtOverride))
	}
	if p.OriginalName != nil {
		name := strings.TrimSpace(*p.OriginalName)
		if name == "" {
			return Asset{}, fmt.Errorf("original_name 不能为空")
		}
		sets = append(sets, "original_name = ?")
		args = append(args, name)
	}
	if len(sets) == 0 {
		return s.GetAsset(ctx, hash)
	}
	args = append(args, hash)
	res, err := s.db.ExecContext(ctx, `UPDATE assets SET `+strings.Join(sets, ", ")+` WHERE hash = ?`, args...)
	if err != nil {
		return Asset{}, fmt.Errorf("更新 assets: %w", err)
	}
	if err := requireAffected(res, 1); err != nil {
		return Asset{}, err
	}
	return s.GetAsset(ctx, hash)
}

// SetAssetNames 用于重复项“替换”时仅重命名库内文件（含 rel_path）。
func (s *Store) SetAssetNames(ctx context.Context, hash, originalName, storageName, relPath string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE assets SET original_name = ?, storage_name = ?, rel_path = ? WHERE hash = ?`,
		originalName, storageName, relPath, hash)
	if err != nil {
		return fmt.Errorf("更新 assets 名称: %w", err)
	}
	return requireAffected(res, 1)
}

// SetAssetStatus 更新状态；deletedAt 为 nil 时写入 NULL。
func (s *Store) SetAssetStatus(ctx context.Context, hash, status string, deletedAt *string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE assets SET status = ?, deleted_at = ? WHERE hash = ?`, status, deletedAt, hash)
	if err != nil {
		return fmt.Errorf("更新 assets 状态: %w", err)
	}
	return requireAffected(res, 1)
}

// DeleteAssetRow 物理移除数据库记录（回收站清空、导入失败回滚时使用）。
func (s *Store) DeleteAssetRow(ctx context.Context, hash string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM assets WHERE hash = ?`, hash); err != nil {
		return fmt.Errorf("删除 assets 记录: %w", err)
	}
	return nil
}

// ListAssetsByStatus 用于启动时的中断恢复。
func (s *Store) ListAssetsByStatus(ctx context.Context, status string) ([]Asset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+assetColumns+` FROM assets a WHERE a.status = ?`, status)
	if err != nil {
		return nil, fmt.Errorf("查询 assets: %w", err)
	}
	defer rows.Close()

	var out []Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AssetFilter 是图库列表的查询条件。
type AssetFilter struct {
	Q        string // 搜索表达式（AND/OR/NOT/括号/字段前缀），见 internal/search
	Status   string
	Tag      string // 标签名（大小写不敏感）或标签 ID
	Untagged bool   // 只返回没有任何标签的图片
	From     string // YYYY / YYYY-MM / YYYY-MM-DD
	To       string
	Sort     string
	Page     int
	PageSize int
}

// AssetPage 是分页结果。
type AssetPage struct {
	Items    []Asset `json:"items"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

const effectiveTimeExpr = `COALESCE(a.taken_at_override, a.taken_at)`

var assetSorts = map[string]string{
	"taken_at_desc":   effectiveTimeExpr + ` IS NULL, ` + effectiveTimeExpr + ` DESC, a.added_at DESC, a.hash`,
	"taken_at_asc":    effectiveTimeExpr + ` IS NULL, ` + effectiveTimeExpr + ` ASC, a.added_at ASC, a.hash`,
	"added_at_desc":   `a.added_at DESC, a.hash`,
	"added_at_asc":    `a.added_at ASC, a.hash`,
	"name_asc":        `a.original_name COLLATE NOCASE ASC, a.hash`,
	"deleted_at_desc": `a.deleted_at DESC, a.hash`,
}

// ListAssets 按条件分页查询；未指定 status 时只返回 ready。
func (s *Store) ListAssets(ctx context.Context, f AssetFilter) (AssetPage, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 100
	}
	if f.PageSize > 500 {
		f.PageSize = 500
	}

	where := []string{"1 = 1"}
	args := make([]any, 0, 6)
	if f.Status != "" {
		where = append(where, "a.status = ?")
		args = append(args, f.Status)
	} else {
		where = append(where, "a.status = 'ready'")
	}
	if tag := strings.TrimSpace(f.Tag); tag != "" {
		where = append(where, `EXISTS (
			SELECT 1 FROM asset_tags at JOIN tags t ON t.id = at.tag_id
			WHERE at.hash = a.hash AND (t.name = ? COLLATE NOCASE OR CAST(t.id AS TEXT) = ?))`)
		args = append(args, tag, tag)
	}
	if f.Untagged {
		where = append(where, `NOT EXISTS (SELECT 1 FROM asset_tags at WHERE at.hash = a.hash)`)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		node, err := search.Parse(q)
		if err != nil {
			return AssetPage{}, fmt.Errorf("%w: %s", ErrSearchSyntax, err.Error())
		}
		if node != nil {
			clause, clauseArgs := buildSearchWhere(node)
			where = append(where, clause)
			args = append(args, clauseArgs...)
		}
	}
	if from, ok := rangeStart(f.From); ok {
		where = append(where, effectiveTimeExpr+" >= ?")
		args = append(args, from)
	}
	if to, ok := rangeEnd(f.To); ok {
		where = append(where, effectiveTimeExpr+" <= ?")
		args = append(args, to)
	}
	return s.queryAssets(ctx, where, args, f.Sort, f.Page, f.PageSize)
}

// queryAssets 是 ListAssets 与智能相册共用的分页查询实现。
func (s *Store) queryAssets(ctx context.Context, where []string, args []any, sort string, page, pageSize int) (AssetPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 500 {
		pageSize = 500
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM assets a WHERE `+clause, args...).Scan(&total); err != nil {
		return AssetPage{}, fmt.Errorf("统计 assets: %w", err)
	}

	order, ok := assetSorts[sort]
	if !ok {
		order = assetSorts["taken_at_desc"]
	}
	query := `SELECT ` + assetColumns + ` FROM assets a WHERE ` + clause +
		` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)

	rows, err := s.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return AssetPage{}, fmt.Errorf("查询 assets: %w", err)
	}
	defer rows.Close()

	items := make([]Asset, 0, pageSize)
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return AssetPage{}, err
		}
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return AssetPage{}, err
	}
	return AssetPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// likePattern 转义 LIKE 通配符，配合 ESCAPE '\' 使用。
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// rangeStart 把 YYYY / YYYY-MM / YYYY-MM-DD 转成闭区间下界。
func rangeStart(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	switch len(v) {
	case 4:
		return v + "-01-01T00:00:00", true
	case 7:
		return v + "-01T00:00:00", true
	case 10:
		return v + "T00:00:00", true
	default:
		return "", false
	}
}

// rangeEnd 把 YYYY / YYYY-MM / YYYY-MM-DD 转成闭区间上界（含整月/整年）。
func rangeEnd(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	switch len(v) {
	case 4:
		return v + "-12-31T23:59:59", true
	case 7:
		t, err := time.Parse("2006-01", v)
		if err != nil {
			return "", false
		}
		lastDay := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
		return fmt.Sprintf("%s-%02dT23:59:59", v, lastDay), true
	case 10:
		return v + "T23:59:59", true
	default:
		return "", false
	}
}

func emptyToNil(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// requireAffected 在期望命中行数不符时返回 sql.ErrNoRows。
func requireAffected(res sql.Result, want int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != want {
		return sql.ErrNoRows
	}
	return nil
}
