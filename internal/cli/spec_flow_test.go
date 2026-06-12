package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protonspy/csdd/internal/templater"
)

// readSpecFlow returns the raw development_flow field written to a spec.json.
func readSpecFlow(t *testing.T, dir, feature string) (SpecJSON, []byte) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "specs", feature, "spec.json"))
	if err != nil {
		t.Fatalf("read spec.json: %v", err)
	}
	var s SpecJSON
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("unmarshal spec.json: %v", err)
	}
	return s, b
}

// Req 1.1 / 8.3: each valid flow is persisted and surfaced by `spec show`.
func TestSpecInitFlowPersistedAndShown(t *testing.T) {
	for _, flow := range []string{"unit", "tdd", "tdd-e2e"} {
		flow := flow
		t.Run(flow, func(t *testing.T) {
			dir := freshWorkspace(t)
			if code, _, errOut := run(t, "spec", "init", "feat-"+flow, "--flow", flow, "--root", dir); code != 0 {
				t.Fatalf("init --flow %s: code=%d err=%q", flow, code, errOut)
			}
			s, _ := readSpecFlow(t, dir, "feat-"+flow)
			if s.DevelopmentFlow != flow {
				t.Errorf("development_flow = %q, want %q", s.DevelopmentFlow, flow)
			}
			_, showOut, _ := run(t, "spec", "show", "feat-"+flow, "--root", dir)
			if !strings.Contains(showOut, "flow:") || !strings.Contains(showOut, flow) {
				t.Errorf("show output missing flow %q: %s", flow, showOut)
			}
		})
	}
}

// Req 1.2: an invalid flow is rejected (exit 1) and writes no spec.json.
func TestSpecInitFlowInvalid(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, errOut := run(t, "spec", "init", "bad-flow", "--flow", "xunit", "--root", dir)
	if code != 1 {
		t.Errorf("invalid flow: code=%d, want 1", code)
	}
	if !strings.Contains(errOut, "flow") {
		t.Errorf("error should mention flow: %q", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "specs", "bad-flow")); !os.IsNotExist(err) {
		t.Error("spec dir must not be created when --flow is invalid")
	}
}

// Req 1.3 / 2.2: with no steering default, an omitted flow resolves to tdd.
func TestSpecInitFlowDefaultsToTdd(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, errOut := run(t, "spec", "init", "no-flow", "--root", dir); code != 0 {
		t.Fatalf("init: code=%d err=%q", code, errOut)
	}
	s, _ := readSpecFlow(t, dir, "no-flow")
	if s.DevelopmentFlow != "tdd" {
		t.Errorf("development_flow = %q, want tdd", s.DevelopmentFlow)
	}
}

// Req 1.3 / 2.1: a steering-declared default is applied when flow is omitted.
func TestSpecInitFlowSteeringDefault(t *testing.T) {
	dir := freshWorkspace(t)
	steering := filepath.Join(dir, ".claude", "steering", "zzz-flow-default.md")
	body := "---\ninclusion: manual\ndefault_development_flow: tdd-e2e\n---\n# default\n"
	if err := os.WriteFile(steering, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run(t, "spec", "init", "inherits", "--root", dir); code != 0 {
		t.Fatalf("init: code=%d err=%q", code, errOut)
	}
	s, _ := readSpecFlow(t, dir, "inherits")
	if s.DevelopmentFlow != "tdd-e2e" {
		t.Errorf("development_flow = %q, want tdd-e2e", s.DevelopmentFlow)
	}
	// An explicit --flow overrides the steering default.
	if code, _, _ := run(t, "spec", "init", "explicit", "--flow", "unit", "--root", dir); code != 0 {
		t.Fatal("init explicit failed")
	}
	s2, _ := readSpecFlow(t, dir, "explicit")
	if s2.DevelopmentFlow != "unit" {
		t.Errorf("explicit flow = %q, want unit", s2.DevelopmentFlow)
	}
}

// Req 2.2: an invalid steering default does not corrupt init; it falls back to tdd.
func TestResolveDefaultFlowInvalidFallsBack(t *testing.T) {
	dir := freshWorkspace(t)
	steering := filepath.Join(dir, ".claude", "steering", "zzz-bad.md")
	if err := os.WriteFile(steering, []byte("---\ninclusion: manual\ndefault_development_flow: bogus\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveDefaultFlow(dir); got != "tdd" {
		t.Errorf("resolveDefaultFlow with invalid steering = %q, want tdd", got)
	}
}

// Req 3.1: a legacy spec.json with no development_flow is read as tdd.
func TestEffectiveFlowLegacy(t *testing.T) {
	if got := effectiveFlow(""); got != "tdd" {
		t.Errorf("effectiveFlow(\"\") = %q, want tdd", got)
	}
	if got := effectiveFlow("unit"); got != "unit" {
		t.Errorf("effectiveFlow(unit) = %q, want unit", got)
	}
}

// Req 3.3: saving a spec with no flow omits the field, so legacy files stay clean.
func TestSaveSpecJSONOmitsEmptyFlow(t *testing.T) {
	dir := t.TempDir()
	if err := saveSpecJSON(dir, SpecJSON{FeatureName: "legacy", Phase: "initialized"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "development_flow") {
		t.Errorf("empty flow must be omitted from spec.json, got:\n%s", b)
	}
}

func TestValidDevelopmentFlow(t *testing.T) {
	for _, ok := range []string{"unit", "tdd", "tdd-e2e"} {
		if !validDevelopmentFlow(ok) {
			t.Errorf("validDevelopmentFlow(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "TDD", "e2e", "unit-test"} {
		if validDevelopmentFlow(bad) {
			t.Errorf("validDevelopmentFlow(%q) = true, want false", bad)
		}
	}
}

// Req 1.1: the exported op is the single source of truth (CLI + TUI call it).
func TestSpecInitOpWritesFlow(t *testing.T) {
	dir := freshWorkspace(t)
	if err := SpecInit(templater.FS, SpecInitOptions{Root: dir, Feature: "via-op", Flow: "unit"}); err != nil {
		t.Fatalf("SpecInit: %v", err)
	}
	s, _ := readSpecFlow(t, dir, "via-op")
	if s.DevelopmentFlow != "unit" {
		t.Errorf("op-written flow = %q, want unit", s.DevelopmentFlow)
	}
	if err := SpecInit(templater.FS, SpecInitOptions{Root: dir, Feature: "bad", Flow: "nope"}); err == nil {
		t.Error("SpecInit with invalid flow should error")
	}
}
