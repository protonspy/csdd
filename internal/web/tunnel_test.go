package web

import (
	"net"
	"testing"
	"time"
)

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
