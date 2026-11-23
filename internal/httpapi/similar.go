package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"iphoto/internal/similar"
)

// startSimilar 启动一次相似图片检测（异步任务）。
func (h *handler) startSimilar(w http.ResponseWriter, r *http.Request) {
	if h.opts.Similar == nil {
		h.fail(w, http.StatusServiceUnavailable, "相似检测服务未初始化")
		return
	}
	var body struct {
		From      string `json:"from"`
		Algorithm string `json:"algorithm"`
		Threshold *int   `json:"threshold"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}

	from := strings.TrimSpace(body.From)
	if from != "" {
		normalized, err := normalizeTime(from)
		if err != nil {
			h.fail(w, http.StatusBadRequest, "起始时间格式需为 YYYY-MM-DD 或 YYYY-MM-DDTHH:MM:SS")
			return
		}
		from = normalized
	}
	threshold := similar.DefaultThreshold
	if body.Threshold != nil {
		threshold = *body.Threshold
	}

	id, err := h.opts.Similar.Start(similar.Options{
		From:      from,
		Algorithm: body.Algorithm,
		Threshold: threshold,
	})
	if err != nil {
		switch {
		case errors.Is(err, similar.ErrBusy):
			h.fail(w, http.StatusConflict, "已有检测任务在执行，请等待完成或取消")
		case errors.Is(err, similar.ErrAlgorithm):
			h.fail(w, http.StatusBadRequest, "暂只支持 dhash 算法")
		case errors.Is(err, similar.ErrThreshold):
			h.fail(w, http.StatusBadRequest, "阈值需为 0–64 的整数")
		default:
			h.fail(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// 记住起始时间，供下次打开页面时回填。
	if err := h.opts.Store.SetSettings(r.Context(), map[string]string{"similar_from": from}); err != nil {
		h.log.Warn("保存相似检测起始时间失败", "err", err)
	}
	h.ok(w, map[string]any{"id": id})
}

// similarProgress 返回当前/最近一次检测的进度。
func (h *handler) similarProgress(w http.ResponseWriter, r *http.Request) {
	if h.opts.Similar == nil {
		h.fail(w, http.StatusServiceUnavailable, "相似检测服务未初始化")
		return
	}
	h.ok(w, h.opts.Similar.Progress())
}

// similarResult 返回最近一次检测的完整结果。
func (h *handler) similarResult(w http.ResponseWriter, r *http.Request) {
	if h.opts.Similar == nil {
		h.fail(w, http.StatusServiceUnavailable, "相似检测服务未初始化")
		return
	}
	result, err := h.opts.Similar.Result()
	if err != nil {
		if errors.Is(err, similar.ErrNoResult) {
			h.fail(w, http.StatusNotFound, "暂无检测结果")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, result)
}

// cancelSimilar 取消正在执行的检测任务。
func (h *handler) cancelSimilar(w http.ResponseWriter, r *http.Request) {
	if h.opts.Similar == nil {
		h.fail(w, http.StatusServiceUnavailable, "相似检测服务未初始化")
		return
	}
	if err := h.opts.Similar.Cancel(); err != nil {
		if errors.Is(err, similar.ErrNotRunning) {
			h.fail(w, http.StatusNotFound, "没有正在执行的检测任务")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"status": "cancelling"})
}
