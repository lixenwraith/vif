package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

// ViewerLine represents a single line in the file viewer
type ViewerLine struct {
	Text      string
	LineNum   int          // 1-indexed display line number
	Spans     []StyledSpan // Styled segments for highlighting
	FoldStart bool         // True if this line starts a fold region
	FoldEnd   int          // End line (1-indexed) if FoldStart, 0 otherwise
	FoldLabel string       // Summary label when collapsed (e.g., "func main() { ... }")
	FoldIndex int          // Index into FoldRegions (-1 if not foldable)
	Hidden    bool         // True if inside a collapsed fold
}

// StyledSpan represents a styled text segment within a line
type StyledSpan struct {
	Start int      // Rune offset in line (0-indexed)
	End   int      // Rune offset end (exclusive)
	Kind  SpanKind // Type of span for styling
}

// SpanKind identifies the type of syntax element
type SpanKind uint8

const (
	SpanDefault SpanKind = iota
	SpanComment
	SpanString
	SpanKeyword
	SpanDefinition
	SpanType
	SpanNumber
)

// FoldRegion represents a collapsible code block
type FoldRegion struct {
	StartLine int    // 0-indexed line number
	EndLine   int    // 0-indexed line number (inclusive)
	Label     string // Display label when collapsed
	Kind      string // "func", "method", "type", "const", "var", "struct", "interface"
}

// FileViewerState manages file viewer overlay state
type FileViewerState struct {
	Visible  bool
	FilePath string

	Lines       []ViewerLine
	FoldRegions []FoldRegion
	FoldState   map[int]bool // FoldRegion index → collapsed

	Cursor    int // Current line index in visible lines
	Scroll    int // First visible line index
	ViewportH int // Updated during render

	SearchMode  bool
	SearchField *tui.TextFieldState
	SearchQuery string
	Matches     []int // Indices into Lines of matches
	MatchIndex  int   // Current match (-1 if none)
}

// NewFileViewerState creates initialized viewer state
func NewFileViewerState() *FileViewerState {
	return &FileViewerState{
		FoldState:   make(map[int]bool),
		SearchField: tui.NewTextFieldState(""),
		MatchIndex:  -1,
	}
}

// OpenFileViewer loads a file and displays the viewer overlay
func (app *AppState) OpenFileViewer(path string) {
	if app.Viewer == nil {
		app.Viewer = NewFileViewerState()
	}

	content, err := os.ReadFile(path)
	if err != nil {
		app.Message = "failed to open: " + err.Error()
		return
	}

	// Load raw lines
	lines := strings.Split(string(content), "\n")
	// Remove trailing empty line from split if file ends with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		lines = []string{""}
	}

	// Parse AST for Go files
	var foldRegions []FoldRegion
	var lineSpans [][]StyledSpan

	if strings.HasSuffix(path, ".go") {
		foldRegions, lineSpans = ParseFileAST(path, content, len(lines))
	}

	// Build ViewerLines
	viewerLines := make([]ViewerLine, len(lines))
	for i, text := range lines {
		viewerLines[i] = ViewerLine{
			Text:      text,
			LineNum:   i + 1,
			FoldIndex: -1,
		}
		if lineSpans != nil && i < len(lineSpans) {
			viewerLines[i].Spans = lineSpans[i]
		}
	}

	// Apply fold region info to lines
	for idx, region := range foldRegions {
		if region.StartLine < len(viewerLines) {
			viewerLines[region.StartLine].FoldStart = true
			viewerLines[region.StartLine].FoldEnd = region.EndLine + 1
			viewerLines[region.StartLine].FoldLabel = region.Label
			viewerLines[region.StartLine].FoldIndex = idx
		}
	}

	// Reset state
	app.Viewer.FilePath = path
	app.Viewer.Lines = viewerLines
	app.Viewer.FoldRegions = foldRegions
	app.Viewer.FoldState = make(map[int]bool)
	app.Viewer.Cursor = 0
	app.Viewer.Scroll = 0
	app.Viewer.SearchMode = false
	app.Viewer.SearchQuery = ""
	app.Viewer.Matches = nil
	app.Viewer.MatchIndex = -1
	app.Viewer.Visible = true
}

// CloseFileViewer hides the viewer overlay
func (app *AppState) CloseFileViewer() {
	if app.Viewer != nil {
		app.Viewer.Visible = false
	}
}

// handleViewerSearchEvent processes input during search mode
func (app *AppState) handleViewerSearchEvent(ev terminal.Event) {
	v := app.Viewer

	switch ev.Key {
	case terminal.KeyEscape:
		v.SearchMode = false
		return

	case terminal.KeyEnter:
		v.SearchMode = false
		v.SearchQuery = v.SearchField.Value()
		app.executeViewerSearch()
		return

	default:
		v.SearchField.HandleKey(ev.Key, ev.Rune, ev.Modifiers)
	}
}

// executeViewerSearch finds all lines matching the search query
func (app *AppState) executeViewerSearch() {
	v := app.Viewer
	if v.SearchQuery == "" {
		v.Matches = nil
		v.MatchIndex = -1
		return
	}

	query := strings.ToLower(v.SearchQuery)
	visible := v.visibleLines()
	v.Matches = nil

	for i, line := range visible {
		if strings.Contains(strings.ToLower(line.Text), query) {
			v.Matches = append(v.Matches, i)
		}
	}

	if len(v.Matches) > 0 {
		// Jump to first match at or after cursor
		v.MatchIndex = 0
		for i, m := range v.Matches {
			if m >= v.Cursor {
				v.MatchIndex = i
				break
			}
		}
		v.Cursor = v.Matches[v.MatchIndex]
		v.adjustScroll(len(visible))
	} else {
		v.MatchIndex = -1
	}
}

// viewerNextMatch jumps to next/prev search match
func (app *AppState) viewerNextMatch(delta int) {
	v := app.Viewer
	if len(v.Matches) == 0 {
		return
	}

	v.MatchIndex += delta
	if v.MatchIndex >= len(v.Matches) {
		v.MatchIndex = 0
	}
	if v.MatchIndex < 0 {
		v.MatchIndex = len(v.Matches) - 1
	}

	v.Cursor = v.Matches[v.MatchIndex]
	v.adjustScroll(len(v.visibleLines()))
}

// moveCursor adjusts cursor position by delta
func (v *FileViewerState) moveCursor(delta, total int) {
	v.Cursor += delta
	if v.Cursor < 0 {
		v.Cursor = 0
	}
	if v.Cursor >= total {
		v.Cursor = total - 1
	}
	if v.Cursor < 0 {
		v.Cursor = 0
	}
	v.adjustScroll(total)
}

// adjustScroll ensures cursor is visible in viewport
func (v *FileViewerState) adjustScroll(total int) {
	if v.ViewportH <= 0 {
		return
	}
	if v.Cursor < v.Scroll {
		v.Scroll = v.Cursor
	}
	if v.Cursor >= v.Scroll+v.ViewportH {
		v.Scroll = v.Cursor - v.ViewportH + 1
	}
	maxScroll := total - v.ViewportH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.Scroll > maxScroll {
		v.Scroll = maxScroll
	}
	if v.Scroll < 0 {
		v.Scroll = 0
	}
}

// formatLineNumber formats line number right-aligned
func formatLineNumber(num, width int) string {
	s := ""
	for num > 0 {
		s = string(rune('0'+num%10)) + s
		num /= 10
	}
	if s == "" {
		s = "0"
	}
	for len(s) < width {
		s = " " + s
	}
	return s
}

// formatSearchStatus formats search match indicator
func formatSearchStatus(current, total int, query string) string {
	return "[" + formatLineNumber(current, 1) + "/" + formatLineNumber(total, 1) + "] " + query
}

// formatPosition formats cursor position indicator
func formatPosition(line, total int) string {
	return formatLineNumber(line, 1) + "/" + formatLineNumber(total, 1) + " "
}

// ParseFileAST extracts fold regions and styled spans from Go source
func ParseFileAST(path string, content []byte, lineCount int) ([]FoldRegion, [][]StyledSpan) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return nil, nil
	}

	var regions []FoldRegion
	lineSpans := make([][]StyledSpan, lineCount)

	// Extract comments first (they have highest styling priority for overlap)
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			addSpanFromPos(fset, lineSpans, c.Pos(), c.End(), SpanComment)
		}
	}

	// Walk AST for fold regions and other spans
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return true
		}

		switch d := n.(type) {
		case *ast.FuncDecl:
			if d.Body != nil {
				startLine := fset.Position(d.Pos()).Line - 1
				endLine := fset.Position(d.End()).Line - 1
				if endLine > startLine {
					label := buildFuncLabel(d)
					kind := "func"
					if d.Recv != nil {
						kind = "method"
					}
					regions = append(regions, FoldRegion{
						StartLine: startLine,
						EndLine:   endLine,
						Label:     label,
						Kind:      kind,
					})
				}
			}
			// Highlight function name as definition
			if d.Name != nil {
				addSpanFromPos(fset, lineSpans, d.Name.Pos(), d.Name.End(), SpanDefinition)
			}

		case *ast.GenDecl:
			// Handle type, const, var declarations
			if d.Lparen.IsValid() && d.Rparen.IsValid() {
				// Multi-spec declaration with parens
				startLine := fset.Position(d.Pos()).Line - 1
				endLine := fset.Position(d.End()).Line - 1
				if endLine > startLine {
					kind := strings.ToLower(d.Tok.String())
					label := kind + " ( ... )"
					regions = append(regions, FoldRegion{
						StartLine: startLine,
						EndLine:   endLine,
						Label:     label,
						Kind:      kind,
					})
				}
			}

			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					// Highlight type name
					if s.Name != nil {
						addSpanFromPos(fset, lineSpans, s.Name.Pos(), s.Name.End(), SpanDefinition)
					}
					// Fold struct/interface bodies
					switch t := s.Type.(type) {
					case *ast.StructType:
						if t.Fields != nil && t.Fields.Opening.IsValid() {
							startLine := fset.Position(s.Pos()).Line - 1
							endLine := fset.Position(t.Fields.Closing).Line - 1
							if endLine > startLine {
								regions = append(regions, FoldRegion{
									StartLine: startLine,
									EndLine:   endLine,
									Label:     "type " + s.Name.Name + " struct { ... }",
									Kind:      "struct",
								})
							}
						}
					case *ast.InterfaceType:
						if t.Methods != nil && t.Methods.Opening.IsValid() {
							startLine := fset.Position(s.Pos()).Line - 1
							endLine := fset.Position(t.Methods.Closing).Line - 1
							if endLine > startLine {
								regions = append(regions, FoldRegion{
									StartLine: startLine,
									EndLine:   endLine,
									Label:     "type " + s.Name.Name + " interface { ... }",
									Kind:      "interface",
								})
							}
						}
					}

				case *ast.ValueSpec:
					// Highlight const/var names
					for _, name := range s.Names {
						if name.IsExported() {
							addSpanFromPos(fset, lineSpans, name.Pos(), name.End(), SpanDefinition)
						}
					}
				}
			}

		case *ast.BasicLit:
			// String and number literals
			switch d.Kind {
			case token.STRING, token.CHAR:
				addSpanFromPos(fset, lineSpans, d.Pos(), d.End(), SpanString)
			case token.INT, token.FLOAT, token.IMAG:
				addSpanFromPos(fset, lineSpans, d.Pos(), d.End(), SpanNumber)
			}

		case *ast.Ident:
			// Highlight type references (capitalized identifiers often are types)
			if isTypeName(d.Name) && !isKeyword(d.Name) {
				addSpanFromPos(fset, lineSpans, d.Pos(), d.End(), SpanType)
			}
		}

		return true
	})

	// Sort regions by start line for consistent folding
	sortFoldRegions(regions)

	return regions, lineSpans
}

// addSpanFromPos adds a styled span to the appropriate line(s)
func addSpanFromPos(fset *token.FileSet, lineSpans [][]StyledSpan, start, end token.Pos, kind SpanKind) {
	startPos := fset.Position(start)
	endPos := fset.Position(end)

	startLine := startPos.Line - 1
	endLine := endPos.Line - 1

	if startLine < 0 || startLine >= len(lineSpans) {
		return
	}

	if startLine == endLine {
		// Single line span
		lineSpans[startLine] = append(lineSpans[startLine], StyledSpan{
			Start: startPos.Column - 1,
			End:   endPos.Column - 1,
			Kind:  kind,
		})
	} else {
		// Multi-line span (mainly for block comments and raw strings)
		// First line: from start to end of line
		lineSpans[startLine] = append(lineSpans[startLine], StyledSpan{
			Start: startPos.Column - 1,
			End:   9999, // To end of line
			Kind:  kind,
		})
		// Middle lines: entire line
		for line := startLine + 1; line < endLine && line < len(lineSpans); line++ {
			lineSpans[line] = append(lineSpans[line], StyledSpan{
				Start: 0,
				End:   9999,
				Kind:  kind,
			})
		}
		// Last line: from start to end column
		if endLine < len(lineSpans) {
			lineSpans[endLine] = append(lineSpans[endLine], StyledSpan{
				Start: 0,
				End:   endPos.Column - 1,
				Kind:  kind,
			})
		}
	}
}

// buildFuncLabel creates display label for a function fold
func buildFuncLabel(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")

	if d.Recv != nil && len(d.Recv.List) > 0 {
		b.WriteString("(")
		if len(d.Recv.List[0].Names) > 0 {
			b.WriteString(d.Recv.List[0].Names[0].Name)
			b.WriteString(" ")
		}
		b.WriteString(typeString(d.Recv.List[0].Type))
		b.WriteString(") ")
	}

	b.WriteString(d.Name.Name)
	b.WriteString("(")

	// Abbreviated params
	if d.Type.Params != nil && len(d.Type.Params.List) > 0 {
		b.WriteString("...")
	}
	b.WriteString(")")

	// Return type hint
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		b.WriteString(" ")
		if len(d.Type.Results.List) == 1 && len(d.Type.Results.List[0].Names) == 0 {
			b.WriteString(typeString(d.Type.Results.List[0].Type))
		} else {
			b.WriteString("(...)")
		}
	}

	b.WriteString(" { ... }")
	return b.String()
}

// typeString returns simple string representation of a type expression
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeString(t.Elt)
		}
		return "[...]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{}"
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		return "chan " + typeString(t.Value)
	default:
		return "..."
	}
}

// isTypeName heuristically identifies type names (starts with uppercase)
func isTypeName(name string) bool {
	if len(name) == 0 {
		return false
	}
	r := []rune(name)
	return r[0] >= 'A' && r[0] <= 'Z'
}

// isKeyword checks if identifier is a Go keyword
func isKeyword(name string) bool {
	keywords := map[string]bool{
		"break": true, "case": true, "chan": true, "const": true, "continue": true,
		"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
		"func": true, "go": true, "goto": true, "if": true, "import": true,
		"interface": true, "map": true, "package": true, "range": true, "return": true,
		"select": true, "struct": true, "switch": true, "type": true, "var": true,
	}
	return keywords[name]
}

// sortFoldRegions sorts by start line, then by end line (larger regions first)
func sortFoldRegions(regions []FoldRegion) {
	for i := 0; i < len(regions); i++ {
		for j := i + 1; j < len(regions); j++ {
			if regions[j].StartLine < regions[i].StartLine ||
				(regions[j].StartLine == regions[i].StartLine && regions[j].EndLine > regions[i].EndLine) {
				regions[i], regions[j] = regions[j], regions[i]
			}
		}
	}
}

// visibleLines returns lines that are not hidden by folding
func (v *FileViewerState) visibleLines() []ViewerLine {
	result := make([]ViewerLine, 0, len(v.Lines))

	for i := 0; i < len(v.Lines); i++ {
		line := v.Lines[i]

		// Check if this line is hidden by a collapsed fold
		hidden := false
		for foldIdx, collapsed := range v.FoldState {
			if collapsed && foldIdx < len(v.FoldRegions) {
				region := v.FoldRegions[foldIdx]
				// Lines after fold start and up to fold end are hidden
				if i > region.StartLine && i <= region.EndLine {
					hidden = true
					break
				}
			}
		}

		if !hidden {
			result = append(result, line)
		}
	}

	return result
}

// toggleFold toggles the fold at the current cursor line
func (app *AppState) viewerToggleFold() {
	v := app.Viewer
	visible := v.visibleLines()
	if v.Cursor >= len(visible) {
		return
	}

	line := visible[v.Cursor]
	lineNum := line.LineNum - 1

	// Check if on fold start line
	if line.FoldStart && line.FoldIndex >= 0 {
		v.FoldState[line.FoldIndex] = true
		return
	}

	// Find enclosing fold region
	for i, region := range v.FoldRegions {
		if lineNum >= region.StartLine && lineNum <= region.EndLine {
			v.FoldState[i] = true
			// Move cursor to fold start
			for vi, vline := range v.visibleLines() {
				if vline.LineNum-1 == region.StartLine {
					v.Cursor = vi
					v.adjustScroll(len(v.visibleLines()))
					break
				}
			}
			return
		}
	}
}

// expandFold expands the fold at cursor
func (app *AppState) viewerExpandFold() {
	v := app.Viewer
	visible := v.visibleLines()
	if v.Cursor >= len(visible) {
		return
	}

	line := visible[v.Cursor]
	if line.FoldStart && line.FoldIndex >= 0 {
		v.FoldState[line.FoldIndex] = false
	}
}

// collapseFold collapses the fold at cursor
func (app *AppState) viewerCollapseFold() {
	v := app.Viewer
	visible := v.visibleLines()
	if v.Cursor >= len(visible) {
		return
	}

	line := visible[v.Cursor]
	if line.FoldStart && line.FoldIndex >= 0 {
		v.FoldState[line.FoldIndex] = true
	}
}

// collapseAllFolds collapses all fold regions
func (app *AppState) viewerCollapseAllFolds() {
	v := app.Viewer
	for i := range v.FoldRegions {
		v.FoldState[i] = true
	}
	// Reset cursor to prevent it being in hidden area
	v.Cursor = 0
	v.Scroll = 0
}

// expandAllFolds expands all fold regions
func (app *AppState) viewerExpandAllFolds() {
	v := app.Viewer
	v.FoldState = make(map[int]bool)
}

// handleViewerEvent processes keyboard input for the file viewer
func (app *AppState) handleViewerEvent(ev terminal.Event) {
	v := app.Viewer
	if v == nil {
		return
	}

	if v.SearchMode {
		app.handleViewerSearchEvent(ev)
		return
	}

	visible := v.visibleLines()
	total := len(visible)

	switch ev.Key {
	case terminal.KeyEscape, terminal.KeyCtrlC:
		app.CloseFileViewer()
		return

	case terminal.KeyUp:
		v.moveCursor(-1, total)
	case terminal.KeyDown:
		v.moveCursor(1, total)
	case terminal.KeyPageUp, terminal.KeyCtrlU:
		v.moveCursor(-v.ViewportH/2, total)
	case terminal.KeyPageDown, terminal.KeyCtrlD:
		v.moveCursor(v.ViewportH/2, total)
	case terminal.KeyHome:
		v.Cursor = 0
		v.adjustScroll(total)
	case terminal.KeyEnd:
		if total > 0 {
			v.Cursor = total - 1
		}
		v.adjustScroll(total)
	case terminal.KeyEnter, terminal.KeyRight:
		app.viewerExpandFold()
	case terminal.KeyLeft:
		app.viewerCollapseFold()

	case terminal.KeyRune:
		switch ev.Rune {
		case 'q':
			app.CloseFileViewer()
			return
		case 'j':
			v.moveCursor(1, total)
		case 'k':
			v.moveCursor(-1, total)
		case 'g':
			v.Cursor = 0
			v.adjustScroll(total)
		case 'G':
			if total > 0 {
				v.Cursor = total - 1
			}
			v.adjustScroll(total)
		case '/':
			v.SearchMode = true
			v.SearchField.Clear()
		case 'n':
			app.viewerNextMatch(1)
		case 'N':
			app.viewerNextMatch(-1)
		case 'l':
			app.viewerExpandFold()
		case 'h':
			app.viewerCollapseFold()
		case 'o':
			app.viewerToggleFold()
		case 'M':
			app.viewerCollapseAllFolds()
		case 'R':
			app.viewerExpandAllFolds()
		}
	}
}

// renderFileViewer draws the file viewer overlay
func (app *AppState) renderFileViewer(r tui.Region) {
	v := app.Viewer
	if v == nil || !v.Visible {
		return
	}

	// Build hint text
	hint := "q:quit /:search o:fold M:foldAll R:unfoldAll"
	if len(v.Matches) > 0 {
		hint = "n/N:match " + hint
	}

	content := r.Modal(tui.ModalOpts{
		Title:    filepath.Base(v.FilePath),
		Hint:     hint,
		Border:   tui.LineDouble,
		BorderFg: app.Theme.Border,
		TitleFg:  app.Theme.HeaderFg,
		HintFg:   app.Theme.StatusFg,
		Bg:       app.Theme.Bg,
	})

	var mainContent, statusBar tui.Region
	mainContent, statusBar = tui.SplitVFixed(content, content.H-1)

	// Dynamic gutter width
	gutterW := 3
	lineCount := len(v.Lines)
	if lineCount >= 1000 {
		gutterW = 5
	} else if lineCount >= 100 {
		gutterW = 4
	}

	gutter, textArea := tui.SplitHFixed(mainContent, gutterW+1)

	v.ViewportH = textArea.H
	visible := v.visibleLines()
	v.adjustScroll(len(visible))

	// Match set for highlighting
	matchSet := make(map[int]bool)
	for _, m := range v.Matches {
		matchSet[m] = true
	}

	// Render visible lines
	for y := 0; y < textArea.H; y++ {
		idx := v.Scroll + y
		if idx >= len(visible) {
			break
		}

		line := visible[idx]
		isCursor := idx == v.Cursor
		isMatch := matchSet[idx]
		isCollapsed := line.FoldStart && line.FoldIndex >= 0 && v.FoldState[line.FoldIndex]

		// Row backgrounds
		bg := app.Theme.Bg
		gutterBg := app.Theme.Bg
		if isCursor {
			bg = app.Theme.CursorBg
			gutterBg = app.Theme.FocusBg
		}

		// Clear gutter
		for x := 0; x < gutter.W; x++ {
			gutter.Cell(x, y, ' ', app.Theme.StatusFg, gutterBg, terminal.AttrNone)
		}

		// Line number
		numStr := formatLineNumber(line.LineNum, gutterW)
		for i, ch := range numStr {
			gutter.Cell(i, y, ch, app.Theme.StatusFg, gutterBg, terminal.AttrDim)
		}

		// Separator
		gutter.Cell(gutterW, y, '│', app.Theme.Border, gutterBg, terminal.AttrDim)

		// Clear text area
		for x := 0; x < textArea.W; x++ {
			textArea.Cell(x, y, ' ', app.Theme.Fg, bg, terminal.AttrNone)
		}

		// Fold indicator
		foldIndicatorW := 0
		if line.FoldStart {
			foldIndicatorW = 2
			indicator := '▼'
			if isCollapsed {
				indicator = '▶'
			}
			textArea.Cell(0, y, indicator, app.Theme.ViewerFold, bg, terminal.AttrBold)
		}

		// Determine text to render
		text := line.Text
		if isCollapsed && line.FoldLabel != "" {
			text = line.FoldLabel
		}

		// Render text with spans
		app.renderViewerLine(textArea, y, foldIndicatorW, text, line.Spans, isCollapsed, isMatch, bg)
	}

	// Status bar
	statusBar.Fill(app.Theme.Bg)

	if v.SearchMode {
		// Use Input which renders directly with cursor
		statusBar.Input(0, tui.InputOpts{
			Label:    "Search: ",
			LabelFg:  app.Theme.StatusFg,
			Text:     v.SearchField.Value(),
			Cursor:   v.SearchField.Cursor,
			CursorBg: app.Theme.HeaderFg,
			TextFg:   app.Theme.HeaderFg,
			Bg:       app.Theme.InputBg,
		})
	} else {
		var status string
		if len(v.Matches) > 0 {
			status = formatSearchStatus(v.MatchIndex+1, len(v.Matches), v.SearchQuery)
		} else if v.SearchQuery != "" {
			status = "no matches: " + v.SearchQuery
		}

		leftText := status
		if leftText == "" {
			leftText = v.FilePath
		}
		statusBar.Text(1, 0, leftText, app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)

		// Fold count
		collapsedCount := 0
		for _, collapsed := range v.FoldState {
			if collapsed {
				collapsedCount++
			}
		}
		foldInfo := ""
		if len(v.FoldRegions) > 0 {
			foldInfo = formatFoldInfo(collapsedCount, len(v.FoldRegions)) + "  "
		}

		posText := foldInfo + formatPosition(v.Cursor+1, len(visible))
		statusBar.TextRight(0, posText, app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
	}
}

// renderViewerLine renders a single line with styled spans
func (app *AppState) renderViewerLine(r tui.Region, y, startX int, text string, spans []StyledSpan, isCollapsed, isMatch bool, bg terminal.RGB) {
	runes := []rune(text)
	x := startX

	// Build span lookup: rune index → SpanKind
	spanKinds := make([]SpanKind, len(runes))
	for _, span := range spans {
		for i := span.Start; i < span.End && i < len(spanKinds); i++ {
			if i >= 0 {
				spanKinds[i] = span.Kind
			}
		}
	}

	// Render with tab expansion
	runeIdx := 0
	for runeIdx < len(runes) {
		if x >= r.W {
			break
		}

		ch := runes[runeIdx]

		// Get style for this rune
		fg := app.Theme.Fg
		attr := terminal.AttrNone

		if runeIdx < len(spanKinds) {
			switch spanKinds[runeIdx] {
			case SpanComment:
				fg = app.Theme.ViewerComment
				attr = terminal.AttrDim
			case SpanString:
				fg = app.Theme.ViewerString
			case SpanKeyword:
				fg = app.Theme.ViewerKeyword
				attr = terminal.AttrBold
			case SpanDefinition:
				fg = app.Theme.ViewerDefinition
				attr = terminal.AttrBold
			case SpanType:
				fg = app.Theme.ViewerType
			case SpanNumber:
				fg = app.Theme.ViewerNumber
			}
		}

		// Collapsed fold uses special style
		if isCollapsed {
			fg = app.Theme.ViewerDefinition
			attr = terminal.AttrDim
		}

		// Match highlight overrides
		if isMatch && app.Viewer.SearchQuery != "" {
			// Check if this position is part of match
			lowerText := strings.ToLower(text)
			lowerQuery := strings.ToLower(app.Viewer.SearchQuery)
			matchIdx := strings.Index(lowerText, lowerQuery)
			if matchIdx >= 0 {
				runeMatchStart := len([]rune(text[:matchIdx]))
				runeMatchEnd := runeMatchStart + len([]rune(app.Viewer.SearchQuery))
				if runeIdx >= runeMatchStart && runeIdx < runeMatchEnd {
					fg = app.Theme.HeaderFg
					bg = app.Theme.ViewerMatch
				}
			}
		}

		if ch == '\t' {
			spaces := 4 - ((x - startX) % 4)
			for s := 0; s < spaces && x < r.W; s++ {
				r.Cell(x, y, ' ', fg, bg, attr)
				x++
			}
		} else {
			r.Cell(x, y, ch, fg, bg, attr)
			x++
		}

		runeIdx++
	}
}

// formatFoldInfo formats fold count indicator
func formatFoldInfo(collapsed, total int) string {
	if collapsed == 0 {
		return ""
	}
	return "[" + formatLineNumber(collapsed, 1) + "/" + formatLineNumber(total, 1) + " folded]"
}