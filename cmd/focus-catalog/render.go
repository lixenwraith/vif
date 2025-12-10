package main

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// Render draws the entire UI
func (app *AppState) Render() {
	w, h := app.Width, app.Height
	if w < minWidth {
		w = minWidth
	}
	if h < minHeight {
		h = minHeight
	}

	cells := make([]terminal.Cell, w*h)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: colorDefaultBg}
	}

	if app.PreviewMode {
		app.renderPreview(cells, w, h)
	} else {
		app.renderMain(cells, w, h)
	}

	app.Term.Flush(cells, w, h)
}

func (app *AppState) renderMain(cells []terminal.Cell, w, h int) {
	// Header
	drawRect(cells, 0, 0, w, headerHeight, w, colorHeaderBg)

	title := "FOCUS-CATALOG"
	drawText(cells, w, 1, 0, title, colorHeaderFg, colorHeaderBg, terminal.AttrBold)

	// Right side: group and deps
	groupStr := "Group: (all)"
	if app.ActiveGroup != "" {
		groupStr = "Group: #" + app.ActiveGroup
	}
	depsStr := fmt.Sprintf("Deps: %d", app.DepthLimit)
	if !app.ExpandDeps {
		depsStr = "Deps: OFF"
	}
	rightInfo := groupStr + "  " + depsStr
	drawText(cells, w, w-len(rightInfo)-2, 0, rightInfo, colorHeaderFg, colorHeaderBg, terminal.AttrNone)

	// Column header
	colHeader := "PACKAGE          FILES  TAGS"
	drawText(cells, w, 1, 1, colHeader, colorStatusFg, colorHeaderBg, terminal.AttrNone)

	// Package list
	listStart := headerHeight
	listEnd := h - statusHeight - helpHeight
	visibleRows := listEnd - listStart
	if visibleRows < 1 {
		visibleRows = 1
	}

	expandedPkgs := make(map[string]bool)
	if app.ExpandDeps && len(app.Selected) > 0 {
		expandedPkgs = ExpandDeps(app.Selected, app.Index, app.DepthLimit)
		for k := range app.Selected {
			delete(expandedPkgs, k)
		}
	}

	for i := 0; i < visibleRows && app.ScrollOffset+i < len(app.PackageList); i++ {
		y := listStart + i
		idx := app.ScrollOffset + i
		pkgName := app.PackageList[idx]
		pkg := app.Index.Packages[pkgName]

		isCursor := idx == app.CursorPos
		isSelected := app.Selected[pkgName]
		isExpanded := expandedPkgs[pkgName]

		bg := colorDefaultBg
		if isCursor {
			bg = colorCursorBg
		}

		// Clear line
		for x := 0; x < w; x++ {
			cells[y*w+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: bg}
		}

		// Cursor indicator
		cursor := " "
		if isCursor {
			cursor = ">"
		}
		drawText(cells, w, 0, y, cursor, colorHeaderFg, bg, terminal.AttrBold)

		// Checkbox
		checkbox := "[ ]"
		checkFg := colorUnselected
		if isSelected {
			checkbox = "[x]"
			checkFg = colorSelected
		} else if isExpanded {
			checkbox = "[+]"
			checkFg = colorExpandedFg
		}
		drawText(cells, w, 1, y, checkbox, checkFg, bg, terminal.AttrNone)

		// Package name (max 16 chars)
		nameDisplay := pkgName
		if len(nameDisplay) > 16 {
			nameDisplay = nameDisplay[:15] + "…"
		}
		nameFg := colorDefaultFg
		if pkg.HasAll {
			nameFg = colorAllTagFg
		}
		drawText(cells, w, 5, y, fmt.Sprintf("%-16s", nameDisplay), nameFg, bg, terminal.AttrNone)

		// File count
		fileCount := fmt.Sprintf("%3d", len(pkg.Files))
		drawText(cells, w, 22, y, fileCount, colorMatchCountFg, bg, terminal.AttrNone)

		// Tags
		tagsStr := formatTags(pkg.AllTags)
		if len(tagsStr) > w-28 {
			tagsStr = tagsStr[:w-31] + "..."
		}
		drawTagsColored(cells, w, 27, y, tagsStr, bg)
	}

	// Status area
	statusY := h - statusHeight - helpHeight

	// Expanded packages
	if app.ExpandDeps && len(expandedPkgs) > 0 {
		names := slices.Collect(maps.Keys(expandedPkgs))
		sort.Strings(names)
		expStr := "Expanded: " + strings.Join(names, ", ")
		if len(expStr) > w-2 {
			expStr = expStr[:w-5] + "..."
		}
		drawText(cells, w, 1, statusY, expStr, colorExpandedFg, colorDefaultBg, terminal.AttrNone)
	}

	// Keyword filter
	if app.KeywordFilter != "" {
		kwStr := fmt.Sprintf("Keyword: %q (%d files)", app.KeywordFilter, len(app.KeywordMatches))
		drawText(cells, w, 1, statusY+1, kwStr, colorTagFg, colorDefaultBg, terminal.AttrNone)
	} else if app.InputMode {
		inputStr := "Search: " + app.InputBuffer + "_"
		drawText(cells, w, 1, statusY+1, inputStr, colorHeaderFg, colorInputBg, terminal.AttrNone)
	}

	// Output count
	outputFiles := app.ComputeOutputFiles()
	outStr := fmt.Sprintf("Output: %d files", len(outputFiles))
	if app.Message != "" {
		outStr = app.Message
	}
	drawText(cells, w, 1, statusY+2, outStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)

	// Help bar
	helpY := h - helpHeight
	drawRect(cells, 0, helpY, w, helpHeight, w, colorDefaultBg)

	help1 := "j/k nav  Space sel  / search  g group  d deps  ±depth"
	help2 := "a all  c clear  p preview  Enter output  q quit"
	drawText(cells, w, 1, helpY, help1, colorHelpFg, colorDefaultBg, terminal.AttrDim)
	drawText(cells, w, 1, helpY+1, help2, colorHelpFg, colorDefaultBg, terminal.AttrDim)
}

func (app *AppState) renderPreview(cells []terminal.Cell, w, h int) {
	// Header
	drawRect(cells, 0, 0, w, 1, w, colorHeaderBg)
	title := fmt.Sprintf("PREVIEW (%d files) - press p/q/Esc to close", len(app.PreviewFiles))
	drawText(cells, w, 1, 0, title, colorHeaderFg, colorHeaderBg, terminal.AttrBold)

	// File list
	for i := 1; i < h-1; i++ {
		idx := app.PreviewScroll + i - 1
		if idx >= len(app.PreviewFiles) {
			break
		}
		drawText(cells, w, 1, i, "./"+app.PreviewFiles[idx], colorDefaultFg, colorDefaultBg, terminal.AttrNone)
	}

	// Scroll indicator
	if len(app.PreviewFiles) > h-2 {
		pct := 0
		if len(app.PreviewFiles) > 0 {
			pct = (app.PreviewScroll * 100) / len(app.PreviewFiles)
		}
		scrollStr := fmt.Sprintf("[%d%%]", pct)
		drawText(cells, w, w-len(scrollStr)-1, h-1, scrollStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)
	}
}

// formatTags formats tags map into display string
func formatTags(tags map[string][]string) string {
	if len(tags) == 0 {
		return ""
	}

	groups := slices.Collect(maps.Keys(tags))
	sort.Strings(groups)

	var parts []string
	for _, g := range groups {
		t := tags[g]
		if len(t) == 0 {
			parts = append(parts, "#"+g)
		} else {
			parts = append(parts, fmt.Sprintf("#%s{%s}", g, strings.Join(t, ",")))
		}
	}

	return strings.Join(parts, " ")
}

// drawText draws text at position, returns chars written
func drawText(cells []terminal.Cell, width, x, y int, text string, fg, bg terminal.RGB, attr terminal.Attr) int {
	for i, r := range text {
		if x+i >= width {
			break
		}
		cells[y*width+x+i] = terminal.Cell{
			Rune:  r,
			Fg:    fg,
			Bg:    bg,
			Attrs: attr,
		}
	}
	return len(text)
}

// drawRect fills a rectangle with background color
func drawRect(cells []terminal.Cell, startX, startY, rectW, rectH, totalWidth int, bg terminal.RGB) {
	for row := startY; row < startY+rectH; row++ {
		for col := startX; col < startX+rectW && col < totalWidth; col++ {
			idx := row*totalWidth + col
			if idx >= 0 && idx < len(cells) {
				cells[idx] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: bg}
			}
		}
	}
}

// drawTagsColored draws tags with group names in yellow and tags in cyan
func drawTagsColored(cells []terminal.Cell, width, x, y int, text string, bg terminal.RGB) {
	pos := x
	inGroup := false

	for _, r := range text {
		if pos >= width {
			break
		}

		fg := colorTagFg

		if r == '#' {
			inGroup = true
			fg = colorGroupFg
		} else if r == '{' || r == '}' {
			inGroup = false
			fg = colorStatusFg // dim braces
		} else if inGroup && r != ' ' {
			fg = colorGroupFg
		} else if r == ' ' {
			inGroup = false
		}
		// Inside braces (tags) defaults to colorTagFg, no tracking needed

		cells[y*width+pos] = terminal.Cell{
			Rune:  r,
			Fg:    fg,
			Bg:    bg,
			Attrs: terminal.AttrNone,
		}
		pos++
	}
}
