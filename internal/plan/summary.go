package plan

// Summary is one plan's list row: enough to answer "which plans exist and how far
// along is each" without carrying every feat's detail.
type Summary struct {
	Slug     string
	Name     string
	Approved bool
	Drift    bool
	Feats    int
	Done     int
	// Complete: the plan has feats and every one of them is delivered. Derived
	// here rather than by each caller comparing Done against Feats, so the places
	// that show plan state cannot drift apart on what "finished" means. A plan
	// with no feats is not complete — it is unstarted, and saying otherwise would
	// call an empty table finished.
	Complete bool
}

// Summarize collapses a derived PlanStatus into its list row.
func Summarize(st PlanStatus) Summary {
	s := Summary{
		Slug:     st.Slug,
		Name:     st.Name,
		Approved: st.Approved,
		Drift:    st.Drift,
		Feats:    len(st.Feats),
	}
	for _, f := range st.Feats {
		if f.State == StateDone {
			s.Done++
		}
	}
	s.Complete = s.Feats > 0 && s.Done >= s.Feats
	return s
}

// Summaries returns one row per plan under docs/plans/, in slug order (List's
// order). A plan whose plan.md cannot be read, or whose status cannot be derived,
// is skipped rather than failing the whole listing: one broken plan must not hide
// the others. A workspace with no docs/plans/ yields an empty list, not an error.
func Summaries(root string) ([]Summary, error) {
	slugs, err := List(root)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(slugs))
	for _, slug := range slugs {
		doc, err := Load(root, slug)
		if err != nil {
			continue
		}
		st, err := DeriveStatus(root, doc)
		if err != nil {
			continue
		}
		out = append(out, Summarize(st))
	}
	return out, nil
}
