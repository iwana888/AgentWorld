// Package webstatic 内嵌前端构建产物。
// 前端由 web/ 下的 `npm run build` 产出到 ../webstatic/dist（见 web/vite.config.js）。
// 由于 Go embed 不能引用父目录，故单独建此包，dist 位于本包目录内。
package webstatic

import "embed"

// DistFS 内嵌 webstatic/dist 下的全部前端静态文件。
//
//go:embed all:dist
var DistFS embed.FS
