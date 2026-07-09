// Package plan implements csdd's plan layer: the structured, contract-bound
// document under docs/plans/<slug>/ that sits ABOVE specs. A plan decomposes an
// initiative into feats, where each feat becomes exactly one spec in specs/. This
// package owns the machine-readable core of that document — parsing the grammar
// (§5.4), linting it (R3), deriving per-feat status from specs/ reality (R5),
// sequencing the next step (R6), assembling deterministic briefs (R7), and (M4)
// driving the autonomous runner (R9).
//
// Design principle 1 holds here as it does in internal/cli/spec.go: this package
// never authors prose. It scaffolds, parses, derives, sequences, and appends
// mechanical state (plan.json, log.md); the plan's content is human/LLM-authored.
package plan

// Frontmatter status values for a plan document (R2.3).
const (
	StatusDraft      = "draft"
	StatusApproved   = "approved"
	StatusSuperseded = "superseded"
)

// validStatuses is the closed set of plan frontmatter status values.
var validStatuses = []string{StatusDraft, StatusApproved, StatusSuperseded}

// ValidStatus reports whether s is a recognized plan frontmatter status.
func ValidStatus(s string) bool {
	for _, v := range validStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// Feat is one row of the `## Feats` table (R2.1). A feat becomes exactly one
// spec directory, so Slug must be a valid spec-directory name (kebab-case). The
// reference lists are pre-tokenized: WikiRefs holds [[wiki-page]] targets (inner
// text only) and StackRefs holds stack:<name> technology names; Refs preserves
// the raw whitespace-separated tokens verbatim for the brief's provenance.
type Feat struct {
	Num       string   // the "#" column, as written (display/order only)
	Slug      string   // the Feat slug — a spec-directory name
	Objective string   // the Objective column
	Depends   []string // explicit feat slugs (range shorthand is never expanded)
	Milestone string   // the Milestone column
	Parallel  bool     // the (P) column — honored for ordering only in v1
	WikiRefs  []string // [[wiki-page]] targets, brackets/aliases stripped
	StackRefs []string // stack:<name> technology names
	ADRRefs   []string // adr:<slug> decision-record slugs (verbatim, incl. malformed)
	Refs      []string // every Refs token verbatim (superset of the three above)
	Line      int      // 1-based source line of the row (for findings)
}

// Gate is one `## Quality Gates` line (R2.2): `- <label>: <command>`. The gate
// list is the plan's own verification contract — the runner executes every gate
// after each implementation step (R9.2).
type Gate struct {
	Label   string
	Command string
	Line    int // 1-based source line (for findings)
}

// PlanDoc is a parsed plan.md. Slug is set by the loader from the directory name,
// not the document; Name/Status come from frontmatter. Feats/Gates/ExecutorNotes
// are the three parseable regions (§5.4); unknown sections are ignored. Grammar
// issues found during parsing (malformed table shape, missing columns) are
// carried on the doc so ValidatePlan can surface them alongside semantic findings.
type PlanDoc struct {
	Slug          string
	Name          string
	Status        string
	Feats         []Feat
	Gates         []Gate
	ExecutorNotes string

	// grammar holds structural parse problems (table shape / missing columns).
	// Unexported: callers reach them via ValidatePlan, never directly.
	grammar []gramIssue
}

// gramIssue is a structural parse problem, kept minimal so model.go carries no
// dependency on internal/validator. ValidatePlan lifts these into validator.Issue.
type gramIssue struct {
	line int
	msg  string
}

// Feat returns the feat with the given slug and whether it was found.
func (d *PlanDoc) Feat(slug string) (Feat, bool) {
	for _, f := range d.Feats {
		if f.Slug == slug {
			return f, true
		}
	}
	return Feat{}, false
}

// FeatSlugs returns the feat slugs in table order.
func (d *PlanDoc) FeatSlugs() []string {
	out := make([]string, 0, len(d.Feats))
	for _, f := range d.Feats {
		out = append(out, f.Slug)
	}
	return out
}

// planSchemaVersion is the current plan.json schema. Written on every save so a
// future csdd can detect and migrate older layouts (the spec.json precedent).
const planSchemaVersion = 1

// PlanApproval binds an approval to exact content. ContentHash covers plan.md +
// seeds/** + the docs/stack.md Decided rows (R4.1) so any post-approval edit is
// detectable as drift (R4.2). CoreHash covers only plan.md + seeds/** — the part
// nobody may change mid-run. The split is what lets the runner tell a recorded
// decision (a new stack row: re-bind and continue) from real drift (an edited
// plan: stop).
type PlanApproval struct {
	Approved    bool   `json:"approved"`
	ContentHash string `json:"content_hash,omitempty"`
	CoreHash    string `json:"core_hash,omitempty"`
	ApprovedAt  string `json:"approved_at,omitempty"`
}

// PlanJSON is the CLI-only approval-state file at docs/plans/<slug>/plan.json
// (§2). It is the plan analog of spec.json: written only by `plan approve`, never
// by a session.
type PlanJSON struct {
	SchemaVersion int          `json:"schema_version,omitempty"`
	Name          string       `json:"name"`
	Approvals     PlanApproval `json:"approvals"`
}

// Verdict is the structured result a runner session returns for one step (§5.6).
// It is how the session DECLARES its intent to the loop — the runner never
// infers it. Status says what to do next: "done" asks the runner to verify with
// the gates and advance, "progress" hands unfinished-but-honest work to the next
// session (Summary carries the handoff), and "halt" ends the whole run because
// something outside the workspace blocks it. Decisions is the audit trail of the
// open decisions the session resolved and recorded this iteration (stack rows,
// ADRs); each one is journaled, and the runner re-binds the approval hash to the
// recorded contract.
type Verdict struct {
	Status    string   `json:"status"`
	Summary   string   `json:"summary"`
	Decisions []string `json:"decisions,omitempty"`
	Revision  string   `json:"revision,omitempty"` // legacy blocked-verdict proposal
}

// Verdict status values. VerdictBlocked is the legacy status older briefs taught:
// the runner still parses it, but treats it as a failed iteration with coaching —
// in the autonomous loop, decisions are the session's to make and record, not to
// stop on.
const (
	VerdictDone     = "done"
	VerdictProgress = "progress"
	VerdictHalt     = "halt"
	VerdictBlocked  = "blocked"
)
