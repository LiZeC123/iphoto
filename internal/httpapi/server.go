// Package httpapi 提供 /api 路由与前端静态资源（含 SPA fallback）。
package httpapi

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/importer"
	"iphoto/internal/store"
	"iphoto/internal/thumbs"
)

// Options 汇总路由依赖。
type Options struct {
	Store    *store.Store
	Paths    config.Paths
	Web      fs.FS
	Version  string
	Logger   *slog.Logger
	Importer *importer.Manager
	Thumbs   *thumbs.Queue
	// Shutdown 由 main 注入，用于设置页的「关闭服务」按钮触发优雅退出。
	Shutdown func()
}

type handler struct {
	opts Options
	log  *slog.Logger
}

// New 组装完整的 http.Handler。
func New(opts Options) http.Handler {
	h := &handler{opts: opts, log: opts.Logger}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", h.health)
	mux.HandleFunc("GET /api/settings", h.getSettings)
	mux.HandleFunc("POST /api/settings/verify", h.verifyLibrary)
	mux.HandleFunc("PUT /api/settings", h.putSettings)
	mux.HandleFunc("POST /api/shutdown", h.shutdown)

	mux.HandleFunc("GET /api/assets", h.listAssets)
	mux.HandleFunc("POST /api/assets/batch/tags", h.batchTags)
	mux.HandleFunc("POST /api/assets/batch/export", h.exportAssets)
	mux.HandleFunc("POST /api/assets/batch/delete", h.batchDelete)
	mux.HandleFunc("GET /api/assets/{hash}", h.getAsset)
	mux.HandleFunc("PATCH /api/assets/{hash}", h.patchAsset)
	mux.HandleFunc("POST /api/assets/{hash}/rename", h.renameAsset)
	mux.HandleFunc("POST /api/assets/{hash}/reveal", h.revealAsset)
	mux.HandleFunc("DELETE /api/assets/{hash}", h.deleteAsset)
	mux.HandleFunc("GET /api/assets/{hash}/file", h.assetFile)
	mux.HandleFunc("GET /api/assets/{hash}/thumb", h.assetThumb)

	mux.HandleFunc("GET /api/assets/{hash}/tags", h.assetTags)

	mux.HandleFunc("GET /api/tags", h.listTags)
	mux.HandleFunc("POST /api/tags", h.createTag)
	mux.HandleFunc("POST /api/tags/summary", h.tagsSummary)
	mux.HandleFunc("PATCH /api/tags/{id}", h.renameTag)
	mux.HandleFunc("DELETE /api/tags/{id}", h.deleteTag)

	mux.HandleFunc("GET /api/albums", h.listAlbums)
	mux.HandleFunc("POST /api/albums", h.createAlbum)
	mux.HandleFunc("GET /api/albums/{id}", h.getAlbum)
	mux.HandleFunc("PATCH /api/albums/{id}", h.updateAlbum)
	mux.HandleFunc("DELETE /api/albums/{id}", h.deleteAlbum)
	mux.HandleFunc("GET /api/albums/{id}/items", h.albumItems)
	mux.HandleFunc("GET /api/albums/{id}/months", h.albumMonths)
	mux.HandleFunc("POST /api/albums/{id}/items", h.addAlbumItems)
	mux.HandleFunc("DELETE /api/albums/{id}/items", h.removeAlbumItems)

	mux.HandleFunc("GET /api/timeline/months", h.timelineMonths)

	mux.HandleFunc("GET /api/trash", h.listTrash)
	mux.HandleFunc("POST /api/trash/restore", h.restoreTrash)
	mux.HandleFunc("POST /api/trash/empty", h.emptyTrash)

	mux.HandleFunc("GET /api/time-rules", h.listTimeRules)
	mux.HandleFunc("POST /api/time-rules", h.createTimeRule)
	mux.HandleFunc("PATCH /api/time-rules/{id}", h.updateTimeRule)
	mux.HandleFunc("DELETE /api/time-rules/{id}", h.deleteTimeRule)
	mux.HandleFunc("POST /api/time-rules/test", h.testTimeRule)

	mux.HandleFunc("POST /api/import/scan", h.scanImport)
	mux.HandleFunc("POST /api/import/start", h.startImport)
	mux.HandleFunc("GET /api/import/active", h.activeImport)
	mux.HandleFunc("GET /api/import/batches", h.listImportBatches)
	mux.HandleFunc("GET /api/import/{batch}/progress", h.importProgress)
	mux.HandleFunc("POST /api/import/{batch}/cancel", h.cancelImport)
	mux.HandleFunc("GET /api/import/{batch}/duplicates", h.importDuplicates)
	mux.HandleFunc("POST /api/import/{batch}/duplicates/resolve", h.resolveDuplicates)
	mux.HandleFunc("GET /api/import/{batch}/report.csv", h.importReport)
	mux.HandleFunc("POST /api/import/{batch}/cleanup", h.cleanupImport)

	// 兜底：/api 前缀返回 JSON 404，其余交给 SPA 静态资源。
	// 用单一 "/" 模式分发，避免 net/http 报 “更具体路径 vs 更多方法” 的模式冲突。
	spa := spaHandler(opts.Web, h.log)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
			h.apiNotFound(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	})

	return requestLogger(h.log, mux)
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	version, err := h.opts.Store.SchemaVersion()
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "读取数据库版本失败: "+err.Error())
		return
	}
	active := int64(0)
	if h.opts.Importer != nil {
		active = h.opts.Importer.ActiveBatchID()
	}
	h.ok(w, map[string]any{
		"status":         "ok",
		"version":        h.opts.Version,
		"schema_version": version,
		"time":           time.Now().Format(time.RFC3339),
		"library":        h.opts.Paths.Library,
		"runtime":        h.opts.Paths.Runtime,
		"port":           config.Port,
		"active_import":  active,
	})
}

// settings 仅允许 app_settings 中的键，键值均为字符串。
func (h *handler) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.opts.Store.Settings(r.Context())
	if err != nil {
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.ok(w, settings)
}

func (h *handler) putSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := decodeJSON(w, r, &body); err != nil {
		h.fail(w, http.StatusBadRequest, "请求体需为 {\"key\":\"value\"} 字符串映射: "+err.Error())
		return
	}
	if err := h.opts.Store.SetSettings(r.Context(), body); err != nil {
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	h.ok(w, map[string]any{"updated": len(body)})
}

func (h *handler) apiNotFound(w http.ResponseWriter, r *http.Request) {
	h.fail(w, http.StatusNotFound, "接口不存在: "+r.Method+" "+r.URL.Path)
}

func (h *handler) ok(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.log.Error("写入响应失败", "err", err)
	}
}

func (h *handler) fail(w http.ResponseWriter, status int, msg string) {
	if err := jsonError(w, status, msg); err != nil {
		h.log.Error("写入错误响应失败", "err", err)
	}
}

func jsonError(w http.ResponseWriter, status int, msg string) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// decodeJSON 限制 1MB 且拒绝未知字段，避免前端拼写错误被静默忽略。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("请求体只能包含一个 JSON 值")
	}
	return nil
}

func parseBatchID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("batch"), 10, 64)
}

// spaHandler 提供 dist 静态文件；非 API 的未知路径回落到 index.html，
// 以支持前端 history 路由。
func spaHandler(webFS fs.FS, log *slog.Logger) http.Handler {
	files := http.FileServer(http.FS(webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		if _, err := fs.Stat(webFS, name); err == nil {
			files.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(webFS, "index.html")
		if err != nil {
			// 仓库只保留 web/dist/.gitkeep，未构建前端时给出可读提示。
			log.Warn("前端资源缺失，请先执行 make web-build")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(frontendMissingPage))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
}

const frontendMissingPage = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>iPhoto · 前端未构建</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#14161a;color:#e6e9ef;
font-family:-apple-system,'PingFang SC',sans-serif}
main{max-width:560px;padding:32px;border:1px solid #2a3038;border-radius:12px;background:#1b1f26;line-height:1.7}
code{color:#4c8dff}
</style></head>
<body><main>
<h1>前端资源未构建</h1>
<p>当前二进制中只有占位文件，没有 Vite 构建产物。</p>
<p>请先在项目根执行 <code>make web-build</code>（或 <code>cd web &amp;&amp; npm install &amp;&amp; npm run build</code>），再重新编译并启动。</p>
</main></body></html>
`

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func requestLogger(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Info("api", "method", r.Method, "path", r.URL.Path, "status", rec.status, "dur_ms", time.Since(start).Milliseconds())
		}
	})
}
