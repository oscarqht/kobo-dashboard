// Package webui contains the offline, device-served dashboard editor.
package webui

import (
	"embed"
	"net/http"
)

// Files is served directly by the device; there are no remote fonts or scripts.
//
//go:embed index.html editor.js style.css
var Files embed.FS

func Handler() http.Handler { return http.FileServer(http.FS(Files)) }
