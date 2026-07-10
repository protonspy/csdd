package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// The ledger is the loop's record of which feats the session has declared done —
// the csdd analog of Ralph's prd.json `passes: true`. Because the loop trusts the
// session (there is no runner-side gate), the ledger, not the disk-derived feat
// state, is the source of truth for advancement: the sequencer hands out the first
// feat the ledger does not yet mark done, and the run completes when every feat is
// marked. It lives under the transient state dir, so it is regenerable per run;
// the durable, reviewable half of the same record is the journal (log.md).
const ledgerSchemaVersion = 1

// Ledger is the parsed progress.json for one plan run.
type Ledger struct {
	SchemaVersion int                   `json:"schema_version,omitempty"`
	Feats         map[string]LedgerFeat `json:"feats"`
}

// LedgerFeat records that the session declared a feat delivered, with the handoff
// summary it returned and when.
type LedgerFeat struct {
	Done    bool   `json:"done"`
	Summary string `json:"summary,omitempty"`
	DoneAt  string `json:"done_at,omitempty"`
}

func ledgerPath(root, slug string) string {
	return filepath.Join(stateDir(root, slug), "progress.json")
}

// LoadLedger reads the ledger for a plan. A missing or unparseable file yields an
// empty ledger — a fresh run starts with nothing done, which is always safe.
func LoadLedger(root, slug string) *Ledger {
	l := &Ledger{SchemaVersion: ledgerSchemaVersion, Feats: map[string]LedgerFeat{}}
	data, err := os.ReadFile(ledgerPath(root, slug))
	if err != nil {
		return l
	}
	var parsed Ledger
	if json.Unmarshal(data, &parsed) != nil {
		return l
	}
	if parsed.Feats == nil {
		parsed.Feats = map[string]LedgerFeat{}
	}
	parsed.SchemaVersion = ledgerSchemaVersion
	return &parsed
}

// Save persists the ledger, creating the state dir if needed.
func (l *Ledger) Save(root, slug string) error {
	if err := os.MkdirAll(stateDir(root, slug), 0o755); err != nil {
		return err
	}
	l.SchemaVersion = ledgerSchemaVersion
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ledgerPath(root, slug), append(data, '\n'), 0o644)
}

// MarkDone records that a feat is delivered.
func (l *Ledger) MarkDone(feat, summary string, now time.Time) {
	if l.Feats == nil {
		l.Feats = map[string]LedgerFeat{}
	}
	l.Feats[feat] = LedgerFeat{Done: true, Summary: summary, DoneAt: now.UTC().Format(time.RFC3339)}
}

// Done reports whether a feat is marked delivered.
func (l *Ledger) Done(feat string) bool {
	return l.Feats[feat].Done
}

// doneSet returns the set of feats marked delivered, for the sequencer.
func (l *Ledger) doneSet() map[string]bool {
	out := make(map[string]bool, len(l.Feats))
	for slug, f := range l.Feats {
		if f.Done {
			out[slug] = true
		}
	}
	return out
}
