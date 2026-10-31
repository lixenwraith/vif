package core

// OverlayLayout selects how the renderer presents overlay content
type OverlayLayout uint8

const (
	OverlayLayoutCards OverlayLayout = iota // Masonry cards (telemetry)
	OverlayLayoutDoc                        // Single-column sections (help)
	OverlayLayoutAbout                      // Logo and info panel
	OverlayLayoutMenu                       // Live configuration
)

// OverlayHelpTitle heads the help overlay, which the help key closes by it
const OverlayHelpTitle = "HELP"

// OverlayContent holds typed overlay data, extensible via OverlayItem interface
// Immutable once published to GameContext
type OverlayContent struct {
	Title  string
	Items  []OverlayItem
	Layout OverlayLayout
	Menu   *OverlayMenu
}

// OverlayMenu is a value snapshot; only the input router applies settings.
type OverlayMenu struct {
	Page      string   // The group shown as a page, by its id; empty lists the pages
	Choice    string   // The row a page's pick list marks
	Scenarios []string // The scenarios installed, scanned once when the menu opened
	Running   string   // The scenario running then
	Rows      []OverlayMenuRow
	Form      *OverlayForm
}

type OverlayForm struct {
	Fields      []OverlayFormField
	Focus       int
	Help, Error string
}

type OverlayFormField struct {
	Label, Value string
	Cursor       int
}

type OverlayMenuRow struct {
	Key, Label, Value, Description string
	Disabled                       string // Why this setting cannot be changed
	Header                         bool   // A section title, which is never selected
	Text                           bool   // Value is a text box's content
	Typing                         bool   // The text box takes keys, its cursor at Cursor
	Cursor                         int
}

// OverlayItem is implemented by all overlay component types
type OverlayItem interface {
	overlayItem() // sealed marker
}

// OverlayCard displays a titled box with key-value entries
type OverlayCard struct {
	Key     string // Stable identity for selection and pinning; Title is display-only
	Title   string
	Entries []CardEntry
	Pinned  bool
}

func (OverlayCard) overlayItem() {}

// CardEntry is a single key-value pair within a card
type CardEntry struct {
	Key   string
	Value string
}

// Cards extracts all OverlayCard items from content
func (c *OverlayContent) Cards() []OverlayCard {
	if c == nil {
		return nil
	}
	var cards []OverlayCard
	for _, item := range c.Items {
		if card, ok := item.(OverlayCard); ok {
			cards = append(cards, card)
		}
	}
	return cards
}
