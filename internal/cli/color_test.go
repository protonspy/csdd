package cli

import "testing"

// TestValidateColor pins the closed, case-sensitive color set Claude Code honors
// in a sub-agent's `color` frontmatter. The empty string is accepted (it means
// "omit the key"); anything else must be one of the eight names exactly.
func TestValidateColor(t *testing.T) {
	cases := []struct {
		name    string
		color   string
		wantErr bool
	}{
		{"empty means omit", "", false},
		{"red", "red", false},
		{"blue", "blue", false},
		{"green", "green", false},
		{"yellow", "yellow", false},
		{"purple", "purple", false},
		{"orange", "orange", false},
		{"pink", "pink", false},
		{"cyan", "cyan", false},
		{"out of set", "teal", true},
		{"wrong case Red", "Red", true},
		{"hex not allowed", "#ff0000", true},
		{"whitespace", " red", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateColor(tc.color)
			if tc.wantErr && err == nil {
				t.Errorf("validateColor(%q): expected error, got nil", tc.color)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateColor(%q): expected nil, got %v", tc.color, err)
			}
		})
	}
}
