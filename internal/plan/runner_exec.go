package plan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// installRealHooks fills any nil hook with its production implementation, so tests
// can inject just the seams they care about and get real behavior for the rest (in
// practice tests inject them all). The only subprocess the runner spawns is
// `claude`; everything else — gates, git, spec approvals — happens inside the
// session, which the runner trusts.
func installRealHooks(h *Hooks) {
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
		h.Session = execClaudeSession
	}
}

// verdictSchema is the JSON schema the session must satisfy — the runner's contract
// with the model (§5.6). The loop understands exactly two intents: done|continue.
const verdictSchema = `{"type":"object","required":["status"],` +
	`"properties":{"status":{"enum":["done","continue"]},"summary":{"type":"string"}}}`

// claudeFlags are every `claude` flag the runner relies on, pinned in one place so
// a version drift is a single, reviewable edit (risk register §8).
var claudeFlags = struct {
	print, outputFormat, jsonSchema, maxBudget, bypass string
}{
	print:        "-p",
	outputFormat: "--output-format",
	jsonSchema:   "--json-schema",
	maxBudget:    "--max-budget-usd",
	bypass:       "--dangerously-skip-permissions",
}

// execClaudeSession spawns a fresh `claude -p` session for a feat and parses its
// verdict from the JSON output envelope. Every session runs bypass-mode; the
// runner's preflight (sandbox doctor + human accept) is the gate in front of it.
func execClaudeSession(_ Feat, brief string, budgetUSD float64) (Verdict, error) {
	args := []string{
		claudeFlags.print, brief,
		claudeFlags.outputFormat, "json",
		claudeFlags.jsonSchema, verdictSchema,
	}
	// A positive budget pins --max-budget-usd; the default (<=0) leaves it off so
	// the session runs under the Claude account's own limits.
	if budgetUSD > 0 {
		args = append(args, claudeFlags.maxBudget, fmt.Sprintf("%.2f", budgetUSD))
	}
	args = append(args, claudeFlags.bypass)
	cmd := exec.Command("claude", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Verdict{}, fmt.Errorf("claude session failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseVerdict(stdout.Bytes())
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
