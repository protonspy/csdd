package session

import "testing"

func TestChangedDeterministic(t *testing.T) {
	base := Snapshot{
		"specs/f/tasks.md":  {Size: 10, ModNano: 100},
		"specs/f/spec.json": {Size: 5, ModNano: 50},
	}

	same := Snapshot{
		"specs/f/tasks.md":  {Size: 10, ModNano: 100},
		"specs/f/spec.json": {Size: 5, ModNano: 50},
	}
	if Changed(base, same) {
		t.Errorf("identical snapshots should not be reported changed")
	}

	cases := map[string]Snapshot{
		"added": {
			"specs/f/tasks.md":  {Size: 10, ModNano: 100},
			"specs/f/spec.json": {Size: 5, ModNano: 50},
			"specs/f/design.md": {Size: 1, ModNano: 1},
		},
		"removed": {
			"specs/f/tasks.md": {Size: 10, ModNano: 100},
		},
		"resized": {
			"specs/f/tasks.md":  {Size: 11, ModNano: 100},
			"specs/f/spec.json": {Size: 5, ModNano: 50},
		},
		"retouched": {
			"specs/f/tasks.md":  {Size: 10, ModNano: 999},
			"specs/f/spec.json": {Size: 5, ModNano: 50},
		},
	}
	for name, next := range cases {
		if !Changed(base, next) {
			t.Errorf("%s: expected Changed to be true", name)
		}
	}
}

func TestTakeSnapshot(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"specs/f/tasks.md":            "x\n",
		".claude/steering/product.md": "y\n",
		".claude/.git/HEAD":           "ref\n", // skipped
		"CLAUDE.md":                   "z\n",
	})
	snap := TakeSnapshot(root)
	if _, ok := snap["specs/f/tasks.md"]; !ok {
		t.Errorf("snapshot missing specs/f/tasks.md (got %v)", keys(snap))
	}
	if _, ok := snap["CLAUDE.md"]; !ok {
		t.Errorf("snapshot missing CLAUDE.md")
	}
	for k := range snap {
		if k == ".claude/.git/HEAD" {
			t.Errorf(".git contents should be skipped from the snapshot")
		}
	}
}

func keys(s Snapshot) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	return out
}
