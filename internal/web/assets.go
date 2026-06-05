package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// distFS holds the built frontend (React + Vite + Monaco), produced into dist/
// by `make web-build`. The built bundle is NOT committed; only dist/.gitkeep is,
// which keeps this embed compiling for source builds (`go build`/`go install`).
// CI and the release pipeline run `make web-build` first, so distributed
// binaries embed the real UI. A source build with no frontend serves
// placeholderHTML instead.
//
//go:embed all:dist
var distFS embed.FS

// distSub returns the embedded frontend rooted at dist/.
func distSub() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// spaHandler serves the embedded single-page app. Requests that map to a real
// embedded file are served directly; everything else falls back to index.html
// (or the placeholder, when the frontend has not been built) so the client-side
// router can take over — standard SPA behaviour.
func spaHandler() http.Handler {
	sub, err := distSub()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			serveIndex(w, nil)
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if f, err := sub.Open(name); err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		serveIndex(w, sub)
	})
}

// serveIndex writes the SPA shell. It serves the built index.html when present,
// otherwise the placeholder (frontend not built into dist/).
func serveIndex(w http.ResponseWriter, sub fs.FS) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if sub != nil {
		if data, err := fs.ReadFile(sub, "index.html"); err == nil {
			_, _ = w.Write(data)
			return
		}
	}
	_, _ = w.Write([]byte(placeholderHTML))
}

// placeholderHTML is shown when the dashboard frontend has not been built into
// dist/ — e.g. a `go install` from source. Run `make web-build` to embed the
// real UI. The read-only JSON API is fully functional regardless.
const placeholderHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="color-scheme" content="dark" />
    <title>csdd web</title>
    <style>
      body { font-family: system-ui, sans-serif; background: #0e1117; color: #d7dee8;
             display: grid; place-items: center; min-height: 100vh; margin: 0; }
      main { max-width: 36rem; padding: 2rem; }
      code { background: #1b212c; padding: 1px 6px; border-radius: 4px; }
      a { color: #5aa6e6; }
    </style>
  </head>
  <body>
    <main>
      <h1>csdd web</h1>
      <p>The dashboard frontend has not been built. Run <code>make web-build</code>
         and restart <code>csdd web</code>.</p>
      <p>The read-only API is live: <code>/api/overview</code>,
         <code>/api/tree</code>, <code>/api/events</code>.</p>
    </main>
  </body>
</html>
`
