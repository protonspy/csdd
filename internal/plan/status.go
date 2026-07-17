package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	StateDone         = "done"         // every task checked, or the ledger marks it delivered
)

// FeatStatus is one feat's derived state plus the evidence behind it.
type FeatStatus struct {
	Slug         string `json:"feat"`
	Num          string `json:"num,omitempty"`
	Milestone    string `json:"milestone,omitempty"`
	State        string `json:"state"`
	TasksTotal   int    `json:"tasks_total,omitempty"`
	TasksChecked int    `json:"tasks_checked,omitempty"`
}

// PlanStatus is the whole-plan derived view: plan-level flags (approval, drift)
// plus per-feat states, in table order.
type PlanStatus struct {
	Slug     string       `json:"plan"`
	Name     string       `json:"name,omitempty"`
	Approved bool         `json:"approved"`
	Drift    bool         `json:"drift"`
	Feats    []FeatStatus `json:"feats"`
}

// DeriveStatus computes the plan-level flags and every feat's state from disk
// reality. It performs no writes and is deterministic given the workspace.
func DeriveStatus(root string, doc *PlanDoc) (PlanStatus, error) {
	st := PlanStatus{Slug: doc.Slug, Name: doc.Name}

	approved, drift, err := IsApproved(root, doc.Slug)
	if err != nil {
		return st, err
	}
	st.Approved = approved
	st.Drift = drift

	done := LoadLedger(root, doc.Slug).doneSet()
	for _, f := range doc.Feats {
		st.Feats = append(st.Feats, deriveFeatStatus(root, f, done))
	}
	return st, nil
}

// deriveFeatStatus resolves a single feat's state. The ledger wins: a feat the loop
// has recorded delivered reads as done regardless of disk. Otherwise the state
// follows spec approvals and, once ready, the tasks.md checkbox progress.
func deriveFeatStatus(root string, f Feat, done map[string]bool) FeatStatus {
	fs := FeatStatus{Slug: f.Slug, Num: f.Num, Milestone: f.Milestone}

	if done[f.Slug] {
		fs.State = StateDone
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

// deliveredSet is the set of feats that count as delivered for sequencing: those
// the ledger records done, UNION those whose spec is fully delivered on disk
// (StateDone — every phase approved and every task checked). It is read-only, so a
// `plan next` query matches what a resumed `plan run` would skip: a feat developed
// by hand in a session (which never writes the ledger) or delivered by an earlier
// run whose transient ledger was cleaned still reads as done, instead of being
// re-handed from scratch. It never removes a ledger-done feat — the union only adds
// completion the ledger could not have known about.
func deliveredSet(root string, doc *PlanDoc, ledgerDone map[string]bool) map[string]bool {
	out := make(map[string]bool, len(doc.Feats))
	for _, f := range doc.Feats {
		if ledgerDone[f.Slug] || deriveFeatStatus(root, f, ledgerDone).State == StateDone {
			out[f.Slug] = true
		}
	}
	return out
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
