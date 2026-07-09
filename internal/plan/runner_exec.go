package plan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// installRealHooks fills any nil hook with its production implementation, so
// tests can inject just the seams they care about and get real behavior for the
// rest (in practice tests inject them all). The real hooks shell out to `claude`,
// `csdd`, and the shell — the runner's process performs every gate and approval,
// standing in for the human; the session authors and owns git (principle 3).
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
	if h.CSDD == nil {
		h.CSDD = execCSDD
	}
	if h.Gate == nil {
		h.Gate = execShellGate
	}
}

// verdictSchema is the JSON schema the session must satisfy — the runner's
// contract with the model (§5.6). Pinned here alongside the flags in one place.
const verdictSchema = `{"type":"object","required":["status","summary"],` +
	`"properties":{"status":{"enum":["done","progress","halt"]},"summary":{"type":"string"},` +
	`"decisions":{"type":"array","items":{"type":"string"}}}}`

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

// execClaudeSession spawns a fresh `claude -p` session for a step and parses its
// verdict from the JSON output envelope. Every session runs bypass-mode; the
// runner's preflight (sandbox doctor + human accept) is the gate in front of it.
func execClaudeSession(step Step, brief string, budgetUSD float64) (Verdict, error) {
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
	case VerdictDone, VerdictProgress, VerdictHalt, VerdictBlocked:
		return v, nil // blocked is legacy: parsed, then coached into a decision
	}
	return Verdict{}, fmt.Errorf("invalid verdict status %q (want done|progress|halt)", v.Status)
}

// csddBinary is the path to the csdd binary the runner shells out to. It prefers
// the currently-running executable so `plan run` uses the same version for the
// gates and approvals it drives.
func csddBinary() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return "csdd"
}

// execCSDD runs a csdd subcommand in root, returning success and combined output.
func execCSDD(root string, args ...string) (bool, string) {
	full := append([]string{"--root", root}, args...)
	cmd := exec.Command(csddBinary(), full...)
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

// stdinConfirm prompts on stderr and reads one line from stdin; only an explicit
// "y"/"yes" accepts. A non-interactive stdin (EOF) therefore declines, which is
// the safe default for the unverified-sandbox alert.
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

// execShellGate runs a gate command through the platform shell in root.
func execShellGate(root, command string) (bool, string) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}
