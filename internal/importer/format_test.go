package importer

import (
	"context"
	"image"
	"image/color"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/gav1d/avif"
	"github.com/gen2brain/h265/heic"

	"iphoto/internal/store"
	"iphoto/internal/thumbs"
)

func gradientImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 8), G: uint8(y * 10), B: 120, A: 255})
		}
	}
	return img
}

func writeImageFile(t *testing.T, path string, encode func(*os.File) error) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 %s: %v", path, err)
	}
	defer f.Close()
	if err := encode(f); err != nil {
		t.Fatalf("编码 %s: %v", path, err)
	}
}

// TestImportAVIFAndHEIC 验证扫描白名单、入库 MIME 与缩略图对新格式生效。
func TestImportAVIFAndHEIC(t *testing.T) {
	base := t.TempDir()
	paths := testPaths(base)
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := thumbs.New(paths, logger, 1)
	qctx, cancelQueue := context.WithCancel(context.Background())
	t.Cleanup(cancelQueue)
	q.Start(qctx)
	t.Cleanup(func() {
		cancelQueue()
		q.Wait()
	})

	m := NewManager(st, paths, q, logger)
	m.Workers = 1

	src := filepath.Join(base, "src")
	img := gradientImage(48, 32)
	writeImageFile(t, filepath.Join(src, "a.avif"), func(f *os.File) error {
		return avif.Encode(f, img, avif.EncodeOptions{Quality: 60, Speed: 10})
	})
	writeImageFile(t, filepath.Join(src, "b.heic"), func(f *os.File) error {
		return heic.Encode(f, img, heic.EncodeOptions{Quality: 60})
	})
	// 内容与 b.heic 不同，避免命中本地导入的「同名异容」去重逻辑。
	writeImageFile(t, filepath.Join(src, "c.heif"), func(f *os.File) error {
		return heic.Encode(f, gradientImage(40, 24), heic.EncodeOptions{Quality: 60})
	})
	// 仍然不支持的格式继续被显式报告。
	if err := os.WriteFile(filepath.Join(src, "d.mkv"), []byte("not-a-video"), 0o644); err != nil {
		t.Fatal(err)
	}

	batchID, err := m.Start(src, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	snap := waitDone(t, m, batchID)
	if snap.Status != "done" {
		t.Fatalf("status = %s", snap.Status)
	}
	if snap.Total != 3 || snap.Imported != 3 || snap.Failed != 0 {
		t.Fatalf("统计不符: %+v", snap)
	}
	if snap.Unsupported.Total != 1 || len(snap.Unsupported.ByExt) != 1 || snap.Unsupported.ByExt[0].Ext != ".mkv" {
		t.Fatalf("不支持报告不符: %+v", snap.Unsupported)
	}

	page, err := st.ListAssets(context.Background(), store.AssetFilter{})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	if page.Total != 3 {
		t.Fatalf("资产数 = %d, want 3", page.Total)
	}
	want := map[string]struct {
		mime string
		w, h int
	}{
		"a.avif": {"image/avif", 48, 32},
		"b.heic": {"image/heic", 48, 32},
		"c.heif": {"image/heic", 40, 24},
	}
	for _, a := range page.Items {
		w, ok := want[a.OriginalName]
		if !ok {
			t.Fatalf("未预期的资产 %q", a.OriginalName)
		}
		if a.Mime != w.mime {
			t.Fatalf("%s MIME = %q, want %q", a.OriginalName, a.Mime, w.mime)
		}
		if a.Width == nil || *a.Width != w.w || a.Height == nil || *a.Height != w.h {
			t.Fatalf("%s 尺寸 = %v x %v, want %dx%d", a.OriginalName, a.Width, a.Height, w.w, w.h)
		}
	}

	// 缩略图由后台队列生成，等待队列清空后逐个确认。
	q.Wait()
	for _, a := range page.Items {
		if _, err := os.Stat(paths.ThumbPath(a.Hash)); err != nil {
			t.Fatalf("%s 缺少缩略图: %v", a.OriginalName, err)
		}
	}
}
