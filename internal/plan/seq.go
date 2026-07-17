package plan

// The sequencer used to break each feat into micro-steps (requirements → design →
// tasks → task N) and hand one step at a time. The Ralph-style loop hands the whole
// feat instead and trusts the session to drive its own lifecycle, so all that is
// left here is choosing WHICH feat comes next: the first feat, in the plan's own
// table order, that the ledger has not yet marked done. There is no dependency
// graph — feats run in the order the author wrote them (the plan is the ordering).

// Sequencer outcomes, mapped to distinct CLI exit codes (R6.3) so an agent can
// branch on $? without parsing.
const (
	SeqFeat     = iota // 0: a feat is available to work
	_                  // (1,2 reserved: generic/usage errors)
	_                  //
	SeqComplete        // 3: every feat is done
	SeqNotReady        // 5: plan not approved
)

// nextFeat returns the next feat to work: the first feat in table order that the
// done set does not already contain. ok is false when every feat is done.
func nextFeat(doc *PlanDoc, done map[string]bool) (Feat, bool) {
	for _, f := range doc.Feats {
		if !done[f.Slug] {
			return f, true
		}
	}
	return Feat{}, false
}

// NextFeat is the CLI-facing sequencer (`plan next`): it reports the next feat to
// work, respecting approval when requireApproved is set. It reads the ledger for
// the done set so it agrees with what the runner would pick.
func NextFeat(root string, doc *PlanDoc, requireApproved bool) (Feat, int, error) {
	if requireApproved {
		approved, drift, err := IsApproved(root, doc.Slug)
		if err != nil {
			return Feat{}, SeqNotReady, err
		}
		// Match `plan run`'s preflight: an unapproved OR drifted plan is not ready.
		if !approved || drift {
			return Feat{}, SeqNotReady, nil
		}
	}
	// The done set unions the ledger with disk-delivered feats so `plan next` agrees
	// with a resumed `plan run` (which reconciles the same disk state into its ledger)
	// — a plan advanced by hand still resumes from where disk reality left off.
	f, ok := nextFeat(doc, deliveredSet(root, doc, LoadLedger(root, doc.Slug).doneSet()))
	if !ok {
		return Feat{}, SeqComplete, nil
	}
	return f, SeqFeat, nil
}
