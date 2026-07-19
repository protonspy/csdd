package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The ledger is the loop's record of which feats the session has declared done —
// the csdd analog of Ralph's prd.json `passes: true`. A feat lands here only once
// its `done` verdict has cleared the verdict gate (R10), and from then on the
// ledger, not the disk-derived feat state, is the source of truth for advancement: the sequencer hands out the first
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

// --- per-session records (R9) --------------------------------------------------

// SessionsFile is the append-only record of every session attempt a plan has
// spent, one JSON object per line, beside the ledger in the runner's state dir.
const SessionsFile = "sessions.jsonl"

// SessionRecord is one session attempt: what it was working, how it ended, and
// what it cost. Every attempt gets a row — `done`, `continue`, and failures alike
// (R9.2) — because the attempts that did NOT deliver are exactly the ones an
// optimization is trying to remove, and a log of only the successes would hide
// them.
//
// It is JSONL rather than a field on the ledger for two reasons: the file is
// append-only, so two writes can never lose each other the way a read-modify-write
// of progress.json can, and it grows without bloating the document the sequencer
// reads on every iteration.
type SessionRecord struct {
	Feat      string `json:"feat"`
	Iteration int    `json:"iteration"`        // the run's session counter
	Attempt   int    `json:"attempt"`          // this feat's attempt number (R10.4)
	Status    string `json:"status"`           // done | continue | failed
	Detail    string `json:"detail,omitempty"` // the handoff, the summary, or the error
	At        string `json:"at"`               // RFC3339 UTC
	Gated     bool   `json:"gated,omitempty"`  // a `done` the verdict gate converted (R10.3)

	DurationMS    int64         `json:"duration_ms,omitempty"`
	APIDurationMS int64         `json:"api_duration_ms,omitempty"`
	CostUSD       float64       `json:"cost_usd,omitempty"`
	NumTurns      int           `json:"num_turns,omitempty"`
	Tokens        SessionTokens `json:"tokens"`
	Models        []string      `json:"models,omitempty"`
}

// newSessionRecord stamps one attempt's outcome and cost into a record.
func newSessionRecord(feat string, iter, attempt int, status, detail string, m SessionMetrics, now time.Time) SessionRecord {
	return SessionRecord{
		Feat:          feat,
		Iteration:     iter,
		Attempt:       attempt,
		Status:        status,
		Detail:        detail,
		At:            now.UTC().Format(time.RFC3339),
		DurationMS:    m.Duration.Milliseconds(),
		APIDurationMS: m.APIDuration.Milliseconds(),
		CostUSD:       m.CostUSD,
		NumTurns:      m.NumTurns,
		Tokens:        m.Tokens,
		Models:        m.Models,
	}
}

func sessionsPath(root, slug string) string {
	return filepath.Join(stateDir(root, slug), SessionsFile)
}

// AppendSessionRecord persists one attempt. Errors are returned but callers treat
// them as non-fatal: instrumentation that can end a run is worse than no
// instrumentation.
func AppendSessionRecord(root, slug string, rec SessionRecord) error {
	if err := os.MkdirAll(stateDir(root, slug), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(sessionsPath(root, slug), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(line, '\n'))
	return err
}

// LoadSessionRecords reads every recorded attempt, oldest first. A missing file
// yields no records; an unparseable line is skipped rather than failing the read,
// so one truncated write (a killed run) never hides the rest of the history. This
// is what makes a feat's run comparable against an earlier one (R9.3).
func LoadSessionRecords(root, slug string) []SessionRecord {
	data, err := os.ReadFile(sessionsPath(root, slug))
	if err != nil {
		return nil
	}
	var out []SessionRecord
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec SessionRecord
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}
