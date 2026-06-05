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
func newMux(root string, h *hub) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": h.currentVersion()})
	})

	mux.HandleFunc("GET /api/overview", func(w http.ResponseWriter, _ *http.Request) {
		ov := session.LoadOverview(root)
		writeJSON(w, http.StatusOK, overviewResponse{Overview: ov, Version: h.currentVersion()})
	})

	mux.HandleFunc("GET /api/spec/{feature}", func(w http.ResponseWriter, r *http.Request) {
		d, err := session.LoadSpecDetail(root, r.PathValue("feature"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, d)
	})

	mux.HandleFunc("GET /api/tree", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, session.Tree(root))
	})

	mux.HandleFunc("GET /api/file", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		fc, err := session.ReadFile(root, p)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, fc)
	})

	mux.HandleFunc("GET /api/tests", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, session.LoadTestReport(root))
	})

	mux.HandleFunc("GET /api/events", h.sseHandler)

	// Everything else is the embedded SPA (with index.html fallback).
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
