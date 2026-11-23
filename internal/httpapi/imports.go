package httpapi

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"iphoto/internal/importer"
)

// scanImport 只统计与预览，不产生任何写入。
func (h *handler) scanImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		h.fail(w, http.StatusBadRequest, "path 不能为空")
		return
	}
	scan, err := importer.Scan(body.Path)
	if err != nil {
		if errors.Is(err, importer.ErrNotDirectory) {
			h.fail(w, http.StatusBadRequest, "路径不存在、不可读或不是目录")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{
		"path":        scan.Path,
		"total":       scan.Total,
		"unreadable":  scan.Unreadable,
		"unsupported": scan.Unsupported,
	})
}

// startImport 创建批次并异步执行，立即返回 batch_id。
func (h *handler) startImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string   `json:"path"`
		Tags []string `json:"tags"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		h.fail(w, http.StatusBadRequest, "path 不能为空")
		return
	}
	if len(body.Tags) > 20 {
		h.fail(w, http.StatusBadRequest, "单次导入最多添加 20 个标签")
		return
	}
	if h.opts.Importer == nil {
		h.fail(w, http.StatusServiceUnavailable, "导入服务未初始化")
		return
	}

	batchID, err := h.opts.Importer.Start(body.Path, body.Tags)
	if err != nil {
		switch {
		case errors.Is(err, importer.ErrBusy):
			h.fail(w, http.StatusConflict, "已有导入任务在执行，请等待完成或取消")
		case errors.Is(err, importer.ErrNotDirectory):
			h.fail(w, http.StatusBadRequest, "路径不存在、不可读或不是目录")
		default:
			h.fail(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// 记住上次导入路径，供导入页回填。
	if abs, absErr := filepath.Abs(body.Path); absErr == nil {
		if err := h.opts.Store.SetSettings(r.Context(), map[string]string{"last_import_path": abs}); err != nil {
			h.log.Warn("保存上次导入路径失败", "err", err)
		}
	}

	h.ok(w, map[string]any{"batch_id": batchID})
}

func (h *handler) activeImport(w http.ResponseWriter, r *http.Request) {
	id := int64(0)
	if h.opts.Importer != nil {
		id = h.opts.Importer.ActiveBatchID()
	}
	h.ok(w, map[string]any{"batch_id": id})
}

func (h *handler) importProgress(w http.ResponseWriter, r *http.Request) {
	batchID, err := parseBatchID(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "批次 ID 非法")
		return
	}
	snapshot, err := h.opts.Importer.Progress(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.fail(w, http.StatusNotFound, "导入批次不存在")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, snapshot)
}

func (h *handler) cancelImport(w http.ResponseWriter, r *http.Request) {
	batchID, err := parseBatchID(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "批次 ID 非法")
		return
	}
	if h.opts.Importer == nil {
		h.fail(w, http.StatusServiceUnavailable, "导入服务未初始化")
		return
	}
	if err := h.opts.Importer.Cancel(batchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.fail(w, http.StatusNotFound, "该批次未在执行（可能已完成）")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"batch_id": batchID, "status": "cancelling"})
}

func (h *handler) importDuplicates(w http.ResponseWriter, r *http.Request) {
	batchID, err := parseBatchID(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "批次 ID 非法")
		return
	}
	if _, err := h.opts.Store.GetBatch(r.Context(), batchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.fail(w, http.StatusNotFound, "导入批次不存在")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := h.opts.Store.ListDuplicateItems(r.Context(), batchID)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range items {
		if stat, statErr := os.Stat(items[i].SourcePath); statErr == nil {
			items[i].IncomingSize = stat.Size()
		}
	}
	h.ok(w, map[string]any{"items": items})
}

func (h *handler) resolveDuplicates(w http.ResponseWriter, r *http.Request) {
	batchID, err := parseBatchID(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "批次 ID 非法")
		return
	}
	var body struct {
		Items []importer.Resolution `json:"items"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体非法: "+err.Error())
		return
	}
	if len(body.Items) == 0 || len(body.Items) > 500 {
		h.fail(w, http.StatusBadRequest, "items 数量需为 1–500")
		return
	}
	result, err := h.opts.Importer.ResolveDuplicates(r.Context(), batchID, body.Items)
	if err != nil {
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	h.ok(w, result)
}

func (h *handler) importReport(w http.ResponseWriter, r *http.Request) {
	batchID, err := parseBatchID(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "批次 ID 非法")
		return
	}
	batch, err := h.opts.Store.GetBatch(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.fail(w, http.StatusNotFound, "导入批次不存在")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := h.opts.Store.ReportRows(r.Context(), batchID)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	unsupported := importer.UnsupportedFromStats(batch.StatsJSON)

	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // BOM，便于 Excel 正确识别 UTF-8
	cw := csv.NewWriter(&buf)
	if err := cw.Write([]string{"source_path", "hash", "status", "message"}); err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, row := range rows {
		if err := cw.Write([]string{row.SourcePath, row.Hash, row.Status, row.Message}); err != nil {
			h.fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	// 类型不受支持的文件不在 import_items 中，额外补进报告，避免「没有记录 = 没被处理」。
	for _, file := range unsupported.Files {
		full := filepath.Join(batch.SourcePath, filepath.FromSlash(file))
		ext := strings.ToLower(filepath.Ext(file))
		if ext == "" {
			ext = "(无后缀)"
		}
		if err := cw.Write([]string{full, "", "unsupported", "不支持的文件类型 " + ext}); err != nil {
			h.fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if unsupported.Truncated {
		if err := cw.Write([]string{"", "", "unsupported_summary",
			fmt.Sprintf("共 %d 个不支持的文件，报告只列出前 %d 个", unsupported.Total, len(unsupported.Files))}); err != nil {
			h.fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="import-%d.csv"`, batchID))
	w.Header().Set("Content-Length", fmt.Sprint(buf.Len()))
	if _, err := io.Copy(w, &buf); err != nil {
		h.log.Error("写入 CSV 失败", "err", err)
	}
}

func (h *handler) cleanupImport(w http.ResponseWriter, r *http.Request) {
	batchID, err := parseBatchID(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "批次 ID 非法")
		return
	}
	if h.opts.Importer != nil && h.opts.Importer.ActiveBatchID() == batchID {
		h.fail(w, http.StatusConflict, "批次仍在执行，无法清理")
		return
	}
	if _, err := h.opts.Store.GetBatch(r.Context(), batchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.fail(w, http.StatusNotFound, "导入批次不存在")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	deleted, err := h.opts.Store.CleanupImportItems(r.Context(), batchID)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, map[string]any{"deleted": deleted})
}
