package maintenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/store"
)

func TestRecoverInterruptedAssets(t *testing.T) {
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
	good := addImporting(t, st, paths, base, "good.png", []byte("good-content"), len("good-content"))
	bad := addImporting(t, st, paths, base, "bad.png", []byte("bad"), 999)
	if _, err := st.CreateBatch(ctx, base, time.Now().Format(store.TimeLayout)); err != nil {
		t.Fatal(err)
	}

	res, err := RecoverInterrupted(ctx, st, paths, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if res.Ready != 1 || res.Removed != 1 || res.Batches != 1 {
		t.Fatalf("结果 = %+v", res)
	}

	a, err := st.GetAsset(ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "ready" {
		t.Fatalf("good status = %s", a.Status)
	}
	if _, err := st.GetAsset(ctx, bad); err == nil {
		t.Fatal("大小不符的记录应被删除")
	}
	if _, err := os.Stat(paths.BlobPath("aa/aa/bad.png")); !os.IsNotExist(err) {
		t.Fatal("半成品文件应被删除")
	}
}

func addImporting(t *testing.T, st *store.Store, paths config.Paths, base, name string, content []byte, declaredSize int) string {
	t.Helper()
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	rel := filepath.ToSlash(filepath.Join(hash[:2], hash[2:4], name))
	if err := os.MkdirAll(filepath.Dir(paths.BlobPath(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.BlobPath(rel), content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertImportingAsset(context.Background(), store.Asset{
		Hash:         hash,
		OriginalName: name,
		StorageName:  name,
		RelPath:      rel,
		Size:         int64(declaredSize),
		Mime:         "image/png",
		AddedAt:      time.Now().Format(store.TimeLayout),
		Status:       "importing",
	}); err != nil {
		t.Fatal(err)
	}
	return hash
}

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
		Logs:     filepath.Join(runtimeDir, "logs"),
		LogFile:  filepath.Join(runtimeDir, "logs", "app.log"),
		Exports:  filepath.Join(runtimeDir, "exports"),
	}
}
