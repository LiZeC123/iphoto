package media

import (
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/gav1d/avif"
	"github.com/gen2brain/h265/heic"
)

const (
	// ThumbLongEdge 缩略图长边上限。
	ThumbLongEdge = 384
	// ThumbQuality JPEG 质量。
	ThumbQuality = 80
	// PreviewLongEdge 查看器预览图长边上限（用于 TIFF/HEIC 等浏览器无法直接显示的格式）。
	PreviewLongEdge = 2560
	// PreviewQuality 预览图 JPEG 质量。
	PreviewQuality = 85
)

// GenerateThumbnail 生成 384px 长边、JPEG 质量 80 的缩略图。
func GenerateThumbnail(src, dst string) error {
	return generateJPEG(src, dst, ThumbLongEdge, ThumbQuality)
}

// GeneratePreview 生成查看器用的 JPEG 预览图（长边 2560、质量 85）。
// 目前用于 TIFF 与 HEIC/HEIF：浏览器不能直接渲染，查看器改读该预览。
func GeneratePreview(src, dst string) error {
	return generateJPEG(src, dst, PreviewLongEdge, PreviewQuality)
}

// loadForRender 解码图片并应用方向。
//
// AVIF/HEIC 的旋转信息在容器的 irot/imir 里，两个解码库的默认选项**不会**旋转，
// 必须显式打开 AutoRotate；其它格式沿用 imaging 的 EXIF 自动转正。
// 注意不要对 AVIF/HEIC 再叠加 EXIF Orientation，否则会二次旋转。
func loadForRender(src string) (image.Image, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, fmt.Errorf("打开图片 %s: %w", src, err)
	}
	defer f.Close()

	// 只读头判断真实格式（扩展名可能与内容不符），再用对应的解码器。
	if _, format, err := image.DecodeConfig(f); err != nil {
		return nil, fmt.Errorf("解析图片 %s: %w", src, err)
	} else if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("读取图片 %s: %w", src, err)
	} else {
		switch format {
		case "heic":
			return heic.Decode(f, heic.Options{AutoRotate: true})
		case "avif":
			return avif.Decode(f, avif.Options{AutoRotate: true})
		}
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("读取图片 %s: %w", src, err)
	}
	return imaging.Decode(f, imaging.AutoOrientation(true))
}

// generateJPEG 解码源图、等比缩放到 longEdge 以内并编码为 JPEG。
// 采用临时文件 + rename，避免中断留下半个文件；同一张图可重复生成（内容只取决于原图）。
func generateJPEG(src, dst string, longEdge, quality int) error {
	img, err := loadForRender(src)
	if err != nil {
		return err
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
