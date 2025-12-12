package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// DiveState holds computed relationship data for dive view visualization
type DiveState struct {
	SourcePath string
	FileInfo   *FileInfo

	DependsOn  []DivePackage
	DependedBy []DivePackage
	TagLinks   []DiveTagGroup
}

// DivePackage represents a package directory with its constituent files
type DivePackage struct {
	Dir   string
	Files []string
}

// DiveTagGroup represents files sharing a specific tag within a group
type DiveTagGroup struct {
	Group string
	Tag   string
	Count int
	Files []string
}

// Box drawing characters - single line
const (
	boxTL = '┌'
	boxTR = '┐'
	boxBL = '└'
	boxBR = '┘'
	boxH  = '─'
	boxV  = '│'
	boxTT = '┬'
	boxBT = '┴'
)

// Box drawing characters - double line
const (
	dboxTL = '╔'
	dboxTR = '╗'
	dboxBL = '╚'
	dboxBR = '╝'
	dboxH  = '═'
	dboxV  = '║'
	dboxLT = '╠'
	dboxRT = '╣'
)

// Connector characters
const (
	arrowDown = '▼'
	arrowUp   = '▲'
	connV     = '│'
	connSplit = '┼'
	starChar  = '★'
)

// EnterDive transitions to dive view for the file at current mindmap cursor
func (app *AppState) EnterDive() {
	if app.MindmapState == nil || len(app.MindmapState.Items) == 0 {
		return
	}

	item := app.MindmapState.Items[app.MindmapState.Cursor]
	if item.IsDir || item.Path == "" {
		app.Message = "select a file to dive"
		return
	}

	state := computeDiveData(app, item.Path)
	if state == nil {
		app.Message = "no data for file"
		return
	}

	app.DiveState = state
	app.DiveMode = true
}

// ExitDive returns from dive view to mindmap view
func (app *AppState) ExitDive() {
	app.DiveMode = false
	app.DiveState = nil
}

// HandleDiveEvent processes keyboard input while in dive view
func (app *AppState) HandleDiveEvent(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		app.ExitDive()
	case terminal.KeyRune:
		if ev.Rune == 'q' {
			app.ExitDive()
		}
	}
}

// computeDiveData gathers dependency and tag relationship data for a file
func computeDiveData(app *AppState, path string) *DiveState {
	fi := app.Index.Files[path]
	if fi == nil {
		return nil
	}

	state := &DiveState{
		SourcePath: path,
		FileInfo:   fi,
	}

	// Get package directory for this file
	fileDir := filepath.Dir(path)
	fileDir = filepath.ToSlash(fileDir)
	if fileDir == "." {
		fileDir = fi.Package
	}

	// Compute DependsOn - packages this file imports
	state.DependsOn = computeDependsOn(app, fi)

	// Compute DependedBy - packages that import this file's package
	state.DependedBy = computeDependedBy(app, fileDir)

	// Compute TagLinks - files sharing tags
	state.TagLinks = computeTagLinks(app, fi, path)

	return state
}

// computeDependsOn resolves packages imported by the given file
func computeDependsOn(app *AppState, fi *FileInfo) []DivePackage {
	var deps []DivePackage
	seen := make(map[string]bool)

	for _, impName := range fi.Imports {
		// Find package by name
		for dir, pkg := range app.Index.Packages {
			if pkg.Name == impName && !seen[dir] {
				seen[dir] = true
				dp := DivePackage{Dir: dir}
				for _, f := range pkg.Files {
					dp.Files = append(dp.Files, filepath.Base(f.Path))
				}
				sort.Strings(dp.Files)
				deps = append(deps, dp)
				break
			}
		}
	}

	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Dir < deps[j].Dir
	})
	return deps
}

// computeDependedBy finds packages that import the file's package
func computeDependedBy(app *AppState, fileDir string) []DivePackage {
	var deps []DivePackage

	importers := app.Index.ReverseDeps[fileDir]
	for _, dir := range importers {
		pkg := app.Index.Packages[dir]
		if pkg == nil {
			continue
		}
		dp := DivePackage{Dir: dir}
		for _, f := range pkg.Files {
			dp.Files = append(dp.Files, filepath.Base(f.Path))
		}
		sort.Strings(dp.Files)
		deps = append(deps, dp)
	}

	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Dir < deps[j].Dir
	})
	return deps
}

// computeTagLinks finds files sharing tags with the source file
func computeTagLinks(app *AppState, fi *FileInfo, selfPath string) []DiveTagGroup {
	var links []DiveTagGroup

	// Get sorted groups
	groups := make([]string, 0, len(fi.Tags))
	for g := range fi.Tags {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	for _, group := range groups {
		tags := fi.Tags[group]
		sortedTags := make([]string, len(tags))
		copy(sortedTags, tags)
		sort.Strings(sortedTags)

		for _, tag := range sortedTags {
			tg := DiveTagGroup{
				Group: group,
				Tag:   tag,
			}

			// Find all files with this tag
			var files []string
			for path, fileInfo := range app.Index.Files {
				if path == selfPath {
					continue
				}
				if fileTags, ok := fileInfo.Tags[group]; ok {
					for _, t := range fileTags {
						if t == tag {
							files = append(files, path)
							break
						}
					}
				}
			}
			sort.Strings(files)

			tg.Count = len(files)
			tg.Files = files
			links = append(links, tg)
		}
	}

	return links
}

// RenderDive draws the dive view layout with dependency and tag boxes
func (app *AppState) RenderDive(cells []terminal.Cell, w, h int) {
	state := app.DiveState
	if state == nil {
		return
	}

	// Clear with background
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: colorDefaultBg}
	}

	// Draw outer double-line frame
	drawDoubleFrame(cells, w, 0, 0, w, h)

	// Header line
	title := fmt.Sprintf(" DIVE: %s ", truncateWithEllipsis(state.SourcePath, w-30))
	drawTextAt(cells, w, 2, 0, title, colorHeaderFg, colorDefaultBg, terminal.AttrBold)
	hint := "[Esc:back]"
	drawTextAt(cells, w, w-len(hint)-2, 0, hint, colorHelpFg, colorDefaultBg, terminal.AttrNone)

	// Draw separator after header
	cells[1*w] = terminal.Cell{Rune: dboxLT, Fg: colorPaneBorder, Bg: colorDefaultBg}
	for x := 1; x < w-1; x++ {
		cells[1*w+x] = terminal.Cell{Rune: dboxH, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}
	cells[1*w+w-1] = terminal.Cell{Rune: dboxRT, Fg: colorPaneBorder, Bg: colorDefaultBg}

	// Calculate layout
	contentTop := 2
	contentBottom := h - 1
	contentH := contentBottom - contentTop

	// Allocate sections based on available height
	focusBoxH := 4
	depsH, tagsH := allocateSections(contentH, focusBoxH, len(state.DependsOn)+len(state.DependedBy), len(state.TagLinks))

	y := contentTop

	// Render dependencies section
	if depsH > 0 && (len(state.DependsOn) > 0 || len(state.DependedBy) > 0) {
		y = renderDepsSection(cells, w, y, depsH, state.DependsOn, state.DependedBy)
	}

	// Connector to focus box
	if y < contentBottom-focusBoxH-tagsH {
		midX := w / 2
		cells[y*w+midX] = terminal.Cell{Rune: connV, Fg: colorStatusFg, Bg: colorDefaultBg}
		y++
		cells[y*w+midX] = terminal.Cell{Rune: arrowDown, Fg: colorStatusFg, Bg: colorDefaultBg}
		y++
	}

	// Render focus box
	y = renderFocusBox(cells, w, y, state)

	// Connector to tags
	if tagsH > 0 && len(state.TagLinks) > 0 {
		midX := w / 2
		cells[y*w+midX] = terminal.Cell{Rune: connV, Fg: colorStatusFg, Bg: colorDefaultBg}
		y++

		// Draw split connector
		tagBoxCount := min(len(state.TagLinks), calcMaxTagBoxes(w))
		if tagBoxCount > 1 {
			y = renderTagConnector(cells, w, y, tagBoxCount)
		} else {
			cells[y*w+midX] = terminal.Cell{Rune: arrowDown, Fg: colorStatusFg, Bg: colorDefaultBg}
			y++
		}
	}

	// Render tag section
	if tagsH > 0 && len(state.TagLinks) > 0 {
		renderTagSection(cells, w, y, tagsH, state.TagLinks)
	}
}

// allocateSections distributes vertical space between dependency and tag areas
func allocateSections(totalH, focusH, depCount, tagCount int) (depsH, tagsH int) {
	available := totalH - focusH - 4 // connectors

	if depCount == 0 && tagCount == 0 {
		return 0, 0
	}

	if depCount == 0 {
		return 0, available
	}
	if tagCount == 0 {
		return available, 0
	}

	// Split proportionally, minimum 4 lines each
	depsH = max(4, available*40/100)
	tagsH = available - depsH
	if tagsH < 4 {
		tagsH = 4
		depsH = available - tagsH
	}

	return depsH, tagsH
}

// renderDepsSection draws the depends-on and depended-by columns
func renderDepsSection(cells []terminal.Cell, w, y, maxH int, dependsOn, dependedBy []DivePackage) int {
	innerW := w - 4
	startY := y

	// Calculate column widths
	hasDepOn := len(dependsOn) > 0
	hasDepBy := len(dependedBy) > 0

	var depOnW, depByW int
	if hasDepOn && hasDepBy {
		depOnW = innerW * 65 / 100
		depByW = innerW - depOnW - 1 // -1 for separator
	} else if hasDepOn {
		depOnW = innerW
	} else {
		depByW = innerW
	}

	// Draw box
	boxHeight := min(maxH, calcDepsBoxHeight(dependsOn, dependedBy, maxH))
	drawSingleBox(cells, w, 2, y, innerW, boxHeight)

	// Headers
	if hasDepOn {
		hdr := fmt.Sprintf(" DEPENDS ON (%d) ", len(dependsOn))
		drawTextAt(cells, w, 4, y, hdr, colorGroupFg, colorDefaultBg, terminal.AttrBold)
	}
	if hasDepBy {
		hdrX := 2 + depOnW + 1
		if hasDepOn {
			// Draw vertical separator
			for sy := y; sy < y+boxHeight; sy++ {
				cells[sy*w+hdrX] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
			}
			cells[y*w+hdrX] = terminal.Cell{Rune: boxTT, Fg: colorPaneBorder, Bg: colorDefaultBg}
			cells[(y+boxHeight-1)*w+hdrX] = terminal.Cell{Rune: boxBT, Fg: colorPaneBorder, Bg: colorDefaultBg}
			hdrX++
		}
		hdr := fmt.Sprintf(" DEPENDED BY (%d) ", len(dependedBy))
		drawTextAt(cells, w, hdrX+1, y, hdr, colorExpandedFg, colorDefaultBg, terminal.AttrBold)
	}

	// Content
	contentY := y + 1
	contentH := boxHeight - 2

	if hasDepOn {
		renderPackageList(cells, w, 3, contentY, depOnW-2, contentH, dependsOn, colorDirFg)
	}
	if hasDepBy {
		startX := 3 + depOnW
		if hasDepOn {
			startX++
		}
		renderPackageList(cells, w, startX, contentY, depByW-2, contentH, dependedBy, colorExpandedFg)
	}

	return startY + boxHeight
}

// calcDepsBoxHeight computes required height for dependency box content
func calcDepsBoxHeight(dependsOn, dependedBy []DivePackage, maxH int) int {
	// Calculate needed height based on content
	maxFiles := 0
	for _, dp := range dependsOn {
		if len(dp.Files)+1 > maxFiles {
			maxFiles = len(dp.Files) + 1
		}
	}
	for _, dp := range dependedBy {
		if len(dp.Files)+1 > maxFiles {
			maxFiles = len(dp.Files) + 1
		}
	}
	return min(maxH, maxFiles+2) // +2 for borders
}

// renderPackageList draws a multi-column list of packages with their files
func renderPackageList(cells []terminal.Cell, totalW, x, y, availW, availH int, packages []DivePackage, fg terminal.RGB) {
	if len(packages) == 0 || availW < 10 || availH < 1 {
		return
	}

	// Calculate column layout
	colW := min(availW, 20)
	numCols := max(1, availW/(colW+1))
	colW = (availW - numCols + 1) / numCols

	col := 0
	rowOffset := 0 // Track row within current "row of columns"
	pkgShown := 0
	totalPkgs := len(packages)
	maxRowInGroup := 0 // Track tallest column in current row of columns

	for _, pkg := range packages {
		if rowOffset >= availH {
			break
		}

		colX := x + col*(colW+1)
		localRow := 0

		// Package directory
		dirStr := truncateWithEllipsis(pkg.Dir+"/", colW)
		drawTextAt(cells, totalW, colX, y+rowOffset+localRow, dirStr, fg, colorDefaultBg, terminal.AttrBold)
		localRow++
		pkgShown++

		// Files under package
		maxFilesPerPkg := min(4, availH-rowOffset-localRow)
		if maxFilesPerPkg < 1 {
			maxFilesPerPkg = 1
		}
		totalFiles := len(pkg.Files)
		hasMore := totalFiles > maxFilesPerPkg

		// Reserve last slot for "(+N)" if needed
		filesToShow := maxFilesPerPkg
		if hasMore && maxFilesPerPkg > 1 {
			filesToShow = maxFilesPerPkg - 1
		}

		filesShown := 0
		for i := 0; i < filesToShow && i < totalFiles; i++ {
			fileStr := " " + truncateWithEllipsis(pkg.Files[i], colW-2)
			drawTextAt(cells, totalW, colX, y+rowOffset+localRow, fileStr, colorDefaultFg, colorDefaultBg, terminal.AttrNone)
			localRow++
			filesShown++
		}

		// Show remaining count on its own line
		if hasMore {
			remaining := totalFiles - filesShown
			moreStr := fmt.Sprintf(" (+%d)", remaining)
			drawTextAt(cells, totalW, colX, y+rowOffset+localRow, moreStr, colorStatusFg, colorDefaultBg, terminal.AttrDim)
			localRow++
		}

		// Track max height in this row of columns
		if localRow > maxRowInGroup {
			maxRowInGroup = localRow
		}

		// Move to next column or wrap
		col++
		if col >= numCols {
			col = 0
			rowOffset += maxRowInGroup + 1 // Move past tallest column + gap
			maxRowInGroup = 0
		}

		if col == 0 && rowOffset >= availH && pkgShown < totalPkgs {
			break
		}
	}

	if pkgShown < totalPkgs {
		remaining := totalPkgs - pkgShown
		moreStr := fmt.Sprintf("(+%d more)", remaining)
		finalRow := rowOffset
		if col > 0 {
			finalRow += maxRowInGroup
		}
		drawTextAt(cells, totalW, x, y+min(finalRow, availH-1), moreStr, colorStatusFg, colorDefaultBg, terminal.AttrDim)
	}
}

// renderFocusBox draws the central box showing the focused file details
func renderFocusBox(cells []terminal.Cell, w, y int, state *DiveState) int {
	boxWidth := w - 8
	boxX := 4
	boxHeight := 4

	// Draw double-line box
	drawDoubleBox(cells, w, boxX, y, boxWidth, boxHeight)

	// Title
	title := fmt.Sprintf(" %s ", state.SourcePath)
	titleX := boxX + (boxWidth-len(title))/2
	drawTextAt(cells, w, titleX, y, title, colorAllTagFg, colorDefaultBg, terminal.AttrBold)

	// Package info
	pkgStr := fmt.Sprintf("Package: %s", state.FileInfo.Package)
	drawTextAt(cells, w, boxX+2, y+1, pkgStr, colorDirFg, colorDefaultBg, terminal.AttrNone)

	// Tags
	tagStr := formatFileTagsCompact(state.FileInfo)
	maxTagLen := boxWidth - len(pkgStr) - 6
	if len(tagStr) > maxTagLen {
		tagStr = truncateWithEllipsis(tagStr, maxTagLen)
	}
	drawTextAt(cells, w, boxX+len(pkgStr)+4, y+1, tagStr, colorTagFg, colorDefaultBg, terminal.AttrNone)

	// Imports summary
	impStr := fmt.Sprintf("Imports: %d local", len(state.FileInfo.Imports))
	drawTextAt(cells, w, boxX+2, y+2, impStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)

	// Importers count
	fileDir := filepath.Dir(state.SourcePath)
	if fileDir == "." {
		fileDir = state.FileInfo.Package
	}
	impByCount := len(state.DependedBy)
	impByStr := fmt.Sprintf("Imported by: %d packages", impByCount)
	drawTextAt(cells, w, boxX+len(impStr)+4, y+2, impByStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)

	return y + boxHeight
}

// renderTagConnector draws branching connector lines to tag boxes
func renderTagConnector(cells []terminal.Cell, w, y, boxCount int) int {
	midX := w / 2

	// Calculate box positions
	boxW := (w - 8) / boxCount
	positions := make([]int, boxCount)
	startX := 4
	for i := 0; i < boxCount; i++ {
		positions[i] = startX + i*boxW + boxW/2
	}

	// Draw horizontal line with splits
	leftmost := positions[0]
	rightmost := positions[boxCount-1]

	// Draw the horizontal line first
	for x := leftmost; x <= rightmost; x++ {
		cells[y*w+x] = terminal.Cell{Rune: boxH, Fg: colorStatusFg, Bg: colorDefaultBg}
	}

	// Draw branch points
	for i, p := range positions {
		var r rune
		if p == midX {
			r = connSplit // Center cross '┼'
		} else if i == 0 {
			r = boxTL // Left end '┌'
		} else if i == boxCount-1 {
			r = boxTR // Right end '┐'
		} else {
			r = boxTT // Middle branch '┬'
		}
		cells[y*w+p] = terminal.Cell{Rune: r, Fg: colorStatusFg, Bg: colorDefaultBg}
	}
	y++

	// Draw down arrows
	for _, p := range positions {
		cells[y*w+p] = terminal.Cell{Rune: arrowDown, Fg: colorStatusFg, Bg: colorDefaultBg}
	}
	y++

	return y
}

// renderTagSection draws tag group boxes with shared file lists
func renderTagSection(cells []terminal.Cell, w, y, maxH int, tagLinks []DiveTagGroup) {
	if len(tagLinks) == 0 {
		return
	}

	// Reserve 1 line for outer frame
	boxHeight := maxH - 1
	if boxHeight < 3 {
		boxHeight = 3
	}

	innerW := w - 4
	numBoxes := min(len(tagLinks), calcMaxTagBoxes(w))
	boxWidth := (innerW - numBoxes + 1) / numBoxes

	for i := 0; i < numBoxes; i++ {
		tg := tagLinks[i]
		boxX := 2 + i*(boxWidth+1)

		drawSingleBox(cells, w, boxX, y, boxWidth, boxHeight)

		// Header
		hdr := fmt.Sprintf(" #%s{%s} (%d) ", tg.Group, tg.Tag, tg.Count)
		if len(hdr) > boxWidth-2 {
			hdr = fmt.Sprintf(" #%s{%s} ", tg.Group, truncateWithEllipsis(tg.Tag, 8))
		}
		drawTextAt(cells, w, boxX+1, y, hdr, colorGroupFg, colorDefaultBg, terminal.AttrBold)

		// Files
		contentY := y + 1
		contentH := boxHeight - 2
		if contentH < 1 {
			contentH = 1
		}

		totalFiles := len(tg.Files)
		hasMore := totalFiles > contentH

		// Reserve last line for "(+N more)" if needed
		filesToShow := contentH
		if hasMore && contentH > 1 {
			filesToShow = contentH - 1
		}

		filesShown := 0
		for j := 0; j < filesToShow && j < totalFiles; j++ {
			fileStr := truncateWithEllipsis(tg.Files[j], boxWidth-3)
			drawTextAt(cells, w, boxX+1, contentY+filesShown, fileStr, colorDefaultFg, colorDefaultBg, terminal.AttrNone)
			filesShown++
		}

		// Show remaining count on its own line
		if hasMore {
			remaining := totalFiles - filesShown
			moreStr := fmt.Sprintf("(+%d more)", remaining)
			drawTextAt(cells, w, boxX+1, contentY+filesShown, moreStr, colorStatusFg, colorDefaultBg, terminal.AttrDim)
		}

		if totalFiles == 0 {
			drawTextAt(cells, w, boxX+1, contentY, "(no other files)", colorStatusFg, colorDefaultBg, terminal.AttrDim)
		}
	}

	// Show remaining tag groups count
	if len(tagLinks) > numBoxes {
		remaining := len(tagLinks) - numBoxes
		moreStr := fmt.Sprintf("(+%d more tags)", remaining)
		drawTextAt(cells, w, w-len(moreStr)-3, y+boxHeight-1, moreStr, colorStatusFg, colorDefaultBg, terminal.AttrDim)
	}
}

// renderTagSection draws tag group boxes with shared file lists
func calcMaxTagBoxes(w int) int {
	if w >= 180 {
		return 5
	}
	if w >= 140 {
		return 4
	}
	if w >= 100 {
		return 3
	}
	return 2
}

// drawSingleBox draws a single-line bordered rectangle
func drawSingleBox(cells []terminal.Cell, totalW, x, y, w, h int) {
	if w < 2 || h < 2 {
		return
	}

	// Corners
	setCell(cells, totalW, x, y, boxTL, colorPaneBorder)
	setCell(cells, totalW, x+w-1, y, boxTR, colorPaneBorder)
	setCell(cells, totalW, x, y+h-1, boxBL, colorPaneBorder)
	setCell(cells, totalW, x+w-1, y+h-1, boxBR, colorPaneBorder)

	// Horizontal edges
	for i := 1; i < w-1; i++ {
		setCell(cells, totalW, x+i, y, boxH, colorPaneBorder)
		setCell(cells, totalW, x+i, y+h-1, boxH, colorPaneBorder)
	}

	// Vertical edges
	for i := 1; i < h-1; i++ {
		setCell(cells, totalW, x, y+i, boxV, colorPaneBorder)
		setCell(cells, totalW, x+w-1, y+i, boxV, colorPaneBorder)
	}
}

// drawDoubleBox draws a double-line bordered rectangle
func drawDoubleBox(cells []terminal.Cell, totalW, x, y, w, h int) {
	if w < 2 || h < 2 {
		return
	}

	setCell(cells, totalW, x, y, dboxTL, colorPaneBorder)
	setCell(cells, totalW, x+w-1, y, dboxTR, colorPaneBorder)
	setCell(cells, totalW, x, y+h-1, dboxBL, colorPaneBorder)
	setCell(cells, totalW, x+w-1, y+h-1, dboxBR, colorPaneBorder)

	for i := 1; i < w-1; i++ {
		setCell(cells, totalW, x+i, y, dboxH, colorPaneBorder)
		setCell(cells, totalW, x+i, y+h-1, dboxH, colorPaneBorder)
	}

	for i := 1; i < h-1; i++ {
		setCell(cells, totalW, x, y+i, dboxV, colorPaneBorder)
		setCell(cells, totalW, x+w-1, y+i, dboxV, colorPaneBorder)
	}
}

// drawDoubleFrame draws outer double-line frame (alias for drawDoubleBox)
func drawDoubleFrame(cells []terminal.Cell, totalW, x, y, w, h int) {
	drawDoubleBox(cells, totalW, x, y, w, h)
}

// setCell safely sets a single cell with bounds checking
func setCell(cells []terminal.Cell, totalW, x, y int, r rune, fg terminal.RGB) {
	if x >= 0 && x < totalW && y >= 0 {
		idx := y*totalW + x
		if idx < len(cells) {
			cells[idx] = terminal.Cell{Rune: r, Fg: fg, Bg: colorDefaultBg}
		}
	}
}

// drawTextAt renders text at position with attributes
func drawTextAt(cells []terminal.Cell, totalW, x, y int, text string, fg, bg terminal.RGB, attr terminal.Attr) {
	for i, r := range text {
		if x+i >= totalW || x+i < 0 {
			break
		}
		idx := y*totalW + x + i
		if idx >= 0 && idx < len(cells) {
			cells[idx] = terminal.Cell{Rune: r, Fg: fg, Bg: bg, Attrs: attr}
		}
	}
}

// truncateWithEllipsis shortens string with ellipsis if exceeds maxLen
func truncateWithEllipsis(s string, maxLen int) string {
	if maxLen <= 3 {
		return s[:min(len(s), maxLen)]
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// formatFileTagsCompact formats file tags as compact #group{tags} string
func formatFileTagsCompact(fi *FileInfo) string {
	if fi == nil || len(fi.Tags) == 0 {
		return ""
	}

	groups := make([]string, 0, len(fi.Tags))
	for g := range fi.Tags {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	var parts []string
	for _, g := range groups {
		tags := fi.Tags[g]
		sorted := make([]string, len(tags))
		copy(sorted, tags)
		sort.Strings(sorted)
		parts = append(parts, fmt.Sprintf("#%s{%s}", g, joinTruncated(sorted, ",", 30)))
	}

	return strings.Join(parts, " ")
}

// joinTruncated joins strings with separator, truncating with ellipsis
func joinTruncated(items []string, sep string, maxLen int) string {
	if len(items) == 0 {
		return ""
	}

	result := items[0]
	for i := 1; i < len(items); i++ {
		next := result + sep + items[i]
		if len(next) > maxLen {
			return result + ",..."
		}
		result = next
	}
	return result
}