package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"iphoto/internal/importer"
	"iphoto/internal/store"
)

var allowedStatuses = map[string]bool{
	"importing": true, "ready": true, "trashed": true, "missing": true,
}

// listAssets 支持 q / status / from / to / sort / page / page_size。
func (h *handler) listAssets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	if status != "" && !allowedStatuses[status] {
		h.fail(w, http.StatusBadRequest, "status 取值非法: "+status)
		return
	}
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

	pageResult, err := h.opts.Store.ListAssets(r.Context(), store.AssetFilter{
		Q:        q.Get("q"),
		Tag:      q.Get("tag"),
		Status:   status,
		From:     q.Get("from"),
		To:       q.Get("to"),
		Sort:     q.Get("sort"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, pageResult)
}

type assetDetail struct {
	store.Asset
	Tags []string `json:"tags"`
}

func (h *handler) getAsset(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !validHashParam(w, h, hash) {
		return
	}
	a, err := h.opts.Store.GetAsset(r.Context(), hash)
	if err != nil {
		h.assetError(w, err)
		return
	}
	tags, err := h.opts.Store.TagsForAsset(r.Context(), hash)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, tag.Name)
	}
	h.ok(w, assetDetail{Asset: a, Tags: names})
}

func (h *handler) patchAsset(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !importer.ValidateHash(hash) {
		h.fail(w, http.StatusBadRequest, "哈希格式非法")
		return
	}
	var body struct {
		Note            *string `json:"note"`
		TakenAtOverride *string `json:"taken_at_override"`
		OriginalName    *string `json:"original_name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if body.Note == nil && body.TakenAtOverride == nil && body.OriginalName == nil {
		h.fail(w, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	if body.TakenAtOverride != nil && *body.TakenAtOverride != "" {
		normalized, err := normalizeTime(*body.TakenAtOverride)
		if err != nil {
			h.fail(w, http.StatusBadRequest, err.Error())
			return
		}
		body.TakenAtOverride = &normalized
	}

	a, err := h.opts.Store.PatchAsset(r.Context(), hash, store.AssetPatch{
		Note:            body.Note,
		TakenAtOverride: body.TakenAtOverride,
		OriginalName:    body.OriginalName,
	})
	if err != nil {
		if strings.Contains(err.Error(), "original_name") {
			h.fail(w, http.StatusBadRequest, err.Error())
			return
		}
		h.assetError(w, err)
		return
	}
	h.ok(w, a)
}

func (h *handler) deleteAsset(w http.ResponseWriter, r *http.Request) {
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
	if err := h.trashAsset(r.Context(), a); err != nil {
		h.fail(w, http.StatusInternalServerError, "移入回收站失败: "+err.Error())
		return
	}
	h.ok(w, map[string]any{"hash": hash, "status": "trashed"})
}

func (h *handler) batchDelete(w http.ResponseWriter, r *http.Request) {
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

	type result struct {
		Hash  string `json:"hash"`
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(body.Hashes))
	for _, hash := range body.Hashes {
		if !importer.ValidateHash(hash) {
			results = append(results, result{Hash: hash, Error: "哈希格式非法"})
			continue
		}
		a, err := h.opts.Store.GetAsset(r.Context(), hash)
		if err != nil {
			results = append(results, result{Hash: hash, Error: "库内不存在"})
			continue
		}
		if err := h.trashAsset(r.Context(), a); err != nil {
			results = append(results, result{Hash: hash, Error: err.Error()})
			continue
		}
		results = append(results, result{Hash: hash, OK: true})
	}
	h.ok(w, map[string]any{"results": results})
}

// trashAsset 把物理文件移动到 runtime/trash/<hash>.<ext> 并软删除记录。
func (h *handler) trashAsset(ctx context.Context, a store.Asset) error {
	if a.Status == "trashed" {
		return nil
	}
	ext := filepath.Ext(a.StorageName)
	if ext == "" {
		ext = filepath.Ext(a.RelPath)
	}
	src := h.opts.Paths.BlobPath(a.RelPath)
	dst := filepath.Join(h.opts.Paths.Trash, a.Hash+ext)

	if _, err := os.Stat(src); err == nil {
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		h.log.Warn("删除时原图缺失，仅更新状态", "hash", a.Hash, "rel_path", a.RelPath)
	}

	now := time.Now().Format(store.TimeLayout)
	return h.opts.Store.SetAssetStatus(ctx, a.Hash, "trashed", &now)
}

func (h *handler) assetFile(w http.ResponseWriter, r *http.Request) {
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
	f, err := os.Open(h.opts.Paths.BlobPath(a.RelPath))
	if err != nil {
		h.fail(w, http.StatusNotFound, "原图文件缺失")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	if a.Mime != "" {
		w.Header().Set("Content-Type", a.Mime)
	} else if ct := mime.TypeByExtension(filepath.Ext(a.StorageName)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(a.StorageName))
	// ServeContent 处理 Range / If-Modified-Since，供大图查看器使用。
	http.ServeContent(w, r, a.StorageName, stat.ModTime(), f)
}

func (h *handler) assetThumb(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !importer.ValidateHash(hash) {
		h.fail(w, http.StatusBadRequest, "哈希格式非法")
		return
	}
	thumbPath := h.opts.Paths.ThumbPath(hash)
	f, err := os.Open(thumbPath)
	if err != nil {
		// 缩略图不存在时排队生成，本次返回 404，前端显示占位符。
		if a, getErr := h.opts.Store.GetAsset(r.Context(), hash); getErr == nil {
			if a.Status == "ready" && h.opts.Thumbs != nil {
				h.opts.Thumbs.Enqueue(a.Hash, a.RelPath)
			}
		}
		h.fail(w, http.StatusNotFound, "缩略图尚未生成")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	// 缩略图按哈希命名，内容不可变，可长期缓存。
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, hash+".jpg", stat.ModTime(), f)
}

func validHashParam(w http.ResponseWriter, h *handler, hash string) bool {
	if !importer.ValidateHash(hash) {
		h.fail(w, http.StatusBadRequest, "哈希格式非法")
		return false
	}
	return true
}

func (h *handler) assetError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		h.fail(w, http.StatusNotFound, "图片不存在")
		return
	}
	h.fail(w, http.StatusInternalServerError, err.Error())
}

func intParam(raw string, def, min, max int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if v < min || v > max {
		return 0, fmt.Errorf("超出范围 %d–%d", min, max)
	}
	return v, nil
}

// normalizeTime 接受 2006-01-02 或 2006-01-02T15:04:05 / 带分钟的 datetime-local。
func normalizeTime(v string) (string, error) {
	v = strings.TrimSpace(v)
	layouts := []string{store.TimeLayout, "2006-01-02T15:04", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t.Format(store.TimeLayout), nil
		}
	}
	return "", fmt.Errorf("时间格式需为 YYYY-MM-DD 或 YYYY-MM-DDTHH:MM:SS")
}
