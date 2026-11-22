package httpapi

import (
	"net/http"

	"iphoto/internal/maintenance"
)

// verifyLibrary 手动触发完整性校验（?full=1 时重算哈希）。
func (h *handler) verifyLibrary(w http.ResponseWriter, r *http.Request) {
	full := r.URL.Query().Get("full") == "1"
	report, err := maintenance.Verify(r.Context(), h.opts.Store, h.opts.Paths, full)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, report)
}

// listImportBatches 返回最近导入批次，供设置页清理历史。
func (h *handler) listImportBatches(w http.ResponseWriter, r *http.Request) {
	limit, err := intParam(r.URL.Query().Get("limit"), 20, 1, 200)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "limit 非法")
		return
	}
	batches, err := h.opts.Store.ListBatches(r.Context(), limit)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"items": batches, "total": len(batches)})
}
