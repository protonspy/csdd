package web

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
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

// A wildcard bind must not advertise the address Go reads back off its
// dual-stack socket ("[::]:port"): that URL is unopenable, and with auth on it
// is also the base of the magic link the user is told to click.
func TestAdvertisedURLRewritesWildcardBind(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	got := advertisedURL(ln, "0.0.0.0")
	if strings.Contains(got, "[::]") || strings.Contains(got, "0.0.0.0") {
		t.Fatalf("advertised an unreachable wildcard address: %s", got)
	}
	host, port, err := net.SplitHostPort(strings.TrimPrefix(got, "http://"))
	if err != nil {
		t.Fatalf("advertised URL is not host:port: %s (%v)", got, err)
	}
	if host == "" {
		t.Fatalf("advertised URL has no host: %s", got)
	}
	if want := strconv.Itoa(tcpPort(ln)); port != want {
		t.Errorf("advertised port = %s, want %s (the bound port)", port, want)
	}
}

// A concrete bind is already reachable as-is and must be reported verbatim.
func TestAdvertisedURLKeepsExplicitHost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if got, want := advertisedURL(ln, "127.0.0.1"), "http://"+ln.Addr().String(); got != want {
		t.Errorf("advertisedURL = %s, want %s", got, want)
	}
}
