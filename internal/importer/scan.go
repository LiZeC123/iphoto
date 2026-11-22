// Package importer 实现递归扫描、哈希计算与 CAS 存储（dev.md 第 3、5 章）。
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"iphoto/internal/media"
)

// ErrNotDirectory 表示导入路径不存在、不可读或不是目录。
var ErrNotDirectory = errors.New("路径不存在或不是可读目录")

// ScanResult 是递归扫描结果。
type ScanResult struct {
	Path       string   `json:"path"`
	Total      int      `json:"total"`
	Files      []string `json:"-"`
	Unreadable int      `json:"unreadable"`
}

// Scan 递归遍历 root，只收集 jpg/jpeg/png；跳过隐藏文件与符号链接。
func Scan(root string) (ScanResult, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return ScanResult{}, err
	}
	abs = filepath.Clean(abs)

	info, err := os.Stat(abs)
	if err != nil {
		return ScanResult{}, ErrNotDirectory
	}
	if !info.IsDir() {
		return ScanResult{}, ErrNotDirectory
	}
	// 目录可读性检查：能打开并读一项才算可读。
	f, err := os.Open(abs)
	if err != nil {
		return ScanResult{}, ErrNotDirectory
	}
	if _, err := f.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		f.Close()
		return ScanResult{}, ErrNotDirectory
	}
	f.Close()

	seen := make(map[string]struct{})
	files := make([]string, 0, 256)
	unreadable := 0

	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			unreadable++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == abs {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// WalkDir 使用 Lstat，符号链接的 IsDir 为 false，直接跳过即可。
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !media.IsSupported(name) {
			return nil
		}
		clean := filepath.Clean(p)
		if _, ok := seen[clean]; ok {
			return nil
		}
		seen[clean] = struct{}{}
		files = append(files, clean)
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}

	sort.Strings(files)
	return ScanResult{Path: abs, Total: len(files), Files: files, Unreadable: unreadable}, nil
}

// HashFile 以只读方式计算 SHA-256，同时返回文件大小。
func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("打开源文件: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("读取源文件: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
