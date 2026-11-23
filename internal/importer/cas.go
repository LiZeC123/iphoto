package importer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HashLen 是 SHA-256 十六进制长度。
const HashLen = 64

// ValidateHash 校验哈希格式。
func ValidateHash(hash string) bool {
	if len(hash) != HashLen {
		return false
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// sanitizeName 取原始文件名并保证可安全落盘。
func sanitizeName(originalName, hash string) string {
	name := filepath.Base(strings.TrimSpace(originalName))
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0:
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = hash + ".jpg"
	}
	if len(name) > 180 {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		// 按字节截断时保证不切坏 UTF-8 字符（文件名可能含中文）。
		name = truncateUTF8(stem, 180-len(ext)) + ext
	}
	return name
}

// truncateUTF8 把 s 截断到不超过 maxBytes 字节，且不拆分多字节字符。
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && (s[cut]&0xC0) == 0x80 { // 落在续字节上就继续往前回退
		cut--
	}
	return s[:cut]
}

func splitName(name string) (stem, ext string) {
	ext = filepath.Ext(name)
	return strings.TrimSuffix(name, ext), ext
}

// PlanTarget 在不复制文件的前提下决定 CAS 目标路径。
// 同名文件已存在时追加 __hash8，避免覆盖其他图片。
func PlanTarget(blobsRoot, originalName, hash string) (relPath, storageName string, err error) {
	if !ValidateHash(hash) {
		return "", "", fmt.Errorf("非法哈希: %q", hash)
	}
	if info, err := os.Stat(blobsRoot); err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("blobs 目录不可用: %s", blobsRoot)
	}

	dirRel := filepath.ToSlash(filepath.Join(hash[:2], hash[2:4]))
	dirAbs := filepath.Join(blobsRoot, filepath.FromSlash(dirRel))
	if err := os.MkdirAll(dirAbs, 0o755); err != nil {
		return "", "", fmt.Errorf("创建 CAS 目录: %w", err)
	}

	name := sanitizeName(originalName, hash)
	target := filepath.Join(dirAbs, name)
	if _, err := os.Stat(target); err == nil {
		stem, ext := splitName(name)
		name = stem + "__" + hash[:8] + ext
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("检查目标文件: %w", err)
	}

	return filepath.ToSlash(filepath.Join(dirRel, name)), name, nil
}

// CopyBlob 把源文件复制到已规划好的目标路径，并校验大小一致。
// 采用临时文件 + rename，避免中断留下半个文件。
func CopyBlob(blobsRoot, relPath, srcPath string) (int64, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return 0, fmt.Errorf("打开源文件: %w", err)
	}
	defer src.Close()
	stat, err := src.Stat()
	if err != nil {
		return 0, fmt.Errorf("读取源文件信息: %w", err)
	}

	target := filepath.Join(blobsRoot, filepath.FromSlash(relPath))
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".import-*.part")
	if err != nil {
		return 0, fmt.Errorf("创建临时文件: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	n, err := io.Copy(tmp, src)
	if err != nil {
		cleanup()
		return 0, fmt.Errorf("复制文件: %w", err)
	}
	if n != stat.Size() {
		cleanup()
		return 0, fmt.Errorf("复制大小不一致: 源 %d 字节，写入 %d 字节", stat.Size(), n)
	}
	if err := tmp.Chmod(0o644); err != nil {
		cleanup()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return 0, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return 0, fmt.Errorf("落盘文件: %w", err)
	}
	return n, nil
}
