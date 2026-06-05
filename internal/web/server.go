// Package web serves a read-only browser dashboard of a csdd workspace: spec
// progress, navigable spec docs, a live task board, and a VS Code-style file
// viewer. It is a *view* only — the CLI remains the sole author of artifacts —
// and depends on internal/session for its read-model. The HTTP surface is pure
// net/http; live updates use Server-Sent Events fed by a filesystem poller, so
// no third-party dependency is introduced.
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
}

// Serve starts the dashboard and blocks until interrupted (Ctrl-C). It returns
// a process exit code: 0 on clean shutdown, 1 on a bind/listen failure.
func Serve(opts Options) int {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	addr := net.JoinHostPort(opts.Host, fmt.Sprintf("%d", opts.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		render.Err("csdd web: cannot bind " + addr + ": " + err.Error())
		return 1
	}

	url := "http://" + ln.Addr().String()
	render.OK("csdd web → " + url)
	render.Info("read-only dashboard · watching " + opts.Root + " · press Ctrl-C to stop")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if opts.OpenBrowser {
		openBrowser(url)
	}

	if err := serve(ctx, ln, opts); err != nil && !errors.Is(err, http.ErrServerClosed) {
		render.Err("csdd web: " + err.Error())
		return 1
	}
	return 0
}

// serve wires the mux, starts the change poller, and runs the HTTP server until
// ctx is cancelled. It is separated from Serve so tests can inject their own
// listener and context and shut down deterministically.
func serve(ctx context.Context, ln net.Listener, opts Options) error {
	h := newHub()

	pollCtx, cancelPoll := context.WithCancel(ctx)
	defer cancelPoll()
	go pollChanges(pollCtx, opts.Root, h)

	srv := &http.Server{
		Handler:      newMux(opts.Root, h),
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
