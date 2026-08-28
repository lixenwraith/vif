package engine

// The audit table is generated from manifest.Components into component_domain_gen.go;
// this file holds the runtime audit that reads it.

import (
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// domainViolationCap bounds the retained descriptions; the counter is the alarm,
// this is the diagnosis
const domainViolationCap = 16

// domainAudit gates the per-attachment check, refreshed once per tick. The pin
// survives that refresh and the counter lets a soak assert zero instead of grepping logs.
var (
	domainAudit    atomic.Bool
	domainAuditPin atomic.Bool
	domainMismatch atomic.Int64

	// Appended under updateMutex, read after the run; the mutex covers a live
	// reader that holds neither.
	violationMu sync.Mutex
	violations  []string
)

// SetDomainAudit refreshes the per-tick gate; a pinned audit stays on regardless
func SetDomainAudit(on bool) { domainAudit.Store(on || domainAuditPin.Load()) }

// PinDomainAudit forces the audit on for a harness run and clears the counter.
// Process-wide: a test holding the pin must not run beside another App.
func PinDomainAudit(on bool) {
	domainAuditPin.Store(on)
	domainAudit.Store(on)
	if on {
		domainMismatch.Store(0)
		violationMu.Lock()
		violations = violations[:0]
		violationMu.Unlock()
	}
}

// DomainMismatches returns the violations counted while the audit was active
func DomainMismatches() int64 { return domainMismatch.Load() }

// DomainViolations returns the first retained violation descriptions
func DomainViolations() []string {
	violationMu.Lock()
	defer violationMu.Unlock()
	return slices.Clone(violations)
}

// recordViolation counts one violation and retains its description up to the cap
func recordViolation(what string) {
	domainMismatch.Add(1)
	violationMu.Lock()
	if len(violations) < domainViolationCap {
		violations = append(violations, what)
	}
	violationMu.Unlock()
}

// auditScope names the system whose Update is running, so a violation names a
// writer. Written only under updateMutex, by UpdateLocked.
var auditScope struct {
	name   string
	domain SystemDomain
	active bool
}

// setAuditScope attributes subsequent component writes to a system
func setAuditScope(name string, d SystemDomain) {
	auditScope.name, auditScope.domain, auditScope.active = name, d, true
}

func clearAuditScope() { auditScope.name, auditScope.active = "", false }

// auditScopeName attributes a write to the running system, or to the settle pass
// when no system is on the stack
func auditScopeName() string {
	if auditScope.active {
		return auditScope.name
	}
	return "event"
}

// auditComponentDomain reports an attachment contradicting the entity's domain
// tag. Diagnostic only; it never blocks the write.
func auditComponentDomain(e core.Entity, bit uint64) {
	rule, ok := componentDomains[bit]
	if !ok || rule.domain == e.Domain() {
		return
	}
	recordViolation("component " + rule.field + " wants " + rule.domain.String() +
		", entity is " + e.Domain().String() + " id " + strconv.FormatUint(e.ID(), 10) +
		" (in " + auditScopeName() + ")")
	vlog.Warn("domain", "msg", "component domain mismatch",
		"component", rule.field,
		"want", rule.domain.String(),
		"got", e.Domain().String(),
		"id", e.ID(),
		"system", auditScopeName())
}

// auditEntityDomain reports a shared-profile system writing a player entity, which
// D-1 forbids. A system that stamped itself into the player domain (D-7, D-12) is exempt.
// Attach-only: SetComponent and SetPosition reach AddComponentMask on first insert,
// so a GetPtr mutation or a MoveUnsafe is the static checker's business, not this one.
func auditEntityDomain(w *World, e core.Entity) {
	if !auditScope.active || auditScope.domain != SystemShared ||
		e.Domain() != core.DomainPlayer || w.Domain() == core.DomainPlayer {
		return
	}
	recordViolation("shared system " + auditScope.name + " wrote player entity id " +
		strconv.FormatUint(e.ID(), 10))
	vlog.Warn("domain", "msg", "shared system wrote player entity",
		"system", auditScope.name, "id", e.ID())
}
