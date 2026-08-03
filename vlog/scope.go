//go:build !wasm && !novlog

package vlog

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Scope is a record category, orthogonal to level. Resolved from the record's
// sub tag, so call sites stay unchanged. Unmapped subs fall into ScopeTap,
// keeping ad-hoc debugging taps visible by default.
type Scope uint32

const (
	ScopeApp Scope = 1 << iota
	ScopeFSM
	ScopeEvent
	ScopeInput
	ScopeStat
	ScopeLock
	ScopeTap
)

const (
	ScopeNone Scope = 0
	ScopeAll        = ScopeApp | ScopeFSM | ScopeEvent | ScopeInput | ScopeStat | ScopeLock | ScopeTap
)

// scopeDef is the canonical name table; short letters are unique
var scopeDef = []struct {
	scope Scope
	long  string
	short byte
}{
	{ScopeApp, "app", 'a'},
	{ScopeFSM, "fsm", 'f'},
	{ScopeEvent, "event", 'e'},
	{ScopeInput, "input", 'i'},
	{ScopeStat, "stat", 's'},
	{ScopeLock, "lock", 'l'},
	{ScopeTap, "tap", 't'},
}

// subScope binds a record's sub tag to its scope
var subScope = map[string]Scope{
	"race":    ScopeApp,
	"crash":   ScopeApp,
	"app":     ScopeApp,
	"service": ScopeApp,
	"fsm":     ScopeFSM,
	"event":   ScopeEvent,
	"push":    ScopeEvent,
	"input":   ScopeInput,
	"stat":    ScopeStat,
	"lock":    ScopeLock,
}

var scopes atomic.Uint32

func init() { scopes.Store(uint32(ScopeAll)) }

// ScopeOf resolves a sub tag; unknown tags are taps
func ScopeOf(sub string) Scope {
	if s, ok := subScope[sub]; ok {
		return s
	}
	return ScopeTap
}

// Scopes returns the active mask
func Scopes() Scope { return Scope(scopes.Load()) }

// SetScopes replaces the active mask
func SetScopes(s Scope) { scopes.Store(uint32(s)) }

// scopeEnabled reports whether records with this sub are admitted
func scopeEnabled(sub string) bool { return Scope(scopes.Load())&ScopeOf(sub) != 0 }

// ScopeString renders a mask as a canonical '+'-joined spec
func ScopeString(s Scope) string {
	switch s {
	case ScopeAll:
		return "all"
	case ScopeNone:
		return "none"
	}
	names := make([]string, 0, len(scopeDef))
	for _, d := range scopeDef {
		if s&d.scope != 0 {
			names = append(names, d.long)
		}
	}
	return strings.Join(names, "+")
}

// ParseScopes resolves a spec against cur. A leading '+' or '-' adjusts cur,
// otherwise the spec replaces it. Tokens split on '+', ',' or space; each is
// a long name, "all"/"none", or a run of short letters ("afs").
func ParseScopes(spec string, cur Scope) (Scope, error) {
	spec = strings.ToLower(strings.TrimSpace(spec))
	if spec == "" {
		return cur, fmt.Errorf("empty scope spec")
	}

	mode := byte('=')
	if spec[0] == '+' || spec[0] == '-' {
		mode, spec = spec[0], spec[1:]
	}

	var set Scope
	fields := strings.FieldsFunc(spec, func(r rune) bool {
		return r == '+' || r == ',' || r == ' '
	})
	if len(fields) == 0 {
		return cur, fmt.Errorf("empty scope spec")
	}
	for _, tok := range fields {
		s, err := parseScopeToken(tok)
		if err != nil {
			return cur, err
		}
		set |= s
	}

	switch mode {
	case '+':
		return cur | set, nil
	case '-':
		return cur &^ set, nil
	}
	return set, nil
}

// parseScopeToken resolves one token to a mask
func parseScopeToken(tok string) (Scope, error) {
	switch tok {
	case "all":
		return ScopeAll, nil
	case "none", "off":
		return ScopeNone, nil
	}
	for _, d := range scopeDef {
		if tok == d.long {
			return d.scope, nil
		}
	}
	var s Scope
	for i := range len(tok) {
		found := false
		for _, d := range scopeDef {
			if tok[i] == d.short {
				s |= d.scope
				found = true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("unknown scope %q", tok)
		}
	}
	return s, nil
}
