package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (h *handler) listTrash(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := intParam(q.Get("page"), 1, 1, 1<<30)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "page 非法")
		return
	}
	pageSize, err := intParam(q.Get("page_size"), 100, 1, 500)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "page_size 需为 1–500")
		return
	}
	data, err := h.opts.Store.ListTrash(r.Context(), page, pageSize)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, data)
}

type trashResult struct {
	Hash  string `json:"hash"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// restoreTrash 把 runtime/trash 中的文件移回原 rel_path 并恢复状态。
func (h *handler) restoreTrash(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hashes []string `json:"hashes"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if len(body.Hashes) == 0 || len(body.Hashes) > 500 {
		h.fail(w, http.StatusBadRequest, "hashes 数量需为 1–500")
		return
	}

	results := make([]trashResult, 0, len(body.Hashes))
	restored := 0
	for _, hash := range body.Hashes {
		a, err := h.opts.Store.GetAsset(r.Context(), hash)
		if err != nil {
			results = append(results, trashResult{Hash: hash, Error: "库内不存在"})
			continue
		}
		if a.Status != "trashed" {
			results = append(results, trashResult{Hash: hash, Error: "该图片不在回收站"})
			continue
		}
		ext := filepath.Ext(a.StorageName)
		if ext == "" {
			ext = filepath.Ext(a.RelPath)
		}
		src := filepath.Join(h.opts.Paths.Trash, a.Hash+ext)
		dst := h.opts.Paths.BlobPath(a.RelPath)
		if _, statErr := os.Stat(src); statErr != nil {
			results = append(results, trashResult{Hash: hash, Error: "回收站文件缺失，无法恢复"})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			results = append(results, trashResult{Hash: hash, Error: err.Error()})
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			results = append(results, trashResult{Hash: hash, Error: "移回原路径失败: " + err.Error()})
			continue
		}
		if err := h.opts.Store.RestoreAsset(r.Context(), hash); err != nil {
			results = append(results, trashResult{Hash: hash, Error: err.Error()})
			continue
		}
		results = append(results, trashResult{Hash: hash, OK: true})
		restored++
	}
	h.ok(w, map[string]any{"restored": restored, "results": results})
}

// emptyTrash 物理删除回收站文件与数据库记录（前端需二次确认）。
func (h *handler) emptyTrash(w http.ResponseWriter, r *http.Request) {
	assets, err := h.opts.Store.TrashedAssets(r.Context())
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	deletedFiles := 0
	for _, a := range assets {
		ext := filepath.Ext(a.StorageName)
		if ext == "" {
			ext = filepath.Ext(a.RelPath)
		}
		path := filepath.Join(h.opts.Paths.Trash, a.Hash+ext)
		if err := os.Remove(path); err == nil {
			deletedFiles++
		} else if !os.IsNotExist(err) {
			h.log.Warn("删除回收站文件失败", "path", path, "err", err)
		}
	}
	rows, err := h.opts.Store.PurgeTrash(r.Context())
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 清理回收站目录中残留的空目录。
	entries, _ := os.ReadDir(h.opts.Paths.Trash)
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			os.Remove(filepath.Join(h.opts.Paths.Trash, entry.Name()))
		}
	}
	h.ok(w, map[string]any{"deleted_records": rows, "deleted_files": deletedFiles})
}
