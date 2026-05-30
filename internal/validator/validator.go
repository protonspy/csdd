// Package validator implements the mechanical checks the Kiro spec mandates:
// EARS phrasing, unique requirement IDs, traceability coverage, task
// annotation hygiene (_Requirements:_, _Boundary:_, _Depends:_), parallel
// safety, and skill structure. The checks here mirror the Python reference
// implementation 1:1 so the Go binary can replace it without behavior drift.
package validator

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/livelo/kspec/internal/frontmatter"
)

var (
	reqHeader       = regexp.MustCompile(`(?m)^###\s+Requirement\s+(\d+)\b`)
	criterion       = regexp.MustCompile(`(?mi)^(\s*)(\d+)\.\s+(WHEN|WHILE|IF|WHERE|THE\s+SYSTEM)\b`)
	criterionNumber = regexp.MustCompile(`(?m)^\s*(\d+)\.\s+`)
	earsLine        = regexp.MustCompile(`(?i)\b(WHEN|WHILE|IF|WHERE)\b.*\bTHE\s+SYSTEM\s+SHALL\b|\bTHE\s+SYSTEM\s+SHALL\b`)
	shouldWord      = regexp.MustCompile(`(?i)\bshould\b`)
	numberedLine    = regexp.MustCompile(`^\d+\.\s`)
	taskLine        = regexp.MustCompile(`^(\s*)-\s+\[\s*[xX ]?\s*\]\s+(\d+(?:\.\d+)?)\.?\s+(.*)$`)
	reqAnnot        = regexp.MustCompile(`_Requirements:\s*([\d,\.\s]+)_`)
	boundaryAnnot   = regexp.MustCompile(`_Boundary:\s*([A-Za-z0-9_\-]+)_`)
	dependsAnnot    = regexp.MustCompile(`_Depends:\s*([\d,\.\s]+)_`)
	parallelMarker  = regexp.MustCompile(`\(P\)`)
	componentHeader = regexp.MustCompile(`(?m)^###\s+([A-Za-z0-9_\-]+)\s*$`)
	traceabilityRow = regexp.MustCompile(`(?m)^\|\s*(\d+\.\d+)\s*\|`)
	numericID       = regexp.MustCompile(`^\d+\.\d+$`)
)

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
		if text, err := os.ReadFile(reqPath); err == nil {
			reqIDs = extractRequirementIDs(string(text))
			issues = append(issues, checkEARS("requirements.md", string(text))...)
			headers := reqHeader.FindAllStringSubmatch(string(text), -1)
			seen := map[string]int{}
			for _, h := range headers {
				seen[h[1]]++
			}
			for k, v := range seen {
				if v > 1 {
					issues = append(issues, Issue{File: "requirements.md", Msg: fmt.Sprintf("duplicate Requirement %s header", k)})
				}
			}
			issues = append(issues, checkDuplicateCriterionIDs(string(text))...)
		} else if phase != PhaseAll {
			if _, err := os.Stat(bugfixPath); err != nil {
				issues = append(issues, Issue{File: "requirements.md", Msg: "missing (or bugfix.md)"})
			}
		}
	}

	// Bugfix
	if data, err := os.ReadFile(bugfixPath); err == nil {
		text := string(data)
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
			text := string(data)
			lineCount := strings.Count(text, "\n") + 1
			if lineCount > 1000 {
				issues = append(issues, Issue{
					File: "design.md",
					Msg:  fmt.Sprintf("%d lines > 1000 — split the feature into multiple specs", lineCount),
				})
			}
			components = extractComponents(text)
			traceability = extractTraceability(text)
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
			tasks := parseTasks(string(data))
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
		} else if phase == PhaseTasks {
			issues = append(issues, Issue{File: "tasks.md", Msg: "missing"})
		}
	}

	return issues
}

func checkDuplicateCriterionIDs(text string) []Issue {
	var issues []Issue
	headers := reqHeader.FindAllStringSubmatchIndex(text, -1)
	for i, h := range headers {
		reqN := text[h[2]:h[3]]
		start := h[1]
		end := len(text)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		block := text[start:end]
		seen := map[string]int{}
		for _, m := range criterionNumber.FindAllStringSubmatchIndex(block, -1) {
			criterionN := block[m[2]:m[3]]
			id := reqN + "." + criterionN
			line := strings.Count(text[:start+m[0]], "\n") + 1
			if prev, ok := seen[id]; ok {
				issues = append(issues, Issue{
					File: "requirements.md",
					Line: line,
					Msg:  fmt.Sprintf("duplicate acceptance criterion ID %s (first seen on line %d)", id, prev),
				})
			} else {
				seen[id] = line
			}
		}
	}
	return issues
}

func extractRequirementIDs(text string) map[string]struct{} {
	out := map[string]struct{}{}
	headers := reqHeader.FindAllStringSubmatchIndex(text, -1)
	for i, m := range headers {
		reqN := text[m[2]:m[3]]
		start := m[1]
		end := len(text)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		block := text[start:end]
		for _, cm := range criterion.FindAllStringSubmatch(block, -1) {
			out[reqN+"."+cm[2]] = struct{}{}
		}
	}
	return out
}

func extractComponents(text string) map[string]struct{} {
	idx := strings.Index(text, "## Components and Interfaces")
	if idx < 0 {
		return nil
	}
	section := text[idx:]
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
		if m := reqAnnot.FindStringSubmatch(joined); m != nil {
			for _, tok := range strings.Split(m[1], ",") {
				if t := strings.TrimSpace(tok); t != "" {
					cur.requirements = append(cur.requirements, t)
				}
			}
		}
		if m := dependsAnnot.FindStringSubmatch(joined); m != nil {
			for _, tok := range strings.Split(m[1], ",") {
				if t := strings.TrimSpace(tok); t != "" {
					cur.depends = append(cur.depends, t)
				}
			}
		}
		if m := boundaryAnnot.FindStringSubmatch(joined); m != nil {
			cur.boundary = m[1]
		}
		if parallelMarker.MatchString(joined) {
			cur.parallel = true
		}
		out = append(out, *cur)
		cur = nil
	}
	for i, line := range strings.Split(text, "\n") {
		if m := taskLine.FindStringSubmatch(line); m != nil {
			flush()
			block = []string{line}
			cur = &taskRecord{id: m[2], lineNo: i + 1}
			if parallelMarker.MatchString(line) {
				cur.parallel = true
			}
			if bm := boundaryAnnot.FindStringSubmatch(line); bm != nil {
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
	// Simple insertion sort: small slices.
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j-1] > cp[j]; j-- {
			cp[j-1], cp[j] = cp[j], cp[j-1]
		}
	}
	return strings.Join(cp, ", ")
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
	text := string(data)
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
