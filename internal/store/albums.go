package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrAlbumType 表示相册类型非法。
var ErrAlbumType = errors.New("相册类型必须为 manual 或 smart")

// Album 对应 albums 表，Count 为可见成员数。
type Album struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description *string `json:"description"`
	RuleJSON    *string `json:"rule_json"`
	CoverHash   *string `json:"cover_hash"`
	CreatedAt   string  `json:"created_at"`
	Count       int     `json:"count"`
}

const albumColumns = `a.id, a.name, a.type, a.description, a.rule_json, a.cover_hash, a.created_at`

func scanAlbum(row rowScanner) (Album, error) {
	var (
		album Album
		desc  sql.NullString
		rule  sql.NullString
		cover sql.NullString
	)
	if err := row.Scan(&album.ID, &album.Name, &album.Type, &desc, &rule, &cover, &album.CreatedAt); err != nil {
		return Album{}, err
	}
	album.Description = nullStringPtr(desc)
	album.RuleJSON = nullStringPtr(rule)
	album.CoverHash = nullStringPtr(cover)
	return album, nil
}

// ListAlbums 支持名称/描述搜索与类型过滤，并计算每个相册的图片数。
func (s *Store) ListAlbums(ctx context.Context, q, typ string) ([]Album, error) {
	where := []string{"1 = 1"}
	args := make([]any, 0, 3)
	if q = strings.TrimSpace(q); q != "" {
		like := likePattern(q)
		where = append(where, `(a.name LIKE ? ESCAPE '\' OR COALESCE(a.description, '') LIKE ? ESCAPE '\')`)
		args = append(args, like, like)
	}
	if typ != "" {
		if typ != "manual" && typ != "smart" {
			return nil, ErrAlbumType
		}
		where = append(where, "a.type = ?")
		args = append(args, typ)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+albumColumns+` FROM albums a WHERE `+strings.Join(where, " AND ")+
			` ORDER BY a.created_at DESC, a.id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("查询相册: %w", err)
	}
	defer rows.Close()

	out := make([]Album, 0, 16)
	for rows.Next() {
		album, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, album)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		count, err := s.albumCount(ctx, out[i])
		if err != nil {
			return nil, err
		}
		out[i].Count = count
		cover, err := s.albumCover(ctx, out[i])
		if err != nil {
			return nil, err
		}
		if cover != nil {
			out[i].CoverHash = cover
		}
	}
	return out, nil
}

// GetAlbum 读取单个相册并补齐计数。
func (s *Store) GetAlbum(ctx context.Context, id int64) (Album, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+albumColumns+` FROM albums a WHERE a.id = ?`, id)
	album, err := scanAlbum(row)
	if err != nil {
		return Album{}, err
	}
	album.Count, err = s.albumCount(ctx, album)
	if err != nil {
		return Album{}, err
	}
	if album.CoverHash == nil {
		cover, err := s.albumCover(ctx, album)
		if err != nil {
			return Album{}, err
		}
		album.CoverHash = cover
	}
	return album, nil
}

// CreateAlbum 创建普通或智能相册；智能相册必须提供合法规则。
func (s *Store) CreateAlbum(ctx context.Context, name, typ string, description, ruleJSON *string) (Album, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Album{}, fmt.Errorf("相册名不能为空")
	}
	if typ != "manual" && typ != "smart" {
		return Album{}, ErrAlbumType
	}
	if typ == "smart" {
		if ruleJSON == nil || strings.TrimSpace(*ruleJSON) == "" {
			return Album{}, fmt.Errorf("智能相册需要 rule_json")
		}
		if err := ValidateSmartRule(*ruleJSON); err != nil {
			return Album{}, err
		}
	}
	now := nowString()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO albums (name, type, description, rule_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		name, typ, description, ruleJSON, now)
	if err != nil {
		return Album{}, fmt.Errorf("创建相册: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Album{}, err
	}
	return s.GetAlbum(ctx, id)
}

// UpdateAlbum 更新名称/描述/规则，nil 表示不修改。
func (s *Store) UpdateAlbum(ctx context.Context, id int64, name, description, ruleJSON *string) (Album, error) {
	album, err := s.GetAlbum(ctx, id)
	if err != nil {
		return Album{}, err
	}
	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return Album{}, fmt.Errorf("相册名不能为空")
		}
		sets = append(sets, "name = ?")
		args = append(args, trimmed)
	}
	if description != nil {
		sets = append(sets, "description = ?")
		args = append(args, emptyToNil(*description))
	}
	if ruleJSON != nil {
		if album.Type != "smart" {
			return Album{}, fmt.Errorf("普通相册不支持 rule_json")
		}
		if err := ValidateSmartRule(*ruleJSON); err != nil {
			return Album{}, err
		}
		sets = append(sets, "rule_json = ?")
		args = append(args, *ruleJSON)
	}
	if len(sets) == 0 {
		return album, nil
	}
	args = append(args, id)
	if _, err := s.db.ExecContext(ctx, `UPDATE albums SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return Album{}, fmt.Errorf("更新相册: %w", err)
	}
	return s.GetAlbum(ctx, id)
}

// DeleteAlbum 删除相册（成员关系级联删除）。
func (s *Store) DeleteAlbum(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除相册: %w", err)
	}
	return requireAffected(res, 1)
}

// AddAlbumItems 向普通相册加入图片，已存在的自动忽略。
func (s *Store) AddAlbumItems(ctx context.Context, albumID int64, hashes []string) (int, error) {
	album, err := s.GetAlbum(ctx, albumID)
	if err != nil {
		return 0, err
	}
	if album.Type != "manual" {
		return 0, fmt.Errorf("智能相册成员由规则动态决定，不能手动添加")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := nowString()
	added := 0
	for _, hash := range hashes {
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO album_items (album_id, hash, position, added_at) VALUES (?, ?, 0, ?)`,
			albumID, hash, now)
		if err != nil {
			return added, fmt.Errorf("加入相册: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	if err := tx.Commit(); err != nil {
		return added, err
	}
	return added, nil
}

// RemoveAlbumItems 从普通相册移除图片。
func (s *Store) RemoveAlbumItems(ctx context.Context, albumID int64, hashes []string) (int, error) {
	removed := 0
	for _, hash := range hashes {
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM album_items WHERE album_id = ? AND hash = ?`, albumID, hash)
		if err != nil {
			return removed, fmt.Errorf("移出相册: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			removed++
		}
	}
	return removed, nil
}

// ListAlbumItems 分页返回普通相册成员，可按有效拍摄时间过滤。
func (s *Store) ListAlbumItems(ctx context.Context, albumID int64, from, to string, page, pageSize int) (AssetPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 500 {
		pageSize = 500
	}

	where := []string{"ai.album_id = ?", "a.status = 'ready'"}
	args := []any{albumID}
	where, args = appendTimeRange(where, args, from, to)
	clause := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM album_items ai JOIN assets a ON a.hash = ai.hash
		WHERE `+clause, args...).Scan(&total); err != nil {
		return AssetPage{}, fmt.Errorf("统计相册成员: %w", err)
	}

	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+assetColumns+`
		FROM album_items ai JOIN assets a ON a.hash = ai.hash
		WHERE `+clause+`
		ORDER BY ai.position ASC, ai.added_at ASC, a.hash
		LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return AssetPage{}, fmt.Errorf("查询相册成员: %w", err)
	}
	defer rows.Close()

	items := make([]Asset, 0, pageSize)
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return AssetPage{}, err
		}
		items = append(items, asset)
	}
	return AssetPage{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

// AlbumMonths 聚合相册内图片的年月及计数，用于相册详情的时间筛选。
func (s *Store) AlbumMonths(ctx context.Context, albumID int64) ([]MonthCount, error) {
	album, err := s.GetAlbum(ctx, albumID)
	if err != nil {
		return nil, err
	}

	monthExpr := `substr(COALESCE(a.taken_at_override, a.taken_at), 1, 7)`
	monthFilter := `COALESCE(a.taken_at_override, a.taken_at) IS NOT NULL
		AND length(COALESCE(a.taken_at_override, a.taken_at)) >= 7`

	var rows *sql.Rows
	if album.Type == "smart" {
		if album.RuleJSON == nil {
			return []MonthCount{}, nil
		}
		root, err := parseSmartRule(*album.RuleJSON)
		if err != nil {
			return nil, err
		}
		clause, args, err := buildSmartWhere(root)
		if err != nil {
			return nil, err
		}
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+monthExpr+` AS month, count(*)
			FROM assets a
			WHERE a.status = 'ready' AND `+clause+` AND `+monthFilter+`
			GROUP BY month ORDER BY month DESC`, args...)
		if err != nil {
			return nil, fmt.Errorf("聚合相册月份: %w", err)
		}
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT `+monthExpr+` AS month, count(*)
			FROM album_items ai JOIN assets a ON a.hash = ai.hash
			WHERE ai.album_id = ? AND a.status = 'ready' AND `+monthFilter+`
			GROUP BY month ORDER BY month DESC`, albumID)
		if err != nil {
			return nil, fmt.Errorf("聚合相册月份: %w", err)
		}
	}
	defer rows.Close()

	out := make([]MonthCount, 0, 24)
	for rows.Next() {
		var m MonthCount
		if err := rows.Scan(&m.Month, &m.Count); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// appendTimeRange 把 from/to 转成闭区间条件。
func appendTimeRange(where []string, args []any, from, to string) ([]string, []any) {
	if start, ok := rangeStart(from); ok {
		where = append(where, effectiveTimeExpr+" >= ?")
		args = append(args, start)
	}
	if end, ok := rangeEnd(to); ok {
		where = append(where, effectiveTimeExpr+" <= ?")
		args = append(args, end)
	}
	return where, args
}

// albumCover 返回相册封面：优先 cover_hash，否则取第一个可见成员。
func (s *Store) albumCover(ctx context.Context, album Album) (*string, error) {
	if album.CoverHash != nil {
		return album.CoverHash, nil
	}
	if album.Type == "manual" {
		var hash string
		err := s.db.QueryRowContext(ctx, `
			SELECT ai.hash FROM album_items ai JOIN assets a ON a.hash = ai.hash
			WHERE ai.album_id = ? AND a.status = 'ready'
			ORDER BY ai.position ASC, ai.added_at ASC LIMIT 1`, album.ID).Scan(&hash)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &hash, nil
	}
	if album.RuleJSON == nil {
		return nil, nil
	}
	page, err := s.SmartAlbumPage(ctx, *album.RuleJSON, "", "", 1, 1)
	if err != nil {
		return nil, err
	}
	if len(page.Items) == 0 {
		return nil, nil
	}
	hash := page.Items[0].Hash
	return &hash, nil
}

func (s *Store) albumCount(ctx context.Context, album Album) (int, error) {
	if album.Type == "smart" {
		if album.RuleJSON == nil {
			return 0, nil
		}
		return s.smartAlbumCount(ctx, *album.RuleJSON)
	}
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM album_items ai JOIN assets a ON a.hash = ai.hash
		WHERE ai.album_id = ? AND a.status = 'ready'`, album.ID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计相册: %w", err)
	}
	return count, nil
}

// AlbumPage 读取相册内容：普通相册读成员表，智能相册按规则动态查询。
// from/to 为可选的 YYYY 或 YYYY-MM 时间筛选。
func (s *Store) AlbumPage(ctx context.Context, albumID int64, from, to string, page, pageSize int) (AssetPage, error) {
	album, err := s.GetAlbum(ctx, albumID)
	if err != nil {
		return AssetPage{}, err
	}
	if album.Type == "smart" {
		if album.RuleJSON == nil {
			return AssetPage{Items: []Asset{}, Total: 0, Page: page, PageSize: pageSize}, nil
		}
		return s.SmartAlbumPage(ctx, *album.RuleJSON, from, to, page, pageSize)
	}
	return s.ListAlbumItems(ctx, albumID, from, to, page, pageSize)
}

func nowString() string {
	return time.Now().Format(TimeLayout)
}
