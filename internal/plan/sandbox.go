package plan

import (
	"embed"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/templater"
	"github.com/protonspy/csdd/internal/workspace"
)

// nodeFeatureID is the devcontainer feature the base scaffold always installs
// (Claude Code's Node runtime). A `--feature node` is therefore a no-op.
const nodeFeatureID = "ghcr.io/devcontainers/features/node:1"

// featurePreset maps a `--feature <name>` to the devcontainer feature it adds and
// the registry domains its toolchain needs (appended to the egress allow-list).
type featurePreset struct {
	id      string
	domains []string
}

// sandboxFeatures is the closed set of `--feature` presets (R10.2). Language
// toolchains arrive as features so the base image stays language-agnostic.
var sandboxFeatures = map[string]featurePreset{
	"go":     {"ghcr.io/devcontainers/features/go:1", []string{"proxy.golang.org", "sum.golang.org"}},
	"python": {"ghcr.io/devcontainers/features/python:1", []string{"pypi.org", "files.pythonhosted.org"}},
	"node":   {nodeFeatureID, nil}, // already in the base scaffold
	"rust":   {"ghcr.io/devcontainers/features/rust:1", []string{"static.crates.io", "crates.io", "index.crates.io"}},
	"java":   {"ghcr.io/devcontainers/features/java:1", []string{"repo.maven.apache.org"}},
}

// SandboxFeatureNames returns the supported --feature names, sorted (for help
// and error messages).
func SandboxFeatureNames() []string {
	out := make([]string, 0, len(sandboxFeatures))
	for k := range sandboxFeatures {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SandboxOptions is the headless input for `csdd sandbox init`.
type SandboxOptions struct {
	Root         string
	Features     []string
	AllowDomains []string
	Hardened     bool
	Force        bool
}

// sandboxFile is one scaffolded devcontainer file with its on-disk mode.
type sandboxFile struct {
	name    string
	content string
	mode    os.FileMode
}

// SandboxInit scaffolds .devcontainer/ with the four-file template (R10). It
// returns the base names actually written (idempotent: an existing file is left
// untouched unless Force). Every file is LF-normalized by the templater so the
// firewall script survives a CRLF working tree (R10.3).
func SandboxInit(templates embed.FS, opts SandboxOptions) ([]string, error) {
	extraFeatureIDs, extraDomains, err := resolveSandboxFeatures(opts)
	if err != nil {
		return nil, err
	}

	hardened := "false"
	if opts.Hardened {
		hardened = "true"
	}
	devcontainer, err := templater.Render(templates, "templates/sandbox/devcontainer.json.tmpl", map[string]string{
		"Hardened":      hardened,
		"ExtraFeatures": renderExtraFeatures(extraFeatureIDs),
	})
	if err != nil {
		return nil, err
	}
	dockerfile, err := templater.Static(templates, "templates/sandbox/Dockerfile.tmpl")
	if err != nil {
		return nil, err
	}
	firewall, err := templater.Static(templates, "templates/sandbox/init-firewall.sh.tmpl")
	if err != nil {
		return nil, err
	}
	domains, err := templater.Static(templates, "templates/sandbox/allowed-domains.txt.tmpl")
	if err != nil {
		return nil, err
	}
	domains = appendAllowDomains(domains, extraDomains)

	files := []sandboxFile{
		{"devcontainer.json", devcontainer, 0o644},
		{"Dockerfile", dockerfile, 0o644},
		{"init-firewall.sh", firewall, 0o755},
		{"allowed-domains.txt", domains, 0o644},
	}
	dir := paths.Devcontainer(opts.Root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	for _, f := range files {
		target := filepath.Join(dir, f.name)
		if _, statErr := os.Stat(target); statErr == nil && !opts.Force {
			continue // idempotent: never clobber without --force
		}
		if err := workspace.AtomicWrite(target, []byte(f.content), f.mode); err != nil {
			return nil, err
		}
		written = append(written, f.name)
	}
	return written, nil
}

// resolveSandboxFeatures validates the requested features and collects the extra
// feature IDs and allow-list domains they imply, plus any explicit --allow-domain
// hosts, deduplicated with first-seen order preserved.
func resolveSandboxFeatures(opts SandboxOptions) (featureIDs, domains []string, err error) {
	seenFeat := map[string]bool{}
	seenDom := map[string]bool{}
	addDomain := func(d string) {
		if d = strings.TrimSpace(d); d != "" && !seenDom[d] {
			seenDom[d] = true
			domains = append(domains, d)
		}
	}
	for _, name := range opts.Features {
		p, ok := sandboxFeatures[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return nil, nil, fmt.Errorf("unknown --feature %q; supported: %s", name, strings.Join(SandboxFeatureNames(), ", "))
		}
		if p.id != "" && p.id != nodeFeatureID && !seenFeat[p.id] {
			seenFeat[p.id] = true
			featureIDs = append(featureIDs, p.id)
		}
		for _, d := range p.domains {
			addDomain(d)
		}
	}
	for _, d := range opts.AllowDomains {
		addDomain(d)
	}
	return featureIDs, domains, nil
}

// renderExtraFeatures formats additional feature IDs as the JSONC tail that slots
// after the base node feature: each becomes `,\n    "<id>": {}`. Empty when there
// are none, keeping the base features object valid.
func renderExtraFeatures(ids []string) string {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(",\n    ")
		b.WriteString(`"` + id + `": {}`)
	}
	return b.String()
}

// appendAllowDomains appends an active-host block to the rendered allow-list. The
// preset registries in the template stay commented; the flags add live entries.
func appendAllowDomains(base string, domains []string) string {
	if len(domains) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(base, "\n"))
	b.WriteString("\n\n# --- added by `csdd sandbox init` flags ---\n")
	for _, d := range domains {
		b.WriteString(d)
		b.WriteByte('\n')
	}
	return b.String()
}

// --- doctor (R11) ---------------------------------------------------------

// SandboxCheck is one isolation check with its individual verdict (R11.2).
type SandboxCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// SandboxReport is the whole doctor result; OK is true only when every check is.
type SandboxReport struct {
	OK     bool           `json:"ok"`
	Checks []SandboxCheck `json:"checks"`
}

// DoctorProbes indirects the three environment queries so tests can drive doctor
// without a real container or network. Nil fields fall back to the real probes.
type DoctorProbes struct {
	Getenv    func(string) string
	Geteuid   func() int
	Reachable func(hostPort string) bool
}

// Control and Anthropic probe targets. The control host is deliberately NOT in
// the default allow-list, so a working firewall makes it unreachable.
const (
	doctorControlHost   = "example.com:443"
	doctorAnthropicHost = "api.anthropic.com:443"
)

// Doctor verifies the session is inside the hardened container (R11.1): the
// devcontainer env marker, a non-root user, and an active firewall (the control
// host blocked AND api.anthropic.com reachable). It performs no network beyond
// the two reachability probes.
func Doctor(p DoctorProbes) SandboxReport {
	if p.Getenv == nil {
		p.Getenv = os.Getenv
	}
	if p.Geteuid == nil {
		p.Geteuid = os.Geteuid
	}
	if p.Reachable == nil {
		p.Reachable = tcpReachable
	}

	var checks []SandboxCheck

	devVal := p.Getenv("DEVCONTAINER")
	checks = append(checks, SandboxCheck{
		Name: "devcontainer_env", OK: devVal == "true",
		Detail: "DEVCONTAINER=" + devVal + " (want true)",
	})

	euid := p.Geteuid()
	checks = append(checks, SandboxCheck{
		Name: "non_root_user", OK: euid != 0,
		Detail: fmt.Sprintf("euid=%d (want non-zero)", euid),
	})

	controlUp := p.Reachable(doctorControlHost)
	anthropicUp := p.Reachable(doctorAnthropicHost)
	checks = append(checks, SandboxCheck{
		Name: "firewall_active", OK: !controlUp && anthropicUp,
		Detail: fmt.Sprintf("control %s reachable=%v (want false); %s reachable=%v (want true)",
			doctorControlHost, controlUp, doctorAnthropicHost, anthropicUp),
	})

	ok := true
	for _, c := range checks {
		if !c.OK {
			ok = false
		}
	}
	return SandboxReport{OK: ok, Checks: checks}
}

// tcpReachable reports whether a TCP connection to hostPort succeeds within a
// short timeout.
func tcpReachable(hostPort string) bool {
	conn, err := net.DialTimeout("tcp", hostPort, 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
