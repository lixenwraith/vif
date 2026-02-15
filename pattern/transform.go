package pattern

import "image"

// Translate shifts all cell offsets by (dx, dy)
func (p PatternResult) Translate(dx, dy int) PatternResult {
	cells := make([]PatternCell, len(p.Cells))
	for i, cell := range p.Cells {
		cells[i] = cell
		cells[i].OffsetX += dx
		cells[i].OffsetY += dy
	}
	return PatternResult{
		Cells:   cells,
		Width:   p.Width,
		Height:  p.Height,
		AnchorX: p.AnchorX + dx,
		AnchorY: p.AnchorY + dy,
	}
}

// Mask removes cells outside the specified bounds
// Bounds are in pattern-local coordinates (relative to offsets)
func (p PatternResult) Mask(bounds image.Rectangle) PatternResult {
	cells := make([]PatternCell, 0, len(p.Cells))
	for _, cell := range p.Cells {
		if cell.OffsetX >= bounds.Min.X && cell.OffsetX < bounds.Max.X &&
			cell.OffsetY >= bounds.Min.Y && cell.OffsetY < bounds.Max.Y {
			cells = append(cells, cell)
		}
	}

	width := bounds.Dx()
	height := bounds.Dy()
	if width > p.Width {
		width = p.Width
	}
	if height > p.Height {
		height = p.Height
	}

	return PatternResult{
		Cells:   cells,
		Width:   width,
		Height:  height,
		AnchorX: p.AnchorX,
		AnchorY: p.AnchorY,
	}
}

// MaskFunc removes cells where keep returns false
func (p PatternResult) MaskFunc(keep func(x, y int) bool) PatternResult {
	cells := make([]PatternCell, 0, len(p.Cells))
	for _, cell := range p.Cells {
		if keep(cell.OffsetX, cell.OffsetY) {
			cells = append(cells, cell)
		}
	}
	return PatternResult{
		Cells:   cells,
		Width:   p.Width,
		Height:  p.Height,
		AnchorX: p.AnchorX,
		AnchorY: p.AnchorY,
	}
}

// Tile repeats the pattern to fill the specified area
// Pattern is tiled from (0,0), cells outside area are clipped
func (p PatternResult) Tile(areaWidth, areaHeight int) PatternResult {
	if p.Width == 0 || p.Height == 0 || len(p.Cells) == 0 {
		return PatternResult{Width: areaWidth, Height: areaHeight}
	}

	tilesX := (areaWidth + p.Width - 1) / p.Width
	tilesY := (areaHeight + p.Height - 1) / p.Height

	cells := make([]PatternCell, 0, len(p.Cells)*tilesX*tilesY)

	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			offsetX := tx * p.Width
			offsetY := ty * p.Height

			for _, cell := range p.Cells {
				newX := cell.OffsetX + offsetX
				newY := cell.OffsetY + offsetY

				if newX >= areaWidth || newY >= areaHeight {
					continue
				}

				newCell := cell
				newCell.OffsetX = newX
				newCell.OffsetY = newY
				cells = append(cells, newCell)
			}
		}
	}

	return PatternResult{
		Cells:  cells,
		Width:  areaWidth,
		Height: areaHeight,
	}
}

// Merge combines multiple patterns into one
// Later patterns overwrite earlier ones at same position
func Merge(patterns ...PatternResult) PatternResult {
	if len(patterns) == 0 {
		return PatternResult{}
	}

	// Calculate total bounds
	var totalCells int
	var maxW, maxH int
	for _, p := range patterns {
		totalCells += len(p.Cells)
		if p.Width > maxW {
			maxW = p.Width
		}
		if p.Height > maxH {
			maxH = p.Height
		}
	}

	// Build position map for deduplication (last write wins)
	type posKey struct{ x, y int }
	posMap := make(map[posKey]PatternCell, totalCells)

	for _, p := range patterns {
		for _, cell := range p.Cells {
			posMap[posKey{cell.OffsetX, cell.OffsetY}] = cell
		}
	}

	cells := make([]PatternCell, 0, len(posMap))
	for _, cell := range posMap {
		cells = append(cells, cell)
	}

	return PatternResult{
		Cells:  cells,
		Width:  maxW,
		Height: maxH,
	}
}