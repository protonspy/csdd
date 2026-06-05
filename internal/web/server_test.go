package web

import (
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
	go func() { done <- serve(ctx, ln, Options{Root: root}) }()

	url := "http://" + ln.Addr().String() + "/api/health"
	var ok bool
	for i := 0; i < 100; i++ {
		resp, err := http.Get(url)
		if err == nil {
			ok = resp.StatusCode == http.StatusOK
			resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ok {
		t.Fatal("server never became healthy")
	}

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
