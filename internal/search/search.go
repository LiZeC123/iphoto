// Package search 解析图库搜索表达式（dev.md 10.1）。
//
// 语法：
//
//	expr    := orExpr
//	orExpr  := andExpr (OR andExpr)*
//	andExpr := unary (AND? unary)*      // 空白即隐式 AND
//	unary   := NOT unary | "-" unary | primary
//	primary := "(" expr ")" | term
//	term    := [字段:]值                // 字段：name/file、note、tag
//	         | "带空格的短语"
//
// 关键字 AND / OR / NOT 大小写不敏感；引号内的内容按字面值处理。
// 值为子串匹配（不区分 ASCII 大小写），空表达式返回 (nil, nil)。
package search

import (
	"fmt"
	"strings"
	"unicode"
)

// Field 是搜索字段范围。
type Field string

const (
	// FieldAny 匹配文件名、备注与标签名（默认）。
	FieldAny Field = ""
	// FieldName 只匹配文件名。
	FieldName Field = "name"
	// FieldNote 只匹配备注。
	FieldNote Field = "note"
	// FieldTag 只匹配标签名。
	FieldTag Field = "tag"
)

// Node 是搜索语法树节点。
type Node interface{ searchNode() }

// Term 是一个子串匹配条件。
type Term struct {
	Field Field
	Value string
}

// And 表示左右两个条件同时成立。
type And struct{ Left, Right Node }

// Or 表示左右任一条件成立。
type Or struct{ Left, Right Node }

// Not 表示条件取反。
type Not struct{ Node Node }

func (*Term) searchNode() {}
func (*And) searchNode()  {}
func (*Or) searchNode()   {}
func (*Not) searchNode()  {}

// MaxQueryLength 是允许的最大查询长度（按 rune 计）。
const MaxQueryLength = 512

type tokenKind int

const (
	tokWord tokenKind = iota
	tokString
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
	tokEOF
)

type token struct {
	kind tokenKind
	text string
}

// Parse 把查询字符串解析成语法树；空查询返回 nil。
func Parse(input string) (Node, error) {
	if len([]rune(input)) > MaxQueryLength {
		return nil, fmt.Errorf("搜索表达式过长（最多 %d 个字符）", MaxQueryLength)
	}
	tokens, err := lex(input)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 1 { // 只有 EOF
		return nil, nil
	}
	p := &parser{tokens: tokens}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if tok := p.peek(); tok.kind != tokEOF {
		return nil, fmt.Errorf("多余的 %q", tok.describe())
	}
	return node, nil
}

func lex(input string) ([]token, error) {
	runes := []rune(input)
	tokens := make([]token, 0, 8)
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			tokens = append(tokens, token{kind: tokLParen, text: "("})
			i++
		case r == ')':
			tokens = append(tokens, token{kind: tokRParen, text: ")"})
			i++
		case r == '"':
			value, next, err := readQuoted(runes, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token{kind: tokString, text: value})
			i = next
		case r == '-':
			// 词首的 - 表示取反；- 后面必须还有内容。
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) || runes[i+1] == ')' {
				return nil, fmt.Errorf("- 后面缺少内容")
			}
			tokens = append(tokens, token{kind: tokNot, text: "-"})
			i++
		default:
			start := i
			for i < len(runes) {
				c := runes[i]
				if unicode.IsSpace(c) || c == '(' || c == ')' || c == '"' {
					break
				}
				i++
			}
			if start == i {
				return nil, fmt.Errorf("无法识别的字符 %q", string(runes[i]))
			}
			word := string(runes[start:i])
			tokens = append(tokens, keywordToken(word))
		}
	}
	tokens = append(tokens, token{kind: tokEOF})
	return tokens, nil
}

// keywordToken 把独立的 AND/OR/NOT 识别为运算符。
func keywordToken(word string) token {
	switch strings.ToUpper(word) {
	case "AND":
		return token{kind: tokAnd, text: word}
	case "OR":
		return token{kind: tokOr, text: word}
	case "NOT":
		return token{kind: tokNot, text: word}
	default:
		return token{kind: tokWord, text: word}
	}
}

func readQuoted(runes []rune, start int) (value string, next int, err error) {
	var b strings.Builder
	for i := start + 1; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			if i+1 >= len(runes) {
				return "", 0, fmt.Errorf("引号内的转义不完整")
			}
			b.WriteRune(runes[i+1])
			i++
		case '"':
			return b.String(), i + 1, nil
		default:
			b.WriteRune(runes[i])
		}
	}
	return "", 0, fmt.Errorf("引号未闭合")
}

func (t token) describe() string {
	if t.kind == tokEOF {
		return "表达式结尾"
	}
	return t.text
}

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token { return p.tokens[p.pos] }

func (p *parser) next() token {
	tok := p.tokens[p.pos]
	if tok.kind != tokEOF {
		p.pos++
	}
	return tok
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Or{Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case tokAnd:
			p.next()
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &And{Left: left, Right: right}
		case tokWord, tokString, tokNot, tokLParen: // 隐式 AND
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &And{Left: left, Right: right}
		default:
			return left, nil
		}
	}
}

func (p *parser) parseUnary() (Node, error) {
	switch p.peek().kind {
	case tokNot:
		p.next()
		node, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Not{Node: node}, nil
	case tokLParen:
		p.next()
		node, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("缺少右括号")
		}
		p.next()
		return node, nil
	case tokWord, tokString:
		return p.parseTerm()
	case tokRParen:
		return nil, fmt.Errorf("多余的右括号")
	case tokAnd, tokOr:
		return nil, fmt.Errorf("%q 前面缺少条件", p.peek().text)
	default:
		return nil, fmt.Errorf("表达式不完整")
	}
}

func (p *parser) parseTerm() (Node, error) {
	tok := p.next()
	if tok.kind == tokString {
		return &Term{Field: FieldAny, Value: tok.text}, nil
	}

	word := tok.text
	if field, rest, ok := splitField(word); ok {
		if rest == "" {
			// 支持 tag:"长途 旅行" 这类写法：字段前缀 + 引号短语。
			if p.peek().kind != tokString {
				return nil, fmt.Errorf("%s: 后面缺少值", word)
			}
			rest = p.next().text
		}
		if rest == "" {
			return nil, fmt.Errorf("%s: 后面缺少值", word)
		}
		return &Term{Field: field, Value: rest}, nil
	}
	return &Term{Field: FieldAny, Value: word}, nil
}

// splitField 识别 name: / file: / note: / tag: 前缀；未知前缀按普通文本处理。
func splitField(word string) (Field, string, bool) {
	idx := strings.IndexByte(word, ':')
	if idx <= 0 {
		return FieldAny, word, false
	}
	prefix := strings.ToLower(word[:idx])
	switch prefix {
	case "name", "file", "filename":
		return FieldName, word[idx+1:], true
	case "note":
		return FieldNote, word[idx+1:], true
	case "tag":
		return FieldTag, word[idx+1:], true
	default:
		return FieldAny, word, false
	}
}
