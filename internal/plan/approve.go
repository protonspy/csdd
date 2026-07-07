package plan

import (
	"time"
)

// ApprovePlan writes plan.json binding an approval to the exact current content
// of plan.md + seeds/** (R4.1). It assumes validation has already passed (the CLI
// runs ValidatePlan and refuses on findings first); this function is purely the
// mechanical state write — the plan analog of SpecApprove's final step, and the
// only place a plan is marked approved (design principle 3). now is injected so
// callers and tests stay deterministic where they need to be.
func ApprovePlan(root string, doc *PlanDoc, now time.Time) error {
	dir := Dir(root, doc.Slug)
	h, err := HashPlan(dir)
	if err != nil {
		return err
	}
	name := doc.Name
	if name == "" {
		name = doc.Slug
	}
	return SavePlanJSON(dir, PlanJSON{
		Name: name,
		Approvals: PlanApproval{
			Approved:    true,
			ContentHash: h,
			ApprovedAt:  now.UTC().Format("2006-01-02T15:04:05Z"),
		},
	})
}

// IsApproved reports a plan's approval state and whether its content has drifted
// since approval (R4.2). Drift is a hash mismatch between the stored approval and
// the current plan.md + seeds/**; an unapproved plan is (false, false).
// next/run consult this to refuse autonomous execution on drift.
func IsApproved(root, slug string) (approved, drift bool, err error) {
	dir := Dir(root, slug)
	pj, ok, err := LoadPlanJSON(dir)
	if err != nil || !ok || !pj.Approvals.Approved {
		return false, false, err
	}
	if pj.Approvals.ContentHash == "" {
		return true, false, nil // approved by an older writer with no hash — trust it
	}
	h, herr := HashPlan(dir)
	if herr != nil {
		return true, false, herr
	}
	return true, h != pj.Approvals.ContentHash, nil
}
