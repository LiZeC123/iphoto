package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"iphoto/internal/store"
)

func (h *handler) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.opts.Store.ListTags(r.Context())
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"items": tags, "total": len(tags)})
}

func (h *handler) createTag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	tag, err := h.opts.Store.CreateTag(r.Context(), body.Name)
	if err != nil {
		if errors.Is(err, store.ErrTagExists) {
			h.fail(w, http.StatusConflict, "标签已存在")
			return
		}
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	h.ok(w, tag)
}

func (h *handler) renameTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	tag, err := h.opts.Store.RenameTag(r.Context(), id, body.Name)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTagExists):
			h.fail(w, http.StatusConflict, "标签已存在")
		case errors.Is(err, sql.ErrNoRows):
			h.fail(w, http.StatusNotFound, "标签不存在")
		default:
			h.fail(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	h.ok(w, tag)
}

func (h *handler) deleteTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.opts.Store.DeleteTag(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.fail(w, http.StatusNotFound, "标签不存在")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"deleted": id})
}

// batchTags 批量加/移除标签：body {hashes, add_tags, remove_tags}。
func (h *handler) batchTags(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hashes     []string `json:"hashes"`
		AddTags    []string `json:"add_tags"`
		RemoveTags []string `json:"remove_tags"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if len(body.Hashes) == 0 || len(body.Hashes) > 500 {
		h.fail(w, http.StatusBadRequest, "hashes 数量需为 1–500")
		return
	}
	if len(body.AddTags) > 50 || len(body.RemoveTags) > 50 {
		h.fail(w, http.StatusBadRequest, "单次最多操作 50 个标签")
		return
	}
	if len(body.AddTags) == 0 && len(body.RemoveTags) == 0 {
		h.fail(w, http.StatusBadRequest, "add_tags 与 remove_tags 至少提供一个")
		return
	}

	if len(body.AddTags) > 0 {
		if err := h.opts.Store.AddTagsToAssets(r.Context(), body.Hashes, body.AddTags); err != nil {
			h.fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if len(body.RemoveTags) > 0 {
		if err := h.opts.Store.RemoveTagsFromAssets(r.Context(), body.Hashes, body.RemoveTags); err != nil {
			h.fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	h.ok(w, map[string]any{"hashes": len(body.Hashes)})
}

// tagsSummary 返回所选图片的标签覆盖统计。
func (h *handler) tagsSummary(w http.ResponseWriter, r *http.Request) {
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
	tags, err := h.opts.Store.TagsSummary(r.Context(), body.Hashes)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"items": tags, "total": len(body.Hashes)})
}

// assetTags 返回单张图片的标签列表。
func (h *handler) assetTags(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !validHashParam(w, h, hash) {
		return
	}
	if _, err := h.opts.Store.GetAsset(r.Context(), hash); err != nil {
		h.assetError(w, err)
		return
	}
	tags, err := h.opts.Store.TagsForAsset(r.Context(), hash)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"items": tags, "total": len(tags)})
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	raw := strings.TrimSpace(r.PathValue(name))
	if raw == "" {
		_ = jsonError(w, http.StatusBadRequest, "缺少路径参数 "+name)
		return 0, false
	}
	id, err := parseInt64(raw)
	if err != nil || id <= 0 {
		_ = jsonError(w, http.StatusBadRequest, "路径参数 "+name+" 非法")
		return 0, false
	}
	return id, true
}
