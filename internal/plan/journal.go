package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The journal (docs/plans/<slug>/log.md) is the durable, reviewable record of a
// run: one mechanical line per event, human-readable, committed with the plan. It
// is the audit half of the transient state dir — the ledger (progress.json) drives
// the loop, the journal explains it. Every writer goes through appendJournal so the
// grammar stays in one place.

// appendJournal writes one `## [YYYY-MM-DD] <step> | <feat> | <outcome>` entry to
// the run journal, plus an optional `- <detail>` line per detail (§5.6). The step
// column is a historical artifact of the old micro-step loop; whole-feat runs pass
// "-".
func appendJournal(dir string, now time.Time, step, feat, outcome string, details ...string) {
	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] %s | %s | %s\n", now.UTC().Format("2006-01-02"), orDash(step), feat, outcome)
	for _, d := range details {
		fmt.Fprintf(&b, "- %s\n", d)
	}
	appendFile(filepath.Join(dir, "log.md"), b.String())
}

// journalMu serializes journal appends. A squad run journals from several session
// goroutines at once (an account-limit pause, a failed spawn), and while O_APPEND
// makes a single small write atomic on POSIX it carries no such guarantee on
// Windows, a first-class target here. The lock costs nothing and removes the
// question — the same reasoning as sessionsMu.
var journalMu sync.Mutex

// appendFile appends content to a file, creating the parent directory if needed.
// Errors are swallowed: a lost journal line must never take a run down with it.
func appendFile(path, content string) {
	journalMu.Lock()
	defer journalMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(content)
}
