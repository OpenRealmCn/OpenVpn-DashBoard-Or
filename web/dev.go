//go:build dev

package web

import "io/fs"

// FS 在 dev 构建下不内嵌前端,面板仅提供 API。
func FS() (fs.FS, bool) { return nil, false }
