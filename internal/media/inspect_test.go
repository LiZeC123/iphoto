package media

import (
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func TestIsSupportedAndMIME(t *testing.T) {
	supported := []string{
		"a.jpg", "a.JPEG", "a.png", "a.gif", "a.webp", "a.bmp", "a.tif", "a.TIFF",
		"a.avif", "a.AVIF", "a.heic", "a.HEIC", "a.heif", "clip.mp4", "clip.MP4",
	}
	unsupported := []string{"a.mkv", "a.mov", "a.raw", "a.txt", "a"}
	for _, name := range supported {
		if !IsSupported(name) {
			t.Errorf("IsSupported(%q) = false, want true", name)
		}
		if got := MIMEOf(name); got == "application/octet-stream" {
			t.Errorf("MIMEOf(%q) = %q，未映射", name, got)
		}
	}
	for _, name := range unsupported {
		if IsSupported(name) {
			t.Errorf("IsSupported(%q) = true, want false", name)
		}
	}

	mimes := map[string]string{
		"a.jpg": "image/jpeg", "a.jpeg": "image/jpeg", "a.png": "image/png",
		"a.gif": "image/gif", "a.webp": "image/webp", "a.bmp": "image/bmp",
		"a.tif": "image/tiff", "a.tiff": "image/tiff", "a.mp4": "video/mp4",
		"a.avif": "image/avif", "a.heic": "image/heic", "a.heif": "image/heic",
	}
	for name, want := range mimes {
		if got := MIMEOf(name); got != want {
			t.Errorf("MIMEOf(%q) = %q, want %q", name, got, want)
		}
	}

	if !NeedsDisplayProxy("image/tiff") {
		t.Error("TIFF 应需要 JPEG 预览代理")
	}
	if !NeedsDisplayProxy("image/heic") || !NeedsDisplayProxy("image/heif") {
		t.Error("HEIC/HEIF 应需要 JPEG 预览代理")
	}
	if NeedsDisplayProxy("image/avif") {
		t.Error("AVIF 主流浏览器可直显，不应走预览代理")
	}
	if NeedsDisplayProxy("image/webp") || NeedsDisplayProxy("image/gif") {
		t.Error("浏览器可直显的格式不应走预览代理")
	}
	if !IsVideo("clip.MP4") || IsVideo("clip.mkv") || IsVideo("clip.jpg") {
		t.Error("IsVideo 只应识别 mp4")
	}
}

func TestInspectVideo(t *testing.T) {
	path := filepath.Join("testdata", "sample.mp4")
	info, err := Inspect(path)
	if err != nil {
		t.Fatalf("Inspect(%s): %v", path, err)
	}
	if info.MIME != "video/mp4" {
		t.Fatalf("MIME = %q, want video/mp4", info.MIME)
	}
	if info.Width != 0 || info.Height != 0 {
		t.Fatalf("视频宽高应为 0（存 NULL），got %dx%d", info.Width, info.Height)
	}
}

func TestGenerateVideoThumbnail(t *testing.T) {
	if !FFmpegAvailable() {
		t.Skip("未安装 ffmpeg，跳过视频抽帧测试")
	}
	src := filepath.Join("testdata", "sample.mp4")
	dst := filepath.Join(t.TempDir(), "video-thumb.jpg")
	if err := GenerateVideoThumbnail(src, dst); err != nil {
		t.Fatalf("GenerateVideoThumbnail: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(mustOpen(t, dst))
	if err != nil {
		t.Fatalf("解码视频缩略图: %v", err)
	}
	if cfg.Width != 384 {
		t.Fatalf("缩略图宽度 = %d, want 384", cfg.Width)
	}
}

// writeGIF 生成 2 帧动图 GIF，验证缩略图取首帧。
func writeGIF(t *testing.T, path string) {
	t.Helper()
	palette := color.Palette{color.Black, color.White, color.RGBA{R: 200, A: 255}}
	frames := []*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 64, 48), palette),
		image.NewPaletted(image.Rect(0, 0, 64, 48), palette),
	}
	frames[1].SetColorIndex(0, 0, 1)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := gif.EncodeAll(f, &gif.GIF{Image: frames, Delay: []int{10, 10}}); err != nil {
		t.Fatalf("编码 GIF: %v", err)
	}
}

func writeBMP(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: 100, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := bmp.Encode(f, img); err != nil {
		t.Fatalf("编码 BMP: %v", err)
	}
}

func writeTIFF(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 80, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 80; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 30, G: uint8(x * 3), B: uint8(y * 4), A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := tiff.Encode(f, img, &tiff.Options{Compression: tiff.Deflate}); err != nil {
		t.Fatalf("编码 TIFF: %v", err)
	}
}

func TestInspectAndThumbnailNewFormats(t *testing.T) {
	dir := t.TempDir()
	gifPath := filepath.Join(dir, "anim.gif")
	bmpPath := filepath.Join(dir, "image.bmp")
	tiffPath := filepath.Join(dir, "scan.tif")
	writeGIF(t, gifPath)
	writeBMP(t, bmpPath)
	writeTIFF(t, tiffPath)

	cases := []struct {
		path string
		mime string
		w, h int
	}{
		{gifPath, "image/gif", 64, 48},
		{bmpPath, "image/bmp", 64, 48},
		{tiffPath, "image/tiff", 80, 50},
		{filepath.Join("testdata", "static.webp"), "image/webp", 64, 48},
	}
	for _, tc := range cases {
		info, err := Inspect(tc.path)
		if err != nil {
			t.Errorf("Inspect(%s): %v", tc.path, err)
			continue
		}
		if info.Width != tc.w || info.Height != tc.h {
			t.Errorf("%s 尺寸 = %dx%d, want %dx%d", tc.path, info.Width, info.Height, tc.w, tc.h)
		}
		if info.MIME != tc.mime {
			t.Errorf("%s MIME = %q, want %q", tc.path, info.MIME, tc.mime)
		}

		dst := filepath.Join(dir, filepath.Base(tc.path)+".thumb.jpg")
		if err := GenerateThumbnail(tc.path, dst); err != nil {
			t.Errorf("GenerateThumbnail(%s): %v", tc.path, err)
			continue
		}
		cfg, err := jpeg.DecodeConfig(mustOpen(t, dst))
		if err != nil {
			t.Errorf("解码缩略图 %s: %v", dst, err)
			continue
		}
		if cfg.Width > ThumbLongEdge || cfg.Height > ThumbLongEdge {
			t.Errorf("%s 缩略图尺寸 %dx%d 超过上限", tc.path, cfg.Width, cfg.Height)
		}
	}
}

// 动图 WebP 当前是已知限制：能读到尺寸（因此可正常导入），但 x/image/webp 不支持动图解码，
// 生成缩略图会失败。此处只断言 Inspect 成功，避免依赖未来是否支持动图。
func TestInspectAnimatedWebPStillImports(t *testing.T) {
	path := filepath.Join("testdata", "animated.webp")
	info, err := Inspect(path)
	if err != nil {
		t.Fatalf("Inspect(%s): %v", path, err)
	}
	if info.Width != 64 || info.Height != 48 || info.MIME != "image/webp" {
		t.Fatalf("动图 WebP info = %+v", info)
	}
	if err := GenerateThumbnail(path, filepath.Join(t.TempDir(), "t.jpg")); err != nil {
		t.Logf("动图 WebP 暂不生成缩略图（预期）：%v", err)
	}
}

func TestGeneratePreviewForTIFF(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.tif")
	// 构造大于 PreviewLongEdge 的图，确认预览被缩到上限以内。
	img := image.NewRGBA(image.Rect(0, 0, PreviewLongEdge+200, 400))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := tiff.Encode(f, img, &tiff.Options{Compression: tiff.Deflate}); err != nil {
		f.Close()
		t.Fatalf("编码 TIFF: %v", err)
	}
	f.Close()

	dst := filepath.Join(dir, "preview.jpg")
	if err := GeneratePreview(src, dst); err != nil {
		t.Fatalf("GeneratePreview: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(mustOpen(t, dst))
	if err != nil {
		t.Fatalf("解码预览: %v", err)
	}
	if cfg.Width != PreviewLongEdge {
		t.Fatalf("预览宽度 = %d, want %d", cfg.Width, PreviewLongEdge)
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开 %s: %v", path, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
