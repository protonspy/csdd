package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const sampleLcov = `TN:
SF:src/foo.go
DA:1,1
DA:2,1
DA:3,0
LF:3
LH:2
end_of_record
SF:src/bar.go
DA:1,0
DA:2,0
LF:2
LH:0
end_of_record
`

const sampleJUnit = `<?xml version="1.0"?>
<testsuites>
  <testsuite name="pkg/foo" tests="3" failures="1" skipped="1" time="0.5">
    <testcase name="TestA" time="0.1"/>
    <testcase name="TestB" time="0.2"><failure message="boom">stack</failure></testcase>
    <testcase name="TestC" time="0.0"><skipped/></testcase>
  </testsuite>
</testsuites>
`

const sampleCobertura = `<?xml version="1.0"?>
<coverage line-rate="0.5" lines-covered="2" lines-valid="4">
  <packages>
    <package name="p">
      <classes>
        <class filename="a.py" line-rate="1.0">
          <lines><line number="1" hits="1"/><line number="2" hits="3"/></lines>
        </class>
        <class filename="b.py" line-rate="0.0">
          <lines><line number="1" hits="0"/><line number="2" hits="0"/></lines>
        </class>
      </classes>
    </package>
  </packages>
</coverage>
`

func TestLoadTestReportLcovJUnit(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"coverage/lcov.info": sampleLcov,
		"junit.xml":          sampleJUnit,
	})
	rep := LoadTestReport(root)

	if rep.Tests == nil {
		t.Fatal("expected tests")
	}
	if rep.Tests.Total != 3 || rep.Tests.Passed != 1 || rep.Tests.Failed != 1 || rep.Tests.Skipped != 1 {
		t.Errorf("tests = %+v", rep.Tests)
	}
	if len(rep.Tests.Failures) != 1 || rep.Tests.Failures[0].Name != "TestB" {
		t.Errorf("failures = %+v", rep.Tests.Failures)
	}

	if rep.Coverage == nil {
		t.Fatal("expected coverage")
	}
	if rep.Coverage.Format != "lcov" || rep.Coverage.Lines != 5 || rep.Coverage.Covered != 2 {
		t.Errorf("coverage = %+v, want lcov 2/5", rep.Coverage)
	}
	if rep.Coverage.Pct < 39.9 || rep.Coverage.Pct > 40.1 {
		t.Errorf("coverage pct = %v, want ~40", rep.Coverage.Pct)
	}
	// Lowest-coverage file first (most actionable).
	if rep.Coverage.Files[0].Path != "src/bar.go" {
		t.Errorf("first file = %q, want src/bar.go (lowest)", rep.Coverage.Files[0].Path)
	}
}

// sampleGoCover is a `go test -coverprofile` profile: foo.go has 3 statements
// with 2 covered, bar.go has 3 statements all covered → 5/6 overall.
const sampleGoCover = `mode: set
github.com/acme/proj/foo.go:10.34,12.2 2 1
github.com/acme/proj/foo.go:14.2,16.3 1 0
github.com/acme/proj/bar.go:5.20,7.4 3 1
`

// sampleJacoco is a JaCoCo XML report: Foo.java 3/4 lines, Bar.java 0/2 → 3/6.
const sampleJacoco = `<?xml version="1.0" encoding="UTF-8"?>
<report name="proj">
  <package name="com/acme">
    <sourcefile name="Foo.java">
      <counter type="INSTRUCTION" missed="3" covered="7"/>
      <counter type="LINE" missed="1" covered="3"/>
    </sourcefile>
    <sourcefile name="Bar.java">
      <counter type="LINE" missed="2" covered="0"/>
    </sourcefile>
  </package>
  <counter type="LINE" missed="3" covered="3"/>
</report>
`

func TestLoadTestReportGoCover(t *testing.T) {
	root := writeWorkspace(t, map[string]string{"coverage.out": sampleGoCover})
	rep := LoadTestReport(root)
	if rep.Coverage == nil || rep.Coverage.Format != "gocover" {
		t.Fatalf("coverage = %+v", rep.Coverage)
	}
	if rep.Coverage.Covered != 5 || rep.Coverage.Lines != 6 {
		t.Errorf("gocover = %d/%d, want 5/6", rep.Coverage.Covered, rep.Coverage.Lines)
	}
	if rep.Coverage.Files[0].Path != "github.com/acme/proj/foo.go" {
		t.Errorf("first file = %q, want foo.go (lowest coverage first)", rep.Coverage.Files[0].Path)
	}
}

func TestLoadTestReportJacoco(t *testing.T) {
	root := writeWorkspace(t, map[string]string{"jacoco.xml": sampleJacoco})
	rep := LoadTestReport(root)
	if rep.Coverage == nil || rep.Coverage.Format != "jacoco" {
		t.Fatalf("coverage = %+v", rep.Coverage)
	}
	if rep.Coverage.Covered != 3 || rep.Coverage.Lines != 6 {
		t.Errorf("jacoco = %d/%d, want 3/6", rep.Coverage.Covered, rep.Coverage.Lines)
	}
	if rep.Coverage.Files[0].Path != "com/acme/Bar.java" {
		t.Errorf("first file = %q, want Bar.java (lowest coverage first)", rep.Coverage.Files[0].Path)
	}
}

func TestParseCoverageFileFormats(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"go/coverage.out": sampleGoCover,
		"java/jacoco.xml": sampleJacoco,
		"ts/lcov.info":    sampleLcov,
	})
	for _, tc := range []struct{ path, format string }{
		{"go/coverage.out", "gocover"},
		{"java/jacoco.xml", "jacoco"},
		{"ts/lcov.info", "lcov"},
	} {
		cov, err := ParseCoverageFile(root, filepath.Join(root, tc.path))
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if cov.Format != tc.format {
			t.Errorf("%s format = %q, want %q", tc.path, cov.Format, tc.format)
		}
	}
}

func TestValidateTestCommandForLang(t *testing.T) {
	cases := []struct {
		lang, cmd string
		wantOK    bool
	}{
		{"python", "pytest --junitxml=junit.xml", true},
		{"python", "go test ./...", false}, // wrong tool for the language
		{"java", "mvn -q test jacoco:report", true},
		{"java", "pytest", false},
		{"go", "gotestsum --junitfile=junit.xml", true},
		{"typescript", "npx vitest run --coverage", true},
		{"rust", "cargo nextest run", true},
		{"go", "", true},            // empty cmd → nothing to validate
		{"cobol", "whatever", true}, // unknown lang → cannot validate, don't complain
	}
	for _, c := range cases {
		ok, _ := ValidateTestCommandForLang(c.lang, c.cmd)
		if ok != c.wantOK {
			t.Errorf("ValidateTestCommandForLang(%q, %q) = %v, want %v", c.lang, c.cmd, ok, c.wantOK)
		}
	}
}

func TestLoadTestReportCobertura(t *testing.T) {
	root := writeWorkspace(t, map[string]string{"coverage.xml": sampleCobertura})
	rep := LoadTestReport(root)
	if rep.Coverage == nil || rep.Coverage.Format != "cobertura" {
		t.Fatalf("coverage = %+v", rep.Coverage)
	}
	if rep.Coverage.Covered != 2 || rep.Coverage.Lines != 4 {
		t.Errorf("cobertura cov = %d/%d, want 2/4", rep.Coverage.Covered, rep.Coverage.Lines)
	}
}

func TestSpecDetailReadsReport(t *testing.T) {
	root := writeWorkspace(t, map[string]string{
		"specs/f/spec.json": `{"phase":"x"}`,
		"specs/f/test-report.json": `{"feature":"f","updatedAt":"2026-01-01T00:00:00Z",` +
			`"tests":{"total":5,"passed":4,"failed":1,"skipped":0},` +
			`"coverage":{"pct":80,"covered":80,"lines":100}}`,
	})
	d, err := LoadSpecDetail(root, "f")
	if err != nil {
		t.Fatal(err)
	}
	if d.Report == nil || d.Report.Tests == nil || d.Report.Coverage == nil {
		t.Fatalf("report = %+v", d.Report)
	}
	if d.Report.Tests.Passed != 4 || d.Report.Tests.Failed != 1 || d.Report.Coverage.Pct != 80 {
		t.Errorf("report = %+v / tests=%+v / cov=%+v", d.Report, d.Report.Tests, d.Report.Coverage)
	}
}

func TestSpecDetailNoReport(t *testing.T) {
	root := writeWorkspace(t, map[string]string{"specs/f/spec.json": `{"phase":"x"}`})
	d, _ := LoadSpecDetail(root, "f")
	if d.Report != nil {
		t.Errorf("expected nil report when no test-report.json")
	}
}

func TestLoadTestReportEmpty(t *testing.T) {
	rep := LoadTestReport(t.TempDir())
	if rep.Tests != nil || rep.Coverage != nil {
		t.Errorf("empty workspace should have no reports: %+v", rep)
	}
	if rep.Sources == nil {
		t.Errorf("sources should be non-nil []")
	}
}

// --- evidence integrity, attribution, and command modes (plan r4) ------------

// TestDetectTestScopeFlags: the marker check proves only that the right TOOL ran.
// This proves we also notice when the command told that tool to run less than the
// whole suite — the failure that wrote a green report with a test file excluded.
func TestDetectTestScopeFlags(t *testing.T) {
	cases := []struct {
		name string
		lang string
		cmd  string
		want []string
	}{
		{
			// The exact command from the reference run that recorded green with a
			// test file excluded, twice, raising no attention at all.
			name: "the real-world exclusion",
			lang: "python",
			cmd:  "uv run pytest --junitxml=junit.xml --cov --cov-report=xml --ignore=tests/unit/test_pinned_embedder.py",
			want: []string{"--ignore"},
		},
		{"pytest -k selection", "python", "pytest -k not_slow --junitxml=junit.xml", []string{"-k"}},
		{"pytest several", "py", "pytest --deselect a::b -x", []string{"--deselect", "-x"}},
		{"jest ignore", "typescript", "npx jest --ci --testPathIgnorePatterns=e2e", []string{"--testPathIgnorePatterns"}},
		{"jest -t", "js", "npx jest -t 'only this'", []string{"-t"}},
		{"go -run", "go", "gotestsum --junitfile=junit.xml -- -run TestFoo ./...", []string{"-run"}},
		{"maven -Dtest", "java", "mvn -q test -Dtest=FooTest", []string{"-Dtest"}},
		{"rust --skip", "rust", "cargo nextest run --profile ci --skip slow", []string{"--skip"}},

		// The honest defaults must never raise an attention, or the signal is noise.
		{"python default is clean", "python", langTestCommands["python"], nil},
		{"typescript default is clean", "typescript", langTestCommands["typescript"], nil},
		{"java default is clean", "java", langTestCommands["java"], nil},
		{"go default is clean", "go", langTestCommands["go"], nil},
		{"rust default is clean", "rust", langTestCommands["rust"], nil},

		// Token-based matching: these LOOK like flags as substrings but are not.
		{"substring is not a flag", "python", "pytest --junitxml=junit.xml # covers -k cases", []string{"-k"}},
		{"path containing -m is clean", "go", "gotestsum --junitfile=junit.xml -- ./cmd/foo-m/...", nil},
		{"unknown language yields nothing", "cobol", "cobol-test --ignore x", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectTestScopeFlags(tc.lang, tc.cmd)
			if len(got) != len(tc.want) {
				t.Fatalf("flags = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("flags = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestFastTestCommandParity: every language with an evidence command has a fast
// one, the fast one drops coverage, and it still emits the JUnit report the
// evidence contract is parsed from. A fast command with no JUnit output would
// silently record nothing.
func TestFastTestCommandParity(t *testing.T) {
	covMarkers := []string{"--cov", "--coverage", "coverageReporters", "jacoco:report", "-coverprofile", "llvm-cov"}
	for alias, evidence := range langTestCommands {
		fast, ok := FastTestCommand(alias)
		if !ok {
			t.Errorf("%s has an evidence command but no fast one", alias)
			continue
		}
		for _, m := range covMarkers {
			if strings.Contains(fast, m) {
				t.Errorf("fast command for %s still collects coverage (%s): %s", alias, m, fast)
			}
		}
		if strings.Contains(evidence, "junit") && !strings.Contains(fast, "junit") {
			t.Errorf("fast command for %s dropped its JUnit report: %s", alias, fast)
		}
		if DetectTestScopeFlags(alias, fast) != nil {
			t.Errorf("fast command for %s narrows the suite: %s", alias, fast)
		}
	}
	if _, ok := FastTestCommand("cobol"); ok {
		t.Errorf("an unsupported language must not report a fast command")
	}
}

// TestWriteSpecReportPreservesConcurrentTasks (R6.1, R6.2): two implementers
// recording evidence for different tasks of the same spec must both survive. The
// old writer rebuilt the file from scratch, so the second one erased the first.
func TestWriteSpecReportPreservesConcurrentTasks(t *testing.T) {
	dir := t.TempDir()
	write := func(taskID string, passed int) {
		if err := WriteSpecReport(dir, taskID, SpecReport{
			Feature:   "f",
			UpdatedAt: "2026-07-19T00:0" + taskID + ":00Z",
			Command:   "pytest --junitxml=junit.xml",
			Tests:     &SpecTestCounts{Total: passed, Passed: passed},
		}); err != nil {
			t.Fatal(err)
		}
	}
	write("1", 5)
	write("2", 7)

	got := LoadSpecReport(dir)
	if got == nil {
		t.Fatal("no report written")
	}
	if len(got.Tasks) != 2 {
		t.Fatalf("both tasks' results must survive, got %+v", got.Tasks)
	}
	if got.Tasks["1"].Tests.Passed != 5 || got.Tasks["2"].Tests.Passed != 7 {
		t.Errorf("per-task counts wrong: %+v", got.Tasks)
	}
	// R6.3: the top level stays the latest run's rollup, so the dashboard and the
	// plan runner's verdict gate keep reading the same shape they always did.
	if got.Tests == nil || got.Tests.Passed != 7 {
		t.Errorf("top-level rollup should be the latest run, got %+v", got.Tests)
	}
	if got.Feature != "f" {
		t.Errorf("rollup lost the feature name: %+v", got)
	}

	// A run with no task ID still preserves the per-task history.
	if err := WriteSpecReport(dir, "", SpecReport{
		Feature: "f", UpdatedAt: "z", Tests: &SpecTestCounts{Total: 12, Passed: 12},
	}); err != nil {
		t.Fatal(err)
	}
	if after := LoadSpecReport(dir); len(after.Tasks) != 2 || after.Tests.Passed != 12 {
		t.Errorf("a feat-exit run must keep per-task history: %+v", after)
	}
	// No lock or temp file is left behind.
	for _, leftover := range []string{SpecReportFile + ".lock", SpecReportFile + ".tmp"} {
		if _, err := os.Stat(filepath.Join(dir, leftover)); err == nil {
			t.Errorf("%s should not survive a completed write", leftover)
		}
	}
}

// TestWriteSpecReportUnderRealConcurrency drives the lock with actual goroutines:
// every task's result must be present afterwards, with no lost update.
func TestWriteSpecReportUnderRealConcurrency(t *testing.T) {
	dir := t.TempDir()
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- WriteSpecReport(dir, fmt.Sprintf("task-%d", i), SpecReport{
				Feature:   "f",
				UpdatedAt: "2026-07-19T00:00:00Z",
				Tests:     &SpecTestCounts{Total: i + 1, Passed: i + 1},
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}
	got := LoadSpecReport(dir)
	if got == nil || len(got.Tasks) != n {
		t.Fatalf("expected %d task results to survive, got %d", n, len(got.Tasks))
	}
}
