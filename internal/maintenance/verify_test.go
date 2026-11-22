package maintenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/store"
)

func TestVerifyDetectsMissingAndOrphans(t *testing.T) {
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

	ctx := context.Background()
	// 正常文件
	okContent := []byte("ok-image")
	okHash := addAsset(t, st, paths, "ok.jpg", okContent)
	// 缺失文件（数据库有记录，磁盘无文件）
	missingContent := []byte("missing-image")
	missingHash := addAsset(t, st, paths, "missing.jpg", missingContent)
	if err := os.Remove(paths.BlobPath(relPathFor(missingHash, "missing.jpg"))); err != nil {
		t.Fatal(err)
	}
	// 孤儿文件
	orphanRel := "_orphan/left.png"
	if err := os.MkdirAll(filepath.Dir(paths.BlobPath(orphanRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.BlobPath(orphanRel), []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Verify(ctx, st, paths, true)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.Checked != 2 {
		t.Fatalf("checked = %d", report.Checked)
	}
	if len(report.Missing) != 1 || report.Missing[0].Hash != missingHash {
		t.Fatalf("missing = %+v", report.Missing)
	}
	if report.MarkedMissing != 1 {
		t.Fatalf("marked_missing = %d", report.MarkedMissing)
	}
	if len(report.Orphans) != 1 || report.Orphans[0] != orphanRel {
		t.Fatalf("orphans = %+v", report.Orphans)
	}
	if report.Hashed != 1 {
		t.Fatalf("hashed = %d", report.Hashed)
	}

	a, err := st.GetAsset(ctx, missingHash)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "missing" {
		t.Fatalf("缺失记录状态 = %s", a.Status)
	}
	okAsset, err := st.GetAsset(ctx, okHash)
	if err != nil {
		t.Fatal(err)
	}
	if okAsset.Status != "ready" {
		t.Fatalf("正常记录状态 = %s", okAsset.Status)
	}
}

func relPathFor(hash, name string) string {
	return filepath.ToSlash(filepath.Join(hash[:2], hash[2:4], name))
}

func addAsset(t *testing.T, st *store.Store, paths config.Paths, name string, content []byte) string {
	t.Helper()
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	rel := relPathFor(hash, name)
	if err := os.MkdirAll(filepath.Dir(paths.BlobPath(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.BlobPath(rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.InsertImportingAsset(ctx, store.Asset{
		Hash: hash, OriginalName: name, StorageName: name, RelPath: rel,
		Size: int64(len(content)), Mime: "image/jpeg",
		AddedAt: time.Now().Format(store.TimeLayout), Status: "importing",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkAssetReady(ctx, hash, int64(len(content)), "image/jpeg", nil, nil,
		"2024-01-01T00:00:00", "test"); err != nil {
		t.Fatal(err)
	}
	return hash
}
