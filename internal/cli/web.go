package cli

import (
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/web"
	"github.com/protonspy/csdd/internal/workspace"
)

// runWeb launches the read-only browser dashboard. It is a *view* over the
// workspace — the CLI remains the only sanctioned author of artifacts — so it
// takes no mutating flags. It blocks until interrupted (Ctrl-C).
func runWeb(args []string) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	var root, host, password, subdomain, provider, pinggyToken string
	var port int
	var noOpen, noAuth, tunnel bool
	addRoot(fs, &root)
	fs.StringVar(&host, "host", "127.0.0.1", "Bind address (default: 127.0.0.1, localhost only).")
	fs.IntVar(&port, "port", 7777, "Port to listen on (0 picks a free port).")
	fs.BoolVar(&noOpen, "no-open", false, "Do not open a browser on start.")
	fs.StringVar(&password, "password", os.Getenv("CSDD_PASSWORD"), "API token (default: random; or CSDD_PASSWORD env).")
	fs.BoolVar(&noAuth, "no-auth", false, "Disable API authentication (localhost only).")
	fs.BoolVar(&tunnel, "tunnel", false, "Expose the dashboard publicly via a tunnel provider (forces auth).")
	fs.StringVar(&provider, "provider", "localtunnel", "Tunnel provider: localtunnel | pinggy.")
	fs.StringVar(&subdomain, "subdomain", "", "localtunnel: request a fixed subdomain for a stable public URL (implies --tunnel).")
	fs.StringVar(&pinggyToken, "pinggy-token", "", "pinggy: Pro access token (custom domains). Saved to .pinggy-token for reuse; also read from CSDD_PINGGY_TOKEN.")
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	explicitToken := pinggyToken
	if subdomain != "" || explicitToken != "" || provider != "localtunnel" {
		tunnel = true // any tunnel-specific flag implies --tunnel
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	// pinggy token: an explicit --pinggy-token is persisted (and gitignored) so it
	// need not be passed again; otherwise fall back to env, then the saved file.
	if provider == "pinggy" {
		pinggyToken = resolvePinggyToken(r, explicitToken)
	}
	return web.Serve(web.Options{
		Root:        r,
		Host:        host,
		Port:        port,
		OpenBrowser: !noOpen,
		Auth:        !noAuth,
		Password:    password,
		Tunnel:      tunnel,
		Provider:    provider,
		Subdomain:   subdomain,
		PinggyToken: pinggyToken,
	})
}

// resolvePinggyToken determines the pinggy Pro token in precedence order:
// an explicit --pinggy-token (which it also persists to <root>/.pinggy-token,
// 0600, and gitignores so it never lands in the repo), then CSDD_PINGGY_TOKEN,
// then the saved file. Returns "" for the free (anonymous) tier.
func resolvePinggyToken(root, explicit string) string {
	path := filepath.Join(root, pinggyTokenFile)
	if explicit != "" {
		if err := os.WriteFile(path, []byte(explicit+"\n"), 0o600); err != nil {
			render.Warn("could not save " + pinggyTokenFile + ": " + err.Error())
		} else {
			if _, err := ensureGitignore(root, []string{pinggyTokenFile}); err != nil {
				render.Warn("could not gitignore " + pinggyTokenFile + ": " + err.Error())
			}
			render.Info("saved pinggy token to " + pinggyTokenFile + " (gitignored) for reuse")
		}
		return explicit
	}
	if env := strings.TrimSpace(os.Getenv("CSDD_PINGGY_TOKEN")); env != "" {
		return env
	}
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}
