package web

import (
	"net/http"
	"testing"
)

const webPlanMD = `---
name: photos
status: draft
---
## Feats

| # | Feat | Objective | Depends | Milestone | (P) | Refs |
|---|------|-----------|---------|-----------|-----|------|
| 1 | upload | Ingest photos | — | M1 | | |
| 2 | thumbs | Thumbnails | upload | M1 | P | |
| 3 | search | Full-text search | — | M2 | | |

## Quality Gates

- verify: make check
`

func planWorkspace(t *testing.T) string {
	return tempWorkspace(t, map[string]string{
		"docs/plans/photos/plan.md": webPlanMD,
		// upload is done, thumbs is blocked, search is pending.
		"specs/upload/spec.json":           `{"feature_name":"upload","ready_for_implementation":true,"approvals":{"requirements":{"approved":true},"design":{"approved":true},"tasks":{"approved":true}}}`,
		"specs/upload/tasks.md":            "- [x] 1. done\n",
		".csdd/plan/photos/blocked/thumbs": "gate failed\n",
		"docs/plans/photos/log.md":         "# Journal\n## [2026-07-07] task 1 | upload | done\n",
	})
}

func TestAPIPlansList(t *testing.T) {
	srv := testServer(t, planWorkspace(t))
	var plans []planSummary
	if code := getJSON(t, srv.URL+"/api/plans", &plans); code != http.StatusOK {
		t.Fatalf("GET /api/plans = %d", code)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	p := plans[0]
	if p.Slug != "photos" || p.Feats != 3 {
		t.Errorf("unexpected summary: %+v", p)
	}
	if p.Done != 1 {
		t.Errorf("expected 1 done feat (upload), got %d", p.Done)
	}
	if p.Blocked != 1 {
		t.Errorf("expected 1 blocked feat (thumbs), got %d", p.Blocked)
	}
	if p.Approved {
		t.Errorf("plan is unapproved; summary should reflect that")
	}
}

func TestAPIPlanDetail(t *testing.T) {
	srv := testServer(t, planWorkspace(t))
	var d planDetail
	if code := getJSON(t, srv.URL+"/api/plan/photos", &d); code != http.StatusOK {
		t.Fatalf("GET /api/plan/photos = %d", code)
	}
	if len(d.Feats) != 3 {
		t.Fatalf("expected 3 feats, got %d", len(d.Feats))
	}
	states := map[string]string{}
	for _, f := range d.Feats {
		states[f.Slug] = f.State
	}
	if states["upload"] != "done" || states["thumbs"] != "blocked" || states["search"] != "pending" {
		t.Errorf("derived feat states wrong: %v", states)
	}
	// Milestone progress: M1 has upload(done)+thumbs(blocked)=1/2, M2 search 0/1.
	var m1 milestoneProgress
	for _, m := range d.Milestones {
		if m.Name == "M1" {
			m1 = m
		}
	}
	if m1.Total != 2 || m1.Done != 1 {
		t.Errorf("M1 progress = %+v, want 1/2", m1)
	}
	// Feat metadata is merged in (objective + depends).
	for _, f := range d.Feats {
		if f.Slug == "thumbs" {
			if f.Objective != "Thumbnails" || len(f.Depends) != 1 || f.Depends[0] != "upload" {
				t.Errorf("thumbs metadata not merged: %+v", f)
			}
			if f.BlockReason == "" {
				t.Errorf("blocked feat should carry its reason")
			}
		}
	}
}

func TestAPIPlanDetailTraversalGuard(t *testing.T) {
	srv := testServer(t, planWorkspace(t))
	// A traversal slug must be rejected (404), never resolve outside docs/plans/.
	if code := getJSON(t, srv.URL+"/api/plan/..%2f..%2fetc", nil); code == http.StatusOK {
		t.Errorf("traversal slug should not succeed, got %d", code)
	}
}
