package main

import (
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

type DemoView int

const (
	ViewTextField DemoView = iota
	ViewEditor
	ViewTree
	ViewList
	ViewDialog
	ViewToast
	ViewProgress
	ViewTable
	ViewCount // sentinel for cycling
)

var viewNames = []string{
	"TextField", "Editor", "Tree", "List", "Dialog", "Toast", "Progress", "Table",
}

type appState struct {
	view   DemoView
	frame  int
	theme  tui.Theme
	term   terminal.Terminal
	width  int
	height int
	quit   bool

	// TextField demo
	textField   *tui.TextFieldState
	searchField *tui.TextFieldState

	// Editor demo
	editor *tui.EditorState

	// Tree demo
	treeState     *tui.TreeState
	treeExpansion *tui.TreeExpansion
	treeNodes     []tui.TreeNode

	// List demo
	listCursor int
	listScroll int
	listItems  []tui.ListItem

	// Dialog demo
	confirmState *tui.ConfirmState
	showConfirm  bool
	dialogResult string

	// Toast demo
	toast      *tui.ToastState
	toastCount int

	// Progress demo
	progress      *tui.ProgressState
	progressValue float64
}

func main() {
	term := terminal.New()
	if err := term.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "terminal init:", err)
		os.Exit(1)
	}
	defer term.Fini()

	w, h := term.Size()
	app := &appState{
		term:   term,
		width:  w,
		height: h,
		theme:  tui.DefaultTheme,
	}
	app.initDemos()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for !app.quit {
		app.render()

		select {
		case <-ticker.C:
			app.frame++
			app.updateAnimations()
		default:
		}

		ev := term.PollEvent()
		app.handleEvent(ev)
	}
}

func (app *appState) initDemos() {
	// TextField
	app.textField = tui.NewTextFieldState("Hello, TUI!")
	app.searchField = tui.NewTextFieldState("")

	// Editor
	app.editor = tui.NewEditorState("Line 1: Welcome to the multi-line editor\nLine 2: Use arrow keys to navigate\nLine 3: Type to insert text\nLine 4: Backspace/Delete to remove")

	// Tree
	app.treeExpansion = tui.NewTreeExpansion()
	app.treeExpansion.Expand("root")
	app.treeState = tui.NewTreeState(10)
	app.rebuildTreeNodes()

	// List
	app.listItems = []tui.ListItem{
		{Icon: tui.IconBullet, Text: "First item", TextStyle: tui.Style{Fg: app.theme.Fg}},
		{Icon: tui.IconBullet, Text: "Second item", TextStyle: tui.Style{Fg: app.theme.Fg}},
		{Icon: tui.IconBullet, Check: tui.CheckFull, CheckFg: app.theme.Selected, Text: "Selected item", TextStyle: tui.Style{Fg: app.theme.Selected}},
		{Icon: tui.IconBullet, Check: tui.CheckPartial, CheckFg: app.theme.Warning, Text: "Partial item", TextStyle: tui.Style{Fg: app.theme.Warning}},
		{Icon: tui.IconBullet, Text: "Fifth item", TextStyle: tui.Style{Fg: app.theme.Fg}},
		{Indent: 2, Icon: tui.IconBullet, Text: "Indented child", TextStyle: tui.Style{Fg: app.theme.HintFg}},
		{Indent: 2, Icon: tui.IconBullet, Text: "Another child", TextStyle: tui.Style{Fg: app.theme.HintFg}},
		{Icon: tui.IconBullet, Text: "Back to root level", TextStyle: tui.Style{Fg: app.theme.Fg}},
	}

	// Dialog
	app.confirmState = tui.NewConfirmState(false)

	// Toast
	app.toast = &tui.ToastState{}

	// Progress
	app.progress = tui.NewProgressState(tui.DefaultProgressOpts("Loading", "Processing files...", tui.ProgressDeterminate))
}

func (app *appState) rebuildTreeNodes() {
	builder := tui.NewTreeBuilder(app.treeExpansion)
	builder.Reset()

	// Root level
	builder.Add(tui.TreeNode{Key: "root", Label: "Project Root", Expandable: true, Depth: 0}, true)

	if app.treeExpansion.IsExpanded("root") {
		builder.Add(tui.TreeNode{Key: "src", Label: "src/", Expandable: true, Depth: 1, Style: tui.Style{Fg: app.theme.DirFg}}, true)

		if app.treeExpansion.IsExpanded("src") {
			builder.Add(tui.TreeNode{Key: "src/main.go", Label: "main.go", Depth: 2, Style: tui.Style{Fg: app.theme.FileFg}}, true)
			builder.Add(tui.TreeNode{Key: "src/util.go", Label: "util.go", Depth: 2, Style: tui.Style{Fg: app.theme.FileFg}}, true)
		}

		builder.Add(tui.TreeNode{Key: "pkg", Label: "pkg/", Expandable: true, Depth: 1, Style: tui.Style{Fg: app.theme.DirFg}}, true)

		if app.treeExpansion.IsExpanded("pkg") {
			builder.Add(tui.TreeNode{Key: "pkg/term", Label: "terminal/", Expandable: true, Depth: 2, Style: tui.Style{Fg: app.theme.DirFg}}, true)
			if app.treeExpansion.IsExpanded("pkg/term") {
				builder.Add(tui.TreeNode{Key: "pkg/term/input.go", Label: "input.go", Depth: 3, Style: tui.Style{Fg: app.theme.FileFg}}, true)
				builder.Add(tui.TreeNode{Key: "pkg/term/output.go", Label: "output.go", Depth: 3, Style: tui.Style{Fg: app.theme.FileFg}}, true)
			}
		}

		builder.Add(tui.TreeNode{Key: "docs", Label: "docs/", Expandable: true, Depth: 1, Style: tui.Style{Fg: app.theme.DirFg}}, true)
	}

	builder.MarkLastSiblings()
	app.treeNodes = builder.Nodes()
}

func (app *appState) updateAnimations() {
	// Update progress
	app.progressValue += 0.02
	if app.progressValue > 1.0 {
		app.progressValue = 0
	}
	app.progress.SetProgress(app.progressValue)
	app.progress.Tick()

	// Update toast
	if app.toast.Visible {
		app.toast.Tick()
	}
}

func (app *appState) render() {
	w, h := app.width, app.height
	cells := make([]terminal.Cell, w*h)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: app.theme.Fg, Bg: app.theme.Bg}
	}

	root := tui.NewRegion(cells, w, 0, 0, w, h)

	// Header with status bar
	header, body := tui.SplitVFixed(root, 1)
	app.renderStatusBar(header)

	// Main content with footer
	content, footer := tui.SplitVFixed(body, body.H-1)
	app.renderFooter(footer)

	// Render current view
	switch app.view {
	case ViewTextField:
		app.renderTextFieldDemo(content)
	case ViewEditor:
		app.renderEditorDemo(content)
	case ViewTree:
		app.renderTreeDemo(content)
	case ViewList:
		app.renderListDemo(content)
	case ViewDialog:
		app.renderDialogDemo(content)
	case ViewToast:
		app.renderToastDemo(content)
	case ViewProgress:
		app.renderProgressDemo(content)
	case ViewTable:
		app.renderTableDemo(content)
	}

	// Overlay: confirm dialog
	if app.showConfirm {
		app.confirmState.Result = tui.ConfirmPending
		root.ConfirmDialog(app.confirmState, tui.ConfirmOpts{
			Title:   "Confirm Action",
			Message: "Do you want to proceed with this operation?",
		})
	}

	// Overlay: toast
	if app.toast.Visible {
		root.Toast(app.toast.Opts)
	}

	app.term.Flush(cells, w, h)
}

func (app *appState) renderStatusBar(r tui.Region) {
	sections := []tui.BarSection{
		{Label: "View: ", Value: viewNames[app.view], LabelStyle: tui.Style{Fg: app.theme.HintFg}, ValueStyle: tui.Style{Fg: app.theme.HeaderFg}},
		{Label: "Frame: ", Value: fmt.Sprintf("%d", app.frame), LabelStyle: tui.Style{Fg: app.theme.HintFg}, ValueStyle: tui.Style{Fg: app.theme.HeaderFg}},
		{Label: "Size: ", Value: fmt.Sprintf("%dx%d", app.width, app.height), LabelStyle: tui.Style{Fg: app.theme.HintFg}, ValueStyle: tui.Style{Fg: app.theme.HeaderFg}},
	}

	r.Fill(app.theme.HeaderBg)
	r.StatusBar(0, sections, tui.BarOpts{
		Bg:    app.theme.HeaderBg,
		Align: tui.BarAlignRight,
	})

	// Title on left
	r.Text(1, 0, "TUI Components Demo", app.theme.HeaderFg, app.theme.HeaderBg, terminal.AttrBold)
}

func (app *appState) renderFooter(r tui.Region) {
	r.Fill(app.theme.HeaderBg)
	hint := "Tab: next view │ Ctrl+Q: quit │ View-specific keys shown in content"
	r.Text(1, 0, hint, app.theme.HintFg, app.theme.HeaderBg, terminal.AttrNone)
}

func (app *appState) renderTextFieldDemo(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title:    "TextField Component",
		Border:   tui.LineDouble,
		BorderFg: app.theme.Border,
		TitleFg:  app.theme.HeaderFg,
		Bg:       app.theme.Bg,
	})

	y := 1
	content.Text(1, y, "Single-line text input with cursor, scrolling, and word navigation", app.theme.HintFg, app.theme.Bg, terminal.AttrNone)
	y += 2

	// Basic text field
	content.Text(1, y, "Basic:", app.theme.Fg, app.theme.Bg, terminal.AttrBold)
	y++
	fieldRegion := content.Sub(1, y, content.W-2, 3)
	fieldRegion.TextField(app.textField, tui.TextFieldOpts{
		Border:  tui.LineSingle,
		Focused: true,
		Style:   tui.DefaultTextFieldStyle(),
	})
	y += 4

	// Search field with prefix
	content.Text(1, y, "Search (with prefix):", app.theme.Fg, app.theme.Bg, terminal.AttrBold)
	y++
	searchRegion := content.Sub(1, y, content.W-2, 3)
	searchRegion.TextField(app.searchField, tui.TextFieldOpts{
		Prefix:      "/ ",
		Placeholder: "Type to search...",
		Border:      tui.LineRounded,
		Focused:     false,
		Style:       tui.DefaultTextFieldStyle(),
	})
	y += 4

	// Key hints
	content.Text(1, y, "Keys: ←/→ move │ Ctrl+←/→ word │ Home/End │ Backspace/Del │ Ctrl+K kill", app.theme.HintFg, app.theme.Bg, terminal.AttrDim)
}

func (app *appState) renderEditorDemo(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title:    "MultiLine Editor",
		Border:   tui.LineDouble,
		BorderFg: app.theme.Border,
		TitleFg:  app.theme.HeaderFg,
		Bg:       app.theme.Bg,
	})

	// Editor takes most of the space
	editorH := content.H - 3
	if editorH < 5 {
		editorH = 5
	}

	editorRegion := content.Sub(1, 1, content.W-2, editorH)
	editorRegion.Editor(app.editor, tui.EditorOpts{
		LineNumbers: true,
		Border:      tui.LineSingle,
		Focused:     true,
		Style:       tui.DefaultEditorStyle(),
	})

	// Status line
	y := editorH + 2
	status := fmt.Sprintf("Line %d, Col %d │ %d lines total", app.editor.CursorLine+1, app.editor.CursorCol+1, len(app.editor.Lines))
	content.Text(1, y, status, app.theme.HintFg, app.theme.Bg, terminal.AttrNone)
}

func (app *appState) renderTreeDemo(r tui.Region) {
	panes := tui.SplitH(r, 0.5, 0.5)

	// Tree pane
	treeContent := panes[0].Pane(tui.PaneOpts{
		Title:    "Tree Component",
		Border:   tui.LineDouble,
		BorderFg: app.theme.Border,
		TitleFg:  app.theme.HeaderFg,
		Bg:       app.theme.Bg,
	})

	app.treeState.SetVisible(treeContent.H - 2)
	treeRegion := treeContent.Sub(1, 1, treeContent.W-2, treeContent.H-2)
	treeRegion.Tree(app.treeNodes, app.treeState.Cursor, app.treeState.Scroll, tui.TreeOpts{
		CursorBg:  app.theme.CursorBg,
		DefaultBg: app.theme.Bg,
		LineMode:  tui.TreeLinesSimple,
		LineFg:    app.theme.Border,
	})

	// Info pane
	infoContent := panes[1].Pane(tui.PaneOpts{
		Title:    "Selected Node",
		Border:   tui.LineSingle,
		BorderFg: app.theme.Border,
		TitleFg:  app.theme.HeaderFg,
		Bg:       app.theme.Bg,
	})

	y := 1
	if app.treeState.Cursor < len(app.treeNodes) {
		node := app.treeNodes[app.treeState.Cursor]
		infoContent.KeyValue(y, "Key", node.Key, tui.Style{Fg: app.theme.HintFg}, tui.Style{Fg: app.theme.Fg}, ':')
		y++
		infoContent.KeyValue(y, "Label", node.Label, tui.Style{Fg: app.theme.HintFg}, tui.Style{Fg: app.theme.Fg}, ':')
		y++
		infoContent.KeyValue(y, "Depth", fmt.Sprintf("%d", node.Depth), tui.Style{Fg: app.theme.HintFg}, tui.Style{Fg: app.theme.Fg}, ':')
		y++
		expandable := "No"
		if node.Expandable {
			expandable = "Yes"
		}
		infoContent.KeyValue(y, "Expandable", expandable, tui.Style{Fg: app.theme.HintFg}, tui.Style{Fg: app.theme.Fg}, ':')
	}

	y = infoContent.H - 2
	infoContent.Text(1, y, "j/k: move │ h/l: collapse/expand", app.theme.HintFg, app.theme.Bg, terminal.AttrDim)
}

func (app *appState) renderListDemo(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title:    "List Component",
		Border:   tui.LineDouble,
		BorderFg: app.theme.Border,
		TitleFg:  app.theme.HeaderFg,
		Bg:       app.theme.Bg,
	})

	listRegion := content.Sub(1, 1, content.W-2, content.H-4)
	listRegion.List(app.listItems, app.listCursor, app.listScroll, tui.ListOpts{
		CursorBg:  app.theme.CursorBg,
		DefaultBg: app.theme.Bg,
	})

	// Scroll bar
	tui.ScrollBar(content.Sub(content.W-2, 1, 1, content.H-4), 0, app.listScroll, content.H-4, len(app.listItems), app.theme.Border)

	y := content.H - 2
	content.Text(1, y, "j/k: move │ Space: toggle │ Shows icons, checkboxes, indentation", app.theme.HintFg, app.theme.Bg, terminal.AttrDim)
}

func (app *appState) renderDialogDemo(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title:    "Dialog Components",
		Border:   tui.LineDouble,
		BorderFg: app.theme.Border,
		TitleFg:  app.theme.HeaderFg,
		Bg:       app.theme.Bg,
	})

	y := 2
	content.Text(2, y, "Press 'c' to show Confirm dialog", app.theme.Fg, app.theme.Bg, terminal.AttrNone)
	y += 2

	if app.dialogResult != "" {
		content.Text(2, y, "Last result: "+app.dialogResult, app.theme.Selected, app.theme.Bg, terminal.AttrBold)
		y += 2
	}

	// Show alert example inline
	y += 2
	alertRegion := content.Sub(2, y, 40, 6)
	alertRegion.AlertDialog(tui.AlertOpts{
		Title:   "Information",
		Message: "This is an inline alert example",
		Button:  "OK",
	})
}

func (app *appState) renderToastDemo(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title:    "Toast Notifications",
		Border:   tui.LineDouble,
		BorderFg: app.theme.Border,
		TitleFg:  app.theme.HeaderFg,
		Bg:       app.theme.Bg,
	})

	y := 2
	content.Text(2, y, "Press 1-4 to show different toast types:", app.theme.Fg, app.theme.Bg, terminal.AttrNone)
	y += 2
	content.Text(4, y, "1 = Info", app.theme.HintFg, app.theme.Bg, terminal.AttrNone)
	y++
	content.Text(4, y, "2 = Success", app.theme.Selected, app.theme.Bg, terminal.AttrNone)
	y++
	content.Text(4, y, "3 = Warning", app.theme.Warning, app.theme.Bg, terminal.AttrNone)
	y++
	content.Text(4, y, "4 = Error", app.theme.Error, app.theme.Bg, terminal.AttrNone)
	y += 2

	content.Text(2, y, fmt.Sprintf("Toasts shown: %d", app.toastCount), app.theme.HintFg, app.theme.Bg, terminal.AttrNone)
}

func (app *appState) renderProgressDemo(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title:    "Progress Components",
		Border:   tui.LineDouble,
		BorderFg: app.theme.Border,
		TitleFg:  app.theme.HeaderFg,
		Bg:       app.theme.Bg,
	})

	y := 2

	// Determinate progress bars
	content.Text(2, y, "Determinate Progress:", app.theme.Fg, app.theme.Bg, terminal.AttrBold)
	y += 2
	content.Progress(4, y, 30, app.progressValue, app.theme.Selected, app.theme.Bg)
	content.Text(36, y, fmt.Sprintf("%.0f%%", app.progressValue*100), app.theme.Fg, app.theme.Bg, terminal.AttrNone)
	y += 2

	// Gauge
	content.Text(2, y, "Gauge:", app.theme.Fg, app.theme.Bg, terminal.AttrBold)
	y++
	content.Gauge(4, y, 40, int(app.progressValue*100), 100, app.theme.DirFg, app.theme.Bg)
	y += 2

	// Spinner
	content.Text(2, y, "Spinner:", app.theme.Fg, app.theme.Bg, terminal.AttrBold)
	content.Spinner(12, y, app.frame, app.theme.SymbolFg)
	y += 2

	// Sparkline
	content.Text(2, y, "Sparkline:", app.theme.Fg, app.theme.Bg, terminal.AttrBold)
	y++
	values := make([]float64, 20)
	for i := range values {
		values[i] = float64((app.frame+i*7)%100) / 100.0
	}
	content.Sub(4, y, 20, 1).Sparkline(0, 0, 20, values, tui.SparklineOpts{Style: tui.Style{Fg: app.theme.Warning}})
	y += 2

	// Progress overlay hint
	content.Text(2, y, "Press 'p' to show progress overlay", app.theme.HintFg, app.theme.Bg, terminal.AttrDim)
}

func (app *appState) renderTableDemo(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title:    "Table Component",
		Border:   tui.LineDouble,
		BorderFg: app.theme.Border,
		TitleFg:  app.theme.HeaderFg,
		Bg:       app.theme.Bg,
	})

	headers := []string{"Name", "Type", "Size", "Modified"}
	rows := [][]string{
		{"main.go", "Go Source", "2.4 KB", "2025-01-15"},
		{"README.md", "Markdown", "1.1 KB", "2025-01-14"},
		{"go.mod", "Go Module", "256 B", "2025-01-10"},
		{"Makefile", "Makefile", "512 B", "2025-01-08"},
		{"config.yaml", "YAML", "1.8 KB", "2025-01-12"},
	}

	tableRegion := content.Sub(2, 2, content.W-4, content.H-4)
	tableRegion.Table(headers, rows, tui.TableOpts{
		HeaderStyle:  tui.Style{Fg: app.theme.HeaderFg, Attr: terminal.AttrBold},
		RowStyle:     tui.Style{Fg: app.theme.Fg},
		AltRowStyle:  tui.Style{Fg: app.theme.Fg, Bg: app.theme.FocusBg},
		ColAligns:    []tui.Align{tui.AlignLeft, tui.AlignLeft, tui.AlignRight, tui.AlignRight},
		RowSeparator: tui.LineSingle,
	})
}

func (app *appState) handleEvent(ev terminal.Event) {
	switch ev.Type {
	case terminal.EventResize:
		app.width = ev.Width
		app.height = ev.Height
		return
	case terminal.EventClosed:
		app.quit = true
		return
	}

	if ev.Key == terminal.KeyCtrlQ || ev.Key == terminal.KeyCtrlC {
		app.quit = true
		return
	}

	// Handle confirm dialog if showing
	if app.showConfirm {
		if app.confirmState.HandleKey(ev.Key, ev.Rune) {
			app.showConfirm = false
			switch app.confirmState.Result {
			case tui.ConfirmYes:
				app.dialogResult = "Confirmed: Yes"
			case tui.ConfirmNo:
				app.dialogResult = "Confirmed: No"
			case tui.ConfirmCancel:
				app.dialogResult = "Cancelled"
			}
		}
		return
	}

	// Tab to switch views
	if ev.Key == terminal.KeyTab {
		app.view = (app.view + 1) % ViewCount
		return
	}

	// View-specific handling
	switch app.view {
	case ViewTextField:
		app.textField.HandleKey(ev.Key, ev.Rune, ev.Modifiers)

	case ViewEditor:
		app.editor.HandleKey(ev.Key, ev.Rune, ev.Modifiers)

	case ViewTree:
		app.handleTreeEvent(ev)

	case ViewList:
		app.handleListEvent(ev)

	case ViewDialog:
		if ev.Key == terminal.KeyRune && ev.Rune == 'c' {
			app.showConfirm = true
			app.confirmState = tui.NewConfirmState(false)
		}

	case ViewToast:
		app.handleToastEvent(ev)

	case ViewProgress:
		if ev.Key == terminal.KeyRune && ev.Rune == 'p' {
			// Toggle progress overlay demo would go here
		}
	}
}

func (app *appState) handleTreeEvent(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyUp, terminal.KeyRune:
		if ev.Key == terminal.KeyUp || ev.Rune == 'k' {
			app.treeState.MoveCursor(-1, len(app.treeNodes))
		}
		if ev.Rune == 'j' {
			app.treeState.MoveCursor(1, len(app.treeNodes))
		}
		if ev.Rune == 'h' {
			// Collapse
			if app.treeState.Cursor < len(app.treeNodes) {
				node := app.treeNodes[app.treeState.Cursor]
				if node.Expandable && app.treeExpansion.IsExpanded(node.Key) {
					app.treeExpansion.Collapse(node.Key)
					app.rebuildTreeNodes()
				}
			}
		}
		if ev.Rune == 'l' {
			// Expand
			if app.treeState.Cursor < len(app.treeNodes) {
				node := app.treeNodes[app.treeState.Cursor]
				if node.Expandable && !app.treeExpansion.IsExpanded(node.Key) {
					app.treeExpansion.Expand(node.Key)
					app.rebuildTreeNodes()
				}
			}
		}
	case terminal.KeyDown:
		app.treeState.MoveCursor(1, len(app.treeNodes))
	case terminal.KeyLeft:
		if app.treeState.Cursor < len(app.treeNodes) {
			node := app.treeNodes[app.treeState.Cursor]
			if node.Expandable && app.treeExpansion.IsExpanded(node.Key) {
				app.treeExpansion.Collapse(node.Key)
				app.rebuildTreeNodes()
			}
		}
	case terminal.KeyRight:
		if app.treeState.Cursor < len(app.treeNodes) {
			node := app.treeNodes[app.treeState.Cursor]
			if node.Expandable && !app.treeExpansion.IsExpanded(node.Key) {
				app.treeExpansion.Expand(node.Key)
				app.rebuildTreeNodes()
			}
		}
	}
}

func (app *appState) handleListEvent(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyUp:
		if app.listCursor > 0 {
			app.listCursor--
		}
	case terminal.KeyDown:
		if app.listCursor < len(app.listItems)-1 {
			app.listCursor++
		}
	case terminal.KeyRune:
		if ev.Rune == 'j' && app.listCursor < len(app.listItems)-1 {
			app.listCursor++
		}
		if ev.Rune == 'k' && app.listCursor > 0 {
			app.listCursor--
		}
		if ev.Rune == ' ' {
			// Toggle checkbox
			item := &app.listItems[app.listCursor]
			switch item.Check {
			case tui.CheckNone:
				item.Check = tui.CheckFull
				item.CheckFg = app.theme.Selected
			case tui.CheckFull:
				item.Check = tui.CheckNone
				item.CheckFg = terminal.RGB{}
			case tui.CheckPartial:
				item.Check = tui.CheckFull
				item.CheckFg = app.theme.Selected
			}
		}
	}

	// Adjust scroll
	visible := app.height - 10
	if visible < 1 {
		visible = 1
	}
	app.listScroll = tui.AdjustScroll(app.listCursor, app.listScroll, visible, len(app.listItems))
}

func (app *appState) handleToastEvent(ev terminal.Event) {
	if ev.Key != terminal.KeyRune {
		return
	}

	var severity tui.ToastSeverity
	var msg string

	switch ev.Rune {
	case '1':
		severity = tui.ToastInfo
		msg = "This is an informational message"
	case '2':
		severity = tui.ToastSuccess
		msg = "Operation completed successfully!"
	case '3':
		severity = tui.ToastWarning
		msg = "Warning: Please review before continuing"
	case '4':
		severity = tui.ToastError
		msg = "Error: Something went wrong"
	default:
		return
	}

	app.toastCount++
	app.toast.Show(tui.DefaultToastOpts(msg, severity), 30) // 3 seconds at 10fps
}