package validator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpec(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const validRequirements = `# Requirements

## Requirements

### Requirement 1: Album

**Acceptance Criteria**
1. WHEN a user creates an album THEN the system SHALL persist it within 500ms.
2. IF the name is empty THEN the system SHALL return ` + "`ALBUM_NAME_REQUIRED`" + ` with HTTP 400.
3. WHILE deletion is in progress THE SYSTEM SHALL block uploads.
`

const validDesign = `# Design

## Architecture Pattern & Boundary Map

` + "```mermaid\ngraph TD\n```\n" + `

## File Structure Plan

` + "```text\nsrc/\n```\n" + `

## Requirements Traceability
| Requirement | Components | Interfaces | Flows |
|---|---|---|---|
| 1.1 | AlbumService | createAlbum | flow-1 |
| 1.2 | AlbumService | validate     | flow-1 |
| 1.3 | AlbumService | delete       | flow-1 |

## Components and Interfaces

### AlbumService
- **Intent**: own albums.
`

const validTasks = `# Tasks

## Phase 1
- [ ] 1. Setup _Boundary: AlbumService_
  - [ ] 1.1 Create migrations
    - _Requirements: 1.1_
  - [ ] 1.2 Add validation
    - _Requirements: 1.2_
    - _Depends: 1.1_
`

func TestValidateSpecHappyPath(t *testing.T) {
	dir := writeSpec(t, map[string]string{
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        validTasks,
	})
	issues := ValidateSpec(dir, PhaseAll)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d: %v", len(issues), issues)
	}
}

func TestValidateSpecDetectsShouldAndMissingEARS(t *testing.T) {
	bad := `# Requirements
## Requirements
### Requirement 1: Bad
**Acceptance Criteria**
1. WHEN a thing THEN the system SHALL do it.
2. The system should validate inputs.
`
	dir := writeSpec(t, map[string]string{"requirements.md": bad})
	issues := ValidateSpec(dir, PhaseAll)
	var sawShould, sawEARS bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "should") {
			sawShould = true
		}
		if strings.Contains(i.Msg, "EARS") {
			sawEARS = true
		}
	}
	if !sawShould {
		t.Error("validator should detect 'should' keyword")
	}
	if !sawEARS {
		t.Error("validator should detect missing EARS structure")
	}
}

func TestValidateSpecDuplicateRequirementHeaders(t *testing.T) {
	dup := `# Requirements
## Requirements
### Requirement 1: A
**Acceptance Criteria**
1. THE SYSTEM SHALL do it.
### Requirement 1: B
**Acceptance Criteria**
1. THE SYSTEM SHALL do other.
`
	dir := writeSpec(t, map[string]string{"requirements.md": dup})
	issues := ValidateSpec(dir, PhaseAll)
	var sawDup bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "duplicate") {
			sawDup = true
		}
	}
	if !sawDup {
		t.Error("duplicate Requirement headers must be flagged")
	}
}

func TestValidateSpecDuplicateCriterionIDs(t *testing.T) {
	dup := `# Requirements
## Requirements
### Requirement 1: A
**Acceptance Criteria**
1. THE SYSTEM SHALL do it.
1. THE SYSTEM SHALL do another thing.
`
	dir := writeSpec(t, map[string]string{"requirements.md": dup})
	issues := ValidateSpec(dir, PhaseRequirements)
	var sawDup bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "duplicate acceptance criterion ID 1.1") {
			sawDup = true
		}
	}
	if !sawDup {
		t.Errorf("duplicate criterion IDs must be flagged: %v", issues)
	}
}

func TestValidateSpecMissingTraceability(t *testing.T) {
	design := `# Design
## Architecture Pattern & Boundary Map
## File Structure Plan
## Requirements Traceability
| Requirement | Components |
|---|---|
| 1.1 | A |
## Components and Interfaces
### A
`
	dir := writeSpec(t, map[string]string{
		"requirements.md": validRequirements,
		"design.md":       design,
	})
	issues := ValidateSpec(dir, PhaseAll)
	var sawMissing bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "Traceability table missing IDs") {
			sawMissing = true
		}
	}
	if !sawMissing {
		t.Error("missing traceability IDs must be flagged")
	}
}

func TestValidateSpecDesignTooLong(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# Design\n## Architecture Pattern & Boundary Map\n## File Structure Plan\n")
	sb.WriteString("## Requirements Traceability\n| Requirement |\n|---|\n")
	sb.WriteString("## Components and Interfaces\n### A\n")
	for i := 0; i < 1100; i++ {
		sb.WriteString("filler line\n")
	}
	dir := writeSpec(t, map[string]string{"design.md": sb.String()})
	issues := ValidateSpec(dir, PhaseDesign)
	var sawSplit bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "lines > 1000") {
			sawSplit = true
		}
	}
	if !sawSplit {
		t.Error("design over 1000 lines must be flagged")
	}
}

func TestValidateSpecTaskAnnotations(t *testing.T) {
	// _Requirements: regex only matches digits/dots/commas/whitespace, so a
	// "non-numeric" token must still consist of digit-like characters that
	// fail the strict <n>.<n> pattern (e.g. just "1" without the dot).
	tasks := `# Tasks
- [ ] 1. Generic _Boundary: GhostComponent_ (P)
  - [ ] 1.1 sub
    - _Requirements: 9.9, 1_
    - _Depends: 99.99_
- [ ] 2. Another _Boundary: GhostComponent_ (P)
  - [ ] 2.1 sub
    - _Requirements: 1.1_
`
	dir := writeSpec(t, map[string]string{
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        tasks,
	})
	issues := ValidateSpec(dir, PhaseAll)
	var sawUnknownReq, sawNonNumeric, sawUnknownBoundary, sawDepends, sawParallel bool
	for _, i := range issues {
		switch {
		case strings.Contains(i.Msg, "references unknown requirement '9.9'"):
			sawUnknownReq = true
		case strings.Contains(i.Msg, "non-numeric token '1'"):
			sawNonNumeric = true
		case strings.Contains(i.Msg, "_Boundary: GhostComponent_"):
			sawUnknownBoundary = true
		case strings.Contains(i.Msg, "_Depends:_ references unknown task '99.99'"):
			sawDepends = true
		case strings.Contains(i.Msg, "share boundary 'GhostComponent'"):
			sawParallel = true
		}
	}
	for name, ok := range map[string]bool{
		"unknown req":     sawUnknownReq,
		"non-numeric":     sawNonNumeric,
		"unknown bndary":  sawUnknownBoundary,
		"unknown depends": sawDepends,
		"parallel clash":  sawParallel,
	} {
		if !ok {
			t.Errorf("task validator missed %q. all issues: %v", name, issues)
		}
	}
}

func TestValidateSpecDuplicateTasksAndSameBoundaryDepends(t *testing.T) {
	tasks := `# Tasks
- [ ] 1. Setup _Boundary: AlbumService_
  - [ ] 1.1 Create first thing _Boundary: AlbumService_
    - _Requirements: 1.1_
  - [ ] 1.1 Create duplicate thing _Boundary: AlbumService_
    - _Requirements: 1.2_
  - [ ] 1.2 Depend inside same boundary _Boundary: AlbumService_
    - _Requirements: 1.3_
    - _Depends: 1.1_
`
	dir := writeSpec(t, map[string]string{
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        tasks,
	})
	issues := ValidateSpec(dir, PhaseTasks)
	var sawDup, sawSameBoundary bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "duplicate task ID 1.1") {
			sawDup = true
		}
		if strings.Contains(i.Msg, "same-boundary task '1.1'") {
			sawSameBoundary = true
		}
	}
	if !sawDup {
		t.Errorf("duplicate task ID must be flagged: %v", issues)
	}
	if !sawSameBoundary {
		t.Errorf("same-boundary _Depends:_ must be flagged: %v", issues)
	}
}

func TestValidateSpecMissingPhaseArtifacts(t *testing.T) {
	dir := writeSpec(t, map[string]string{"requirements.md": validRequirements})
	issues := ValidateSpec(dir, PhaseDesign)
	var sawDesign bool
	for _, i := range issues {
		if i.File == "design.md" && strings.Contains(i.Msg, "missing") {
			sawDesign = true
		}
	}
	if !sawDesign {
		t.Errorf("missing design.md must be flagged for design phase: %v", issues)
	}
	issues = ValidateSpec(dir, PhaseTasks)
	var sawTasks bool
	for _, i := range issues {
		if i.File == "tasks.md" && strings.Contains(i.Msg, "missing") {
			sawTasks = true
		}
	}
	if !sawTasks {
		t.Errorf("missing tasks.md must be flagged for tasks phase: %v", issues)
	}
}

func TestValidateSpecLeafTaskRequirements(t *testing.T) {
	tasks := `# Tasks
- [ ] 1. Top _Boundary: AlbumService_
  - [ ] 1.1 leaf
` // 1.1 missing _Requirements:_
	dir := writeSpec(t, map[string]string{
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        tasks,
	})
	issues := ValidateSpec(dir, PhaseAll)
	var sawLeaf bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "leaf task 1.1 missing _Requirements:_") {
			sawLeaf = true
		}
	}
	if !sawLeaf {
		t.Error("leaf task missing _Requirements:_ must be flagged")
	}
}

func TestValidateSpecParallelMissingBoundary(t *testing.T) {
	tasks := `# Tasks
- [ ] 1. Solo (P)
  - [ ] 1.1 sub
    - _Requirements: 1.1_
`
	dir := writeSpec(t, map[string]string{
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        tasks,
	})
	issues := ValidateSpec(dir, PhaseAll)
	var sawMissing bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "missing _Boundary:_") {
			sawMissing = true
		}
	}
	if !sawMissing {
		t.Error("(P) without Boundary must be flagged")
	}
}

func TestValidateSpecBugfixSections(t *testing.T) {
	bug := `# Bugfix
## Reproduction
Some steps.

Current Behavior:
WHEN x THEN broken
`
	dir := writeSpec(t, map[string]string{"bugfix.md": bug})
	issues := ValidateSpec(dir, PhaseAll)
	want := []string{
		"missing 'Expected Behavior:'",
		"missing 'Unchanged Behavior:'",
		"missing Root Cause",
	}
	for _, w := range want {
		var sawIt bool
		for _, i := range issues {
			if strings.Contains(i.Msg, w) {
				sawIt = true
				break
			}
		}
		if !sawIt {
			t.Errorf("bugfix validator missed %q", w)
		}
	}
}

func TestValidateSpecMissingDesignSections(t *testing.T) {
	design := "# Design\nincomplete\n"
	dir := writeSpec(t, map[string]string{
		"requirements.md": validRequirements,
		"design.md":       design,
	})
	issues := ValidateSpec(dir, PhaseAll)
	var sawFSP, sawAPB bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "## File Structure Plan") {
			sawFSP = true
		}
		if strings.Contains(i.Msg, "## Architecture Pattern & Boundary Map") {
			sawAPB = true
		}
	}
	if !sawFSP {
		t.Error("missing File Structure Plan must be flagged")
	}
	if !sawAPB {
		t.Error("missing Architecture Pattern & Boundary Map must be flagged")
	}
}

func TestValidateSteering(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"ok.md":                   "---\ninclusion: always\n---\n# OK\n",
		"bad-fm.md":               "---\ninclusion: wrong\n---\n",
		"filematch-no-pattern.md": "---\ninclusion: fileMatch\n---\n",
		"auto-missing.md":         "---\ninclusion: auto\nname: x\n---\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	issues, err := ValidateSteering(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 3 {
		t.Errorf("expected 3 issues, got %d: %v", len(issues), issues)
	}
}

// Req 2.3 / 3.2: an invalid default_development_flow is flagged; a valid or
// absent one is not.
func TestValidateSteeringFlowDefault(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"valid-flow.md":   "---\ninclusion: always\ndefault_development_flow: tdd-e2e\n---\n",
		"absent-flow.md":  "---\ninclusion: always\n---\n",
		"invalid-flow.md": "---\ninclusion: always\ndefault_development_flow: bogus\n---\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	issues, err := ValidateSteering(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].File != "invalid-flow.md" || !strings.Contains(issues[0].Msg, "default_development_flow") {
		t.Errorf("unexpected issue: %+v", issues[0])
	}
}

func TestValidateSteeringByName(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "missing.md"), []byte(""), 0o644)
	issues, err := ValidateSteering(dir, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) == 0 {
		t.Error("empty steering file must produce issues")
	}
}

func TestValidateSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo")
	_ = os.MkdirAll(filepath.Join(skillDir, "references"), 0o755)
	body := `---
name: demo
description: Demo skill.
---

# Demo

## Goal
Do things.

## Execution Workflow
Steps.

## Gotchas
None.

## Verification Before Reporting
Run tests.

## Completion Criteria
- [ ] Done.
`
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644)
	_ = os.WriteFile(filepath.Join(skillDir, "references", "lonely.md"), []byte("hi"), 0o644)
	issues, lines, tokens := ValidateSkill(skillDir, "demo")
	if lines <= 0 || tokens <= 0 {
		t.Errorf("expected lines/tokens metrics > 0 (lines=%d tokens=%d)", lines, tokens)
	}
	// The validator stores the reference filename in Issue.File, not Msg.
	var sawLonely bool
	for _, i := range issues {
		if strings.Contains(i.File, "lonely.md") {
			sawLonely = true
		}
	}
	if !sawLonely {
		t.Errorf("unreferenced reference file must be flagged; issues: %v", issues)
	}
}

func TestValidateSkillMissingFile(t *testing.T) {
	issues, _, _ := ValidateSkill(t.TempDir(), "demo")
	if len(issues) == 0 {
		t.Error("missing SKILL.md must produce issues")
	}
}

func TestValidateSkillOversize(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "huge")
	_ = os.MkdirAll(skillDir, 0o755)
	var sb strings.Builder
	sb.WriteString("---\nname: huge\ndescription: desc\n---\n# Huge\n")
	for _, h := range []string{"## Goal", "## Execution Workflow", "## Gotchas", "## Verification Before Reporting", "## Completion Criteria"} {
		sb.WriteString(h + "\nfiller\n")
	}
	for i := 0; i < 600; i++ {
		sb.WriteString("filler line ")
		for j := 0; j < 30; j++ {
			sb.WriteString("xx ")
		}
		sb.WriteString("\n")
	}
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(sb.String()), 0o644)
	issues, _, _ := ValidateSkill(skillDir, "huge")
	var sawLines, sawTokens bool
	for _, i := range issues {
		if strings.Contains(i.Msg, ">500") {
			sawLines = true
		}
		if strings.Contains(i.Msg, ">5000") {
			sawTokens = true
		}
	}
	if !sawLines {
		t.Error("oversize SKILL.md (lines) must be flagged")
	}
	if !sawTokens {
		t.Error("oversize SKILL.md (tokens) must be flagged")
	}
}

func TestValidateSkillNameMismatch(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "actual-name")
	_ = os.MkdirAll(skillDir, 0o755)
	body := "---\nname: wrong-name\ndescription: d\n---\n# T\n## Goal\n## Execution Workflow\n## Gotchas\n## Verification Before Reporting\n## Completion Criteria\n"
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644)
	issues, _, _ := ValidateSkill(skillDir, "actual-name")
	var sawMismatch bool
	for _, i := range issues {
		if strings.Contains(i.Msg, "does not match dir") {
			sawMismatch = true
		}
	}
	if !sawMismatch {
		t.Error("name mismatch must be flagged")
	}
}

func TestTruncateHelper(t *testing.T) {
	if got := truncate("0123456789", 4); got != "0123" {
		t.Errorf("truncate long: %q", got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Errorf("truncate short: %q", got)
	}
}

func TestSortedJoinHelper(t *testing.T) {
	if got := sortedJoin([]string{"2.1", "1.1", "3.2"}); got != "1.1, 2.1, 3.2" {
		t.Errorf("sortedJoin: %q", got)
	}
}

func TestIssueStringFormat(t *testing.T) {
	withLine := Issue{File: "x.md", Line: 7, Msg: "boom"}.String()
	if withLine != "x.md:7: boom" {
		t.Errorf("with-line format wrong: %s", withLine)
	}
	noLine := Issue{File: "x.md", Msg: "boom"}.String()
	if noLine != "x.md: boom" {
		t.Errorf("no-line format wrong: %s", noLine)
	}
}

// --- Regression tests for grammar-unification fixes ---

func TestExtractRequirementIDsMidLineEARSKeyword(t *testing.T) {
	// A criterion whose EARS keyword is not immediately after the number must
	// still register an ID, or tasks referencing it get false "unknown requirement".
	req := "### Requirement 1: Boot\n\n" +
		"1. On startup, WHEN the app boots THE SYSTEM SHALL load config.\n"
	ids := extractRequirementIDs(req)
	if _, ok := ids["1.1"]; !ok {
		t.Errorf("mid-line EARS keyword criterion not registered as 1.1: got %v", ids)
	}
}

func TestExtractRequirementIDsIgnoresNonEARSNotes(t *testing.T) {
	req := "### Requirement 1: Boot\n\n" +
		"1. WHEN x THE SYSTEM SHALL y.\n\n" +
		"Notes:\n1. just a note, not a criterion\n"
	ids := extractRequirementIDs(req)
	if _, ok := ids["1.1"]; !ok {
		t.Errorf("real criterion 1.1 missing: %v", ids)
	}
	// The note reuses list number 1 but has no EARS structure, so it must not
	// create a phantom duplicate criterion.
	if dups := checkDuplicateCriterionIDs(req); len(dups) != 0 {
		t.Errorf("non-EARS numbered note caused false duplicate: %v", dups)
	}
}

func TestExtractComponentsStopsAtNextSection(t *testing.T) {
	design := "## Components and Interfaces\n\n" +
		"### AlbumService\n- intent\n\n" +
		"## Testing Strategy\n\n" +
		"### UnitTests\n- cases\n"
	comps := extractComponents(design)
	if _, ok := comps["AlbumService"]; !ok {
		t.Errorf("AlbumService component missing: %v", comps)
	}
	if _, ok := comps["UnitTests"]; ok {
		t.Errorf("UnitTests from a later section leaked into components: %v", comps)
	}
}

func TestParseTasksThreeLevelID(t *testing.T) {
	tasks := "## Phase 1\n- [ ] 1. Parent\n  - [ ] 1.1 Child\n    - [ ] 1.1.1 Grandchild\n"
	got := parseTasks(tasks)
	var ids []string
	for _, tk := range got {
		ids = append(ids, tk.id)
	}
	found := false
	for _, id := range ids {
		if id == "1.1.1" {
			found = true
		}
	}
	if !found {
		t.Errorf("three-level task id 1.1.1 not parsed; got ids %v", ids)
	}
}

func TestValidatorIgnoresFencedTaskExamples(t *testing.T) {
	// A fenced example must not be counted as a real task or trip duplicate-ID.
	tasks := "## Phase 1\n- [ ] 1. Real _Boundary: Svc_\n" +
		"    - _Requirements: 1.1_\n\n" +
		"```\n- [ ] 1. Fenced example\n```\n"
	masked := MaskCodeFences(tasks)
	got := parseTasks(masked)
	if len(got) != 1 {
		t.Errorf("expected 1 real task, fenced example was counted: got %d", len(got))
	}
}

func TestDuplicateCriterionLineNumberAccurate(t *testing.T) {
	req := "### Requirement 1: R\n" + // line 1
		"1. WHEN a THE SYSTEM SHALL b.\n" + // line 2 -> 1.1 first
		"1. WHEN c THE SYSTEM SHALL d.\n" // line 3 -> 1.1 dup
	dups := checkDuplicateCriterionIDs(req)
	if len(dups) != 1 {
		t.Fatalf("expected 1 duplicate, got %d: %v", len(dups), dups)
	}
	if dups[0].Line != 3 {
		t.Errorf("duplicate reported on line %d, want 3", dups[0].Line)
	}
	if !strings.Contains(dups[0].Msg, "first seen on line 2") {
		t.Errorf("first-seen line wrong: %q", dups[0].Msg)
	}
}

func TestValidateSpecCRLFRequirements(t *testing.T) {
	// CRLF requirements must validate identically to LF.
	crlf := strings.ReplaceAll(validRequirements, "\n", "\r\n")
	dir := writeSpec(t, map[string]string{"requirements.md": crlf})
	issues := ValidateSpec(dir, PhaseRequirements)
	for _, is := range issues {
		if strings.Contains(is.Msg, "EARS") {
			t.Errorf("CRLF requirements produced a false EARS issue: %v", is)
		}
	}
}

// specJSON renders a minimal spec.json carrying just the development flow, which
// is all the flow checks read.
func specJSON(flow string) string {
	if flow == "" {
		return `{"schema_version": 1, "feature_name": "albums"}`
	}
	return `{"schema_version": 1, "feature_name": "albums", "development_flow": "` + flow + `"}`
}

// flowIssues narrows a validation run to the messages the flow checks emit, so a
// fixture's unrelated findings never mask (or fake) the assertion.
func flowIssues(t *testing.T, files map[string]string) []Issue {
	t.Helper()
	var out []Issue
	for _, is := range ValidateSpec(writeSpec(t, files), PhaseAll) {
		if strings.Contains(is.Msg, "RED") || strings.Contains(is.Msg, "GREEN") {
			out = append(out, is)
		}
	}
	return out
}

func TestValidateSpecTDDFlowCatchesOrphanGreenAndDanglingRed(t *testing.T) {
	tasks := `# Tasks

## Phase 1
- [ ] 1. Connect
  - [ ] 1.1 GREEN — implement without a failing test first
    - _Requirements: 1.1_
- [ ] 2. Rotate
  - [ ] 2.1 RED — write the failing test
    - _Requirements: 1.2_
`
	issues := flowIssues(t, map[string]string{
		"spec.json":       specJSON("tdd"),
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        tasks,
	})
	var sawOrphanGreen, sawDanglingRed bool
	for _, is := range issues {
		if strings.Contains(is.Msg, "task 1.1 is a GREEN step with no preceding RED") {
			sawOrphanGreen = true
		}
		if strings.Contains(is.Msg, "task 2.1 is a RED step with no GREEN sub-task after it") {
			sawDanglingRed = true
		}
	}
	if !sawOrphanGreen {
		t.Errorf("orphan GREEN not reported. issues: %v", issues)
	}
	if !sawDanglingRed {
		t.Errorf("dangling RED not reported. issues: %v", issues)
	}
}

func TestValidateSpecTDDFlowAcceptsRealCorpusShapes(t *testing.T) {
	// Both shapes are taken from the shipped corpus: one RED answered by several
	// separate GREEN steps, and a single sub-task carrying both halves. Requiring
	// the RED to be the *immediately* preceding sibling would fail all of these.
	tasks := `# Tasks

## Phase 1
- [ ] 1. Harness
  - [ ] 1.1 RED — the failing test
    - _Requirements: 1.1_
  - [ ] 1.2 GREEN (mechanical) — the generated half
    - _Requirements: 1.1_
  - [ ] 1.3 GREEN (by hand) — the rest
    - _Requirements: 1.2_
  - [ ] 1.4 GREEN (baseline) — record it
    - _Requirements: 1.3_
- [ ] 2. Routes
  - [ ] 2.1 RED — the failing test
    - _Requirements: 1.1_
  - [ ] 2.2 GREEN — make it pass
    - _Requirements: 1.1_
  - [ ] 2.3 RED→GREEN — one step that opens and closes the pair
    - _Requirements: 1.2_
`
	if issues := flowIssues(t, map[string]string{
		"spec.json":       specJSON("tdd"),
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        tasks,
	}); len(issues) != 0 {
		t.Errorf("well-formed tdd tasks produced flow issues: %v", issues)
	}
}

func TestValidateSpecTDDFlowGroupsBySibling(t *testing.T) {
	// A RED under task 1 must not answer a GREEN under task 2 — the pair is only
	// meaningful among siblings.
	tasks := `# Tasks

## Phase 1
- [ ] 1. First
  - [ ] 1.1 RED — the failing test
    - _Requirements: 1.1_
  - [ ] 1.2 GREEN — make it pass
    - _Requirements: 1.1_
- [ ] 2. Second
  - [ ] 2.1 GREEN — borrowed someone else's RED
    - _Requirements: 1.2_
`
	issues := flowIssues(t, map[string]string{
		"spec.json":       specJSON("tdd"),
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        tasks,
	})
	if len(issues) != 1 || !strings.Contains(issues[0].Msg, "task 2.1 is a GREEN step") {
		t.Errorf("expected exactly the cross-parent GREEN to be reported, got: %v", issues)
	}
}

func TestValidateSpecUnitFlowRejectsRedGreenShape(t *testing.T) {
	tasks := `# Tasks

## Phase 1
- [ ] 1. Connect
  - [ ] 1.1 RED — write the failing test
    - _Requirements: 1.1_
  - [ ] 1.2 GREEN — make it pass
    - _Requirements: 1.1_
`
	issues := flowIssues(t, map[string]string{
		"spec.json":       specJSON("unit"),
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        tasks,
	})
	if len(issues) != 2 {
		t.Fatalf("expected both RED/GREEN tasks flagged under 'unit', got: %v", issues)
	}
	for _, is := range issues {
		if !strings.Contains(is.Msg, "development_flow 'unit'") {
			t.Errorf("unit mismatch message does not name the flow: %v", is)
		}
	}
}

func TestValidateSpecUnitFlowAcceptsTestsAfterShape(t *testing.T) {
	tasks := `# Tasks

## Phase 1
- [ ] 1. Connect
  - [ ] 1.1 Implement the connect action
    - _Requirements: 1.1_
  - [ ] 1.2 Cover connect with unit tests
    - _Requirements: 1.1_
`
	if issues := flowIssues(t, map[string]string{
		"spec.json":       specJSON("unit"),
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        tasks,
	}); len(issues) != 0 {
		t.Errorf("tests-after tasks produced flow issues under 'unit': %v", issues)
	}
}

func TestValidateSpecFlowDefaultsAndSilence(t *testing.T) {
	orphanGreen := `# Tasks

## Phase 1
- [ ] 1. Connect
  - [ ] 1.1 GREEN — implement without a failing test first
    - _Requirements: 1.1_
`
	// spec.json present but no development_flow: the documented default is tdd.
	if issues := flowIssues(t, map[string]string{
		"spec.json":       specJSON(""),
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        orphanGreen,
	}); len(issues) != 1 {
		t.Errorf("absent development_flow should default to tdd and report the orphan GREEN, got: %v", issues)
	}
	// No spec.json at all: nothing declares a flow, so the checks stay silent
	// rather than inventing one.
	if issues := flowIssues(t, map[string]string{
		"requirements.md": validRequirements,
		"design.md":       validDesign,
		"tasks.md":        orphanGreen,
	}); len(issues) != 0 {
		t.Errorf("a spec with no spec.json must not get flow findings, got: %v", issues)
	}
}
