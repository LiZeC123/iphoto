// Package config 提供写死的运行常量，并把 library / runtime 目录解析到
// 可执行文件所在目录（见 dev.md 2.1 固定约定）。
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// Port 固定端口，冲突即启动失败，不做自动换端口。
	Port = 18765
	// BindAddr 只绑定回环地址，不监听 0.0.0.0。
	BindAddr = "127.0.0.1"

	LibraryDirName = "library"
	RuntimeDirName = "runtime"

	// EnvBaseDir 仅用于开发调试：go run 的可执行文件位于临时目录，
	// 无法据此定位 ./library，因此允许显式指定基准目录。
	// 正式运行时不应设置该变量。
	EnvBaseDir = "IPHOTO_BASE_DIR"
)

// Paths 汇总所有固定路径。
type Paths struct {
	Base    string // 可执行文件所在目录（或开发时的 IPHOTO_BASE_DIR）
	Library string // 需要备份
	Runtime string // 可删除重建

	Blobs    string // library/blobs
	DB       string // library/library.sqlite
	Thumbs   string // runtime/thumbs
	Previews string // runtime/previews（TIFF 等浏览器不可直显格式的 JPEG 预览）
	Trash    string // runtime/trash
	Logs     string // runtime/logs
	LogFile  string // runtime/logs/app.log
	Exports  string // runtime/exports
}

// Load 解析路径，但不创建目录。
func Load() (Paths, error) {
	base, err := baseDir()
	if err != nil {
		return Paths{}, err
	}
	library := filepath.Join(base, LibraryDirName)
	runtimeDir := filepath.Join(base, RuntimeDirName)
	logs := filepath.Join(runtimeDir, "logs")

	return Paths{
		Base:     base,
		Library:  library,
		Runtime:  runtimeDir,
		Blobs:    filepath.Join(library, "blobs"),
		DB:       filepath.Join(library, "library.sqlite"),
		Thumbs:   filepath.Join(runtimeDir, "thumbs"),
		Previews: filepath.Join(runtimeDir, "previews"),
		Trash:    filepath.Join(runtimeDir, "trash"),
		Logs:     logs,
		LogFile:  filepath.Join(logs, "app.log"),
		Exports:  filepath.Join(runtimeDir, "exports"),
	}, nil
}

// BlobPath 由数据库中的 rel_path（斜杠分隔）得到磁盘绝对路径。
func (p Paths) BlobPath(relPath string) string {
	return filepath.Join(p.Blobs, filepath.FromSlash(relPath))
}

// ThumbPath 由哈希得到缩略图路径：runtime/thumbs/ab/cd/<hash>.jpg。
func (p Paths) ThumbPath(hash string) string {
	if len(hash) < 4 {
		return filepath.Join(p.Thumbs, hash+".jpg")
	}
	return filepath.Join(p.Thumbs, hash[:2], hash[2:4], hash+".jpg")
}

// PreviewPath 由哈希得到查看器预览图路径：runtime/previews/ab/cd/<hash>.jpg。
func (p Paths) PreviewPath(hash string) string {
	if len(hash) < 4 {
		return filepath.Join(p.Previews, hash+".jpg")
	}
	return filepath.Join(p.Previews, hash[:2], hash[2:4], hash+".jpg")
}

// Ensure 创建所有必需目录（幂等）。
func (p Paths) Ensure() error {
	dirs := []string{
		p.Library,
		p.Blobs,
		p.Runtime,
		p.Thumbs,
		p.Previews,
		p.Trash,
		p.Logs,
		p.Exports,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s: %w", dir, err)
		}
	}
	return nil
}

func baseDir() (string, error) {
	if v := os.Getenv(EnvBaseDir); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", fmt.Errorf("解析 %s: %w", EnvBaseDir, err)
		}
		return abs, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("定位可执行文件: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe), nil
}
