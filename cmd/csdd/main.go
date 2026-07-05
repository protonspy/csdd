// csdd — single-binary CLI + TUI for managing Claude Code workflow artifacts.
//
// Behavior summary:
//   - `csdd` with no arguments launches the interactive TUI.
//   - `csdd <resource> <action> ...` dispatches to the CLI surface.
//   - The CLI exposes 100% of the TUI functionality so AI agents can drive
//     the binary headlessly via flags.
package main

import (
	"os"

	"github.com/protonspy/csdd/internal/cli"
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/tui"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "tui" {
		// TUI gets the same embedded templates so its wizards can render artifacts.
		if err := tui.Run(templater.FS); err != nil {
			_, _ = os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		return
	}
	os.Exit(cli.Run(args, templater.FS))
}
