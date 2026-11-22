// Package web embeds the local browser workbench into the executable.
package web

import "embed"

//go:embed index.html app.js graph.js style.css
var Assets embed.FS
