package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"iphoto/internal/store"
)

func (h *handler) listAlbums(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	albums, err := h.opts.Store.ListAlbums(r.Context(), q.Get("q"), q.Get("type"))
	if err != nil {
		if errors.Is(err, store.ErrAlbumType) {
			h.fail(w, http.StatusBadRequest, err.Error())
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"items": albums, "total": len(albums)})
}

func (h *handler) createAlbum(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string          `json:"name"`
		Type        string          `json:"type"`
		Description *string         `json:"description"`
		RuleJSON    json.RawMessage `json:"rule_json"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	var rule *string
	if len(body.RuleJSON) > 0 && string(body.RuleJSON) != "null" {
		raw := string(body.RuleJSON)
		rule = &raw
	}
	album, err := h.opts.Store.CreateAlbum(r.Context(), body.Name, body.Type, body.Description, rule)
	if err != nil {
		if errors.Is(err, store.ErrAlbumType) {
			h.fail(w, http.StatusBadRequest, err.Error())
			return
		}
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	h.ok(w, album)
}

func (h *handler) getAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	album, err := h.opts.Store.GetAlbum(r.Context(), id)
	if err != nil {
		h.albumError(w, err)
		return
	}
	h.ok(w, album)
}

func (h *handler) updateAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Name        *string          `json:"name"`
		Description *string          `json:"description"`
		RuleJSON    *json.RawMessage `json:"rule_json"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	var rule *string
	if body.RuleJSON != nil && string(*body.RuleJSON) != "null" {
		raw := string(*body.RuleJSON)
		rule = &raw
	}
	if body.Name == nil && body.Description == nil && body.RuleJSON == nil {
		h.fail(w, http.StatusBadRequest, "没有需要更新的字段")
		return
	}
	album, err := h.opts.Store.UpdateAlbum(r.Context(), id, body.Name, body.Description, rule)
	if err != nil {
		h.albumError(w, err)
		return
	}
	h.ok(w, album)
}

func (h *handler) deleteAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.opts.Store.DeleteAlbum(r.Context(), id); err != nil {
		h.albumError(w, err)
		return
	}
	h.ok(w, map[string]any{"deleted": id})
}

func (h *handler) albumItems(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
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
	pageData, err := h.opts.Store.AlbumPage(r.Context(), id, q.Get("from"), q.Get("to"), page, pageSize)
	if err != nil {
		h.albumError(w, err)
		return
	}
	h.ok(w, pageData)
}

// albumMonths 返回相册内图片的年月聚合，供详情页时间筛选使用。
func (h *handler) albumMonths(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	months, err := h.opts.Store.AlbumMonths(r.Context(), id)
	if err != nil {
		h.albumError(w, err)
		return
	}
	h.ok(w, map[string]any{"months": months})
}

func (h *handler) addAlbumItems(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
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
	added, err := h.opts.Store.AddAlbumItems(r.Context(), id, body.Hashes)
	if err != nil {
		h.albumError(w, err)
		return
	}
	h.ok(w, map[string]any{"added": added})
}

func (h *handler) removeAlbumItems(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
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
	removed, err := h.opts.Store.RemoveAlbumItems(r.Context(), id, body.Hashes)
	if err != nil {
		h.albumError(w, err)
		return
	}
	h.ok(w, map[string]any{"removed": removed})
}

func (h *handler) albumError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		h.fail(w, http.StatusNotFound, "相册不存在")
	case errors.Is(err, store.ErrAlbumType):
		h.fail(w, http.StatusBadRequest, err.Error())
	case strings.Contains(err.Error(), "智能相册"), strings.Contains(err.Error(), "rule_json"):
		h.fail(w, http.StatusBadRequest, err.Error())
	default:
		h.fail(w, http.StatusInternalServerError, err.Error())
	}
}

func parseInt64(v string) (int64, error) {
	return strconv.ParseInt(v, 10, 64)
}
