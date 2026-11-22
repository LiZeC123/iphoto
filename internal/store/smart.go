package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// 智能相册规则 AST（dev.md 9.2）：第一版支持 and + tag + time_range，
// 保留 or / not 等扩展点，遇到未知节点直接报错。
type smartNode struct {
	Op       string      `json:"op"`
	Type     string      `json:"type"`
	Value    string      `json:"value"`
	From     string      `json:"from"`
	To       string      `json:"to"`
	Children []smartNode `json:"children"`
}

// ValidateSmartRule 校验规则 JSON 是否能被解析与执行。
func ValidateSmartRule(ruleJSON string) error {
	_, err := parseSmartRule(ruleJSON)
	return err
}

func parseSmartRule(ruleJSON string) (smartNode, error) {
	var root smartNode
	dec := json.NewDecoder(strings.NewReader(ruleJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&root); err != nil {
		return root, fmt.Errorf("规则 JSON 非法: %w", err)
	}
	if _, _, err := buildSmartWhere(root); err != nil {
		return root, err
	}
	return root, nil
}

func buildSmartWhere(n smartNode) (string, []any, error) {
	switch {
	case n.Op != "":
		if n.Op != "and" {
			return "", nil, fmt.Errorf("暂只支持 and 组合（收到 %q）", n.Op)
		}
		if len(n.Children) == 0 {
			return "", nil, errors.New("and 至少需要一个子条件")
		}
		parts := make([]string, 0, len(n.Children))
		args := make([]any, 0, len(n.Children))
		for _, child := range n.Children {
			clause, childArgs, err := buildSmartWhere(child)
			if err != nil {
				return "", nil, err
			}
			parts = append(parts, clause)
			args = append(args, childArgs...)
		}
		return "(" + strings.Join(parts, " AND ") + ")", args, nil

	case n.Type == "tag":
		value := strings.TrimSpace(n.Value)
		if value == "" {
			return "", nil, errors.New("tag 条件缺少 value")
		}
		return `EXISTS (
			SELECT 1 FROM asset_tags at JOIN tags t ON t.id = at.tag_id
			WHERE at.hash = a.hash AND t.name = ? COLLATE NOCASE)`, []any{value}, nil

	case n.Type == "time_range":
		parts := make([]string, 0, 2)
		args := make([]any, 0, 2)
		if from, ok := rangeStart(n.From); ok {
			parts = append(parts, effectiveTimeExpr+" >= ?")
			args = append(args, from)
		}
		if to, ok := rangeEnd(n.To); ok {
			parts = append(parts, effectiveTimeExpr+" <= ?")
			args = append(args, to)
		}
		if len(parts) == 0 {
			return "", nil, errors.New("time_range 需要合法的 from 或 to（YYYY 或 YYYY-MM）")
		}
		return "(" + strings.Join(parts, " AND ") + ")", args, nil

	default:
		if n.Type == "" {
			return "", nil, errors.New("规则节点缺少 op 或 type")
		}
		return "", nil, fmt.Errorf("未知规则节点类型: %q", n.Type)
	}
}

func (s *Store) smartAlbumCount(ctx context.Context, ruleJSON string) (int, error) {
	root, err := parseSmartRule(ruleJSON)
	if err != nil {
		return 0, err
	}
	clause, args, err := buildSmartWhere(root)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM assets a WHERE a.status = 'ready' AND `+clause, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计智能相册: %w", err)
	}
	return count, nil
}

// SmartAlbumPage 按规则动态查询智能相册成员。
func (s *Store) SmartAlbumPage(ctx context.Context, ruleJSON, from, to string, page, pageSize int) (AssetPage, error) {
	root, err := parseSmartRule(ruleJSON)
	if err != nil {
		return AssetPage{}, err
	}
	clause, args, err := buildSmartWhere(root)
	if err != nil {
		return AssetPage{}, err
	}
	where := []string{"a.status = 'ready'", clause}
	where, args = appendTimeRange(where, args, from, to)
	return s.queryAssets(ctx, where, args, "taken_at_desc", page, pageSize)
}
