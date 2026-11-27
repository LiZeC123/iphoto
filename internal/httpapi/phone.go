package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"iphoto/internal/importer"
	"iphoto/internal/phonedock"
	"iphoto/internal/store"
)

// listPhoneDevices 返回已登记的手机设备（不含 Token），附同步账本统计。
func (h *handler) listPhoneDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := h.opts.Store.ListPhoneDevices(r.Context())
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	type deviceItem struct {
		store.PhoneDevice
		Counts map[string]int64 `json:"counts"`
	}
	items := make([]deviceItem, 0, len(devices))
	for _, d := range devices {
		counts, err := h.opts.Store.PhoneMediaCounts(r.Context(), d.ID)
		if err != nil {
			h.fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, deviceItem{PhoneDevice: d, Counts: counts})
	}
	h.ok(w, map[string]any{"items": items, "total": len(items)})
}

// probePhone 检查手机连通性、鉴权与待同步数量，不写任何数据。
func (h *handler) probePhone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address string `json:"address"`
		Token   string `json:"token"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	probe, err := importer.ProbePhone(r.Context(), h.opts.Store, body.Address, body.Token)
	if err != nil {
		if errors.Is(err, importer.ErrPhoneInput) {
			h.fail(w, http.StatusBadRequest, inputMessage(err))
			return
		}
		h.failPhone(w, err)
		return
	}
	h.ok(w, probe)
}

// syncPhone 开始一轮手机同步，异步执行并立即返回 batch_id。
// 进度、取消、CSV 报告复用 /api/import/{batch}/*。
func (h *handler) syncPhone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address string   `json:"address"`
		Token   string   `json:"token"`
		Mode    string   `json:"mode"`
		Tags    []string `json:"tags"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Address) == "" {
		h.fail(w, http.StatusBadRequest, "address 不能为空")
		return
	}
	if len(body.Tags) > 20 {
		h.fail(w, http.StatusBadRequest, "单次同步最多添加 20 个标签")
		return
	}
	if h.opts.Importer == nil {
		h.fail(w, http.StatusServiceUnavailable, "导入服务未初始化")
		return
	}

	batchID, err := h.opts.Importer.StartPhone(importer.PhoneOptions{
		Address: body.Address,
		Token:   body.Token,
		Mode:    body.Mode,
		Tags:    body.Tags,
	})
	if err != nil {
		switch {
		case errors.Is(err, importer.ErrBusy):
			h.fail(w, http.StatusConflict, "已有导入任务在执行，请等待完成或取消")
		case errors.Is(err, importer.ErrPhoneInput):
			h.fail(w, http.StatusBadRequest, inputMessage(err))
		default:
			h.failPhone(w, err)
		}
		return
	}
	h.ok(w, map[string]any{"batch_id": batchID})
}

// forgetPhoneDevice 删除设备与同步账本（库内图片不受影响）。
func (h *handler) forgetPhoneDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		h.fail(w, http.StatusBadRequest, "设备 ID 非法")
		return
	}
	if h.opts.Importer != nil && h.opts.Importer.ActiveBatchID() != 0 {
		h.fail(w, http.StatusConflict, "有导入任务正在执行，请先取消或等待完成")
		return
	}
	if _, err := h.opts.Store.GetPhoneDevice(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.fail(w, http.StatusNotFound, "设备不存在")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.opts.Store.DeletePhoneDevice(r.Context(), id); err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"deleted": true})
}

// failPhone 把手机侧错误翻成页面能直接显示的中文。
func (h *handler) failPhone(w http.ResponseWriter, err error) {
	h.log.Warn("手机接口调用失败", "err", err)
	h.fail(w, http.StatusBadGateway, phonedock.UserMessage(err))
}

// inputMessage 去掉 sentinel 前缀，只留具体原因。
func inputMessage(err error) string {
	msg := err.Error()
	if trimmed := strings.TrimPrefix(msg, importer.ErrPhoneInput.Error()+": "); trimmed != msg {
		return trimmed
	}
	return msg
}
