// csdd — single-binary CLI + TUI for managing Kiro workflow artifacts.
//
// Behavior summary:
//   - `csdd` with no arguments launches the interactive TUI.
//   - `csdd <resource> <action> ...` dispatches to the CLI surface.
//   - The CLI exposes 100% of the TUI functionality so AI agents can drive
//     the binary headlessly via flags.
package main

import (
	"os"

	"github.com/livelo/csdd/cmd"
	"github.com/livelo/csdd/internal/templater"
	"github.com/livelo/csdd/tui"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "tui" {
		// TUI gets the same embedded templates so its wizards can render artifacts.
		if err := tui.Run(templater.FS); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		return
	}
	os.Exit(cmd.Run(args, templater.FS))
}
