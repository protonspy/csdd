package plan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSummarizeCountsDoneFeats(t *testing.T) {
	st := PlanStatus{
		Slug: "photos", Name: "Photos", Approved: true,
		Feats: []FeatStatus{
			{Slug: "upload", State: StateDone},
			{Slug: "thumbs", State: StateImplementing},
			{Slug: "share", State: StatePending},
		},
	}
	s := Summarize(st)
	if s.Slug != "photos" || s.Name != "Photos" || !s.Approved || s.Drift {
		t.Errorf("plan-level fields not carried through: %+v", s)
	}
	if s.Feats != 3 || s.Done != 1 {
		t.Errorf("want 1/3 done, got %d/%d", s.Done, s.Feats)
	}
	if s.Complete {
		t.Error("a plan with undelivered feats is not complete")
	}
}

func TestSummarizeCompleteOnlyWithFeats(t *testing.T) {
	all := Summarize(PlanStatus{Feats: []FeatStatus{
		{Slug: "a", State: StateDone},
		{Slug: "b", State: StateDone},
	}})
	if !all.Complete {
		t.Error("every feat delivered should read as complete")
	}
	// An empty feat table is unstarted, not finished — calling it complete would
	// report a plan nobody has decomposed yet as shipped.
	if Summarize(PlanStatus{Slug: "empty"}).Complete {
		t.Error("a plan with no feats must not read as complete")
	}
}

func TestSummariesSkipsBrokenPlansAndMissingDir(t *testing.T) {
	root := t.TempDir()

	// No docs/plans/ at all: an empty list, not an error.
	sums, err := Summaries(root)
	if err != nil {
		t.Fatalf("a workspace with no plans should not error: %v", err)
	}
	if len(sums) != 0 {
		t.Fatalf("want no summaries, got %d", len(sums))
	}

	write := func(slug, body string) {
		dir := Dir(root, slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("photos", "## Feats\n\n| # | Feat | Objective | Depends | Milestone | (P) | Refs |\n|---|------|-----------|---------|-----------|-----|------|\n| 1 | upload | Ingest | — | M1 | | |\n")
	// A directory with no plan.md is not a plan and must not appear as a row.
	if err := os.MkdirAll(filepath.Join(Dir(root, "not-a-plan"), "seeds"), 0o755); err != nil {
		t.Fatal(err)
	}

	sums, err = Summaries(root)
	if err != nil {
		t.Fatalf("Summaries: %v", err)
	}
	if len(sums) != 1 || sums[0].Slug != "photos" {
		t.Fatalf("want only the photos row, got %+v", sums)
	}
	if sums[0].Feats != 1 || sums[0].Done != 0 {
		t.Errorf("want 0/1 feats, got %d/%d", sums[0].Done, sums[0].Feats)
	}
}
