package cli

import "testing"

// TestValidateEffort pins the closed, case-sensitive effort set shared by the
// agent and skill create ops. The empty string is accepted (it means "omit the
// key"); anything else must be one of low|medium|high|xhigh|max exactly.
func TestValidateEffort(t *testing.T) {
	cases := []struct {
		name    string
		effort  string
		wantErr bool
	}{
		{"empty means omit", "", false},
		{"low", "low", false},
		{"medium", "medium", false},
		{"high", "high", false},
		{"xhigh", "xhigh", false},
		{"max", "max", false},
		{"out of set", "highish", true},
		{"wrong case High", "High", true},
		{"wrong case LOW", "LOW", true},
		{"numeric", "5", true},
		{"whitespace", " low", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEffort(tc.effort)
			if tc.wantErr && err == nil {
				t.Errorf("validateEffort(%q): expected error, got nil", tc.effort)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("validateEffort(%q): expected nil, got %v", tc.effort, err)
			}
		})
	}
}
