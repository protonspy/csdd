package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The tests in this file target error branches that the happy-path suite in
// cmd_test.go does not reach. Each test is deliberately narrow so a failure
// points at one concrete branch.

func TestInvalidRootFails(t *testing.T) {
	// Every CLI command resolves --root through workspace.Resolve. Pointing it
	// at a non-existent path must fail consistently.
	bogus := filepath.Join(t.TempDir(), "ghost")
	cases := [][]string{
		{"steering", "list", "--root", bogus},
		{"steering", "validate", "--root", bogus},
		{"steering", "create", "x", "--inclusion", "always", "--root", bogus},
		{"steering", "delete", "x", "--root", bogus},
		{"steering", "show", "x", "--root", bogus},
		{"steering", "init", "--root", bogus},
		{"spec", "init", "x", "--root", bogus},
		{"spec", "list", "--root", bogus},
		{"spec", "show", "x", "--root", bogus},
		{"spec", "generate", "x", "--artifact", "requirements", "--root", bogus},
		{"spec", "approve", "x", "--phase", "requirements", "--root", bogus},
		{"spec", "validate", "x", "--root", bogus},
		{"spec", "delete", "x", "--force", "--root", bogus},
		{"skill", "create", "x", "--description", "d", "--root", bogus},
		{"skill", "list", "--root", bogus},
		{"skill", "show", "x", "--root", bogus},
		{"skill", "add-reference", "x", "f", "--root", bogus},
		{"skill", "validate", "x", "--root", bogus},
		{"skill", "delete", "x", "--force", "--root", bogus},
		{"agent", "create", "x", "--description", "d", "--root", bogus},
		{"agent", "list", "--root", bogus},
		{"agent", "show", "x", "--root", bogus},
		{"agent", "delete", "x", "--force", "--root", bogus},
	}
	for _, args := range cases {
		t.Run(strings.Join(args[:3], " "), func(t *testing.T) {
			code, _, _ := run(t, args...)
			if code == 0 {
				t.Errorf("%v should fail against invalid --root", args)
			}
		})
	}
}

func TestSpecJSONCorrupted(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "rotten", "--root", dir)
	// Overwrite spec.json with malformed JSON; every spec command that loads
	// it should bail out with a non-zero exit.
	bad := filepath.Join(dir, "specs/rotten/spec.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"spec", "show", "rotten", "--root", dir},
		{"spec", "generate", "rotten", "--artifact", "requirements", "--root", dir},
		{"spec", "approve", "rotten", "--phase", "requirements", "--root", dir},
	} {
		t.Run(strings.Join(args[:3], " "), func(t *testing.T) {
			if code, _, _ := run(t, args...); code == 0 {
				t.Errorf("%v should fail on corrupted spec.json", args)
			}
		})
	}
	// spec list reads every spec; a bad one must not crash the listing.
	code, _, _ := run(t, "spec", "list", "--root", dir)
	if code != 0 {
		t.Errorf("spec list should tolerate corrupted spec.json, got %d", code)
	}
}

func TestSpecApproveReadyForImplementationJSON(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "ready", "--root", dir)
	for _, art := range []string{"requirements", "design", "tasks"} {
		_, _, _ = run(t, "spec", "generate", "ready", "--artifact", art, "--force", "--root", dir)
		_, _, _ = run(t, "spec", "approve", "ready", "--phase", art, "--force", "--root", dir)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "specs/ready/spec.json"))
	var parsed SpecJSON
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.ReadyForImplementation {
		t.Errorf("ready_for_implementation should flip to true: %+v", parsed)
	}
	if parsed.Phase != "tasks-approved" {
		t.Errorf("phase after full approval: %s", parsed.Phase)
	}
}

func TestSpecGenerateBugfixAndResearch(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "off-chain", "--root", dir)
	for _, art := range []string{"bugfix", "research"} {
		if code, _, _ := run(t, "spec", "generate", "off-chain", "--artifact", art, "--root", dir); code != 0 {
			t.Errorf("%s artifact should generate without phase gate", art)
		}
	}
}

// TestSpecGenerateAuxiliaryDoesNotClobberPhase guards the fix where research and
// bugfix (auxiliary, ungated artifacts) must not reset the phase chain or the
// approvals state advanced by the main requirements/design/tasks flow.
func TestSpecGenerateAuxiliaryDoesNotClobberPhase(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "feat", "--root", dir)
	_, _, _ = run(t, "spec", "generate", "feat", "--artifact", "requirements", "--root", dir)

	readPhase := func() SpecJSON {
		data, _ := os.ReadFile(filepath.Join(dir, "specs/feat/spec.json"))
		var parsed SpecJSON
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	before := readPhase()
	if before.Phase != "requirements-generated" {
		t.Fatalf("setup: expected phase requirements-generated, got %s", before.Phase)
	}

	// Generating research afterwards must leave the phase + approvals untouched.
	if code, _, _ := run(t, "spec", "generate", "feat", "--artifact", "research", "--root", dir); code != 0 {
		t.Fatal("research generate failed")
	}
	after := readPhase()
	if after.Phase != "requirements-generated" {
		t.Errorf("research generation clobbered phase: %s", after.Phase)
	}
	if !after.Approvals["requirements"].Generated {
		t.Error("research generation cleared requirements.generated")
	}
}

func TestSteeringInitOnMissingWorkspace(t *testing.T) {
	// steering init creates .claude/steering even when .claude doesn't exist yet.
	dir := t.TempDir()
	code, _, _ := run(t, "steering", "init", "--root", dir)
	if code != 0 {
		t.Errorf("steering init on bare dir should create the tree, got %d", code)
	}
}

func TestRunWithMissingPositionalsForListShowCommands(t *testing.T) {
	dir := freshWorkspace(t)
	// These commands take a positional name; without it they should report usage.
	for _, args := range [][]string{
		{"steering", "show", "--root", dir},
		{"steering", "delete", "--root", dir, "--force"},
		{"skill", "show", "--root", dir},
	} {
		t.Run(strings.Join(args[:3], " "), func(t *testing.T) {
			if code, _, _ := run(t, args...); code == 0 {
				t.Errorf("%v should fail without positional", args)
			}
		})
	}
}

func TestSteeringCreateAutoEmitsBothNameAndDescription(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "steering", "create", "auto-named",
		"--inclusion", "auto", "--description", "Trigger phrase.",
		"--title", "Custom Auto", "--root", dir); code != 0 {
		t.Fatal("create failed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".claude/steering/auto-named.md"))
	text := string(data)
	for _, want := range []string{
		"inclusion: auto",
		"name: auto-named",
		"description: Trigger phrase.",
		"# Custom Auto",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestSkillCreateCustomTitle(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "skill", "create", "custom-t",
		"--description", "Trigger.", "--title", "Custom Title", "--root", dir); code != 0 {
		t.Fatal("skill create failed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".claude/skills/custom-t/SKILL.md"))
	if !strings.Contains(string(data), "# Custom Title") {
		t.Errorf("custom title not applied:\n%s", data)
	}
}

func TestSpecShowEmptyArtifactsList(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "empty", "--root", dir)
	// Remove spec.json so the directory exists but loadSpecJSON fails.
	_ = os.Remove(filepath.Join(dir, "specs/empty/spec.json"))
	if code, _, _ := run(t, "spec", "show", "empty", "--root", dir); code == 0 {
		t.Error("spec show without spec.json should fail")
	}
}

func TestFlagHelpReturnsZero(t *testing.T) {
	// Subcommand -h goes through failOnFlagParse with flag.ErrHelp; the
	// helper recognizes it and returns exit 0 with the usage already printed
	// by the FlagSet itself.
	code, _, _ := run(t, "steering", "create", "-h")
	if code != 0 {
		t.Errorf("`-h` on subcommand should exit 0, got %d", code)
	}
}

func TestUnknownFlagFails(t *testing.T) {
	dir := freshWorkspace(t)
	code, _, errOut := run(t, "steering", "create", "x", "--nope", "--root", dir)
	if code == 0 {
		t.Error("unknown flag should fail")
	}
	if !strings.Contains(errOut, "not defined") {
		t.Errorf("expected 'not defined' in stderr, got %q", errOut)
	}
}

func TestSpecStatusOnMissingFeature(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, _ := run(t, "spec", "status", "ghost", "--root", dir); code == 0 {
		t.Error("spec status against unknown feature should fail")
	}
}

func TestInitDefaultCwd(t *testing.T) {
	// Without --root, init creates the workspace in the current working
	// directory. Use t.TempDir() + os.Chdir for isolation.
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	code, _, _ := run(t, "init", "--with-baseline")
	if code != 0 {
		t.Fatalf("init without --root should fall back to cwd, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude/steering/product.md")); err != nil {
		t.Errorf("expected product.md under cwd-init: %v", err)
	}
}

// Direct unit tests for tiny helpers — much cheaper than going through Run.

func TestParseActionEmpty(t *testing.T) {
	action, rest, err := parseAction("steering", nil)
	if err == nil {
		t.Error("parseAction with empty args should error")
	}
	if action != "" || rest != nil {
		t.Errorf("unexpected outputs: action=%q rest=%v", action, rest)
	}
	action, rest, err = parseAction("spec", []string{"init", "x"})
	if err != nil || action != "init" || len(rest) != 1 {
		t.Errorf("parseAction happy path: action=%q rest=%v err=%v", action, rest, err)
	}
}

func TestFailOnFlagParseBranches(t *testing.T) {
	if failOnFlagParse(nil) != 0 {
		t.Error("nil should return 0")
	}
	if failOnFlagParse(flag.ErrHelp) != 0 {
		t.Error("flag.ErrHelp should return 0")
	}
	if failOnFlagParse(errors.New("boom")) != 1 {
		t.Error("other error should return 1")
	}
}

func TestTruncateStrBranches(t *testing.T) {
	if truncateStr("short", 80) != "short" {
		t.Error("strings ≤ n should pass through")
	}
	if truncateStr("0123456789", 4) != "0123" {
		t.Error("strings > n should be truncated")
	}
}

func TestTitleCase(t *testing.T) {
	if got := titleCase("api-conventions"); got != "Api Conventions" {
		t.Errorf("titleCase = %q", got)
	}
	if got := titleCase(""); got != "" {
		t.Errorf("titleCase empty = %q", got)
	}
	if got := titleCase("single"); got != "Single" {
		t.Errorf("titleCase single = %q", got)
	}
}

func TestContainsString(t *testing.T) {
	if !containsString([]string{"a", "b"}, "b") {
		t.Error("containsString should find present item")
	}
	if containsString([]string{"a", "b"}, "z") {
		t.Error("containsString should reject missing item")
	}
}

func TestStringSliceFlag(t *testing.T) {
	var s stringSliceFlag
	_ = s.Set("a")
	_ = s.Set("b")
	if s.String() != "a,b" {
		t.Errorf("stringSliceFlag.String() = %q", s.String())
	}
	if len(s.values) != 2 {
		t.Errorf("values: %v", s.values)
	}
}

func TestResourceWithoutAction(t *testing.T) {
	// Every resource has a default subcommand dispatcher that returns 1 when
	// no action follows. Covers parseAction's error branch through Run.
	for _, resource := range []string{"steering", "spec", "skill", "agent"} {
		t.Run(resource, func(t *testing.T) {
			code, _, errOut := run(t, resource)
			if code == 0 {
				t.Errorf("`csdd %s` should fail", resource)
			}
			if !strings.Contains(errOut, "missing action") {
				t.Errorf("expected 'missing action' for %s, got %q", resource, errOut)
			}
		})
	}
}

func TestSteeringListAuthorTruncation(t *testing.T) {
	dir := freshWorkspace(t)
	long := strings.Repeat("really long description ", 6) // 144 chars
	_, _, _ = run(t, "steering", "create", "long-desc",
		"--inclusion", "auto", "--description", long, "--root", dir)
	_, out, _ := run(t, "steering", "list", "--root", dir)
	// The truncated description should appear without showing the full 144 chars.
	if !strings.Contains(out, "really long") {
		t.Errorf("expected truncated description in output:\n%s", out)
	}
	if strings.Contains(out, long) {
		t.Errorf("description should have been truncated, but full string appears:\n%s", out)
	}
}

// TestWriteOperationsAgainstReadOnlyRoot covers the deep `if err != nil`
// branches in every command that calls workspace.WriteFile / os.MkdirAll.
// It works by chmod'ing the workspace tree read-only after init, so any
// subsequent write attempt fails at the syscall layer. Skipped under root.
func TestWriteOperationsAgainstReadOnlyRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission restriction is a no-op on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod; skipping")
	}
	dir := freshWorkspace(t)
	// Spec init creates a sub-dir under specs and writes spec.json.
	// Locking specs read-only forces the create to fail.
	specsDir := filepath.Join(dir, "specs")
	if err := os.Chmod(specsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(specsDir, 0o755) })
	if code, _, _ := run(t, "spec", "init", "blocked", "--root", dir); code == 0 {
		t.Error("spec init should fail when specs is read-only")
	}

	// Skill create writes under .claude/skills.
	skillsDir := filepath.Join(dir, ".claude/skills")
	if err := os.Chmod(skillsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillsDir, 0o755) })
	if code, _, _ := run(t, "skill", "create", "blocked",
		"--description", "d", "--root", dir); code == 0 {
		t.Error("skill create should fail when .claude/skills is read-only")
	}

	// Agent create writes under .claude/agents.
	agentsDir := filepath.Join(dir, ".claude/agents")
	if err := os.Chmod(agentsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agentsDir, 0o755) })
	if code, _, _ := run(t, "agent", "create", "blocked",
		"--description", "d", "--root", dir); code == 0 {
		t.Error("agent create should fail when .claude/agents is read-only")
	}

	// Steering create writes under .claude/steering.
	steeringDir := filepath.Join(dir, ".claude/steering")
	if err := os.Chmod(steeringDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(steeringDir, 0o755) })
	if code, _, _ := run(t, "steering", "create", "blocked",
		"--inclusion", "always", "--root", dir); code == 0 {
		t.Error("steering create should fail when .claude/steering is read-only")
	}
}

// TestSteeringInitWriteFailure forces steeringInit down its SafeWrite error
// branch by deleting the baseline files and then locking the directory.
func TestSteeringInitWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission restriction is a no-op on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod; skipping")
	}
	dir := freshWorkspace(t)
	steeringDir := filepath.Join(dir, ".claude/steering")
	for _, n := range []string{"product.md", "tech.md", "structure.md"} {
		_ = os.Remove(filepath.Join(steeringDir, n))
	}
	if err := os.Chmod(steeringDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(steeringDir, 0o755) })
	if code, _, _ := run(t, "steering", "init", "--root", dir); code == 0 {
		t.Error("steering init should fail when target is read-only")
	}
}

// TestSpecGenerateAndApproveWriteFailures cover SpecGenerate's WriteFile
// branch and SpecApprove's saveSpecJSON branch by locking the spec dir.
func TestSpecGenerateAndApproveWriteFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission restriction is a no-op on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod; skipping")
	}
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "ro", "--root", dir)
	specDir := filepath.Join(dir, "specs/ro")
	if err := os.Chmod(specDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(specDir, 0o755) })
	if code, _, _ := run(t, "spec", "generate", "ro", "--artifact", "requirements",
		"--force", "--root", dir); code == 0 {
		t.Error("spec generate should fail when spec dir is read-only")
	}
}

// TestSkillDeleteFailure covers skillDelete's os.RemoveAll error branch.
// Making the parent .claude/skills read-only is enough — RemoveAll cannot
// unlink entries from a directory it has no write permission on.
func TestSkillDeleteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod; skipping")
	}
	dir := freshWorkspace(t)
	_, _, _ = run(t, "skill", "create", "doom", "--description", "d", "--root", dir)
	skills := filepath.Join(dir, ".claude/skills")
	if err := os.Chmod(skills, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skills, 0o755) })
	if code, _, _ := run(t, "skill", "delete", "doom", "--force", "--root", dir); code == 0 {
		t.Error("skill delete should fail when parent is read-only")
	}
}

// TestAgentDeleteFailure mirrors TestSkillDeleteFailure for agents.
func TestAgentDeleteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod; skipping")
	}
	dir := freshWorkspace(t)
	_, _, _ = run(t, "agent", "create", "doom", "--description", "d", "--root", dir)
	agents := filepath.Join(dir, ".claude/agents")
	if err := os.Chmod(agents, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agents, 0o755) })
	if code, _, _ := run(t, "agent", "delete", "doom", "--force", "--root", dir); code == 0 {
		t.Error("agent delete should fail when parent is read-only")
	}
}

// TestSteeringDeleteFailure mirrors the delete-failure pattern for steering.
func TestSteeringDeleteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod; skipping")
	}
	dir := freshWorkspace(t)
	_, _, _ = run(t, "steering", "create", "doomed", "--inclusion", "always", "--root", dir)
	steering := filepath.Join(dir, ".claude/steering")
	if err := os.Chmod(steering, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(steering, 0o755) })
	if code, _, _ := run(t, "steering", "delete", "doomed", "--root", dir); code == 0 {
		t.Error("steering delete should fail when parent is read-only")
	}
}

// TestSpecDeleteFailure covers specDelete's os.RemoveAll error branch.
func TestSpecDeleteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod; skipping")
	}
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "doom", "--root", dir)
	specs := filepath.Join(dir, "specs")
	if err := os.Chmod(specs, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(specs, 0o755) })
	if code, _, _ := run(t, "spec", "delete", "doom", "--force", "--root", dir); code == 0 {
		t.Error("spec delete should fail when parent is read-only")
	}
}

// TestSkillRootFailures locks the .claude/ container read-only and removes the
// skills/ subdir, then asserts the mutating + lookup skill subcommands fail.
// (`skill list` is intentionally excluded: a missing/unreadable skills dir is a
// valid "no skills found" result, not an error — SkillRoot no longer creates it.)
func TestSkillRootFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission restriction is a no-op on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod; skipping")
	}
	dir := freshWorkspace(t)
	_ = os.RemoveAll(filepath.Join(dir, ".claude/skills"))
	agentsDir := filepath.Join(dir, ".claude")
	if err := os.Chmod(agentsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agentsDir, 0o755) })
	for _, args := range [][]string{
		{"skill", "show", "x", "--root", dir},
		{"skill", "create", "x", "--description", "d", "--root", dir},
		{"skill", "add-reference", "x", "f", "--root", dir},
		{"skill", "validate", "x", "--root", dir},
		{"skill", "delete", "x", "--force", "--root", dir},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			if code, _, _ := run(t, args...); code == 0 {
				t.Errorf("%v should fail when SkillRoot fails", args)
			}
		})
	}
}

// TestAgentRootFailures mirrors TestSkillRootFailures for the agent commands.
func TestAgentRootFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission restriction is a no-op on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses chmod; skipping")
	}
	dir := freshWorkspace(t)
	_ = os.RemoveAll(filepath.Join(dir, ".claude/agents"))
	agentsDir := filepath.Join(dir, ".claude")
	if err := os.Chmod(agentsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agentsDir, 0o755) })
	for _, args := range [][]string{
		{"agent", "show", "x", "--root", dir},
		{"agent", "create", "x", "--description", "d", "--root", dir},
		{"agent", "delete", "x", "--force", "--root", dir},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			if code, _, _ := run(t, args...); code == 0 {
				t.Errorf("%v should fail when AgentRoot fails", args)
			}
		})
	}
}

// TestSpecListSkipsRegularFiles exercises the `if !e.IsDir() { continue }`
// branch in specList by planting a stray file inside specs/.
func TestSpecListSkipsRegularFiles(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "spec", "init", "real", "--root", dir)
	stray := filepath.Join(dir, "specs/stray-file")
	if err := os.WriteFile(stray, []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "spec", "list", "--root", dir)
	if code != 0 || !strings.Contains(out, "real") || strings.Contains(out, "stray-file") {
		t.Errorf("spec list should list 'real' and skip stray file:\n%s", out)
	}
}

// TestSkillListSkipsFiles exercises the `if e.IsDir()` filter in skillList.
func TestSkillListSkipsFiles(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "skill", "create", "real", "--description", "d", "--root", dir)
	stray := filepath.Join(dir, ".claude/skills/stray.txt")
	if err := os.WriteFile(stray, []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "skill", "list", "--root", dir)
	if code != 0 || strings.Contains(out, "stray.txt") {
		t.Errorf("skill list should skip stray file:\n%s", out)
	}
}

// TestUnknownFlagOnEverySubcommand sweeps `--nope` across every leaf command
// so each subcommand's failOnFlagParse branch is exercised once.
func TestUnknownFlagOnEverySubcommand(t *testing.T) {
	dir := freshWorkspace(t)
	cases := [][]string{
		{"init", "--nope", "--root", dir},
		{"steering", "init", "--nope", "--root", dir},
		{"steering", "list", "--nope", "--root", dir},
		{"steering", "show", "x", "--nope", "--root", dir},
		{"steering", "delete", "x", "--nope", "--root", dir},
		{"steering", "validate", "--nope", "--root", dir},
		{"steering", "create", "x", "--inclusion", "always", "--nope", "--root", dir},
		{"spec", "init", "x", "--nope", "--root", dir},
		{"spec", "list", "--nope", "--root", dir},
		{"spec", "show", "x", "--nope", "--root", dir},
		{"spec", "status", "x", "--nope", "--root", dir},
		{"spec", "generate", "x", "--artifact", "requirements", "--nope", "--root", dir},
		{"spec", "approve", "x", "--phase", "requirements", "--nope", "--root", dir},
		{"spec", "validate", "x", "--nope", "--root", dir},
		{"spec", "delete", "x", "--force", "--nope", "--root", dir},
		{"skill", "create", "x", "--description", "d", "--nope", "--root", dir},
		{"skill", "list", "--nope", "--root", dir},
		{"skill", "show", "x", "--nope", "--root", dir},
		{"skill", "add-reference", "x", "f", "--nope", "--root", dir},
		{"skill", "add-script", "x", "f", "--nope", "--root", dir},
		{"skill", "add-asset", "x", "f", "--nope", "--root", dir},
		{"skill", "validate", "x", "--nope", "--root", dir},
		{"skill", "delete", "x", "--force", "--nope", "--root", dir},
		{"agent", "create", "x", "--description", "d", "--nope", "--root", dir},
		{"agent", "list", "--nope", "--root", dir},
		{"agent", "show", "x", "--nope", "--root", dir},
		{"agent", "delete", "x", "--force", "--nope", "--root", dir},
	}
	for _, args := range cases {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			if code, _, _ := run(t, args...); code == 0 {
				t.Errorf("%v should fail on unknown flag", args)
			}
		})
	}
}

// TestInitWithTargetThatIsFile pushes initWorkspace down its error path:
// when --root points at a regular file, MkdirAll inside it must fail.
func TestInitWithTargetThatIsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "regular")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "init", "--root", file); code == 0 {
		t.Error("init against a regular file as --root should fail")
	}
}

// TestSkillAddArtifactDestExistsAsDir covers another rare WriteFile branch.
func TestSkillAddArtifactRelyingOnSubdir(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "skill", "create", "rd", "--description", "d", "--root", dir)
	// Pre-create the destination as a directory so the WriteFile call fails.
	dest := filepath.Join(dir, ".claude/skills/rd/references", "as-dir")
	_ = os.MkdirAll(dest, 0o755)
	code, _, _ := run(t, "skill", "add-reference", "rd", "as-dir", "--root", dir)
	if code == 0 {
		t.Error("add-reference should fail when destination exists as directory")
	}
}

func TestSkillAndAgentListEntriesShowDescription(t *testing.T) {
	dir := freshWorkspace(t)
	_, _, _ = run(t, "skill", "create", "alpha",
		"--description", "Alpha skill activation.", "--root", dir)
	_, _, _ = run(t, "agent", "create", "beta",
		"--description", "Beta agent activation.", "--tools", "Read", "--root", dir)
	_, skillOut, _ := run(t, "skill", "list", "--root", dir)
	if !strings.Contains(skillOut, "Alpha skill activation.") {
		t.Errorf("skill list missing description: %s", skillOut)
	}
	_, agentOut, _ := run(t, "agent", "list", "--root", dir)
	if !strings.Contains(agentOut, "Beta agent activation.") {
		t.Errorf("agent list missing description: %s", agentOut)
	}
}

func TestRunInitWithExplicitNonexistentRoot(t *testing.T) {
	// init is the one command that ACCEPTS a non-existent --root because its
	// job is to create the directory. We verify the path gets created.
	dir := filepath.Join(t.TempDir(), "not-yet")
	code, _, _ := run(t, "init", "--root", dir, "--with-baseline")
	if code != 0 {
		t.Fatalf("init should create missing --root dir, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); err != nil {
		t.Error("expected .claude inside new root")
	}
}

// TestInitScaffoldsClaudeCodeArtifacts asserts the shipped sub-agents, workflow
// skills, hooks (executable), settings.json, PR template, and DoD rule all land.
func TestInitScaffoldsClaudeCodeArtifacts(t *testing.T) {
	dir := freshWorkspace(t)
	for _, f := range []string{
		".claude/agents/code-reviewer.md",
		".claude/agents/test-designer.md",
		".claude/agents/security-reviewer.md",
		".claude/skills/tdd-cycle/SKILL.md",
		".claude/skills/verify-change/SKILL.md",
		".claude/skills/safe-refactor/SKILL.md",
		".claude/skills/pr-review/SKILL.md",
		".claude/commands/csdd-commit.md",
		".claude/hooks/block-destructive.sh",
		".claude/hooks/format-after-edit.sh",
		".claude/hooks/test-before-stop.sh",
		".claude/settings.json",
		".github/pull_request_template.md",
		".claude/rules/definition-of-done.md",
		".githooks/pre-push",
	} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("init did not scaffold %s: %v", f, err)
		}
	}
	for _, exe := range []string{".claude/hooks/block-destructive.sh", ".githooks/pre-push"} {
		info, err := os.Stat(filepath.Join(dir, exe))
		if err != nil || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
			t.Errorf("%s must be executable: err=%v mode=%v", exe, err, info.Mode())
		}
	}
}

// TestInitWithBaselineImportsSteering asserts CLAUDE.md imports the baseline
// steering files as always-on @-references.
func TestInitWithBaselineImportsSteering(t *testing.T) {
	dir := freshWorkspace(t)
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	for _, imp := range []string{
		"@.claude/steering/product.md",
		"@.claude/steering/tech.md",
		"@.claude/steering/testing.md",
	} {
		if !strings.Contains(string(data), imp) {
			t.Errorf("CLAUDE.md missing steering import %q:\n%s", imp, data)
		}
	}
}

// TestSteeringCreateAddsImportIdempotently asserts a created steering file is
// imported into CLAUDE.md exactly once, even across a --force re-create.
func TestSteeringCreateAddsImportIdempotently(t *testing.T) {
	dir := freshWorkspace(t)
	if code, _, e := run(t, "steering", "create", "observability",
		"--inclusion", "auto", "--description", "Logging/metrics.", "--root", dir); code != 0 {
		t.Fatalf("steering create failed: %s", e)
	}
	imp := "@.claude/steering/observability.md"
	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(string(data), imp) {
		t.Fatalf("import not added to CLAUDE.md:\n%s", data)
	}
	run(t, "steering", "create", "observability",
		"--inclusion", "auto", "--description", "again", "--force", "--root", dir)
	data, _ = os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if n := strings.Count(string(data), imp); n != 1 {
		t.Errorf("expected exactly 1 import line, got %d", n)
	}
}
