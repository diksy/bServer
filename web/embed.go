// Package web menyediakan aset SPA hasil build Vite yang di-embed ke binary.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
