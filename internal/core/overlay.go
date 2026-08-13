package core

// OverlayLayout selects how the renderer presents overlay content
type OverlayLayout uint8

const (
	OverlayLayoutCards OverlayLayout = iota // Masonry cards (debug)
	OverlayLayoutDoc                        // Single-column sections (help)
	OverlayLayoutAbout                      // Logo and info panel
)

// OverlayContent holds typed overlay data, extensible via OverlayItem interface
// Immutable once published to GameContext
type OverlayContent struct {
	Title  string
	Items  []OverlayItem
	Layout OverlayLayout
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
