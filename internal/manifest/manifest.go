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

	"github.com/protonspy/csdd/internal/textutil"
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
	return writeFileAtomic(path, data, 0o644)
}

// writeFileAtomic writes data to a sibling temp file and renames it over path,
// so a crash or concurrent reader never observes a half-written manifest. The
// temp file lives in the same directory as path so the rename stays on one
// filesystem (a cross-device rename would fail).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Hash is the canonical content hash used throughout the manifest and the update
// reconciler. The "sha256:" prefix makes the algorithm explicit on disk.
// Content is line-ending normalized first so a file's hash is identical whether
// git checked it out as LF or CRLF — otherwise every managed file on a Windows
// autocrlf clone would be misread as user-edited by `csdd update`.
func Hash(content string) string {
	sum := sha256.Sum256([]byte(textutil.NormalizeNewlines(content)))
	return "sha256:" + hex.EncodeToString(sum[:])
}
