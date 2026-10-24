package main

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"
	"text/template"
)

const eventPkgPath = "github.com/lixenwraith/vif/internal/event"

// PayloadCodec is one struct's generated field table.
type PayloadCodec struct {
	Name    string   // variable suffix, qualified for a struct of another package
	Type    string   // the type as the event package writes it
	Payload bool     // registered as an event payload, so it carries the codec method
	Fields  []string // one field literal each, in declaration order
}

// PayloadDefs is the template input for event/payload_gen.go.
type PayloadDefs struct {
	Codecs  []PayloadCodec
	Events  []EventDef
	Imports []string
}

// payloadCodecs type-checks the event package and renders a field table for every
// registered payload struct and every struct reachable from one.
func payloadCodecs(events EventDefs) PayloadDefs {
	imp := importer.ForCompiler(token.NewFileSet(), "source", nil)
	pkg, err := imp.(types.ImporterFrom).ImportFrom(eventPkgPath, ".", 0)
	if err != nil {
		panic(fmt.Sprintf("type-check %s: %v", eventPkgPath, err))
	}
	g := &payloadGen{imports: map[string]string{}, seen: map[string]int{}}
	var withPayload []EventDef
	for _, e := range events.Events {
		if e.Payload == "" {
			continue
		}
		obj := pkg.Scope().Lookup(e.Payload)
		if obj == nil {
			panic(fmt.Sprintf("payload type %s not found", e.Payload))
		}
		n, ok := obj.Type().(*types.Named)
		if !ok || !isStruct(n) {
			panic(fmt.Sprintf("payload %s is not a struct", e.Payload))
		}
		g.codec(n, true)
		withPayload = append(withPayload, e)
	}
	imports := make([]string, 0, len(g.imports))
	for path := range g.imports {
		imports = append(imports, path)
	}
	sort.Strings(imports)
	return PayloadDefs{Codecs: g.codecs, Events: withPayload, Imports: imports}
}

type payloadGen struct {
	codecs  []PayloadCodec
	seen    map[string]int // type string to codec index, -1 while in progress
	imports map[string]string
}

func isStruct(n *types.Named) bool {
	_, ok := n.Underlying().(*types.Struct)
	return ok
}

// typeString writes a type as the event package names it and notes the import.
func (g *payloadGen) typeString(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p.Path() == eventPkgPath {
			return ""
		}
		g.imports[p.Path()] = p.Name()
		return p.Name()
	})
}

// codecName is a struct's table name: its own, qualified by its package's for one
// from outside the event package.
func (g *payloadGen) codecName(n *types.Named) string {
	if n.Obj().Pkg().Path() == eventPkgPath {
		return "codec" + n.Obj().Name()
	}
	p := n.Obj().Pkg().Name()
	return "codec" + strings.ToUpper(p[:1]) + p[1:] + n.Obj().Name()
}

// codec renders n's table once, nested structs first so the file reads bottom-up.
func (g *payloadGen) codec(n *types.Named, payload bool) string {
	name := g.codecName(n)
	key := g.typeString(n)
	if i, ok := g.seen[key]; ok {
		if i >= 0 && payload {
			g.codecs[i].Payload = true
		}
		return name
	}
	g.seen[key] = -1
	st := n.Underlying().(*types.Struct)
	var fields []string
	for i := range st.NumFields() {
		f := st.Field(i)
		if !f.Exported() || reflect.StructTag(st.Tag(i)).Get("toml") == "-" {
			continue // never encoded, as the text codec never did
		}
		where := key + "." + f.Name()
		fields = append(fields, "{off: unsafe.Offsetof("+key+"{}."+f.Name()+"), "+g.desc(f.Type(), where)+"}")
	}
	g.codecs = append(g.codecs, PayloadCodec{Name: name, Type: key, Payload: payload, Fields: fields})
	g.seen[key] = len(g.codecs) - 1
	return name
}

// kindOf classifies a type: b bool, i signed, u unsigned, f float64, s string,
// S slice, A array, p pointer to struct, T struct.
func kindOf(t types.Type) byte {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		info := u.Info()
		switch {
		case info&types.IsBoolean != 0:
			return 'b'
		case info&types.IsUnsigned != 0:
			return 'u'
		case info&types.IsInteger != 0:
			return 'i'
		case u.Kind() == types.Float64:
			return 'f'
		case info&types.IsString != 0:
			return 's'
		}
	case *types.Slice:
		return 'S'
	case *types.Array:
		return 'A'
	case *types.Pointer:
		if n, ok := u.Elem().(*types.Named); ok && isStruct(n) {
			return 'p'
		}
	case *types.Struct:
		if _, ok := t.(*types.Named); ok {
			return 'T'
		}
	}
	return 0
}

// desc renders the kind part of a field literal for a value of type t.
func (g *payloadGen) desc(t types.Type, where string) string {
	size := func() string { return "size: unsafe.Sizeof(" + g.typeString(t) + "(0))" }
	switch kindOf(t) {
	case 'b':
		return "kind: kindBool"
	case 'i':
		return "kind: kindInt, " + size()
	case 'u':
		return "kind: kindUint, " + size()
	case 'f':
		return "kind: kindFloat"
	case 's':
		return "kind: kindString"
	case 'S':
		elem := t.Underlying().(*types.Slice).Elem()
		ts := g.typeString(t)
		return "kind: kindSlice, " + g.elem(elem, where) +
			", make: func(p unsafe.Pointer, n int) { *(*" + ts + ")(p) = make(" + ts + ", n) }"
	case 'A':
		a := t.Underlying().(*types.Array)
		if k := kindOf(a.Elem()); k == 'S' || k == 'p' || k == 'T' || k == 's' {
			panic(fmt.Sprintf("%s: an array of %s has no byte-wise zero test", where, a.Elem()))
		}
		return fmt.Sprintf("kind: kindArray, n: %d, %s", a.Len(), g.elem(a.Elem(), where))
	case 'p':
		n := t.Underlying().(*types.Pointer).Elem().(*types.Named)
		ts := g.typeString(n)
		return "kind: kindPtr, sub: &" + g.codec(n, false) +
			", new: func(p unsafe.Pointer) { *(**" + ts + ")(p) = new(" + ts + ") }"
	case 'T':
		return "kind: kindStruct, sub: &" + g.codec(t.(*types.Named), false)
	}
	panic(fmt.Sprintf("%s: %s has no codec", where, t))
}

// elem renders a slice's or array's element: its descriptor and its stride.
func (g *payloadGen) elem(elem types.Type, where string) string {
	if k := kindOf(elem); k == 'b' || k == 0 {
		panic(fmt.Sprintf("%s: elements of %s have no codec", where, elem))
	}
	stride := "unsafe.Sizeof(" + g.typeString(elem) + "{})"
	if kindOf(elem) != 'T' {
		stride = "unsafe.Sizeof(" + g.typeString(elem) + "(0))"
	}
	if kindOf(elem) == 's' {
		stride = `unsafe.Sizeof("")`
	}
	return "elem: &field{" + g.desc(elem, where+"[]") + "}, stride: " + stride
}

var payloadTemplate = template.Must(template.New("payload").Parse(`// Code generated by gen-manifest; DO NOT EDIT.

package event

import (
	"unsafe"
{{- range .Imports }}
	"{{ . }}"
{{- end }}
)

// payloadNew allocates each event's registered payload.
var payloadNew = [EventTypeCount]func() any{
{{- range .Events }}
	{{ .Name }}: func() any { return new({{ .Payload }}) },
{{- end }}
}

// payloadCodecs is each event's payload table.
var payloadCodecs = [EventTypeCount]*structCodec{
{{- range .Events }}
	{{ .Name }}: &codec{{ .Payload }},
{{- end }}
}
{{ range .Codecs }}
{{- if .Payload }}
func (p *{{ .Type }}) codec() (*structCodec, unsafe.Pointer) { return &{{ .Name }}, unsafe.Pointer(p) }
{{ end }}
var {{ .Name }} = structCodec{size: unsafe.Sizeof({{ .Type }}{}), fields: []field{
{{- range .Fields }}
	{{ . }},
{{- end }}
}}
{{ end }}
`))
