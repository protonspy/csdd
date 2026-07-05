package cli

import (
	"fmt"
	"strings"
)

// agentColors is the closed, case-sensitive set of display colors Claude Code
// honors in a sub-agent's `color` frontmatter field (shown in the task list and
// transcript). Like effortLevels, it is the single source of truth for both the
// membership check and the error message, so the two cannot drift.
var agentColors = []string{"red", "blue", "green", "yellow", "purple", "orange", "pink", "cyan"}

// validateColor reports whether a color value may be written to frontmatter. The
// empty string is accepted and means "omit the key" (no color assigned); any
// other value must appear in agentColors exactly. It never touches the
// filesystem, so callers run it before creating any artifact.
func validateColor(color string) error {
	if color == "" {
		return nil
	}
	for _, c := range agentColors {
		if color == c {
			return nil
		}
	}
	return fmt.Errorf("invalid --color %q: must be one of %s", color, strings.Join(agentColors, ", "))
}
