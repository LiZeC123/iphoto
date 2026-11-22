// Package media 封装图片探测与缩略图生成（dev.md 第 6、7 章）。
package media

import (
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	// 注册 jpeg / png 解码器（MVP 只承诺 JPG/JPEG/PNG）。
	_ "image/jpeg"
	_ "image/png"

	"github.com/rwcarlsen/goexif/exif"
)

// Info 是从文件本身读到的只读信息。
type Info struct {
	Width      int
	Height     int
	MIME       string
	Rotation   int // EXIF Orientation，1 表示无需旋转
	TakenAt    *time.Time
	TakenAtTag string // DateTimeOriginal / DateTimeDigitized / DateTime
}

// Inspect 读取尺寸、MIME、EXIF 方向与拍摄时间；EXIF 缺失不视为错误。
func Inspect(p string) (Info, error) {
	var info Info
	f, err := os.Open(p)
	if err != nil {
		return info, err
	}
	defer f.Close()

	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return info, fmt.Errorf("解析图片 %s: %w", p, err)
	}
	info.Width = cfg.Width
	info.Height = cfg.Height
	info.MIME = mimeOf(format)
	info.Rotation = 1

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return info, nil
	}
	x, err := exif.Decode(f)
	if err != nil {
		return info, nil // 无 EXIF 属正常情况
	}
	if tag, err := x.Get(exif.Orientation); err == nil {
		if v, err := tag.Int(0); err == nil && v >= 1 && v <= 8 {
			info.Rotation = v
		}
	}
	info.TakenAt, info.TakenAtTag = exifTime(x)
	return info, nil
}

// exifTime 按 DateTimeOriginal → DateTimeDigitized → DateTime 的顺序取拍摄时间。
func exifTime(x *exif.Exif) (*time.Time, string) {
	orders := []exif.FieldName{exif.DateTimeOriginal, exif.DateTimeDigitized, exif.DateTime}
	for _, name := range orders {
		tag, err := x.Get(name)
		if err != nil {
			continue
		}
		raw, err := tag.StringVal()
		if err != nil {
			continue
		}
		t, err := time.Parse("2006:01:02 15:04:05", strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		return &t, string(name)
	}
	return nil, ""
}

func mimeOf(format string) string {
	switch strings.ToLower(format) {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	default:
		return "application/octet-stream"
	}
}

// IsSupported 按扩展名白名单判断是否可导入（MVP 承诺 jpg/jpeg/png）。
func IsSupported(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg", ".png":
		return true
	default:
		return false
	}
}

// MIMEOf 按扩展名推断 MIME，用于尚未解码时写入中间记录。
func MIMEOf(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	default:
		return "application/octet-stream"
	}
}
