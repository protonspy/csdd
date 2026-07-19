package templater

import (
	"strings"
	"testing"
)

// tierTableRows is the verification-tier contract, verbatim. Requirement 1.4
// demands every template that mandates verification state it IDENTICALLY, and
// this is the mechanical check that keeps them honest.
//
// It is not pedantry. The r1 draft of this change edited six templates and missed
// `rules/definition-of-done.md.tmpl` — a rule file, ambiently loaded, binding
// harder than any skill that has to be invoked. One stale copy of the contract
// silently restores the old doctrine everywhere, and nothing else in the system
// would have noticed.
var tierTableRows = []string{
	"| | Tier 1 — inner loop | Tier 2 — task exit | Tier 3 — integration |",
	"| Trigger | each RED→GREEN step | one task finishes | batch merge; feat exit |",
	"| Tests | the focused test(s) | the full suite, fast mode | the full suite |",
	"| Lint | — | the touched files | the whole tree |",
	"| Typecheck | — | — | yes |",
	"| Build | — | — | yes |",
	"| Coverage | — | — | feat exit only |",
	`| Claim it earns | "this behavior works" | "this task is green" | "the feat is green" |`,
}

// tierContractHolders are the templates that state the contract in full. Both are
// ambient: the rule is always loaded, and verify-change is the skill every gate
// runs through.
func tierContractHolders(t *testing.T) map[string]string {
	t.Helper()
	rules, err := RuleFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := SkillFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"rules/definition-of-done.md":   rules["definition-of-done.md"],
		"skills/verify-change/SKILL.md": skills["verify-change/SKILL.md"],
	}
}

// TestTierContractIsStatedIdentically (R1.1, R1.4).
func TestTierContractIsStatedIdentically(t *testing.T) {
	for name, body := range tierContractHolders(t) {
		if body == "" {
			t.Fatalf("%s is missing from the shipped templates", name)
		}
		for _, row := range tierTableRows {
			if !strings.Contains(body, row) {
				t.Errorf("%s does not state the tier contract row:\n  want: %s", name, row)
			}
		}
	}
}

// TestNoTemplateImposesTheOldDoctrine (R1.4, R2.1, R2.3, R12.3): the redundancy
// this change removed must not survive in any shipped template. A single leftover
// re-imposes a stricter rule than the contract and the savings evaporate.
func TestNoTemplateImposesTheOldDoctrine(t *testing.T) {
	rules, err := RuleFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := SkillFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := AgentFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	bodies := map[string]string{}
	for n, b := range rules {
		bodies["rules/"+n] = b
	}
	for n, b := range skills {
		bodies["skills/"+n] = b
	}
	for n, b := range agents {
		bodies["agents/"+n] = b
	}

	banned := []struct{ text, why string }{
		{"Widen the net", "the separate full-suite run was collapsed into the single Tier-2 gate (R2.1)"},
		{"diff-report", "the derived diff artifact was removed (R12.3)"},
		{"diff_report", "the derived diff artifact was removed (R12.3)"},
		{"A prior run doesn't count", "the Iron Law is scoped now: a recorded in-session run at the right scope counts (R3.1)"},
	}
	for name, body := range bodies {
		for _, b := range banned {
			if strings.Contains(body, b.text) {
				t.Errorf("%s still contains %q — %s", name, b.text, b.why)
			}
		}
	}
}

// TestScopedIronLawKeepsItsFloor (R3.3, R3.4): scoping the law must not delete it.
// The failure it closes — claiming green with no recorded output at all — stays
// closed, and an orchestrator may now accept a sub-agent's reported result.
func TestScopedIronLawKeepsItsFloor(t *testing.T) {
	skills, err := SkillFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	body := skills["verify-change/SKILL.md"]
	for _, want := range []string{
		"No \"done\", \"it works\", or \"tests pass\" claim without recorded output", // R3.4: the floor
		"at a scope that covers the claim",                                           // R3.1: scoped, not deleted
		"sub-agent's reported result",                                                // R3.3
		"No recorded output at all",                                                  // R3.4 restated as a rejection
	} {
		if !strings.Contains(body, want) {
			t.Errorf("verify-change lost the Iron Law's floor: missing %q", want)
		}
	}
}

// TestTaskExitGateDropsTypecheckAndBuild (R2.3): the expensive checks moved to
// Tier 3, and the templates that drive one task must say so.
func TestTaskExitGateDropsTypecheckAndBuild(t *testing.T) {
	skills, err := SkillFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := AgentFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := RuleFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	// Every template that shows a Tier-2 command must show it in fast mode. The
	// rule file is included because it is the ambient one: its example omitted
	// --fast while its own table one line above demanded fast mode, and copying
	// that command reintroduces per-task coverage everywhere at once.
	for name, body := range map[string]string{
		"skills/tdd-cycle/SKILL.md":     skills["tdd-cycle/SKILL.md"],
		"agents/implementer.md":         agents["implementer.md"],
		"rules/definition-of-done.md":   rules["definition-of-done.md"],
		"skills/verify-change/SKILL.md": skills["verify-change/SKILL.md"],
	} {
		if !strings.Contains(body, "--fast") {
			t.Errorf("%s should record the Tier-2 run in fast mode (R8.2)", name)
		}
		if name == "rules/definition-of-done.md" || name == "skills/verify-change/SKILL.md" {
			continue // these state the contract; the per-task wording below is the agents'''
		}
		if !strings.Contains(strings.ToLower(body), "no typecheck and no build") {
			t.Errorf("%s should state that typecheck and build are not task-exit checks (R2.3)", name)
		}
	}
}

// TestOrchestratorDoesNotReVerify (R4.1, R4.2, R4.4, R7.1): plan-dev takes each
// task's result from the implementer's return message, never from the shared
// evidence artifact, and packs batches by dependency depth.
func TestOrchestratorDoesNotReVerify(t *testing.T) {
	skills, err := SkillFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	body := skills["plan-dev/SKILL.md"]
	for _, want := range []string{
		"Take each task's result from the implementer's return message", // R4.1
		"do not read `specs/<feat>/test-report.json` to decide",         // R4.2
		"re-dispatch it",         // R4.4
		"once per feat",          // R4.3
		"batch integration gate", // R5.1
		"dispatch a fix task naming the failing check",           // R5.2
		"Record which tasks composed the batch",                  // R5.3
		"Pack batches by dependency depth, not by phase heading", // R7.1
		"honor the graph",        // R7.3
		"You own the checkboxes", // single writer of tasks.md
	} {
		if !strings.Contains(body, want) {
			t.Errorf("plan-dev is missing the orchestration contract: %q", want)
		}
	}
}
