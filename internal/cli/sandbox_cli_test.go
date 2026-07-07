package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protonspy/csdd/internal/plan"
)

func TestSandboxInitCLI(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := run(t, "sandbox", "init", "--root", dir, "--feature", "go", "--hardened")
	if code != 0 {
		t.Fatalf("sandbox init failed (code=%d): %s / %s", code, out, errOut)
	}
	for _, name := range []string{"devcontainer.json", "Dockerfile", "init-firewall.sh", "allowed-domains.txt"} {
		if _, err := os.Stat(filepath.Join(dir, ".devcontainer", name)); err != nil {
			t.Errorf("missing .devcontainer/%s: %v", name, err)
		}
	}
	// Idempotent re-run reports no change.
	code, out, _ = run(t, "sandbox", "init", "--root", dir)
	if code != 0 || !strings.Contains(out, "already scaffolded") {
		t.Errorf("re-init should be idempotent, got code=%d out=%q", code, out)
	}
}

func TestSandboxDoctorCLIExitCodes(t *testing.T) {
	// Stub the probes so the CLI doctor path is exercised without a container
	// or the network. Restore afterward.
	orig := sandboxProbes
	t.Cleanup(func() { sandboxProbes = orig })

	sandboxProbes = plan.DoctorProbes{
		Getenv:    func(string) string { return "true" },
		Geteuid:   func() int { return 1000 },
		Reachable: func(hp string) bool { return hp != "example.com:443" },
	}
	if code, _, _ := run(t, "sandbox", "doctor"); code != 0 {
		t.Errorf("doctor should exit 0 when isolation holds, got %d", code)
	}

	// A leaking control host fails the firewall check → non-zero exit.
	sandboxProbes = plan.DoctorProbes{
		Getenv:    func(string) string { return "true" },
		Geteuid:   func() int { return 1000 },
		Reachable: func(hp string) bool { return true },
	}
	code, out, _ := run(t, "sandbox", "doctor", "--json")
	if code == 0 {
		t.Errorf("doctor should exit non-zero when the firewall leaks")
	}
	if !strings.Contains(out, `"ok": false`) {
		t.Errorf("doctor --json should report ok=false: %s", out)
	}
}
