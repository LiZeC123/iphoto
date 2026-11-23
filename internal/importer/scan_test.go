package importer

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestScanRecursiveFilters(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.jpg"), "a")
	writeFile(t, filepath.Join(root, "sub", "b.PNG"), "b")
	writeFile(t, filepath.Join(root, "sub", "c.txt"), "c")
	writeFile(t, filepath.Join(root, ".hidden.jpg"), "h")
	writeFile(t, filepath.Join(root, ".hiddendir", "d.jpg"), "d")
	writeFile(t, filepath.Join(root, "deep", "deeper", "e.jpeg"), "e")

	target := filepath.Join(root, "a.jpg")
	link := filepath.Join(root, "link.jpg")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("创建符号链接: %v", err)
	}

	scan, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if scan.Total != 3 {
		t.Fatalf("Total = %d, want 3 (%v)", scan.Total, scan.Files)
	}
	want := []string{
		filepath.Join(root, "a.jpg"),
		filepath.Join(root, "deep", "deeper", "e.jpeg"),
		filepath.Join(root, "sub", "b.PNG"),
	}
	for i, w := range want {
		if scan.Files[i] != w {
			t.Errorf("Files[%d] = %q, want %q", i, scan.Files[i], w)
		}
	}
}

func TestScanRejectsInvalidPaths(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("不存在的路径 err = %v, want ErrNotDirectory", err)
	}
	file := filepath.Join(t.TempDir(), "a.jpg")
	writeFile(t, file, "x")
	if _, err := Scan(file); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("文件路径 err = %v, want ErrNotDirectory", err)
	}
}

func TestHashFileStable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.jpg")
	writeFile(t, file, "hello")

	h1, size, err := HashFile(file)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if size != 5 {
		t.Fatalf("size = %d, want 5", size)
	}
	// echo -n hello | shasum -a 256
	if h1 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("hash = %s", h1)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入文件: %v", err)
	}
}

func TestScanReportsUnsupported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.jpg"), "a")
	writeFile(t, filepath.Join(root, "notes.txt"), "n")
	writeFile(t, filepath.Join(root, "sub", "clip.mkv"), "m")
	writeFile(t, filepath.Join(root, "sub", "noext"), "x")
	// 隐藏文件按约定跳过，不计入「类型不支持」。
	writeFile(t, filepath.Join(root, ".hidden.txt"), "h")

	scan, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if scan.Total != 1 {
		t.Fatalf("Total = %d, want 1", scan.Total)
	}
	if scan.Unsupported.Total != 3 {
		t.Fatalf("Unsupported.Total = %d, want 3 (%+v)", scan.Unsupported.Total, scan.Unsupported)
	}
	if scan.Unsupported.Truncated {
		t.Fatal("未超上限时 Truncated 应为 false")
	}
	gotByExt := map[string]int{}
	for _, e := range scan.Unsupported.ByExt {
		gotByExt[e.Ext] = e.Count
	}
	wantByExt := map[string]int{".txt": 1, ".mkv": 1, "(无后缀)": 1}
	if len(gotByExt) != len(wantByExt) {
		t.Fatalf("ByExt = %+v", gotByExt)
	}
	for ext, want := range wantByExt {
		if gotByExt[ext] != want {
			t.Errorf("ByExt[%q] = %d, want %d", ext, gotByExt[ext], want)
		}
	}
	wantFiles := map[string]bool{"notes.txt": true, "sub/clip.mkv": true, "sub/noext": true}
	if len(scan.Unsupported.Files) != len(wantFiles) {
		t.Fatalf("Files = %v", scan.Unsupported.Files)
	}
	for _, f := range scan.Unsupported.Files {
		if !wantFiles[f] {
			t.Errorf("意外的文件: %q", f)
		}
	}
}

func TestScanUnsupportedFileListCapped(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < UnsupportedFileListLimit+5; i++ {
		writeFile(t, filepath.Join(root, "file-"+strconv.Itoa(i)+".txt"), "x")
	}
	scan, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if scan.Unsupported.Total != UnsupportedFileListLimit+5 {
		t.Fatalf("Total = %d", scan.Unsupported.Total)
	}
	if len(scan.Unsupported.Files) != UnsupportedFileListLimit {
		t.Fatalf("Files 长度 = %d, want %d", len(scan.Unsupported.Files), UnsupportedFileListLimit)
	}
	if !scan.Unsupported.Truncated {
		t.Fatal("超过上限时 Truncated 应为 true")
	}
	if scan.Unsupported.ByExt[0].Ext != ".txt" || scan.Unsupported.ByExt[0].Count != UnsupportedFileListLimit+5 {
		t.Fatalf("ByExt = %+v", scan.Unsupported.ByExt)
	}
}

func TestScanAcceptsNewImageFormats(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.gif", "b.webp", "c.bmp", "d.tif", "e.TIFF", "v.mp4"} {
		writeFile(t, filepath.Join(root, name), "x")
	}
	writeFile(t, filepath.Join(root, "still.txt"), "x")

	scan, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if scan.Total != 6 {
		t.Fatalf("Total = %d, want 6 (%v)", scan.Total, scan.Files)
	}
	if scan.Unsupported.Total != 1 || scan.Unsupported.ByExt[0].Ext != ".txt" {
		t.Fatalf("Unsupported = %+v", scan.Unsupported)
	}
}
