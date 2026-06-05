package cli

import (
	"flag"

	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/web"
	"github.com/protonspy/csdd/internal/workspace"
)

// runWeb launches the read-only browser dashboard. It is a *view* over the
// workspace — the CLI remains the only sanctioned author of artifacts — so it
// takes no mutating flags. It blocks until interrupted (Ctrl-C).
func runWeb(args []string) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	var root, host string
	var port int
	var noOpen bool
	addRoot(fs, &root)
	fs.StringVar(&host, "host", "127.0.0.1", "Bind address (default: 127.0.0.1, localhost only).")
	fs.IntVar(&port, "port", 7777, "Port to listen on (0 picks a free port).")
	fs.BoolVar(&noOpen, "no-open", false, "Do not open a browser on start.")
	if _, err := parseFlags(fs, args); err != nil {
		return failOnFlagParse(err)
	}
	r, err := workspace.Resolve(root)
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	return web.Serve(web.Options{Root: r, Host: host, Port: port, OpenBrowser: !noOpen})
}
