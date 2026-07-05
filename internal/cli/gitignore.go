package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/protonspy/csdd/internal/workspace"
)

// pinggyTokenFile is the workspace-local store for the pinggy Pro access token.
// It is a secret — never committed — so it is always gitignored.
const pinggyTokenFile = ".pinggy-token"

// pinggyKnownHostsFile is the workspace-local TOFU known_hosts the tunnel writes
// for pinggy ssh host-key pinning. Not a secret, but workspace-local churn that
// should not be committed.
const pinggyKnownHostsFile = ".pinggy-known_hosts"

// gitignoreTargets lists the root-level csdd artifacts that should not be
// committed — the compiled binary, the pinggy token secret, and the pinggy
// known_hosts. The binary is added only when present; the pinggy files are
// always ignored because `csdd web` may create them later.
func gitignoreTargets(root string) []string {
	targets := []string{pinggyTokenFile, pinggyKnownHostsFile}
	for _, name := range []string{"csdd", "csdd.exe"} {
		if pathExists(filepath.Join(root, name)) {
			targets = append(targets, name)
		}
	}
	return targets
}

// ensureGitignore appends the given names to root/.gitignore (creating it when
// absent) and returns the entries it actually added. Entries already present —
// whether anchored (/csdd) or bare (csdd) — are skipped, so it is idempotent.
// Names are written anchored to the repo root, matching csdd's own .gitignore.
func ensureGitignore(root string, names []string) ([]string, error) {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	existing := string(data)

	have := map[string]bool{}
	for _, line := range strings.Split(existing, "\n") {
		have[strings.TrimSpace(line)] = true
	}

	var toAdd []string
	for _, name := range names {
		entry := "/" + name
		if have[entry] || have[name] {
			continue
		}
		toAdd = append(toAdd, entry)
	}
	if len(toAdd) == 0 {
		return nil, nil
	}

	var b strings.Builder
	b.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		b.WriteString("\n")
	}
	if !have["# csdd"] {
		if existing != "" {
			b.WriteString("\n")
		}
		b.WriteString("# csdd\n")
	}
	for _, e := range toAdd {
		b.WriteString(e + "\n")
	}
	if err := workspace.AtomicWrite(path, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	return toAdd, nil
}
