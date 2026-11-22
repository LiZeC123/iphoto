package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// exportAssets 复制选中原图到目标目录，文件名用 original_name，冲突加 __hash8。
func (h *handler) exportAssets(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hashes []string `json:"hashes"`
		Subdir string   `json:"subdir"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if len(body.Hashes) == 0 || len(body.Hashes) > 500 {
		h.fail(w, http.StatusBadRequest, "hashes 数量需为 1–500")
		return
	}

	// 导出只允许落在 runtime/exports/<名称>/ 下，避免任意路径写入。
	name := strings.TrimSpace(body.Subdir)
	if name == "" {
		h.fail(w, http.StatusBadRequest, "请输入导出子目录名称")
		return
	}
	if len(name) > 80 {
		h.fail(w, http.StatusBadRequest, "导出子目录名称过长（最多 80 字符）")
		return
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		h.fail(w, http.StatusBadRequest, "导出子目录名称不能包含路径分隔符或以点开头")
		return
	}
	targetDir := filepath.Join(h.opts.Paths.Exports, name)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		h.fail(w, http.StatusInternalServerError, "创建导出目录失败: "+err.Error())
		return
	}

	assets, err := h.opts.Store.ListAssetsByHashes(r.Context(), body.Hashes)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	type failure struct {
		Hash  string `json:"hash"`
		Error string `json:"error"`
	}
	failures := make([]failure, 0)
	exported := 0
	for _, a := range assets {
		if a.Status != "ready" {
			failures = append(failures, failure{Hash: a.Hash, Error: "图片状态为 " + a.Status + "，不可导出"})
			continue
		}
		src := h.opts.Paths.BlobPath(a.RelPath)
		info, statErr := os.Stat(src)
		if statErr != nil {
			failures = append(failures, failure{Hash: a.Hash, Error: "原图文件缺失"})
			continue
		}
		name, nameErr := uniqueExportName(targetDir, a.OriginalName, a.Hash)
		if nameErr != nil {
			failures = append(failures, failure{Hash: a.Hash, Error: nameErr.Error()})
			continue
		}
		if copyErr := copyFileVerified(src, filepath.Join(targetDir, name), info.Size()); copyErr != nil {
			failures = append(failures, failure{Hash: a.Hash, Error: copyErr.Error()})
			continue
		}
		exported++
	}

	h.ok(w, map[string]any{
		"exported":   exported,
		"target_dir": targetDir,
		"failures":   failures,
	})
}

func uniqueExportName(dir, originalName, hash string) (string, error) {
	name := filepath.Base(strings.TrimSpace(originalName))
	if name == "" || name == "." || name == ".." {
		name = hash + ".jpg"
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	candidate := name
	if !fileExists(filepath.Join(dir, candidate)) {
		return candidate, nil
	}
	candidate = fmt.Sprintf("%s__%s%s", stem, hash[:8], ext)
	for i := 2; i <= 100; i++ {
		if !fileExists(filepath.Join(dir, candidate)) {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s__%s-%d%s", stem, hash[:8], i, ext)
	}
	return "", fmt.Errorf("导出文件名冲突过多: %s", originalName)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFileVerified(src, dst string, wantSize int64) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开原图: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建导出文件: %w", err)
	}
	written, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(dst)
		return fmt.Errorf("复制文件: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(dst)
		return closeErr
	}
	if written != wantSize {
		os.Remove(dst)
		return fmt.Errorf("复制大小不一致: 源 %d 字节，写入 %d 字节", wantSize, written)
	}
	return nil
}
