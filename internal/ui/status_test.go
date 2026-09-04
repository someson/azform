package ui_test

import (
	"testing"

	"github.com/someson/azform/internal/ui"
)

func TestStatusOf(t *testing.T) {
	cases := []struct {
		name        string
		value       string
		sessionVars map[string]bool
		want        ui.VarStatus
	}{
		{"empty", "", map[string]bool{}, ui.VarStatusNone},
		{"literal", "my-group", map[string]bool{}, ui.VarStatusNone},
		{"var in session", "$RG", map[string]bool{"RG": true}, ui.VarStatusGreen},
		{"var missing", "$RG", map[string]bool{}, ui.VarStatusGray},
		{"braced var in session", "${RG}", map[string]bool{"RG": true}, ui.VarStatusGreen},
		{"braced var missing", "${RG}", map[string]bool{}, ui.VarStatusGray},
		{"command subst", "$(az group list)", map[string]bool{}, ui.VarStatusNeutral},
		{"command subst with var", "$(echo $RG)", map[string]bool{}, ui.VarStatusNeutral},
		{"unknown var with prefix", "$MY_TYPO", map[string]bool{"RG": true}, ui.VarStatusGray},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ui.StatusOf(tc.value, tc.sessionVars)
			if got != tc.want {
				t.Errorf("StatusOf(%q, %v) = %v, want %v", tc.value, tc.sessionVars, got, tc.want)
			}
		})
	}
}
