package session

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// SpecReportFile is the per-spec metrics artifact written by
// `csdd spec test-report` and read by the dashboard.
const SpecReportFile = "test-report.json"

// SpecReport is the structured TDD test/coverage record for one spec, stored at
// specs/<feature>/test-report.json. It is authored by `csdd spec test-report`
// (the sanctioned writer) and surfaced as per-feature metrics in the dashboard.
type SpecReport struct {
	Feature   string          `json:"feature"`
	UpdatedAt string          `json:"updatedAt"`
	Command   string          `json:"command,omitempty"`
	Tests     *SpecTestCounts `json:"tests,omitempty"`
	Coverage  *SpecCovSummary `json:"coverage,omitempty"`
}

// SpecTestCounts is the test tally recorded for a spec.
type SpecTestCounts struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// SpecCovSummary is the coverage summary recorded for a spec.
type SpecCovSummary struct {
	Pct     float64 `json:"pct"`
	Covered int     `json:"covered"`
	Lines   int     `json:"lines"`
}

// loadSpecReport reads specs/<feature>/test-report.json if present, tolerating
// a missing or malformed file by returning nil.
func loadSpecReport(specDir string) *SpecReport {
	data, err := os.ReadFile(filepath.Join(specDir, SpecReportFile))
	if err != nil {
		return nil
	}
	var r SpecReport
	if json.Unmarshal(data, &r) != nil {
		return nil
	}
	return &r
}

// ParseJUnit parses a single JUnit XML file into a TestSummary (used by the CLI
// `spec test-report` writer).
func ParseJUnit(root, path string) (*TestSummary, error) {
	ts := parseJUnitFiles(root, []string{path})
	if ts == nil {
		return nil, fmt.Errorf("no JUnit test data in %s", path)
	}
	return ts, nil
}

// ParseCoverageFile parses a single coverage file (lcov by .info/.lcov, else
// Cobertura, with an lcov fallback) into a Coverage.
func ParseCoverageFile(root, path string) (*Coverage, error) {
	name := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(name, ".info") || strings.HasSuffix(name, ".lcov") {
		if cov := parseLcov(root, path); cov != nil {
			return cov, nil
		}
		return nil, fmt.Errorf("could not parse lcov from %s", path)
	}
	if cov := parseCobertura(root, path); cov != nil {
		return cov, nil
	}
	if cov := parseLcov(root, path); cov != nil {
		return cov, nil
	}
	return nil, fmt.Errorf("could not parse coverage from %s", path)
}

// TestReport aggregates test results and coverage parsed from generic report
// files the user generates (JUnit XML, lcov, Cobertura). The dashboard is
// read-only and never runs tests — it only reads these reports if present.
type TestReport struct {
	Coverage *Coverage    `json:"coverage"` // nil when no coverage report is found
	Tests    *TestSummary `json:"tests"`    // nil when no JUnit report is found
	Sources  []string     `json:"sources"`  // workspace-relative report files used
}

// Coverage is line-coverage parsed from lcov.info or a Cobertura coverage.xml.
type Coverage struct {
	Format  string         `json:"format"` // "lcov" | "cobertura"
	Source  string         `json:"source"`
	Pct     float64        `json:"pct"`
	Lines   int            `json:"lines"`
	Covered int            `json:"covered"`
	Files   []FileCoverage `json:"files"`
}

// FileCoverage is per-file line coverage.
type FileCoverage struct {
	Path    string  `json:"path"`
	Pct     float64 `json:"pct"`
	Lines   int     `json:"lines"`
	Covered int     `json:"covered"`
}

// TestSummary is the aggregate of one or more JUnit reports.
type TestSummary struct {
	Source   string        `json:"source"`
	Total    int           `json:"total"`
	Passed   int           `json:"passed"`
	Failed   int           `json:"failed"`
	Skipped  int           `json:"skipped"`
	Duration float64       `json:"durationSec"`
	Suites   []TestSuite   `json:"suites"`
	Failures []TestFailure `json:"failures,omitempty"`
}

// TestSuite is one JUnit <testsuite>.
type TestSuite struct {
	Name    string  `json:"name"`
	Total   int     `json:"total"`
	Passed  int     `json:"passed"`
	Failed  int     `json:"failed"`
	Skipped int     `json:"skipped"`
	Time    float64 `json:"time"`
}

// TestFailure is one failed/errored test case (capped for display).
type TestFailure struct {
	Suite   string `json:"suite"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

const maxScanEntries = 60000 // safety cap on the report-discovery walk

// LoadTestReport discovers and parses test/coverage reports under root. It never
// errors: missing reports simply yield nil Coverage/Tests.
func LoadTestReport(root string) TestReport {
	junitFiles, lcovFiles, coberturaFiles := discoverReports(root)

	rep := TestReport{Sources: []string{}}

	if len(junitFiles) > 0 {
		if ts := parseJUnitFiles(root, junitFiles); ts != nil {
			rep.Tests = ts
			rep.Sources = append(rep.Sources, ts.Source)
		}
	}
	// Prefer lcov, then Cobertura.
	for _, f := range lcovFiles {
		if cov := parseLcov(root, f); cov != nil {
			rep.Coverage = cov
			rep.Sources = append(rep.Sources, cov.Source)
			break
		}
	}
	if rep.Coverage == nil {
		for _, f := range coberturaFiles {
			if cov := parseCobertura(root, f); cov != nil {
				rep.Coverage = cov
				rep.Sources = append(rep.Sources, cov.Source)
				break
			}
		}
	}
	return rep
}

// discoverReports walks the project (skipping noise dirs) and classifies report
// files by name, capped for safety on huge trees.
func discoverReports(root string) (junit, lcov, cobertura []string) {
	junitDirs := map[string]bool{"test-results": true, "surefire-reports": true, "reports": true, "test-reports": true}
	scanned := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if scanned++; scanned > maxScanEntries {
			return filepath.SkipAll
		}
		name := strings.ToLower(d.Name())
		switch {
		case name == "lcov.info" || strings.HasSuffix(name, ".lcov"):
			lcov = append(lcov, p)
		case name == "coverage.xml" || name == "cobertura.xml" || strings.Contains(name, "cobertura"):
			cobertura = append(cobertura, p)
		case strings.HasSuffix(name, ".xml") &&
			(strings.Contains(name, "junit") || name == "report.xml" || name == "results.xml" ||
				name == "test-results.xml" || junitDirs[strings.ToLower(filepath.Base(filepath.Dir(p)))]):
			junit = append(junit, p)
		}
		return nil
	})
	sort.Strings(junit)
	sort.Strings(lcov)
	sort.Strings(cobertura)
	return junit, lcov, cobertura
}

// ---- JUnit XML -----------------------------------------------------------

type xmlTestSuites struct {
	XMLName xml.Name       `xml:"testsuites"`
	Suites  []xmlTestSuite `xml:"testsuite"`
}

type xmlTestSuite struct {
	XMLName  xml.Name       `xml:"testsuite"`
	Name     string         `xml:"name,attr"`
	Tests    int            `xml:"tests,attr"`
	Failures int            `xml:"failures,attr"`
	Errors   int            `xml:"errors,attr"`
	Skipped  int            `xml:"skipped,attr"`
	Time     float64        `xml:"time,attr"`
	Cases    []xmlTestCase  `xml:"testcase"`
	Nested   []xmlTestSuite `xml:"testsuite"`
}

type xmlTestCase struct {
	Name    string      `xml:"name,attr"`
	Time    float64     `xml:"time,attr"`
	Failure *xmlFailure `xml:"failure"`
	Error   *xmlFailure `xml:"error"`
	Skipped *struct{}   `xml:"skipped"`
}

type xmlFailure struct {
	Message string `xml:"message,attr"`
}

func parseJUnitFiles(root string, files []string) *TestSummary {
	ts := &TestSummary{Suites: []TestSuite{}}
	used := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		suites := decodeJUnit(data)
		if len(suites) == 0 {
			continue
		}
		used++
		for _, s := range suites {
			collectSuite(ts, s)
		}
	}
	if used == 0 {
		return nil
	}
	ts.Source = rel(root, files[0])
	if len(files) > 1 {
		ts.Source += " (+" + strconv.Itoa(used-1) + " more)"
	}
	// Cap displayed failures.
	if len(ts.Failures) > 50 {
		ts.Failures = ts.Failures[:50]
	}
	return ts
}

// decodeJUnit tolerates both <testsuites> and a bare <testsuite> root.
func decodeJUnit(data []byte) []xmlTestSuite {
	var multi xmlTestSuites
	if err := xml.Unmarshal(data, &multi); err == nil && len(multi.Suites) > 0 {
		return multi.Suites
	}
	var single xmlTestSuite
	if err := xml.Unmarshal(data, &single); err == nil && (single.Tests > 0 || len(single.Cases) > 0 || len(single.Nested) > 0) {
		return []xmlTestSuite{single}
	}
	return nil
}

func collectSuite(ts *TestSummary, s xmlTestSuite) {
	for _, n := range s.Nested { // some tools nest suites
		collectSuite(ts, n)
	}
	if len(s.Cases) == 0 && len(s.Nested) > 0 {
		return
	}
	suite := TestSuite{Name: s.Name, Time: s.Time}
	for _, c := range s.Cases {
		suite.Total++
		switch {
		case c.Failure != nil || c.Error != nil:
			suite.Failed++
			msg := ""
			if c.Failure != nil {
				msg = c.Failure.Message
			} else {
				msg = c.Error.Message
			}
			ts.Failures = append(ts.Failures, TestFailure{Suite: s.Name, Name: c.Name, Message: truncate(msg, 240)})
		case c.Skipped != nil:
			suite.Skipped++
		default:
			suite.Passed++
		}
	}
	suite.Passed = suite.Total - suite.Failed - suite.Skipped
	ts.Total += suite.Total
	ts.Failed += suite.Failed
	ts.Skipped += suite.Skipped
	ts.Passed += suite.Passed
	ts.Duration += s.Time
	ts.Suites = append(ts.Suites, suite)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ---- lcov ----------------------------------------------------------------

func parseLcov(root, path string) *Coverage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	cov := &Coverage{Format: "lcov", Source: rel(root, path), Files: []FileCoverage{}}
	var cur *FileCoverage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "SF:"):
			cur = &FileCoverage{Path: normalizeCovPath(root, strings.TrimPrefix(line, "SF:"))}
		case cur != nil && strings.HasPrefix(line, "DA:"):
			// DA:<line>,<hits>
			parts := strings.SplitN(strings.TrimPrefix(line, "DA:"), ",", 2)
			if len(parts) == 2 {
				cur.Lines++
				if n, _ := strconv.Atoi(strings.TrimSpace(parts[1])); n > 0 {
					cur.Covered++
				}
			}
		case cur != nil && strings.HasPrefix(line, "LF:"):
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "LF:")); err == nil && n > 0 {
				cur.Lines = n
			}
		case cur != nil && strings.HasPrefix(line, "LH:"):
			if n, err := strconv.Atoi(strings.TrimPrefix(line, "LH:")); err == nil {
				cur.Covered = n
			}
		case line == "end_of_record" && cur != nil:
			cur.Pct = pct(cur.Covered, cur.Lines)
			cov.Lines += cur.Lines
			cov.Covered += cur.Covered
			cov.Files = append(cov.Files, *cur)
			cur = nil
		}
	}
	if len(cov.Files) == 0 {
		return nil
	}
	cov.Pct = pct(cov.Covered, cov.Lines)
	sortFileCoverage(cov.Files)
	return cov
}

// ---- Cobertura -----------------------------------------------------------

type xmlCobertura struct {
	XMLName      xml.Name `xml:"coverage"`
	LineRate     float64  `xml:"line-rate,attr"`
	LinesCovered int      `xml:"lines-covered,attr"`
	LinesValid   int      `xml:"lines-valid,attr"`
	Packages     []struct {
		Classes []struct {
			Filename string  `xml:"filename,attr"`
			LineRate float64 `xml:"line-rate,attr"`
			Lines    struct {
				Line []struct {
					Hits int `xml:"hits,attr"`
				} `xml:"line"`
			} `xml:"lines"`
		} `xml:"classes>class"`
	} `xml:"packages>package"`
}

func parseCobertura(root, path string) *Coverage {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c xmlCobertura
	if err := xml.Unmarshal(data, &c); err != nil {
		return nil
	}
	cov := &Coverage{Format: "cobertura", Source: rel(root, path), Files: []FileCoverage{}}
	for _, p := range c.Packages {
		for _, cl := range p.Classes {
			fc := FileCoverage{Path: normalizeCovPath(root, cl.Filename)}
			for _, l := range cl.Lines.Line {
				fc.Lines++
				if l.Hits > 0 {
					fc.Covered++
				}
			}
			if fc.Lines == 0 {
				// Fall back to line-rate when per-line data is absent.
				fc.Pct = cl.LineRate * 100
			} else {
				fc.Pct = pct(fc.Covered, fc.Lines)
			}
			cov.Lines += fc.Lines
			cov.Covered += fc.Covered
			cov.Files = append(cov.Files, fc)
		}
	}
	if len(cov.Files) == 0 && c.LineRate == 0 {
		return nil
	}
	if cov.Lines > 0 {
		cov.Pct = pct(cov.Covered, cov.Lines)
	} else {
		cov.Pct = c.LineRate * 100
		cov.Lines = c.LinesValid
		cov.Covered = c.LinesCovered
	}
	sortFileCoverage(cov.Files)
	return cov
}

// ---- helpers -------------------------------------------------------------

func pct(covered, lines int) float64 {
	if lines <= 0 {
		return 0
	}
	return float64(covered) * 100 / float64(lines)
}

// normalizeCovPath makes a coverage file path workspace-relative (slash) when it
// is under root; otherwise returns it slash-cleaned as-is.
func normalizeCovPath(root, p string) string {
	p = filepath.FromSlash(p)
	if filepath.IsAbs(p) {
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return filepath.ToSlash(p)
}

func sortFileCoverage(files []FileCoverage) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Pct != files[j].Pct {
			return files[i].Pct < files[j].Pct // lowest coverage first (most actionable)
		}
		return files[i].Path < files[j].Path
	})
}
