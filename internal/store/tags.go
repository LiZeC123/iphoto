package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrTagExists 表示同名标签已存在（大小写不敏感）。
var ErrTagExists = errors.New("标签已存在")

// Tag 是扁平标签及其图片数。
type Tag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ListTags 返回全部标签与 ready 图片计数，按名称排序。
func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, count(a.hash)
		FROM tags t
		LEFT JOIN asset_tags at ON at.tag_id = t.id
		LEFT JOIN assets a ON a.hash = at.hash AND a.status = 'ready'
		GROUP BY t.id, t.name
		ORDER BY t.name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询标签: %w", err)
	}
	defer rows.Close()

	out := make([]Tag, 0, 32)
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Count); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// CountUntagged 统计没有任何标签的 ready 图片数量，供标签页固定首项展示。
func (s *Store) CountUntagged(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM assets a
		WHERE a.status = 'ready'
			AND NOT EXISTS (SELECT 1 FROM asset_tags at WHERE at.hash = a.hash)`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("统计未标记图片: %w", err)
	}
	return n, nil
}

// CreateTag 新建标签；已存在（忽略大小写）时返回 ErrTagExists。
func (s *Store) CreateTag(ctx context.Context, name string) (Tag, error) {
	name, err := normalizeTagName(name)
	if err != nil {
		return Tag{}, err
	}
	var existing int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, name).Scan(&existing)
	if err == nil {
		return Tag{}, ErrTagExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Tag{}, fmt.Errorf("检查标签: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO tags (name) VALUES (?)`, name)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Tag{}, ErrTagExists
		}
		return Tag{}, fmt.Errorf("创建标签: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Tag{}, err
	}
	return Tag{ID: id, Name: name}, nil
}

// RenameTag 重命名标签。
func (s *Store) RenameTag(ctx context.Context, id int64, name string) (Tag, error) {
	name, err := normalizeTagName(name)
	if err != nil {
		return Tag{}, err
	}
	var conflict int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ? AND id <> ?`, name, id).Scan(&conflict)
	if err == nil {
		return Tag{}, ErrTagExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Tag{}, fmt.Errorf("检查标签: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE tags SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return Tag{}, fmt.Errorf("重命名标签: %w", err)
	}
	if err := requireAffected(res, 1); err != nil {
		return Tag{}, err
	}
	return Tag{ID: id, Name: name}, nil
}

// DeleteTag 删除标签及其关联。
func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除标签: %w", err)
	}
	return requireAffected(res, 1)
}

// TagsForAsset 返回某张图片的标签。
func (s *Store) TagsForAsset(ctx context.Context, hash string) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name FROM tags t
		JOIN asset_tags at ON at.tag_id = t.id
		WHERE at.hash = ? ORDER BY t.name COLLATE NOCASE ASC`, hash)
	if err != nil {
		return nil, fmt.Errorf("查询图片标签: %w", err)
	}
	defer rows.Close()

	out := make([]Tag, 0, 8)
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

// AddTagsToAssets 给多张图片批量添加标签（标签不存在则创建）。
func (s *Store) AddTagsToAssets(ctx context.Context, hashes, names []string) error {
	if len(hashes) == 0 || len(names) == 0 {
		return fmt.Errorf("hashes 与 add_tags 均不能为空")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("添加标签: %w", err)
	}
	defer tx.Rollback()

	tagIDs := make([]int64, 0, len(names))
	for _, raw := range names {
		name, err := normalizeTagName(raw)
		if err != nil {
			return err
		}
		var id int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			res, insErr := tx.ExecContext(ctx, `INSERT INTO tags (name) VALUES (?)`, name)
			if insErr != nil {
				return fmt.Errorf("创建标签 %q: %w", name, insErr)
			}
			id, err = res.LastInsertId()
			if err != nil {
				return err
			}
		} else if err != nil {
			return fmt.Errorf("查询标签: %w", err)
		}
		tagIDs = append(tagIDs, id)
	}

	for _, hash := range hashes {
		for _, tagID := range tagIDs {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO asset_tags (hash, tag_id) VALUES (?, ?)`, hash, tagID); err != nil {
				return fmt.Errorf("关联标签: %w", err)
			}
		}
	}
	return tx.Commit()
}

// RemoveTagsFromAssets 批量移除标签。
func (s *Store) RemoveTagsFromAssets(ctx context.Context, hashes, names []string) error {
	if len(hashes) == 0 || len(names) == 0 {
		return fmt.Errorf("hashes 与 remove_tags 均不能为空")
	}
	normalized := make([]any, 0, len(names))
	for _, raw := range names {
		name, err := normalizeTagName(raw)
		if err != nil {
			return err
		}
		normalized = append(normalized, name)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(normalized)), ",")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("移除标签: %w", err)
	}
	defer tx.Rollback()
	for _, hash := range hashes {
		args := append([]any{hash}, normalized...)
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM asset_tags WHERE hash = ? AND tag_id IN
				(SELECT id FROM tags WHERE name IN (`+placeholders+`))`, args...); err != nil {
			return fmt.Errorf("移除标签: %w", err)
		}
	}
	return tx.Commit()
}

func normalizeTagName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("标签名不能为空")
	}
	if len(name) > 64 {
		return "", fmt.Errorf("标签名过长（最多 64 字节）")
	}
	return name, nil
}

// TagsSummary 统计所选图片的标签覆盖数量（count = 有多少张图带该标签），
// 用于批量移除标签时展示“选中图片已有的标签”。
func (s *Store) TagsSummary(ctx context.Context, hashes []string) ([]Tag, error) {
	if len(hashes) == 0 {
		return []Tag{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(hashes)), ",")
	args := make([]any, 0, len(hashes))
	for _, hash := range hashes {
		args = append(args, hash)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, count(*)
		FROM asset_tags at JOIN tags t ON t.id = at.tag_id
		WHERE at.hash IN (`+placeholders+`)
		GROUP BY t.id, t.name
		ORDER BY count(*) DESC, t.name COLLATE NOCASE ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("统计所选标签: %w", err)
	}
	defer rows.Close()

	out := make([]Tag, 0, 16)
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Count); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}
