package cli

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/protonspy/csdd/internal/render"
)

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func mkdirAll(p string) error {
	return os.MkdirAll(p, 0o755)
}

func intStr(n int) string { return strconv.Itoa(n) }

// confirmStdin is indirected so tests can feed answers to interactive prompts.
// When nil (the default), confirm reads from the real terminal — but only if
// stdin is a TTY, so headless/agent runs stay fully scriptable.
var confirmStdin io.Reader

func stdinIsInteractive() bool {
	if confirmStdin != nil {
		return true
	}
	// A prompt needs stdout to show the question and stdin to read the answer,
	// so require both to be a terminal. This also keeps the test suite from
	// blocking: the capture helper swaps stdout for a pipe.
	return isCharDevice(os.Stdin) && isCharDevice(os.Stdout)
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// confirm prints a yes/no question and returns true only on an explicit "y"/"yes".
// In non-interactive contexts (piped stdin, agents, tests without an injected
// reader) it returns false without prompting, so the safe default is "no".
func confirm(question string) bool {
	if !stdinIsInteractive() {
		return false
	}
	var r io.Reader = os.Stdin
	if confirmStdin != nil {
		r = confirmStdin
	}
	render.Ask(question + " [y/N]: ")
	line, _ := bufio.NewReader(r).ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
