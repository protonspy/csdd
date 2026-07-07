package plan

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/protonspy/csdd/internal/workspace"
)

// LoadPlanJSON reads docs/plans/<slug>/plan.json. A missing file is not an error
// (a plan is unapproved until `plan approve` writes it): it returns exists=false
// with a zero-value doc so callers branch on approval without special-casing I/O.
func LoadPlanJSON(dir string) (PlanJSON, bool, error) {
	var pj PlanJSON
	data, err := os.ReadFile(planJSONPath(dir))
	if os.IsNotExist(err) {
		return pj, false, nil
	}
	if err != nil {
		return pj, false, err
	}
	if err := json.Unmarshal(data, &pj); err != nil {
		return pj, true, fmt.Errorf("parse %s: %w", planJSONPath(dir), err)
	}
	return pj, true, nil
}

// SavePlanJSON writes plan.json atomically, stamping the schema version. It is
// the plan analog of saveSpecJSON — the only writer of approval state, invoked
// solely by `plan approve` (design principle 3).
func SavePlanJSON(dir string, pj PlanJSON) error {
	if pj.SchemaVersion == 0 {
		pj.SchemaVersion = planSchemaVersion
	}
	b, err := json.MarshalIndent(pj, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return workspace.AtomicWrite(planJSONPath(dir), b, 0o644)
}
