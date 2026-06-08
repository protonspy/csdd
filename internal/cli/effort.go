package cli

import (
	"fmt"
	"strings"
)

// effortLevels is the closed, case-sensitive, ordered set of effort levels
// Claude Code honors in agent/skill frontmatter. It is the single source of
// truth shared by the agent and skill create ops so the rule cannot drift
// between them — both the membership check and the error message derive from it.
var effortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// validateEffort reports whether an effort value may be written to frontmatter.
// The empty string is accepted and means "omit the key" (inherit the session
// configuration); any other value must appear in effortLevels exactly. It never
// touches the filesystem, so callers run it before creating any artifact.
func validateEffort(effort string) error {
	if effort == "" {
		return nil
	}
	for _, lvl := range effortLevels {
		if effort == lvl {
			return nil
		}
	}
	return fmt.Errorf("invalid --effort %q: must be one of %s", effort, strings.Join(effortLevels, ", "))
}
