package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TimeRule 是时间解析规则（dev.md 4.4）。
type TimeRule struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Pattern  string `json:"pattern"`
	Priority int    `json:"priority"`
}

// EnabledTimeRules 按 priority 升序返回启用的规则。
func (s *Store) EnabledTimeRules(ctx context.Context) ([]TimeRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, pattern, priority FROM time_parse_rules
		WHERE enabled = 1 ORDER BY priority ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("读取时间规则: %w", err)
	}
	defer rows.Close()

	var out []TimeRule
	for rows.Next() {
		var r TimeRule
		if err := rows.Scan(&r.ID, &r.Name, &r.Pattern, &r.Priority); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
