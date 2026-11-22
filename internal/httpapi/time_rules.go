package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"iphoto/internal/timeparse"
)

func (h *handler) listTimeRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.opts.Store.ListTimeRules(r.Context())
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"items": rules, "total": len(rules)})
}

func (h *handler) createTimeRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Pattern  string `json:"pattern"`
		Priority int    `json:"priority"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if body.Priority == 0 {
		body.Priority = 100
	}
	if err := validateTimeRule(body.Name, body.Pattern, body.Priority); err != nil {
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	rule, err := h.opts.Store.CreateTimeRule(r.Context(), strings.TrimSpace(body.Name), body.Pattern, body.Priority, enabled)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, rule)
}

func (h *handler) updateTimeRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Name     *string `json:"name"`
		Pattern  *string `json:"pattern"`
		Priority *int    `json:"priority"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if body.Name == nil && body.Pattern == nil && body.Priority == nil && body.Enabled == nil {
		h.fail(w, http.StatusBadRequest, "没有需要更新的字段")
		return
	}

	existing, err := h.opts.Store.GetTimeRule(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.fail(w, http.StatusNotFound, "规则不存在")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := existing.Name
	if body.Name != nil {
		name = *body.Name
	}
	pattern := existing.Pattern
	if body.Pattern != nil {
		pattern = *body.Pattern
	}
	priority := existing.Priority
	if body.Priority != nil {
		priority = *body.Priority
	}
	if err := validateTimeRule(name, pattern, priority); err != nil {
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}

	rule, err := h.opts.Store.UpdateTimeRule(r.Context(), id, body.Name, body.Pattern, body.Priority, body.Enabled)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, rule)
}

func (h *handler) deleteTimeRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.opts.Store.DeleteTimeRule(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.fail(w, http.StatusNotFound, "规则不存在")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"deleted": id})
}

// testTimeRule 仅测试正则，不写库。
func (h *handler) testTimeRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pattern  string `json:"pattern"`
		Filename string `json:"filename"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Filename) == "" {
		h.fail(w, http.StatusBadRequest, "filename 不能为空")
		return
	}
	takenAt, matched, err := timeparse.TestPattern(body.Pattern, body.Filename)
	if err != nil {
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	h.ok(w, map[string]any{"matched": matched, "taken_at": takenAt})
}

func validateTimeRule(name, pattern string, priority int) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("规则名不能为空")
	}
	if priority < 1 || priority > 10000 {
		return errors.New("priority 需为 1–10000")
	}
	if err := timeparse.ValidatePattern(pattern); err != nil {
		return err
	}
	return nil
}
