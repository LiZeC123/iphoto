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
	if version != 1 {
		t.Fatalf("user_version = %d, want 1", version)
	}

	var tables int
	if err := s.DB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN
		 ('assets','tags','asset_tags','albums','album_items','time_parse_rules',
		  'import_batches','import_items','app_settings','operations')`,
	).Scan(&tables); err != nil {
		t.Fatalf("统计表数量: %v", err)
	}
	if tables != 10 {
		t.Fatalf("表数量 = %d, want 10", tables)
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
