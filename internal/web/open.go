package web

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/protonspy/csdd/internal/render"
)

// openBrowser best-effort opens url in the user's default browser. It never
// blocks and never fails the server: if no opener is found (headless, locked
// down, unusual environment) it just leaves the URL printed on screen.
func openBrowser(url string) {
	if cmd := browserCommand(url); cmd != nil {
		_ = cmd.Start() // detached; we neither wait nor surface its error
		return
	}
	render.Info("open " + url + " in your browser")
}

func browserCommand(url string) *exec.Cmd {
	// On WSL, GUI apps live on the Windows side: prefer wslview (wslu), and fall
	// back to launching the Windows shell, before the generic Linux opener.
	if isWSL() {
		if cmd := lookCmd("wslview", url); cmd != nil {
			return cmd
		}
		if path, err := exec.LookPath("cmd.exe"); err == nil {
			return exec.Command(path, "/c", "start", "", url)
		}
	}
	switch runtime.GOOS {
	case "darwin":
		return lookCmd("open", url)
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux, *bsd, …
		return lookCmd("xdg-open", url)
	}
}

func lookCmd(bin, url string) *exec.Cmd {
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil
	}
	return exec.Command(path, url)
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	v := strings.ToLower(string(data))
	return strings.Contains(v, "microsoft") || strings.Contains(v, "wsl")
}
