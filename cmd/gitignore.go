package cmd

import (
	"os"
	"path/filepath"
	"strings"
)

// gitignoreTargets lists the root-level kspec artifacts that should not be
// committed — the compiled binary and the operational CLI guide — but only the
// ones that actually exist at root. A fresh `kspec init` always materializes
// kspec.md; the binary is present only when the user dropped it in the repo.
func gitignoreTargets(root string) []string {
	var targets []string
	for _, name := range []string{"kspec", "kspec.exe", "kspec.md"} {
		if pathExists(filepath.Join(root, name)) {
			targets = append(targets, name)
		}
	}
	return targets
}

// ensureGitignore appends the given names to root/.gitignore (creating it when
// absent) and returns the entries it actually added. Entries already present —
// whether anchored (/kspec) or bare (kspec) — are skipped, so it is idempotent.
// Names are written anchored to the repo root, matching kspec's own .gitignore.
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
	if !have["# kspec"] {
		if existing != "" {
			b.WriteString("\n")
		}
		b.WriteString("# kspec\n")
	}
	for _, e := range toAdd {
		b.WriteString(e + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	return toAdd, nil
}
