package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// SpecDiffFile is the per-spec file-change artifact written by
// `csdd spec diff-report` and read by the dashboard. It is the diff sibling of
// SpecReportFile (test-report.json): a structured, auditable record of the
// source changes made while implementing a spec.
const SpecDiffFile = "diff-report.json"

// SpecDiff is the structured diff of base → working tree for one spec, stored at
// specs/<feature>/diff-report.json. It is authored by `csdd spec diff-report`
// (the sanctioned writer) and surfaced as the Changes view in the dashboard.
type SpecDiff struct {
	Feature   string      `json:"feature"`
	UpdatedAt string      `json:"updatedAt"`
	Base      string      `json:"base"`              // resolved base ref (e.g. "main")
	BaseSHA   string      `json:"baseSha,omitempty"` // merge-base commit
	Head      string      `json:"head"`              // "working-tree"
	Summary   DiffSummary `json:"summary"`
	Files     []DiffFile  `json:"files"`
	Truncated bool        `json:"truncated,omitempty"` // any file truncated
}

// DiffSummary is the aggregate change tally across all files.
type DiffSummary struct {
	Files     int `json:"files"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// DiffFile is one changed file with its status, counts, and hunks.
type DiffFile struct {
	Path      string     `json:"path"`
	OldPath   string     `json:"oldPath,omitempty"` // set for renames
	Status    string     `json:"status"`            // added|modified|deleted|renamed
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Binary    bool       `json:"binary,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
	Hunks     []DiffHunk `json:"hunks,omitempty"`
}

// DiffHunk is one @@ block: its old/new ranges and its lines.
type DiffHunk struct {
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Header   string     `json:"header,omitempty"` // section heading after @@ (function context)
	Lines    []DiffLine `json:"lines"`
}

// DiffLine is one line within a hunk, classified and numbered.
type DiffLine struct {
	Type string `json:"type"`          // "context" | "add" | "del"
	Old  int    `json:"old,omitempty"` // old line number (0 for an added line)
	New  int    `json:"new,omitempty"` // new line number (0 for a deleted line)
	Text string `json:"text"`
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?: (.*))?$`)

// ParseUnifiedDiff turns `git diff --no-color -M` output into structured files,
// capping recorded hunk lines per file at maxLines (0 = uncapped) and flagging
// truncation. Addition/deletion counts stay exact even past the cap so the
// summary remains an accurate audit total.
func ParseUnifiedDiff(patch string, maxLines int) []DiffFile {
	var files []DiffFile
	var cur *DiffFile
	var oldNo, newNo int
	var inHunk bool
	recorded := 0

	flush := func() {
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}
	record := func(h *DiffHunk, dl DiffLine) {
		if maxLines > 0 && recorded >= maxLines {
			cur.Truncated = true
			return
		}
		h.Lines = append(h.Lines, dl)
		recorded++
	}

	sc := bufio.NewScanner(strings.NewReader(patch))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()

		if strings.HasPrefix(line, "diff --git ") {
			flush()
			a, b := parseDiffGitPaths(line)
			path := b
			if path == "" {
				path = a
			}
			cur = &DiffFile{Path: path, Status: "modified"}
			oldNo, newNo, recorded, inHunk = 0, 0, 0, false
			continue
		}
		if cur == nil {
			continue
		}

		if strings.HasPrefix(line, "@@") {
			m := hunkHeaderRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			h := DiffHunk{
				OldStart: atoi(m[1]),
				OldLines: atoiOr(m[2], 1),
				NewStart: atoi(m[3]),
				NewLines: atoiOr(m[4], 1),
				Header:   m[5],
			}
			oldNo, newNo, inHunk = h.OldStart, h.NewStart, true
			if !cur.Binary && !cur.Truncated {
				cur.Hunks = append(cur.Hunks, h)
			}
			continue
		}

		if !inHunk {
			switch {
			case strings.HasPrefix(line, "new file mode"):
				cur.Status = "added"
			case strings.HasPrefix(line, "deleted file mode"):
				cur.Status = "deleted"
			case strings.HasPrefix(line, "rename from "):
				cur.Status = "renamed"
				cur.OldPath = strings.TrimPrefix(line, "rename from ")
			case strings.HasPrefix(line, "rename to "):
				cur.Status = "renamed"
				cur.Path = strings.TrimPrefix(line, "rename to ")
			case strings.HasPrefix(line, "Binary files "), strings.HasPrefix(line, "GIT binary patch"):
				cur.Binary = true
			case strings.HasPrefix(line, "--- "):
				if cur.Status == "deleted" {
					if p := headerPath(line[4:]); p != "" {
						cur.Path = p
					}
				}
			case strings.HasPrefix(line, "+++ "):
				if cur.Status != "renamed" && cur.Status != "deleted" {
					if p := headerPath(line[4:]); p != "" {
						cur.Path = p
					}
				}
			}
			continue
		}

		// Inside a hunk body. Count +/- exactly; record lines until the cap.
		if cur.Binary || len(line) == 0 {
			continue
		}
		var h *DiffHunk
		if n := len(cur.Hunks); n > 0 {
			h = &cur.Hunks[n-1]
		}
		switch line[0] {
		case '+':
			cur.Additions++
			if h != nil {
				record(h, DiffLine{Type: "add", New: newNo, Text: line[1:]})
			}
			newNo++
		case '-':
			cur.Deletions++
			if h != nil {
				record(h, DiffLine{Type: "del", Old: oldNo, Text: line[1:]})
			}
			oldNo++
		case ' ':
			if h != nil {
				record(h, DiffLine{Type: "context", Old: oldNo, New: newNo, Text: line[1:]})
			}
			oldNo++
			newNo++
		case '\\':
			// "\ No newline at end of file" — not a content line.
		}
	}
	flush()
	return files
}

// AddedFile synthesizes an "added" DiffFile from an untracked file's raw content
// (every line is an addition), honoring the per-file line cap and marking binary
// content (which gets no hunks).
func AddedFile(path string, content []byte, maxLines int) DiffFile {
	f := DiffFile{Path: path, Status: "added"}
	if isBinary(content) {
		f.Binary = true
		return f
	}
	text := strings.TrimSuffix(string(content), "\n")
	if text == "" && len(content) == 0 {
		return f // empty file: added with no lines
	}
	lines := strings.Split(text, "\n")
	f.Additions = len(lines)
	// NewLines is the true line count, not the recorded count: on a truncated file
	// it can exceed len(h.Lines) (same convention as git's @@ header in the parser).
	h := DiffHunk{NewStart: 1, NewLines: len(lines)}
	for i, ln := range lines {
		if maxLines > 0 && len(h.Lines) >= maxLines {
			f.Truncated = true
			break
		}
		h.Lines = append(h.Lines, DiffLine{Type: "add", New: i + 1, Text: ln})
	}
	if len(h.Lines) > 0 {
		f.Hunks = []DiffHunk{h}
	}
	return f
}

// Summarize aggregates files into a DiffSummary.
func Summarize(files []DiffFile) DiffSummary {
	s := DiffSummary{Files: len(files)}
	for _, f := range files {
		s.Additions += f.Additions
		s.Deletions += f.Deletions
	}
	return s
}

// loadSpecDiff reads specs/<feature>/diff-report.json if present, tolerating a
// missing or malformed file by returning nil (mirrors loadSpecReport).
func loadSpecDiff(specDir string) *SpecDiff {
	data, err := os.ReadFile(filepath.Join(specDir, SpecDiffFile))
	if err != nil {
		return nil
	}
	var d SpecDiff
	if json.Unmarshal(data, &d) != nil {
		return nil
	}
	return &d
}

// parseDiffGitPaths extracts the a/ and b/ paths from a "diff --git a/X b/Y"
// header. It is a fallback path source (binary files have no ---/+++ lines).
// Known limitation: git-quoted paths (filenames with spaces/unicode, rendered
// as "a/x y") are not unquoted here; such files fall back to the ---/+++ lines.
func parseDiffGitPaths(line string) (string, string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	if i := strings.Index(rest, " b/"); i >= 0 {
		return strings.TrimPrefix(rest[:i], "a/"), strings.TrimPrefix(rest[i+1:], "b/")
	}
	return "", ""
}

// headerPath strips the a//b/ prefix from a ---/+++ path, returning "" for /dev/null.
func headerPath(s string) string {
	s = strings.TrimSpace(s)
	if s == "/dev/null" {
		return ""
	}
	s = strings.TrimPrefix(s, "a/")
	s = strings.TrimPrefix(s, "b/")
	return s
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	return atoi(s)
}
