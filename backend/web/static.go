package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist/*
var assets embed.FS

func Handler() http.Handler {
	root, _ := fs.Sub(assets, "dist")
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleaned := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if cleaned == "." || cleaned == "" {
			cleaned = "index.html"
		}
		if _, err := fs.Stat(root, cleaned); err != nil {
			r.URL.Path = "/index.html"
		}
		files.ServeHTTP(w, r)
	})
}
