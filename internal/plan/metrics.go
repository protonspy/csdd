package plan

import (
	"encoding/json"
	"sort"
	"time"
)

// SessionMetrics is what one claude session cost, read from the final `result`
// event of its stream (§1.5). It is OBSERVED from the session's envelope, never
// declared by the model, which is why it is carried separately from the Verdict:
// a session that crashed or returned an unparseable verdict still spent real time
// and real money, and that is precisely the attempt worth recording (R9.2).
//
// Every field is best-effort. A stream that ended before its `result` event
// yields a zero SessionMetrics rather than an error — instrumentation must never
// be able to fail a run.
type SessionMetrics struct {
	Duration    time.Duration // wall clock the session took (duration_ms)
	APIDuration time.Duration // time spent in API calls (duration_api_ms)
	CostUSD     float64       // total_cost_usd
	NumTurns    int           // assistant turns the session took
	Tokens      SessionTokens
	// Models are the model IDs that billed tokens, sorted. The plan loop tiers
	// its work (an orchestrator model plus the implementer sub-agent's own), so
	// which models ran is part of comparing one run against another (R9.3).
	Models []string
	// ByModel is what each model billed, sorted by model ID.
	//
	// It exists because Tokens alone cannot answer the question a cost report is
	// for. The plan loop deliberately tiers its work — an orchestrator that decides
	// and sub-agents that execute on a cheaper model — and two sessions with nearly
	// identical token counts have been observed costing 2.4× different amounts
	// purely because one delegated and the other did not. Which tier the tokens
	// landed on is therefore the single most actionable figure in the record, and
	// until now the runner parsed `modelUsage` only far enough to read the KEYS and
	// threw the counts away.
	ByModel []ModelTokens
}

// ModelTokens is one model's share of a session.
type ModelTokens struct {
	Model   string        `json:"model"`
	Tokens  SessionTokens `json:"tokens"`
	CostUSD float64       `json:"cost_usd,omitempty"`
}

// SessionTokens is the token usage a session billed, split the way the API
// reports it — cache reads are an order of magnitude cheaper than fresh input,
// so collapsing them into one number would make two runs look alike when their
// costs differ by 10×.
type SessionTokens struct {
	Input         int `json:"input,omitempty"`
	Output        int `json:"output,omitempty"`
	CacheRead     int `json:"cache_read,omitempty"`
	CacheCreation int `json:"cache_creation,omitempty"`
}

// Total is every token the session billed, for a single headline figure.
func (t SessionTokens) Total() int {
	return t.Input + t.Output + t.CacheRead + t.CacheCreation
}

// Empty reports whether nothing was observed — a stream that never produced a
// usable `result` event. Callers use it to omit cost columns rather than print
// a row of confident zeros.
func (m SessionMetrics) Empty() bool {
	return m.Duration == 0 && m.CostUSD == 0 && m.Tokens.Total() == 0 && m.NumTurns == 0
}

// resultEvent is the subset of the `result` event this package reads. The event
// carries far more (ttft, permission denials, the structured output itself);
// decoding only these fields keeps a CLI-side addition from breaking the parse.
type resultEvent struct {
	DurationMS    int64   `json:"duration_ms"`
	DurationAPIMS int64   `json:"duration_api_ms"`
	CostUSD       float64 `json:"total_cost_usd"`
	NumTurns      int     `json:"num_turns"`
	Usage         struct {
		Input         int `json:"input_tokens"`
		Output        int `json:"output_tokens"`
		CacheRead     int `json:"cache_read_input_tokens"`
		CacheCreation int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	ModelUsage map[string]json.RawMessage `json:"modelUsage"`
}

// parseSessionMetrics reads the metrics out of a `result` event line. Anything
// unparseable yields the zero value: this is bookkeeping, and losing a cost
// figure must never take down a session that otherwise succeeded.
func parseSessionMetrics(raw []byte) SessionMetrics {
	var ev resultEvent
	if json.Unmarshal(raw, &ev) != nil {
		return SessionMetrics{}
	}
	m := SessionMetrics{
		Duration:    time.Duration(ev.DurationMS) * time.Millisecond,
		APIDuration: time.Duration(ev.DurationAPIMS) * time.Millisecond,
		CostUSD:     ev.CostUSD,
		NumTurns:    ev.NumTurns,
		Tokens: SessionTokens{
			Input:         ev.Usage.Input,
			Output:        ev.Usage.Output,
			CacheRead:     ev.Usage.CacheRead,
			CacheCreation: ev.Usage.CacheCreation,
		},
	}
	for name, raw := range ev.ModelUsage {
		m.Models = append(m.Models, name)
		m.ByModel = append(m.ByModel, ModelTokens{Model: name, Tokens: modelTokens(raw), CostUSD: modelCost(raw)})
	}
	sort.Strings(m.Models) // deterministic: the record is diffed across runs
	sort.Slice(m.ByModel, func(i, j int) bool { return m.ByModel[i].Model < m.ByModel[j].Model })
	return m
}

// modelUsageKeys maps each field to every spelling the CLI has been observed to
// use for it. `modelUsage` is assembled by the Claude CLI rather than returned by
// the API, and the two do not agree on case — the envelope's own `usage` block is
// snake_case while CLI-side aggregations are commonly camelCase. Reading both
// costs nothing and means a rename upstream degrades to a zero in one column
// instead of silently zeroing the whole breakdown.
var modelUsageKeys = map[string][]string{
	"input":          {"input_tokens", "inputTokens"},
	"output":         {"output_tokens", "outputTokens"},
	"cache_read":     {"cache_read_input_tokens", "cacheReadInputTokens"},
	"cache_creation": {"cache_creation_input_tokens", "cacheCreationInputTokens"},
	"cost":           {"cost_usd", "costUSD", "totalCostUSD", "total_cost_usd"},
}

// modelTokens reads one model's entry out of `modelUsage`. Anything unparseable
// yields zeros: this is bookkeeping, and a breakdown that cannot be read must not
// take down a session that otherwise succeeded.
func modelTokens(raw json.RawMessage) SessionTokens {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return SessionTokens{}
	}
	return SessionTokens{
		Input:         pickNum(m, modelUsageKeys["input"]),
		Output:        pickNum(m, modelUsageKeys["output"]),
		CacheRead:     pickNum(m, modelUsageKeys["cache_read"]),
		CacheCreation: pickNum(m, modelUsageKeys["cache_creation"]),
	}
}

func modelCost(raw json.RawMessage) float64 {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return 0
	}
	for _, k := range modelUsageKeys["cost"] {
		if f, ok := m[k].(float64); ok {
			return f
		}
	}
	return 0
}

// pickNum returns the first key that holds a number, as an int.
func pickNum(m map[string]any, keys []string) int {
	for _, k := range keys {
		if f, ok := m[k].(float64); ok {
			return int(f)
		}
	}
	return 0
}

// SessionOutcome is everything one session produced: the verdict it DECLARED and
// what it COST. The two are separate on purpose — the verdict is the model's
// claim (which the loop now gates, R10) while the metrics are observed fact, and
// a failed session has the second without the first.
type SessionOutcome struct {
	Verdict Verdict
	Metrics SessionMetrics
}
