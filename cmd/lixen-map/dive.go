package main

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// DiveState holds computed relationship data for dive view visualization
type DiveState struct {
	SourcePath string
	FileInfo   *FileInfo

	DependsOn     []DivePackage
	DependedBy    []DivePackage
	FocusLinks    []DiveTagGroup
	InteractLinks []DiveTagGroup
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
		switch ev.Rune {
		case '?':
			app.HelpMode = true
		case 'q':
			app.ExitDive()
		}
	}
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
	drawText(cells, w, 2, 0, title, colorHeaderFg, colorDefaultBg, terminal.AttrBold)
	hint := "[Esc:back]"
	drawText(cells, w, w-len(hint)-2, 0, hint, colorHelpFg, colorDefaultBg, terminal.AttrNone)

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

	// Focus box height
	focusBoxH := 4

	// Allocate sections dynamically based on content
	depsH, tagsH := allocateSections(contentH, focusBoxH, state.DependsOn, state.DependedBy, state.FocusLinks, state.InteractLinks)

	y := contentTop

	// Render dependencies section
	if depsH > 0 && (len(state.DependsOn) > 0 || len(state.DependedBy) > 0) {
		y = renderDepsSection(cells, w, y, depsH, state.DependsOn, state.DependedBy)
	}

	// Render focus box (file info)
	y = renderFocusBox(cells, w, y, state)

	// Render tag links sections
	if tagsH > 0 && (len(state.FocusLinks) > 0 || len(state.InteractLinks) > 0) {
		remainingSpace := contentBottom - y

		focusLinksH := 0
		interactLinksH := 0

		if len(state.FocusLinks) > 0 && len(state.InteractLinks) > 0 {
			// Split based on content
			focusContent := len(state.FocusLinks)
			interactContent := len(state.InteractLinks)
			total := focusContent + interactContent
			focusLinksH = remainingSpace * focusContent / total
			interactLinksH = remainingSpace - focusLinksH
			// Ensure minimums
			if focusLinksH < 4 {
				focusLinksH = 4
			}
			if interactLinksH < 4 {
				interactLinksH = 4
			}
		} else if len(state.FocusLinks) > 0 {
			focusLinksH = remainingSpace
		} else {
			interactLinksH = remainingSpace
		}

		// Render Focus links
		if focusLinksH > 0 && len(state.FocusLinks) > 0 {
			y = renderTagLinkSection(cells, w, y, focusLinksH, "FOCUS", state.FocusLinks, colorGroupFg)
		}

		// Render Interact links
		if interactLinksH > 0 && len(state.InteractLinks) > 0 {
			y = renderTagLinkSection(cells, w, y, interactLinksH, "INTERACT", state.InteractLinks, colorExpandedFg)
		}
	}

	// Help bar
	helpY := h - 1
	help := "?:help Esc/q:back ^Q:quit"
	drawText(cells, w, 2, helpY, help, colorHelpFg, colorDefaultBg, terminal.AttrDim)
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

	// Compute tag links for both Focus and Interact
	state.FocusLinks = computeTagLinks(app, fi.Focus, path)
	state.InteractLinks = computeTagLinks(app, fi.Interact, path)

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
func computeTagLinks(app *AppState, sourceTagMap map[string][]string, selfPath string) []DiveTagGroup {
	var links []DiveTagGroup

	// Get sorted groups
	groups := make([]string, 0, len(sourceTagMap))
	for g := range sourceTagMap {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	for _, group := range groups {
		tags := sourceTagMap[group]
		sortedTags := make([]string, len(tags))
		copy(sortedTags, tags)
		sort.Strings(sortedTags)

		for _, tag := range sortedTags {
			tg := DiveTagGroup{
				Group: group,
				Tag:   tag,
			}

			// Find all files with this tag (in same tag category)
			var files []string
			for path, fileInfo := range app.Index.Files {
				if path == selfPath {
					continue
				}
				// Check if file has this group in same category
				var targetMap map[string][]string
				if _, inFocus := fileInfo.Focus[group]; inFocus {
					targetMap = fileInfo.Focus
				} else if _, inInteract := fileInfo.Interact[group]; inInteract {
					targetMap = fileInfo.Interact
				}

				if targetMap != nil {
					if fileTags, ok := targetMap[group]; ok {
						for _, t := range fileTags {
							if t == tag {
								files = append(files, path)
								break
							}
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

// allocateSections distributes vertical space between dependency and tag areas
func allocateSections(totalH, focusH int, dependsOn, dependedBy []DivePackage, focusLinks, interactLinks []DiveTagGroup) (depsH, tagsH int) {
	available := totalH - focusH - 2 // -2 for connectors/margins

	if available < 4 {
		return 0, 0
	}

	// Calculate actual content needs
	depsContentH := calcDepsContentHeight(dependsOn, dependedBy)
	tagsContentH := calcTagsContentHeight(focusLinks, interactLinks)

	hasDeps := len(dependsOn) > 0 || len(dependedBy) > 0
	hasTags := len(focusLinks) > 0 || len(interactLinks) > 0

	if !hasDeps && !hasTags {
		return 0, 0
	}

	if !hasDeps {
		return 0, available
	}
	if !hasTags {
		return available, 0
	}

	// Proportional allocation based on content
	totalContent := depsContentH + tagsContentH
	if totalContent == 0 {
		totalContent = 1
	}

	depsH = (available * depsContentH) / totalContent
	tagsH = available - depsH

	// Enforce minimums
	minDeps := min(6, depsContentH+2)
	minTags := min(6, tagsContentH+2)

	if depsH < minDeps && available >= minDeps+minTags {
		depsH = minDeps
		tagsH = available - depsH
	}
	if tagsH < minTags && available >= minDeps+minTags {
		tagsH = minTags
		depsH = available - tagsH
	}

	// Cap at actual content needs + borders
	maxDepsH := depsContentH + 3
	if depsH > maxDepsH && tagsContentH > 0 {
		depsH = maxDepsH
		tagsH = available - depsH
	}

	return depsH, tagsH
}

// calcDepsContentHeight estimates lines needed for dependency content
func calcDepsContentHeight(dependsOn, dependedBy []DivePackage) int {
	maxLines := 0
	for _, dp := range dependsOn {
		lines := 1 + len(dp.Files) // dir + files
		if lines > maxLines {
			maxLines = lines
		}
	}
	for _, dp := range dependedBy {
		lines := 1 + len(dp.Files)
		if lines > maxLines {
			maxLines = lines
		}
	}
	// Account for multiple packages in columns
	totalPkgs := len(dependsOn) + len(dependedBy)
	if totalPkgs > 2 {
		maxLines += (totalPkgs / 3) * 2 // rough estimate for wrapping
	}
	return maxLines
}

// calcTagsContentHeight estimates lines needed for tag link content
func calcTagsContentHeight(focusLinks, interactLinks []DiveTagGroup) int {
	maxFiles := 0
	for _, tg := range focusLinks {
		if len(tg.Files) > maxFiles {
			maxFiles = len(tg.Files)
		}
	}
	for _, tg := range interactLinks {
		if len(tg.Files) > maxFiles {
			maxFiles = len(tg.Files)
		}
	}
	// Two sections (focus + interact) with headers
	sections := 0
	if len(focusLinks) > 0 {
		sections++
	}
	if len(interactLinks) > 0 {
		sections++
	}
	return maxFiles + 3 + (sections * 2)
}

// renderDepsSection draws the depends-on and depended-by columns
func renderDepsSection(cells []terminal.Cell, w, y, maxH int, dependsOn, dependedBy []DivePackage) int {
	innerW := w - 4
	startY := y

	hasDepOn := len(dependsOn) > 0
	hasDepBy := len(dependedBy) > 0

	if !hasDepOn && !hasDepBy {
		return y
	}

	// Dynamic column width based on content count
	var depOnW, depByW int
	if hasDepOn && hasDepBy {
		// Split proportionally based on package count
		totalPkgs := len(dependsOn) + len(dependedBy)
		depOnRatio := len(dependsOn) * 100 / totalPkgs
		// Clamp between 30% and 70%
		if depOnRatio < 30 {
			depOnRatio = 30
		}
		if depOnRatio > 70 {
			depOnRatio = 70
		}
		depOnW = innerW * depOnRatio / 100
		depByW = innerW - depOnW - 1 // -1 for separator
	} else if hasDepOn {
		depOnW = innerW
	} else {
		depByW = innerW
	}

	// Calculate required height based on content
	boxHeight := calcDepsBoxHeightDynamic(dependsOn, dependedBy, depOnW, depByW, maxH)

	drawSingleBox(cells, w, 2, y, innerW, boxHeight)

	// Headers
	if hasDepOn {
		hdr := fmt.Sprintf(" DEPENDS ON (%d) ", len(dependsOn))
		drawText(cells, w, 4, y, hdr, colorGroupFg, colorDefaultBg, terminal.AttrBold)
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
		drawText(cells, w, hdrX+1, y, hdr, colorExpandedFg, colorDefaultBg, terminal.AttrBold)
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

// calcDepsBoxHeightDynamic computes height based on actual content layout
func calcDepsBoxHeightDynamic(dependsOn, dependedBy []DivePackage, depOnW, depByW, maxH int) int {
	// Simulate layout to find actual height needed
	leftH := calcColumnHeight(dependsOn, depOnW)
	rightH := calcColumnHeight(dependedBy, depByW)

	neededH := max(leftH, rightH) + 2 // +2 for borders
	if neededH < 4 {
		neededH = 4
	}
	return min(maxH, neededH)
}

// calcColumnHeight calculates rows needed to render packages in given width
func calcColumnHeight(packages []DivePackage, availW int) int {
	if len(packages) == 0 || availW < 10 {
		return 0
	}

	colW := min(availW, 22)
	numCols := max(1, availW/(colW+1))

	totalRows := 0
	col := 0
	rowHeight := 0

	for _, pkg := range packages {
		pkgHeight := 1 + min(len(pkg.Files), 6) // dir + up to 6 files
		if len(pkg.Files) > 6 {
			pkgHeight++ // +1 for "(+N more)"
		}

		if pkgHeight > rowHeight {
			rowHeight = pkgHeight
		}

		col++
		if col >= numCols {
			totalRows += rowHeight + 1 // +1 gap
			col = 0
			rowHeight = 0
		}
	}

	// Final partial row
	if col > 0 {
		totalRows += rowHeight
	}

	return totalRows
}

// renderPackageList draws a multi-column list of packages with their files
func renderPackageList(cells []terminal.Cell, totalW, x, y, availW, availH int, packages []DivePackage, fg terminal.RGB) {
	if len(packages) == 0 || availW < 10 || availH < 1 {
		return
	}

	// Calculate column layout
	colW := min(availW, 22)
	numCols := max(1, availW/(colW+1))
	colW = (availW - numCols + 1) / numCols

	col := 0
	rowOffset := 0
	maxRowInGroup := 0
	pkgShown := 0
	totalPkgs := len(packages)

	for _, pkg := range packages {
		if rowOffset >= availH {
			break
		}

		colX := x + col*(colW+1)
		localRow := 0

		// Package directory
		dirStr := truncateWithEllipsis(pkg.Dir+"/", colW)
		drawText(cells, totalW, colX, y+rowOffset+localRow, dirStr, fg, colorDefaultBg, terminal.AttrBold)
		localRow++
		pkgShown++

		// Calculate how many files we can show
		remainingRows := availH - rowOffset - localRow
		maxFilesPerPkg := min(remainingRows, 8) // Show up to 8 files
		if maxFilesPerPkg < 1 {
			maxFilesPerPkg = 1
		}

		totalFiles := len(pkg.Files)
		hasMore := totalFiles > maxFilesPerPkg

		filesToShow := maxFilesPerPkg
		if hasMore && maxFilesPerPkg > 1 {
			filesToShow = maxFilesPerPkg - 1
		}

		filesShown := 0
		for i := 0; i < filesToShow && i < totalFiles; i++ {
			fileStr := " " + truncateWithEllipsis(pkg.Files[i], colW-2)
			drawText(cells, totalW, colX, y+rowOffset+localRow, fileStr, colorDefaultFg, colorDefaultBg, terminal.AttrNone)
			localRow++
			filesShown++
		}

		if hasMore {
			remaining := totalFiles - filesShown
			moreStr := fmt.Sprintf(" (+%d)", remaining)
			drawText(cells, totalW, colX, y+rowOffset+localRow, moreStr, colorStatusFg, colorDefaultBg, terminal.AttrDim)
			localRow++
		}

		if localRow > maxRowInGroup {
			maxRowInGroup = localRow
		}

		col++
		if col >= numCols {
			col = 0
			rowOffset += maxRowInGroup + 1
			maxRowInGroup = 0
		}

		if col == 0 && rowOffset >= availH && pkgShown < totalPkgs {
			break
		}
	}

	// Show remaining packages indicator only if we couldn't show all
	if pkgShown < totalPkgs {
		remaining := totalPkgs - pkgShown
		moreStr := fmt.Sprintf("(+%d more)", remaining)
		finalRow := min(rowOffset, availH-1)
		if col > 0 {
			finalRow = min(rowOffset+maxRowInGroup, availH-1)
		}
		drawText(cells, totalW, x, y+finalRow, moreStr, colorStatusFg, colorDefaultBg, terminal.AttrDim)
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

// renderTagLinkSection draws a labeled section of tag link boxes
func renderTagLinkSection(cells []terminal.Cell, w, y, maxH int, label string, tagLinks []DiveTagGroup, labelColor terminal.RGB) int {
	if len(tagLinks) == 0 || maxH < 3 {
		return y
	}

	// Draw section label
	labelStr := fmt.Sprintf(" %s LINKS ", label)
	drawText(cells, w, 2, y, labelStr, labelColor, colorDefaultBg, terminal.AttrBold)
	y++

	// Calculate box layout
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
			hdr = fmt.Sprintf(" %s (%d) ", truncateWithEllipsis(tg.Tag, 8), tg.Count)
		}
		drawText(cells, w, boxX+1, y, hdr, colorGroupFg, colorDefaultBg, terminal.AttrBold)

		// Files
		contentY := y + 1
		contentH := boxHeight - 2
		if contentH < 1 {
			contentH = 1
		}

		totalFiles := len(tg.Files)
		hasMore := totalFiles > contentH

		filesToShow := contentH
		if hasMore && contentH > 1 {
			filesToShow = contentH - 1
		}

		filesShown := 0
		for j := 0; j < filesToShow && j < totalFiles; j++ {
			fileStr := truncateWithEllipsis(tg.Files[j], boxWidth-3)
			drawText(cells, w, boxX+1, contentY+filesShown, fileStr, colorDefaultFg, colorDefaultBg, terminal.AttrNone)
			filesShown++
		}

		if hasMore {
			remaining := totalFiles - filesShown
			moreStr := fmt.Sprintf("(+%d more)", remaining)
			drawText(cells, w, boxX+1, contentY+filesShown, moreStr, colorStatusFg, colorDefaultBg, terminal.AttrDim)
		}

		if totalFiles == 0 {
			drawText(cells, w, boxX+1, contentY, "(no other files)", colorStatusFg, colorDefaultBg, terminal.AttrDim)
		}
	}

	// Show remaining tag groups count
	if len(tagLinks) > numBoxes {
		remaining := len(tagLinks) - numBoxes
		moreStr := fmt.Sprintf("(+%d more)", remaining)
		drawText(cells, w, w-len(moreStr)-3, y+boxHeight-1, moreStr, colorStatusFg, colorDefaultBg, terminal.AttrDim)
	}

	return y + boxHeight
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
	drawText(cells, w, titleX, y, title, colorAllTagFg, colorDefaultBg, terminal.AttrBold)

	// Package info
	pkgStr := fmt.Sprintf("Package: %s", state.FileInfo.Package)
	drawText(cells, w, boxX+2, y+1, pkgStr, colorDirFg, colorDefaultBg, terminal.AttrNone)

	// Tags summary (both focus and interact counts)
	focusCount := 0
	for _, tags := range state.FileInfo.Focus {
		focusCount += len(tags)
	}
	interactCount := 0
	for _, tags := range state.FileInfo.Interact {
		interactCount += len(tags)
	}
	tagStr := fmt.Sprintf("Focus: %d tags  Interact: %d tags", focusCount, interactCount)
	drawText(cells, w, boxX+len(pkgStr)+4, y+1, tagStr, colorTagFg, colorDefaultBg, terminal.AttrNone)

	// Imports summary
	impStr := fmt.Sprintf("Imports: %d local", len(state.FileInfo.Imports))
	drawText(cells, w, boxX+2, y+2, impStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)

	// Importers count
	impByCount := len(state.DependedBy)
	impByStr := fmt.Sprintf("Imported by: %d packages", impByCount)
	drawText(cells, w, boxX+len(impStr)+4, y+2, impByStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)

	return y + boxHeight
}