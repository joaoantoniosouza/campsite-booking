package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// StaticHandler serves the embedded static/ directory under the /static/
// prefix. Missing assets yield the underlying file server's 404.
func StaticHandler(fsys embed.FS) http.Handler {
	sub, _ := fs.Sub(fsys, "static") // static is guaranteed embedded via go:embed
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
