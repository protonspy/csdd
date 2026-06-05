package session

import (
	"io/fs"
	"path/filepath"
)

// FileMeta is the size + modification time used to detect a file change without
// reading its contents.
type FileMeta struct {
	Size    int64
	ModNano int64
}

// Snapshot maps each watched file's workspace-relative path to its FileMeta.
type Snapshot map[string]FileMeta

// TakeSnapshot walks the csdd-managed roots (specs/, .claude/, CLAUDE.md,
// .mcp.json) and records every file's size + mtime. Live updates track these —
// not the whole project — so unrelated edits don't churn the dashboard.
// Transient read errors are tolerated, so a snapshot is always returned.
func TakeSnapshot(root string) Snapshot {
	snap := Snapshot{}
	for _, r := range csddRoots {
		abs := filepath.Join(root, r.rel)
		_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			snap[rel(root, p)] = FileMeta{Size: info.Size(), ModNano: info.ModTime().UnixNano()}
			return nil
		})
	}
	return snap
}

// Changed reports whether two snapshots differ — a file added, removed, resized,
// or re-touched. It is a pure comparison with no disk access, so it is
// deterministic and trivially testable.
func Changed(prev, next Snapshot) bool {
	if len(prev) != len(next) {
		return true
	}
	for k, v := range next {
		if pv, ok := prev[k]; !ok || pv != v {
			return true
		}
	}
	return false
}
