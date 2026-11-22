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
)

// GenerateThumbnail 生成 384px 长边、JPEG 质量 80 的缩略图。
// 使用 imaging.AutoOrientation 让缩略图按 EXIF 转正，原图不做任何修改。
func GenerateThumbnail(src, dst string) error {
	img, err := imaging.Open(src, imaging.AutoOrientation(true))
	if err != nil {
		return fmt.Errorf("打开图片 %s: %w", src, err)
	}
	thumb := imaging.Fit(img, ThumbLongEdge, ThumbLongEdge, imaging.Lanczos)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建缩略图目录: %w", err)
	}
	// 先写临时文件再改名，避免中断后留下半张缩略图。
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("创建缩略图: %w", err)
	}
	if err := jpeg.Encode(f, thumb, &jpeg.Options{Quality: ThumbQuality}); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("编码缩略图: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
