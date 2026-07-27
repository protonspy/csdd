package plan

// The sequencer chooses WHICH feat comes next. The Ralph-style loop hands out a
// whole feat and trusts the session to drive its own lifecycle, so this is the
// loop's only scheduling decision — but it is a real one.
//
// It used to be "the first row the ledger has not marked done", and this file said
// so: "There is no dependency graph — feats run in the order the author wrote
// them." That was safe only because table order happened to be a topological order
// in practice. The graph was always present: `Depends` is parsed (model.go),
// checked for cycles with Tarjan SCC and rejected when it names an unknown or self
// slug (validate.go), and emitted as `depends_on` edges by the graph extractor.
// Every consumer used it except the one that schedules.
//
// readyFeats uses it. Taking the head of the ready set is a strict refinement of
// table order — never a regression — and it is also what the concurrent squad draws
// from (admitFeat), so serial and parallel runs schedule off one derivation.

// Sequencer outcomes, mapped to distinct CLI exit codes (R6.3) so an agent can
// branch on $? without parsing.
const (
	SeqFeat     = iota // 0: a feat is available to work
	_                  // (1,2 reserved: generic/usage errors)
	_                  //
	SeqComplete        // 3: every feat is done
	SeqNotReady        // 5: plan not approved
)

// readyFeats returns every feat that can be worked right now, in the plan's own
// table order so the choice is deterministic and reads the way the author wrote it.
//
// A feat is ready when every slug it depends on is delivered and it is not itself
// delivered or unavailable. `unavailable` carries what the caller must not hand
// out: feats that exhausted their attempt bound, and — once concurrency exists —
// the ones already in flight. `extraDeps` carries edges discovered at run time; see
// depsSatisfied.
//
// Dependencies need no defensive handling here: `plan run` preflights on an
// approval that requires a clean `plan validate`, which rejects unknown slugs,
// self-dependencies, range shorthand and cycles. If that ever changes, this
// becomes unsound.
func readyFeats(doc *PlanDoc, done, unavailable map[string]bool, extraDeps map[string][]string) []Feat {
	var out []Feat
	for _, f := range doc.Feats {
		if done[f.Slug] || unavailable[f.Slug] {
			continue
		}
		if depsSatisfied(f, done, extraDeps) {
			out = append(out, f)
		}
	}
	return out
}

// depsSatisfied reports whether every dependency of f is delivered. extraDeps holds
// edges a session discovered at run time — it reported itself blocked on a peer the
// plan never declared — unioned with the plan's own. They are never written back to
// plan.md, which would trip the approval hash and stop the very run that learned
// them.
func depsSatisfied(f Feat, done map[string]bool, extraDeps map[string][]string) bool {
	for _, dep := range f.Depends {
		if !done[dep] {
			return false
		}
	}
	for _, dep := range extraDeps[f.Slug] {
		if !done[dep] {
			return false
		}
	}
	return true
}

// nextFeat returns the feat to work next: the first ready one in table order. ok is
// false when nothing can be worked — either everything is done, or what remains is
// stranded behind something that is not going to finish.
func nextFeat(doc *PlanDoc, done, unavailable map[string]bool, extraDeps map[string][]string) (Feat, bool) {
	ready := readyFeats(doc, done, unavailable, extraDeps)
	if len(ready) == 0 {
		return Feat{}, false
	}
	return ready[0], true
}

// admitFeat picks the next feat that may START right now, given what is already in
// flight: the head of the ready set, minus the feats a session is already driving.
//
// The dependency graph is the whole rule. It is what makes concurrency safe — two
// feats that are ready at the same moment cannot depend on each other — and once
// every feat works in its own git worktree, there is nothing left for a second rule
// to protect. The (P) column is NOT consulted: it used to be the author's consent to
// share a working tree, and feats no longer share one. It survives as ordering
// intent, which is what the plan template always described it as.
//
// With an empty squad this returns exactly what nextFeat returns, so a serial run
// schedules identically to before.
func admitFeat(doc *PlanDoc, done, unavailable map[string]bool, inflight map[string]Feat, extraDeps map[string][]string) (Feat, bool) {
	busy := unavailable
	if len(inflight) > 0 {
		busy = map[string]bool{}
		for k, v := range unavailable {
			busy[k] = v
		}
		// One feat, one session: handing out a feat another session is already
		// driving would put two agents on one branch and make the attempt bound and
		// the ledger meaningless.
		for slug := range inflight {
			busy[slug] = true
		}
	}
	ready := readyFeats(doc, done, busy, extraDeps)
	if len(ready) == 0 {
		return Feat{}, false
	}
	return ready[0], true
}

// stranded returns the feats that are neither delivered nor workable because
// something they depend on is unavailable or itself stranded, each paired with the
// dependency that stops it.
//
// It exists so a run that stops early can say why. Under the old sequencer a
// blocked feat just left the rest of the table to be handed out, so a feat whose
// dependency had failed was dispatched anyway — into a tree where the thing it
// builds on does not exist — and the run's summary could not tell the difference
// between "finished" and "gave up".
func stranded(doc *PlanDoc, done, unavailable map[string]bool, extraDeps map[string][]string) map[string]string {
	out := map[string]string{}
	// Being stranded is transitive, so iterate to a fixed point: a feat two hops
	// downstream of a blocked one is stranded as surely as its neighbour.
	for {
		grew := false
		for _, f := range doc.Feats {
			if done[f.Slug] || out[f.Slug] != "" {
				continue
			}
			deps := append(append([]string(nil), f.Depends...), extraDeps[f.Slug]...)
			for _, dep := range deps {
				if done[dep] {
					continue
				}
				if !unavailable[dep] && out[dep] == "" {
					continue // merely not done yet — it can still be worked
				}
				out[f.Slug] = dep
				grew = true
				break
			}
		}
		if !grew {
			return out
		}
	}
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
	done := deliveredSet(root, doc, LoadLedger(root, doc.Slug).doneSet())
	f, ok := nextFeat(doc, done, nil, LoadDiscoveredDeps(root, doc.Slug))
	if !ok {
		return Feat{}, SeqComplete, nil
	}
	return f, SeqFeat, nil
}
