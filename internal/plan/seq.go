package plan

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protonspy/csdd/internal/paths"
	"github.com/protonspy/csdd/internal/textutil"
	"github.com/protonspy/csdd/internal/validator"
)

// Step names the next unit of work the runner (or a human) should perform for a
// feat. The spec-phase steps drive the spec flow; a task step names one leaf task
// in tasks.md to implement.
const (
	StepSpecRequirements = "spec-requirements"
	StepSpecDesign       = "spec-design"
	StepSpecTasks        = "spec-tasks"
	StepTaskPrefix       = "task " // followed by the task id, e.g. "task 1.2"
)

// Step is the sequencer's answer: which feat, which step, and why. It is a pure
// function of the parsed plan, the derived statuses, and block markers (§5.5), so
// the same workspace always yields the same next step.
type Step struct {
	Feat   string `json:"feat"`
	Step   string `json:"step"`
	Reason string `json:"reason"`
}

// Sequencer outcomes. These map to distinct CLI exit codes (R6.3) so an agent
// can branch on $? without parsing.
const (
	SeqStep     = iota // 0: a step is available
	_                  // (1,2 reserved: generic/usage errors)
	_                  //
	SeqComplete        // 3: every feat is done
	SeqNothing         // 4: work remains but nothing is unblocked
	SeqNotReady        // 5: plan not approved, or drifted
)

// Next selects the next step to perform (R6.1). It walks feats in milestone order
// then table order, skips feats that are done or blocked, requires every
// dependency to be done, and returns the first actionable step within the first
// eligible feat. The returned int is the sequencer outcome (SeqStep/…); on
// SeqStep the Step is populated.
//
// requireApproved gates autonomy: when true (the runner), an unapproved or
// drifted plan returns SeqNotReady. A human following `plan next` by hand passes
// false and sequences a draft freely.
func Next(root string, doc *PlanDoc, requireApproved bool) (Step, int, error) {
	if requireApproved {
		approved, drift, err := IsApproved(root, doc.Slug)
		if err != nil {
			return Step{}, SeqNotReady, err
		}
		if !approved || drift {
			return Step{}, SeqNotReady, nil
		}
	}

	statuses := map[string]FeatStatus{}
	for _, f := range doc.Feats {
		statuses[f.Slug] = deriveFeatStatus(root, doc.Slug, f)
	}

	allDone := true // vacuously true for an empty plan → SeqComplete
	for _, f := range orderFeats(doc.Feats) {
		state := statuses[f.Slug].State
		if state != StateDone {
			allDone = false
		}
		if state == StateDone || state == StateBlocked {
			continue
		}
		// Dependencies must all be done before this feat is eligible. A feat
		// waiting on a not-yet-done dep is skipped; the dep itself (if actionable)
		// is reached earlier or later in the same walk.
		if _, unmet := firstUnmetDep(f, statuses); unmet {
			continue
		}
		return nextStepForFeat(root, f, statuses[f.Slug]), SeqStep, nil
	}

	if allDone {
		return Step{}, SeqComplete, nil
	}
	// Work remains but nothing is unblocked: every incomplete feat is blocked or
	// its dependency chain bottoms out at a blocked feat.
	return Step{}, SeqNothing, nil
}

// orderFeats returns feats in milestone order (first-seen) then table order — the
// total deterministic order the sequencer walks (§5.5).
func orderFeats(feats []Feat) []Feat {
	milestoneRank := map[string]int{}
	next := 0
	for _, f := range feats {
		if _, ok := milestoneRank[f.Milestone]; !ok {
			milestoneRank[f.Milestone] = next
			next++
		}
	}
	idx := make([]int, len(feats))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		fa, fb := feats[idx[a]], feats[idx[b]]
		if milestoneRank[fa.Milestone] != milestoneRank[fb.Milestone] {
			return milestoneRank[fa.Milestone] < milestoneRank[fb.Milestone]
		}
		return idx[a] < idx[b] // stable table order within a milestone
	})
	out := make([]Feat, len(feats))
	for i, j := range idx {
		out[i] = feats[j]
	}
	return out
}

// firstUnmetDep returns the first dependency of f that is not yet done, and
// whether one exists. Unknown deps (not in statuses) are treated as unmet so a
// misdeclared dependency never lets a feat run ahead of validation.
func firstUnmetDep(f Feat, statuses map[string]FeatStatus) (string, bool) {
	for _, dep := range f.Depends {
		st, ok := statuses[dep]
		if !ok || st.State != StateDone {
			return dep, true
		}
	}
	return "", false
}

// nextStepForFeat returns the actionable step within an eligible feat: advance
// the spec flow through requirements → design → tasks, then implement the next
// unchecked leaf task honoring tasks.md order and _Depends:_.
func nextStepForFeat(root string, f Feat, fs FeatStatus) Step {
	switch fs.State {
	case StatePending:
		return Step{Feat: f.Slug, Step: StepSpecRequirements, Reason: "no spec yet — start the requirements phase"}
	case StateRequirements:
		return Step{Feat: f.Slug, Step: StepSpecRequirements, Reason: "requirements not yet approved"}
	case StateDesign:
		return Step{Feat: f.Slug, Step: StepSpecDesign, Reason: "requirements approved — draft the design"}
	case StateTasks:
		return Step{Feat: f.Slug, Step: StepSpecTasks, Reason: "design approved — draft the tasks"}
	default:
		// ready / implementing: the next unchecked leaf task.
		if id, ok := nextTask(filepath.Join(paths.Specs(root), f.Slug)); ok {
			return Step{Feat: f.Slug, Step: StepTaskPrefix + id, Reason: "implement task " + id}
		}
		// No leaf task found though the feat isn't marked done — fall back to tasks.
		return Step{Feat: f.Slug, Step: StepSpecTasks, Reason: "no actionable task found; review tasks.md"}
	}
}

// taskRow is a minimal tasks.md task line used for sequencing.
type taskRow struct {
	id      string
	done    bool
	depends []string
}

// nextTask returns the id of the next task to implement in specs/<feat>/tasks.md:
// the first unchecked leaf task (no other task has it as a numeric prefix) whose
// _Depends:_ tasks are all checked, in file order. Returns ok=false when every
// task is checked or none parse.
func nextTask(specDir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(specDir, "tasks.md"))
	if err != nil {
		return "", false
	}
	rows := parseTaskRows(string(data))
	if len(rows) == 0 {
		return "", false
	}
	// Leaf = no other task id has this id as a "<id>." prefix.
	ids := map[string]bool{}
	doneByID := map[string]bool{}
	for _, r := range rows {
		ids[r.id] = true
		doneByID[r.id] = r.done
	}
	isLeaf := func(id string) bool {
		for other := range ids {
			if other != id && strings.HasPrefix(other, id+".") {
				return false
			}
		}
		return true
	}
	for _, r := range rows {
		if r.done || !isLeaf(r.id) {
			continue
		}
		if depsMet(r.depends, doneByID) {
			return r.id, true
		}
	}
	return "", false
}

// depsMet reports whether every listed dependency task is checked. An unknown
// dependency is treated as unmet (conservative).
func depsMet(depends []string, done map[string]bool) bool {
	for _, d := range depends {
		if !done[d] {
			return false
		}
	}
	return true
}

// parseTaskRows extracts task lines and their _Depends:_ from tasks.md, reusing
// the validator's canonical grammar and fence masking so sequencing agrees with
// linting and the dashboard on what a task is.
func parseTaskRows(content string) []taskRow {
	text := validator.MaskCodeFences(textutil.NormalizeNewlines(content))
	lines := strings.Split(text, "\n")
	var out []taskRow
	var cur *taskRow
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	var block []string
	commit := func() {
		if cur == nil {
			return
		}
		joined := strings.Join(block, "\n")
		if m := validator.DependsAnnotRe.FindStringSubmatch(joined); m != nil {
			for _, tok := range strings.Split(m[1], ",") {
				if t := strings.TrimSpace(tok); t != "" {
					cur.depends = append(cur.depends, t)
				}
			}
		}
	}
	for _, line := range lines {
		if m := validator.TaskLineRe.FindStringSubmatch(line); m != nil {
			commit()
			flush()
			block = []string{line}
			cur = &taskRow{id: m[3], done: strings.EqualFold(strings.TrimSpace(m[2]), "x")}
			continue
		}
		if cur != nil {
			block = append(block, line)
		}
	}
	commit()
	flush()
	return out
}
