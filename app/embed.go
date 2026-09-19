// Package app 只做一件事：把前端静态资源嵌进二进制。
// go:embed 只能嵌入自己目录及子目录，所以资源和这个文件放在一起。
package app

import (
	"embed"
	"io/fs"
)

//go:embed index.html assets
var files embed.FS

// FS 返回以 app/ 为根的文件系统，供 HTTP 层直接挂载。
func FS() fs.FS {
	sub, err := fs.Sub(files, ".")
	if err != nil {
		panic(err)
	}
	return sub
}
