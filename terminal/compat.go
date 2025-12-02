package terminal

// TcellAttrMask mirrors tcell.AttrMask for migration compatibility
// Remove after full migration
type TcellAttrMask int

const (
	TcellAttrBold TcellAttrMask = 1 << iota
	TcellAttrBlink
	TcellAttrReverse
	TcellAttrUnderline
	TcellAttrDim
	TcellAttrItalic
	TcellAttrStrikeThrough
	TcellAttrInvalid
	TcellAttrNone TcellAttrMask = 0
)

// TcellAttrCompat converts terminal.Attr to tcell-compatible AttrMask
func TcellAttrCompat(a Attr) TcellAttrMask {
	var mask TcellAttrMask
	if a&AttrBold != 0 {
		mask |= TcellAttrBold
	}
	if a&AttrDim != 0 {
		mask |= TcellAttrDim
	}
	if a&AttrItalic != 0 {
		mask |= TcellAttrItalic
	}
	if a&AttrUnderline != 0 {
		mask |= TcellAttrUnderline
	}
	if a&AttrBlink != 0 {
		mask |= TcellAttrBlink
	}
	if a&AttrReverse != 0 {
		mask |= TcellAttrReverse
	}
	return mask
}

// AttrFromTcell converts tcell-compatible AttrMask to terminal.Attr
func AttrFromTcell(mask TcellAttrMask) Attr {
	var a Attr
	if mask&TcellAttrBold != 0 {
		a |= AttrBold
	}
	if mask&TcellAttrDim != 0 {
		a |= AttrDim
	}
	if mask&TcellAttrItalic != 0 {
		a |= AttrItalic
	}
	if mask&TcellAttrUnderline != 0 {
		a |= AttrUnderline
	}
	if mask&TcellAttrBlink != 0 {
		a |= AttrBlink
	}
	if mask&TcellAttrReverse != 0 {
		a |= AttrReverse
	}
	return a
}
