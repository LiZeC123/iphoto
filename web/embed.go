// Package web 把前端构建产物嵌入二进制（dev.md 12.4）。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist 返回以 dist 为根的文件系统。仓库内只提交 dist/.gitkeep 以保证 embed
// 可编译；真正的界面需执行 make web-build 生成。
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist 由 go:embed 保证存在，失败属于编译期错误。
		panic(err)
	}
	return sub
}
