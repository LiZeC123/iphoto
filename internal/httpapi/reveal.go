package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"iphoto/internal/importer"
)

// revealAsset 在系统文件浏览器中显示图片所在的目录。
// 跨平台实现：macOS 用 Finder 定位文件，Windows 用资源管理器选中文件，
// Linux/其它用 xdg-open 打开目录。
func (h *handler) revealAsset(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !importer.ValidateHash(hash) {
		h.fail(w, http.StatusBadRequest, "哈希格式非法")
		return
	}
	a, err := h.opts.Store.GetAsset(r.Context(), hash)
	if err != nil {
		h.assetError(w, err)
		return
	}
	abs := h.opts.Paths.BlobPath(a.RelPath)
	if _, err := os.Stat(abs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			h.fail(w, http.StatusNotFound, "原图文件缺失，无法打开所在目录")
			return
		}
		h.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := revealInFileBrowser(abs); err != nil {
		h.fail(w, http.StatusInternalServerError, "调用系统文件浏览器失败: "+err.Error())
		return
	}
	h.ok(w, map[string]any{"path": filepath.Dir(abs)})
}

// revealInFileBrowser 打开 abs 所在目录；尽量同时选中该文件。
func revealInFileBrowser(abs string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", abs).Start()
	case "windows":
		// explorer 的 /select,<path> 必须是同一个参数。
		return exec.Command("explorer", "/select,"+abs).Start()
	default:
		dir := filepath.Dir(abs)
		for _, opener := range []string{"xdg-open", "gio"} {
			if _, err := exec.LookPath(opener); err != nil {
				continue
			}
			if opener == "gio" {
				return exec.Command("gio", "open", dir).Start()
			}
			return exec.Command("xdg-open", dir).Start()
		}
		return fmt.Errorf("未找到 xdg-open 或 gio")
	}
}
