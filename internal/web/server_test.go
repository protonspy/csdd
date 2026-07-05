package web

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeStartAndShutdown(t *testing.T) {
	root := tempWorkspace(t, map[string]string{
		"specs/f/spec.json": `{"phase":"x"}`,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, Options{Root: root}, newAuth(false, ""), "") }()

	waitHealthy(t, "http://"+ln.Addr().String())

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down after context cancel")
	}
}

// TestServeShutdownWithSSEClient reproduces the Ctrl-C hang: with a live SSE
// connection, hub.shutdown() must close the subscriber channel so the streaming
// handler returns and http.Server.Shutdown completes cleanly (nil), rather than
// blocking on the long-lived stream until its timeout and returning
// context.DeadlineExceeded.
func TestServeShutdownWithSSEClient(t *testing.T) {
	root := tempWorkspace(t, map[string]string{"specs/f/spec.json": `{"phase":"x"}`})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, Options{Root: root}, newAuth(false, ""), "") }()

	base := "http://" + ln.Addr().String()
	waitHealthy(t, base)

	// Open an SSE stream and read the initial frame so we know the handler has
	// subscribed to the hub (an active, long-lived request during shutdown).
	sseCtx, sseCancel := context.WithCancel(context.Background())
	defer sseCancel()
	req, _ := http.NewRequestWithContext(sseCtx, "GET", base+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := bufio.NewReader(resp.Body).ReadString('\n'); err != nil {
		t.Fatalf("reading initial SSE frame: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown with SSE client returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down after context cancel")
	}
}

func waitHealthy(t *testing.T, base string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		resp, err := http.Get(base + "/api/health")
		if err == nil {
			ok := resp.StatusCode == http.StatusOK
			resp.Body.Close()
			if ok {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server never became healthy")
}
