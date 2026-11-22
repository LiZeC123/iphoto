package httpapi

import (
	"net/http"
)

// timelineMonths 返回有图片的年月及计数，供时间线导航使用。
func (h *handler) timelineMonths(w http.ResponseWriter, r *http.Request) {
	months, err := h.opts.Store.TimelineMonths(r.Context())
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"months": months})
}
