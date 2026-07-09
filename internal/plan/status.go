package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/textutil"
	"github.com/protonspy/csdd/internal/validator"
	"github.com/protonspy/csdd/internal/workspace"
)

// Feat states (R5.1). A feat's state is DERIVED from specs/<feat>/ reality on
// every read — never stored — so there is no second bookkeeping to desync (design
// principle 4).
const (
	StatePending      = "pending"      // no spec directory yet
	StateRequirements = "requirements" // spec exists; requirements not yet approved
	StateDesign       = "design"       // requirements approved; design not yet approved
	StateTasks        = "tasks"        // design approved; tasks not yet approved
	StateReady        = "ready"        // ready_for_implementation; no task checked yet
	StateImplementing = "implementing" // some but not all tasks checked
	StateDone         = "done"         // every task checked
	StateBlocked      = "blocked"      // a runner block marker exists for the feat
)

// FeatStatus is one feat's derived state plus the evidence behind it.
type FeatStatus struct {
	Slug         string `json:"feat"`
	Num          string `json:"num,omitempty"`
	Milestone    string `json:"milestone,omitempty"`
	State        string `json:"state"`
	TasksTotal   int    `json:"tasks_total,omitempty"`
	TasksChecked int    `json:"tasks_checked,omitempty"`
	BlockKind    string `json:"block_kind,omitempty"` // which way out applies (see Block)
	BlockReason  string `json:"block_reason,omitempty"`
	BlockLog     string `json:"block_log,omitempty"`
}

// PlanStatus is the whole-plan derived view: plan-level flags (approval, drift,
// unprocessed deviations) plus per-feat states, in table order.
type PlanStatus struct {
	Slug       string       `json:"plan"`
	Name       string       `json:"name,omitempty"`
	Approved   bool         `json:"approved"`
	Drift      bool         `json:"drift"`
	Deviations int          `json:"deviations"`
	Feats      []FeatStatus `json:"feats"`
}

// DeriveStatus computes the plan-level flags and every feat's state from disk
// reality. It performs no writes and is deterministic given the workspace.
func DeriveStatus(root string, doc *PlanDoc) (PlanStatus, error) {
	dir := Dir(root, doc.Slug)
	st := PlanStatus{Slug: doc.Slug, Name: doc.Name}

	approved, drift, err := IsApproved(root, doc.Slug)
	if err != nil {
		return st, err
	}
	st.Approved = approved
	st.Drift = drift
	st.Deviations = countDeviations(dir)

	for _, f := range doc.Feats {
		st.Feats = append(st.Feats, deriveFeatStatus(root, doc.Slug, f))
	}
	return st, nil
}

// deriveFeatStatus resolves a single feat's state. A block marker always wins —
// clearing it is a runner or human act (`plan run` retires the repairable ones,
// `plan approve` retires deviations against a revised plan, `plan unblock` retires
// any) — otherwise the state follows spec approvals and, once ready, the tasks.md
// checkbox progress.
func deriveFeatStatus(root, slug string, f Feat) FeatStatus {
	fs := FeatStatus{Slug: f.Slug, Num: f.Num, Milestone: f.Milestone}

	if b, blocked := ReadBlock(root, slug, f.Slug); blocked {
		fs.State = StateBlocked
		fs.BlockKind = b.Kind
		fs.BlockReason = b.Reason
		fs.BlockLog = b.Log
		return fs
	}

	specDir := filepath.Join(paths.Specs(root), f.Slug)
	if _, err := os.Stat(specDir); err != nil {
		fs.State = StatePending
		return fs
	}

	ap := readSpecApprovals(specDir)
	switch {
	case !ap["requirements"]:
		fs.State = StateRequirements
	case !ap["design"]:
		fs.State = StateDesign
	case !ap["tasks"]:
		fs.State = StateTasks
	default:
		total, checked := taskProgress(specDir)
		fs.TasksTotal, fs.TasksChecked = total, checked
		switch {
		case total > 0 && checked == total:
			fs.State = StateDone
		case checked > 0:
			fs.State = StateImplementing
		default:
			fs.State = StateReady
		}
	}
	return fs
}

// readSpecApprovals returns which of the three phases are approved in spec.json.
// A missing or malformed spec.json yields no approvals (state "requirements"),
// never a crash — the spec folder exists, so work has begun but nothing is
// certified.
func readSpecApprovals(specDir string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(specDir, "spec.json"))
	if err != nil {
		return out
	}
	var doc struct {
		Approvals map[string]struct {
			Approved bool `json:"approved"`
		} `json:"approvals"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return out
	}
	for _, p := range workspace.SpecPhases {
		out[p] = doc.Approvals[p].Approved
	}
	return out
}

// taskProgress counts task checkboxes in tasks.md, reusing the validator's
// canonical task grammar (validator.TaskLineRe) and fence masking so a fenced
// example is never miscounted — the same parse the dashboard and lint use.
func taskProgress(specDir string) (total, checked int) {
	data, err := os.ReadFile(filepath.Join(specDir, "tasks.md"))
	if err != nil {
		return 0, 0
	}
	text := validator.MaskCodeFences(textutil.NormalizeNewlines(string(data)))
	for _, line := range strings.Split(text, "\n") {
		m := validator.TaskLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		total++
		if s := strings.ToLower(strings.TrimSpace(m[2])); s == "x" {
			checked++
		}
	}
	return total, checked
}

// stateDir is the transient runner-state directory .csdd/plan/<slug>/ (§2),
// regenerable and gitignored.
func stateDir(root, slug string) string {
	return filepath.Join(paths.State(root), "plan", slug)
}

// A deviation entry ends in a bare `blocked` outcome. A mechanical block is
// journaled as `blocked (<kind>)` and an unblock as `unblocked`, so neither can be
// mistaken for the thing the human owes the plan.
var (
	reDeviationEntry = regexp.MustCompile(`^##\s+\[\d{4}-\d{2}-\d{2}\].*\|\s*` + regexp.QuoteMeta(VerdictBlocked) + `\s*$`)
	reUnblockedEntry = regexp.MustCompile(`^##\s+\[\d{4}-\d{2}-\d{2}\].*\|\s*unblocked\b`)
)

// countDeviations counts the deviations in the run journal (log.md) that nobody has
// answered yet: blocked entries minus the unblocks that retired them. A deviation is
// a structured objection the human must fold into the plan and re-approve
// (principle 5); the outstanding count is surfaced as a plan-level flag (R5.2).
func countDeviations(dir string) int {
	data, err := os.ReadFile(filepath.Join(dir, "log.md"))
	if err != nil {
		return 0
	}
	blocked, unblocked := 0, 0
	for _, line := range strings.Split(textutil.NormalizeNewlines(string(data)), "\n") {
		line = strings.TrimRight(line, " \t")
		switch {
		case reDeviationEntry.MatchString(line):
			blocked++
		case reUnblockedEntry.MatchString(line):
			unblocked++
		}
	}
	if n := blocked - unblocked; n > 0 {
		return n
	}
	return 0
}
