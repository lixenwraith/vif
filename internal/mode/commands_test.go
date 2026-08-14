package mode_test

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/help"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/mode"
)

// TestCommandsDocumented asserts every documented command exists. The reverse
// direction is not asserted: aliases are deliberately undocumented.
func TestCommandsDocumented(t *testing.T) {
	known := make(map[string]struct{}, len(mode.CommandNames()))
	for _, n := range mode.CommandNames() {
		known[n] = struct{}{}
	}

	for _, topic := range help.Topics(input.DefaultKeyTable()) {
		for _, e := range topic.Entries {
			for tok := range strings.FieldsSeq(e.Keys) {
				name, ok := strings.CutPrefix(tok, ":")
				if !ok || name == "" {
					continue // Bare ':' is the mode-switch key, not a command
				}
				if strings.ContainsAny(name, "{<") {
					continue // Placeholder, e.g. :{command}
				}
				if _, exists := known[name]; !exists {
					t.Errorf("%s: documented command %q does not exist", topic.Title, tok)
				}
			}
		}
	}

}
