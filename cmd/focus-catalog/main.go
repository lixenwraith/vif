package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// Colors
var (
	colorHeaderBg     = terminal.RGB{R: 40, G: 60, B: 90}
	colorHeaderFg     = terminal.RGB{R: 255, G: 255, B: 255}
	colorSelected     = terminal.RGB{R: 80, G: 200, B: 80}
	colorUnselected   = terminal.RGB{R: 100, G: 100, B: 100}
	colorCursorBg     = terminal.RGB{R: 50, G: 50, B: 70}
	colorTagFg        = terminal.RGB{R: 100, G: 200, B: 220}
	colorGroupFg      = terminal.RGB{R: 220, G: 180, B: 80}
	colorStatusFg     = terminal.RGB{R: 140, G: 140, B: 140}
	colorHelpFg       = terminal.RGB{R: 100, G: 180, B: 200}
	colorInputBg      = terminal.RGB{R: 30, G: 30, B: 50}
	colorDefaultBg    = terminal.RGB{R: 20, G: 20, B: 30}
	colorDefaultFg    = terminal.RGB{R: 200, G: 200, B: 200}
	colorExpandedFg   = terminal.RGB{R: 180, G: 140, B: 220}
	colorAllTagFg     = terminal.RGB{R: 255, G: 180, B: 100}
	colorMatchCountFg = terminal.RGB{R: 180, G: 220, B: 180}
)

// Layout
const (
	headerHeight = 2
	statusHeight = 3
	helpHeight   = 2
	minWidth     = 60
	minHeight    = 15
)

const defaultModulePath = "github.com/USER/vi-fighter"

var outputPath string

func init() {
	flag.StringVar(&outputPath, "o", "catalog.txt", "output file path")
}

// FileInfo holds parsed data for a single Go file
type FileInfo struct {
	Path    string              // relative path: "systems/drain.go"
	Package string              // package name: "systems"
	Tags    map[string][]string // group → tags: {"core": ["ecs"], "game": ["drain"]}
	Imports []string            // local package names: ["events", "engine"]
	IsAll   bool                // has #all group
}

// PackageInfo aggregates files in a package
type PackageInfo struct {
	Name      string // "systems"
	Dir       string // "systems" or "cmd/focus-catalog"
	Files     []*FileInfo
	AllTags   map[string][]string // union of all file tags
	LocalDeps []string            // union of all file imports (local only)
	HasAll    bool                // any file has #all
}

// Index holds the complete codebase index
type Index struct {
	ModulePath string
	Packages   map[string]*PackageInfo // package name → info
	Files      map[string]*FileInfo    // relative path → info
	Groups     []string                // sorted list of all group names
}

// AppState holds all application state
type AppState struct {
	Term  terminal.Terminal
	Index *Index

	// Selection
	Selected   map[string]bool // selected package names
	ExpandDeps bool            // auto-expand dependencies
	DepthLimit int             // expansion depth

	// Filtering
	ActiveGroup    string          // "" = all, else specific group
	GroupIndex     int             // index into Groups slice for cycling
	KeywordFilter  string          // current keyword (empty = none)
	KeywordMatches map[string]bool // file paths matching keyword
	CaseSensitive  bool            // keyword case sensitivity
	RgAvailable    bool            // ripgrep installed

	// UI state
	PackageList   []string // sorted package names (filtered for display)
	AllPackages   []string // all package names (unfiltered)
	CursorPos     int      // currently highlighted package index
	ScrollOffset  int      // for scrolling long lists
	InputMode     bool     // true when typing keyword
	InputBuffer   string   // keyword input buffer
	Message       string   // status message (clears on next action)
	PreviewMode   bool     // showing file preview
	PreviewFiles  []string // files to preview
	PreviewScroll int      // preview scroll offset

	// Dimensions
	Width  int
	Height int
}

func main() {
	flag.Parse()

	term := terminal.New()
	if err := term.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "terminal init:", err)
		os.Exit(1)
	}
	defer term.Fini()

	w, h := term.Size()

	index, err := buildIndex(".")
	if err != nil {
		term.Fini()
		fmt.Fprintln(os.Stderr, "index build:", err)
		os.Exit(1)
	}

	_, rgErr := exec.LookPath("rg")

	app := &AppState{
		Term:           term,
		Index:          index,
		Selected:       make(map[string]bool),
		ExpandDeps:     true,
		DepthLimit:     2,
		KeywordMatches: make(map[string]bool),
		RgAvailable:    rgErr == nil,
		Width:          w,
		Height:         h,
	}

	app.AllPackages = make([]string, 0, len(index.Packages))
	for name := range index.Packages {
		app.AllPackages = append(app.AllPackages, name)
	}
	sort.Strings(app.AllPackages)
	app.updatePackageList()

	app.render()

	for {
		ev := term.PollEvent()

		switch ev.Type {
		case terminal.EventResize:
			app.Width = ev.Width
			app.Height = ev.Height
			app.render()
			continue

		case terminal.EventKey:
			if ev.Key == terminal.KeyCtrlC {
				return
			}

			quit, output := app.handleEvent(ev)
			if quit {
				return
			}
			if output {
				files := app.computeOutputFiles()
				err := writeOutputFile(outputPath, files)
				if err != nil {
					app.Message = fmt.Sprintf("write error: %v", err)
					app.render()
					continue
				}
				app.Message = fmt.Sprintf("wrote %d files to %s", len(files), outputPath)
				app.render()
				// Brief pause to show message before exit
				term.PollEvent()
				return
			}
		}

		app.render()
	}
}

// buildIndex scans the codebase and builds the index
func buildIndex(root string) (*Index, error) {
	modPath := getModulePath()

	index := &Index{
		ModulePath: modPath,
		Packages:   make(map[string]*PackageInfo),
		Files:      make(map[string]*FileInfo),
	}

	groupSet := make(map[string]bool)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		// Skip directories
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == ".git" || name == "testdata" {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}

		// Only .go files, skip tests
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, "/.") {
			return nil
		}

		relPath, _ := filepath.Rel(root, path)
		relPath = filepath.ToSlash(relPath)

		fi, err := parseFile(relPath, modPath)
		if err != nil {
			return nil // skip parse errors
		}
		if fi == nil {
			return nil
		}

		index.Files[relPath] = fi

		// Add to package
		pkg, ok := index.Packages[fi.Package]
		if !ok {
			dir := filepath.Dir(relPath)
			if dir == "." {
				dir = fi.Package
			}
			pkg = &PackageInfo{
				Name:    fi.Package,
				Dir:     dir,
				Files:   make([]*FileInfo, 0),
				AllTags: make(map[string][]string),
			}
			index.Packages[fi.Package] = pkg
		}

		pkg.Files = append(pkg.Files, fi)
		if fi.IsAll {
			pkg.HasAll = true
		}

		// Merge tags
		for group, tags := range fi.Tags {
			groupSet[group] = true
			existing := pkg.AllTags[group]
			tagSet := make(map[string]bool)
			for _, t := range existing {
				tagSet[t] = true
			}
			for _, t := range tags {
				if !tagSet[t] {
					existing = append(existing, t)
					tagSet[t] = true
				}
			}
			pkg.AllTags[group] = existing
		}

		// Merge imports
		depSet := make(map[string]bool)
		for _, d := range pkg.LocalDeps {
			depSet[d] = true
		}
		for _, imp := range fi.Imports {
			if !depSet[imp] {
				pkg.LocalDeps = append(pkg.LocalDeps, imp)
				depSet[imp] = true
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Build sorted groups list
	for g := range groupSet {
		if g != "all" {
			index.Groups = append(index.Groups, g)
		}
	}
	sort.Strings(index.Groups)

	return index, nil
}

// parseFile extracts info from a single Go file
func parseFile(path, modPath string) (*FileInfo, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fi := &FileInfo{
		Path: path,
		Tags: make(map[string][]string),
	}

	// Scan for package and @focus tags
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "package ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				fi.Package = parts[1]
			}
			break
		}

		if strings.HasPrefix(trimmed, "//") {
			tags, isAll, ok := parseTagLine(trimmed)
			if ok {
				for group, t := range tags {
					fi.Tags[group] = append(fi.Tags[group], t...)
				}
				if isAll {
					fi.IsAll = true
				}
			}
		}
	}

	if fi.Package == "" {
		return nil, nil
	}

	// Parse imports from already-read content
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, path, content, parser.ImportsOnly)
	if err != nil {
		return fi, nil
	}

	for _, imp := range astFile.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		if strings.HasPrefix(impPath, modPath+"/") {
			localPkg := strings.TrimPrefix(impPath, modPath+"/")
			parts := strings.Split(localPkg, "/")
			fi.Imports = append(fi.Imports, parts[len(parts)-1])
		}
	}

	return fi, nil
}

// parseTagLine parses a @focus comment line
// Returns tags map, isAll flag, and ok
func parseTagLine(line string) (map[string][]string, bool, bool) {
	line = strings.TrimPrefix(line, "//")
	line = strings.TrimSpace(line)

	if !strings.HasPrefix(line, "@focus:") {
		return nil, false, false
	}

	line = strings.TrimPrefix(line, "@focus:")
	line = strings.TrimSpace(line)

	result := make(map[string][]string)
	isAll := false

	// Parse #group { tag, tag } patterns
	for len(line) > 0 {
		// Find next #
		idx := strings.Index(line, "#")
		if idx == -1 {
			break
		}
		line = line[idx+1:]

		// Find group name (until space or {)
		endIdx := strings.IndexAny(line, " \t{")
		var groupName string
		if endIdx == -1 {
			groupName = line
			line = ""
		} else {
			groupName = line[:endIdx]
			line = line[endIdx:]
		}

		groupName = strings.TrimSpace(groupName)
		if groupName == "" {
			continue
		}

		if groupName == "all" {
			isAll = true
			continue
		}

		// Find tags in braces
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			result[groupName] = []string{}
			continue
		}

		line = line[1:] // skip {
		endBrace := strings.Index(line, "}")
		if endBrace == -1 {
			break
		}

		tagsStr := line[:endBrace]
		line = line[endBrace+1:]

		// Parse comma-separated tags
		tags := strings.Split(tagsStr, ",")
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t != "" {
				result[groupName] = append(result[groupName], t)
			}
		}
	}

	return result, isAll, true
}

// getModulePath reads module path from go.mod
func getModulePath() string {
	f, err := os.Open("go.mod")
	if err != nil {
		return defaultModulePath
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}

	return defaultModulePath
}

// expandDeps expands selected packages with their dependencies
func expandDeps(selected map[string]bool, index *Index, maxDepth int) map[string]bool {
	result := maps.Clone(selected)
	frontier := slices.Collect(maps.Keys(selected))

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, pkg := range frontier {
			if info, ok := index.Packages[pkg]; ok {
				for _, dep := range info.LocalDeps {
					if !result[dep] {
						result[dep] = true
						next = append(next, dep)
					}
				}
			}
		}
		frontier = next
	}

	return result
}

// searchKeyword shells to rg for content search
func searchKeyword(root, pattern string, caseSensitive bool) ([]string, error) {
	args := []string{"--files-with-matches", "-g", "*.go"}
	if !caseSensitive {
		args = append(args, "-i")
	}
	args = append(args, "--", pattern, root)

	cmd := exec.Command("rg", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // No matches
		}
		return nil, err
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}

	lines := strings.Split(raw, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimPrefix(l, "./")
		l = filepath.ToSlash(l)
		result = append(result, l)
	}

	return result, nil
}

// updatePackageList filters packages based on current group/keyword
func (app *AppState) updatePackageList() {
	app.PackageList = make([]string, 0, len(app.AllPackages))

	for _, name := range app.AllPackages {
		pkg := app.Index.Packages[name]

		// Group filter
		if app.ActiveGroup != "" {
			if _, ok := pkg.AllTags[app.ActiveGroup]; !ok && !pkg.HasAll {
				continue
			}
		}

		// Keyword filter
		if app.KeywordFilter != "" && len(app.KeywordMatches) > 0 {
			hasMatch := false
			for _, f := range pkg.Files {
				if app.KeywordMatches[f.Path] {
					hasMatch = true
					break
				}
			}
			if !hasMatch {
				continue
			}
		}

		app.PackageList = append(app.PackageList, name)
	}

	// Adjust cursor if out of bounds
	if app.CursorPos >= len(app.PackageList) {
		app.CursorPos = len(app.PackageList) - 1
	}
	if app.CursorPos < 0 {
		app.CursorPos = 0
	}
}

// computeOutputFiles generates the final file list
func (app *AppState) computeOutputFiles() []string {
	pkgSet := maps.Clone(app.Selected)

	if app.ExpandDeps {
		pkgSet = expandDeps(pkgSet, app.Index, app.DepthLimit)
	}

	fileSet := make(map[string]bool)

	// Add files from selected/expanded packages
	for pkgName := range pkgSet {
		if pkg, ok := app.Index.Packages[pkgName]; ok {
			for _, f := range pkg.Files {
				// If keyword filter active, intersect
				if app.KeywordFilter != "" && len(app.KeywordMatches) > 0 {
					if !app.KeywordMatches[f.Path] {
						continue
					}
				}
				fileSet[f.Path] = true
			}
		}
	}

	// Always include #all files
	for _, pkg := range app.Index.Packages {
		for _, f := range pkg.Files {
			if f.IsAll {
				fileSet[f.Path] = true
			}
		}
	}

	result := slices.Collect(maps.Keys(fileSet))
	sort.Strings(result)

	return result
}

// writeOutputFile writes the catalog to file
func writeOutputFile(path string, files []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, file := range files {
		fmt.Fprintf(w, "./%s\n", file)
	}
	return w.Flush()
}

// handleEvent processes a key event
func (app *AppState) handleEvent(ev terminal.Event) (quit, output bool) {
	app.Message = ""

	if app.PreviewMode {
		return app.handlePreviewEvent(ev)
	}

	if app.InputMode {
		return app.handleInputEvent(ev)
	}

	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case 'q':
			return true, false
		case 'j':
			app.moveCursor(1)
		case 'k':
			app.moveCursor(-1)
		case ' ':
			app.toggleSelection()
		case '/':
			if app.RgAvailable {
				app.InputMode = true
				app.InputBuffer = ""
			} else {
				app.Message = "ripgrep (rg) not found"
			}
		case 'g':
			app.cycleGroup()
		case 'd':
			app.ExpandDeps = !app.ExpandDeps
			if app.ExpandDeps {
				app.Message = "dependency expansion ON"
			} else {
				app.Message = "dependency expansion OFF"
			}
		case '+', '=':
			if app.DepthLimit < 5 {
				app.DepthLimit++
				app.Message = fmt.Sprintf("depth limit: %d", app.DepthLimit)
			}
		case '-':
			if app.DepthLimit > 1 {
				app.DepthLimit--
				app.Message = fmt.Sprintf("depth limit: %d", app.DepthLimit)
			}
		case 'a':
			for _, name := range app.PackageList {
				app.Selected[name] = true
			}
			app.Message = "selected all visible"
		case 'c':
			app.Selected = make(map[string]bool)
			app.Message = "cleared selection"
		case 'i':
			app.CaseSensitive = !app.CaseSensitive
			if app.CaseSensitive {
				app.Message = "case sensitive ON"
			} else {
				app.Message = "case sensitive OFF"
			}
		case 'p':
			app.enterPreview()
		}

	case terminal.KeyUp:
		app.moveCursor(-1)
	case terminal.KeyDown:
		app.moveCursor(1)
	case terminal.KeyEnter:
		return false, true
	case terminal.KeyEscape:
		if app.KeywordFilter != "" {
			app.KeywordFilter = ""
			app.KeywordMatches = make(map[string]bool)
			app.updatePackageList()
			app.Message = "keyword filter cleared"
		}
	}

	return false, false
}

// handleInputEvent handles keyboard input in input mode
func (app *AppState) handleInputEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyEscape:
		app.InputMode = false
		app.InputBuffer = ""
		return false, false

	case terminal.KeyEnter:
		app.InputMode = false
		if app.InputBuffer != "" {
			app.KeywordFilter = app.InputBuffer
			matches, err := searchKeyword(".", app.KeywordFilter, app.CaseSensitive)
			if err != nil {
				app.Message = "search error: " + err.Error()
				app.KeywordFilter = ""
			} else {
				app.KeywordMatches = make(map[string]bool)
				for _, m := range matches {
					app.KeywordMatches[m] = true
				}
				app.Message = fmt.Sprintf("found %d files", len(matches))
			}
			app.updatePackageList()
		}
		app.InputBuffer = ""
		return false, false

	case terminal.KeyBackspace:
		if len(app.InputBuffer) > 0 {
			app.InputBuffer = app.InputBuffer[:len(app.InputBuffer)-1]
		}
		return false, false

	case terminal.KeyRune:
		app.InputBuffer += string(ev.Rune)
		return false, false
	}

	return false, false
}

// handlePreviewEvent handles keyboard input in preview mode
func (app *AppState) handlePreviewEvent(ev terminal.Event) (quit, output bool) {
	maxScroll := len(app.PreviewFiles) - (app.Height - headerHeight - 2)
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch ev.Key {
	case terminal.KeyEscape, terminal.KeyRune:
		if ev.Key == terminal.KeyEscape || ev.Rune == 'p' || ev.Rune == 'q' {
			app.PreviewMode = false
			return false, false
		}
	case terminal.KeyUp:
		if app.PreviewScroll > 0 {
			app.PreviewScroll--
		}
	case terminal.KeyDown:
		if app.PreviewScroll < maxScroll {
			app.PreviewScroll++
		}
	}

	if ev.Key == terminal.KeyRune {
		switch ev.Rune {
		case 'j':
			if app.PreviewScroll < maxScroll {
				app.PreviewScroll++
			}
		case 'k':
			if app.PreviewScroll > 0 {
				app.PreviewScroll--
			}
		}
	}

	return false, false
}

func (app *AppState) moveCursor(delta int) {
	if len(app.PackageList) == 0 {
		app.CursorPos = 0
		app.ScrollOffset = 0
		return
	}
	app.CursorPos += delta
	if app.CursorPos < 0 {
		app.CursorPos = 0
	}
	if app.CursorPos >= len(app.PackageList) {
		app.CursorPos = len(app.PackageList) - 1
	}

	// Adjust scroll
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight
	if visibleRows < 1 {
		visibleRows = 1
	}

	if app.CursorPos < app.ScrollOffset {
		app.ScrollOffset = app.CursorPos
	}
	if app.CursorPos >= app.ScrollOffset+visibleRows {
		app.ScrollOffset = app.CursorPos - visibleRows + 1
	}
}

func (app *AppState) toggleSelection() {
	if len(app.PackageList) == 0 {
		return
	}
	name := app.PackageList[app.CursorPos]
	app.Selected[name] = !app.Selected[name]
	if !app.Selected[name] {
		delete(app.Selected, name)
	}
}

func (app *AppState) cycleGroup() {
	if len(app.Index.Groups) == 0 {
		return
	}

	if app.ActiveGroup == "" {
		app.GroupIndex = 0
		app.ActiveGroup = app.Index.Groups[0]
	} else {
		app.GroupIndex++
		if app.GroupIndex >= len(app.Index.Groups) {
			app.GroupIndex = -1
			app.ActiveGroup = ""
		} else {
			app.ActiveGroup = app.Index.Groups[app.GroupIndex]
		}
	}

	app.updatePackageList()
}

func (app *AppState) enterPreview() {
	app.PreviewFiles = app.computeOutputFiles()
	app.PreviewMode = true
	app.PreviewScroll = 0
}

// render draws the entire UI
func (app *AppState) render() {
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
		expandedPkgs = expandDeps(app.Selected, app.Index, app.DepthLimit)
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
	outputFiles := app.computeOutputFiles()
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