package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protonspy/csdd/internal/templater"
)

func readSandboxFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".devcontainer", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func TestSandboxInitScaffold(t *testing.T) {
	root := t.TempDir()
	written, err := SandboxInit(templater.FS, SandboxOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 4 {
		t.Fatalf("expected 4 files written, got %v", written)
	}
	for _, name := range []string{"devcontainer.json", "Dockerfile", "init-firewall.sh", "allowed-domains.txt"} {
		content := readSandboxFile(t, root, name)
		if strings.Contains(content, "\r\n") {
			t.Errorf("%s contains CRLF; must be LF-normalized", name)
		}
	}
	// The firewall script is executable.
	fi, err := os.Stat(filepath.Join(root, ".devcontainer", "init-firewall.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("init-firewall.sh should be executable, got %v", fi.Mode())
	}
	// Default (non-hardened) build arg.
	if !strings.Contains(readSandboxFile(t, root, "devcontainer.json"), `"HARDENED": "false"`) {
		t.Errorf("default devcontainer should set HARDENED=false")
	}
}

func TestSandboxInitIdempotentAndForce(t *testing.T) {
	root := t.TempDir()
	if _, err := SandboxInit(templater.FS, SandboxOptions{Root: root}); err != nil {
		t.Fatal(err)
	}
	// Hand-edit a file, then re-run without --force: it must be preserved.
	target := filepath.Join(root, ".devcontainer", "allowed-domains.txt")
	if err := os.WriteFile(target, []byte("# custom\nmy.host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := SandboxInit(templater.FS, SandboxOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 {
		t.Errorf("idempotent re-init should write nothing, wrote %v", written)
	}
	if got := readSandboxFile(t, root, "allowed-domains.txt"); !strings.Contains(got, "my.host") {
		t.Errorf("re-init clobbered a hand-edited file")
	}
	// --force regenerates.
	written, err = SandboxInit(templater.FS, SandboxOptions{Root: root, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 4 {
		t.Errorf("--force should rewrite all four files, wrote %v", written)
	}
}

func TestSandboxInitFeaturesAndDomains(t *testing.T) {
	root := t.TempDir()
	_, err := SandboxInit(templater.FS, SandboxOptions{
		Root:         root,
		Features:     []string{"go"},
		AllowDomains: []string{"internal.registry.example"},
		Hardened:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dc := readSandboxFile(t, root, "devcontainer.json")
	if !strings.Contains(dc, "features/go:1") {
		t.Errorf("--feature go should add the go devcontainer feature: %s", dc)
	}
	if !strings.Contains(dc, `"HARDENED": "true"`) {
		t.Errorf("--hardened should set HARDENED=true")
	}
	domains := readSandboxFile(t, root, "allowed-domains.txt")
	if !strings.Contains(domains, "proxy.golang.org") {
		t.Errorf("--feature go should add its registry domains")
	}
	if !strings.Contains(domains, "internal.registry.example") {
		t.Errorf("--allow-domain should append the host")
	}
}

func TestSandboxInitUnknownFeature(t *testing.T) {
	root := t.TempDir()
	_, err := SandboxInit(templater.FS, SandboxOptions{Root: root, Features: []string{"cobol"}})
	if err == nil || !strings.Contains(err.Error(), "unknown --feature") {
		t.Errorf("unknown feature should error, got %v", err)
	}
}

func TestDoctor(t *testing.T) {
	cases := []struct {
		name      string
		dev       string
		euid      int
		control   bool // control host reachable
		anthropic bool // anthropic reachable
		wantOK    bool
	}{
		{"all good", "true", 1000, false, true, true},
		{"not in devcontainer", "", 1000, false, true, false},
		{"root user", "true", 0, false, true, false},
		{"firewall leaks control", "true", 1000, true, true, false},
		{"anthropic blocked", "true", 1000, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := Doctor(DoctorProbes{
				Getenv:  func(string) string { return tc.dev },
				Geteuid: func() int { return tc.euid },
				Reachable: func(hp string) bool {
					if hp == doctorControlHost {
						return tc.control
					}
					return tc.anthropic
				},
			})
			if report.OK != tc.wantOK {
				t.Errorf("Doctor OK = %v, want %v (checks=%+v)", report.OK, tc.wantOK, report.Checks)
			}
			if len(report.Checks) != 3 {
				t.Errorf("expected 3 individual checks, got %d", len(report.Checks))
			}
		})
	}
}
