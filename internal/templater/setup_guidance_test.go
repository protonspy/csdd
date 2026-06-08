package templater

import (
	"strings"
	"testing"
)

// TestSetupCommandsModelEffortGuidance covers requirements 5.1, 5.2, 5.3: both
// shipped setup commands must instruct picking model/effort by task complexity
// and omitting them (to inherit the session config) when neither is warranted.
func TestSetupCommandsModelEffortGuidance(t *testing.T) {
	cmds, err := CommandFiles(FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"csdd-setup-init.md", "csdd-setup-update.md"} {
		body := cmds[name]
		lower := strings.ToLower(body)
		for _, want := range []string{"complexity", "--effort", "--model", "inherit"} {
			if !strings.Contains(lower, strings.ToLower(want)) {
				t.Errorf("%s is missing model/effort-by-complexity guidance: no %q", name, want)
			}
		}
	}
}
