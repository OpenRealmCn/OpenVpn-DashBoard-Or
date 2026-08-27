//go:build !dev

// Package web 内嵌 React 构建产物;开发时用 -tags dev 构建可跳过内嵌,
// 由 Vite dev server 代理 API。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS 返回内嵌前端文件系统;bool 表示是否为内嵌构建。
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	return sub, true
}
