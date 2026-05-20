package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// webFS holds the built Vite SPA. `make web` writes the bundle here
// (vite build --outDir ../internal/server/web_dist).
//
//go:embed all:web_dist
var webFS embed.FS

func spaHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web_dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback: serve index.html for client-side routes that aren't real files.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(sub, path); err != nil {
				r.URL.Path = "/"
			}
		}
		files.ServeHTTP(w, r)
	})
}
