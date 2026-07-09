package plan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Block kinds. Only a deviation says something about the PLAN: the session hit an
// open decision, so the contract itself has to change and folding the revision back
// in — then re-approving — is the only legitimate way out (principle 5). The other
// kinds are mechanical failures of one step; the runner may repair and retry those
// on its own. A marker written by an older csdd carries no kind and reads as
// BlockUnknown, which is never auto-cleared: we cannot tell which of the two it was.
const (
	BlockDeviation   = "deviation"
	BlockGateFailure = "gate-failure"
	BlockScaffold    = "scaffold"
	BlockBrief       = "brief"
	BlockUnknown     = "unknown"
)

// Block is the marker at .csdd/plan/<slug>/blocked/<feat>: why the runner stopped
// working a feat, and what it would take to resume. It lives under the transient
// state dir, so it is regenerable and gitignored — the durable record of the same
// event is the journal line in docs/plans/<slug>/log.md.
type Block struct {
	Feat      string `json:"feat"`
	Step      string `json:"step,omitempty"`
	Kind      string `json:"kind"`
	Reason    string `json:"reason"`
	Revision  string `json:"revision,omitempty"`  // deviation only: the session's proposal
	Attempts  int    `json:"attempts,omitempty"`  // sessions spent on the step before blocking
	Repairs   int    `json:"repairs,omitempty"`   // repair sessions already spent on this feat
	PlanHash  string `json:"plan_hash,omitempty"` // deviation only: the plan the objection was against
	Log       string `json:"log,omitempty"`       // workspace-relative failure log
	BlockedAt string `json:"blocked_at,omitempty"`
}

// Mechanical reports whether the block records a failure of one attempt rather
// than a fault in the plan. Mechanical blocks are the runner's to retry; the rest
// need the plan revised or an explicit `csdd plan unblock`.
func (b Block) Mechanical() bool {
	switch b.Kind {
	case BlockGateFailure, BlockScaffold, BlockBrief:
		return true
	}
	return false
}

func blockedDir(root, slug string) string      { return filepath.Join(stateDir(root, slug), "blocked") }
func blockPath(root, slug, feat string) string { return filepath.Join(blockedDir(root, slug), feat) }

// WriteBlock persists a marker, replacing any existing one for the feat.
func WriteBlock(root, slug string, b Block) error {
	if err := os.MkdirAll(blockedDir(root, slug), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(blockPath(root, slug, b.Feat), append(data, '\n'), 0o644)
}

// ReadBlock returns a feat's marker and whether one exists. A marker that does not
// parse as a typed Block is a plain-text reason written by an older csdd (or by
// hand): it reads as BlockUnknown so nothing auto-clears it.
func ReadBlock(root, slug, feat string) (Block, bool) {
	data, err := os.ReadFile(blockPath(root, slug, feat))
	if err != nil {
		return Block{}, false
	}
	text := strings.TrimSpace(string(data))
	var b Block
	if json.Unmarshal([]byte(text), &b) == nil && b.Kind != "" {
		b.Feat = feat
		return b, true
	}
	return Block{Feat: feat, Kind: BlockUnknown, Reason: text}, true
}

// ListBlocks returns every block marker for a plan, in feat order.
func ListBlocks(root, slug string) []Block {
	entries, err := os.ReadDir(blockedDir(root, slug))
	if err != nil {
		return nil
	}
	var out []Block
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if b, ok := ReadBlock(root, slug, e.Name()); ok {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Feat < out[j].Feat })
	return out
}

// ClearBlock removes a feat's marker. A missing marker is not an error.
func ClearBlock(root, slug, feat string) error {
	err := os.Remove(blockPath(root, slug, feat))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// Unblock clears a marker and journals the event, so the run journal carries the
// unblock next to the block that provoked it. via names the actor for the audit
// line (a command, or the re-approval that retired a deviation).
func Unblock(root, slug string, b Block, via string, now time.Time) error {
	if err := ClearBlock(root, slug, b.Feat); err != nil {
		return err
	}
	appendJournal(Dir(root, slug), now, b.Step, b.Feat, "unblocked", "via: "+via)
	return nil
}

// ClearStaleDeviations retires every deviation marker raised against a plan other
// than the one now approved. Revising plan.md and re-approving IS the unblock: the
// session objected to a contract that no longer exists (principle 5). A deviation
// against the still-current plan survives — re-approving an unchanged plan resolves
// nothing. Returns the feats it unblocked.
func ClearStaleDeviations(root, slug string, now time.Time) ([]string, error) {
	pj, ok, err := LoadPlanJSON(Dir(root, slug))
	if err != nil || !ok || !pj.Approvals.Approved {
		return nil, err
	}
	approved := pj.Approvals.ContentHash
	var cleared []string
	for _, b := range ListBlocks(root, slug) {
		if b.Kind != BlockDeviation || b.PlanHash == "" || b.PlanHash == approved {
			continue
		}
		if err := Unblock(root, slug, b, "plan approve (the plan was revised)", now); err != nil {
			return cleared, err
		}
		cleared = append(cleared, b.Feat)
	}
	return cleared, nil
}

// appendJournal writes one `## [YYYY-MM-DD] <step> | <feat> | <outcome>` entry to
// the run journal, plus an optional `- <detail>` line per detail (§5.6). Every
// runner and CLI writer of log.md goes through here so the grammar stays in one
// place — the journal is the durable, reviewable half of the transient state dir.
func appendJournal(dir string, now time.Time, step, feat, outcome string, details ...string) {
	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] %s | %s | %s\n", now.UTC().Format("2006-01-02"), orDash(step), feat, outcome)
	for _, d := range details {
		fmt.Fprintf(&b, "- %s\n", d)
	}
	appendFile(filepath.Join(dir, "log.md"), b.String())
}
