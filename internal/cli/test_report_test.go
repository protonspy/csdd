package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestSpecTestReportNothing(t *testing.T) {
	root := t.TempDir()
	writeSpec(t, root)
	if code := specTestReport([]string{"demo", "--root", root}); code == 0 {
		t.Errorf("expected non-zero when nothing to record")
	}
}
