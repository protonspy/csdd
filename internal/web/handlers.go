package web

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/protonspy/csdd/internal/graph"
	"github.com/protonspy/csdd/internal/session"
)

// overviewResponse augments the workspace overview with the current change
// version so the client can correlate it with the SSE stream.
type overviewResponse struct {
	session.Overview
	Version int `json:"version"`
}

// errFileNotFound is the generic error surfaced for /api/file failures. It is
// deliberately opaque: the underlying os error embeds the absolute path, which
// must not leak into the response body.
var errFileNotFound = errors.New("file not found")

// errGraphNotFound is surfaced when docs/graph/graph.json.gz is absent (no build
// yet). Kept distinct from errFileNotFound so the Graph tab can show the "run
// csdd graph build" hint rather than a generic file error.
var errGraphNotFound = errors.New("graph not built")

// pinggyTokenFilename is the workspace-local pinggy Pro token store (mirrors the
// cli package's constant). It is never served, even with a valid auth token.
const pinggyTokenFilename = ".pinggy-token"

// responseWriteTimeout bounds how long a non-streaming handler may take to flush
// its response, so a slow reader cannot pin the goroutine forever (the server's
// global WriteTimeout is disabled for the indefinite SSE stream).
const responseWriteTimeout = 30 * time.Second

// newMux builds the read-only HTTP surface: a small JSON API over the workspace
// read-model plus the embedded SPA. Every route is GET; nothing mutates disk.
// The API is guarded by the auth token (except /api/health); the static SPA is
// always public so the page can load to authenticate. The whole surface is
// wrapped in a Host-header guard (DNS-rebinding defense): allowedHosts lists any
// non-loopback names the server is legitimately reachable under (the bound host
// and, when tunnelling, the public tunnel hostname).
func newMux(root string, h *hub, a *auth, allowedHosts ...string) http.Handler {
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
		setWriteDeadline(w)
		writeJSON(w, http.StatusOK, session.Tree(root))
	}))

	mux.HandleFunc("GET /api/file", a.protect(func(w http.ResponseWriter, r *http.Request) {
		setWriteDeadline(w)
		reqPath := r.URL.Query().Get("path")
		// Defence-in-depth: never serve the pinggy token file or anything under
		// .git, independent of the session read-model's own secret redaction.
		if isBlockedPath(reqPath) {
			writeError(w, http.StatusNotFound, errFileNotFound)
			return
		}
		fc, err := session.ReadFile(root, reqPath)
		if err != nil {
			// Generic message: the raw error embeds the absolute path.
			writeError(w, http.StatusNotFound, errFileNotFound)
			return
		}
		writeJSON(w, http.StatusOK, fc)
	}))

	// The knowledge graph is served straight from its gzip blob with
	// Content-Encoding: gzip — the browser decompresses transparently, so there is
	// zero server-side decompression and the payload skips the /api/file plaintext
	// path (its 2 MiB preview cap and binary rejection). The Graph tab reads this.
	mux.HandleFunc("GET /api/graph", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		setWriteDeadline(w)
		blob, err := os.ReadFile(graph.GraphGzPath(root))
		if err != nil {
			writeError(w, http.StatusNotFound, errGraphNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(blob)
	}))

	mux.HandleFunc("GET /api/tests", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, session.LoadTestReport(root))
	}))

	mux.HandleFunc("GET /api/plans", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		setWriteDeadline(w)
		writeJSON(w, http.StatusOK, loadPlansList(root))
	}))

	mux.HandleFunc("GET /api/wiki", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		setWriteDeadline(w)
		writeJSON(w, http.StatusOK, loadWikiOverview(root))
	}))

	// The knowledge-base surfaces that had no route. Each reuses the CLI's own
	// parser, so the dashboard cannot disagree with the gates about what the
	// workspace says.
	mux.HandleFunc("GET /api/adr", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		setWriteDeadline(w)
		writeJSON(w, http.StatusOK, loadADRs(root, loadCitations(root)))
	}))

	mux.HandleFunc("GET /api/stack", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		setWriteDeadline(w)
		writeJSON(w, http.StatusOK, loadStack(root, loadCitations(root)))
	}))

	mux.HandleFunc("GET /api/glossary", a.protect(func(w http.ResponseWriter, _ *http.Request) {
		setWriteDeadline(w)
		writeJSON(w, http.StatusOK, loadGlossary(root))
	}))

	// Citation resolution. Batched (`?token=a&token=b`) because the UI resolves a
	// whole Refs column at once, and answered in request order.
	mux.HandleFunc("GET /api/ref", a.protect(func(w http.ResponseWriter, r *http.Request) {
		setWriteDeadline(w)
		tokens := r.URL.Query()["token"]
		if len(tokens) > maxRefTokens {
			tokens = tokens[:maxRefTokens]
		}
		writeJSON(w, http.StatusOK, resolveRefs(root, tokens))
	}))

	mux.HandleFunc("GET /api/plan/{slug}", a.protect(func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		d, err := loadPlanDetail(root, slug)
		if err != nil {
			// Opaque: the raw error may embed a filesystem path.
			writeError(w, http.StatusNotFound, planNotFound(slug))
			return
		}
		writeJSON(w, http.StatusOK, d)
	}))

	mux.HandleFunc("GET /api/events", a.protect(h.sseHandler))

	// Everything else is the embedded SPA (with index.html fallback). Public so
	// the page can load; the client then authenticates to reach the API.
	mux.Handle("/", spaHandler())

	return requireHost(mux, allowedHosts...)
}

// alwaysAllowedHosts are the loopback names/addresses the dashboard is always
// reachable under locally. Any other Host is rejected unless explicitly allowed.
var alwaysAllowedHosts = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
}

// requireHost rejects (403) any request whose Host header is not a recognised
// loopback name/address or one of the explicitly allowed hosts. This is a
// DNS-rebinding defense: a malicious page that rebinds its own domain to
// 127.0.0.1 still sends that domain in the Host header, which is not on the
// allowlist. It wraps every route — API, SSE, and static assets — in both auth
// and --no-auth modes.
func requireHost(next http.Handler, allowed ...string) http.Handler {
	extra := map[string]bool{}
	for _, hn := range allowed {
		if hn = strings.ToLower(strings.TrimSpace(hn)); hn != "" {
			extra[hn] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if extra["*"] {
			next.ServeHTTP(w, r) // wildcard bind: caller opted into any-Host access
			return
		}
		h := hostname(r.Host)
		if !alwaysAllowedHosts[h] && !extra[h] {
			http.Error(w, "forbidden: unrecognised Host header", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isWildcardHost reports whether addr is an all-interfaces bind address, for
// which the Host-header guard is relaxed (see serve).
func isWildcardHost(addr string) bool {
	switch strings.TrimSpace(addr) {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	return false
}

// hostname returns the lowercased host portion of a Host header, stripped of any
// port and IPv6 brackets (so "[::1]:7777" and "[::1]" both become "::1").
func hostname(hostHeader string) string {
	h := hostHeader
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	return strings.ToLower(h)
}

// isBlockedPath reports whether a requested workspace-relative path must never be
// served, regardless of the session read-model's own redaction: the pinggy token
// file and anything under the .git directory (its config can hold credentials
// and its objects expose the full history). Both separators are normalised so a
// backslash variant cannot slip past.
func isBlockedPath(p string) bool {
	s := strings.ReplaceAll(p, "\\", "/")
	clean := path.Clean("/" + strings.TrimPrefix(s, "/"))
	clean = strings.TrimPrefix(clean, "/")
	if clean == pinggyTokenFilename {
		return true
	}
	seg := clean
	if i := strings.IndexByte(clean, '/'); i >= 0 {
		seg = clean[:i]
	}
	return seg == ".git"
}

// setWriteDeadline caps the time a non-streaming handler may spend writing its
// response. It is a no-op when the ResponseWriter does not support deadlines.
func setWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(responseWriteTimeout))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
