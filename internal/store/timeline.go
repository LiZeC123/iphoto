package store

import (
	"context"
	"fmt"
)

// MonthCount 是时间线导航的一个年月及其图片数。
type MonthCount struct {
	Month string `json:"month"` // YYYY-MM
	Count int    `json:"count"`
}

// TimelineMonths 聚合 ready 图片的有效拍摄时间，按年月倒序返回。
func (s *Store) TimelineMonths(ctx context.Context) ([]MonthCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT substr(COALESCE(a.taken_at_override, a.taken_at), 1, 7) AS month, count(*)
		FROM assets a
		WHERE a.status = 'ready'
			AND COALESCE(a.taken_at_override, a.taken_at) IS NOT NULL
			AND length(COALESCE(a.taken_at_override, a.taken_at)) >= 7
		GROUP BY month
		ORDER BY month DESC`)
	if err != nil {
		return nil, fmt.Errorf("聚合时间线: %w", err)
	}
	defer rows.Close()

	out := make([]MonthCount, 0, 32)
	for rows.Next() {
		var m MonthCount
		if err := rows.Scan(&m.Month, &m.Count); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
