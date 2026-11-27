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

	// AVIF 与 HEIC 由纯 Go 解码库提供：import 即通过 image.RegisterFormat 注册，
	// 之后 image.DecodeConfig、imaging.Open 都能直接解码。命名导入用于读容器里的 EXIF。
	"github.com/gen2brain/gav1d/avif"
	"github.com/gen2brain/h265/heic"
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

	// AVIF/HEIC 的 EXIF 在容器里，goexif 只认 JPEG/TIFF 的 EXIF 结构，
	// 因此按解码器识别出的真实格式分派到各自的 DecodeExif。
	switch format {
	case "avif":
		if x, err := avif.DecodeExif(f); err == nil {
			info.TakenAt, info.TakenAtTag = containerExifTime(x.DateTimeOriginal, x.DateTime)
		}
		info.Width, info.Height = modernDisplaySize(f, format, info.Width, info.Height)
		return info, nil
	case "heic":
		if x, err := heic.DecodeExif(f); err == nil {
			info.TakenAt, info.TakenAtTag = containerExifTime(x.DateTimeOriginal, x.DateTime)
		}
		info.Width, info.Height = modernDisplaySize(f, format, info.Width, info.Height)
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
		if t, ok := parseExifTimeString(raw); ok {
			return &t, string(name)
		}
	}
	return nil, ""
}

// modernDisplaySize 返回 AVIF/HEIC 显示尺寸。
//
// DecodeConfig 读的是容器 ispe（旋转前）尺寸，竖拍照片的 irot/imir 不会体现；
// 这里按需解码一次（AutoRotate 应用容器旋转）拿到转正后的宽高。
// 解码失败时退回 ispe 尺寸，不让整个导入失败（缩略图/预览会各自报错）。
func modernDisplaySize(f *os.File, format string, fallbackW, fallbackH int) (int, int) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fallbackW, fallbackH
	}
	var (
		img image.Image
		err error
	)
	switch format {
	case "heic":
		img, err = heic.Decode(f, heic.Options{AutoRotate: true})
	case "avif":
		img, err = avif.Decode(f, avif.Options{AutoRotate: true})
	default:
		return fallbackW, fallbackH
	}
	if err != nil || img == nil {
		return fallbackW, fallbackH
	}
	return img.Bounds().Dx(), img.Bounds().Dy()
}

// containerExifTime 取 AVIF/HEIC 容器 Exif 项里的拍摄时间。
// 这两类 Exif 结构没有 DateTimeDigitized，按 DateTimeOriginal → DateTime 取。
func containerExifTime(dateTimeOriginal, dateTime string) (*time.Time, string) {
	if t, ok := parseExifTimeString(dateTimeOriginal); ok {
		return &t, "DateTimeOriginal"
	}
	if t, ok := parseExifTimeString(dateTime); ok {
		return &t, "DateTime"
	}
	return nil, ""
}

// parseExifTimeString 解析 EXIF 的 "YYYY:MM:DD HH:MM:SS" 字符串。
func parseExifTimeString(raw string) (time.Time, bool) {
	t, err := time.Parse("2006:01:02 15:04:05", strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
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
	case "avif":
		return "image/avif"
	case "heic":
		return "image/heic"
	default:
		return "application/octet-stream"
	}
}

// IsSupported 按扩展名白名单判断是否可导入。
// 当前支持 jpg/jpeg/png/gif/webp/bmp/tif/tiff/avif/heic/heif 与 mp4（视频只存原文件）；
// MKV/MOV 等其它视频容器与 RAW 仍走「不支持」报告。
func IsSupported(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tif", ".tiff",
		".avif", ".heic", ".heif", ".mp4":
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
	case ".avif":
		return "image/avif"
	case ".heic", ".heif":
		// .heif 是 HEVC-in-HEIF 容器，与 .heic 使用同一套解码与展示策略。
		return "image/heic"
	case ".mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

// NeedsDisplayProxy 判断该 MIME 是否需要服务端转成 JPEG 才能在浏览器中查看。
// TIFF 不被 Chrome/Firefox 支持；HEIC/HEIF 只有 Safari 能显示，统一改用
// /api/assets/{hash}/preview 生成的 JPEG 预览。AVIF 主流浏览器已原生支持，直接读原图。
func NeedsDisplayProxy(mime string) bool {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/tiff", "image/heic", "image/heif":
		return true
	default:
		return false
	}
}
