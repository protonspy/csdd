package plan

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// installRealHooks fills any nil hook with its production implementation, so tests
// can inject just the seams they care about and get real behavior for the rest (in
// practice tests inject them all). The only subprocess the runner spawns is
// `claude`; everything else — gates, git, spec approvals — happens inside the
// session, which the runner trusts. runModel/runEffort are the run's per-session
// model and effort (captured into the default Session hook): the orchestrating
// session runs on these, while task implementation it delegates to the
// `implementer` sub-agent runs on that agent's own (cheaper) model. Empty values
// mean "inherit the ambient default" — the corresponding flag is simply omitted.
func installRealHooks(h *Hooks, runModel, runEffort string, sess sessionEnv) {
	if h.Now == nil {
		h.Now = time.Now
	}
	if h.ClaudeAvailable == nil {
		h.ClaudeAvailable = func() bool {
			_, err := exec.LookPath("claude")
			return err == nil
		}
	}
	if h.Doctor == nil {
		h.Doctor = func() SandboxReport { return Doctor(DoctorProbes{}) }
	}
	if h.Confirm == nil {
		h.Confirm = stdinConfirm
	}
	if h.Session == nil {
		h.Session = func(feat Feat, brief string, budgetUSD float64) (SessionOutcome, error) {
			return execClaudeSession(feat, brief, budgetUSD, runModel, runEffort, sess)
		}
	}
	if h.Sleep == nil {
		h.Sleep = time.Sleep
	}
}

// sessionEnv carries what the real Session hook needs beyond the feat itself: the
// watchdog's idle budget and the seams the live session view reports through. It is
// passed rather than read from globals so a test can drive a real session end-to-end.
type sessionEnv struct {
	idle time.Duration
	// out is where the live session view is drawn, and tty says whether that
	// destination can take cursor control (a terminal) or must get plain
	// append-only lines (a pipe or a redirected log).
	out io.Writer
	tty bool
	now func() time.Time
}

// verdictSchema is the JSON schema the session must satisfy — the runner's contract
// with the model (§5.6). The loop understands exactly two intents: done|continue.
const verdictSchema = `{"type":"object","required":["status"],` +
	`"properties":{"status":{"enum":["done","continue"]},"summary":{"type":"string"}}}`

// claudeFlags are every `claude` flag the runner relies on, pinned in one place so
// a version drift is a single, reviewable edit (risk register §8).
var claudeFlags = struct {
	print, outputFormat, verbose, jsonSchema, maxBudget, model, effort, bypass string
}{
	print:        "-p",
	outputFormat: "--output-format",
	verbose:      "--verbose",
	jsonSchema:   "--json-schema",
	maxBudget:    "--max-budget-usd",
	model:        "--model",
	effort:       "--effort",
	bypass:       "--dangerously-skip-permissions",
}

// sessionArgs builds the `claude` argument vector for one plan session. It is pure
// (no I/O) so the flag wiring is unit-testable without spawning a subprocess. An
// empty model or effort omits its flag, letting the session inherit the ambient
// default; a non-positive budget omits --max-budget-usd (the account's own limits).
func sessionArgs(brief string, budgetUSD float64, model, effort string) []string {
	args := []string{
		claudeFlags.print, brief,
		// stream-json (not json) so the session reports as it works: each NDJSON
		// event is both a liveness tick for the watchdog and the material for the
		// runner's heartbeat. The verdict still arrives in the final `result` event,
		// in the same envelope parseVerdict already reads. --verbose is what makes
		// the stream emit those per-step events rather than the result alone.
		claudeFlags.outputFormat, "stream-json",
		claudeFlags.verbose,
		claudeFlags.jsonSchema, verdictSchema,
	}
	if strings.TrimSpace(model) != "" {
		args = append(args, claudeFlags.model, model)
	}
	if strings.TrimSpace(effort) != "" {
		args = append(args, claudeFlags.effort, effort)
	}
	// A positive budget pins --max-budget-usd; the default (<=0) leaves it off so
	// the session runs under the Claude account's own limits.
	if budgetUSD > 0 {
		args = append(args, claudeFlags.maxBudget, fmt.Sprintf("%.2f", budgetUSD))
	}
	args = append(args, claudeFlags.bypass)
	return args
}

// execClaudeSession spawns a fresh `claude -p` session for a feat and parses its
// verdict from the JSON output envelope. Every session runs bypass-mode; the
// runner's preflight (sandbox doctor + human accept) is the gate in front of it.
// It streams the session under a progress watchdog rather than waiting on it
// blindly. Previously this buffered the whole run and called cmd.Run(), which had
// no bound at all: a session that stopped making progress hung `plan run` forever,
// silently, and that is the failure this addresses. The watchdog kills a session
// only when it has produced neither output nor CPU for the idle budget, so long
// honest work is never cut short — see watchdog.go.
func execClaudeSession(_ Feat, brief string, budgetUSD float64, model, effort string, sess sessionEnv) (SessionOutcome, error) {
	cmd := exec.Command("claude", sessionArgs(brief, budgetUSD, model, effort)...)

	stream := newSessionStream(sess.now)
	stop := make(chan struct{})
	if sess.out != nil {
		go newReporter(sess.out, sess.tty, sess.now).run(stream, stop)
	}

	stdout, stderr, err := supervised{
		cmd:          cmd,
		idle:         sess.idle,
		onLine:       stream.line,
		lastActivity: func() string { a, _ := stream.snapshot(); return a },
	}.run()
	close(stop)

	// The `result` event carries both the verdict and what the session cost, so
	// read the metrics ONCE here and attach them to every return path below. A
	// session that failed still spent time and money, and R9.2 wants that attempt
	// recorded — losing the cost of the runs that went wrong would bias every
	// comparison toward the runs that went right.
	src, haveResult := stream.verdictSource()
	out := SessionOutcome{}
	if haveResult {
		out.Metrics = parseSessionMetrics([]byte(src))
	}

	// An account-limit stop is not a work failure: surface it as a typed error so
	// the runner sleeps until the window reopens and retries, rather than counting
	// it against the stall guard. The notice can land on either stream, so scan
	// both regardless of the exit code.
	if lim, ok := detectLimit(stdout + "\n" + stderr); ok {
		return out, lim
	}
	if err != nil {
		return out, fmt.Errorf("claude session failed: %v: %s", err, strings.TrimSpace(stderr))
	}
	// The verdict rides in the final `result` event; fall back to the whole stream
	// so a stream that ended unusually still gets parseVerdict's last-ditch scan.
	raw := stdout
	if haveResult {
		raw = src
	}
	v, err := parseVerdict([]byte(raw))
	out.Verdict = v
	return out, err
}

// LimitError signals the claude session stopped because the Claude account hit
// its session/usage limit rather than because the work failed. It carries the
// reset moment-of-day parsed from the notice ("resets 10:10pm (America/Sao_Paulo)")
// so the runner can sleep until the window reopens instead of treating the stop as
// a failure (waitForLimit).
type LimitError struct {
	Raw    string         // the limit line, verbatim, for logs
	Hour   int            // reset hour, 24h ("10:10pm" -> 22)
	Minute int            // reset minute
	Loc    *time.Location // reset timezone; nil means the runner's local zone
	known  bool           // a reset time-of-day was parsed
}

func (e *LimitError) Error() string { return "claude session limit reached: " + e.Raw }

// Reset resolves the next absolute reset moment relative to now: the parsed
// time-of-day, today if still ahead of now, else tomorrow. ok is false when the
// notice carried no parsable reset time, so the caller falls back to a fixed wait.
func (e *LimitError) Reset(now time.Time) (time.Time, bool) {
	if !e.known {
		return time.Time{}, false
	}
	loc := e.Loc
	if loc == nil {
		loc = now.Location()
	}
	n := now.In(loc)
	r := time.Date(n.Year(), n.Month(), n.Day(), e.Hour, e.Minute, 0, 0, loc)
	if !r.After(n) {
		r = r.Add(24 * time.Hour)
	}
	return r, true
}

var (
	reSessionLimit = regexp.MustCompile(`(?i)you'?ve hit your (?:session|usage|account) limit`)
	reLimitReset   = regexp.MustCompile(`(?i)resets?\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?(?:\s*\(([^)]+)\))?`)
)

// detectLimit reports whether text is a Claude account-limit notice and, if so,
// returns a LimitError with the reset moment parsed from a trailing
// "resets 10:10pm (America/Sao_Paulo)" clause when one is present. A notice with
// no parsable reset still returns a LimitError (known == false) so the runner
// pauses on a fallback wait rather than crashing the loop.
func detectLimit(text string) (*LimitError, bool) {
	loc := reSessionLimit.FindStringIndex(text)
	if loc == nil {
		return nil, false
	}
	e := &LimitError{Raw: limitLine(text, loc[0])}
	if m := reLimitReset.FindStringSubmatch(text); m != nil {
		if h, err := strconv.Atoi(m[1]); err == nil {
			min := 0
			if m[2] != "" {
				min, _ = strconv.Atoi(m[2])
			}
			switch strings.ToLower(m[3]) {
			case "pm":
				if h != 12 {
					h += 12
				}
			case "am":
				if h == 12 {
					h = 0
				}
			}
			if h >= 0 && h < 24 && min >= 0 && min < 60 {
				e.Hour, e.Minute, e.known = h, min, true
			}
		}
		if tz := strings.TrimSpace(m[4]); tz != "" {
			if l, err := time.LoadLocation(tz); err == nil {
				e.Loc = l
			}
		}
	}
	return e, true
}

// limitLine returns the single line of text containing byte offset at, trimmed —
// the human-readable limit notice for logs.
func limitLine(text string, at int) string {
	start := strings.LastIndexByte(text[:at], '\n') + 1
	if end := strings.IndexByte(text[at:], '\n'); end >= 0 {
		return strings.TrimSpace(text[start : at+end])
	}
	return strings.TrimSpace(text[start:])
}

var reJSONObject = regexp.MustCompile(`(?s)\{.*\}`)

// parseVerdict extracts the Verdict from a claude JSON output. It first tries the
// `result` field of the standard envelope, then falls back to the last JSON object
// carrying a "status" field, so a plain-JSON or enveloped response both parse.
func parseVerdict(output []byte) (Verdict, error) {
	// Direct verdict object.
	var v Verdict
	if json.Unmarshal(output, &v) == nil && v.Status != "" {
		return normalizeVerdict(v)
	}
	// Standard `--output-format json` envelope: {"result":"<text or json>", ...}.
	var env struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(output, &env) == nil && env.Result != "" {
		if json.Unmarshal([]byte(env.Result), &v) == nil && v.Status != "" {
			return normalizeVerdict(v)
		}
		if m := reJSONObject.FindString(env.Result); m != "" {
			if json.Unmarshal([]byte(m), &v) == nil && v.Status != "" {
				return normalizeVerdict(v)
			}
		}
	}
	// Last-ditch: any embedded JSON object with a status.
	if m := reJSONObject.FindString(string(output)); m != "" {
		if json.Unmarshal([]byte(m), &v) == nil && v.Status != "" {
			return normalizeVerdict(v)
		}
	}
	return Verdict{}, fmt.Errorf("could not parse a verdict from the session output")
}

func normalizeVerdict(v Verdict) (Verdict, error) {
	v.Status = strings.ToLower(strings.TrimSpace(v.Status))
	switch v.Status {
	case VerdictDone, VerdictContinue:
		return v, nil
	}
	return Verdict{}, fmt.Errorf("invalid verdict status %q (want done|continue)", v.Status)
}

// stdinConfirm prompts on stderr and reads one line from stdin; only an explicit
// "y"/"yes" accepts. A non-interactive stdin (EOF) therefore declines, which is the
// safe default for the unverified-sandbox alert.
func stdinConfirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
