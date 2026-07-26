// Package validator implements the mechanical checks the Claude Code spec mandates:
// EARS phrasing, unique requirement IDs, traceability coverage, task
// annotation hygiene (_Requirements:_, _Boundary:_, _Depends:_), parallel
// safety, and skill structure. The checks here mirror the Python reference
// implementation 1:1 so the Go binary can replace it without behavior drift.
package validator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/textutil"
)

var (
	reqHeader = regexp.MustCompile(`(?m)^###\s+Requirement\s+(\d+)\b`)
	// criterionLine matches a numbered list item; group 1 is the list number and
	// group 2 the remaining text. Whether it counts as an acceptance criterion is
	// decided by earsLine on group 2, so extraction and the EARS check agree.
	criterionLine   = regexp.MustCompile(`^\s*(\d+)\.\s+(.*)$`)
	earsLine        = regexp.MustCompile(`(?i)\b(WHEN|WHILE|IF|WHERE)\b.*\bTHE\s+SYSTEM\s+SHALL\b|\bTHE\s+SYSTEM\s+SHALL\b`)
	shouldWord      = regexp.MustCompile(`(?i)\bshould\b`)
	numberedLine    = regexp.MustCompile(`^\d+\.\s`)
	componentHeader = regexp.MustCompile(`(?m)^###\s+([A-Za-z0-9_\-]+)\s*$`)
	componentsHead  = regexp.MustCompile(`(?m)^##\s+Components and Interfaces\s*$`)
	nextH2          = regexp.MustCompile(`(?m)^##\s`)
	traceabilityRow = regexp.MustCompile(`(?m)^\|\s*(\d+\.\d+)\s*\|`)
	numericID       = regexp.MustCompile(`^\d+\.\d+$`)

	// Canonical task grammar. It is exported and reused by internal/session so the
	// validator (correctness) and the dashboard (display) can never disagree on
	// what a task line or annotation is. Group indices: 1=indent, 2=checkbox
	// state, 3=id (any depth: 1, 1.2, 1.2.3), 4=title. Boundary names share the
	// component-header charset — a boundary must name a real component.
	TaskLineRe      = regexp.MustCompile(`^(\s*)-\s+\[\s*([xX ]?)\s*\]\s+(\d+(?:\.\d+)*)\.?\s+(.*)$`)
	ReqAnnotRe      = regexp.MustCompile(`_Requirements:\s*([\d,\.\s]+)_`)
	BoundaryAnnotRe = regexp.MustCompile(`_Boundary:\s*([A-Za-z0-9_\-]+)_`)
	DependsAnnotRe  = regexp.MustCompile(`_Depends:\s*([\d,\.\s]+)_`)
	ParallelRe      = regexp.MustCompile(`\(P\)`)

	// Development-flow task shape. tasks-generation names the pair explicitly
	// ("2.1 RED — …", "2.2 GREEN — …"), so the shape is readable from the title
	// alone. A single sub-task may carry both halves ("4.3 RED→GREEN — …"): it
	// opens the pair and closes it in one step, so it counts as each.
	taskRedRe      = regexp.MustCompile(`(?i)^RED\b`)
	taskGreenRe    = regexp.MustCompile(`(?i)^GREEN\b`)
	taskRedGreenRe = regexp.MustCompile(`(?i)^RED\b.*\bGREEN\b`)
)

// MaskCodeFences is the exported entry point to fence masking, shared with
// internal/session so the dashboard's task stats ignore fenced examples exactly
// as the validator's checks do.
func MaskCodeFences(text string) string { return maskCodeFences(text) }

// maskCodeFences blanks every line inside a fenced code block (``` or ~~~) while
// preserving the total line count, so line-oriented regexes never mistake a
// fenced example ("- [ ] 1. …" in a doc, a numbered list in prose) for a real
// task or acceptance criterion. Line numbers in reported issues stay accurate.
func maskCodeFences(text string) string {
	lines := strings.Split(text, "\n")
	inFence := false
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = true
				fence = trimmed[:3]
				lines[i] = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, fence) {
			inFence = false
		}
		lines[i] = ""
	}
	return strings.Join(lines, "\n")
}

// Issue represents a single validation problem.
type Issue struct {
	File string
	Line int // 0 when not line-attributable
	Msg  string
}

// String renders an issue in the same shape the Python CLI used: "<file>:<line>: msg" or "<file>: msg".
func (i Issue) String() string {
	if i.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", i.File, i.Line, i.Msg)
	}
	return fmt.Sprintf("%s: %s", i.File, i.Msg)
}

// Phase narrows the validator's scope. An empty string runs every available check.
type Phase string

const (
	PhaseRequirements Phase = "requirements"
	PhaseDesign       Phase = "design"
	PhaseTasks        Phase = "tasks"
	PhaseAll          Phase = ""
)

type taskRecord struct {
	id           string
	title        string
	lineNo       int
	parallel     bool
	boundary     string
	requirements []string
	depends      []string
}

// ValidateSpec runs the mechanical checks for a single feature spec directory.
// The phase argument restricts which artifacts are evaluated; PhaseAll runs
// every check that the present files permit.
func ValidateSpec(specDir string, phase Phase) []Issue {
	var issues []Issue

	reqPath := filepath.Join(specDir, "requirements.md")
	designPath := filepath.Join(specDir, "design.md")
	tasksPath := filepath.Join(specDir, "tasks.md")
	bugfixPath := filepath.Join(specDir, "bugfix.md")

	wantReq := phase == PhaseAll || phase == PhaseRequirements || phase == PhaseDesign || phase == PhaseTasks
	wantDesign := phase == PhaseAll || phase == PhaseDesign || phase == PhaseTasks
	wantTasks := phase == PhaseAll || phase == PhaseTasks

	var reqIDs map[string]struct{}
	var components map[string]struct{}
	var traceability map[string]struct{}

	// Requirements
	if wantReq {
		if raw, err := os.ReadFile(reqPath); err == nil {
			text := maskCodeFences(textutil.NormalizeNewlines(string(raw)))
			reqIDs = extractRequirementIDs(text)
			issues = append(issues, checkEARS("requirements.md", text)...)
			headers := reqHeader.FindAllStringSubmatch(text, -1)
			seen := map[string]int{}
			for _, h := range headers {
				seen[h[1]]++
			}
			for k, v := range seen {
				if v > 1 {
					issues = append(issues, Issue{File: "requirements.md", Msg: fmt.Sprintf("duplicate Requirement %s header", k)})
				}
			}
			issues = append(issues, checkDuplicateCriterionIDs(text)...)
		} else if phase != PhaseAll {
			if _, err := os.Stat(bugfixPath); err != nil {
				issues = append(issues, Issue{File: "requirements.md", Msg: "missing (or bugfix.md)"})
			}
		}
	}

	// Bugfix
	if data, err := os.ReadFile(bugfixPath); err == nil {
		text := textutil.NormalizeNewlines(string(data))
		for _, needle := range []string{"Current Behavior:", "Expected Behavior:", "Unchanged Behavior:"} {
			if !strings.Contains(text, needle) {
				issues = append(issues, Issue{File: "bugfix.md", Msg: "missing '" + needle + "' section"})
			}
		}
		if !strings.Contains(text, "Root Cause") {
			issues = append(issues, Issue{File: "bugfix.md", Msg: "missing Root Cause section"})
		}
	}

	// Design
	if wantDesign {
		if data, err := os.ReadFile(designPath); err == nil {
			text := textutil.NormalizeNewlines(string(data))
			masked := maskCodeFences(text)
			lineCount := strings.Count(text, "\n") + 1
			if lineCount > 1000 {
				issues = append(issues, Issue{
					File: "design.md",
					Msg:  fmt.Sprintf("%d lines > 1000 — split the feature into multiple specs", lineCount),
				})
			}
			components = extractComponents(masked)
			traceability = extractTraceability(masked)
			if !strings.Contains(text, "## File Structure Plan") {
				issues = append(issues, Issue{File: "design.md", Msg: "missing '## File Structure Plan' section"})
			}
			if !strings.Contains(text, "## Architecture Pattern & Boundary Map") {
				issues = append(issues, Issue{File: "design.md", Msg: "missing '## Architecture Pattern & Boundary Map' section"})
			}
			if len(reqIDs) > 0 {
				var missing []string
				for id := range reqIDs {
					if _, ok := traceability[id]; !ok {
						missing = append(missing, id)
					}
				}
				if len(missing) > 0 {
					issues = append(issues, Issue{
						File: "design.md",
						Msg:  "Requirements Traceability table missing IDs: " + sortedJoin(missing),
					})
				}
			}
		} else if phase == PhaseDesign || phase == PhaseTasks {
			issues = append(issues, Issue{File: "design.md", Msg: "missing"})
		}
	}

	// Tasks
	if wantTasks {
		if data, err := os.ReadFile(tasksPath); err == nil {
			tasks := parseTasks(maskCodeFences(textutil.NormalizeNewlines(string(data))))
			ids := map[string]struct{}{}
			boundaries := map[string]string{}
			firstLine := map[string]int{}
			for _, t := range tasks {
				if prev, ok := firstLine[t.id]; ok {
					issues = append(issues, Issue{
						File: "tasks.md",
						Line: t.lineNo,
						Msg:  fmt.Sprintf("duplicate task ID %s (first seen on line %d)", t.id, prev),
					})
				} else {
					firstLine[t.id] = t.lineNo
				}
				ids[t.id] = struct{}{}
				boundaries[t.id] = t.boundary
			}
			// Leaves: no other task has this ID as a numeric prefix.
			leaves := map[string]struct{}{}
			for _, t := range tasks {
				isLeaf := true
				for id := range ids {
					if id != t.id && strings.HasPrefix(id, t.id+".") {
						isLeaf = false
						break
					}
				}
				if isLeaf {
					leaves[t.id] = struct{}{}
				}
			}
			for _, t := range tasks {
				if _, leaf := leaves[t.id]; leaf && len(t.requirements) == 0 {
					issues = append(issues, Issue{
						File: "tasks.md",
						Line: t.lineNo,
						Msg:  fmt.Sprintf("leaf task %s missing _Requirements:_", t.id),
					})
				}
				for _, rid := range t.requirements {
					if !numericID.MatchString(rid) {
						issues = append(issues, Issue{
							File: "tasks.md",
							Line: t.lineNo,
							Msg:  fmt.Sprintf("task %s _Requirements:_ contains non-numeric token '%s' (only IDs like '1.2' are allowed)", t.id, rid),
						})
						continue
					}
					if len(reqIDs) > 0 {
						if _, ok := reqIDs[rid]; !ok {
							issues = append(issues, Issue{
								File: "tasks.md",
								Line: t.lineNo,
								Msg:  fmt.Sprintf("task %s references unknown requirement '%s'", t.id, rid),
							})
						}
					}
				}
				if t.parallel && t.boundary == "" {
					issues = append(issues, Issue{
						File: "tasks.md",
						Line: t.lineNo,
						Msg:  fmt.Sprintf("parallel task %s missing _Boundary:_", t.id),
					})
				}
				if t.boundary != "" && len(components) > 0 {
					if _, ok := components[t.boundary]; !ok {
						issues = append(issues, Issue{
							File: "tasks.md",
							Line: t.lineNo,
							Msg:  fmt.Sprintf("task %s _Boundary: %s_ does not match any component in design.md", t.id, t.boundary),
						})
					}
				}
				for _, dep := range t.depends {
					if _, ok := ids[dep]; !ok {
						issues = append(issues, Issue{
							File: "tasks.md",
							Line: t.lineNo,
							Msg:  fmt.Sprintf("task %s _Depends:_ references unknown task '%s'", t.id, dep),
						})
						continue
					}
					if t.boundary != "" && boundaries[dep] != "" && t.boundary == boundaries[dep] {
						issues = append(issues, Issue{
							File: "tasks.md",
							Line: t.lineNo,
							Msg:  fmt.Sprintf("task %s _Depends:_ references same-boundary task '%s'; sequential dependencies inside a boundary are implicit", t.id, dep),
						})
					}
				}
			}
			seen := map[string]string{}
			for _, t := range tasks {
				if !t.parallel || t.boundary == "" {
					continue
				}
				if prev, ok := seen[t.boundary]; ok {
					issues = append(issues, Issue{
						File: "tasks.md",
						Msg:  fmt.Sprintf("parallel tasks %s and %s share boundary '%s' — parallel safety violated", prev, t.id, t.boundary),
					})
				} else {
					seen[t.boundary] = t.id
				}
			}
			issues = append(issues, checkDevelopmentFlow(specDevelopmentFlow(specDir), tasks)...)
		} else if phase == PhaseTasks {
			issues = append(issues, Issue{File: "tasks.md", Msg: "missing"})
		}
	}

	return issues
}

// scannedCriterion is one acceptance criterion discovered by scanCriteria.
type scannedCriterion struct {
	id     string // "<reqNum>.<listNum>"
	lineNo int    // 1-based line in the (masked) text
}

// scanCriteria walks the text line by line, tracking the current Requirement
// header, and returns every numbered list item that carries EARS structure. Both
// ID extraction and duplicate detection use this single pass, so a criterion the
// EARS check accepts always yields exactly one ID (no anchoring mismatch), an
// incidental numbered note (no EARS keywords) is never mistaken for a criterion,
// and reported line numbers point at the criterion itself.
func scanCriteria(text string) []scannedCriterion {
	var out []scannedCriterion
	curReq := ""
	for i, line := range strings.Split(text, "\n") {
		if hm := reqHeader.FindStringSubmatch(line); hm != nil {
			curReq = hm[1]
			continue
		}
		if curReq == "" {
			continue
		}
		m := criterionLine.FindStringSubmatch(line)
		if m == nil || !earsLine.MatchString(m[2]) {
			continue
		}
		out = append(out, scannedCriterion{id: curReq + "." + m[1], lineNo: i + 1})
	}
	return out
}

func checkDuplicateCriterionIDs(text string) []Issue {
	var issues []Issue
	seen := map[string]int{}
	for _, c := range scanCriteria(text) {
		if prev, ok := seen[c.id]; ok {
			issues = append(issues, Issue{
				File: "requirements.md",
				Line: c.lineNo,
				Msg:  fmt.Sprintf("duplicate acceptance criterion ID %s (first seen on line %d)", c.id, prev),
			})
		} else {
			seen[c.id] = c.lineNo
		}
	}
	return issues
}

func extractRequirementIDs(text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, c := range scanCriteria(text) {
		out[c.id] = struct{}{}
	}
	return out
}

func extractComponents(text string) map[string]struct{} {
	loc := componentsHead.FindStringIndex(text)
	if loc == nil {
		return nil
	}
	section := text[loc[1]:]
	// Stop at the next top-level "## " heading so component "###" headers don't
	// leak in from later sections (Testing Strategy, Error Handling, …).
	if end := nextH2.FindStringIndex(section); end != nil {
		section = section[:end[0]]
	}
	out := map[string]struct{}{}
	for _, m := range componentHeader.FindAllStringSubmatch(section, -1) {
		out[m[1]] = struct{}{}
	}
	return out
}

func extractTraceability(text string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range traceabilityRow.FindAllStringSubmatch(text, -1) {
		out[m[1]] = struct{}{}
	}
	return out
}

func checkEARS(file, text string) []Issue {
	var out []Issue
	for i, line := range strings.Split(text, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		if strings.HasPrefix(stripped, "#") ||
			strings.HasPrefix(stripped, "<!--") ||
			strings.HasPrefix(stripped, "-->") ||
			strings.HasPrefix(stripped, "**") ||
			strings.HasPrefix(stripped, "|") {
			continue
		}
		if !numberedLine.MatchString(stripped) {
			continue
		}
		if !earsLine.MatchString(stripped) {
			out = append(out, Issue{
				File: file,
				Line: i + 1,
				Msg:  "criterion lacks EARS structure (WHEN/WHILE/IF/WHERE ... THE SYSTEM SHALL ...): " + truncate(stripped, 80),
			})
		}
		if shouldWord.MatchString(stripped) {
			out = append(out, Issue{
				File: file,
				Line: i + 1,
				Msg:  "uses 'should' — replace with 'SHALL'",
			})
		}
	}
	return out
}

func parseTasks(text string) []taskRecord {
	var out []taskRecord
	var cur *taskRecord
	var block []string
	flush := func() {
		if cur == nil {
			return
		}
		joined := strings.Join(block, "\n")
		if m := ReqAnnotRe.FindStringSubmatch(joined); m != nil {
			for _, tok := range strings.Split(m[1], ",") {
				if t := strings.TrimSpace(tok); t != "" {
					cur.requirements = append(cur.requirements, t)
				}
			}
		}
		if m := DependsAnnotRe.FindStringSubmatch(joined); m != nil {
			for _, tok := range strings.Split(m[1], ",") {
				if t := strings.TrimSpace(tok); t != "" {
					cur.depends = append(cur.depends, t)
				}
			}
		}
		if m := BoundaryAnnotRe.FindStringSubmatch(joined); m != nil {
			cur.boundary = m[1]
		}
		if ParallelRe.MatchString(joined) {
			cur.parallel = true
		}
		out = append(out, *cur)
		cur = nil
	}
	for i, line := range strings.Split(text, "\n") {
		if m := TaskLineRe.FindStringSubmatch(line); m != nil {
			flush()
			block = []string{line}
			cur = &taskRecord{id: m[3], title: strings.TrimSpace(m[4]), lineNo: i + 1}
			if ParallelRe.MatchString(line) {
				cur.parallel = true
			}
			if bm := BoundaryAnnotRe.FindStringSubmatch(line); bm != nil {
				cur.boundary = bm[1]
			}
			continue
		}
		if cur != nil {
			block = append(block, line)
		}
	}
	flush()
	return out
}

// truncate caps s to n runes (not bytes) so UTF-8 criteria text is never cut
// mid-codepoint when echoed back in a validation message.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func sortedJoin(in []string) string {
	cp := append([]string(nil), in...)
	sort.Strings(cp)
	return strings.Join(cp, ", ")
}

// validDevelopmentFlow reports whether f is one of the selectable development
// flows. Mirrors cli.developmentFlows (validator must not import cli).
func validDevelopmentFlow(f string) bool {
	switch f {
	case "unit", "tdd", "tdd-e2e":
		return true
	}
	return false
}

// specDevelopmentFlow reads development_flow out of specDir/spec.json.
//
// A missing spec.json yields "" and every flow check stays silent: without the
// file there is no declared flow to hold the tasks to, and inventing one would
// manufacture findings on a correctly-shaped file. A spec.json that exists but
// omits the field resolves to "tdd", which is the default the whole toolchain
// already assumes (tasks-generation: "absent ⇒ tdd", cli.effectiveFlow).
func specDevelopmentFlow(specDir string) string {
	data, err := os.ReadFile(filepath.Join(specDir, "spec.json"))
	if err != nil {
		return ""
	}
	var s struct {
		DevelopmentFlow string `json:"development_flow"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return ""
	}
	f := strings.TrimSpace(s.DevelopmentFlow)
	if f == "" {
		return "tdd"
	}
	if !validDevelopmentFlow(f) {
		return "" // already reported where the flow is written; don't double-report here
	}
	return f
}

// parentTaskID returns the ID of the task one level up ("2.3" -> "2"), or "" for
// a top-level task. Sub-task ordering is only meaningful among siblings, so every
// flow check groups by this.
func parentTaskID(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[:i]
	}
	return ""
}

func isRedTask(title string) bool { return taskRedRe.MatchString(title) }

func isGreenTask(title string) bool {
	return taskGreenRe.MatchString(title) || taskRedGreenRe.MatchString(title)
}

// checkDevelopmentFlow holds tasks.md to the shape the spec's development_flow
// declares. Until now that contract lived only as prose in
// .claude/rules/tasks-generation.md — guidance the model reads, with no exit code
// behind it — so a spec could declare `unit` and be handed RED/GREEN tasks anyway
// (or declare `tdd` and get an implementation step with no test in front of it)
// and nothing would say so.
//
// These are deliberately the weakest rules that are still mechanical. Whether a
// task carries behavior or is pure scaffolding is a judgment call, and
// tasks-generation exempts scaffolding from needing a test at all — so a check
// that guessed at it would fire on correct files, and a gate that cries wolf is a
// gate someone turns off. Naming, by contrast, is something you can see: the
// rules mandate the explicit "RED —"/"GREEN —" pair, so these checks read only
// that.
//
// Calibrated against a 28-spec corpus: silent on all of it, including a task
// whose single RED is followed by four separate GREEN steps and one combined
// "RED→GREEN" sub-task. Hence "some earlier RED among the siblings" rather than
// "the immediately preceding sibling".
func checkDevelopmentFlow(flow string, tasks []taskRecord) []Issue {
	var issues []Issue
	switch flow {
	case "unit":
		for _, t := range tasks {
			if !isRedTask(t.title) && !isGreenTask(t.title) {
				continue
			}
			issues = append(issues, Issue{
				File: "tasks.md",
				Line: t.lineNo,
				Msg: fmt.Sprintf("task %s is shaped as a RED/GREEN pair but spec.json declares development_flow 'unit'"+
					" — under 'unit' the implementation comes first and the test covers it in the same task;"+
					" regenerate tasks for this flow, or set the flow to 'tdd'", t.id),
			})
		}
	case "tdd", "tdd-e2e":
		// One pass in document order: a GREEN is answered by any RED already seen
		// among its siblings, and a RED stays pending until a GREEN closes it.
		seenRed := map[string]bool{}
		pendingRed := map[string]string{}
		for _, t := range tasks {
			parent := parentTaskID(t.id)
			if isRedTask(t.title) {
				seenRed[parent] = true
				pendingRed[parent] = t.id
			}
			if isGreenTask(t.title) {
				if !seenRed[parent] {
					issues = append(issues, Issue{
						File: "tasks.md",
						Line: t.lineNo,
						Msg: fmt.Sprintf("task %s is a GREEN step with no preceding RED sub-task"+
							" — under development_flow '%s' an implementation step must follow a test that failed first", t.id, flow),
					})
				}
				delete(pendingRed, parent)
			}
		}
		// Re-walk in document order so the leftovers report deterministically.
		for _, t := range tasks {
			if pendingRed[parentTaskID(t.id)] != t.id {
				continue
			}
			issues = append(issues, Issue{
				File: "tasks.md",
				Line: t.lineNo,
				Msg: fmt.Sprintf("task %s is a RED step with no GREEN sub-task after it"+
					" — under development_flow '%s' a failing test must be answered by the implementation that passes it", t.id, flow),
			})
		}
	}
	return issues
}

// ValidateSteering inspects every (or one) steering file's frontmatter and
// reports inclusion-mode errors. Pass empty name to validate every file.
func ValidateSteering(steeringDir, name string) ([]Issue, error) {
	var files []string
	if name != "" {
		files = []string{filepath.Join(steeringDir, name+".md")}
	} else {
		entries, err := os.ReadDir(steeringDir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				files = append(files, filepath.Join(steeringDir, e.Name()))
			}
		}
	}
	var issues []Issue
	for _, path := range files {
		fname := filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, Issue{File: fname, Msg: "missing"})
			continue
		}
		fm := frontmatter.Parse(string(data))
		if df := fm.AsString("default_development_flow", ""); df != "" && !validDevelopmentFlow(df) {
			issues = append(issues, Issue{File: fname, Msg: "default_development_flow must be one of unit|tdd|tdd-e2e (got '" + df + "')"})
		}
		inc := fm.AsString("inclusion", "")
		valid := false
		for _, m := range []string{"always", "fileMatch", "manual", "auto"} {
			if m == inc {
				valid = true
				break
			}
		}
		if !valid {
			issues = append(issues, Issue{File: fname, Msg: "inclusion missing or invalid (got '" + inc + "')"})
			continue
		}
		if inc == "fileMatch" {
			if pats := fm.AsStringSlice("fileMatchPattern"); len(pats) == 0 {
				issues = append(issues, Issue{File: fname, Msg: "fileMatch requires non-empty fileMatchPattern"})
			}
		}
		if inc == "auto" {
			if fm.AsString("name", "") == "" || fm.AsString("description", "") == "" {
				issues = append(issues, Issue{File: fname, Msg: "auto inclusion requires name and description"})
			}
		}
	}
	return issues, nil
}

// ValidateSkill mirrors the Python skill validator: frontmatter, line/token
// budget, required headings, and reference load-trigger presence.
func ValidateSkill(skillDir, name string) ([]Issue, int, int) {
	var issues []Issue
	sf := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(sf)
	if err != nil {
		return []Issue{{File: filepath.Base(skillDir), Msg: "SKILL.md missing"}}, 0, 0
	}
	text := textutil.NormalizeNewlines(string(data))
	fm := frontmatter.Parse(text)
	if fm.AsString("name", "") == "" {
		issues = append(issues, Issue{File: "SKILL.md", Msg: "frontmatter missing 'name'"})
	} else if fm.AsString("name", "") != name {
		issues = append(issues, Issue{
			File: "SKILL.md",
			Msg:  fmt.Sprintf("frontmatter name='%s' does not match dir '%s'", fm.AsString("name", ""), name),
		})
	}
	if fm.AsString("description", "") == "" {
		issues = append(issues, Issue{File: "SKILL.md", Msg: "frontmatter missing 'description' (the activation trigger)"})
	}
	lines := strings.Count(text, "\n") + 1
	if lines > 500 {
		issues = append(issues, Issue{
			File: "SKILL.md",
			Msg:  fmt.Sprintf("%d lines (>500). Move detail into references/ with explicit load triggers.", lines),
		})
	}
	tokens := len(text) / 4
	if tokens < 1 {
		tokens = 1
	}
	if tokens > 5000 {
		issues = append(issues, Issue{
			File: "SKILL.md",
			Msg:  fmt.Sprintf("~%d tokens (>5000). Apply progressive disclosure.", tokens),
		})
	}
	for _, h := range []string{"## Goal", "## Execution Workflow", "## Gotchas", "## Verification Before Reporting", "## Completion Criteria"} {
		if !strings.Contains(text, h) {
			issues = append(issues, Issue{File: "SKILL.md", Msg: "missing section '" + h + "'"})
		}
	}
	refsDir := filepath.Join(skillDir, "references")
	if entries, err := os.ReadDir(refsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !strings.Contains(text, e.Name()) {
				issues = append(issues, Issue{
					File: "references/" + e.Name(),
					Msg:  "not mentioned in SKILL.md (no load trigger)",
				})
			}
		}
	}
	return issues, lines, tokens
}
