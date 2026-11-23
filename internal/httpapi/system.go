package httpapi

import (
	"net/http"
	"strings"
)

// shutdown 处理设置页的「关闭服务」请求。
// 先写回响应，再在后台触发主循环的优雅退出（关闭 HTTP 服务、取消导入、停止缩略图队列）。
func (h *handler) shutdown(w http.ResponseWriter, r *http.Request) {
	// 要求 application/json：跨站表单无法直接发送该类型（会先触发预检），
	// 从而避免任意网页通过简单请求关闭本地服务。
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		h.fail(w, http.StatusUnsupportedMediaType, "需要 Content-Type: application/json")
		return
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if !body.Confirm {
		h.fail(w, http.StatusBadRequest, "需要 confirm=true 才能关闭服务")
		return
	}
	if h.opts.Shutdown == nil {
		h.fail(w, http.StatusServiceUnavailable, "关闭功能不可用")
		return
	}
	h.log.Info("收到关闭服务请求", "remote", r.RemoteAddr)
	h.ok(w, map[string]any{"status": "shutting_down"})
	// 响应写出后再触发退出：http.Server.Shutdown 会等待当前请求结束。
	go h.opts.Shutdown()
}
