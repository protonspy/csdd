package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Dependencies the plan did not declare.
//
// The DAG guarantees a feat's declared dependencies are delivered before it is
// handed out, which is what makes concurrent feats safe: two feats running at once
// cannot depend on each other. What it cannot guarantee is that the author wrote
// every edge down. A session that discovers mid-flight that it needs something a
// peer owns reports `blocked` naming that peer, and the scheduler has to remember.
//
// It is remembered HERE and not in plan.md. Writing the edge back into the plan is
// the obvious move and it is wrong: `plan.json` binds approval to a hash of
// plan.md (CoreHash), so the runner mutating the plan would make its own next
// preflight report drift and refuse to run — the loop would sabotage itself the
// moment it learned something. Absorbing a discovered edge into the plan proper is
// therefore a human act, after review, followed by re-approval.
//
// The file lives in the transient state dir beside the ledger. Losing it costs
// nothing durable: a session that was blocked will report blocked again.

// DiscoveredDepsFile is the sidecar's name, beside progress.json.
const DiscoveredDepsFile = "discovered-deps.json"

// DiscoveredDeps maps a feat slug to the feats it was found to depend on at run
// time, over and above what plan.md declares.
type DiscoveredDeps struct {
	SchemaVersion int                 `json:"schema_version,omitempty"`
	Deps          map[string][]string `json:"deps"`
}

const discoveredSchemaVersion = 1

func discoveredPath(root, slug string) string {
	return filepath.Join(stateDir(root, slug), DiscoveredDepsFile)
}

// LoadDiscoveredDeps reads the sidecar. A missing or unparseable file yields an
// empty map — the scheduler then sees exactly the plan's own edges, which is always
// safe.
func LoadDiscoveredDeps(root, slug string) map[string][]string {
	data, err := os.ReadFile(discoveredPath(root, slug))
	if err != nil {
		return map[string][]string{}
	}
	var d DiscoveredDeps
	if json.Unmarshal(data, &d) != nil || d.Deps == nil {
		return map[string][]string{}
	}
	return d.Deps
}

// RecordDiscoveredDeps merges edges into the sidecar and persists it. Edges are
// deduplicated and sorted so the file is stable across runs and reviewable in a
// diff. Returns the merged map so the caller can keep scheduling without re-reading.
func RecordDiscoveredDeps(root, slug, feat string, blockedOn []string) (map[string][]string, error) {
	deps := LoadDiscoveredDeps(root, slug)
	seen := map[string]bool{}
	for _, d := range deps[feat] {
		seen[d] = true
	}
	for _, d := range blockedOn {
		if d != "" && d != feat && !seen[d] {
			seen[d] = true
			deps[feat] = append(deps[feat], d)
		}
	}
	sort.Strings(deps[feat])

	if err := os.MkdirAll(stateDir(root, slug), 0o755); err != nil {
		return deps, err
	}
	out, err := json.MarshalIndent(DiscoveredDeps{SchemaVersion: discoveredSchemaVersion, Deps: deps}, "", "  ")
	if err != nil {
		return deps, err
	}
	return deps, os.WriteFile(discoveredPath(root, slug), append(out, '\n'), 0o644)
}
