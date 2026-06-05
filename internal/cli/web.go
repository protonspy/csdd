package cli

import (
	"flag"
	"os"

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
	fs.StringVar(&pinggyToken, "pinggy-token", os.Getenv("CSDD_PINGGY_TOKEN"), "pinggy: access token for the Pro tier / custom domains (or CSDD_PINGGY_TOKEN env).")
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	if subdomain != "" || pinggyToken != "" || provider != "localtunnel" {
		tunnel = true // any tunnel-specific flag implies --tunnel
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
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
