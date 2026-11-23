package search

import (
	"strings"
	"testing"
)

// dump 把语法树还原成带括号的规范字符串，便于断言结构。
func dump(n Node) string {
	switch v := n.(type) {
	case nil:
		return ""
	case *Term:
		if v.Field == FieldAny {
			return v.Value
		}
		return string(v.Field) + "=" + v.Value
	case *And:
		return "(" + dump(v.Left) + " AND " + dump(v.Right) + ")"
	case *Or:
		return "(" + dump(v.Left) + " OR " + dump(v.Right) + ")"
	case *Not:
		return "NOT " + dump(v.Node)
	default:
		return "?"
	}
}

func TestParseExpressions(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"cat", "cat"},
		{"cat dog", "(cat AND dog)"},
		{"cat AND dog", "(cat AND dog)"},
		{"cat OR dog", "(cat OR dog)"},
		{"cat OR dog AND bird", "(cat OR (dog AND bird))"},
		{"(cat OR dog) AND bird", "((cat OR dog) AND bird)"},
		{"NOT cat", "NOT cat"},
		{"-cat", "NOT cat"},
		{"cat -dog", "(cat AND NOT dog)"},
		{"NOT cat OR dog", "(NOT cat OR dog)"},
		{"NOT (cat OR dog)", "NOT (cat OR dog)"},
		{"tag:旅行", "tag=旅行"},
		{"TAG:旅行", "tag=旅行"},
		{"file:beach", "name=beach"},
		{"note:夏天", "note=夏天"},
		{`"beach day"`, "beach day"},
		{`tag:"长途 旅行"`, "tag=长途 旅行"},
		{"tag:旅行 AND NOT name:截图", "(tag=旅行 AND NOT name=截图)"},
		{"IMG-2023", "IMG-2023"},
		{"2023-05", "2023-05"},
		{`"AND"`, "AND"}, // 引号内按字面值处理
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tc.in, err)
			continue
		}
		if d := dump(got); d != tc.want {
			t.Errorf("Parse(%q) = %q, want %q", tc.in, d, tc.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		"and",
		"AND",
		"cat AND",
		"cat OR",
		"(cat",
		"cat)",
		"NOT",
		"-",
		"- ",
		`"cat`,
		"tag:",
		"cat AND OR dog",
	}
	for _, in := range cases {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) 应该报错", in)
		}
	}
}

func TestParseTooLong(t *testing.T) {
	if _, err := Parse(strings.Repeat("a", MaxQueryLength+1)); err == nil {
		t.Fatal("超长表达式应报错")
	}
}
