package web

import (
	"fmt"

	"github.com/protonspy/csdd/internal/plan"
	"github.com/protonspy/csdd/internal/workspace"
)

// planSummary is one row of GET /api/plans: enough to render the Plans list with
// approval/drift badges and milestone-agnostic progress counts.
type planSummary struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Approved bool   `json:"approved"`
	Drift    bool   `json:"drift"`
	Feats    int    `json:"feats"`
	Done     int    `json:"done"`
	// Complete: every feat delivered. Derived by plan.Summarize rather than by
	// each caller comparing done against feats, so the places that show plan state
	// cannot drift apart on what "finished" means.
	Complete bool `json:"complete"`
}

// isComplete reports whether a plan has feats and every one of them is done. A
// plan with no feats is not complete — it is unstarted, and saying otherwise
// would call an empty table finished.
func isComplete(done, total int) bool { return total > 0 && done >= total }

// summaryRow projects a plan.Summary onto this API's row shape. The derivation
// lives in the plan package (shared with `csdd plan list`); only the JSON key
// names are the web's own.
func summaryRow(s plan.Summary) planSummary {
	return planSummary{
		Slug: s.Slug, Name: s.Name, Approved: s.Approved, Drift: s.Drift,
		Feats: s.Feats, Done: s.Done, Complete: s.Complete,
	}
}

// webFeat is one feat row in the per-plan view, merging the plan's static feat
// metadata with its derived status.
type webFeat struct {
	Slug      string   `json:"slug"`
	Num       string   `json:"num"`
	Objective string   `json:"objective"`
	Milestone string   `json:"milestone"`
	Depends   []string `json:"depends"`
	Parallel  bool     `json:"parallel"`
	State     string   `json:"state"`
	// Refs is the feat's citation tokens, verbatim and in table order. The plan
	// parser already tokenizes them; the UI resolves them through /api/ref rather
	// than re-deriving what a token points at.
	Refs         []string `json:"refs"`
	TasksTotal   int      `json:"tasks_total"`
	TasksChecked int      `json:"tasks_checked"`
}

// milestoneProgress is per-milestone completion, in first-seen order.
type milestoneProgress struct {
	Name  string `json:"name"`
	Total int    `json:"total"`
	Done  int    `json:"done"`
}

// planDetail is GET /api/plan/{slug}: the whole read-only per-plan view. The run
// journal (log.md) is intentionally NOT inlined here — the UI loads it through
// the existing hardened /api/file route so it inherits host-guard + redaction.
type planDetail struct {
	Slug       string              `json:"slug"`
	Name       string              `json:"name"`
	Approved   bool                `json:"approved"`
	Drift      bool                `json:"drift"`
	Complete   bool                `json:"complete"`
	Feats      []webFeat           `json:"feats"`
	Milestones []milestoneProgress `json:"milestones"`
}

// loadPlansList derives a summary for every plan under docs/plans/. A plan that
// fails to load is skipped (the list stays useful) rather than failing the whole
// request.
func loadPlansList(root string) []planSummary {
	sums, err := plan.Summaries(root)
	if err != nil {
		return []planSummary{}
	}
	out := make([]planSummary, 0, len(sums))
	for _, s := range sums {
		out = append(out, summaryRow(s))
	}
	return out
}

// loadPlanDetail assembles the per-plan view. The slug is validated as a single
// path segment first (defense-in-depth against traversal through the read-model).
func loadPlanDetail(root, slug string) (planDetail, error) {
	if err := workspace.SafeName(slug, "plan"); err != nil {
		return planDetail{}, err
	}
	doc, err := plan.Load(root, slug)
	if err != nil {
		return planDetail{}, err
	}
	st, err := plan.DeriveStatus(root, doc)
	if err != nil {
		return planDetail{}, err
	}

	byState := map[string]plan.FeatStatus{}
	for _, f := range st.Feats {
		byState[f.Slug] = f
	}
	d := planDetail{
		Slug: slug, Name: st.Name, Approved: st.Approved,
		Drift: st.Drift,
	}
	milestoneIndex := map[string]int{}
	done := 0
	for _, f := range doc.Feats {
		s := byState[f.Slug]
		refs := f.Refs
		if refs == nil {
			refs = []string{} // marshal as [], never null — the UI maps over it
		}
		d.Feats = append(d.Feats, webFeat{
			Slug: f.Slug, Num: f.Num, Objective: f.Objective, Milestone: f.Milestone,
			Depends: f.Depends, Parallel: f.Parallel, State: s.State, Refs: refs,
			TasksTotal: s.TasksTotal, TasksChecked: s.TasksChecked,
		})
		mi, ok := milestoneIndex[f.Milestone]
		if !ok {
			mi = len(d.Milestones)
			milestoneIndex[f.Milestone] = mi
			d.Milestones = append(d.Milestones, milestoneProgress{Name: f.Milestone})
		}
		d.Milestones[mi].Total++
		if s.State == plan.StateDone {
			d.Milestones[mi].Done++
			done++
		}
	}
	d.Complete = isComplete(done, len(d.Feats))
	return d, nil
}

// planNotFound is the opaque error returned when a plan can't be loaded, so the
// underlying filesystem path never leaks into the response.
func planNotFound(slug string) error {
	return fmt.Errorf("plan not found: %s", slug)
}
