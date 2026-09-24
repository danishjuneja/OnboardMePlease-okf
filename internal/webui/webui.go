package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// The web build emits into dist before a Go build; the release binary embeds it.
//
//go:embed dist
var assets embed.FS

func Handler() http.HandlerFunc {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic("embedded web UI is missing")
	}
	files := http.FileServer(http.FS(root))
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(root, path); err != nil {
				r.URL.Path = "/"
			}
		}
		files.ServeHTTP(w, r)
	}
}
