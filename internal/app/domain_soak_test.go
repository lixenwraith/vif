package app

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
)

// TestDomainAuditSoakClean asserts the audit counts zero over a full soak, which is
// 4.16(3) without a log grep. The pin is process-wide: never t.Parallel here.
// The pin also survives between ticks, so component attaches made by event handlers
// outside processTick are audited too.
func TestDomainAuditSoakClean(t *testing.T) {
	engine.PinDomainAudit(true)
	defer engine.PinDomainAudit(false)

	a := mustHeadless(t, 0xD0A17, 120, 40)
	defer a.Close()

	if _, err := RunScript(a, DefaultScript(0xD0A17, 3000)); err != nil {
		t.Fatalf("soak: %v", err)
	}
	if n := engine.DomainMismatches(); n != 0 {
		t.Fatalf("domain audit counted %d violations:\n  %s",
			n, strings.Join(engine.DomainViolations(), "\n  "))
	}
}
