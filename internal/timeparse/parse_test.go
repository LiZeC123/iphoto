package timeparse

import (
	"testing"
	"time"
)

func ptr(t time.Time) *time.Time { return &t }

func TestParsePriority(t *testing.T) {
	exifAt := time.Date(2019, 5, 6, 7, 8, 9, 0, time.UTC)
	mod := time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local)
	imported := time.Date(2024, 3, 4, 5, 6, 7, 0, time.Local)
	rules := []Rule{{ID: 7, Pattern: `IMG_(?P<year>\d{4})(?P<month>\d{2})(?P<day>\d{2})`}}

	res := Parse("IMG_20230102.jpg", ptr(exifAt), "DateTimeOriginal", mod, imported, rules)
	if res.Source != "exif:DateTimeOriginal" || res.At.Year() != 2019 || res.At.Day() != 6 {
		t.Fatalf("EXIF 优先失败: %+v", res)
	}

	res = Parse("IMG_20230102.jpg", nil, "", mod, imported, rules)
	if res.Source != "filename_rule:7" || Format(res.At) != "2023-01-02T00:00:00" {
		t.Fatalf("文件名规则失败: %+v", res)
	}

	res = Parse("random.jpg", nil, "", mod, imported, rules)
	if res.Source != "file_mtime" || Format(res.At) != "2020-01-02T03:04:05" {
		t.Fatalf("mtime 回退失败: %+v", res)
	}

	res = Parse("random.jpg", nil, "", time.Time{}, imported, nil)
	if res.Source != "import" || Format(res.At) != "2024-03-04T05:06:07" {
		t.Fatalf("导入时间兜底失败: %+v", res)
	}
}

func TestParseInvalidDateFallsThrough(t *testing.T) {
	rules := []Rule{{ID: 1, Pattern: `D(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})`}}
	mod := time.Date(2021, 6, 7, 8, 9, 10, 0, time.Local)
	res := Parse("D2023-02-30.jpg", nil, "", mod, mod, rules)
	if res.Source != "file_mtime" {
		t.Fatalf("非法日期应跳过规则，got %+v", res)
	}
}

func TestParsePartialComponents(t *testing.T) {
	rules := []Rule{{ID: 3, Pattern: `P(?P<year>\d{4})_(?P<month>\d{2})`}}
	res := Parse("P2022_11.jpg", nil, "", time.Time{}, time.Now(), rules)
	if Format(res.At) != "2022-11-01T00:00:00" {
		t.Fatalf("缺省 day 应为 01: %s", Format(res.At))
	}
}

func TestTestPattern(t *testing.T) {
	got, ok, err := TestPattern(`IMG_(?P<year>\d{4})(?P<month>\d{2})(?P<day>\d{2})_(?P<hour>\d{2})(?P<minute>\d{2})(?P<second>\d{2})`, "IMG_20231122_190102.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != "2023-11-22T19:01:02" {
		t.Fatalf("TestPattern = %q, %v", got, ok)
	}

	if _, _, err := TestPattern(`IMG_(\d{4})`, "IMG_2023.jpg"); err == nil {
		t.Fatal("缺少 year 命名组应报错")
	}
	if _, _, err := TestPattern(`(?P<year>\d{4})`, "nope"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := TestPattern(`(?P<year>\d{4})`, "nope"); ok {
		t.Fatal("不应匹配")
	}
}

func TestValidatePatternUnknownGroup(t *testing.T) {
	if err := ValidatePattern(`(?P<year>\d{4})(?P<foo>\d{2})`); err == nil {
		t.Fatal("未知命名组应报错")
	}
}
