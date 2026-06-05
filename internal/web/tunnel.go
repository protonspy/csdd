package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// tunnelHost is the localtunnel service. The dashboard reuses its public
// protocol (request a subdomain, then proxy raw TCP connections) directly in
// Go — no Node, no third-party dependency.
const tunnelHost = "localtunnel.me"

// subdomainRE is a single DNS label: lowercase alphanumeric, internal hyphens
// allowed, 1–63 chars. loca.lt enforces the same shape, so rejecting early
// gives a clearer error than a remote 4xx.
var subdomainRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// tunnelEndpoint builds the localtunnel request URL. An empty subdomain asks for
// a random one (/?new); a non-empty one requests that fixed subdomain so the
// public URL — and thus loca.lt's per-subdomain interstitial bypass — is stable
// across restarts. Invalid names are rejected before any network call.
func tunnelEndpoint(subdomain string) (string, error) {
	if subdomain == "" {
		return "https://" + tunnelHost + "/?new", nil
	}
	if !subdomainRE.MatchString(subdomain) {
		return "", fmt.Errorf("invalid --subdomain %q: use 1–63 lowercase letters, digits, or hyphens (no leading/trailing hyphen)", subdomain)
	}
	return "https://" + tunnelHost + "/" + subdomain, nil
}

type tunnelInfo struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Port    int    `json:"port"`
	MaxConn int    `json:"max_conn_count"`
}

// startTunnel requests a public URL from localtunnel and starts proxying its
// connections to the local server. A non-empty subdomain requests a fixed public
// URL (stable across restarts); empty asks for a random one. It returns the
// public URL; the proxy goroutines run until ctx is cancelled.
func startTunnel(ctx context.Context, localPort int, subdomain string) (string, error) {
	info, err := requestTunnel(ctx, subdomain)
	if err != nil {
		return "", err
	}
	// Size the connection pool. loca.lt's free tier advertises max_conn_count=2,
	// which is far too few for a browser (it opens ~6 parallel connections and the
	// dashboard lazy-loads many chunks) — with only 2 pooled upstreams the rest of
	// the requests get a 502 from loca.lt. So floor the pool well above the advert,
	// while still capping a hostile/huge value to bound local goroutines/sockets.
	const poolFloor, poolCap = 10, 16
	pool := info.MaxConn
	if pool < poolFloor {
		pool = poolFloor
	}
	if pool > poolCap {
		pool = poolCap
	}
	for i := 0; i < pool; i++ {
		go tunnelWorker(ctx, info.Port, localPort)
	}
	return info.URL, nil
}

func requestTunnel(ctx context.Context, subdomain string) (tunnelInfo, error) {
	var info tunnelInfo
	endpoint, err := tunnelEndpoint(subdomain)
	if err != nil {
		return info, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return info, err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return info, fmt.Errorf("contact localtunnel: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("localtunnel returned %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, fmt.Errorf("decode localtunnel response: %w", err)
	}
	if info.URL == "" || info.Port == 0 {
		return info, fmt.Errorf("localtunnel response missing url/port")
	}
	return info, nil
}

// tunnelWorker maintains one pooled connection to the tunnel server, piping it
// to the local server and reconnecting when it drops, until ctx is cancelled.
//
// The local connection is opened lazily — only once loca.lt actually routes a
// request to this pooled connection (signalled by the first byte arriving). This
// avoids holding an idle local socket that the HTTP server could close out from
// under a pooled connection, which loca.lt would surface to visitors as a 502.
func tunnelWorker(ctx context.Context, remotePort, localPort int) {
	d := net.Dialer{Timeout: 10 * time.Second}
	for ctx.Err() == nil {
		remote, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", tunnelHost, remotePort))
		if err != nil {
			tunnelSleep(ctx, time.Second)
			continue
		}
		// Block until a request arrives on this pooled connection (or it closes).
		first := make([]byte, 1)
		n, rerr := remote.Read(first)
		if rerr != nil {
			remote.Close()
			continue // idle pooled connection dropped; reconnect promptly
		}
		local, lerr := d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
		if lerr != nil {
			remote.Close()
			tunnelSleep(ctx, time.Second)
			continue
		}
		if _, werr := local.Write(first[:n]); werr != nil {
			remote.Close()
			local.Close()
			continue
		}
		pipe(remote, local)
	}
}

// pipe copies bidirectionally until either side closes, then closes both.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
	a.Close()
	b.Close()
}

// tunnelPassword fetches the value loca.lt's "Tunnel website ahead" interstitial
// asks first-time browser visitors to type: this machine's public IP. Surfacing
// it in the startup banner turns that one-time click into a copy-paste. Best
// effort — an empty string just omits the hint.
func tunnelPassword(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://loca.lt/mytunnelpassword", nil)
	if err != nil {
		return ""
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func tunnelSleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
