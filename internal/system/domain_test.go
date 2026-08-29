package system

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

// allowedDomainAccess exempts one system's D-12 access to the other domain,
// keyed "system:Store". Shared consequences remain cell-derived; player victims
// and their lifecycle notifications remain local.
var allowedDomainAccess = map[string]string{
	"wall:Decay":    "D-12 push-out classifies any occupant; read-only mask lookup",
	"wall:Blossom":  "D-12 push-out classifies any occupant; read-only mask lookup",
	"drain:Wall":    "GetPtr reads BlockMask for spawn denial; the wall is never written",
	"death:Wall":    "GetPtr reads BlockMask while resolving a shared wall death",
	"quasar:Nugget": "D-12 footprint sweep classifies a personal victim for local lifecycle notification",
	"storm:Nugget":  "D-12 footprint sweep classifies a personal victim for local lifecycle notification",
}

// ownerAuthoredStores are the cursor-exclusive components exactly one instance
// writes and every other receives as transported values (D-13). A player system may
// write them; a shared system may not. Shield and Combat are absent deliberately:
// they also carry quasar, loot and species state, which is re-derived, and the store
// name alone cannot tell the two apart.
var ownerAuthoredStores = map[string]bool{
	"Energy": true, "Heat": true, "Boost": true,
	"Weapon": true, "CursorView": true, "Ping": true, "Pulse": true,
}

// ownerAuthoredCreators may write the D-13 set despite a shared profile: they create
// the entity, and the initial values are constants the shared creation order carries.
var ownerAuthoredCreators = map[string]bool{"cursor": true}

// storeWriters are the Store methods that can mutate a component. GetPtr hands
// out a mutable pointer, so it counts as a write unless allowedDomainAccess
// records that the call site only reads.
var storeWriters = map[string]bool{
	"SetComponent": true, "GetPtr": true,
	"RemoveEntity": true, "RemoveBatch": true, "ClearAllComponents": true,
}

// systemEvidence is what one system's file shows about the domains it touches
type systemEvidence struct {
	name    string
	domain  string // declared profile: shared, player or dual
	file    string
	creates map[string]bool // shared, player, stamped
	draws   map[string]bool // shared, player
	writes  map[string]bool // component store field names
	reads   map[string]bool // component store field names
	stamps  bool            // calls WithDomain
}

// TestSystemDomainProfiles checks every declared profile against the RNG
// streams, entity domains and component stores its file actually touches.
// Evidence is attributed per file: helpers shared between systems (sweep.go,
// targeting.go) declare no system and are not attributed to one.
func TestSystemDomainProfiles(t *testing.T) {
	storeDomains := parseStoreDomains(t, "../engine/component_domain_gen.go")
	systems := parseSystemEvidence(t, ".")

	if len(systems) < 50 {
		t.Fatalf("found %d systems, expected the full set; the parser has drifted", len(systems))
	}

	for _, e := range systems {
		switch e.domain {
		case "shared":
			// A shared system's every write must be re-derived identically on
			// every instance, so nothing player-domain may reach it.
			if e.creates["player"] {
				t.Errorf("%s: declared shared but creates player-domain entities", e.file)
			}
			if e.draws["player"] {
				t.Errorf("%s: declared shared but draws the player RNG stream", e.file)
			}
			for _, store := range sortedStores(e.writes) {
				if storeDomains[store] == "player" && !exempt(e.name, store) {
					t.Errorf("%s: declared shared but writes the player-only %s store", e.file, store)
				}
			}
			// D-13: owner-authored values are transported, never re-derived, so a
			// shared system must not author them. Blind to writes made through a
			// World helper rather than a Components selector.
			if !ownerAuthoredCreators[e.name] {
				for _, store := range sortedStores(e.writes) {
					if ownerAuthoredStores[store] {
						t.Errorf("%s: declared shared but writes the owner-authored %s store (D-13)",
							e.file, store)
					}
				}
			}
			for _, store := range sortedStores(e.reads) {
				if ownerAuthoredStores[store] {
					t.Errorf("%s: declared shared but reads the owner-authored %s store (D-13)", e.file, store)
				}
				if storeDomains[store] == "player" && !exempt(e.name, store) {
					t.Errorf("%s: declared shared but reads the player-only %s store (D-1)", e.file, store)
				}
			}

		case "player":
			// A player system may read shared state and write owner-authored
			// cursor components, but must not author replicated state.
			if e.creates["shared"] {
				t.Errorf("%s: declared player but creates shared-domain entities", e.file)
			}
			if e.draws["shared"] {
				t.Errorf("%s: declared player but draws the shared RNG stream", e.file)
			}
			for _, store := range sortedStores(e.writes) {
				if storeDomains[store] == "shared" && !ownerAuthoredStores[store] && !exempt(e.name, store) {
					t.Errorf("%s: declared player but writes the shared-only %s store", e.file, store)
				}
			}

		case "dual":
			// Dual resolves both domains: by ambient stamping (D-7), by drawing
			// a stream per domain (D-8), or by acting on components that attach
			// in either. A profile whose every trace points at one domain
			// should narrow to that domain instead.
			stamped := e.stamps || e.creates["stamped"]
			shared := e.creates["shared"] || e.draws["shared"] || pinned(e, storeDomains, "shared")
			player := e.creates["player"] || e.draws["player"] || pinned(e, storeDomains, "player")
			if !stamped && shared != player {
				t.Errorf("%s: declared dual but only resolves the %s domain; narrow the profile",
					e.file, pick(shared, "shared", "player"))
			}

		default:
			t.Errorf("%s: unknown domain profile %q", e.file, e.domain)
		}
	}
}

// pinned reports whether the system writes a store belonging to the domain
func pinned(e systemEvidence, storeDomains map[string]string, domain string) bool {
	for store := range e.writes {
		if storeDomains[store] == domain && !ownerAuthoredStores[store] && !exempt(e.name, store) {
			return true
		}
	}
	return false
}

// pick returns a when cond holds, b otherwise
func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// exempt reports whether allowedDomainAccess records a reason for this access
func exempt(system, store string) bool {
	_, ok := allowedDomainAccess[system+":"+store]
	return ok
}

// TestAllowedDomainAccessIsLive fails on an entry no longer describing real code,
// so the exemption list cannot outlive the access it excuses.
func TestAllowedDomainAccessIsLive(t *testing.T) {
	systems := parseSystemEvidence(t, ".")
	touched := make(map[string]bool)
	for _, e := range systems {
		for store := range e.writes {
			touched[e.name+":"+store] = true
		}
		for store := range e.reads {
			touched[e.name+":"+store] = true
		}
	}
	for key := range allowedDomainAccess {
		if !touched[key] {
			t.Errorf("allowedDomainAccess[%q] excuses an access that no longer exists", key)
		}
	}
}

// parseStoreDomains reads the engine audit table so store classification has one
// source. An unlisted store attaches in either domain and is reported as "".
func parseStoreDomains(t *testing.T, path string) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	domains := make(map[string]string)
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || !strings.HasSuffix(key.Name, "Bit") {
			return true
		}
		lit, ok := kv.Value.(*ast.CompositeLit)
		if !ok || len(lit.Elts) != 2 {
			return true
		}

		field := strings.TrimSuffix(key.Name, "Bit")
		if sel, ok := lit.Elts[1].(*ast.SelectorExpr); ok {
			domains[field] = strings.ToLower(strings.TrimPrefix(sel.Sel.Name, "Domain"))
		}
		return true
	})

	if len(domains) < 30 {
		t.Fatalf("parsed %d store domains from %s; the table or parser has drifted", len(domains), path)
	}
	return domains
}

// parseSystemEvidence walks every non-test file in dir and pairs the system it
// declares with the domain evidence its code shows
func parseSystemEvidence(t *testing.T, dir string) []systemEvidence {
	t.Helper()

	domains := parseSystemDomains(t, "../manifest/definition.go")
	var systems []systemEvidence
	fset := token.NewFileSet()
	for _, n := range packageFiles(t, dir) {
		f, err := parser.ParseFile(fset, dir+"/"+n, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		name := declaredName(f)
		if name == "" {
			continue // a helper file declares no system
		}
		domain, ok := domains[name]
		if !ok {
			if _, known := unregisteredSystems[name]; !known {
				t.Errorf("%s declares system %q, which the manifest does not declare", n, name)
			}
			continue
		}
		e := systemEvidence{
			name: name, domain: domain, file: n,
			creates: map[string]bool{}, draws: map[string]bool{},
			writes: map[string]bool{}, reads: map[string]bool{},
		}
		collectEvidence(f, &e, collectStoreAliases(f))
		systems = append(systems, e)
	}
	return systems
}

// packageFiles returns the sorted non-test sources of dir
func packageFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		names = append(names, n)
	}
	slices.Sort(names) // deterministic diagnostics
	return names
}

// unregisteredSystems declare a system nothing registers, so the manifest carries
// no profile and the boundary suite cannot check them. Empty since Phase 7
// registered network; an entry here is a system escaping the boundary suite.
var unregisteredSystems = map[string]string{}

// parseSystemDomains reads Systems and ContextSystems from the manifest authority.
func parseSystemDomains(t *testing.T, path string) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	domains := make(map[string]string)
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) == 0 || len(value.Values) == 0 {
				continue
			}
			listName := value.Names[0].Name
			if listName != "Systems" && listName != "ContextSystems" {
				continue
			}
			list, ok := value.Values[0].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, elt := range list.Elts {
				lit, ok := elt.(*ast.CompositeLit)
				if !ok {
					continue
				}
				name := keyedString(lit, "Name")
				domain := keyedString(lit, "Domain")
				if name != "" && domain != "" {
					domains[name] = domain
				}
			}
		}
	}

	if len(domains) < 50 {
		t.Fatalf("parsed %d system domains from %s; the table or parser has drifted", len(domains), path)
	}
	return domains
}

// keyedString returns one string field from a keyed composite literal.
func keyedString(lit *ast.CompositeLit, field string) string {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != field {
			continue
		}
		value, ok := kv.Value.(*ast.BasicLit)
		if ok {
			return strings.Trim(value.Value, `"`)
		}
	}
	return ""
}

// TestSystemsDeclareNoDomainMethod pins the single-source rule for domain and dependencies.
// These methods satisfy no interface, so nothing but this test would notice a leftover.
func TestSystemsDeclareNoDomainMethod(t *testing.T) {
	fset := token.NewFileSet()
	for _, n := range packageFiles(t, ".") {
		f, err := parser.ParseFile(fset, "./"+n, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv != nil && (fn.Name.Name == "Domain" || fn.Name.Name == "Requires") {
				t.Errorf("%s: %s declares %s(); the manifest is the declaration site",
					n, boundaryRecvName(fn), fn.Name.Name)
			}
		}
	}
}

// boundaryRecvName returns the receiver's bare type name
func boundaryRecvName(fn *ast.FuncDecl) string {
	e := fn.Recv.List[0].Type
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}

// helperFiles declare no system, so their domain evidence is attributed to nobody.
// Pinned rather than tolerated: a new one is a hole in the boundary suite.
var helperFiles = []string{
	"blast.go",
	"interaction.go",
	"sweep.go",
	"targeting.go",
	"telemetry.go",
}

// TestHelperFilesArePinned fails when the unattributed set changes, in either
// direction: a new helper is unchecked, a vanished one leaves a stale entry.
func TestHelperFilesArePinned(t *testing.T) {
	fset := token.NewFileSet()
	var got []string
	for _, n := range packageFiles(t, ".") {
		f, err := parser.ParseFile(fset, "./"+n, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		if declaredName(f) == "" {
			got = append(got, n)
		}
	}
	want := slices.Clone(helperFiles)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("unattributed file set changed; update helperFiles to:\n\t%#v", got)
	}
}

// declaredName returns the system name the file declares, from its Name method
func declaredName(f *ast.File) string {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Body == nil || fn.Name.Name != "Name" {
			continue
		}
		ret, ok := soleReturn(fn.Body)
		if !ok {
			continue
		}
		if lit, ok := ret.(*ast.BasicLit); ok {
			return strings.Trim(lit.Value, `"`)
		}
	}
	return ""
}

// soleReturn returns the single expression of a one-statement return body
func soleReturn(body *ast.BlockStmt) (ast.Expr, bool) {
	if len(body.List) != 1 {
		return nil, false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil, false
	}
	return ret.Results[0], true
}

// collectEvidence records every entity creation, RNG draw and component store
// access the file makes, resolving hoisted store variables through aliases.
func collectEvidence(f *ast.File, e *systemEvidence, aliases map[string]string) {
	record := func(store, method string) {
		if storeWriters[method] {
			e.writes[store] = true
			return
		}
		e.reads[store] = true
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		switch sel.Sel.Name {
		case "CreateEntity":
			if len(call.Args) == 1 {
				e.creates[domainArg(call.Args[0])] = true
			}
		case "Rand":
			if len(call.Args) == 2 {
				e.draws[domainArg(call.Args[0])] = true
			}
		case "WithDomain", "PushEventDomain":
			e.stamps = true
		}

		// s.world.Components.<Store>.<Method>(...)
		if store, ok := componentStore(sel.X); ok {
			record(store, sel.Sel.Name)
			return true
		}

		// <alias>.<Method>(...) where the alias was bound to a store
		if store, ok := aliases[aliasName(sel.X)]; ok {
			record(store, sel.Sel.Name)
		}
		return true
	})
}

// collectStoreAliases maps local variables and struct fields bound to a component
// store, so a hoisted store is still attributed to the system that uses it.
// Single-value bindings only: a two-value form yields a component, not a store.
func collectStoreAliases(f *ast.File) map[string]string {
	aliases := make(map[string]string)
	bind := func(lhs, rhs ast.Expr) {
		name := aliasName(lhs)
		store, ok := componentStore(rhs)
		if name != "" && ok {
			aliases[name] = store
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.AssignStmt:
			if len(t.Lhs) == 1 && len(t.Rhs) == 1 {
				bind(t.Lhs[0], t.Rhs[0])
			}
		case *ast.ValueSpec:
			if len(t.Names) == 1 && len(t.Values) == 1 {
				bind(t.Names[0], t.Values[0])
			}
		case *ast.KeyValueExpr:
			bind(t.Key, t.Value)
		}
		return true
	})
	return aliases
}

// aliasName reduces a binding target to the identifier a later call selects on
func aliasName(x ast.Expr) string {
	switch t := x.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// domainArg names the domain a core.Domain argument selects; a non-constant
// argument is "stamped", the ambient-domain form of D-7
func domainArg(arg ast.Expr) string {
	sel, ok := arg.(*ast.SelectorExpr)
	if !ok {
		return "stamped"
	}
	switch sel.Sel.Name {
	case "DomainShared":
		return "shared"
	case "DomainPlayer":
		return "player"
	}
	return "stamped"
}

// componentStore returns the store field of a Components selector chain
func componentStore(x ast.Expr) (string, bool) {
	field, ok := x.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	owner, ok := field.X.(*ast.SelectorExpr)
	if !ok || owner.Sel.Name != "Components" {
		return "", false
	}
	return field.Sel.Name, true
}

// sortedStores returns store names in a fixed order for stable diagnostics
func sortedStores(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
