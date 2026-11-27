package media

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/gav1d/avif"
	"github.com/gen2brain/h265/heic"
)

// testNRGBA 生成一张有渐变的小图，避免纯色缩略图掩盖解码问题。
func testNRGBA(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 255 / max(w-1, 1)),
				G: uint8(y * 255 / max(h-1, 1)),
				B: 96,
				A: 255,
			})
		}
	}
	return img
}

func writeHEIC(t *testing.T, path string, img image.Image, opts heic.EncodeOptions) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 HEIC: %v", err)
	}
	defer f.Close()
	if err := heic.Encode(f, img, opts); err != nil {
		t.Fatalf("编码 HEIC: %v", err)
	}
}

func writeAVIF(t *testing.T, path string, img image.Image, opts avif.EncodeOptions) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 AVIF: %v", err)
	}
	defer f.Close()
	if err := avif.Encode(f, img, opts); err != nil {
		t.Fatalf("编码 AVIF: %v", err)
	}
}

// TestInspectAVIFAndHEIC 验证两种新格式的尺寸、MIME 与缩略图/预览链路。
func TestInspectAVIFAndHEIC(t *testing.T) {
	dir := t.TempDir()
	heicPath := filepath.Join(dir, "photo.heic")
	heifPath := filepath.Join(dir, "photo.heif")
	avifPath := filepath.Join(dir, "photo.avif")
	heic10Path := filepath.Join(dir, "photo10.heic")

	writeHEIC(t, heicPath, testNRGBA(120, 80), heic.EncodeOptions{Quality: 60})
	writeHEIC(t, heifPath, testNRGBA(120, 80), heic.EncodeOptions{Quality: 60})
	writeAVIF(t, avifPath, testNRGBA(120, 80), avif.EncodeOptions{Quality: 60, Speed: 10})
	// 10 位 HEIC 解码为 NRGBA64，验证缩略图仍能生成。
	writeHEIC(t, heic10Path, testNRGBA(120, 80), heic.EncodeOptions{Quality: 60, BitDepth: 10})

	cases := []struct {
		path string
		mime string
	}{
		{heicPath, "image/heic"},
		{heifPath, "image/heic"},
		{avifPath, "image/avif"},
		{heic10Path, "image/heic"},
	}
	for _, tc := range cases {
		info, err := Inspect(tc.path)
		if err != nil {
			t.Fatalf("Inspect(%s): %v", tc.path, err)
		}
		if info.Width != 120 || info.Height != 80 {
			t.Errorf("%s 尺寸 = %dx%d, want 120x80", tc.path, info.Width, info.Height)
		}
		if info.MIME != tc.mime {
			t.Errorf("%s MIME = %q, want %q", tc.path, info.MIME, tc.mime)
		}

		thumb := filepath.Join(dir, filepath.Base(tc.path)+".thumb.jpg")
		if err := GenerateThumbnail(tc.path, thumb); err != nil {
			t.Fatalf("GenerateThumbnail(%s): %v", tc.path, err)
		}
		cfg, err := jpeg.DecodeConfig(mustOpen(t, thumb))
		if err != nil {
			t.Fatalf("解码缩略图 %s: %v", thumb, err)
		}
		if cfg.Width != 120 || cfg.Height != 80 {
			t.Errorf("%s 缩略图 = %dx%d, want 120x80", tc.path, cfg.Width, cfg.Height)
		}

		preview := filepath.Join(dir, filepath.Base(tc.path)+".preview.jpg")
		if err := GeneratePreview(tc.path, preview); err != nil {
			t.Fatalf("GeneratePreview(%s): %v", tc.path, err)
		}
		if _, err := jpeg.DecodeConfig(mustOpen(t, preview)); err != nil {
			t.Fatalf("解码预览 %s: %v", preview, err)
		}
	}
}

// TestGeneratePreviewForModernFormats 验证超过长边上限的 HEIC/AVIF 预览会被缩放。
func TestGeneratePreviewForModernFormats(t *testing.T) {
	dir := t.TempDir()
	wide := testNRGBA(PreviewLongEdge+400, 120)

	heicPath := filepath.Join(dir, "wide.heic")
	writeHEIC(t, heicPath, wide, heic.EncodeOptions{Quality: 60})
	avifPath := filepath.Join(dir, "wide.avif")
	writeAVIF(t, avifPath, wide, avif.EncodeOptions{Quality: 60, Speed: 10})

	for _, src := range []string{heicPath, avifPath} {
		dst := src + ".preview.jpg"
		if err := GeneratePreview(src, dst); err != nil {
			t.Fatalf("GeneratePreview(%s): %v", src, err)
		}
		cfg, err := jpeg.DecodeConfig(mustOpen(t, dst))
		if err != nil {
			t.Fatalf("解码预览 %s: %v", dst, err)
		}
		if cfg.Width != PreviewLongEdge {
			t.Fatalf("%s 预览宽度 = %d, want %d", src, cfg.Width, PreviewLongEdge)
		}
	}
}

// TestInspectHEICExifTime 验证容器 Exif 项里的 DateTimeOriginal 能被取到。
func TestInspectHEICExifTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exif.heic")
	const want = "2026:11:27 15:33:20"
	writeHEIC(t, path, testNRGBA(64, 48), heic.EncodeOptions{
		Quality: 60,
		Exif:    exifTIFFWithDateTimeOriginal(want),
	})

	info, err := Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.TakenAt == nil {
		t.Fatal("未从 HEIC 容器读到拍摄时间")
	}
	if got := info.TakenAt.Format("2006:01:02 15:04:05"); got != want {
		t.Fatalf("拍摄时间 = %q, want %q", got, want)
	}
	if info.TakenAtTag != "DateTimeOriginal" {
		t.Fatalf("taken_at tag = %q, want DateTimeOriginal", info.TakenAtTag)
	}
}

// exifTIFFWithDateTimeOriginal 构造一个最小 TIFF：IFD0 只有 ExifIFDPointer，
// Exif 子 IFD 里放 DateTimeOriginal（ASCII 20 字节，含结尾 NUL）。
func exifTIFFWithDateTimeOriginal(value string) []byte {
	const (
		ifd0Offset    = 8
		exifIFDOffset = 26
		stringOffset  = 44
	)
	buf := make([]byte, stringOffset+20)

	copy(buf[0:2], "II")
	binary.LittleEndian.PutUint16(buf[2:4], 42)
	binary.LittleEndian.PutUint32(buf[4:8], ifd0Offset)

	// IFD0: 1 个条目，指向 Exif 子 IFD。
	binary.LittleEndian.PutUint16(buf[ifd0Offset:], 1)
	binary.LittleEndian.PutUint16(buf[ifd0Offset+2:], 0x8769) // ExifIFDPointer
	binary.LittleEndian.PutUint16(buf[ifd0Offset+4:], 4)      // LONG
	binary.LittleEndian.PutUint32(buf[ifd0Offset+6:], 1)
	binary.LittleEndian.PutUint32(buf[ifd0Offset+10:], exifIFDOffset)
	binary.LittleEndian.PutUint32(buf[ifd0Offset+14:], 0) // 没有下一个 IFD

	// Exif 子 IFD: 1 个条目 DateTimeOriginal。
	binary.LittleEndian.PutUint16(buf[exifIFDOffset:], 1)
	binary.LittleEndian.PutUint16(buf[exifIFDOffset+2:], 0x9003) // DateTimeOriginal
	binary.LittleEndian.PutUint16(buf[exifIFDOffset+4:], 2)      // ASCII
	binary.LittleEndian.PutUint32(buf[exifIFDOffset+6:], 20)
	binary.LittleEndian.PutUint32(buf[exifIFDOffset+10:], stringOffset)
	binary.LittleEndian.PutUint32(buf[exifIFDOffset+14:], 0)

	copy(buf[stringOffset:], value)
	buf[stringOffset+19] = 0
	return buf
}
