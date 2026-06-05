package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpec(t *testing.T, root string) string {
	t.Helper()
	specDir := filepath.Join(root, "specs", "demo")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "spec.json"), []byte(`{"phase":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return specDir
}

func TestSpecTestReportExplicit(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root)

	code := specTestReport([]string{
		"demo", "--root", root,
		"--total", "10", "--passed", "9", "--failed", "1",
		"--pct", "85.5", "--command", "make check",
	})
	if code != 0 {
		t.Fatalf("test-report exit = %d", code)
	}

	data, err := os.ReadFile(filepath.Join(specDir, "test-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Feature  string `json:"feature"`
		Command  string `json:"command"`
		Tests    struct{ Total, Passed, Failed, Skipped int }
		Coverage struct{ Pct float64 }
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Feature != "demo" || rep.Command != "make check" {
		t.Errorf("meta = %+v", rep)
	}
	if rep.Tests.Total != 10 || rep.Tests.Passed != 9 || rep.Tests.Failed != 1 {
		t.Errorf("tests = %+v", rep.Tests)
	}
	if rep.Coverage.Pct != 85.5 {
		t.Errorf("pct = %v, want 85.5", rep.Coverage.Pct)
	}
}

func TestSpecTestReportFromJUnit(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root)
	junit := filepath.Join(root, "junit.xml")
	os.WriteFile(junit, []byte(`<testsuites><testsuite name="s" tests="2" failures="1"><testcase name="a"/><testcase name="b"><failure message="x"/></testcase></testsuite></testsuites>`), 0o644)

	if code := specTestReport([]string{"demo", "--root", root, "--junit", junit}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(specDir, "test-report.json"))
	var rep struct {
		Tests struct{ Total, Passed, Failed int }
	}
	json.Unmarshal(data, &rep)
	if rep.Tests.Total != 2 || rep.Tests.Failed != 1 || rep.Tests.Passed != 1 {
		t.Errorf("junit tests = %+v", rep.Tests)
	}
}

func TestSpecTestReportDiscoverByLangAndPath(t *testing.T) {
	root := t.TempDir()
	specDir := writeSpec(t, root)
	testsDir := filepath.Join(root, "tests")
	if err := os.MkdirAll(testsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A Go coverprofile and a JUnit report under tests/.
	os.WriteFile(filepath.Join(testsDir, "coverage.out"), []byte(
		"mode: set\ngithub.com/acme/p/a.go:1.1,2.2 4 1\ngithub.com/acme/p/a.go:3.1,4.2 1 0\n"), 0o644)
	os.WriteFile(filepath.Join(testsDir, "junit.xml"), []byte(
		`<testsuites><testsuite name="s" tests="3" failures="0"><testcase name="a"/><testcase name="b"/><testcase name="c"/></testsuite></testsuites>`), 0o644)

	code := specTestReport([]string{"demo", "--root", root, "--lang", "go", "--path", "tests"})
	if code != 0 {
		t.Fatalf("test-report exit = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(specDir, "test-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Tests    *struct{ Total, Passed int }
		Coverage *struct{ Covered, Lines int }
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Tests == nil || rep.Tests.Total != 3 || rep.Tests.Passed != 3 {
		t.Errorf("discovered tests = %+v", rep.Tests)
	}
	if rep.Coverage == nil || rep.Coverage.Covered != 4 || rep.Coverage.Lines != 5 {
		t.Errorf("discovered coverage = %+v, want 4/5 statements", rep.Coverage)
	}
}

func TestSpecTestReportBadPath(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root)
	if code := specTestReport([]string{"demo", "--root", root, "--path", "nope"}); code == 0 {
		t.Errorf("expected non-zero for missing --path")
	}
}

func TestSpecTestReportRunPassing(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs POSIX shell")
	}
	root := t.TempDir()
	specDir := writeSpec(t, root)
	junit := `<testsuites><testsuite name="s" tests="2" failures="0"><testcase name="a"/><testcase name="b"/></testsuite></testsuites>`
	cmd := `printf '%s' '` + junit + `' > junit.xml; printf 'mode: set\ngithub.com/a/b.go:1.1,2.2 3 1\n' > coverage.out`

	code := specTestReport([]string{"demo", "--root", root, "--run", "--cmd", cmd, "--lang", "go", "--path", "."})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 for passing run", code)
	}
	var rep struct {
		Command   string
		Tests     *struct{ Total, Passed, Failed int }
		Coverage  *struct{ Covered, Lines int }
		TestPaths []string `json:"testPaths"`
	}
	data, _ := os.ReadFile(filepath.Join(specDir, "test-report.json"))
	json.Unmarshal(data, &rep)
	if rep.Tests == nil || rep.Tests.Total != 2 || rep.Tests.Passed != 2 {
		t.Errorf("tests = %+v", rep.Tests)
	}
	if rep.Coverage == nil || rep.Coverage.Covered != 3 || rep.Coverage.Lines != 3 {
		t.Errorf("coverage = %+v", rep.Coverage)
	}
	if rep.Command != cmd {
		t.Errorf("command = %q", rep.Command)
	}
	if len(rep.TestPaths) == 0 {
		t.Errorf("expected testPaths evidence, got none")
	}
}

func TestSpecTestReportRunFailing(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs POSIX shell")
	}
	root := t.TempDir()
	specDir := writeSpec(t, root)
	junit := `<testsuites><testsuite name="s" tests="2" failures="1"><testcase name="a"/><testcase name="b"><failure message="boom"/></testcase></testsuite></testsuites>`
	cmd := `printf '%s' '` + junit + `' > junit.xml; exit 1`

	code := specTestReport([]string{"demo", "--root", root, "--run", "--cmd", cmd, "--lang", "go", "--path", "."})
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero when the test command fails")
	}
	var rep struct {
		Tests      *struct{ Total, Failed int }
		Attentions []string `json:"attentions"`
	}
	data, _ := os.ReadFile(filepath.Join(specDir, "test-report.json"))
	json.Unmarshal(data, &rep)
	if rep.Tests == nil || rep.Tests.Failed != 1 {
		t.Errorf("tests = %+v", rep.Tests)
	}
	if len(rep.Attentions) == 0 {
		t.Fatalf("expected attentions for a failing run")
	}
	joined := strings.Join(rep.Attentions, " | ")
	if !strings.Contains(joined, "exited with code 1") || !strings.Contains(joined, "FAIL") {
		t.Errorf("attentions = %v", rep.Attentions)
	}
}

func TestSpecTestReportUnsupportedLang(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root)
	if code := specTestReport([]string{"demo", "--root", root, "--lang", "cobol", "--path", "."}); code == 0 {
		t.Errorf("expected non-zero for unsupported --lang")
	}
}

func TestSpecTestReportNothing(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root)
	if code := specTestReport([]string{"demo", "--root", root}); code == 0 {
		t.Errorf("expected non-zero when nothing to record")
	}
}
