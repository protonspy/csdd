package web

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHubLifecycle(t *testing.T) {
	h := newHub()
	if h.clientCount() != 0 {
		t.Fatalf("fresh hub clients = %d", h.clientCount())
	}
	ch := h.subscribe()
	if h.clientCount() != 1 {
		t.Fatalf("after subscribe clients = %d", h.clientCount())
	}

	if v := h.broadcast(); v != 1 {
		t.Fatalf("broadcast version = %d, want 1", v)
	}
	select {
	case got := <-ch:
		if got != 1 {
			t.Fatalf("received version = %d, want 1", got)
		}
	default:
		t.Fatal("subscriber did not receive the broadcast")
	}

	h.unsubscribe(ch)
	if h.clientCount() != 0 {
		t.Fatalf("after unsubscribe clients = %d, want 0", h.clientCount())
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after unsubscribe")
	}
}

func TestSSEHandler(t *testing.T) {
	h := newHub()
	srv := httptest.NewServer(http.HandlerFunc(h.sseHandler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("sse content-type = %q", ct)
	}

	r := bufio.NewReader(resp.Body)
	if v := readVersion(t, r); v != 0 {
		t.Fatalf("initial frame version = %d, want 0", v)
	}

	// Wait for the subscriber to be registered, then broadcast.
	waitForClient(t, h)
	h.broadcast()
	if v := readVersion(t, r); v != 1 {
		t.Fatalf("post-broadcast version = %d, want 1", v)
	}
}

func waitForClient(t *testing.T, h *hub) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if h.clientCount() >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no SSE client registered in time")
}

func readVersion(t *testing.T, r *bufio.Reader) int {
	t.Helper()
	type result struct {
		v   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ch <- result{0, err}
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				var d struct {
					Version int `json:"version"`
				}
				_ = json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &d)
				ch <- result{d.Version, nil}
				return
			}
		}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("reading sse: %v", res.err)
		}
		return res.v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for an SSE frame")
		return -1
	}
}
