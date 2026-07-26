package plan

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/protonspy/csdd/internal/paths"
)

// Resuming a broken run.
//
// The ledger answers "which feats are delivered", and reconcileLedgerFromDisk
// repairs it from the spec tree, so progress has always survived a crash. What did
// NOT survive is everything runState holds: how many attempts a feat has spent,
// the handoff the last session left, which feats already exhausted their bound.
// All of it lived in memory, so a Ctrl-C between two feats silently reset the
// attempt bound and threw away the handoff — the next session started cold on a
// half-finished feat and re-derived what its predecessor had already written down.
//
// None of that needs a new file. sessions.jsonl already records every attempt with
// its feat, number, status and detail (the handoff IS the detail of a `continue`
// row). It was written on every run and read by nothing. restoreRunState reads it
// back, which is why resume costs a reader rather than a checkpoint format —
// consistent with the principle that state is re-derived, never snapshotted.
//
// What is derived rather than stored:
//
//   - blocked: a feat whose attempts meet the bound and which the ledger does not
//     mark done. Storing it would let a stale flag outlive the fact.
//   - the failure log path: deterministic (failureLogRel), so a stat is enough.
//
// A missing, truncated or unparseable file yields an empty runState — the exact
// behavior every run had before this existed, which is what makes the reader safe
// to add.

// attemptKey identifies one attempt across the record: a `started` row and its
// settling row share it.
type attemptKey struct {
	feat      string
	iteration int
	attempt   int
}

func recKey(r SessionRecord) attemptKey {
	return attemptKey{r.Feat, r.Iteration, r.Attempt}
}

// restoreRunState rebuilds the loop's cross-iteration memory from the append-only
// session record. Rows are folded oldest-first, so the last write for a feat wins
// and a delivered feat's trail is cleared exactly as clearFeat would have.
func restoreRunState(root, slug string, featAttempts int, ledger *Ledger) *runState {
	st := newRunState()
	recs := LoadSessionRecords(root, slug)
	if len(recs) == 0 {
		return st
	}

	// Pass 1: which attempts have an opening row. Records written before `started`
	// existed have none, so pass 2 counts those from the settling row instead —
	// which keeps an older sessions.jsonl fully readable.
	opened := make(map[attemptKey]bool, len(recs))
	for _, r := range recs {
		if r.Status == SessionStarted {
			opened[recKey(r)] = true
		}
	}

	// Pass 2: fold. An attempt is OPEN from its `started` row until a settling row
	// arrives; whatever is still open at the end never settled, meaning the host
	// died mid-session.
	open := map[attemptKey]bool{}
	for _, r := range recs {
		k := recKey(r)
		switch r.Status {
		case SessionStarted:
			open[k] = true
			st.attempts[r.Feat]++
			continue
		case SessionInfra:
			// The session never ran, so it was never an attempt. Settle the row and
			// give the count back — the same bookkeeping executeFeat does live.
			delete(open, k)
			st.undoAttempt(r.Feat)
			continue
		}

		delete(open, k)
		if !opened[k] {
			st.attempts[r.Feat]++
		}

		switch r.Status {
		case SessionContinue:
			st.handoffs[r.Feat] = r.Detail
		case SessionFailed:
			st.hist(r.Feat).add("session from an earlier run (recovered)", r.Detail)
		case SessionDone:
			st.clearFeat(r.Feat)
		}
	}

	// A still-open attempt is a crash. It counts — a feat that reliably kills its
	// host must still exhaust its bound, or the loop retries it forever.
	st.crashed = len(open)

	for feat := range st.attempts {
		if rel := failureLogRel(slug, feat); fileExists(filepath.Join(root, rel)) {
			st.logs[feat] = rel
		}
		if st.attempts[feat] >= featAttempts && !ledger.Done(feat) {
			st.blocked[feat] = true
		}
	}
	return st
}

// resumeSummary describes what a resume recovered, or "" when there was nothing to
// recover. One line, because a resumed run must be visibly distinguishable from a
// fresh one without being noisy about it (R3.5).
func (s *runState) resumeSummary() string {
	if len(s.attempts) == 0 {
		return ""
	}
	attempts := 0
	for _, n := range s.attempts {
		attempts += n
	}
	msg := fmt.Sprintf("resumed: %d attempt(s) across %d unfinished feat(s)", attempts, len(s.attempts))
	if len(s.handoffs) > 0 {
		msg += fmt.Sprintf(", %d handoff(s) recovered", len(s.handoffs))
	}
	if len(s.blocked) > 0 {
		msg += fmt.Sprintf(", %d already blocked", len(s.blocked))
	}
	if s.crashed > 0 {
		msg += fmt.Sprintf(", %d attempt(s) died mid-session and still count", s.crashed)
	}
	return msg
}

// failureLogRel is the workspace-relative failure log path for a feat — the single
// definition, used both by writeFailureLog when it writes one and by the resume
// when it looks for one, so the two cannot drift.
func failureLogRel(slug, feat string) string {
	return filepath.ToSlash(filepath.Join(paths.StateDir, "plan", slug, "failures", feat+".log"))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
