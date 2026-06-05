package session

import "testing"

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
