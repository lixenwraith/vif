// Package help projects the live key bindings into displayable documentation.
// Leaf package: it imports only internal/input, so any package may consume it.
package help

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/input"
)

// Topic is a resolved help section
type Topic struct {
	Key     string // Stable identity
	Title   string
	Entries []Entry
}

// Entry is one resolved line: the keys that invoke it and what it does
type Entry struct {
	Keys string
	Desc string
}

// entryDef documents one binding. Actions resolve their keys from the live key
// table; an empty Actions list means Keys is a literal the table cannot supply.
type entryDef struct {
	Actions      []string
	Scope        input.KeySection // SectionNormal by default
	PrefixAction string           // Primary key of this action is prepended, e.g. prefix_g
	Suffix       string           // Appended to every resolved key, e.g. "{c}"
	Keys         string           // Literal display
	Desc         string
	All          bool // Show every binding of each action, not just the primary
}

// topicDef is a section template
type topicDef struct {
	Key     string
	Title   string
	Entries []entryDef
}

var active atomic.Pointer[input.KeyTable]

// defaultTable backs KeyTable until a keymap is installed
var defaultTable = sync.OnceValue(input.DefaultKeyTable)

// SetKeyTable installs the active bindings; call it beside Machine.SetKeyTable
// so a user keymap documents itself
func SetKeyTable(kt *input.KeyTable) { active.Store(kt) }

// KeyTable returns the active bindings, falling back to the defaults
func KeyTable() *input.KeyTable {
	if kt := active.Load(); kt != nil {
		return kt
	}
	return defaultTable()
}

// Topics resolves every section against a key table. Entries whose actions are
// all unbound in this keymap are dropped, as is a section left empty.
func Topics(kt *input.KeyTable) []Topic {
	out := make([]Topic, 0, len(topics))
	for i := range topics {
		t := &topics[i]
		entries := make([]Entry, 0, len(t.Entries))
		for j := range t.Entries {
			if keys, ok := t.Entries[j].resolve(kt); ok {
				entries = append(entries, Entry{Keys: keys, Desc: t.Entries[j].Desc})
			}
		}
		if len(entries) > 0 {
			out = append(out, Topic{Key: t.Key, Title: t.Title, Entries: entries})
		}
	}
	return out
}

// resolve renders an entry's key column, reporting false when nothing is bound
func (e *entryDef) resolve(kt *input.KeyTable) (string, bool) {
	if len(e.Actions) == 0 {
		return e.Keys, e.Keys != ""
	}

	prefix := primaryKey(kt, input.SectionNormal, e.PrefixAction)
	parts := make([]string, 0, len(e.Actions))
	for _, action := range e.Actions {
		bound := kt.Bindings(e.Scope, action)
		if len(bound) == 0 {
			continue
		}
		if !e.All {
			bound = bound[:1]
		}
		for _, k := range bound {
			parts = append(parts, prefix+k+e.Suffix)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "/"), true
}

// primaryKey returns an action's first binding, empty when unbound or unnamed
func primaryKey(kt *input.KeyTable, section input.KeySection, action string) string {
	if action == "" {
		return ""
	}
	if b := kt.Bindings(section, action); len(b) > 0 {
		return b[0]
	}
	return ""
}
