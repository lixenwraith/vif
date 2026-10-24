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

// PayloadCodec is one struct's generated encoder and decoder: a presence mask,
// then each present field in declaration order.
type PayloadCodec struct {
	Name    string // function suffix, qualified for a struct of another package
	Type    string // the type as the event package writes it
	Payload bool   // registered as an event payload, so it carries the codec methods
	Mask    int    // mask bytes
	Fields  []PayloadField
}

// PayloadField is one field's three generated parts.
type PayloadField struct {
	Present string // sets the field's mask bit when the field is not zero
	Encode  string // appends the field; empty for a bool, whose bit is its value
	Decode  string // reads the field when its bit is set
}

// PayloadDefs is the template input for event/payload_gen.go.
type PayloadDefs struct {
	Codecs  []PayloadCodec
	Imports []string
	Schema  string
	Math    bool
}

// payloadCodecs type-checks the event package and renders a codec for every
// registered payload struct and every struct reachable from one.
func payloadCodecs(events EventDefs) PayloadDefs {
	imp := importer.ForCompiler(token.NewFileSet(), "source", nil)
	pkg, err := imp.(types.ImporterFrom).ImportFrom(eventPkgPath, ".", 0)
	if err != nil {
		panic(fmt.Sprintf("type-check %s: %v", eventPkgPath, err))
	}
	g := &payloadGen{imports: map[string]string{}, seen: map[string]int{}}
	var schema []string
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
		schema = append(schema, e.Name+"="+g.typeString(n))
	}
	for _, c := range g.codecs {
		schema = append(schema, c.schema)
	}
	imports := make([]string, 0, len(g.imports))
	for path := range g.imports {
		imports = append(imports, path)
	}
	sort.Strings(imports)
	out := PayloadDefs{Imports: imports, Schema: strings.Join(schema, ";"), Math: g.math}
	for _, c := range g.codecs {
		out.Codecs = append(out.Codecs, c.PayloadCodec)
	}
	return out
}

type payloadGen struct {
	codecs  []payloadCodec
	seen    map[string]int // type string to codec index, -1 while in progress
	imports map[string]string
	math    bool
	depth   int
}

type payloadCodec struct {
	PayloadCodec
	schema string
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

// codecName is the function suffix for a struct: its name, qualified by its
// package's for one from outside the event package.
func (g *payloadGen) codecName(n *types.Named) string {
	if n.Obj().Pkg().Path() == eventPkgPath {
		return n.Obj().Name()
	}
	p := n.Obj().Pkg().Name()
	return strings.ToUpper(p[:1]) + p[1:] + n.Obj().Name()
}

// codec renders n's codec once, nested structs first so the file reads bottom-up.
func (g *payloadGen) codec(n *types.Named, payload bool) string {
	name := g.codecName(n)
	key := g.typeString(n)
	if i, ok := g.seen[key]; ok {
		if i >= 0 && payload && !g.codecs[i].Payload {
			g.codecs[i].Payload = true
		}
		return name
	}
	g.seen[key] = -1
	st := n.Underlying().(*types.Struct)
	var fields []PayloadField
	var keys []string
	bit := 0
	for i := range st.NumFields() {
		f := st.Field(i)
		tag := reflect.StructTag(st.Tag(i)).Get("toml")
		if !f.Exported() || tag == "-" {
			continue // never encoded, as the text codec never did
		}
		where := key + "." + f.Name()
		expr := "p." + f.Name()
		mask := fmt.Sprintf("m[%d]&(1<<%d) != 0", bit/8, bit%8)
		set := fmt.Sprintf("m[%d] |= 1 << %d", bit/8, bit%8)
		pf := PayloadField{}
		switch k := kindOf(f.Type()); k {
		case 'b':
			pf.Present = "if " + expr + " {\n" + set + "\n}"
			pf.Decode = expr + " = " + mask
		case 'T':
			pf.Present = set
			pf.Encode = "if " + mask + " {\n" + g.encode(f.Type(), expr, where) + "\n}"
			pf.Decode = "if " + mask + " {\n" + g.decode(f.Type(), expr, where) + "\n}"
		default:
			pf.Present = "if " + g.present(f.Type(), expr, where) + " {\n" + set + "\n}"
			pf.Encode = "if " + mask + " {\n" + g.encode(f.Type(), expr, where) + "\n}"
			pf.Decode = "if " + mask + " {\n" + g.decode(f.Type(), expr, where) + "\n}"
		}
		fields = append(fields, pf)
		keyName := f.Name()
		if t, _, _ := strings.Cut(tag, ","); t != "" {
			keyName = t
		}
		keys = append(keys, keyName+":"+g.schemaKind(f.Type(), where))
		bit++
	}
	g.codecs = append(g.codecs, payloadCodec{
		PayloadCodec: PayloadCodec{Name: name, Type: key, Payload: payload, Mask: (bit + 7) / 8, Fields: fields},
		schema:       key + "{" + strings.Join(keys, ",") + "}",
	})
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

func (g *payloadGen) present(t types.Type, expr, where string) string {
	switch kindOf(t) {
	case 'i', 'u':
		return expr + " != 0"
	case 'f':
		g.math = true
		return "math.Float64bits(float64(" + expr + ")) != 0"
	case 's':
		return expr + ` != ""`
	case 'S':
		return "len(" + expr + ") != 0"
	case 'A':
		if k := kindOf(t.Underlying().(*types.Array).Elem()); k == 0 || k == 'S' || k == 'p' || k == 'T' {
			panic(fmt.Sprintf("%s: an array of %s has no zero test", where, t))
		}
		return expr + " != (" + g.typeString(t) + "{})"
	case 'p':
		return expr + " != nil"
	}
	panic(fmt.Sprintf("%s: %s has no codec", where, t))
}

// encode renders the statements appending expr, a value of type t.
func (g *payloadGen) encode(t types.Type, expr, where string) string {
	switch kindOf(t) {
	case 'i':
		return "b = binary.AppendVarint(b, int64(" + expr + "))"
	case 'u':
		return "b = binary.AppendUvarint(b, uint64(" + expr + "))"
	case 'f':
		g.math = true
		return "b = binary.LittleEndian.AppendUint64(b, math.Float64bits(float64(" + expr + ")))"
	case 's':
		return "b = binary.AppendUvarint(b, uint64(len(" + expr + ")))\nb = append(b, " + expr + "...)"
	case 'S':
		return "b = binary.AppendUvarint(b, uint64(len(" + expr + ")))\n" + g.loop(t.Underlying().(*types.Slice).Elem(), expr, where, g.encode)
	case 'A':
		return g.loop(t.Underlying().(*types.Array).Elem(), expr, where, g.encode)
	case 'p':
		return "b = append" + g.codec(t.Underlying().(*types.Pointer).Elem().(*types.Named), false) + "(b, " + expr + ")"
	case 'T':
		return "b = append" + g.codec(t.(*types.Named), false) + "(b, &" + expr + ")"
	}
	panic(fmt.Sprintf("%s: %s has no codec", where, t))
}

// decode renders the statements reading expr, a settable value of type t.
func (g *payloadGen) decode(t types.Type, expr, where string) string {
	switch kindOf(t) {
	case 'i':
		return expr + " = " + g.typeString(t) + "(r.varint())"
	case 'u':
		return expr + " = " + g.typeString(t) + "(r.uvarint())"
	case 'f':
		g.math = true
		return expr + " = " + g.typeString(t) + "(math.Float64frombits(r.u64()))"
	case 's':
		return expr + " = " + g.typeString(t) + "(r.bytes(r.uvarint()))"
	case 'S':
		return expr + " = make(" + g.typeString(t) + ", r.length())\n" + g.loop(t.Underlying().(*types.Slice).Elem(), expr, where, g.decode)
	case 'A':
		return g.loop(t.Underlying().(*types.Array).Elem(), expr, where, g.decode)
	case 'p':
		n := t.Underlying().(*types.Pointer).Elem().(*types.Named)
		return expr + " = new(" + g.typeString(n) + ")\nread" + g.codec(n, false) + "(r, " + expr + ")"
	case 'T':
		return "read" + g.codec(t.(*types.Named), false) + "(r, &" + expr + ")"
	}
	panic(fmt.Sprintf("%s: %s has no codec", where, t))
}

// loop renders one statement per element of expr; an element has no mask, so a
// bool element has no encoding.
func (g *payloadGen) loop(elem types.Type, expr, where string, each func(types.Type, string, string) string) string {
	if k := kindOf(elem); k == 'b' || k == 0 {
		panic(fmt.Sprintf("%s: elements of %s have no codec", where, elem))
	}
	i := fmt.Sprintf("i%d", g.depth)
	g.depth++
	body := each(elem, expr+"["+i+"]", where+"[]")
	g.depth--
	return "for " + i + " := range " + expr + " {\n" + body + "\n}"
}

// schemaKind is a type's kind as the anchor's schema spells it.
func (g *payloadGen) schemaKind(t types.Type, where string) string {
	switch k := kindOf(t); k {
	case 'b', 'i', 'u', 'f', 's':
		return string(k)
	case 'S':
		return "[]" + g.schemaKind(t.Underlying().(*types.Slice).Elem(), where)
	case 'A':
		a := t.Underlying().(*types.Array)
		return fmt.Sprintf("[%d]%s", a.Len(), g.schemaKind(a.Elem(), where))
	case 'p':
		return "*" + g.typeString(t.Underlying().(*types.Pointer).Elem())
	case 'T':
		return g.typeString(t)
	}
	panic(fmt.Sprintf("%s: %s has no codec", where, t))
}

var payloadTemplate = template.Must(template.New("payload").Parse(`// Code generated by gen-manifest; DO NOT EDIT.

package event

import (
	"encoding/binary"
{{- if .Math }}
	"math"
{{- end }}
{{- range .Imports }}
	"{{ . }}"
{{- end }}
)

// PayloadSchema is every event's payload struct and every struct's fields and
// kinds (b bool, i signed, u unsigned, f float64, s string, []k slice, [n]k
// array, *S pointer, S struct), as the journal's anchor carries it so a reader
// on another build can render a payload.
const PayloadSchema = "{{ .Schema }}"
{{ range .Codecs }}
{{- if .Payload }}
func (p *{{ .Type }}) appendPayload(b []byte) []byte { return append{{ .Name }}(b, p) }
func (p *{{ .Type }}) readPayload(r *wireReader)   { read{{ .Name }}(r, p) }
{{ end }}
func append{{ .Name }}(b []byte, p *{{ .Type }}) []byte {
	var m [{{ .Mask }}]byte
{{- range .Fields }}
	{{ .Present }}
{{- end }}
	b = append(b, m[:]...)
{{- range .Fields }}
{{- if .Encode }}
	{{ .Encode }}
{{- end }}
{{- end }}
	return b
}

func read{{ .Name }}(r *wireReader, p *{{ .Type }}) {
	var m [{{ .Mask }}]byte
	for i := range m {
		m[i] = r.u8()
	}
{{- range .Fields }}
	{{ .Decode }}
{{- end }}
}
{{ end }}
`))
