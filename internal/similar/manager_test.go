package similar

import (
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/store"
)

func testPaths(base string) config.Paths {
	library := filepath.Join(base, "library")
	runtimeDir := filepath.Join(base, "runtime")
	return config.Paths{
		Base:     base,
		Library:  library,
		Runtime:  runtimeDir,
		Blobs:    filepath.Join(library, "blobs"),
		DB:       filepath.Join(library, "library.sqlite"),
		Thumbs:   filepath.Join(runtimeDir, "thumbs"),
		Previews: filepath.Join(runtimeDir, "previews"),
		Trash:    filepath.Join(runtimeDir, "trash"),
		Temp:     filepath.Join(runtimeDir, "tmp"),
		Logs:     filepath.Join(runtimeDir, "logs"),
		LogFile:  filepath.Join(runtimeDir, "logs", "app.log"),
		Exports:  filepath.Join(runtimeDir, "exports"),
	}
}

func newTestManager(t *testing.T) (*Manager, *store.Store, config.Paths) {
	t.Helper()
	paths := testPaths(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewManager(st, paths, logger), st, paths
}

func hashOf(ch rune) string {
	return strings.Repeat(string(ch), 64)
}

// addAsset 把一张图片写入 CAS 目录并插入 ready 记录。
func addAsset(t *testing.T, st *store.Store, paths config.Paths, hash, name, takenAt string, img image.Image) {
	t.Helper()
	rel := hash[:2] + "/" + hash[2:4] + "/" + name
	abs := paths.BlobPath(rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("创建目录: %v", err)
	}
	f, err := os.Create(abs)
	if err != nil {
		t.Fatalf("创建图片: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("编码 PNG: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(abs)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := st.InsertImportingAsset(ctx, store.Asset{
		Hash:         hash,
		OriginalName: name,
		StorageName:  name,
		RelPath:      rel,
		Size:         stat.Size(),
		Mime:         "image/png",
		AddedAt:      takenAt,
		Status:       "importing",
	}); err != nil {
		t.Fatalf("InsertImportingAsset: %v", err)
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if err := st.MarkAssetReady(ctx, hash, stat.Size(), "image/png", &w, &h, takenAt, "test"); err != nil {
		t.Fatalf("MarkAssetReady: %v", err)
	}
}

// addBrokenAsset 写入一个无法解码的「图片」文件。
func addBrokenAsset(t *testing.T, st *store.Store, paths config.Paths, hash, name, takenAt string) {
	t.Helper()
	rel := hash[:2] + "/" + hash[2:4] + "/" + name
	abs := paths.BlobPath(rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("this is not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.InsertImportingAsset(ctx, store.Asset{
		Hash: hash, OriginalName: name, StorageName: name, RelPath: rel,
		Size: 20, Mime: "image/png", AddedAt: takenAt, Status: "importing",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkAssetReady(ctx, hash, 20, "image/png", intPtr(10), intPtr(10), takenAt, "test"); err != nil {
		t.Fatal(err)
	}
}

func waitDone(t *testing.T, m *Manager) Snapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		snap := m.Progress()
		if snap.Status != StatusRunning {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("检测超时")
	return Snapshot{}
}

func TestDetectGroupsAndCache(t *testing.T) {
	m, st, paths := newTestManager(t)
	horizontal := horizontalGradient(90, 80)
	vertical := verticalGradient(90, 80)
	addAsset(t, st, paths, hashOf('a'), "a1.png", "2024-01-01T10:00:00", horizontal)
	addAsset(t, st, paths, hashOf('b'), "a2.png", "2024-01-01T10:01:00", horizontal)
	addAsset(t, st, paths, hashOf('c'), "a3.png", "2024-01-01T10:02:00", horizontal)
	addAsset(t, st, paths, hashOf('d'), "b1.png", "2024-01-01T10:03:00", vertical)
	addAsset(t, st, paths, hashOf('e'), "b2.png", "2024-01-01T10:04:00", vertical)

	if _, err := m.Start(Options{Algorithm: AlgorithmDHash, Threshold: 8}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	snap := waitDone(t, m)
	if snap.Status != StatusDone {
		t.Fatalf("status = %s, error=%s", snap.Status, snap.Error)
	}
	if snap.Total != 5 || snap.Processed != 5 {
		t.Fatalf("total/processed = %d/%d", snap.Total, snap.Processed)
	}
	if snap.Groups != 2 {
		t.Fatalf("groups = %d, want 2", snap.Groups)
	}

	res, err := m.Result()
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	if res.GroupCount != 2 || len(res.Groups) != 2 {
		t.Fatalf("group_count = %d", res.GroupCount)
	}
	if res.Computed != 5 || res.Cached != 0 {
		t.Fatalf("computed/cached = %d/%d", res.Computed, res.Cached)
	}
	if res.Truncated {
		t.Fatal("不应标记为提前停止")
	}
	if len(res.Groups[0].Images) != 3 || len(res.Groups[1].Images) != 2 {
		t.Fatalf("组大小 = %d/%d", len(res.Groups[0].Images), len(res.Groups[1].Images))
	}
	if res.Groups[0].Images[0].DistanceFromPrev != nil {
		t.Fatal("组内第一张不应有前一张距离")
	}
	if res.Groups[0].Images[1].DistanceFromPrev == nil || *res.Groups[0].Images[1].DistanceFromPrev != 0 {
		t.Fatalf("第二张距离 = %v, want 0", res.Groups[0].Images[1].DistanceFromPrev)
	}
	if res.Groups[0].Images[0].OriginalName != "a1.png" || res.Groups[1].Images[0].OriginalName != "b1.png" {
		t.Fatalf("分组顺序错误: %q / %q", res.Groups[0].Images[0].OriginalName, res.Groups[1].Images[0].OriginalName)
	}

	sigs, err := st.Signatures(context.Background(), AlgorithmDHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(sigs) != 5 {
		t.Fatalf("指纹数量 = %d, want 5", len(sigs))
	}

	// 第二次检测应全部命中缓存，不重复解码。
	if _, err := m.Start(Options{Algorithm: AlgorithmDHash, Threshold: 8}); err != nil {
		t.Fatalf("第二次 Start: %v", err)
	}
	waitDone(t, m)
	res, err = m.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res.Cached != 5 || res.Computed != 0 {
		t.Fatalf("第二次 cached/computed = %d/%d, want 5/0", res.Cached, res.Computed)
	}
	if res.GroupCount != 2 {
		t.Fatalf("第二次 group_count = %d", res.GroupCount)
	}
}

func TestDetectSkipsBrokenImageAndBreaksChain(t *testing.T) {
	m, st, paths := newTestManager(t)
	horizontal := horizontalGradient(90, 80)
	addAsset(t, st, paths, hashOf('a'), "a1.png", "2024-01-01T10:00:00", horizontal)
	addBrokenAsset(t, st, paths, hashOf('b'), "broken.png", "2024-01-01T10:01:00")
	addAsset(t, st, paths, hashOf('c'), "a2.png", "2024-01-01T10:02:00", horizontal)

	if _, err := m.Start(Options{Algorithm: AlgorithmDHash, Threshold: 8}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitDone(t, m)
	res, err := m.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res.Failures != 1 || res.Scanned != 3 {
		t.Fatalf("failures/scanned = %d/%d", res.Failures, res.Scanned)
	}
	// 损坏图片打断相似链：前后两张完全相同的图片不应成组。
	if res.GroupCount != 0 {
		t.Fatalf("group_count = %d, want 0（链应在损坏图片处断开）", res.GroupCount)
	}
}

func TestDetectStopsAtGroupLimit(t *testing.T) {
	m, st, paths := newTestManager(t)
	m.MaxGroups = 1
	horizontal := horizontalGradient(90, 80)
	vertical := verticalGradient(90, 80)
	addAsset(t, st, paths, hashOf('a'), "a1.png", "2024-01-01T10:00:00", horizontal)
	addAsset(t, st, paths, hashOf('b'), "a2.png", "2024-01-01T10:01:00", horizontal)
	addAsset(t, st, paths, hashOf('c'), "b1.png", "2024-01-01T10:02:00", vertical)
	addAsset(t, st, paths, hashOf('d'), "b2.png", "2024-01-01T10:03:00", vertical)
	addAsset(t, st, paths, hashOf('e'), "a3.png", "2024-01-01T10:04:00", horizontal)
	addAsset(t, st, paths, hashOf('f'), "a4.png", "2024-01-01T10:05:00", horizontal)

	if _, err := m.Start(Options{Algorithm: AlgorithmDHash, Threshold: 8}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitDone(t, m)
	res, err := m.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res.GroupCount != 1 {
		t.Fatalf("group_count = %d, want 1", res.GroupCount)
	}
	if !res.Truncated {
		t.Fatal("达到 1 组上限且仍有图片时应标记 truncated")
	}
	if res.Scanned >= res.Total {
		t.Fatalf("scanned = %d, total = %d，应在达到上限后提前停止", res.Scanned, res.Total)
	}
}

func TestSignaturePrefersThumbnail(t *testing.T) {
	m, st, paths := newTestManager(t)
	horizontal := horizontalGradient(90, 80)
	vertical := verticalGradient(90, 80)
	hash := hashOf('a')
	addAsset(t, st, paths, hash, "a1.png", "2024-01-01T10:00:00", horizontal)

	thumb := paths.ThumbPath(hash)
	if err := os.MkdirAll(filepath.Dir(thumb), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(thumb)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, vertical); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	asset, err := st.GetAsset(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	value, fromCache, err := m.signatureFor(asset, map[string]string{})
	if err != nil {
		t.Fatalf("signatureFor: %v", err)
	}
	if fromCache {
		t.Fatal("首次计算不应命中缓存")
	}
	if want := DHash(vertical); value != want {
		t.Fatalf("签名 = %016x, want 缩略图指纹 %016x（说明没有优先使用缩略图）", value, want)
	}
}

func TestStartValidationAndNoResult(t *testing.T) {
	m, _, _ := newTestManager(t)
	if _, err := m.Result(); err != ErrNoResult {
		t.Fatalf("未检测时 Result err = %v, want ErrNoResult", err)
	}
	if _, err := m.Start(Options{Algorithm: "phash"}); err != ErrAlgorithm {
		t.Fatalf("未知算法 err = %v, want ErrAlgorithm", err)
	}
	if _, err := m.Start(Options{Algorithm: AlgorithmDHash, Threshold: 65}); err != ErrThreshold {
		t.Fatalf("越界阈值 err = %v, want ErrThreshold", err)
	}
	if got := m.Progress().Status; got != StatusIdle {
		t.Fatalf("初始状态 = %s, want idle", got)
	}
}

func TestStartTimeFiltersAssets(t *testing.T) {
	m, st, paths := newTestManager(t)
	horizontal := horizontalGradient(90, 80)
	addAsset(t, st, paths, hashOf('a'), "old1.png", "2020-01-01T10:00:00", horizontal)
	addAsset(t, st, paths, hashOf('b'), "old2.png", "2020-01-01T10:01:00", horizontal)
	addAsset(t, st, paths, hashOf('c'), "new1.png", "2024-06-01T10:00:00", horizontal)
	addAsset(t, st, paths, hashOf('d'), "new2.png", "2024-06-01T10:01:00", horizontal)

	if _, err := m.Start(Options{From: "2024-01-01T00:00:00", Algorithm: AlgorithmDHash, Threshold: 8}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitDone(t, m)
	res, err := m.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || res.GroupCount != 1 || len(res.Groups[0].Images) != 2 {
		t.Fatalf("按起始时间过滤失败: total=%d groups=%d", res.Total, res.GroupCount)
	}
	if res.Groups[0].Images[0].OriginalName != "new1.png" {
		t.Fatalf("首张 = %q", res.Groups[0].Images[0].OriginalName)
	}
}
