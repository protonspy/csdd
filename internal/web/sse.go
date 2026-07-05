package web

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/protonspy/csdd/internal/session"
)

// pollInterval is how often the workspace is re-snapshotted for live reload.
// 800ms sits comfortably above typical editor save cadence while keeping the
// dashboard feeling instant, and is coarse enough not to thrash the disk.
const pollInterval = 800 * time.Millisecond

// hub is the SSE broadcaster. It holds a monotonically increasing version that
// the change poller bumps on every detected workspace edit, and fans that
// version out to all connected clients. All state is guarded by mu.
type hub struct {
	mu      sync.Mutex
	clients map[chan int]struct{}
	version int
	closed  bool // set by shutdown; blocks further subscriptions/broadcasts
}

func newHub() *hub {
	return &hub{clients: map[chan int]struct{}{}}
}

func (h *hub) currentVersion() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.version
}

func (h *hub) subscribe() chan int {
	ch := make(chan int, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	// After shutdown, hand back an already-closed channel so the SSE handler's
	// receive returns immediately (it treats a closed channel as "disconnect").
	if h.closed {
		close(ch)
		return ch
	}
	h.clients[ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(ch chan int) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// shutdown closes every subscriber channel and marks the hub closed, so all SSE
// handler goroutines return promptly (they exit when their channel closes). It
// is called before http.Server.Shutdown; otherwise the long-lived event streams
// would keep their connections active until Shutdown's timeout elapses and it
// returned context.DeadlineExceeded. Idempotent; subscribe/broadcast are no-ops
// afterwards, so no send can race a closed channel.
func (h *hub) shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.clients {
		delete(h.clients, ch)
		close(ch)
	}
}

// broadcast bumps the version and notifies every client without blocking. A
// client whose 1-slot buffer is full (slow consumer) simply misses this tick;
// because each notification carries the latest version, it catches up on the
// next one. Returns the new version.
func (h *hub) broadcast() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return h.version
	}
	h.version++
	for ch := range h.clients {
		select {
		case ch <- h.version:
		default:
		}
	}
	return h.version
}

func (h *hub) clientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// sseHandler streams change events to one client over Server-Sent Events. It
// cleans up on client disconnect (r.Context().Done()), so there is no leak.
func (h *hub) sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.subscribe()
	defer h.unsubscribe(ch)

	// Immediately sync the freshly-connected client to the current version.
	writeEvent(w, flusher, h.currentVersion())

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case v, ok := <-ch:
			if !ok {
				return
			}
			writeEvent(w, flusher, v)
		case <-keepalive.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func writeEvent(w http.ResponseWriter, flusher http.Flusher, version int) {
	_, _ = fmt.Fprintf(w, "event: change\ndata: {\"version\":%d}\n\n", version)
	flusher.Flush()
}

// pollChanges snapshots the workspace every pollInterval and bumps the hub
// version whenever something changed, driving the live-reload stream. It stops
// when ctx is cancelled (server shutdown).
func pollChanges(ctx context.Context, root string, h *hub) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	last := session.TakeSnapshot(root)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next := session.TakeSnapshot(root)
			if session.Changed(last, next) {
				last = next
				h.broadcast()
			}
		}
	}
}
