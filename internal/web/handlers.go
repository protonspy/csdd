package web

import (
	"encoding/json"
	"net/http"

	"github.com/protonspy/csdd/internal/session"
)

// overviewResponse augments the workspace overview with the current change
// version so the client can correlate it with the SSE stream.
type overviewResponse struct {
	session.Overview
	Version int `json:"version"`
}

// newMux builds the read-only HTTP surface: a small JSON API over the workspace
// read-model plus the embedded SPA. Every route is GET; nothing mutates disk.
// The API is guarded by the auth token (except /api/health); the static SPA is
// always public so the page can load to authenticate.
func newMux(root string, h *hub, a *auth) http.Handler {
	mux := http.NewServeMux()

	// Public: liveness + whether auth is required (so the client knows to log in).
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": h.currentVersion(), "auth": a.enabled})
	})

	mux.HandleFunc("GET /api/overview", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		ov := session.LoadOverview(root)
		writeJSON(w, http.StatusOK, overviewResponse{Overview: ov, Version: h.currentVersion()})
	}))

	mux.HandleFunc("GET /api/spec/{feature}", a.protect(func(w http.ResponseWriter, r *http.Request) {
		d, err := session.LoadSpecDetail(root, r.PathValue("feature"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, d)
	}))

	mux.HandleFunc("GET /api/tree", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, session.Tree(root))
	}))

	mux.HandleFunc("GET /api/file", a.protect(func(w http.ResponseWriter, r *http.Request) {
		fc, err := session.ReadFile(root, r.URL.Query().Get("path"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, fc)
	}))

	mux.HandleFunc("GET /api/tests", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, session.LoadTestReport(root))
	}))

	mux.HandleFunc("GET /api/events", a.protect(h.sseHandler))

	// Everything else is the embedded SPA (with index.html fallback). Public so
	// the page can load; the client then authenticates to reach the API.
	mux.Handle("/", spaHandler())

	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
