package web

import "embed"

// Dist 是构建好的 React 前端（web/dist）。构建二进制时一起打包，
// 运行时不再需要 Node.js，也不依赖外部静态目录。
//
//go:embed dist
var Dist embed.FS

//go:embed logo.svg
var logoSVG []byte

// Logo 返回应用图标（/api/logo）。
func Logo() []byte { return logoSVG }
