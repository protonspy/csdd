package cli

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/protonspy/csdd/internal/render"
	"github.com/protonspy/csdd/internal/validator"
)

// addJSON binds the conventional --json flag. Every command that has a
// machine-readable variant uses this so the flag name and help text are
// identical across the CLI — the surface an AI agent scripts against.
func addJSON(fs *flag.FlagSet, dst *bool) {
	fs.BoolVar(dst, "json", false, "Emit machine-readable JSON on stdout instead of the human table.")
}

// emitJSON writes v as indented JSON to stdout and returns a process exit code.
// Diagnostics still go to stderr via render, so stdout stays a clean JSON stream
// an agent can parse.
func emitJSON(v any) int {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		render.Err(err.Error())
		return 1
	}
	fmt.Println(string(b))
	return 0
}

// issueJSON is the stable machine-readable shape of a validation issue. It uses
// explicit snake_case keys so the schema is decoupled from validator.Issue's Go
// field names.
type issueJSON struct {
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
	Msg  string `json:"message"`
}

func issuesToJSON(in []validator.Issue) []issueJSON {
	out := make([]issueJSON, 0, len(in))
	for _, i := range in {
		out = append(out, issueJSON{File: i.File, Line: i.Line, Msg: i.Msg})
	}
	return out
}

// validationJSON is the payload for every `... validate --json` command.
type validationJSON struct {
	Target string      `json:"target"`
	OK     bool        `json:"ok"`
	Issues []issueJSON `json:"issues"`
}

// planSummaryJSON is one row of `plan list --json`. It keys the slug as "plan",
// matching what `plan status --json` calls it, so an agent reads the same field
// name whether it listed the plans or asked about one.
type planSummaryJSON struct {
	Slug     string `json:"plan"`
	Name     string `json:"name,omitempty"`
	Approved bool   `json:"approved"`
	Drift    bool   `json:"drift"`
	Feats    int    `json:"feats"`
	Done     int    `json:"done"`
	Complete bool   `json:"complete"`
}

// specSummaryJSON is one row of `spec list --json`.
type specSummaryJSON struct {
	Feature  string   `json:"feature"`
	Phase    string   `json:"phase"`
	Approved []string `json:"approved"`
	Ready    bool     `json:"ready_for_implementation"`
}
