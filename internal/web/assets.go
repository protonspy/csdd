package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// distFS holds the built frontend (React + Vite + Monaco), produced into dist/
// by `make web-build`. A placeholder dist/index.html is committed so this embed
// always compiles, even before the frontend is built.
//
//go:embed all:dist
var distFS embed.FS

// distSub returns the embedded frontend rooted at dist/.
func distSub() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// spaHandler serves the embedded single-page app. Requests that map to a real
// embedded file are served directly; everything else falls back to index.html
// so the client-side router can take over (standard SPA behaviour).
func spaHandler() http.Handler {
	sub, err := distSub()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "frontend assets unavailable", http.StatusInternalServerError)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			serveIndex(w, sub)
			return
		}
		if f, err := sub.Open(name); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, sub)
	})
}

func serveIndex(w http.ResponseWriter, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "index.html missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}
