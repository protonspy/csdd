package session

import (
	"regexp"
	"strings"
)

// Task display parser. This deliberately differs from validator.parseTasks:
// the validator is concerned with correctness (it ignores the checkbox state
// and phase grouping), while the dashboard needs the done-state, the phase a
// task lives under, and the RED/GREEN TDD marker. The base regexes mirror the
// validator's literals so the two stay behaviourally consistent.
var (
	taskLineRe  = regexp.MustCompile(`^(\s*)-\s+\[\s*([xX ]?)\s*\]\s+(\d+(?:\.\d+)?)\.?\s+(.*)$`)
	phaseHeadRe = regexp.MustCompile(`^##\s+Phase\s+\d+\s*:?\s*(.*)$`)
	reqAnnotRe  = regexp.MustCompile(`_Requirements:\s*([\d,\.\s]+)_`)
	boundaryRe  = regexp.MustCompile(`_Boundary:\s*([A-Za-z0-9_\-<>]+)_`)
	dependsRe   = regexp.MustCompile(`_Depends:\s*([\d,\.\s]+)_`)
	parallelRe  = regexp.MustCompile(`\(P\)`)
	tddRe       = regexp.MustCompile(`(?i)^(RED|GREEN)\b`)
)

// TaskPhase groups the tasks under one "## Phase N: …" heading.
type TaskPhase struct {
	Name  string `json:"name"`
	Tasks []Task `json:"tasks"`
}

// Task is one checkbox line plus its annotations, for display on the board.
type Task struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Done         bool     `json:"done"`
	Parallel     bool     `json:"parallel"`
	Boundary     string   `json:"boundary,omitempty"`
	Requirements []string `json:"requirements,omitempty"`
	Depends      []string `json:"depends,omitempty"`
	TDD          string   `json:"tdd,omitempty"` // "RED" | "GREEN" | ""
	Indent       int      `json:"indent"`
}

// TaskStats is the aggregate progress for one tasks.md.
type TaskStats struct {
	Total int `json:"total"`
	Done  int `json:"done"`
	RED   int `json:"red"`
	GREEN int `json:"green"`
	Pct   int `json:"pct"`
}

// ParseTasks parses a tasks.md body into phase-grouped tasks and aggregate
// stats. Annotation lines (indented "_Requirements:_" etc.) following a task
// line are attributed to that task.
func ParseTasks(text string) ([]TaskPhase, TaskStats) {
	var phases []TaskPhase
	var stats TaskStats
	curPhase := -1

	addPhase := func(name string) {
		phases = append(phases, TaskPhase{Name: name})
		curPhase = len(phases) - 1
	}

	var cur *Task
	flush := func() {
		if cur == nil {
			return
		}
		phases[curPhase].Tasks = append(phases[curPhase].Tasks, *cur)
		cur = nil
	}

	for _, line := range strings.Split(text, "\n") {
		if m := phaseHeadRe.FindStringSubmatch(line); m != nil {
			flush()
			name := strings.TrimSpace(m[1])
			if name == "" {
				name = "Phase"
			}
			addPhase(name)
			continue
		}
		if m := taskLineRe.FindStringSubmatch(line); m != nil {
			flush()
			if curPhase == -1 {
				addPhase("Tasks")
			}
			t := Task{
				ID:     m[3],
				Indent: len(m[1]),
				Done:   strings.EqualFold(strings.TrimSpace(m[2]), "x"),
				Title:  strings.TrimSpace(m[4]),
			}
			applyAnnotations(&t, line)
			if tm := tddRe.FindStringSubmatch(t.Title); tm != nil {
				t.TDD = strings.ToUpper(tm[1])
			}
			cur = &t
			stats.Total++
			if t.Done {
				stats.Done++
			}
			switch t.TDD {
			case "RED":
				stats.RED++
			case "GREEN":
				stats.GREEN++
			}
			continue
		}
		if cur != nil {
			applyAnnotations(cur, line)
		}
	}
	flush()
	if stats.Total > 0 {
		stats.Pct = (stats.Done*100 + stats.Total/2) / stats.Total
	}
	return phases, stats
}

func applyAnnotations(t *Task, line string) {
	if parallelRe.MatchString(line) {
		t.Parallel = true
	}
	if m := boundaryRe.FindStringSubmatch(line); m != nil {
		t.Boundary = m[1]
	}
	if m := reqAnnotRe.FindStringSubmatch(line); m != nil {
		t.Requirements = appendTokens(t.Requirements, m[1])
	}
	if m := dependsRe.FindStringSubmatch(line); m != nil {
		t.Depends = appendTokens(t.Depends, m[1])
	}
}

func appendTokens(dst []string, raw string) []string {
	for _, tok := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(tok); s != "" {
			dst = append(dst, s)
		}
	}
	return dst
}
