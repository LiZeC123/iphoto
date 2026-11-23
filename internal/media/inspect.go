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

	// 注册解码器：GIF 用标准库，BMP/TIFF/WebP 用 golang.org/x/image。
	// WebP 只支持静态图（动图 WebP 可导入、浏览器可播放，但暂不生成缩略图）。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

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
// 视频不解码，只返回 MIME（宽高留空，由前端/查看器按视口自适应）。
func Inspect(p string) (Info, error) {
	if IsVideo(p) {
		return Info{MIME: MIMEOf(p), Rotation: 1}, nil
	}

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
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "bmp":
		return "image/bmp"
	case "tiff":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}

// IsSupported 按扩展名白名单判断是否可导入。
// 当前支持 jpg/jpeg/png/gif/webp/bmp/tif/tiff 与 mp4（视频只存原文件）；
// AVIF、HEIC 及其它视频容器仍走「不支持」报告。
func IsSupported(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".mp4":
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
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".tif", ".tiff":
		return "image/tiff"
	case ".mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

// NeedsDisplayProxy 判断该 MIME 是否需要服务端转成 JPEG 才能在浏览器中查看。
// TIFF 不被 Chrome/Firefox 支持，查看器改用 /api/assets/{hash}/preview 生成的 JPEG 预览。
func NeedsDisplayProxy(mime string) bool {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/tiff":
		return true
	default:
		return false
	}
}
