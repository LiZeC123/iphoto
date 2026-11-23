package httpapi

import (
	"errors"
	"net/http"
	"os"

	"iphoto/internal/importer"
	"iphoto/internal/media"
	"iphoto/internal/store"
)

// assetPreview 返回浏览器可直接显示的 JPEG 预览图。
//
// TIFF 等浏览器无法直接渲染的格式（见 media.NeedsDisplayProxy）在首次请求时
// 由服务端解码并缓存到 runtime/previews/ab/cd/<hash>.jpg；其它格式直接回落到原图。
// 预览只是派生数据，原图与导出/导入流程都不受影响。
func (h *handler) assetPreview(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !importer.ValidateHash(hash) {
		h.fail(w, http.StatusBadRequest, "哈希格式非法")
		return
	}
	a, err := h.opts.Store.GetAsset(r.Context(), hash)
	if err != nil {
		h.assetError(w, err)
		return
	}
	if a.Status != "ready" && a.Status != "missing" {
		h.fail(w, http.StatusNotFound, "图片当前不可用（状态 "+a.Status+"）")
		return
	}
	if !media.NeedsDisplayProxy(a.Mime) {
		h.assetFile(w, r)
		return
	}

	dst := h.opts.Paths.PreviewPath(hash)
	if err := h.ensurePreview(a, dst); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			h.fail(w, http.StatusNotFound, "原图文件缺失，无法生成预览")
			return
		}
		h.fail(w, http.StatusInternalServerError, "生成预览失败: "+err.Error())
		return
	}

	f, err := os.Open(dst)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	// 预览内容只取决于不可变的原图，可长期缓存。
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, hash+".preview.jpg", stat.ModTime(), f)
}

// ensurePreview 在缓存缺失时同步生成预览；并发请求用全局锁串行化，
// 避免同一张大图被重复解码（预览生成只在查看器首次打开时发生，频率很低）。
func (h *handler) ensurePreview(a store.Asset, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	src := h.opts.Paths.BlobPath(a.RelPath)
	if _, err := os.Stat(src); err != nil {
		return err
	}

	h.previewMu.Lock()
	defer h.previewMu.Unlock()
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	return media.GeneratePreview(src, dst)
}
