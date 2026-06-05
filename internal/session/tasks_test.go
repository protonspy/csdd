package session

import "testing"

const sampleTasks = `# Tasks

## Phase 1: Foundation

- [x] 1. Foundation _Boundary: Core_
  - [x] 1.1 Minimal structure
    - _Requirements: 1.1_

## Phase 2: Core

- [ ] 2. Core capability _Boundary: Core_
  - [x] 2.1 RED — write the failing test
    - _Requirements: 1.1, 1.2_
    - _Depends: 1.1_
  - [ ] 2.2 GREEN — minimal implementation (P)
    - _Requirements: 1.1_
`

func TestParseTasksStats(t *testing.T) {
	phases, stats := ParseTasks(sampleTasks)
	if stats.Total != 5 || stats.Done != 3 {
		t.Fatalf("total/done = %d/%d, want 5/3", stats.Total, stats.Done)
	}
	if stats.RED != 1 || stats.GREEN != 1 {
		t.Errorf("red/green = %d/%d, want 1/1", stats.RED, stats.GREEN)
	}
	if stats.Pct != 60 {
		t.Errorf("pct = %d, want 60", stats.Pct)
	}
	if len(phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(phases))
	}
	if phases[0].Name != "Foundation" || phases[1].Name != "Core" {
		t.Errorf("phase names = %q/%q", phases[0].Name, phases[1].Name)
	}
	if len(phases[0].Tasks) != 2 || len(phases[1].Tasks) != 3 {
		t.Fatalf("phase task counts = %d/%d, want 2/3", len(phases[0].Tasks), len(phases[1].Tasks))
	}
}

func TestParseTasksFields(t *testing.T) {
	phases, _ := ParseTasks(sampleTasks)
	core := phases[1].Tasks // 2, 2.1, 2.2

	red := core[1]
	if red.ID != "2.1" || red.TDD != "RED" {
		t.Errorf("red task id/tdd = %q/%q", red.ID, red.TDD)
	}
	if !red.Done {
		t.Errorf("2.1 should be done")
	}
	if len(red.Requirements) != 2 || red.Requirements[0] != "1.1" || red.Requirements[1] != "1.2" {
		t.Errorf("2.1 requirements = %v, want [1.1 1.2]", red.Requirements)
	}
	if len(red.Depends) != 1 || red.Depends[0] != "1.1" {
		t.Errorf("2.1 depends = %v, want [1.1]", red.Depends)
	}

	green := core[2]
	if green.ID != "2.2" || green.TDD != "GREEN" || !green.Parallel {
		t.Errorf("green task = %+v, want id 2.2, tdd GREEN, parallel true", green)
	}
	if green.Done {
		t.Errorf("2.2 should not be done")
	}

	major := phases[0].Tasks[0]
	if major.Boundary != "Core" {
		t.Errorf("task 1 boundary = %q, want Core", major.Boundary)
	}
	sub := phases[0].Tasks[1]
	if sub.Indent != 2 {
		t.Errorf("task 1.1 indent = %d, want 2", sub.Indent)
	}
}

func TestParseTasksEmpty(t *testing.T) {
	phases, stats := ParseTasks("# Tasks\n\nno checkboxes here\n")
	if stats.Total != 0 || stats.Pct != 0 {
		t.Errorf("empty tasks: total/pct = %d/%d, want 0/0", stats.Total, stats.Pct)
	}
	if len(phases) != 0 {
		t.Errorf("empty tasks: phases = %d, want 0", len(phases))
	}
}

func TestParseTasksNoPhaseHeading(t *testing.T) {
	// Tasks with no "## Phase" heading fall into a synthetic "Tasks" bucket.
	phases, stats := ParseTasks("- [ ] 1. do a thing\n    - _Requirements: 1.1_\n")
	if len(phases) != 1 || phases[0].Name != "Tasks" {
		t.Fatalf("phases = %+v, want one named Tasks", phases)
	}
	if stats.Total != 1 {
		t.Errorf("total = %d, want 1", stats.Total)
	}
}
