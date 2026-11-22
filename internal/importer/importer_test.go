package importer

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/store"
	"iphoto/internal/thumbs"
)

func TestManagerImportAndReplaceDuplicate(t *testing.T) {
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

	m := NewManager(st, paths, q, logger)
	m.Workers = 1 // 顺序处理，保证去重结果可预期

	src := filepath.Join(base, "src")
	sameColor := color.RGBA{R: 10, G: 20, B: 30, A: 255}
	writePNG(t, filepath.Join(src, "a.png"), sameColor)
	writePNG(t, filepath.Join(src, "b.png"), color.RGBA{R: 200, G: 100, B: 50, A: 255})
	writePNG(t, filepath.Join(src, "copy-of-a.png"), sameColor) // 内容同、名字不同 → pending
	writePNG(t, filepath.Join(src, "sub", "a.png"), sameColor)  // 内容与名字都同 → skip

	batchID, err := m.Start(src, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	snap := waitDone(t, m, batchID)
	if snap.Status != "done" {
		t.Fatalf("status = %s", snap.Status)
	}
	if snap.Total != 4 || snap.Processed != 4 {
		t.Fatalf("total/processed = %d/%d, want 4/4", snap.Total, snap.Processed)
	}
	if snap.Imported != 2 || snap.DuplicateSkipped != 1 || snap.DuplicatePending != 1 || snap.Failed != 0 {
		t.Fatalf("统计不符: %+v", snap)
	}

	ctx := context.Background()
	page, err := st.ListAssets(ctx, store.AssetFilter{})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("assets total = %d, want 2", page.Total)
	}
	for _, a := range page.Items {
		if a.Status != "ready" {
			t.Errorf("asset %s status = %s", a.Hash, a.Status)
		}
		if a.Width == nil || *a.Width != 8 {
			t.Errorf("asset %s 宽度未写入", a.Hash)
		}
		if _, err := os.Stat(paths.BlobPath(a.RelPath)); err != nil {
			t.Errorf("blob 缺失: %v", err)
		}
	}
	for i := 0; i < 100; i++ {
		if thumbCount(paths, page) == len(page.Items) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := thumbCount(paths, page); got != len(page.Items) {
		t.Fatalf("缩略图数量 = %d, want %d", got, len(page.Items))
	}

	dups, err := st.ListDuplicateItems(ctx, batchID)
	if err != nil {
		t.Fatalf("ListDuplicateItems: %v", err)
	}
	if len(dups) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(dups))
	}
	if dups[0].ExistingName != "a.png" {
		t.Fatalf("existing name = %q", dups[0].ExistingName)
	}

	res, err := m.ResolveDuplicates(ctx, batchID, []Resolution{{ID: dups[0].ID, Action: "replace"}})
	if err != nil {
		t.Fatalf("ResolveDuplicates: %v", err)
	}
	if res.Replaced != 1 || res.Updated != 1 {
		t.Fatalf("resolve result = %+v", res)
	}

	asset, err := st.GetAsset(ctx, dups[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if asset.OriginalName != "copy-of-a.png" || asset.StorageName != "copy-of-a.png" {
		t.Fatalf("替换后名称 = %q / %q", asset.OriginalName, asset.StorageName)
	}
	if _, err := os.Stat(paths.BlobPath(asset.RelPath)); err != nil {
		t.Fatalf("替换后 blob 缺失: %v", err)
	}

	item, err := st.GetImportItem(ctx, batchID, dups[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "duplicate_replace" {
		t.Fatalf("item status = %s", item.Status)
	}

	rows, err := st.ReportRows(ctx, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("report rows = %d, want 4", len(rows))
	}
}

func TestManagerRejectsBusyAndInvalidPath(t *testing.T) {
	base := t.TempDir()
	paths := testPaths(base)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(st, paths, thumbs.New(paths, logger, 1), logger)

	if _, err := m.Start(filepath.Join(base, "missing"), nil); err == nil {
		t.Fatal("不存在的目录应报错")
	}

	src := filepath.Join(base, "src")
	writePNG(t, filepath.Join(src, "a.png"), color.RGBA{R: 1, A: 255})
	if _, err := m.Start(src, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	// 批次可能已经极快完成；只有仍在运行时才要求 ErrBusy。
	if m.ActiveBatchID() != 0 {
		if _, err := m.Start(src, nil); err == nil {
			t.Fatal("并发导入应返回 ErrBusy")
		}
	}
}

func testPaths(base string) config.Paths {
	library := filepath.Join(base, "library")
	runtimeDir := filepath.Join(base, "runtime")
	return config.Paths{
		Base:    base,
		Library: library,
		Runtime: runtimeDir,
		Blobs:   filepath.Join(library, "blobs"),
		DB:      filepath.Join(library, "library.sqlite"),
		Thumbs:  filepath.Join(runtimeDir, "thumbs"),
		Trash:   filepath.Join(runtimeDir, "trash"),
		Logs:    filepath.Join(runtimeDir, "logs"),
		LogFile: filepath.Join(runtimeDir, "logs", "app.log"),
		Exports: filepath.Join(runtimeDir, "exports"),
	}
}

func writePNG(t *testing.T, path string, c color.Color) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建图片: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("编码 PNG: %v", err)
	}
}

func waitDone(t *testing.T, m *Manager, batchID int64) Snapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		snap, err := m.Progress(context.Background(), batchID)
		if err != nil {
			t.Fatalf("Progress: %v", err)
		}
		if snap.Status != "running" {
			return snap
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("导入超时")
	return Snapshot{}
}

func thumbCount(paths config.Paths, page store.AssetPage) int {
	n := 0
	for _, a := range page.Items {
		if _, err := os.Stat(paths.ThumbPath(a.Hash)); err == nil {
			n++
		}
	}
	return n
}

func TestManagerCancel(t *testing.T) {
	base := t.TempDir()
	paths := testPaths(base)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(st, paths, thumbs.New(paths, logger, 1), logger)

	src := filepath.Join(base, "src")
	for i := 0; i < 120; i++ {
		writePNG(t, filepath.Join(src, fmt.Sprintf("img-%03d.png", i)), color.RGBA{R: uint8(i), G: 50, B: 60, A: 255})
	}
	batchID, err := m.Start(src, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Cancel(batchID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	snap := waitDone(t, m, batchID)
	if snap.Status != "cancelled" {
		t.Fatalf("status = %s, want cancelled（total=%d processed=%d）", snap.Status, snap.Total, snap.Processed)
	}
	left, err := st.ListAssetsByStatus(context.Background(), "importing")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("取消后仍有 %d 条 importing 记录", len(left))
	}
}

func TestManagerImportAppliesTags(t *testing.T) {
	base := t.TempDir()
	paths := testPaths(base)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(st, paths, thumbs.New(paths, logger, 1), logger)

	src := filepath.Join(base, "src")
	writePNG(t, filepath.Join(src, "tagged.png"), color.RGBA{R: 9, G: 9, B: 9, A: 255})
	if _, err := m.Start(src, []string{"旅行", " 导入 "}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	snap := waitDone(t, m, 1)
	if snap.Status != "done" || snap.Imported != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}

	tags, err := st.ListTags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("tags = %+v", tags)
	}
	for _, tag := range tags {
		if tag.Count != 1 {
			t.Errorf("tag %q count = %d, want 1", tag.Name, tag.Count)
		}
	}
	page, err := st.ListAssets(context.Background(), store.AssetFilter{Tag: "旅行"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("按标签筛选 total = %d", page.Total)
	}
}
