// Package webstatic 内嵌 Economy World 前端的构建产物。
//
// 前端由 worlds/economy/web 下的 `npm run build` 产出到 ../webstatic/dist
// （见 worlds/economy/web/vite.config.ts 的 build.outDir）。
// 由于 Go embed 不能引用父目录，故单独建此包，dist 位于本包目录内。
package webstatic

import "embed"

// DistFS 内嵌 webstatic/dist 下的全部前端静态文件。
//
//go:embed all:dist
var DistFS embed.FS
