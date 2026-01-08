package core

// OverlayContent holds typed overlay data, extensible via OverlayItem interface
type OverlayContent struct {
	Title string
	Items []OverlayItem
}

// OverlayItem is implemented by all overlay component types
type OverlayItem interface {
	overlayItem() // sealed marker
}

// OverlayCard displays a titled box with key-value entries
type OverlayCard struct {
	Title   string
	Entries []CardEntry
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