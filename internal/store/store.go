// Package store 负责 SQLite 连接、PRAGMA 设置与 schema 迁移（dev.md 第 4 章）。
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无需 CGO
)

//go:embed schema/*.sql
var schemaFS embed.FS

// Store 包装 *sql.DB，后续仓储方法都挂在它上面。
type Store struct {
	db *sql.DB
}

// Open 打开数据库，设置 PRAGMA 并执行未应用的迁移。
func Open(path string) (*Store, error) {
	dsn := dsnFor(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 sqlite: %w", err)
	}
	// WAL 下允许少量并发读；写入由调用方串行化。
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接 sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// DB 暴露底层连接，供仓储层与测试使用。
func (s *Store) DB() *sql.DB { return s.db }

// Close 关闭数据库连接。
func (s *Store) Close() error { return s.db.Close() }

// SchemaVersion 返回当前 PRAGMA user_version。
func (s *Store) SchemaVersion() (int, error) {
	var v int
	err := s.db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}

func (s *Store) migrate() error {
	current, err := s.SchemaVersion()
	if err != nil {
		return fmt.Errorf("读取 schema 版本: %w", err)
	}

	names, err := fs.Glob(schemaFS, "schema/*.sql")
	if err != nil {
		return fmt.Errorf("读取内置 schema: %w", err)
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := versionOf(name)
		if err != nil {
			return err
		}
		if version <= current {
			continue
		}
		body, err := schemaFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("读取 %s: %w", name, err)
		}
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("开始迁移 %s: %w", name, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("执行迁移 %s: %w", name, err)
		}
		// PRAGMA 不支持占位符，version 来自文件名，已校验为整数。
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("设置 schema 版本 %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交迁移 %s: %w", name, err)
		}
	}
	return nil
}

// versionOf 从 001_init.sql 这类文件名解析版本号。
func versionOf(name string) (int, error) {
	base := filepath.Base(name)
	idx := strings.IndexByte(base, '_')
	if idx <= 0 {
		return 0, fmt.Errorf("迁移文件名需形如 001_init.sql: %s", name)
	}
	v, err := strconv.Atoi(base[:idx])
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("迁移文件名版本号非法: %s", name)
	}
	return v, nil
}

func dsnFor(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	q := u.Query()
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	u.RawQuery = q.Encode()
	return u.String()
}
