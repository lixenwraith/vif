// Code generation:
//   - internal/engine/component_store_gen.go: Component struct, entity lifecycle methods
//   - internal/engine/snapshot_pages_gen.go: canonical page rows over the capture stores
//   - internal/manifest/build_gen.go: typed system and renderer builders, ActiveSystems
//   - internal/event/registry_gen.go: Event registry, derived from event/type.go
//   - internal/input/strings_gen.go: Reverse lookup strings for input constants
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

type ComponentDef struct {
	Field  string
	Type   string
	Domain string
}

// DomainConst renders the core.Domain constant, empty when the bit attaches in either
func (c ComponentDef) DomainConst() string {
	switch c.Domain {
	case "shared":
		return "core.DomainShared"
	case "player":
		return "core.DomainPlayer"
	}
	return ""
}

type SystemDef struct {
	Name        string
	Constructor string
	Domain      string
	Requires    []string
	Optional    []string
	Snapshot    string
}

// SnapshotConst renders the engine.SnapshotProfile constant for a declared
// snapshot obligation (D-19). Empty declares none, which is the common case.
func (s SystemDef) SnapshotConst() string {
	switch s.Snapshot {
	case "", "none":
		return "SnapshotNone"
	case "state":
		return "SnapshotState"
	}
	return ""
}

// DomainConst renders the engine.SystemDomain constant for a declared profile
func (s SystemDef) DomainConst() string {
	switch s.Domain {
	case "shared":
		return "SystemShared"
	case "player":
		return "SystemPlayer"
	case "dual":
		return "SystemDual"
	}
	return ""
}

// RequiresExpr renders the dependency set as the engine constructor call the
// generated table uses; branching lives here rather than in the template.
func (s SystemDef) RequiresExpr() string {
	switch {
	case len(s.Requires) == 0 && len(s.Optional) == 0:
		return "nil"
	case len(s.Optional) == 0:
		return "engine.Require(" + quoteList(s.Requires) + ")"
	case len(s.Requires) == 0:
		return "engine.Optional(" + quoteList(s.Optional) + ")"
	default:
		return "append(engine.Require(" + quoteList(s.Requires) + "), engine.Optional(" +
			quoteList(s.Optional) + ")...)"
	}
}

// quoteList renders a name list as Go string literal arguments
func quoteList(names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = `"` + n + `"`
	}
	return strings.Join(parts, ", ")
}

type RendererDef struct {
	Name        string
	Constructor string
	Priority    string
}

type Definitions struct {
	Components     []ComponentDef
	Systems        []SystemDef
	ContextSystems []SystemDef
	Renderers      []RendererDef
}

// EnumDef represents a custom type and its list of constant names
type EnumDef struct {
	Type  string
	Names []string
}

// EventDef is one entry in the generated registry
type EventDef struct {
	Name    string // constant identifier, e.g. "EventLevelSetup"
	Payload string // payload struct name; "" = registered as nil
	Class   string // replication class constant, e.g. "ClassBus"
}

// EventDefs is the template input for event/registry_gen.go
type EventDefs struct {
	Events []EventDef
	Count  int // total constants in the block, including EventNone
}

const (
	eventTypeName = "EventType"
	eventNoneName = "EventNone"
	eventPrefix   = "Event"
	payloadSuffix = "Payload"
	classPrefix   = "Class"
)

// eventClasses are the replication classes a doc comment may declare, lowercase
// in the annotation and title-cased into the event.EventClass constant.
var eventClasses = map[string]string{
	"local": "ClassLocal", "shared": "ClassShared",
	"bus": "ClassBus", "stamped": "ClassStamped",
}

func main() {
	defs := parseDefinitions("definition.go") // cwd is 'manifest/'

	generateFile("../engine/component_store_gen.go", componentStoreTemplate, defs)
	generateFile("../engine/component_domain_gen.go", componentDomainTemplate, defs)
	generateFile("../engine/snapshot_world_gen.go", snapshotWorldTemplate, defs)
	generateFile("../engine/snapshot_pages_gen.go", snapshotPagesTemplate, defs)
	generateFile("build_gen.go", buildTemplate, defs)

	events := parseEvents("../event")
	generateFile("../event/registry_gen.go", eventRegistryTemplate, events)

	inputEnums := parseInputTypes("../input")
	generateFile("../input/strings_gen.go", inputStringsTemplate, inputEnums)

	fmt.Printf("Generated: %d components, %d systems, %d renderers, %d events, %d input enums\n",
		len(defs.Components), len(defs.Systems), len(defs.Renderers), len(events.Events), len(inputEnums))
}

func parseDefinitions(path string) Definitions {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		panic(fmt.Sprintf("parse %s: %v", path, err))
	}

	var defs Definitions

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, spec := range genDecl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) == 0 || len(vs.Values) == 0 {
				continue
			}

			name := vs.Names[0].Name
			comp, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				continue
			}

			switch name {
			case "Components":
				defs.Components = parseComponents(comp)
			case "Systems":
				defs.Systems = parseSystems(comp)
			case "ContextSystems":
				defs.ContextSystems = parseSystems(comp)
			case "Renderers":
				defs.Renderers = parseRenderers(comp)
			}
		}
	}

	return defs
}

func parseComponents(comp *ast.CompositeLit) []ComponentDef {
	var result []ComponentDef
	for _, elt := range comp.Elts {
		lit, ok := elt.(*ast.CompositeLit)
		if !ok || len(lit.Elts) < 2 {
			continue
		}
		def := ComponentDef{
			Field: extractString(lit.Elts[0]),
			Type:  extractString(lit.Elts[1]),
		}
		if len(lit.Elts) > 2 {
			def.Domain = extractString(lit.Elts[2])
		}
		if def.Domain != "" && def.DomainConst() == "" {
			panic(fmt.Sprintf("component %s: domain %q is not \"shared\", \"player\" or \"\"", def.Field, def.Domain))
		}
		result = append(result, def)
	}
	return result
}

func parseSystems(comp *ast.CompositeLit) []SystemDef {
	var result []SystemDef
	for _, elt := range comp.Elts {
		lit, ok := elt.(*ast.CompositeLit)
		if !ok {
			continue
		}
		def := parseSystemDef(lit)
		if def.Name == "" || def.Constructor == "" {
			panic(fmt.Sprintf("system definition requires name and constructor: %#v", def))
		}
		if def.DomainConst() == "" {
			panic(fmt.Sprintf("system %s: domain %q is not \"shared\", \"player\" or \"dual\"", def.Name, def.Domain))
		}
		if def.SnapshotConst() == "" {
			panic(fmt.Sprintf("system %s: snapshot %q is not \"\", \"none\" or \"state\"", def.Name, def.Snapshot))
		}
		result = append(result, def)
	}
	return result
}

// parseSystemDef reads one keyed SystemDef literal
func parseSystemDef(lit *ast.CompositeLit) SystemDef {
	var def SystemDef
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Name":
			def.Name = extractString(kv.Value)
		case "Constructor":
			def.Constructor = extractString(kv.Value)
		case "Domain":
			def.Domain = extractString(kv.Value)
		case "Requires":
			def.Requires = extractStringList(kv.Value)
		case "Optional":
			def.Optional = extractStringList(kv.Value)
		case "Snapshot":
			def.Snapshot = extractString(kv.Value)
		}
	}
	return def
}

// extractStringList reads a []string composite literal
func extractStringList(expr ast.Expr) []string {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(lit.Elts))
	for _, e := range lit.Elts {
		if s := extractString(e); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseRenderers(comp *ast.CompositeLit) []RendererDef {
	var result []RendererDef
	for _, elt := range comp.Elts {
		lit, ok := elt.(*ast.CompositeLit)
		if !ok || len(lit.Elts) < 3 {
			continue
		}
		result = append(result, RendererDef{
			Name:        extractString(lit.Elts[0]),
			Constructor: extractString(lit.Elts[1]),
			Priority:    extractString(lit.Elts[2]),
		})
	}
	return result
}

func extractString(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return ""
	}
	return strings.Trim(lit.Value, `"`)
}

// parseInputTypes walks the input package and collects constants for specific enums.
// It leverages the fact that Go's 'iota' infers the type from the previous line.
func parseInputTypes(dir string) []EnumDef {
	files, _ := parsePackage(dir)

	targetTypes := map[string]bool{
		"IntentType": true, "MotionOp": true,
		"OperatorOp": true, "SpecialOp": true,
		"ModeTarget": true, "ScrollDir": true,
	}

	collected := make(map[string][]string)

	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}

			var currentType string
			for _, s := range gd.Specs {
				vs, ok := s.(*ast.ValueSpec)
				if !ok {
					continue
				}

				// In an iota block, type is defined on the first line.
				// If Type is missing, it carries over from currentType.
				if vs.Type != nil {
					if id, ok := vs.Type.(*ast.Ident); ok {
						currentType = id.Name
					} else {
						currentType = "" // Reset if it's some other explicit type we don't care about
					}
				}

				if targetTypes[currentType] {
					for _, name := range vs.Names {
						if name.Name != "_" { // Skip sentinel holes if any exist
							collected[currentType] = append(collected[currentType], name.Name)
						}
					}
				}
			}
		}
	}

	// Sort map keys for deterministic generation
	var types []string
	for t := range collected {
		types = append(types, t)
	}
	sort.Strings(types)

	var defs []EnumDef
	for _, t := range types {
		defs = append(defs, EnumDef{
			Type:  t,
			Names: collected[t],
		})
	}

	return defs
}

func generateFile(path string, tmpl *template.Template, data any) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("template %s: %v", path, err))
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// Write unformatted for debugging
		os.WriteFile(path+".err", buf.Bytes(), 0644)
		panic(fmt.Sprintf("gofmt %s: %v", path, err))
	}

	if err := os.WriteFile(path, formatted, 0644); err != nil {
		panic(fmt.Sprintf("write %s: %v", path, err))
	}
}

var componentStoreTemplate = template.Must(template.New("store").Parse(`// Code generated by gen-manifest; DO NOT EDIT.

package engine

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
)

// Component ID Bitmasks Mapping for Engine-Level Entity Signatures
// Used for O(1) destruction skipping and future fast queries
const (
{{- range $index, $comp := .Components }}
	{{ if eq $index 0 }}{{ $comp.Field }}Bit uint64 = 1 << iota{{ else }}{{ $comp.Field }}Bit{{ end }}
{{- end }}
	PositionBit // Special index designated for spatial grid presence
)

// Component provides typed component store pointers
// Embedded in World, initialized once at world creation
type Component struct {
{{- range .Components }}
	{{ .Field }} *Store[component.{{ .Type }}]
{{- end }}
}

// initComponents creates all component stores
// Called once from NewWorld()
func initComponents(w *World) {
{{- range .Components }}
	w.Components.{{ .Field }} = NewStore[component.{{ .Type }}](w, {{ .Field }}Bit)
{{- end }}
	w.Positions = NewPosition(w, PositionBit)
}

// removeEntity removes entity from every component store
// Caller MUST hold updateMutex
func (w *World) removeEntity(e core.Entity) {

	// Guard against unallocated entity
	mask, ok := w.componentMask[e]
	if !ok {
		return
	}

	// O(1) Fast-Path: If the entity has no components, exit immediately
	if mask == 0 {
		delete(w.componentMask, e)
		return
	}

	// O(1) Skip: Only invoke removal on stores where the bit is strictly present
{{- range .Components }}
	if mask&{{ .Field }}Bit != 0 {
		w.Components.{{ .Field }}.RemoveEntity(e, true)
	}
{{- end }}
	if mask&PositionBit != 0 {
		w.Positions.RemoveEntity(e, true)
	}

	// Remove entity from component mask
	delete(w.componentMask, e)
}

// removeEntitiesBatch removes entities from all stores using batch operations
// Caller MUST hold updateMutex
func (w *World) removeEntitiesBatch(entities []core.Entity) {
	// Union of component signatures: skip every store no entity touches
	var union uint64
	for _, e := range entities {
		union |= w.componentMask[e]
	}
	if union == 0 {
		for _, e := range entities {
			delete(w.componentMask, e)
		}
		return
	}
{{- range .Components }}
	if union&{{ .Field }}Bit != 0 {
		w.Components.{{ .Field }}.RemoveBatch(entities, true)
	}
{{- end }}
	if union&PositionBit != 0 {
		w.Positions.RemoveBatch(entities, true)
	}

	for _, e := range entities {
		delete(w.componentMask, e)
	}
}

// wipeAll clears all component stores
// Caller MUST hold updateMutex
func (w *World) wipeAll() {
{{- range .Components }}
	w.Components.{{ .Field }}.ClearAllComponents()
{{- end }}
	w.Positions.ClearAllComponents()
	
	// Clear component mask
	clear(w.componentMask)
}
`))

// parseEvents reads the event package and derives the registry from the
// EventType const block. Payload association comes from the doc comment
// annotation: "// EventFoo (FooPayload) description".
//
// An annotation containing '[' or '.' (BatchPayload[T], core.Entity) denotes a
// pooled or scalar payload that is not TOML-decodable; it registers as nil.
func parseEvents(dir string) EventDefs {
	files, names := parsePackage(dir)

	structs := collectStructs(files)

	var (
		block *ast.GenDecl
		src   string
	)
	for i, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST || !isEventTypeBlock(gd) {
				continue
			}
			if block != nil {
				panic(fmt.Sprintf("multiple EventType const blocks (%s, %s); generator expects one", src, names[i]))
			}
			block, src = gd, names[i]
		}
	}
	if block == nil {
		panic(fmt.Sprintf("EventType const block not found in %s", dir))
	}

	return collectEvents(block, structs)
}

// parsePackage parses every non-test, non-generated .go file in dir
func parsePackage(dir string) ([]*ast.File, []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		panic(fmt.Sprintf("read %s: %v", dir, err))
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") ||
			strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, "_gen.go") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names) // deterministic diagnostics

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(names))
	for _, n := range names {
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.ParseComments)
		if err != nil {
			panic(fmt.Sprintf("parse %s: %v", n, err))
		}
		files = append(files, f)
	}
	return files, names
}

// collectStructs returns non-generic struct type names declared in the package
func collectStructs(files []*ast.File) map[string]bool {
	structs := make(map[string]bool, 256)
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts, ok := s.(*ast.TypeSpec)
				if !ok || ts.TypeParams != nil {
					continue // skip BatchPayload[T] and friends
				}
				if _, isStruct := ts.Type.(*ast.StructType); isStruct {
					structs[ts.Name.Name] = true
				}
			}
		}
	}
	return structs
}

func isEventTypeBlock(gd *ast.GenDecl) bool {
	for _, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if id, ok := vs.Type.(*ast.Ident); ok && id.Name == eventTypeName {
			return true
		}
	}
	return false
}

// collectEvents walks the const block, resolves payloads, and enforces the
// annotation contract. Any violation aborts generation.
func collectEvents(block *ast.GenDecl, structs map[string]bool) EventDefs {
	var (
		defs       []EventDef
		count      int
		errs       []string
		warns      []string
		referenced = make(map[string]bool)
	)

	for _, s := range block.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, ident := range vs.Names {
			count++
			name := ident.Name

			if name == "_" || name == eventNoneName {
				continue // sentinel and holes are never registered
			}
			if !strings.HasPrefix(name, eventPrefix) {
				errs = append(errs, fmt.Sprintf("%s: EventType constants must be prefixed %q", name, eventPrefix))
				continue
			}

			annot := docAnnot(vs.Doc, name)
			payload, annotated := annot.payload, annot.hasPayload

			class, ok := eventClasses[annot.class]
			switch {
			case annot.class == "":
				errs = append(errs, fmt.Sprintf(
					"%s: no replication class; write \"// %s [local|shared|bus|stamped] ...\" (D-10)", name, name))
				continue
			case !ok:
				errs = append(errs, fmt.Sprintf("%s: unknown replication class %q", name, annot.class))
				continue
			}

			if !annotated {
				// Safety net: catches "added FooPayload, forgot the annotation"
				if c := strings.TrimPrefix(name, eventPrefix) + payloadSuffix; structs[c] {
					errs = append(errs, fmt.Sprintf(
						"%s: struct %s exists but the doc comment carries no payload annotation; write \"// %s (%s) ...\"",
						name, c, name, c))
					continue
				}
				if vs.Doc == nil {
					warns = append(warns, name+": no doc comment")
				}
				defs = append(defs, EventDef{Name: name, Class: class})
				continue
			}

			switch {
			case payload == "":
				defs = append(defs, EventDef{Name: name, Class: class})
			case strings.ContainsAny(payload, "[."):
				// Pooled generic or scalar: opaque to TOML, registers as nil
				defs = append(defs, EventDef{Name: name, Class: class})
			case !structs[payload]:
				errs = append(errs, fmt.Sprintf("%s: annotated payload %s is not a struct in package event", name, payload))
			default:
				referenced[payload] = true
				defs = append(defs, EventDef{Name: name, Payload: payload, Class: class})
			}
		}
	}

	for s := range structs {
		if strings.HasSuffix(s, payloadSuffix) && !referenced[s] {
			warns = append(warns, s+": declared but referenced by no event")
		}
	}
	sort.Strings(warns)

	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "warning: event:", w)
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "error: event:", e)
		}
		panic(fmt.Sprintf("event registry: %d annotation error(s)", len(errs)))
	}

	return EventDefs{Events: defs, Count: count}
}

// eventAnnot is what one constant's doc comment declares: an optional payload
// type in parentheses and an optional replication class in brackets.
type eventAnnot struct {
	payload    string // payload type name; "" when the group is absent or empty
	class      string // replication class, lowercase; "" when absent
	hasPayload bool   // a "(...)" group was present
}

// docAnnot parses the doc line that opens with the constant name. Both groups
// are optional and may appear in either order:
//
//	// EventFoo (FooPayload) [bus] description
//	// EventFoo [local] description
func docAnnot(doc *ast.CommentGroup, name string) eventAnnot {
	var a eventAnnot
	if doc == nil {
		return a
	}
	for _, line := range strings.Split(doc.Text(), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), name)
		if !ok {
			continue
		}
		// Consume leading groups until the prose starts; order is not significant.
		for {
			rest = strings.TrimSpace(rest)
			switch {
			case strings.HasPrefix(rest, "(") && !a.hasPayload:
				end := strings.Index(rest, ")")
				if end < 0 {
					return a
				}
				a.payload, a.hasPayload = strings.TrimSpace(rest[1:end]), true
				rest = rest[end+1:]
			case strings.HasPrefix(rest, "[") && a.class == "":
				end := strings.Index(rest, "]")
				if end < 0 {
					return a
				}
				a.class = strings.ToLower(strings.TrimSpace(rest[1:end]))
				rest = rest[end+1:]
			default:
				return a
			}
		}
	}
	return a
}

var eventRegistryTemplate = template.Must(template.New("events").Parse(`// Code generated by gen-manifest; DO NOT EDIT.

package event

// EventTypeCount is the number of declared EventType constants, including EventNone
// Values are contiguous in [0, EventTypeCount)
const EventTypeCount = {{ .Count }}

// InitRegistry populates the registry from the EventType const block in type.go
// Must be called once at startup
func InitRegistry() {
	if registryInit {
		return
	}
	registryInit = true

{{- range .Events }}
	RegisterType("{{ .Name }}", {{ .Name }}, {{ if .Payload }}&{{ .Payload }}{}{{ else }}nil{{ end }})
{{- end }}

}

// eventClasses is every type's replication class, declared by the "[class]"
// annotation in type.go. Index is the EventType; an unlisted slot is ClassUnset,
// which replicates nothing.
var eventClasses = [EventTypeCount]EventClass{
{{- range .Events }}
	{{ .Name }}: {{ .Class }},
{{- end }}
}
`))

var buildTemplate = template.Must(template.New("build").Parse(`// Code generated by gen-manifest; DO NOT EDIT.

package manifest

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/internal/render/renderer"
	"github.com/lixenwraith/vi-fighter/internal/system"
)

// BuildSystems constructs every active system in manifest order
// World.AddSystem sorts by Priority(); the sort is stable, so manifest order
// breaks ties between systems sharing a priority constant
func BuildSystems(w *engine.World) []engine.System {
	return []engine.System{
{{- range .Systems }}
		system.{{ .Constructor }}(w),
{{- end }}
	}
}

// BuildRenderers constructs every active renderer paired with its priority
// RenderOrchestrator.Register sorts by priority with a stable index tiebreak,
// so manifest order breaks ties between renderers at the same layer
func BuildRenderers(ctx *engine.GameContext) []render.Registration {
	return []render.Registration{
{{- range .Renderers }}
		{Renderer: renderer.{{ .Constructor }}(ctx), Priority: render.{{ .Priority }}},
{{- end }}
	}
}

// ActiveSystems returns the names of the systems BuildSystems constructs
// Consumed by config validation of region enabled_systems/disabled_systems
func ActiveSystems() []string {
	return []string{
{{- range .Systems }}
		"{{ .Name }}",
{{- end }}
	}
}

// systemProfiles is every system's declared profile: the domain it resolves and the
// systems it depends on. AddSystem takes it as a registration argument.
var systemProfiles = map[string]engine.SystemProfile{
{{- range .Systems }}
	"{{ .Name }}": {Domain: engine.{{ .DomainConst }}, Requires: {{ .RequiresExpr }}},
{{- end }}
{{- range .ContextSystems }}
	"{{ .Name }}": {Domain: engine.{{ .DomainConst }}, Requires: {{ .RequiresExpr }}},
{{- end }}
}

// systemSnapshots is every system's declared D-19 obligation: whether it holds
// future-affecting state outside the component stores, and therefore whether it
// must implement engine.SharedStateSaver. The boundary suite asserts the
// declaration against the code, which is what turns the hidden-state survey from
// a list someone maintains into one the build checks.
var systemSnapshots = map[string]engine.SnapshotProfile{
{{- range .Systems }}
	"{{ .Name }}": engine.{{ .SnapshotConst }},
{{- end }}
{{- range .ContextSystems }}
	"{{ .Name }}": engine.{{ .SnapshotConst }},
{{- end }}
}
`))

var inputStringsTemplate = template.Must(template.New("inputStrings").Parse(`// Code generated by gen-manifest; DO NOT EDIT.

package input

import "fmt"

{{- range . }}

func (i {{ .Type }}) String() string {
	switch i {
	{{- range .Names }}
	case {{ . }}:
		return "{{ . }}"
	{{- end }}
	default:
		return fmt.Sprintf("{{ .Type }}(%d)", i)
	}
}
{{- end }}
`))

var componentDomainTemplate = template.Must(template.New("domain").Parse(`// Code generated by gen-manifest; DO NOT EDIT.

package engine

import "github.com/lixenwraith/vi-fighter/internal/core"

// componentRule names the entity domain a component bit may attach to
type componentRule struct {
	field  string
	domain core.Domain
}

// componentDomains is the audit table for AddComponentMask, derived from
// manifest.Components. A bit absent here attaches in either domain.
var componentDomains = map[uint64]componentRule{
{{- range .Components }}{{ if .DomainConst }}
	{{ .Field }}Bit: {"{{ .Field }}", {{ .DomainConst }}},
{{- end }}{{ end }}
}
`))

// snapshotWorldTemplate emits the shared-world half of a D-19 capture. Generated
// rather than hand-written for the same reason the stores themselves are: a
// component added to the manifest must appear in the snapshot without anyone
// remembering to add it, or the capture silently omits state the plan requires it
// to carry.
var snapshotWorldTemplate = template.Must(template.New("snapshotWorld").Funcs(template.FuncMap{"lower": strings.ToLower}).Parse(`// Code generated by gen-manifest; DO NOT EDIT.

package engine

import (
	"slices"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
)

// StoreEntry is one entity's component in a capture. The reference is a
// core.Entity, never a dense index: dense positions are an artifact of insertion
// order on the instance that produced them and mean nothing on any other.
type StoreEntry[T any] struct {
	Entity core.Entity ` + "`json:\"e\"`" + `
	Value  T           ` + "`json:\"v\"`" + `
}

// SharedWorldState is every shared-domain component in the world, by store, plus
// the placements and the allocator counters. Player-domain entities are excluded
// at capture: a snapshot describes the shared world and nothing else (D-1, D-6),
// and a participant's own simulation does not exist on any other instance (D-2).
type SharedWorldState struct {
	// NextEntity is the shared allocator's next ID. It travels because entity
	// identity is shared state: an installed world that allocated from a different
	// counter would give the next spawned entity an ID the sender used for
	// something else.
	NextEntity uint64 ` + "`json:\"next_entity\"`" + `

	// Created and Destroyed are the shared domain's lifetime counters. They are in
	// the compared surface, because two participants creating and destroying the
	// same shared entities must agree on how many there have been; an installed
	// world reporting its own totals disagrees from the tick it arrives.
	Created   int64 ` + "`json:\"created\"`" + `
	Destroyed int64 ` + "`json:\"destroyed\"`" + `

	Positions []StoreEntry[component.PositionComponent] ` + "`json:\"positions,omitempty\"`" + `
{{- range .Components }}
	{{ .Field }} []StoreEntry[component.{{ .Type }}] ` + "`json:\"{{ .Field | lower }},omitempty\"`" + `
{{- end }}
}

// CaptureSharedWorld reads every shared entity's components into a transferable
// value. Entities are emitted in store order, which is the order the receiving
// instance re-inserts them in, so a capture of an installed world equals the
// capture it was installed from.
//
// Caller MUST hold updateMutex: this reads every store.
func (w *World) CaptureSharedWorld() SharedWorldState {
	var s SharedWorldState
	s.NextEntity = w.nextEntityID[core.DomainShared]
	s.Created = w.createdCount[core.DomainShared].Load()
	s.Destroyed = w.destroyedCount[core.DomainShared].Load()

	for _, e := range w.Positions.Entities() {
		if e.Domain() != core.DomainShared {
			continue
		}
		if v, ok := w.Positions.GetPosition(e); ok {
			s.Positions = append(s.Positions, StoreEntry[component.PositionComponent]{Entity: e, Value: v})
		}
	}
{{- range .Components }}
	for _, e := range w.Components.{{ .Field }}.Entities() {
		if e.Domain() != core.DomainShared {
			continue
		}
		if v, ok := w.Components.{{ .Field }}.GetComponent(e); ok {
			s.{{ .Field }} = append(s.{{ .Field }}, StoreEntry[component.{{ .Type }}]{Entity: e, Value: DetachSnapshotValue(v)})
		}
	}
{{- end }}
	return s
}

// InstallSharedWorld replaces this world's shared half with a capture. Player
// entities are left untouched: they are this instance's own and no capture
// describes them.
//
// Caller MUST hold updateMutex.
func (w *World) InstallSharedWorld(s SharedWorldState) {
	w.clearSharedEntities()

	for _, en := range s.Positions {
		w.Positions.SetPosition(en.Entity, en.Value)
	}
{{- range .Components }}
	for _, en := range s.{{ .Field }} {
		w.Components.{{ .Field }}.SetComponent(en.Entity, DetachSnapshotValue(en.Value))
	}
{{- end }}
	if s.NextEntity > 0 {
		w.nextEntityID[core.DomainShared] = s.NextEntity
	}
	w.createdCount[core.DomainShared].Store(s.Created)
	w.destroyedCount[core.DomainShared].Store(s.Destroyed)

	// The spatial index is derived, not shipped: SetPosition above rebuilt it.
	// Its gauges are published on the status cadence, so republish them here —
	// they describe what was just installed, and they are in the compared surface.
	w.Positions.PublishTelemetry()
}

// SharedWorldDelta is one shared world expressed against another, store by store.
// Generated for the same reason the capture is: a component added to the manifest
// has to reach a correction without anyone remembering to add it, or the delta
// silently omits state the baseline then keeps forever.
//
// The scalars are carried whole. They are three numbers, and a delta that omitted
// an unchanged one would need a way to say "unchanged" that costs more than the
// number.
type SharedWorldDelta struct {
	NextEntity uint64 ` + "`json:\"next_entity\"`" + `
	Created    int64  ` + "`json:\"created\"`" + `
	Destroyed  int64  ` + "`json:\"destroyed\"`" + `

	Positions StoreDelta[component.PositionComponent] ` + "`json:\"positions,omitzero\"`" + `
{{- range .Components }}
	{{ .Field }} StoreDelta[component.{{ .Type }}] ` + "`json:\"{{ .Field | lower }},omitzero\"`" + `
{{- end }}
}

// DiffSharedWorld expresses next as a difference against base.
//
// Applying the result to base reproduces next exactly — same entries, same order —
// which is the property the receiver's integrity check depends on. Nothing here
// reads the world, so it runs on whatever goroutine took the two captures rather
// than under the world lock.
func DiffSharedWorld(base, next SharedWorldState) SharedWorldDelta {
	var d SharedWorldDelta
	d.NextEntity, d.Created, d.Destroyed = next.NextEntity, next.Created, next.Destroyed
	d.Positions = diffStore(base.Positions, next.Positions)
{{- range .Components }}
	d.{{ .Field }} = diffStore(base.{{ .Field }}, next.{{ .Field }})
{{- end }}
	return d
}

// ApplySharedWorldDelta reconstructs the world a delta was computed for.
func ApplySharedWorldDelta(base SharedWorldState, d SharedWorldDelta) SharedWorldState {
	var s SharedWorldState
	s.NextEntity, s.Created, s.Destroyed = d.NextEntity, d.Created, d.Destroyed
	s.Positions = applyStore(base.Positions, d.Positions)
{{- range .Components }}
	s.{{ .Field }} = applyStore(base.{{ .Field }}, d.{{ .Field }})
{{- end }}
	return s
}

// DeltaEntries counts the component cells a delta moves, which is what a
// correction's size is reported in.
func (d SharedWorldDelta) DeltaEntries() int {
	n := d.Positions.Entries()
{{- range .Components }}
	n += d.{{ .Field }}.Entries()
{{- end }}
	return n
}

// SharedWorldDifference measures how far apart two readings of the shared world
// are. On a guest that is the correction magnitude: the distance between what it
// predicted and what the host is telling it.
func SharedWorldDifference(a, b SharedWorldState) WorldDifference {
	touched := make(map[core.Entity]struct{}, 64)
	var w WorldDifference
	w.Entries = countStoreDifference(a.Positions, b.Positions, touched)
{{- range .Components }}
	w.Entries += countStoreDifference(a.{{ .Field }}, b.{{ .Field }}, touched)
{{- end }}
	w.Entities = len(touched)
	w.CellShift = positionShift(a.Positions, b.Positions)
	return w
}

// ReconcileSharedWorld brings this world's shared half to s by writing what
// differs instead of replacing everything.
//
// It exists because a correction is not a join. InstallSharedWorld clears every
// shared entity out of all fifty-two stores and re-inserts the capture, which is
// the right shape once, for a participant that has no world yet, and the wrong
// shape two to five times a second for one that has a world nearly identical to
// the capture already. The teardown is the expensive half — every removal is a
// mask edit, a dense swap-back in each store it touches, and a spatial index
// eviction — and a correction throws away and rebuilds state that never moved.
//
// What this writes is bounded by the *correction magnitude* rather than by the
// world size: the entities the authority no longer has, the components an entity
// no longer carries, and every entry's value. What it still scans is the world,
// because finding the first two means looking.
//
// The result is the same world InstallSharedWorld would leave, with one difference
// that is deliberate: the dense store order is whatever the live world already had
// rather than the sender's. Nothing compares it — the shared digest sorts, and the
// compared surface is keyed — and preserving it is what keeps the spatial index
// from churning. TestReconcileMatchesAFullInstall is what holds the equality.
//
// Caller MUST hold updateMutex.
func (w *World) ReconcileSharedWorld(s SharedWorldState) {
	target := make(map[core.Entity]struct{}, len(s.Positions))
	for _, en := range s.Positions {
		target[en.Entity] = struct{}{}
	}
{{- range .Components }}
	for _, en := range s.{{ .Field }} {
		target[en.Entity] = struct{}{}
	}
{{- end }}

	// Entities the authority no longer holds leave whole, so no zero-component
	// mask entry is left behind for clearSharedEntities to find later.
	gone := make([]core.Entity, 0, 8)
	for e := range w.componentMask {
		if e.Domain() != core.DomainShared {
			continue
		}
		if _, ok := target[e]; !ok {
			gone = append(gone, e)
		}
	}
	slices.Sort(gone)
	for _, e := range gone {
		w.removeEntity(e)
	}

	reconcilePositions(w.Positions, s.Positions)
{{- range .Components }}
	reconcileStore(w.Components.{{ .Field }}, s.{{ .Field }})
{{- end }}

	if s.NextEntity > 0 {
		w.nextEntityID[core.DomainShared] = s.NextEntity
	}
	w.createdCount[core.DomainShared].Store(s.Created)
	w.destroyedCount[core.DomainShared].Store(s.Destroyed)
	w.Positions.PublishTelemetry()
}

// clearSharedEntities removes every shared entity from every store, so an install
// replaces the shared world rather than merging into it. Merging would leave
// entities this instance had derived on its own beside the capture's, which is
// the divergence an install exists to end.
//
// Caller MUST hold updateMutex.
func (w *World) clearSharedEntities() {
	shared := make([]core.Entity, 0, len(w.componentMask))
	for e := range w.componentMask {
		if e.Domain() == core.DomainShared {
			shared = append(shared, e)
		}
	}
	// Sorted before removal: the stores are dense with swap-back removal, so the
	// order entities leave in decides where the player entities that stay end up.
	// That layout is not compared, but an install must not be one of the places a
	// map's iteration order reaches the world.
	slices.Sort(shared)
	for _, e := range shared {
		w.removeEntity(e)
	}
}
`))

// snapshotPagesTemplate emits the page-level view of a capture's component stores.
//
// Phase 6 partitions a capture into sections and bounded pages so a correction can
// prove equality with a hash instead of carrying state. The store inventory that
// partition is built over has to be the same one the capture is built over, or a
// component added to the manifest would be captured and never hashed — hashed
// state the receiver silently never repairs is worse than state nobody carries.
// So the inventory is generated from manifest.Components beside the capture
// itself, and there is no second hand-maintained list.
//
// Rows are (entity, canonical JSON value) pairs in entity-ascending order. That
// ordering is the manifest's own and is deliberately not the store's: the dense
// order a live world holds is an artifact of its insertion history — a reconciled
// world keeps its own — so two instances holding equal state produce equal page
// hashes only if the page is read in an order neither of them chose.
var snapshotPagesTemplate = template.Must(template.New("snapshotPages").Funcs(template.FuncMap{
	"lower": strings.ToLower,
	"add1":  func(i int) int { return i + 1 },
}).Parse(`// Code generated by gen-manifest; DO NOT EDIT.

package engine

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/lixenwraith/vi-fighter/internal/core"
)

// StoreRow is one component cell in the manifest's canonical form: the entity it
// belongs to and the exact bytes its value marshals to. The bytes are the hashed
// content and the repaired content both, so a row that hashes equal decodes equal.
type StoreRow struct {
	Entity core.Entity     ` + "`json:\"e\"`" + `
	Value  json.RawMessage ` + "`json:\"v\"`" + `
}

// SharedWorldStoreNames are the wire-stable section ids of the capture's component
// stores, in a fixed order. The name is what a shard names, so it may not change
// without a manifest version change.
var SharedWorldStoreNames = []string{
	"positions",
{{- range .Components }}
	"{{ .Field | lower }}",
{{- end }}
}

// SharedWorldStoreCount is how many component stores a capture carries.
var SharedWorldStoreCount = len(SharedWorldStoreNames)

// SharedWorldStoreRows appends store i's cells to dst in entity-ascending order.
//
// Marshalling per cell rather than per store is what makes a page independently
// hashable and independently repairable; it runs on the correction goroutine,
// outside the world lock, on a capture that has already been taken.
func SharedWorldStoreRows(s *SharedWorldState, i int, dst []StoreRow) ([]StoreRow, error) {
	switch i {
	case 0:
		return appendStoreRows(dst, s.Positions)
{{- range $i, $c := .Components }}
	case {{ add1 $i }}:
		return appendStoreRows(dst, s.{{ $c.Field }})
{{- end }}
	}
	return nil, fmt.Errorf("capture holds no store %d", i)
}

// SharedWorldApplyStoreRows replaces every cell of store i the page owns with rows.
//
// owns names the page's entities rather than the rows': a page whose content the
// authority no longer holds is repaired by an empty row set, and a receiver that
// only overwrote what arrived would keep a cell the authority had dropped.
func SharedWorldApplyStoreRows(s *SharedWorldState, i int, owns func(core.Entity) bool, rows []StoreRow) error {
	switch i {
	case 0:
		out, err := applyStoreRows(s.Positions, owns, rows)
		if err != nil {
			return err
		}
		s.Positions = out
		return nil
{{- range $i, $c := .Components }}
	case {{ add1 $i }}:
		out, err := applyStoreRows(s.{{ $c.Field }}, owns, rows)
		if err != nil {
			return err
		}
		s.{{ $c.Field }} = out
		return nil
{{- end }}
	}
	return fmt.Errorf("capture holds no store %d", i)
}

// appendStoreRows renders one store's entries in the manifest's canonical order.
func appendStoreRows[T any](dst []StoreRow, entries []StoreEntry[T]) ([]StoreRow, error) {
	start := len(dst)
	for _, en := range entries {
		v, err := json.Marshal(en.Value)
		if err != nil {
			return nil, err
		}
		dst = append(dst, StoreRow{Entity: en.Entity, Value: v})
	}
	rows := dst[start:]
	slices.SortFunc(rows, func(a, b StoreRow) int {
		switch {
		case a.Entity < b.Entity:
			return -1
		case a.Entity > b.Entity:
			return 1
		}
		return 0
	})
	return dst, nil
}

// applyStoreRows rebuilds one store with the page's cells replaced.
//
// The result keeps the entries the page does not own in their existing order and
// appends the page's in canonical order. Store order is not compared and is not in
// the manifest's hashed surface — see SharedWorldStoreRows — so what matters here
// is only that the set and the values are the authority's.
func applyStoreRows[T any](entries []StoreEntry[T], owns func(core.Entity) bool, rows []StoreRow) ([]StoreEntry[T], error) {
	out := make([]StoreEntry[T], 0, len(entries)+len(rows))
	for _, en := range entries {
		if owns(en.Entity) {
			continue
		}
		out = append(out, en)
	}
	for _, row := range rows {
		if !owns(row.Entity) {
			return nil, fmt.Errorf("shard carries entity %d, which its page does not own", uint64(row.Entity))
		}
		var v T
		if err := json.Unmarshal(row.Value, &v); err != nil {
			return nil, err
		}
		out = append(out, StoreEntry[T]{Entity: row.Entity, Value: v})
	}
	return out, nil
}
`))
