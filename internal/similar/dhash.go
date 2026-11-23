// Package similar 实现相似图片检测：dHash 指纹计算、指纹缓存与按时间顺序的相邻比对。
package similar

import (
	"fmt"
	"image"
	"image/color"
	"math/bits"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
)

// AlgorithmDHash 是当前唯一支持的检测算法：64 位差分哈希。
const AlgorithmDHash = "dhash"

// dHash 网格：9×8 像素，横向比较相邻像素得到 8×8 = 64 位指纹。
const (
	dhashWidth  = 9
	dhashHeight = 8
)

// DHash 计算图片的 64 位 dHash 指纹。
func DHash(img image.Image) uint64 {
	small := imaging.Resize(img, dhashWidth, dhashHeight, imaging.Lanczos)
	var value uint64
	for y := 0; y < dhashHeight; y++ {
		for x := 0; x < dhashWidth-1; x++ {
			value <<= 1
			if luminance(small.At(x, y)) > luminance(small.At(x+1, y)) {
				value |= 1
			}
		}
	}
	return value
}

// luminance 把颜色转成 0–65535 的灰度值（BT.601 权重）。
func luminance(c color.Color) int {
	r, g, b, _ := c.RGBA()
	return int((299*int64(r) + 587*int64(g) + 114*int64(b)) / 1000)
}

// Hamming 返回两个指纹的汉明距离（0 = 完全相同，64 = 完全不同）。
func Hamming(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// FormatSignature 把指纹格式化为 16 位十六进制字符串。
func FormatSignature(value uint64) string {
	return fmt.Sprintf("%016x", value)
}

// ParseSignature 解析 FormatSignature 的输出；格式非法时返回错误。
func ParseSignature(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 16 {
		return 0, fmt.Errorf("指纹格式非法: %q", s)
	}
	value, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("指纹格式非法: %q", s)
	}
	return value, nil
}

// dhashFile 解码图片文件并计算指纹。autoOrientation 为 true 时按 EXIF 转正
// （缩略图在生成时已经转正，无需重复处理）。
func dhashFile(path string, autoOrientation bool) (uint64, error) {
	img, err := imaging.Open(path, imaging.AutoOrientation(autoOrientation))
	if err != nil {
		return 0, fmt.Errorf("解码图片 %s: %w", path, err)
	}
	return DHash(img), nil
}
