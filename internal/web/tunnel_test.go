package web

import (
	"net"
	"testing"
	"time"
)

// TestTunnelEndpoint covers the localtunnel request URL: a random subdomain when
// none is requested, a fixed subdomain when one is, and rejection of names that
// are not valid DNS labels (which loca.lt would reject anyway).
func TestTunnelEndpoint(t *testing.T) {
	cases := []struct {
		name      string
		subdomain string
		want      string
		wantErr   bool
	}{
		{"random when empty", "", "https://localtunnel.me/?new", false},
		{"fixed subdomain", "csdd-demo", "https://localtunnel.me/csdd-demo", false},
		{"single char", "a", "https://localtunnel.me/a", false},
		{"rejects uppercase", "Csdd", "", true},
		{"rejects underscore", "bad_name", "", true},
		{"rejects leading hyphen", "-bad", "", true},
		{"rejects trailing hyphen", "bad-", "", true},
		{"rejects dot", "a.b", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := tunnelEndpoint(c.subdomain)
			if c.wantErr {
				if err == nil {
					t.Fatalf("tunnelEndpoint(%q) = %q, want error", c.subdomain, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("tunnelEndpoint(%q) unexpected error: %v", c.subdomain, err)
			}
			if got != c.want {
				t.Errorf("tunnelEndpoint(%q) = %q, want %q", c.subdomain, got, c.want)
			}
		})
	}
}

// TestPipeRelays checks the tunnel's bidirectional proxy without any network.
func TestPipeRelays(t *testing.T) {
	ac, as := net.Pipe()
	bc, bs := net.Pipe()
	go pipe(as, bs)

	// a → b
	go ac.Write([]byte("ping"))
	buf := make([]byte, 4)
	_ = bc.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := bc.Read(buf); err != nil {
		t.Fatalf("read a→b: %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("a→b = %q, want ping", buf)
	}

	// b → a
	go bc.Write([]byte("pong"))
	buf2 := make([]byte, 4)
	_ = ac.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := ac.Read(buf2); err != nil {
		t.Fatalf("read b→a: %v", err)
	}
	if string(buf2) != "pong" {
		t.Errorf("b→a = %q, want pong", buf2)
	}

	ac.Close()
	bc.Close()
}
