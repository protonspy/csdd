package plan

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/protonspy/csdd/internal/frontmatter"
	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/textutil"
	"github.com/protonspy/csdd/internal/validator"
)

// ADR status values (R1.2). An ADR with no `status` frontmatter is "accepted".
const (
	ADRStatusAccepted   = "accepted"
	ADRStatusSuperseded = "superseded"
)

// reADRFile is the decision-record filename grammar (§5.4): a four-digit
// zero-padded number, a hyphen, then a kebab-case slug, `.md`. The number is
// identity (stable, append-only); the slug is the citation currency.
var reADRFile = regexp.MustCompile(`^(\d{4})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)

// reADRSlug is the grammar an `adr:<slug>` citation token must satisfy — the same
// kebab-case rule as the filename slug, kept local so the finding is decision-ref
// specific.
var reADRSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ADR is one parsed decision record under docs/adr/. Number is its identity (the
// NNNN filename prefix); Slug is its citation currency (the kebab tail). Title is
// the first `# ` heading; Body is everything after it (convention: 1–3 sentences).
// Status defaults to accepted; SupersededBy is the number of the successor record
// (0 when absent). File is the workspace-relative path, for findings.
type ADR struct {
	Number       int
	Slug         string
	Title        string
	Body         string
	Status       string
	SupersededBy int
	File         string
}

// adrIssue is a docs/adr well-formedness problem (malformed filename, duplicate
// number, missing title, dangling supersession), kept minimal like gramIssue so
// ValidatePlan lifts it into a validator.Issue with the ADR's own file path.
type adrIssue struct {
	file string
	msg  string
}

// ADRSet is the parsed docs/adr/ directory: every well-formed record indexed by
// slug and by number, plus the well-formedness issues discovered while scanning.
// Present records whether the directory exists at all (R3.2 is gated on it).
type ADRSet struct {
	Present  bool
	All      []*ADR
	bySlug   map[string][]*ADR
	byNumber map[int][]*ADR
	issues   []adrIssue
}

// ScanADRs reads and parses docs/adr/, returning the record set. It never fails:
// a malformed record becomes a well-formedness issue carried on the set (surfaced
// by ValidatePlan), never a hard error — the lenient discipline the whole package
// shares so a half-authored corpus still lints. A missing directory yields an
// empty, not-present set.
func ScanADRs(root string) *ADRSet {
	s := &ADRSet{bySlug: map[string][]*ADR{}, byNumber: map[int][]*ADR{}}
	dir := paths.DocsADR(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return s // absent directory: not present, no records, no findings
	}
	s.Present = true
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if strings.EqualFold(e.Name(), "README.md") {
			continue // the conventional index is not a decision record
		}
		rel := filepath.ToSlash(filepath.Join("docs", paths.DocsADRSeg, e.Name()))
		m := reADRFile.FindStringSubmatch(e.Name())
		if m == nil {
			s.issues = append(s.issues, adrIssue{
				file: rel,
				msg:  "malformed ADR filename '" + e.Name() + "': expected NNNN-<kebab-slug>.md",
			})
			continue
		}
		num, _ := strconv.Atoi(m[1])
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		adr := parseADR(num, m[2], rel, string(data))
		if adr.Title == "" {
			s.issues = append(s.issues, adrIssue{
				file: rel,
				msg:  "ADR has no '# <title>' heading",
			})
		}
		s.All = append(s.All, adr)
		s.bySlug[adr.Slug] = append(s.bySlug[adr.Slug], adr)
		s.byNumber[adr.Number] = append(s.byNumber[adr.Number], adr)
	}
	sort.Slice(s.All, func(i, j int) bool { return s.All[i].Number < s.All[j].Number })
	s.finalize()
	return s
}

// parseADR reads one record's content: optional frontmatter (`status`,
// `superseded-by`), then the first `# ` line is the title and the remainder is the
// body. superseded-by targets a number (file identity), not a slug.
func parseADR(num int, slug, file, content string) *ADR {
	fm := frontmatter.Parse(content)
	adr := &ADR{Number: num, Slug: slug, File: file, Status: ADRStatusAccepted}
	if st := strings.ToLower(strings.TrimSpace(fm.AsString("status", ""))); st != "" {
		adr.Status = st
	}
	if sb := strings.TrimSpace(fm.AsString("superseded-by", "")); sb != "" {
		// Atoi handles the zero-padded NNNN form; a malformed value leaves
		// SupersededBy 0, so the dangling-supersession check only fires for a
		// parseable number that names no record, matching intent.
		if n, err := strconv.Atoi(sb); err == nil {
			adr.SupersededBy = n
		}
	}
	body := fm.Body
	lines := strings.Split(textutil.NormalizeNewlines(body), "\n")
	titleIdx := -1
	for i, raw := range lines {
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "# ") {
			adr.Title = strings.TrimSpace(strings.TrimPrefix(t, "# "))
			titleIdx = i
			break
		}
	}
	if titleIdx >= 0 {
		adr.Body = strings.TrimSpace(strings.Join(lines[titleIdx+1:], "\n"))
	}
	return adr
}

// finalize records the well-formedness issues that need the whole set: duplicate
// numbers (R3.2) and dangling supersession (R3.2). Slug ambiguity is not an issue
// here — it is a finding raised against the citing feat when a slug is resolved
// (ValidatePlan), so a duplicated slug nobody cites is silent by design.
func (s *ADRSet) finalize() {
	nums := make([]int, 0, len(s.byNumber))
	for n := range s.byNumber {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		recs := s.byNumber[n]
		if len(recs) > 1 {
			for _, r := range recs {
				s.issues = append(s.issues, adrIssue{
					file: r.File,
					msg:  "duplicate ADR number " + fourDigit(n) + " (numbers are identity; renumber the newer record)",
				})
			}
		}
	}
	for _, r := range s.All {
		if r.SupersededBy != 0 && len(s.byNumber[r.SupersededBy]) == 0 {
			s.issues = append(s.issues, adrIssue{
				file: r.File,
				msg:  "dangling supersession: superseded-by " + fourDigit(r.SupersededBy) + " names no ADR",
			})
		}
	}
}

// ADRResolution is the outcome of resolving one `adr:<slug>` citation.
type ADRResolution int

const (
	// ADRResolved: exactly one record matches the slug.
	ADRResolved ADRResolution = iota
	// ADRMissing: no record matches the slug (broken decision ref).
	ADRMissing
	// ADRAmbiguous: two or more records share the slug (ambiguous decision ref).
	ADRAmbiguous
)

// Resolve maps a citation slug to at most one record (R1.3). It returns the
// record (nil unless resolved) and the resolution outcome.
func (s *ADRSet) Resolve(slug string) (*ADR, ADRResolution) {
	recs := s.bySlug[slug]
	switch len(recs) {
	case 0:
		return nil, ADRMissing
	case 1:
		return recs[0], ADRResolved
	default:
		return nil, ADRAmbiguous
	}
}

// Issues returns the docs/adr well-formedness problems as validator-ready
// (file, message) pairs. ValidatePlan only surfaces these WHERE the directory
// exists (R3.2).
func (s *ADRSet) issuesResolved() []adrIssue { return s.issues }

// fourDigit renders a number as the zero-padded NNNN identity form.
func fourDigit(n int) string {
	str := itoa(n)
	for len(str) < 4 {
		str = "0" + str
	}
	return str
}

// ValidADRSlug reports whether s is a well-formed citation slug (kebab-case).
func ValidADRSlug(s string) bool { return reADRSlug.MatchString(s) }

// adrRefIssues resolves a feat's adr:<slug> citations against the record set and
// returns the findings: a malformed slug (R2.2), a slug matching no record
// ("broken decision ref", R3.1), a slug matching two or more ("ambiguous decision
// ref", R3.1), and a citation of a superseded record ("cites superseded decision",
// R3.3). It is imported by ValidatePlan; kept here so the ADR grammar and its
// findings live together.
func adrRefIssues(f Feat, adrs *ADRSet) []validator.Issue {
	var out []validator.Issue
	for _, slug := range f.ADRRefs {
		if !ValidADRSlug(slug) {
			label := slug
			if label == "" {
				label = "(empty)"
			}
			out = append(out, validator.Issue{
				File: "plan.md", Line: f.Line,
				Msg: "feat '" + f.Slug + "' has a malformed decision ref 'adr:" + label + "' (slug must be kebab-case)",
			})
			continue
		}
		adr, res := adrs.Resolve(slug)
		switch res {
		case ADRMissing:
			out = append(out, validator.Issue{
				File: "plan.md", Line: f.Line,
				Msg: "feat '" + f.Slug + "' has a broken decision ref 'adr:" + slug + "' (no matching record under docs/adr/)",
			})
		case ADRAmbiguous:
			out = append(out, validator.Issue{
				File: "plan.md", Line: f.Line,
				Msg: "feat '" + f.Slug + "' has an ambiguous decision ref 'adr:" + slug + "' (two or more records share this slug; renumber the newer)",
			})
		case ADRResolved:
			if adr.Status == ADRStatusSuperseded {
				out = append(out, validator.Issue{
					File: "plan.md", Line: f.Line,
					Msg: "feat '" + f.Slug + "' cites superseded decision 'adr:" + slug + "' — cite its successor",
				})
			}
		}
	}
	return out
}
