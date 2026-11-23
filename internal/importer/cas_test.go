package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPlanTargetAndCopyBlob(t *testing.T) {
	blobs := filepath.Join(t.TempDir(), "blobs")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "IMG_001.jpg")
	writeFile(t, src, "content-123")

	hash, size, err := HashFile(src)
	if err != nil {
		t.Fatal(err)
	}
	rel, name, err := PlanTarget(blobs, "IMG_001.jpg", hash)
	if err != nil {
		t.Fatalf("PlanTarget: %v", err)
	}
	if name != "IMG_001.jpg" {
		t.Fatalf("storageName = %q", name)
	}
	if rel != filepath.ToSlash(filepath.Join(hash[:2], hash[2:4], name)) {
		t.Fatalf("relPath = %q", rel)
	}

	n, err := CopyBlob(blobs, rel, src)
	if err != nil {
		t.Fatalf("CopyBlob: %v", err)
	}
	if n != size {
		t.Fatalf("copied = %d, want %d", n, size)
	}
	got, err := os.ReadFile(filepath.Join(blobs, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "content-123" {
		t.Fatalf("content = %q", got)
	}
}

func TestPlanTargetNameConflict(t *testing.T) {
	blobs := filepath.Join(t.TempDir(), "blobs")
	hash := strings.Repeat("ab", 32)
	dir := filepath.Join(blobs, "ab", "ab")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, name, err := PlanTarget(blobs, "photo.jpg", hash)
	if err != nil {
		t.Fatal(err)
	}
	if name != "photo__abababab.jpg" {
		t.Fatalf("storageName = %q, want photo__abababab.jpg", name)
	}
}

func TestPlanTargetRejectsBadHash(t *testing.T) {
	blobs := t.TempDir()
	if _, _, err := PlanTarget(blobs, "a.jpg", "short"); err == nil {
		t.Fatal("期望非法哈希报错")
	}
	if ValidateHash(strings.Repeat("g", 64)) {
		t.Fatal("非十六进制哈希应校验失败")
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"../evil.jpg":     "evil.jpg",
		"/abs/path/a.jpg": "a.jpg",
		"  normal.png  ":  "normal.png",
		"with:colon?.jpg": "with:colon?.jpg",
		"trailing...":     "trailing",
	}
	for in, want := range cases {
		got := sanitizeName(in, strings.Repeat("a", 64))
		if got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeNameTruncatesUTF8Safely(t *testing.T) {
	name := strings.Repeat("图", 100) + ".png" // 300 字节 + 扩展名，超过 180 字节上限
	got := sanitizeName(name, strings.Repeat("a", 64))
	if !utf8.ValidString(got) {
		t.Fatalf("截断后不是合法 UTF-8: %q", got)
	}
	if len(got) > 180 {
		t.Fatalf("截断后长度 = %d, want <= 180", len(got))
	}
	if filepath.Ext(got) != ".png" {
		t.Fatalf("扩展名丢失: %q", got)
	}
}
