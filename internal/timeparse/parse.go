// Package timeparse 按 dev.md 第 7 章的优先级确定拍摄时间：
// 手动覆盖（读取时生效）> EXIF > 文件名规则 > 文件修改时间 > 导入时间。
package timeparse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TimeLayout 与数据库存储格式一致（本地时间、无时区）。
const TimeLayout = "2006-01-02T15:04:05"

// Rule 是文件名解析规则，ID 用于生成 taken_at_source。
type Rule struct {
	ID      int64
	Pattern string
}

// Result 是解析结果。
type Result struct {
	At     time.Time
	Source string
}

// Parse 依次尝试 EXIF、文件名规则、文件修改时间、导入时间。
// exifAt 为 nil 时跳过 EXIF；importedAt 作为最终兜底。
func Parse(filename string, exifAt *time.Time, exifSource string, modTime time.Time, importedAt time.Time, rules []Rule) Result {
	if exifAt != nil {
		src := "exif"
		if exifSource != "" {
			src = "exif:" + exifSource
		}
		return Result{At: truncate(*exifAt), Source: src}
	}

	base := filename
	for _, r := range rules {
		if t, ok := matchRule(r.Pattern, base); ok {
			return Result{At: t, Source: fmt.Sprintf("filename_rule:%d", r.ID)}
		}
	}

	if !modTime.IsZero() {
		return Result{At: truncate(modTime), Source: "file_mtime"}
	}
	return Result{At: truncate(importedAt), Source: "import"}
}

// Format 输出库内时间字符串。
func Format(t time.Time) string { return t.Format(TimeLayout) }

func truncate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*regexp.Regexp{}
)

func compile(pattern string) (*regexp.Regexp, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if re, ok := cache[pattern]; ok {
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	cache[pattern] = re
	return re, nil
}

// matchRule 用命名捕获组 year/month/day/hour/minute/second 解析文件名。
// 日期非法（如 2023-02-30）视为不匹配，继续尝试下一条规则。
func matchRule(pattern, filename string) (time.Time, bool) {
	re, err := compile(pattern)
	if err != nil {
		return time.Time{}, false
	}
	m := re.FindStringSubmatch(filename)
	if m == nil {
		return time.Time{}, false
	}

	get := func(name string) (int, bool) {
		idx := re.SubexpIndex(name)
		if idx <= 0 || idx >= len(m) || m[idx] == "" {
			return 0, false
		}
		v, err := strconv.Atoi(m[idx])
		if err != nil {
			return 0, false
		}
		return v, true
	}

	year, ok := get("year")
	if !ok {
		return time.Time{}, false
	}
	month, ok := get("month")
	if !ok {
		month = 1
	}
	day, ok := get("day")
	if !ok {
		day = 1
	}
	hour, _ := get("hour")
	minute, _ := get("minute")
	second, _ := get("second")

	t := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day ||
		t.Hour() != hour || t.Minute() != minute || t.Second() != second {
		return time.Time{}, false
	}
	return t, true
}

// ValidatePattern 供规则测试接口使用：编译并返回命名捕获组是否齐全。
func ValidatePattern(pattern string) error {
	re, err := compile(pattern)
	if err != nil {
		return fmt.Errorf("正则无效: %w", err)
	}
	if re.SubexpIndex("year") <= 0 {
		return fmt.Errorf("规则缺少命名捕获组 year")
	}
	names := re.SubexpNames()
	known := map[string]bool{"year": true, "month": true, "day": true, "hour": true, "minute": true, "second": true}
	for _, n := range names {
		if n == "" {
			continue
		}
		if !known[n] {
			return fmt.Errorf("未知命名捕获组: %s", n)
		}
	}
	return nil
}

// TestPattern 用给定文件名试跑规则，返回格式化时间与是否匹配。
func TestPattern(pattern, filename string) (string, bool, error) {
	if err := ValidatePattern(pattern); err != nil {
		return "", false, err
	}
	t, ok := matchRule(pattern, filename)
	if !ok {
		return "", false, nil
	}
	return strings.TrimSpace(t.Format(TimeLayout)), true, nil
}
