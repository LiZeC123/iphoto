package store

import (
	"errors"

	"iphoto/internal/search"
)

// ErrSearchSyntax 表示图库搜索表达式无法解析（调用方应返回 400）。
var ErrSearchSyntax = errors.New("搜索表达式有误")

// buildSearchWhere 把搜索语法树编译成 SQL 条件（使用别名 a 指向 assets）。
func buildSearchWhere(node search.Node) (string, []any) {
	switch n := node.(type) {
	case *search.Term:
		return termWhere(n)
	case *search.And:
		left, leftArgs := buildSearchWhere(n.Left)
		right, rightArgs := buildSearchWhere(n.Right)
		args := append(leftArgs, rightArgs...)
		return "(" + left + " AND " + right + ")", args
	case *search.Or:
		left, leftArgs := buildSearchWhere(n.Left)
		right, rightArgs := buildSearchWhere(n.Right)
		args := append(leftArgs, rightArgs...)
		return "(" + left + " OR " + right + ")", args
	case *search.Not:
		inner, args := buildSearchWhere(n.Node)
		return "(NOT " + inner + ")", args
	default:
		return "1 = 1", nil
	}
}

func termWhere(t *search.Term) (string, []any) {
	like := likePattern(t.Value)
	tagExists := `EXISTS (
		SELECT 1 FROM asset_tags at JOIN tags t ON t.id = at.tag_id
		WHERE at.hash = a.hash AND t.name LIKE ? ESCAPE '\')`
	switch t.Field {
	case search.FieldName:
		return `a.original_name LIKE ? ESCAPE '\'`, []any{like}
	case search.FieldNote:
		return `COALESCE(a.note, '') LIKE ? ESCAPE '\'`, []any{like}
	case search.FieldTag:
		return tagExists, []any{like}
	default:
		return `(a.original_name LIKE ? ESCAPE '\'
			OR COALESCE(a.note, '') LIKE ? ESCAPE '\'
			OR ` + tagExists + `)`, []any{like, like, like}
	}
}
