package mode

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
)

// MenuOp is what a host drawing the menu itself asks of a node, by its id: set a
// toggle or a choice to Arg, step one by ±1, activate an action or a pick, submit a
// form's fields as a JSON array, search the menu for Arg, or close it
type MenuOp struct {
	Op, ID, Arg string
}

// The contract a host's menu is written to: the node kinds, by nodeKind, the ops and
// the field inputs. A test holds each host's source to these.
var (
	MenuKinds  = [...]string{"group", "toggle", "choice", "step", "pick", "action", "form", "info"}
	MenuOps    = []string{"set", "step", "activate", "submit", "search", "close"}
	MenuInputs = []string{inputText, inputNumber, inputURI}
)

// toggleChoices is what a toggle without choices of its own reads and takes
var toggleChoices = []string{"off", "on"}

var statusKindWords = [parameter.StatusKindCount]string{"notice", "progress", "alert"}

// menuSnapshot is the menu as a host draws it: the tree with each row's live label,
// value and reason, the status line, a failed submit's error and a search's matches
type menuSnapshot struct {
	Title  string      `json:"title"`
	Status *menuStatus `json:"status,omitempty"`
	Form   *menuError  `json:"form,omitempty"`
	Query  string      `json:"query,omitempty"`
	Found  []menuFound `json:"found,omitempty"`
	Nodes  []menuNode  `json:"nodes"`
}

type menuStatus struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

type menuError struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type menuFound struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type menuNode struct {
	ID          string      `json:"id"`
	Kind        string      `json:"kind"`
	Label       string      `json:"label"`
	Description string      `json:"description,omitempty"`
	Value       string      `json:"value,omitempty"`
	Choices     []string    `json:"choices,omitempty"`
	Marked      bool        `json:"marked,omitempty"`
	Confirm     bool        `json:"confirm,omitempty"`
	Disabled    string      `json:"disabled,omitempty"`
	Fields      []menuField `json:"fields,omitempty"`
	Nodes       []menuNode  `json:"nodes,omitempty"`
}

type menuField struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Input string `json:"input"`
	Help  string `json:"help,omitempty"`
}

// MenuSnapshot is the menu as a host draws it, nil while it is closed. Built under
// the world lock; the caller pushes it when its bytes changed.
func (r *Router) MenuSnapshot() []byte {
	if configMenu(r.ctx) == nil {
		return nil
	}
	s := menuSnapshot{Title: configMenuTitle(r.ctx, ""), Nodes: menuNodes(r.ctx, "", configPages)}
	if text, kind := r.ctx.ShownStatus(r.ctx.TimeCtl.RealTime().UnixNano()); text != "" {
		s.Status = &menuStatus{Text: text, Kind: statusKindWords[kind]}
	}
	if f := r.configForm; f != nil && f.err != "" {
		s.Form = &menuError{ID: f.row, Error: f.err}
	}
	if terms := strings.Fields(strings.ToLower(r.ctx.OverlayFilter())); len(terms) > 0 {
		s.Query = r.ctx.OverlayFilter()
		for _, row := range menuMatches(r.ctx, terms) {
			s.Found = append(s.Found, menuFound{ID: row.Key, Label: row.Label})
		}
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return b
}

// menuNodes is the tree under prefix as the snapshot carries it: a value for the
// rows a widget shows one, a toggle's two words, a form's fields, and the reason a
// row is disabled, which a read-only row's kind already says
func menuNodes(ctx *engine.GameContext, prefix string, nodes []configNode) []menuNode {
	out := make([]menuNode, 0, len(nodes))
	for _, n := range nodes {
		id := nodeID(prefix, n.key)
		node := menuNode{ID: id, Kind: MenuKinds[n.kind], Label: n.label, Description: n.description, Marked: n.marked, Confirm: n.confirm}
		if n.name != nil {
			node.Label = n.name(ctx)
		}
		switch n.kind {
		case kindGroup:
			node.Nodes = menuNodes(ctx, id, n.children(ctx))
			out = append(out, node)
			continue
		case kindToggle:
			node.Value, node.Choices = n.read(ctx), n.choices
			if node.Choices == nil {
				node.Choices = toggleChoices
			}
		case kindChoice, kindStep, kindPick:
			node.Value, node.Choices = n.read(ctx), n.choices
		case kindForm:
			for _, f := range configForms[n.form].fields {
				node.Fields = append(node.Fields, menuField{Label: f.label, Value: f.value, Input: f.input, Help: f.help})
			}
		}
		if n.kind != kindInfo {
			node.Disabled = n.unavailable(ctx)
		}
		out = append(out, node)
	}
	return out
}

// MenuOp applies what a host's menu asked, through the function the key path calls
// once it has the selection: an op on an id that no longer exists is dropped, one on
// a disabled node answers with its reason on the status line, and any op drops the
// error a failed submit left standing
func (r *Router) MenuOp(op MenuOp) {
	m := configMenu(r.ctx)
	if m == nil {
		return
	}
	r.configForm = nil
	switch op.Op {
	case "search":
		r.setOverlayFilter(op.Arg)
		return
	case "close":
		r.closeOverlay()
		return
	}
	n, ok := configNodeFor(r.ctx, op.ID)
	if !ok {
		return
	}
	switch op.Op {
	case "set":
		if n.kind != kindToggle && n.kind != kindChoice {
			return
		}
		choices := n.choices
		if choices == nil {
			choices = toggleChoices
		}
		j, current := slices.Index(choices, op.Arg), n.read(r.ctx)
		if j < 0 || op.Arg == current {
			return
		}
		d := 1 // a toggle without choices turns over whichever way
		if n.choices != nil {
			d = j - slices.Index(n.choices, current)
		}
		r.applyConfigNode(op.ID, n, d, false)
	case "step":
		if d, err := strconv.Atoi(op.Arg); err == nil && n.kind == kindStep && (d == 1 || d == -1) {
			r.applyConfigNode(op.ID, n, d, false)
		}
	case "activate":
		if n.kind == kindAction || n.kind == kindPick {
			r.applyConfigNode(op.ID, n, 1, true)
		}
	case "submit":
		var values []string
		if n.kind != kindForm || json.Unmarshal([]byte(op.Arg), &values) != nil {
			return
		}
		spec := configForms[n.form]
		state := tui.NewFormState()
		for i, field := range spec.fields {
			value := field.value
			if i < len(values) {
				value = values[i]
			}
			state.Fields = append(state.Fields, tui.FormField{Label: field.label, State: tui.NewTextFieldState(value)})
		}
		f := &configForm{node: n, spec: spec, state: state, page: m.Page, row: op.ID}
		if !r.submitConfigForm(f) {
			r.configForm = f
		}
	}
}
