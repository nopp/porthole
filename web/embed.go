// Package web holds the embedded visualizer UI.
package web

import "embed"

// Files is the static UI served at / and /static/.
//
//go:embed index.html app.js style.css logo.jpg
var Files embed.FS
