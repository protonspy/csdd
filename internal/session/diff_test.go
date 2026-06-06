package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseUnifiedDiffModified(t *testing.T) {
	patch := `diff --git a/foo.txt b/foo.txt
index 1234567..89abcde 100644
--- a/foo.txt
+++ b/foo.txt
@@ -1,3 +1,4 @@ func ctx()
 line one
-line two
+line TWO
+line two and a half
 line three
`
	files := ParseUnifiedDiff(patch, 0)
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d", len(files))
	}
	f := files[0]
	if f.Path != "foo.txt" || f.Status != "modified" {
		t.Fatalf("path/status = %q/%q", f.Path, f.Status)
	}
	if f.Additions != 2 || f.Deletions != 1 {
		t.Fatalf("additions/deletions = %d/%d, want 2/1", f.Additions, f.Deletions)
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("want 1 hunk, got %d", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 3 || h.NewStart != 1 || h.NewLines != 4 {
		t.Fatalf("hunk range = -%d,%d +%d,%d", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
	}
	if h.Header != "func ctx()" {
		t.Fatalf("hunk header = %q", h.Header)
	}
	want := []DiffLine{
		{Type: "context", Old: 1, New: 1, Text: "line one"},
		{Type: "del", Old: 2, Text: "line two"},
		{Type: "add", New: 2, Text: "line TWO"},
		{Type: "add", New: 3, Text: "line two and a half"},
		{Type: "context", Old: 3, New: 4, Text: "line three"},
	}
	if len(h.Lines) != len(want) {
		t.Fatalf("want %d lines, got %d", len(want), len(h.Lines))
	}
	for i, w := range want {
		got := h.Lines[i]
		if got != w {
			t.Errorf("line %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestParseUnifiedDiffAddedDeletedRenamed(t *testing.T) {
	added := `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..1111111
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+alpha
+beta
`
	if f := ParseUnifiedDiff(added, 0)[0]; f.Status != "added" || f.Path != "new.txt" || f.Additions != 2 || f.Deletions != 0 {
		t.Errorf("added: %+v", f)
	}

	deleted := `diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index 1111111..0000000
--- a/gone.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-x
-y
`
	if f := ParseUnifiedDiff(deleted, 0)[0]; f.Status != "deleted" || f.Path != "gone.txt" || f.Deletions != 2 || f.Additions != 0 {
		t.Errorf("deleted: %+v", f)
	}

	renamed := `diff --git a/old.txt b/new2.txt
similarity index 80%
rename from old.txt
rename to new2.txt
index 1234567..89abcde 100644
--- a/old.txt
+++ b/new2.txt
@@ -1,2 +1,2 @@
 keep
-old line
+new line
`
	f := ParseUnifiedDiff(renamed, 0)[0]
	if f.Status != "renamed" || f.OldPath != "old.txt" || f.Path != "new2.txt" {
		t.Errorf("renamed: status=%q oldPath=%q path=%q", f.Status, f.OldPath, f.Path)
	}
	if f.Additions != 1 || f.Deletions != 1 {
		t.Errorf("renamed counts: +%d -%d", f.Additions, f.Deletions)
	}
}

func TestParseUnifiedDiffBinary(t *testing.T) {
	patch := `diff --git a/img.png b/img.png
index 1111111..2222222 100644
Binary files a/img.png and b/img.png differ
`
	f := ParseUnifiedDiff(patch, 0)[0]
	if !f.Binary {
		t.Fatalf("want binary=true: %+v", f)
	}
	if len(f.Hunks) != 0 {
		t.Errorf("binary file should have no hunks, got %d", len(f.Hunks))
	}
	if f.Path != "img.png" {
		t.Errorf("path = %q", f.Path)
	}

	// An added binary file: the `new file mode` line precedes `Binary files`, so
	// status must still be "added" (not the default "modified").
	addedBin := `diff --git a/logo.png b/logo.png
new file mode 100644
index 0000000..2222222
Binary files /dev/null and b/logo.png differ
`
	if af := ParseUnifiedDiff(addedBin, 0)[0]; af.Status != "added" || !af.Binary || len(af.Hunks) != 0 {
		t.Errorf("added binary: %+v", af)
	}
}

func TestParseUnifiedDiffMultipleFiles(t *testing.T) {
	patch := `diff --git a/a.txt b/a.txt
index 1..2 100644
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-a
+A
diff --git a/b.txt b/b.txt
index 3..4 100644
--- a/b.txt
+++ b/b.txt
@@ -1 +1 @@
-b
+B
`
	files := ParseUnifiedDiff(patch, 0)
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
	if files[0].Path != "a.txt" || files[1].Path != "b.txt" {
		t.Errorf("paths = %q, %q", files[0].Path, files[1].Path)
	}
}

func TestParseUnifiedDiffTruncates(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/big.txt b/big.txt\nnew file mode 100644\nindex 0..1\n--- /dev/null\n+++ b/big.txt\n@@ -0,0 +1,50 @@\n")
	for i := 0; i < 50; i++ {
		b.WriteString("+line\n")
	}
	f := ParseUnifiedDiff(b.String(), 10)[0]
	if !f.Truncated {
		t.Fatalf("want truncated=true")
	}
	recorded := 0
	for _, h := range f.Hunks {
		recorded += len(h.Lines)
	}
	if recorded > 10 {
		t.Errorf("recorded %d lines, want <= 10", recorded)
	}
	// True additions are still counted even past the cap (audit accuracy).
	if f.Additions != 50 {
		t.Errorf("additions = %d, want 50", f.Additions)
	}
}

func TestAddedFile(t *testing.T) {
	f := AddedFile("a.txt", []byte("l1\nl2\n"), 0)
	if f.Status != "added" || f.Path != "a.txt" || f.Additions != 2 {
		t.Fatalf("added file: %+v", f)
	}
	if len(f.Hunks) != 1 || len(f.Hunks[0].Lines) != 2 {
		t.Fatalf("hunk: %+v", f.Hunks)
	}
	if f.Hunks[0].Lines[0].Type != "add" || f.Hunks[0].Lines[0].New != 1 {
		t.Errorf("first line: %+v", f.Hunks[0].Lines[0])
	}

	bin := AddedFile("x.bin", []byte("a\x00b"), 0)
	if !bin.Binary || len(bin.Hunks) != 0 {
		t.Errorf("binary added file: %+v", bin)
	}

	capped := AddedFile("c.txt", []byte("1\n2\n3\n4\n5\n"), 2)
	if !capped.Truncated {
		t.Errorf("want truncated for capped added file")
	}
	rec := 0
	for _, h := range capped.Hunks {
		rec += len(h.Lines)
	}
	if rec > 2 {
		t.Errorf("recorded %d, want <= 2", rec)
	}
}

func TestSummarize(t *testing.T) {
	s := Summarize([]DiffFile{
		{Additions: 2, Deletions: 1},
		{Additions: 3, Deletions: 0},
	})
	if s.Files != 2 || s.Additions != 5 || s.Deletions != 1 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestLoadSpecDiff(t *testing.T) {
	dir := t.TempDir()
	if loadSpecDiff(dir) != nil {
		t.Errorf("missing file should yield nil")
	}
	if err := os.WriteFile(filepath.Join(dir, SpecDiffFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if loadSpecDiff(dir) != nil {
		t.Errorf("malformed file should yield nil")
	}
	good := SpecDiff{Feature: "f", Base: "main", Files: []DiffFile{{Path: "x", Status: "added"}}}
	b, _ := json.Marshal(good)
	if err := os.WriteFile(filepath.Join(dir, SpecDiffFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadSpecDiff(dir)
	if got == nil || got.Feature != "f" || len(got.Files) != 1 {
		t.Errorf("valid file: %+v", got)
	}
}

func TestLoadSpecDetailIncludesDiff(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "specs", "feat")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "spec.json"), []byte(`{"feature_name":"feat","phase":"tasks"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// No diff-report.json yet → Diff is nil.
	d, err := LoadSpecDetail(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if d.Diff != nil {
		t.Errorf("Diff should be nil without diff-report.json")
	}
	// Write one → Diff is populated.
	b, _ := json.Marshal(SpecDiff{Feature: "feat", Base: "main"})
	if err := os.WriteFile(filepath.Join(specDir, SpecDiffFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err = LoadSpecDetail(root, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if d.Diff == nil || d.Diff.Base != "main" {
		t.Errorf("Diff not populated: %+v", d.Diff)
	}
}
