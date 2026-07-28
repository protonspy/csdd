package plan

import (
	"fmt"
	"sort"
)

// What a run spent, read back out of sessions.jsonl.
//
// The file has always recorded every attempt with its cost (R9.2) and nothing has
// ever read it back except the resume path. That gap is not cosmetic: a real run
// (`frontend-design-refresh`, violet, 2026-07-27) recorded $20.01 across three
// settled sessions while sixteen further attempts sat on disk as bare `started`
// rows, and the only way to notice was to parse the JSONL by hand.
//
// So this report answers two questions, and the second matters more than the
// first: what did the run spend, and how much of what it spent can it still
// account for.

// CostReport is a run's spend, per feat and in total.
type CostReport struct {
	Plan   string     `json:"plan"`
	Feats  []FeatCost `json:"feats"`
	Totals FeatCost   `json:"totals"`
	// ByModel is the whole run's spend split across the models that billed it.
	// The plan loop tiers its work on purpose, so this is where "the orchestrator
	// did the implementers' job" becomes visible as a number.
	ByModel []ModelTokens `json:"by_model,omitempty"`
	// Unmeasured is every attempt opened and never settled — a session the host
	// outlived. Its cost is real and absent from every figure above, which is why
	// it is reported as a named list rather than folded into a count.
	Unmeasured []string `json:"unmeasured_attempts,omitempty"`
}

// FeatCost is one feat's share of a run (or, as Totals, the whole of it).
type FeatCost struct {
	Feat string `json:"feat,omitempty"`
	// Attempts is how many sessions were OPENED for this feat, settled or not.
	Attempts int `json:"attempts"`
	// Settled is how many of them came back with a result to record.
	Settled int `json:"settled"`
	// Gated is how many `done` verdicts were refused and handed back — sessions
	// paid for in full that delivered nothing (R10.3).
	Gated     int            `json:"gated"`
	Delivered bool           `json:"delivered"`
	CostUSD   float64        `json:"cost_usd"`
	Tokens    SessionTokens  `json:"tokens"`
	Turns     int            `json:"turns,omitempty"`
	ByStatus  map[string]int `json:"by_status,omitempty"`
}

// BuildCostReport reads sessions.jsonl and aggregates it. A missing file yields an
// empty report rather than an error: a plan that has never been run has spent
// nothing, which is a fine thing to report.
func BuildCostReport(root, slug string) CostReport {
	rep := CostReport{Plan: slug, Totals: FeatCost{ByStatus: map[string]int{}}}
	byFeat := map[string]*FeatCost{}
	byModel := map[string]*ModelTokens{}
	open := map[string]bool{}
	order := []string{}

	feat := func(name string) *FeatCost {
		if f, ok := byFeat[name]; ok {
			return f
		}
		f := &FeatCost{Feat: name, ByStatus: map[string]int{}}
		byFeat[name] = f
		order = append(order, name)
		return f
	}

	for _, r := range LoadSessionRecords(root, slug) {
		f := feat(r.Feat)
		key := fmt.Sprintf("%s#%d", r.Feat, r.Attempt)
		if r.Status == SessionStarted {
			f.Attempts++
			rep.Totals.Attempts++
			open[key] = true
			continue
		}
		delete(open, key)
		f.Settled++
		f.ByStatus[r.Status]++
		f.CostUSD += r.CostUSD
		f.Turns += r.NumTurns
		f.Tokens = addTokens(f.Tokens, r.Tokens)
		if r.Gated {
			f.Gated++
		}
		if r.Status == SessionDone {
			f.Delivered = true
		}
		rep.Totals.Settled++
		rep.Totals.ByStatus[r.Status]++
		rep.Totals.CostUSD += r.CostUSD
		rep.Totals.Turns += r.NumTurns
		rep.Totals.Tokens = addTokens(rep.Totals.Tokens, r.Tokens)
		if r.Gated {
			rep.Totals.Gated++
		}
		for _, m := range r.ByModel {
			acc, ok := byModel[m.Model]
			if !ok {
				acc = &ModelTokens{Model: m.Model}
				byModel[m.Model] = acc
			}
			acc.Tokens = addTokens(acc.Tokens, m.Tokens)
			acc.CostUSD += m.CostUSD
		}
	}

	// An attempt that a delivered feat opened and never settled is still an
	// unmeasured attempt: the feat landed on a later try, and whatever the crashed
	// one spent is gone all the same.
	for key := range open {
		rep.Unmeasured = append(rep.Unmeasured, key)
	}
	sort.Strings(rep.Unmeasured)
	for _, name := range order {
		rep.Feats = append(rep.Feats, *byFeat[name])
	}
	for _, m := range byModel {
		rep.ByModel = append(rep.ByModel, *m)
	}
	sort.Slice(rep.ByModel, func(i, j int) bool { return rep.ByModel[i].Model < rep.ByModel[j].Model })
	return rep
}

func addTokens(a, b SessionTokens) SessionTokens {
	a.Input += b.Input
	a.Output += b.Output
	a.CacheRead += b.CacheRead
	a.CacheCreation += b.CacheCreation
	return a
}

// ModelTotal is the sum of the per-model breakdown. Comparing it against Totals
// .Tokens is the point: the two are read from different fields of the same event,
// so a gap between them is the sub-agent work the top-level `usage` did not count.
func (r CostReport) ModelTotal() SessionTokens {
	var t SessionTokens
	for _, m := range r.ByModel {
		t = addTokens(t, m.Tokens)
	}
	return t
}
