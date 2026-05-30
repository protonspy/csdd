// Package render groups terminal output helpers used by the CLI surface.
// The TUI does its own rendering through lipgloss and never imports this package.
package render

import (
	"fmt"
	"os"
)

var useColor = isTTY(os.Stdout) && os.Getenv("NO_COLOR") == ""

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func code(c, text string) string {
	if !useColor {
		return text
	}
	return "\033[" + c + "m" + text + "\033[0m"
}

func Cyan(s string) string   { return code("36", s) }
func Green(s string) string  { return code("32", s) }
func Yellow(s string) string { return code("33", s) }
func Red(s string) string    { return code("31", s) }
func Bold(s string) string   { return code("1", s) }

// Info prints a neutral status line to stdout.
func Info(msg string) { fmt.Println(Cyan("•") + " " + msg) }

// Ask prints an interactive prompt to stdout without a trailing newline so the
// user's answer appears on the same line.
func Ask(msg string) { fmt.Print(Cyan("?") + " " + msg) }

// OK prints a success status line to stdout.
func OK(msg string) { fmt.Println(Green("✓") + " " + msg) }

// Warn prints a warning status line to stderr.
func Warn(msg string) { fmt.Fprintln(os.Stderr, Yellow("!")+" "+msg) }

// Err prints an error status line to stderr.
func Err(msg string) { fmt.Fprintln(os.Stderr, Red("✗")+" "+msg) }

// Die prints an error and exits with the given code (default 1).
func Die(msg string, code ...int) {
	Err(msg)
	c := 1
	if len(code) > 0 {
		c = code[0]
	}
	os.Exit(c)
}
