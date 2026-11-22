package store

import (
	"context"
	"fmt"
	"strings"
)

// TimeRuleDetail 是规则管理页使用的完整字段。
type TimeRuleDetail struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Pattern  string `json:"pattern"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
	Builtin  bool   `json:"builtin"`
}

func scanTimeRule(row rowScanner) (TimeRuleDetail, error) {
	var (
		r       TimeRuleDetail
		enabled int
		builtin int
	)
	if err := row.Scan(&r.ID, &r.Name, &r.Pattern, &r.Priority, &enabled, &builtin); err != nil {
		return TimeRuleDetail{}, err
	}
	r.Enabled = enabled == 1
	r.Builtin = builtin == 1
	return r, nil
}

// ListTimeRules 返回全部规则（含禁用的），按优先级排序。
func (s *Store) ListTimeRules(ctx context.Context) ([]TimeRuleDetail, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, pattern, priority, enabled, builtin
		FROM time_parse_rules ORDER BY priority ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询时间规则: %w", err)
	}
	defer rows.Close()

	out := make([]TimeRuleDetail, 0, 16)
	for rows.Next() {
		r, err := scanTimeRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetTimeRule 读取单条规则。
func (s *Store) GetTimeRule(ctx context.Context, id int64) (TimeRuleDetail, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, pattern, priority, enabled, builtin FROM time_parse_rules WHERE id = ?`, id)
	return scanTimeRule(row)
}

// CreateTimeRule 新建规则。
func (s *Store) CreateTimeRule(ctx context.Context, name, pattern string, priority int, enabled bool) (TimeRuleDetail, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO time_parse_rules (name, pattern, priority, enabled, builtin) VALUES (?, ?, ?, ?, 0)`,
		name, pattern, priority, boolToInt(enabled))
	if err != nil {
		return TimeRuleDetail{}, fmt.Errorf("创建时间规则: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return TimeRuleDetail{}, err
	}
	return s.GetTimeRule(ctx, id)
}

// UpdateTimeRule 更新规则；nil 表示不修改。
func (s *Store) UpdateTimeRule(ctx context.Context, id int64, name, pattern *string, priority *int, enabled *bool) (TimeRuleDetail, error) {
	sets := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *name)
	}
	if pattern != nil {
		sets = append(sets, "pattern = ?")
		args = append(args, *pattern)
	}
	if priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *priority)
	}
	if enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*enabled))
	}
	if len(sets) == 0 {
		return s.GetTimeRule(ctx, id)
	}
	args = append(args, id)
	if _, err := s.db.ExecContext(ctx, `UPDATE time_parse_rules SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return TimeRuleDetail{}, fmt.Errorf("更新时间规则: %w", err)
	}
	return s.GetTimeRule(ctx, id)
}

// DeleteTimeRule 删除规则（内置规则也允许删除，由用户决定）。
func (s *Store) DeleteTimeRule(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM time_parse_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("删除时间规则: %w", err)
	}
	return requireAffected(res, 1)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
