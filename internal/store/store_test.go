package store

import (
	"path/filepath"
	"testing"
)

// TestOpenMigrates 验证 schema 迁移可重复执行，且内置时间规则已写入。
func TestOpenMigrates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "library.sqlite")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	version, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != 2 {
		t.Fatalf("user_version = %d, want 2", version)
	}

	var tables int
	if err := s.DB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN
		 ('assets','tags','asset_tags','albums','album_items','time_parse_rules',
		  'import_batches','import_items','app_settings','operations','asset_signatures')`,
	).Scan(&tables); err != nil {
		t.Fatalf("统计表数量: %v", err)
	}
	if tables != 11 {
		t.Fatalf("表数量 = %d, want 11", tables)
	}

	var rules int
	if err := s.DB().QueryRow(`SELECT count(*) FROM time_parse_rules WHERE builtin = 1`).Scan(&rules); err != nil {
		t.Fatalf("统计内置规则: %v", err)
	}
	if rules == 0 {
		t.Fatal("内置时间解析规则未写入")
	}

	var journalMode string
	if err := s.DB().QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("读取 journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
}

// TestOpenIsIdempotent 验证对已存在的库重复 Open 不会重复执行迁移。
func TestOpenIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "library.sqlite")

	first, err := Open(dbPath)
	if err != nil {
		t.Fatalf("首次 Open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("关闭: %v", err)
	}

	second, err := Open(dbPath)
	if err != nil {
		t.Fatalf("二次 Open: %v", err)
	}
	defer second.Close()

	var rules int
	if err := second.DB().QueryRow(`SELECT count(*) FROM time_parse_rules WHERE builtin = 1`).Scan(&rules); err != nil {
		t.Fatalf("统计内置规则: %v", err)
	}
	if rules != 2 {
		t.Fatalf("内置规则数量 = %d, want 2（迁移被重复执行？）", rules)
	}
}

// TestMigrateFromV1ToV2 验证已有 v1 库升级时会补上指纹表与表达式索引。
func TestMigrateFromV1ToV2(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "library.sqlite")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// 模拟 001 时代的库：删除 002 引入的对象并回退版本号。
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_assets_effective_time`,
		`DROP INDEX IF EXISTS idx_asset_signatures_algorithm`,
		`DROP TABLE IF EXISTS asset_signatures`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := s.DB().Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(dbPath)
	if err != nil {
		t.Fatalf("升级 Open: %v", err)
	}
	defer upgraded.Close()

	version, err := upgraded.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("升级后 user_version = %d, want 2", version)
	}
	var table int
	if err := upgraded.DB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'asset_signatures'`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != 1 {
		t.Fatal("升级后缺少 asset_signatures 表")
	}
	var index int
	if err := upgraded.DB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_assets_effective_time'`).Scan(&index); err != nil {
		t.Fatal(err)
	}
	if index != 1 {
		t.Fatal("升级后缺少 idx_assets_effective_time 索引")
	}
}
