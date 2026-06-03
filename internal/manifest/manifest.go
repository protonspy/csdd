// Package manifest records the csdd-managed artifacts written into a workspace
// and a content hash for each. It is the memory that lets `csdd update` tell a
// pristine shipped file (safe to refresh in place) from one the user has edited
// (preserved as a numbered .old backup before the new version lands). The
// package is pure: no knowledge of templates, paths, or the CLI — just load,
// save, and hash — so it stays trivially testable.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Manifest is the on-disk record at .claude/.csdd-manifest.json. Files maps a
// workspace-relative, forward-slash path to the hash of the content csdd shipped
// for it under CsddVersion.
type Manifest struct {
	CsddVersion string            `json:"csdd_version"`
	UpdatedAt   string            `json:"updated_at"`
	Files       map[string]string `json:"files"`
}

// New returns an empty, ready-to-use manifest.
func New() *Manifest {
	return &Manifest{Files: map[string]string{}}
}

// Load reads the manifest at path. A missing file is not an error: it returns a
// fresh empty manifest and exists=false, so callers can special-case the first
// `csdd update` on a workspace created before manifests existed.
func Load(path string) (m *Manifest, exists bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return New(), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	m = New()
	if err := json.Unmarshal(data, m); err != nil {
		return nil, true, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	return m, true, nil
}

// Save stamps the manifest with version and now, then writes it as pretty JSON.
// now is injected (not read from the clock) so callers stay deterministic and
// tests stay stable.
func (m *Manifest) Save(path, version string, now time.Time) error {
	m.CsddVersion = version
	m.UpdatedAt = now.UTC().Format(time.RFC3339)
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Hash is the canonical content hash used throughout the manifest and the update
// reconciler. The "sha256:" prefix makes the algorithm explicit on disk.
func Hash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}
