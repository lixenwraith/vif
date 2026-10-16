//go:build !vif_headless

package asset

import _ "embed"

// DefaultFont is the bitmap font atlas a graphical host draws the cell grid with,
// in the document pkg/glyph reads; tool/font-atlas regenerates it.
//
//go:embed font/mono.toml
var DefaultFont []byte
