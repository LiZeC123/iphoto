// Command timecheck 在导入前预检时间解析结果。
//
// 用法：
//
//	iphoto-timecheck [-db library/library.sqlite] [-json] <目录>
//
// 它使用与导入完全相同的扫描过滤与时间解析逻辑（EXIF → 文件名规则 → 文件修改时间 → 导入时间），
// 从 library.sqlite 读取启用的规则；数据库不存在时回退到内置规则，便于在首次导入前预检。
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"

	"iphoto/internal/importer"
	"iphoto/internal/media"
	"iphoto/internal/timeparse"
)

// 与 schema 内置规则保持一致，作为无数据库时的兜底。
var builtinRules = []timeparse.Rule{
	{ID: 0, Pattern: `IMG_(?P<year>\d{4})(?P<month>\d{2})(?P<day>\d{2})_(?P<hour>\d{2})(?P<minute>\d{2})(?P<second>\d{2})`},
	{ID: 0, Pattern: `Screenshot_(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})-(?P<hour>\d{2})-(?P<minute>\d{2})-(?P<second>\d{2})`},
}

type result struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	TakenAt  string `json:"taken_at,omitempty"`
	Source   string `json:"source,omitempty"`
	Fallback bool   `json:"fallback"`
	Error    string `json:"error,omitempty"`
}

type output struct {
	Dir         string   `json:"dir"`
	RulesSource string   `json:"rules_source"`
	Total       int      `json:"total"`
	Fallbacks   int      `json:"fallbacks"`
	Items       []result `json:"items"`
}

func main() {
	dbPath := flag.String("db", "library/library.sqlite", "SQLite 库路径（读取启用的时间解析规则）")
	asJSON := flag.Bool("json", false, "以 JSON 输出")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "用法: iphoto-timecheck [-db library/library.sqlite] [-json] <目录>\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	root := flag.Arg(0)
	rules, rulesSource := loadRules(*dbPath)

	scan, err := importer.Scan(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "扫描失败:", err)
		os.Exit(1)
	}
	if scan.Unreadable > 0 {
		fmt.Fprintf(os.Stderr, "提示：%d 个路径不可读，已跳过\n", scan.Unreadable)
	}

	now := time.Now()
	items := make([]result, 0, len(scan.Files))
	fallbacks := 0
	for _, path := range scan.Files {
		item := result{Path: path, Name: filepath.Base(path)}
		info, inspectErr := media.Inspect(path)
		if inspectErr != nil {
			item.Error = inspectErr.Error()
			items = append(items, item)
			continue
		}
		var modTime time.Time
		if stat, statErr := os.Stat(path); statErr == nil {
			modTime = stat.ModTime()
		}
		resolved := timeparse.Parse(filepath.Base(path), info.TakenAt, info.TakenAtTag, modTime, now, rules)
		item.TakenAt = timeparse.Format(resolved.At)
		item.Source = resolved.Source
		item.Fallback = resolved.Source == "import"
		if item.Fallback {
			fallbacks++
		}
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	out := output{
		Dir:         scan.Path,
		RulesSource: rulesSource,
		Total:       len(items),
		Fallbacks:   fallbacks,
		Items:       items,
	}

	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(out); err != nil {
			fmt.Fprintln(os.Stderr, "输出失败:", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("目录: %s\n规则: %s\n\n", out.Dir, out.RulesSource)
	for _, item := range items {
		if item.Error != "" {
			fmt.Printf("!! %-19s %-24s %s\n", "解析失败", item.Error, item.Name)
			continue
		}
		flagText := "  "
		if item.Fallback {
			flagText = "⚠ "
		}
		fmt.Printf("%s%-19s %-24s %s\n", flagText, item.TakenAt, item.Source, item.Name)
	}

	fmt.Printf("\n共 %d 个文件", out.Total)
	if fallbacks > 0 {
		fmt.Printf("，其中 %d 个文件将回退到导入时间（建议补充时间解析规则）：\n", fallbacks)
		for _, item := range items {
			if item.Fallback {
				fmt.Printf("  - %s\n", item.Path)
			}
		}
	} else {
		fmt.Println("，全部命中 EXIF/文件名规则/文件修改时间。")
	}
}

// loadRules 优先从数据库读取启用规则；数据库缺失时使用内置规则。
func loadRules(path string) ([]timeparse.Rule, string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return builtinRules, "builtin（数据库路径无效）"
	}
	if _, err := os.Stat(abs); err != nil {
		return builtinRules, "builtin（未找到 " + abs + "）"
	}

	dsn := "file:" + filepath.ToSlash(abs) + "?mode=ro&_pragma=busy_timeout(3000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return builtinRules, "builtin（打开数据库失败: " + err.Error() + "）"
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, pattern FROM time_parse_rules WHERE enabled = 1 ORDER BY priority ASC, id ASC`)
	if err != nil {
		return builtinRules, "builtin（读取规则失败: " + err.Error() + "）"
	}
	defer rows.Close()

	rules := make([]timeparse.Rule, 0, 16)
	for rows.Next() {
		var rule timeparse.Rule
		if err := rows.Scan(&rule.ID, &rule.Pattern); err != nil {
			return builtinRules, "builtin（解析规则失败）"
		}
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return builtinRules, "builtin（库中没有启用中的规则）"
	}
	return rules, "database: " + abs
}
