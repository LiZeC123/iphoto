package media

import (
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
)

const (
	// ThumbLongEdge 缩略图长边上限。
	ThumbLongEdge = 384
	// ThumbQuality JPEG 质量。
	ThumbQuality = 80
	// PreviewLongEdge 查看器预览图长边上限（用于 TIFF 等浏览器无法直接显示的格式）。
	PreviewLongEdge = 2560
	// PreviewQuality 预览图 JPEG 质量。
	PreviewQuality = 85
)

// GenerateThumbnail 生成 384px 长边、JPEG 质量 80 的缩略图。
// 使用 imaging.AutoOrientation 让缩略图按 EXIF 转正，原图不做任何修改。
func GenerateThumbnail(src, dst string) error {
	return generateJPEG(src, dst, ThumbLongEdge, ThumbQuality)
}

// GeneratePreview 生成查看器用的 JPEG 预览图（长边 2560、质量 85）。
// 目前用于 TIFF：浏览器不能直接渲染 TIFF，查看器改读该预览。
func GeneratePreview(src, dst string) error {
	return generateJPEG(src, dst, PreviewLongEdge, PreviewQuality)
}

// generateJPEG 解码源图、等比缩放到 longEdge 以内并编码为 JPEG。
// 采用临时文件 + rename，避免中断留下半个文件；同一张图可重复生成（内容只取决于原图）。
func generateJPEG(src, dst string, longEdge, quality int) error {
	img, err := imaging.Open(src, imaging.AutoOrientation(true))
	if err != nil {
		return fmt.Errorf("打开图片 %s: %w", src, err)
	}
	out := imaging.Fit(img, longEdge, longEdge, imaging.Lanczos)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建图片缓存目录: %w", err)
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("创建图片缓存: %w", err)
	}
	if err := jpeg.Encode(f, out, &jpeg.Options{Quality: quality}); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("编码 JPEG: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("落盘图片缓存: %w", err)
	}
	return nil
}
