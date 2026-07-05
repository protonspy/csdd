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
	Feature    string          `json:"feature"`
	UpdatedAt  string          `json:"updatedAt"`
	Command    string          `json:"command,omitempty"`
	Tests      *SpecTestCounts `json:"tests,omitempty"`
	Coverage   *SpecCovSummary `json:"coverage,omitempty"`
	TestPaths  []string        `json:"testPaths,omitempty"`  // the spec folder this evidence belongs to (deterministic: specs/<feature>/)
	Attentions []string        `json:"attentions,omitempty"` // key unit-test signals: failures, skips, command failure
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

// covParser is one coverage-format parser: a name, a filename detector, and the
// parse function. To support a new language/format, add a single entry to
// covParsers below — discovery, single-file parsing, and report aggregation all
// consult this one list.
type covParser struct {
	format string                            // Coverage.Format value it produces
	detect func(name string) bool            // matches a lowercased base filename
	parse  func(root, path string) *Coverage // returns nil if the file is not this format
}

// covParsers is the ordered registry of supported coverage formats, listed in
// preference order (used when several reports are present):
//   - lcov       → JS/TS (jest, vitest, nyc), Python (coverage lcov), C/C++ (gcov)
//   - jacoco     → Java/Kotlin (JaCoCo jacoco.xml)
//   - cobertura  → Python (coverage xml), .NET, Ruby, others
//   - gocover    → Go (`go test -coverprofile`)
var covParsers = []covParser{
	{"lcov", func(n string) bool { return n == "lcov.info" || strings.HasSuffix(n, ".lcov") }, parseLcov},
	{"jacoco", func(n string) bool { return n == "jacoco.xml" || strings.Contains(n, "jacoco") }, parseJacoco},
	{"cobertura", func(n string) bool {
		return n == "coverage.xml" || n == "cobertura.xml" || strings.Contains(n, "cobertura")
	}, parseCobertura},
	{"gocover", func(n string) bool {
		return n == "coverage.out" || n == "cover.out" || strings.HasSuffix(n, ".coverprofile")
	}, parseGoCover},
}

// ParseCoverageFile parses a single coverage file into a Coverage. It tries the
// parser(s) whose detector matches the filename first, then every other parser
// as a fallback (so an unconventionally-named report still parses by content).
func ParseCoverageFile(root, path string) (*Coverage, error) {
	name := strings.ToLower(filepath.Base(path))
	tried := map[string]bool{}
	try := func(only func(covParser) bool) *Coverage {
		for _, p := range covParsers {
			if tried[p.format] || !only(p) {
				continue
			}
			tried[p.format] = true
			if cov := p.parse(root, path); cov != nil {
				return cov
			}
		}
		return nil
	}
	if cov := try(func(p covParser) bool { return p.detect(name) }); cov != nil {
		return cov, nil
	}
	if cov := try(func(covParser) bool { return true }); cov != nil {
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

// Coverage is line-coverage parsed from one of the supported report formats.
// For Go coverprofiles the unit is statements rather than lines, reported in the
// same Lines/Covered fields.
type Coverage struct {
	Format  string         `json:"format"` // "lcov" | "jacoco" | "cobertura" | "gocover"
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
	ts, cov, sources := discoverAndParse(root, root, nil)
	rep := TestReport{Tests: ts, Coverage: cov, Sources: sources}
	return rep
}

// DiscoverReport scans dir (a subtree of root) for JUnit + coverage reports and
// returns the parsed summaries. When lang is non-empty, only coverage in that
// language's format(s) is considered; JUnit tests are language-agnostic and
// always picked up. Either result may be nil. Used by
// `csdd spec test-report --lang/--path`.
func DiscoverReport(root, dir, lang string) (*TestSummary, *Coverage) {
	formats, _ := CoverageFormatsForLang(lang)
	ts, cov, _ := discoverAndParse(root, dir, formats)
	return ts, cov
}

// discoverAndParse walks dir for reports and parses them, restricting coverage to
// formats when non-empty. sources lists the workspace-relative files used.
func discoverAndParse(root, dir string, formats []string) (ts *TestSummary, cov *Coverage, sources []string) {
	junitFiles, covFiles := discoverReports(root, dir)
	sources = []string{}
	if len(junitFiles) > 0 {
		if ts = parseJUnitFiles(root, junitFiles); ts != nil {
			sources = append(sources, ts.Source)
		}
	}
	if cov = pickCoverage(root, covFiles, formats); cov != nil {
		sources = append(sources, cov.Source)
	}
	return ts, cov, sources
}

// pickCoverage parses the first discovered coverage file that succeeds, trying
// formats in covParsers preference order (lcov, jacoco, cobertura, gocover). When
// formats is non-empty, only those formats are considered.
func pickCoverage(root string, files, formats []string) *Coverage {
	allow := map[string]bool{}
	for _, f := range formats {
		allow[f] = true
	}
	for _, p := range covParsers {
		if len(allow) > 0 && !allow[p.format] {
			continue
		}
		for _, f := range files {
			if p.detect(strings.ToLower(filepath.Base(f))) {
				if cov := p.parse(root, f); cov != nil {
					return cov
				}
			}
		}
	}
	return nil
}

// langCovFormats maps each supported language (and its common aliases) to the
// coverage formats its tooling emits. The supported languages are Python,
// TypeScript/Node.js, Java, Go, and Rust. To extend, add an alias/entry here
// (and a covParser entry above when the format itself is new).
var langCovFormats = map[string][]string{
	"python": {"cobertura", "lcov"}, "py": {"cobertura", "lcov"},
	"typescript": {"lcov"}, "ts": {"lcov"},
	"javascript": {"lcov"}, "js": {"lcov"}, "nodejs": {"lcov"}, "node": {"lcov"},
	"java": {"jacoco"},
	"go":   {"gocover"}, "golang": {"gocover"},
	"rust": {"lcov", "cobertura"}, "rs": {"lcov", "cobertura"},
}

// supportedLangNames lists the canonical supported languages (for help/errors).
var supportedLangNames = []string{"python", "typescript", "java", "go", "rust"}

// SupportedLangs returns the canonical names of languages `--lang` accepts.
func SupportedLangs() []string { return supportedLangNames }

// langTestCommands maps a language to the default test command `--run` executes
// when no explicit --cmd is given. Each is expected to emit a JUnit report and a
// coverage report this package can parse; override with --cmd when your project
// differs. The commands assume the conventional toolchain is installed.
var langTestCommands = map[string]string{
	"python":     "pytest --junitxml=junit.xml --cov --cov-report=xml",
	"py":         "pytest --junitxml=junit.xml --cov --cov-report=xml",
	"typescript": "npx jest --ci --reporters=default --reporters=jest-junit --coverage --coverageReporters=lcov",
	"ts":         "npx jest --ci --reporters=default --reporters=jest-junit --coverage --coverageReporters=lcov",
	"javascript": "npx jest --ci --reporters=default --reporters=jest-junit --coverage --coverageReporters=lcov",
	"js":         "npx jest --ci --reporters=default --reporters=jest-junit --coverage --coverageReporters=lcov",
	"nodejs":     "npx jest --ci --reporters=default --reporters=jest-junit --coverage --coverageReporters=lcov",
	"node":       "npx jest --ci --reporters=default --reporters=jest-junit --coverage --coverageReporters=lcov",
	"java":       "mvn -q test jacoco:report",
	"go":         "gotestsum --junitfile=junit.xml -- -coverprofile=coverage.out ./...",
	"golang":     "gotestsum --junitfile=junit.xml -- -coverprofile=coverage.out ./...",
	"rust":       "cargo nextest run --profile ci && cargo llvm-cov report --lcov --output-path lcov.info",
	"rs":         "cargo nextest run --profile ci && cargo llvm-cov report --lcov --output-path lcov.info",
}

// DefaultTestCommand returns the default `--run` command for a language, and
// whether one is defined.
func DefaultTestCommand(lang string) (string, bool) {
	c, ok := langTestCommands[strings.ToLower(strings.TrimSpace(lang))]
	return c, ok
}

// langCanonical maps every accepted alias to its canonical language name.
var langCanonical = map[string]string{
	"python": "python", "py": "python",
	"typescript": "typescript", "ts": "typescript", "javascript": "typescript",
	"js": "typescript", "nodejs": "typescript", "node": "typescript",
	"java": "java",
	"go":   "go", "golang": "go",
	"rust": "rust", "rs": "rust",
}

// langTestMarkers lists substrings that signal a command really invokes that
// language's test/coverage tooling. Used to validate a custom --cmd override.
var langTestMarkers = map[string][]string{
	"python":     {"pytest", "unittest", "tox", "nox", "coverage"},
	"typescript": {"jest", "vitest", "mocha", "playwright", "npm test", "npm run test", "yarn test", "pnpm test", "node --test"},
	"java":       {"mvn", "gradle", "./gradlew", "jacoco", "surefire"},
	"go":         {"go test", "gotestsum"},
	"rust":       {"cargo", "nextest", "llvm-cov"},
}

// ValidateTestCommandForLang reports whether cmd looks like lang's test command
// (it contains a known tooling marker). ok is true for an empty cmd or an
// unknown/unmarkered language (nothing to validate against). markers is the
// expected set, for the caller's message.
func ValidateTestCommandForLang(lang, cmd string) (ok bool, markers []string) {
	if strings.TrimSpace(cmd) == "" {
		return true, nil
	}
	canon, found := langCanonical[strings.ToLower(strings.TrimSpace(lang))]
	if !found {
		return true, nil
	}
	markers = langTestMarkers[canon]
	if len(markers) == 0 {
		return true, nil
	}
	lc := strings.ToLower(cmd)
	for _, m := range markers {
		if strings.Contains(lc, m) {
			return true, markers
		}
	}
	return false, markers
}

// CoverageFormatsForLang resolves a single language to the coverage formats to
// look for (in covParsers preference order). ok is false for an unsupported
// language. An empty lang returns (nil, true), meaning "any format".
func CoverageFormatsForLang(lang string) (formats []string, ok bool) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return nil, true
	}
	want, found := langCovFormats[lang]
	if !found {
		return nil, false
	}
	set := map[string]bool{}
	for _, f := range want {
		set[f] = true
	}
	for _, p := range covParsers { // preference order
		if set[p.format] {
			formats = append(formats, p.format)
		}
	}
	return formats, true
}

// discoverReports walks the project (skipping noise dirs) and classifies report
// files by name into JUnit (tests) and coverage candidates, capped for safety on
// huge trees. Coverage files are recognised via the covParsers registry, so a
// new format is picked up here automatically.
func discoverReports(root, dir string) (junit, coverage []string) {
	junitDirs := map[string]bool{"test-results": true, "surefire-reports": true, "reports": true, "test-reports": true}
	scanned := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
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
		case isCoverageName(name):
			coverage = append(coverage, p)
		case strings.HasSuffix(name, ".xml") &&
			(strings.Contains(name, "junit") || name == "report.xml" || name == "results.xml" ||
				name == "test-results.xml" || junitDirs[strings.ToLower(filepath.Base(filepath.Dir(p)))]):
			junit = append(junit, p)
		}
		return nil
	})
	sort.Strings(junit)
	sort.Strings(coverage)
	return junit, coverage
}

// isCoverageName reports whether a lowercased base filename is recognised by any
// registered coverage parser.
func isCoverageName(name string) bool {
	for _, p := range covParsers {
		if p.detect(name) {
			return true
		}
	}
	return false
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
	defer func() { _ = f.Close() }()

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

// ---- Go coverprofile -----------------------------------------------------

// parseGoCover parses a `go test -coverprofile` file. Lines look like
// `import/path/file.go:sL.sC,eL.eC <numStmts> <count>`; coverage is measured in
// statements (count>0 ⇒ that block's statements are covered), matching `go tool
// cover`. The leading `mode:` line is required, which disambiguates the format.
func parseGoCover(root, path string) *Coverage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	cov := &Coverage{Format: "gocover", Source: rel(root, path), Files: []FileCoverage{}}
	perFile := map[string]*FileCoverage{}
	var order []string
	sawMode := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "mode:") {
			sawMode = true
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		colon := strings.LastIndex(fields[0], ":") // name has no ':'; the range follows it
		if colon <= 0 {
			continue
		}
		name := fields[0][:colon]
		stmts, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || stmts <= 0 {
			continue
		}
		fc := perFile[name]
		if fc == nil {
			fc = &FileCoverage{Path: normalizeCovPath(root, name)}
			perFile[name] = fc
			order = append(order, name)
		}
		fc.Lines += stmts
		if count > 0 {
			fc.Covered += stmts
		}
	}
	if !sawMode || len(order) == 0 {
		return nil
	}
	for _, name := range order {
		fc := perFile[name]
		fc.Pct = pct(fc.Covered, fc.Lines)
		cov.Lines += fc.Lines
		cov.Covered += fc.Covered
		cov.Files = append(cov.Files, *fc)
	}
	cov.Pct = pct(cov.Covered, cov.Lines)
	sortFileCoverage(cov.Files)
	return cov
}

// ---- JaCoCo --------------------------------------------------------------

type xmlJacoco struct {
	XMLName  xml.Name `xml:"report"`
	Packages []struct {
		Name        string `xml:"name,attr"`
		SourceFiles []struct {
			Name     string             `xml:"name,attr"`
			Counters []xmlJacocoCounter `xml:"counter"`
		} `xml:"sourcefile"`
	} `xml:"package"`
	Counters []xmlJacocoCounter `xml:"counter"`
}

type xmlJacocoCounter struct {
	Type    string `xml:"type,attr"`
	Missed  int    `xml:"missed,attr"`
	Covered int    `xml:"covered,attr"`
}

// jacocoLineCounter returns the LINE counter from a set, or nil.
func jacocoLineCounter(cs []xmlJacocoCounter) *xmlJacocoCounter {
	for i := range cs {
		if cs[i].Type == "LINE" {
			return &cs[i]
		}
	}
	return nil
}

// parseJacoco parses a JaCoCo XML report (Java/Kotlin). Line coverage comes from
// each <sourcefile>'s `<counter type="LINE" .../>`; it falls back to the report's
// top-level LINE counter when per-file data is absent.
func parseJacoco(root, path string) *Coverage {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var r xmlJacoco
	if err := xml.Unmarshal(data, &r); err != nil {
		return nil
	}
	cov := &Coverage{Format: "jacoco", Source: rel(root, path), Files: []FileCoverage{}}
	for _, p := range r.Packages {
		for _, sf := range p.SourceFiles {
			c := jacocoLineCounter(sf.Counters)
			if c == nil {
				continue
			}
			total := c.Missed + c.Covered
			name := sf.Name
			if p.Name != "" {
				name = p.Name + "/" + sf.Name
			}
			fc := FileCoverage{Path: normalizeCovPath(root, name), Lines: total, Covered: c.Covered, Pct: pct(c.Covered, total)}
			cov.Lines += total
			cov.Covered += c.Covered
			cov.Files = append(cov.Files, fc)
		}
	}
	if len(cov.Files) == 0 {
		c := jacocoLineCounter(r.Counters)
		if c == nil || c.Missed+c.Covered == 0 {
			return nil
		}
		cov.Lines = c.Missed + c.Covered
		cov.Covered = c.Covered
	}
	cov.Pct = pct(cov.Covered, cov.Lines)
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
