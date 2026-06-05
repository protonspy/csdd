// Package web serves a read-only browser dashboard of a csdd workspace: spec
// progress, navigable spec docs, a live task board, a VS Code-style file viewer,
// and test/coverage metrics. It is a *view* only — the CLI remains the sole
// author of artifacts — and depends on internal/session for its read-model. The
// HTTP surface is pure net/http; live updates use Server-Sent Events fed by a
// filesystem poller; an optional token guards the API and an optional
// localtunnel exposes it publicly — all stdlib, no third-party dependency.
package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/protonspy/csdd/internal/render"
)

// Options configures the dashboard server.
type Options struct {
	Root        string // workspace root (already resolved by the caller)
	Host        string // bind address; defaults to 127.0.0.1
	Port        int    // TCP port; 0 picks a free port
	OpenBrowser bool   // best-effort open the default browser on start
	Auth        bool   // require a token on the API
	Password    string // explicit token (else a random one is generated when Auth)
	Tunnel      bool   // expose publicly via localtunnel (forces Auth)
}

// Serve starts the dashboard and blocks until interrupted (Ctrl-C). It returns
// a process exit code: 0 on clean shutdown, 1 on a bind/listen failure.
func Serve(opts Options) int {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Tunnel {
		opts.Auth = true // never expose the API publicly without a token
		if opts.Password != "" && len(opts.Password) < 8 {
			render.Err("refusing to tunnel with a weak --password (<8 chars). Use a stronger one, or omit it for a random token.")
			return 1
		}
	}
	addr := net.JoinHostPort(opts.Host, fmt.Sprintf("%d", opts.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		render.Err("csdd web: cannot bind " + addr + ": " + err.Error())
		return 1
	}

	a := newAuth(opts.Auth, opts.Password)
	localURL := "http://" + ln.Addr().String()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var publicURL string
	if opts.Tunnel {
		render.Info("requesting a public tunnel…")
		if u, err := startTunnel(ctx, tcpPort(ln)); err != nil {
			render.Warn("tunnel failed: " + err.Error())
		} else {
			publicURL = u
		}
	}

	printStartup(localURL, publicURL, a, opts.Root)
	if opts.OpenBrowser {
		openBrowser(entryURL(localURL, a))
	}

	if err := serve(ctx, ln, opts, a); err != nil && !errors.Is(err, http.ErrServerClosed) {
		render.Err("csdd web: " + err.Error())
		return 1
	}
	return 0
}

// serve wires the mux, starts the change poller, and runs the HTTP server until
// ctx is cancelled. Separated from Serve so tests can inject a listener + ctx.
func serve(ctx context.Context, ln net.Listener, opts Options, a *auth) error {
	h := newHub()

	pollCtx, cancelPoll := context.WithCancel(ctx)
	defer cancelPoll()
	go pollChanges(pollCtx, opts.Root, h)

	srv := &http.Server{
		Handler:      newMux(opts.Root, h, a),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // SSE streams indefinitely — no write deadline
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

// printStartup writes the access banner: the local URL, the auth token and a
// one-click magic link, and the public tunnel URL when enabled.
func printStartup(localURL, publicURL string, a *auth, root string) {
	render.OK("csdd web → " + localURL)
	if a.enabled {
		render.Info("auth token: " + render.Bold(a.token))
		render.Info("open authenticated: " + entryURL(localURL, a))
	}
	if publicURL != "" {
		render.OK("public tunnel → " + publicURL)
		if a.enabled {
			render.Info("share this link: " + entryURL(publicURL, a))
		}
		render.Warn("tunnel is PUBLIC: anyone with the link can read every file under " + root + " (read-only).")
		render.Warn("the link embeds the token and passes through the tunnel provider — share it only with people you trust.")
	}
	render.Info("read-only dashboard · watching " + root + " · press Ctrl-C to stop")
}

// entryURL returns the URL to open: the /token/<token> magic link when auth is
// on (it stores the token and redirects), otherwise the bare URL.
func entryURL(base string, a *auth) string {
	if a.enabled {
		return base + "/token/" + a.token
	}
	return base
}

func tcpPort(ln net.Listener) int {
	if t, ok := ln.Addr().(*net.TCPAddr); ok {
		return t.Port
	}
	return 0
}
