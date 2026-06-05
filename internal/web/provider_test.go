package web

import (
	"context"
	"strings"
	"testing"
)

// TestResolveTunnelOptionsForcesAuth is the security guarantee: any tunnel, for
// any provider, forces the API token on — so a public URL can never reach
// project data unauthenticated. Even an explicit "auth off" is overridden.
func TestResolveTunnelOptionsForcesAuth(t *testing.T) {
	for _, provider := range []string{"", "localtunnel", "pinggy"} {
		opts := Options{Tunnel: true, Auth: false, Provider: provider} // Auth:false ~ --no-auth
		got, err := resolveTunnelOptions(opts)
		if err != nil {
			t.Fatalf("provider %q: unexpected error: %v", provider, err)
		}
		if !got.Auth {
			t.Errorf("provider %q: tunnel must force Auth on, got Auth=false", provider)
		}
		if got.Provider == "" {
			t.Errorf("provider %q: provider should be defaulted, got empty", provider)
		}
	}
}

func TestResolveTunnelOptionsRejections(t *testing.T) {
	if _, err := resolveTunnelOptions(Options{Tunnel: true, Provider: "ngrok"}); err == nil {
		t.Error("unknown provider should be rejected")
	}
	if _, err := resolveTunnelOptions(Options{Tunnel: true, Provider: "pinggy", Password: "short"}); err == nil {
		t.Error("weak password should be rejected when tunnelling")
	}
	// Without a tunnel, nothing is forced and a weak password is the user's call.
	got, err := resolveTunnelOptions(Options{Tunnel: false, Auth: false, Password: "x"})
	if err != nil {
		t.Fatalf("no-tunnel should not error: %v", err)
	}
	if got.Auth {
		t.Error("no tunnel must not force Auth on")
	}
}

func TestStartTunnelUnknownProvider(t *testing.T) {
	_, err := startTunnel(context.Background(), 12345, Options{Provider: "bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown tunnel provider") {
		t.Errorf("expected unknown-provider error, got %v", err)
	}
}

func TestIsKnownProvider(t *testing.T) {
	for _, p := range []string{"localtunnel", "pinggy"} {
		if !isKnownProvider(p) {
			t.Errorf("%q should be known", p)
		}
	}
	for _, p := range []string{"", "ngrok", "cloudflare"} {
		if isKnownProvider(p) {
			t.Errorf("%q should not be known", p)
		}
	}
}
