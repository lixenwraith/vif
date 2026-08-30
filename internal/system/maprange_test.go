package system

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// lintDirs are the packages whose iteration order can reach simulation state
var lintDirs = []string{
	".", "../engine", "../mode", "../fsm", "../fsm/std", "../status", "../event",
	"../../pkg/genetic", "../../pkg/genetic/registry", "../../pkg/navigation",
	"../../content",
}

// allowedMapRanges lists iterations whose order provably cannot affect
// simulation state, keyed "Receiver.Func:expr" (or "Func:expr").
//
// The detector matches identifier names, not resolved types, so a slice
// sharing a name with a map field is flagged; such entries are marked.
var allowedMapRanges = map[string]string{
	// --- Collect-then-sort ---
	"sortedKeys:m":                             "collects keys for sorting",
	"Machine.LoadConfigFromMap:States":         "collects state names, sorted before ID assignment",
	"Machine.DeclaredRegions:regionConfigs":    "collects names, sorted before return",
	"Machine.Init:regionInitials":              "collects names, sorted before region init",
	"Machine.RegisteredGuards:guardReg":        "collects names, sorted before return",
	"Machine.RegisteredGuards:guardFactoryReg": "collects names, sorted before return",
	"Machine.RegisteredActions:actionReg":      "collects names, sorted before return",
	"MetricMap.Keys:items":                     "collects keys, sorted before caching",
	"Registry.buildIndex:byGroup":              "collects group names, sorted before index build",
	"CleanerSystem.scanTargetRows:targetRows":  "collects rows, sorted before spawn",
	"digestRecordDifference:Groups":            "collects differing record names, sorted before joining",
	"MacroManager.StartAllPlayback:buffers":    "collects labels, sorted before playback start",

	// --- Writes target a map or distinct keys; order cannot change the result ---
	"Machine.CompilePaths:nodes":                   "writes each node's own Path",
	"Machine.LoadConfigFromMap:Regions":            "writes regionConfigs/regionInitials by key",
	"loadAndResolve:regions":                       "load-time include resolution; merges into a map",
	"mergeStates:addition":                         "load-time; merges into a map",
	"Machine.GetStateID:nodes":                     "returns first name match; names are unique",
	"Machine.capturePayloadVars:captureVars":       "distinct payload fields to distinct variables",
	"ApplyPayloadVars:vars":                        "distinct dot-paths to distinct payload fields",
	"DustSystem.applyAccumulatedImpulses:impulses": "one write per distinct entity; accumulation is dense-ordered",

	// --- Commutative ---
	"Machine.refreshActive:regions":                 "bitwise OR of trigger masks",
	"AdaptationSystem.pruneDrained:Entries":         "deletes only",
	"AdaptationSystem.handleGraphComputed:tracking": "deletes only",
	"AdaptationSystem.updateTelemetry:Entries":      "collects keys, sorted before use",
	"NavigationSystem.Init:groups":                  "per-group resize; independent",
	"NavigationSystem.HandleEvent:groups":           "per-group dirty flag; independent",
	"NavigationSystem.Update:groups":                "per-group field update; recompute sum is commutative",

	// --- Output re-sorted by a unique deterministic key ---
	"MacroManager.Tick:active":         "output sorted by startOrder",
	"MacroManager.ActiveLabels:active": "output sorted by startOrder",

	// --- Detector false positive: parameter is a slice ---
	"AdaptationSystem.applyEXP3:outcomes": "parameter is []routeOutcome, not the map field",
	"FuseSystem.killDrains:drains":        "parameter is []core.Entity, not DustSystem's map field",
}

type finding struct {
	pos  string
	key  string
	expr string
}

// TestNoOrderDependentMapRange reports every map iteration not on the
// allowlist. Go randomizes map order per run, so any such loop whose order
// reaches a write, an RNG draw, or an event push is a divergence source.
func TestNoOrderDependentMapRange(t *testing.T) {
	var findings []finding

	for _, dir := range lintDirs {
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", dir, err)
		}

		for _, pkg := range pkgs {
			files := make([]*ast.File, 0, len(pkg.Files))
			for _, f := range pkg.Files {
				files = append(files, f)
			}
			mapNames := collectMapNames(files)

			for _, f := range files {
				for _, decl := range f.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok {
						continue
					}
					name := funcKey(fn)
					ast.Inspect(fn, func(n ast.Node) bool {
						rs, ok := n.(*ast.RangeStmt)
						if !ok {
							return true
						}
						base := rangeBase(rs.X)
						if base == "" || !mapNames[base] {
							return true
						}
						key := name + ":" + base
						if _, allowed := allowedMapRanges[key]; allowed {
							return true
						}
						findings = append(findings, finding{
							pos: filepath.Join(dir, filepath.Base(fset.Position(rs.Pos()).Filename)) +
								":" + itoaLint(fset.Position(rs.Pos()).Line),
							key:  key,
							expr: base,
						})
						return true
					})
				}
			}
		}
	}

	if len(findings) == 0 {
		return
	}

	slices.SortFunc(findings, func(a, b finding) int { return strings.Compare(a.pos, b.pos) })

	var b strings.Builder
	b.WriteString("order-dependent map iteration (")
	b.WriteString(itoaLint(len(findings)))
	b.WriteString("):\n")
	for _, f := range findings {
		b.WriteString("  " + f.pos + "  " + f.key + "\n")
	}
	b.WriteString("\nEach must be rewritten to iterate a sorted or dense-ordered sequence,\n")
	b.WriteString("or added to allowedMapRanges with the reason its order is irrelevant.")
	t.Fatal(b.String())
}

// collectMapNames gathers every identifier in the package declared with a map
// type: struct fields, vars, short declarations, params, and map-returning funcs
func collectMapNames(files []*ast.File) map[string]bool {
	names := make(map[string]bool)

	addField := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			if _, ok := f.Type.(*ast.MapType); !ok {
				continue
			}
			for _, n := range f.Names {
				names[n.Name] = true
			}
		}
	}

	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch t := n.(type) {
			case *ast.StructType:
				addField(t.Fields)
			case *ast.FuncDecl:
				addField(t.Type.Params)
				if t.Type.Results != nil {
					for _, r := range t.Type.Results.List {
						if _, ok := r.Type.(*ast.MapType); ok {
							names[t.Name.Name] = true
						}
					}
				}
			case *ast.ValueSpec:
				if _, ok := t.Type.(*ast.MapType); ok {
					for _, n := range t.Names {
						names[n.Name] = true
					}
				}
			case *ast.AssignStmt:
				if t.Tok != token.DEFINE {
					return true
				}
				for i, rhs := range t.Rhs {
					if i >= len(t.Lhs) || !isMapExpr(rhs) {
						continue
					}
					if id, ok := t.Lhs[i].(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			}
			return true
		})
	}
	return names
}

// isMapExpr reports whether an expression constructs a map
func isMapExpr(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.CompositeLit:
		_, ok := t.Type.(*ast.MapType)
		return ok
	case *ast.CallExpr:
		if id, ok := t.Fun.(*ast.Ident); ok && id.Name == "make" && len(t.Args) > 0 {
			_, ok := t.Args[0].(*ast.MapType)
			return ok
		}
	}
	return false
}

// rangeBase names the ranged expression; indexing yields the element type,
// never the map itself, so it reports nothing
func rangeBase(x ast.Expr) string {
	switch t := x.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.CallExpr:
		switch fn := t.Fun.(type) {
		case *ast.Ident:
			return fn.Name
		case *ast.SelectorExpr:
			return fn.Sel.Name
		}
	}
	return ""
}

// funcKey renders "Receiver.Func" for methods, "Func" for plain functions
func funcKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if idx, ok := t.(*ast.IndexExpr); ok { // generic receiver
		t = idx.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

func itoaLint(n int) string { return strconv.Itoa(n) }
