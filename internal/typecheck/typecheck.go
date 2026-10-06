// Package typecheck resolves names and type-checks Cero v0.1 programs.
package typecheck

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/token"
	"github.com/deltama37/cero/internal/types"
)

// SymbolKind classifies a declared name.
type SymbolKind int

const (
	SymFunc    SymbolKind = iota // top-level function
	SymParam                     // parameter of a FuncDecl or FuncLit
	SymLocal                     // let binding or variable pattern
	SymCtor                      // constructor
	SymBuiltin                   // built-in function
)

// Builtin identifies a built-in function (ADR-0010).
type Builtin int

const (
	BuiltinStringLength Builtin = iota
	BuiltinStringByteAt
	BuiltinStringSlice
	BuiltinStringFromByte
	BuiltinStringCompare
	BuiltinIntToString
	BuiltinPure
	BuiltinBind
	BuiltinPrint
	BuiltinEPrint
	BuiltinReadStdin
	BuiltinReadFile
	BuiltinFileExists
	BuiltinWriteFile
	BuiltinArgCount
	BuiltinArgAt
	BuiltinExit
)

// Symbol is one declared name. Each declaration (every let, even when
// shadowing) gets its own *Symbol.
type Symbol struct {
	Kind    SymbolKind
	Name    string
	Type    types.Type
	Pos     diag.Pos      // position of the declaring name
	Decl    *ast.FuncDecl // set only for SymFunc
	Ctor    *types.Ctor   // set only for SymCtor
	Builtin Builtin       // set only for SymBuiltin
	// TypeParams are the type parameters Type is generic over: the function's
	// own for a generic SymFunc, Ctor.Data.Params for a SymCtor, the ones
	// created by generalization for a generalized let (SymLocal), and nil
	// otherwise.
	TypeParams []*types.TypeParam
}

// Info records the result of a successful type check.
type Info struct {
	Types    map[ast.Expr]types.Type       // type of every expression node
	Uses     map[*ast.Ident]*Symbol        // symbol referenced by every Ident expression
	Defs     map[*ast.LetStmt]*Symbol      // symbol declared by every let
	Params   map[*ast.Param]*Symbol        // symbol declared by every parameter
	Funcs    map[*ast.FuncDecl]*Symbol     // symbol of every top-level function
	FuncLits map[*ast.FuncLit]*types.Func  // signature of every anonymous function
	Datas    map[*ast.TypeDecl]*types.Data // every type declaration
	CtorPats map[*ast.CtorPat]*types.Ctor  // constructor of every constructor pattern
	PatVars  map[*ast.VarPat]*Symbol       // symbol declared by every variable pattern
	// TypeArgs holds, for every Ident whose symbol has TypeParams (including
	// a generalized let), the type arguments of that use in TypeParams order.
	TypeArgs map[*ast.Ident][]types.Type
	Main     *Symbol // the entry module's 'main'
	MainIO   bool    // main's type is () -> IO[Unit]
	// FuncModule maps every top-level function declaration to its module.
	FuncModule map[*ast.FuncDecl]*Module
}

// Module is one source file of a program.
type Module struct {
	Path    string // module path ("std/list", "syntax/ast"); the entry module's is its file name without ".cero"
	File    string // file name used in error messages ("std/list.cero", "examples/app/main.cero")
	Ast     *ast.File
	Imports []*Module // the modules of Ast.Imports, in the same order
}

// importedValue is a function or constructor brought in by import.
type importedValue struct {
	sym  *Symbol
	from *Module
}

// importedData is a type brought in by import.
type importedData struct {
	data *types.Data
	from *Module
}

// exports is what a module makes visible to its importers.
type exports struct {
	values map[string]*Symbol     // pub functions and the constructors of pub types
	datas  map[string]*types.Data // pub types
}

// Check type-checks a program made of file alone. On error it returns
// (nil, *diag.Error) with an empty File.
func Check(file *ast.File) (*Info, error) {
	return CheckProgram([]*Module{{Path: "main", Ast: file}})
}

// CheckProgram type-checks mods, which are in dependency order: every module
// appears after the modules it imports, and the last one is the entry
// module. Errors are *diag.Error with File set to the module's File.
func CheckProgram(mods []*Module) (*Info, error) {
	info := &Info{
		Types:      make(map[ast.Expr]types.Type),
		Uses:       make(map[*ast.Ident]*Symbol),
		Defs:       make(map[*ast.LetStmt]*Symbol),
		Params:     make(map[*ast.Param]*Symbol),
		Funcs:      make(map[*ast.FuncDecl]*Symbol),
		FuncLits:   make(map[*ast.FuncLit]*types.Func),
		Datas:      make(map[*ast.TypeDecl]*types.Data),
		CtorPats:   make(map[*ast.CtorPat]*types.Ctor),
		PatVars:    make(map[*ast.VarPat]*Symbol),
		TypeArgs:   make(map[*ast.Ident][]types.Type),
		FuncModule: make(map[*ast.FuncDecl]*Module),
	}
	exp := make(map[*Module]*exports, len(mods))
	for i, mod := range mods {
		c := newChecker(info)
		if err := c.bindImports(mod, exp); err != nil {
			err.File = mod.File
			return nil, err
		}
		if err := c.checkModule(mod.Ast, i == len(mods)-1); err != nil {
			err.File = mod.File
			return nil, err
		}
		exp[mod] = c.moduleExports(mod.Ast)
		for _, d := range mod.Ast.Funcs {
			info.FuncModule[d] = mod
		}
		c.resolveInfo()
	}
	if len(mods) > 0 {
		entry := mods[len(mods)-1]
		for _, d := range entry.Ast.Funcs {
			if d.Name == "main" {
				info.Main = info.Funcs[d]
				break
			}
		}
	}
	return info, nil
}

func (c *checker) bindImports(mod *Module, exp map[*Module]*exports) *diag.Error {
	if len(mod.Imports) != len(mod.Ast.Imports) {
		panic("typecheck: module imports are not linked")
	}
	seen := make(map[*Module]bool, len(mod.Ast.Imports))
	for i, imp := range mod.Ast.Imports {
		dep := mod.Imports[i]
		if seen[dep] {
			return diag.Errorf(imp.Pos, "duplicate import '%s'", imp.Path)
		}
		seen[dep] = true
		ex, ok := exp[dep]
		if !ok {
			panic(fmt.Sprintf("typecheck: module %q is imported before it is checked", dep.Path))
		}
		for name, sym := range ex.values {
			if prev, exists := c.importedValues[name]; exists && prev.from != dep {
				return diag.Errorf(imp.Pos, "'%s' is imported from both '%s' and '%s'", name, prev.from.Path, dep.Path)
			}
			c.importedValues[name] = importedValue{sym: sym, from: dep}
		}
		for name, data := range ex.datas {
			if prev, exists := c.importedDatas[name]; exists && prev.from != dep {
				return diag.Errorf(imp.Pos, "'%s' is imported from both '%s' and '%s'", name, prev.from.Path, dep.Path)
			}
			c.importedDatas[name] = importedData{data: data, from: dep}
		}
	}
	return nil
}

func (c *checker) moduleExports(file *ast.File) *exports {
	out := &exports{
		values: make(map[string]*Symbol),
		datas:  make(map[string]*types.Data),
	}
	for _, d := range file.Funcs {
		if d.Pub {
			out.values[d.Name] = c.info.Funcs[d]
		}
	}
	for _, d := range file.Types {
		if !d.Pub {
			continue
		}
		data := c.info.Datas[d]
		out.datas[d.Name] = data
		for _, ctor := range data.Ctors {
			out.values[ctor.Name] = c.globals[ctor.Name]
		}
	}
	return out
}

// checkModule type-checks file. main is required, and must have type
// () -> Int or () -> IO[Unit], only when isEntry is set.
func (c *checker) checkModule(file *ast.File, isEntry bool) *diag.Error {
	c.isEntry = isEntry

	// Type names are registered before constructors so a field type can refer
	// to any declared type, including this one and ones declared later.
	for _, d := range file.Types {
		if d.Name == "Int" || d.Name == "Bool" || d.Name == "String" || d.Name == "Unit" || d.Name == "IO" {
			return diag.Errorf(d.NamePos, "cannot redefine built-in type '%s'", d.Name)
		}
		if _, ok := c.datas[d.Name]; ok {
			return diag.Errorf(d.NamePos, "duplicate type '%s'", d.Name)
		}
		data := &types.Data{Name: d.Name}
		c.datas[d.Name] = data
		c.info.Datas[d] = data
	}

	for _, d := range file.Types {
		params, err := c.declareTypeParams(d.TypeParams)
		if err != nil {
			return err
		}
		c.datas[d.Name].Params = params
	}

	for _, d := range file.Types {
		data := c.datas[d.Name]
		c.tparams = scopeOf(data.Params)
		for i, cd := range d.Ctors {
			if sym := c.globals[cd.Name]; sym != nil && sym.Kind == SymBuiltin {
				return diag.Errorf(cd.Pos, "constructor '%s' conflicts with built-in function '%s'", cd.Name, cd.Name)
			}
			if _, ok := c.ctors[cd.Name]; ok {
				return diag.Errorf(cd.Pos, "duplicate constructor '%s'", cd.Name)
			}
			fields := make([]types.Type, len(cd.Fields))
			for j, f := range cd.Fields {
				ft, err := c.resolveType(f)
				if err != nil {
					return err
				}
				fields[j] = ft
			}
			ctor := &types.Ctor{
				Name:   cd.Name,
				Index:  i,
				Fields: fields,
				Data:   data,
			}
			data.Ctors = append(data.Ctors, ctor)
			c.ctors[cd.Name] = ctor
			c.globals[cd.Name] = &Symbol{
				Kind:       SymCtor,
				Name:       cd.Name,
				Type:       ctorType(ctor),
				Pos:        cd.Pos,
				Ctor:       ctor,
				TypeParams: data.Params,
			}
		}
		c.tparams = nil
	}

	for _, d := range file.Funcs {
		if sym := c.globals[d.Name]; sym != nil && sym.Kind == SymBuiltin {
			return diag.Errorf(d.NamePos, "function '%s' conflicts with built-in function '%s'", d.Name, d.Name)
		}
		if sym := c.globals[d.Name]; sym != nil && sym.Kind == SymCtor {
			return diag.Errorf(d.NamePos, "function '%s' conflicts with constructor '%s'", d.Name, d.Name)
		}
		if _, ok := c.globals[d.Name]; ok {
			return diag.Errorf(d.NamePos, "duplicate function '%s'", d.Name)
		}
		annotated := isAnnotated(d)
		if !annotated && len(d.TypeParams) > 0 {
			return diag.Errorf(d.TypeParams[0].Pos, "function '%s' declares type parameters, so every parameter and its result need a type annotation", d.Name)
		}
		sym := &Symbol{
			Kind: SymFunc,
			Name: d.Name,
			Pos:  d.NamePos,
			Decl: d,
		}
		if annotated {
			tps, err := c.declareTypeParams(d.TypeParams)
			if err != nil {
				return err
			}
			c.tparams = scopeOf(tps)
			sig, err := c.resolveFuncType(d.Params, d.Result)
			if err != nil {
				return err
			}
			for j, tp := range tps {
				if !types.Mentions(sig, tp) {
					return diag.Errorf(d.TypeParams[j].Pos, "type parameter '%s' is not used in the signature of '%s'", tp.Name, d.Name)
				}
			}
			c.tparams = nil
			sym.Type = sig
			sym.TypeParams = tps
		}
		c.globals[d.Name] = sym
		c.info.Funcs[d] = sym
	}

	var inferred []*ast.FuncDecl
	for _, d := range file.Funcs {
		if !isAnnotated(d) {
			inferred = append(inferred, d)
		}
	}
	indexOf := make(map[string]int, len(inferred))
	for i, d := range inferred {
		indexOf[d.Name] = i
	}
	edges := make([][]int, len(inferred))
	for i, d := range inferred {
		for _, name := range funcRefs(d.Params, d.Body, func(name string) bool {
			sym := c.globals[name]
			return sym != nil && sym.Kind == SymFunc
		}) {
			if j, ok := indexOf[name]; ok {
				edges[i] = append(edges[i], j)
			}
		}
	}
	for _, comp := range sccs(len(inferred), edges) {
		decls := make([]*ast.FuncDecl, len(comp))
		for i, idx := range comp {
			decls[i] = inferred[idx]
		}
		if err := c.checkComponent(decls); err != nil {
			return err
		}
	}

	for _, d := range file.Funcs {
		if !isAnnotated(d) {
			continue
		}
		sym := c.info.Funcs[d]
		c.tparams = scopeOf(sym.TypeParams)
		c.metas = nil
		c.fresh = 0
		c.current = sym
		if err := c.checkFunc(&funcCtx{}, d.Params, sym.Type.(*types.Func), d.Body); err != nil {
			return err
		}
		if err := c.checkSolved(); err != nil {
			return err
		}
		c.tparams = nil
	}
	c.current = nil

	if !isEntry {
		return nil
	}
	main, ok := c.globals["main"]
	if !ok {
		return diag.Errorf(diag.Pos{Line: 1, Col: 1}, "missing function 'main'")
	}
	if len(main.TypeParams) > 0 {
		return diag.Errorf(main.Pos, "function 'main' cannot have type parameters")
	}
	if !isMainType(main.Type) {
		return mainTypeError(main.Pos, main.Type)
	}
	c.info.MainIO = isMainIO(main.Type)
	return nil
}

func mainTypeError(pos diag.Pos, t types.Type) *diag.Error {
	return diag.Errorf(pos, "function 'main' must have type () -> Int or () -> IO[Unit], found %s", t)
}

func isMainType(t types.Type) bool {
	fn, ok := t.(*types.Func)
	if !ok || len(fn.Params) != 0 {
		return false
	}
	return types.Equal(fn.Result, types.Int) || isIOUnit(fn.Result)
}

func isMainIO(t types.Type) bool {
	fn, ok := t.(*types.Func)
	return ok && len(fn.Params) == 0 && isIOUnit(fn.Result)
}

func isIOUnit(t types.Type) bool {
	return types.Equal(t, types.IOOf(types.Unit))
}

type checker struct {
	info           *Info
	globals        map[string]*Symbol
	datas          map[string]*types.Data
	ctors          map[string]*types.Ctor
	importedValues map[string]importedValue // imported functions and constructors
	importedDatas  map[string]importedData  // imported types
	isEntry        bool
	tparams        map[string]*types.TypeParam // type parameters in scope; nil outside generic declarations
	metas          []*pendingMeta              // created while checking the current top-level function, in creation order
	fresh          int                         // number of t1, t2, ... names used in the current top-level function
	// comp holds the symbols of the inferred functions being checked
	// together, while checkComponent runs; nil otherwise. A reference to
	// one of them is not instantiated, and generalize leaves the metas in
	// their types alone.
	comp map[*Symbol]bool
	// compRefs records the references to symbols in comp, with the
	// function whose body contains each one.
	compRefs []compRef
	// compTPs are the type parameters created by generalizeTop while
	// checking the current component.
	compTPs map[*types.TypeParam]bool
	// current is the top-level function being checked.
	current *Symbol
}

type compRef struct {
	id    *ast.Ident
	sym   *Symbol // the referenced function
	owner *Symbol // the function whose body contains id
}

// pendingMeta remembers where a unification variable was created so that an
// unsolved one can be reported.
type pendingMeta struct {
	meta  *types.Meta
	pos   diag.Pos
	msg   string  // the error message reported when meta is still unsolved
	owner *Symbol // top-level function being checked when meta was created
}

// isAnnotated reports whether every parameter and the result of d have a
// type annotation.
func isAnnotated(d *ast.FuncDecl) bool {
	if d.Result == nil {
		return false
	}
	for _, p := range d.Params {
		if p.Type == nil {
			return false
		}
	}
	return true
}

func (c *checker) checkComponent(decls []*ast.FuncDecl) *diag.Error {
	c.metas = nil
	c.fresh = 0
	c.comp = make(map[*Symbol]bool, len(decls))
	c.compRefs = nil
	c.compTPs = make(map[*types.TypeParam]bool)

	for _, d := range decls {
		sym := c.info.Funcs[d]
		sig := &types.Func{Params: make([]types.Type, len(d.Params))}
		c.current = sym
		for i, p := range d.Params {
			if p.Type != nil {
				pt, err := c.resolveType(p.Type)
				if err != nil {
					return err
				}
				sig.Params[i] = pt
				continue
			}
			msg := fmt.Sprintf("cannot infer the type of parameter '%s'; add a type annotation", p.Name)
			sig.Params[i] = c.newMeta(c.freshName(), p.Pos, msg)
		}
		if d.Result != nil {
			rt, err := c.resolveType(d.Result)
			if err != nil {
				return err
			}
			sig.Result = rt
		} else {
			msg := fmt.Sprintf("cannot infer the result type of '%s'; add a type annotation", d.Name)
			sig.Result = c.newMeta(c.freshName(), d.NamePos, msg)
		}
		if c.isEntry && d.Name == "main" && len(d.Params) > 0 {
			return mainTypeError(sym.Pos, sig)
		}
		sym.Type = sig
		c.comp[sym] = true
	}

	for _, d := range decls {
		sym := c.info.Funcs[d]
		c.current = sym
		if err := c.checkFunc(&funcCtx{}, d.Params, sym.Type.(*types.Func), d.Body); err != nil {
			return err
		}
	}

	// An inferred main whose result is still a unification variable defaults to Int.
	for _, d := range decls {
		if !c.isEntry || d.Name != "main" {
			continue
		}
		sig := c.info.Funcs[d].Type.(*types.Func)
		if _, ok := types.Prune(sig.Result).(*types.Meta); ok {
			if !unify(sig.Result, types.Int) {
				return mainTypeError(c.info.Funcs[d].Pos, sig)
			}
		}
	}

	for _, d := range decls {
		sym := c.info.Funcs[d]
		t, tps := c.generalizeTop(sym.Type)
		sym.Type = t
		sym.TypeParams = tps
	}

	if err := c.checkComponentMetas(); err != nil {
		return err
	}

	for _, r := range c.compRefs {
		if len(r.sym.TypeParams) > 0 {
			args := make([]types.Type, len(r.sym.TypeParams))
			for i, tp := range r.sym.TypeParams {
				args[i] = tp
			}
			c.info.TypeArgs[r.id] = args
		}
		for _, tp := range r.sym.TypeParams {
			if containsTypeParam(r.owner.TypeParams, tp) {
				continue
			}
			return diag.Errorf(
				r.id.Pos,
				"cannot infer type argument '%s' of '%s'; add a type annotation",
				tp.Name,
				r.sym.Name,
			)
		}
	}

	c.comp = nil
	c.compRefs = nil
	c.compTPs = nil
	c.current = nil
	return nil
}

// generalizeTop turns the unsolved metas of t into type parameters, in order
// of first occurrence in t, and also includes type parameters created earlier
// in this component that occur in t. Shared metas are solved by the first
// function that generalizes them, so a later function reuses those type
// parameters.
func (c *checker) generalizeTop(t types.Type) (types.Type, []*types.TypeParam) {
	var tps []*types.TypeParam
	seenMeta := make(map[*types.Meta]bool)
	seenTP := make(map[*types.TypeParam]bool)
	var walk func(types.Type)
	walk = func(t types.Type) {
		t = types.Prune(t)
		switch t := t.(type) {
		case *types.Meta:
			if seenMeta[t] {
				return
			}
			seenMeta[t] = true
			tp := &types.TypeParam{Name: t.Name}
			t.Solution = tp
			c.compTPs[tp] = true
			seenTP[tp] = true
			tps = append(tps, tp)
		case *types.TypeParam:
			if !c.compTPs[t] || seenTP[t] {
				return
			}
			seenTP[t] = true
			tps = append(tps, t)
		case *types.Func:
			for _, p := range t.Params {
				walk(p)
			}
			walk(t.Result)
		case *types.Named:
			for _, a := range t.Args {
				walk(a)
			}
		}
	}
	walk(t)
	if len(tps) == 0 {
		return types.Resolve(t), nil
	}
	return types.Resolve(t), tps
}

func (c *checker) checkComponentMetas() *diag.Error {
	for _, pm := range c.metas {
		if _, ok := types.Prune(pm.meta).(*types.Meta); ok {
			return diag.Errorf(pm.pos, "%s", pm.msg)
		}
		resolved := types.Resolve(pm.meta)
		for sym := range c.comp {
			for _, tp := range sym.TypeParams {
				if !types.Mentions(resolved, tp) {
					continue
				}
				if pm.owner != nil && containsTypeParam(pm.owner.TypeParams, tp) {
					continue
				}
				return diag.Errorf(pm.pos, "%s", pm.msg)
			}
		}
	}
	return nil
}

func containsTypeParam(tps []*types.TypeParam, tp *types.TypeParam) bool {
	for _, p := range tps {
		if p == tp {
			return true
		}
	}
	return false
}

// scopeOf returns a name-to-parameter map, or nil when ps is empty.
func scopeOf(ps []*types.TypeParam) map[string]*types.TypeParam {
	if len(ps) == 0 {
		return nil
	}
	m := make(map[string]*types.TypeParam, len(ps))
	for _, p := range ps {
		m[p.Name] = p
	}
	return m
}

func newChecker(info *Info) *checker {
	c := &checker{
		info:           info,
		globals:        make(map[string]*Symbol),
		datas:          make(map[string]*types.Data),
		ctors:          make(map[string]*types.Ctor),
		importedValues: make(map[string]importedValue),
		importedDatas:  make(map[string]importedData),
	}
	c.registerBuiltins()
	return c
}

// registerBuiltins adds the built-in functions (ADR-0010, ADR-0012) to globals.
func (c *checker) registerBuiltins() {
	tParam := &types.TypeParam{Name: "T"}
	aParam := &types.TypeParam{Name: "A"}
	bParam := &types.TypeParam{Name: "B"}
	ioUnit := types.IOOf(types.Unit)
	specs := []struct {
		name string
		b    Builtin
		sig  types.Type
		tps  []*types.TypeParam
	}{
		{"stringLength", BuiltinStringLength, &types.Func{Params: []types.Type{types.String}, Result: types.Int}, nil},
		{"stringByteAt", BuiltinStringByteAt, &types.Func{Params: []types.Type{types.String, types.Int}, Result: types.Int}, nil},
		{"stringSlice", BuiltinStringSlice, &types.Func{Params: []types.Type{types.String, types.Int, types.Int}, Result: types.String}, nil},
		{"stringFromByte", BuiltinStringFromByte, &types.Func{Params: []types.Type{types.Int}, Result: types.String}, nil},
		{"stringCompare", BuiltinStringCompare, &types.Func{Params: []types.Type{types.String, types.String}, Result: types.Int}, nil},
		{"intToString", BuiltinIntToString, &types.Func{Params: []types.Type{types.Int}, Result: types.String}, nil},
		{"pure", BuiltinPure, &types.Func{Params: []types.Type{tParam}, Result: types.IOOf(tParam)}, []*types.TypeParam{tParam}},
		{"bind", BuiltinBind, &types.Func{
			Params: []types.Type{
				types.IOOf(aParam),
				&types.Func{Params: []types.Type{aParam}, Result: types.IOOf(bParam)},
			},
			Result: types.IOOf(bParam),
		}, []*types.TypeParam{aParam, bParam}},
		{"print", BuiltinPrint, &types.Func{Params: []types.Type{types.String}, Result: ioUnit}, nil},
		{"eprint", BuiltinEPrint, &types.Func{Params: []types.Type{types.String}, Result: ioUnit}, nil},
		{"readStdin", BuiltinReadStdin, &types.Func{Result: types.IOOf(types.String)}, nil},
		{"readFile", BuiltinReadFile, &types.Func{Params: []types.Type{types.String}, Result: types.IOOf(types.String)}, nil},
		{"fileExists", BuiltinFileExists, &types.Func{Params: []types.Type{types.String}, Result: types.IOOf(types.Bool)}, nil},
		{"writeFile", BuiltinWriteFile, &types.Func{Params: []types.Type{types.String, types.String}, Result: ioUnit}, nil},
		{"argCount", BuiltinArgCount, &types.Func{Result: types.IOOf(types.Int)}, nil},
		{"argAt", BuiltinArgAt, &types.Func{Params: []types.Type{types.Int}, Result: types.IOOf(types.String)}, nil},
		{"exit", BuiltinExit, &types.Func{Params: []types.Type{types.Int}, Result: ioUnit}, nil},
	}
	for _, spec := range specs {
		c.globals[spec.name] = &Symbol{
			Kind:       SymBuiltin,
			Name:       spec.name,
			Type:       spec.sig,
			Builtin:    spec.b,
			TypeParams: spec.tps,
		}
	}
}

func (c *checker) declareTypeParams(ps []*ast.TypeParam) ([]*types.TypeParam, *diag.Error) {
	if len(ps) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(ps))
	out := make([]*types.TypeParam, 0, len(ps))
	for _, p := range ps {
		if p.Name == "Int" || p.Name == "Bool" || p.Name == "String" || p.Name == "Unit" || p.Name == "IO" || c.lookupData(p.Name) != nil {
			return nil, diag.Errorf(p.Pos, "type parameter '%s' conflicts with type '%s'", p.Name, p.Name)
		}
		if seen[p.Name] {
			return nil, diag.Errorf(p.Pos, "duplicate type parameter '%s'", p.Name)
		}
		seen[p.Name] = true
		out = append(out, &types.TypeParam{Name: p.Name})
	}
	return out, nil
}

// funcCtx is the scope stack of one function (a FuncDecl or a FuncLit).
// parent is the function that encloses an anonymous function.
type funcCtx struct {
	parent *funcCtx
	scope  *scope
}

type scope struct {
	parent *scope
	names  map[string]*Symbol
}

func (c *checker) resolveFuncType(params []*ast.Param, result ast.TypeExpr) (*types.Func, *diag.Error) {
	sig := &types.Func{Params: make([]types.Type, len(params))}
	for i, p := range params {
		pt, err := c.resolveType(p.Type)
		if err != nil {
			return nil, err
		}
		sig.Params[i] = pt
	}
	rt, err := c.resolveType(result)
	if err != nil {
		return nil, err
	}
	sig.Result = rt
	return sig, nil
}

func (c *checker) resolveType(t ast.TypeExpr) (types.Type, *diag.Error) {
	switch t := t.(type) {
	case *ast.NamedType:
		m := len(t.Args)
		if tp := c.tparams[t.Name]; tp != nil {
			if m != 0 {
				return nil, diag.Errorf(t.Pos, "wrong number of type arguments for '%s': expected 0, found %d", t.Name, m)
			}
			return tp, nil
		}
		if t.Name == "Int" || t.Name == "Bool" || t.Name == "String" || t.Name == "Unit" {
			if m != 0 {
				return nil, diag.Errorf(t.Pos, "wrong number of type arguments for '%s': expected 0, found %d", t.Name, m)
			}
			switch t.Name {
			case "Int":
				return types.Int, nil
			case "Bool":
				return types.Bool, nil
			case "Unit":
				return types.Unit, nil
			default:
				return types.String, nil
			}
		}
		if t.Name == "IO" {
			if m != 1 {
				return nil, diag.Errorf(t.Pos, "wrong number of type arguments for 'IO': expected 1, found %d", m)
			}
			arg, err := c.resolveType(t.Args[0])
			if err != nil {
				return nil, err
			}
			return &types.Named{Data: types.IO, Args: []types.Type{arg}}, nil
		}
		if data := c.lookupData(t.Name); data != nil {
			n := len(data.Params)
			if m != n {
				return nil, diag.Errorf(t.Pos, "wrong number of type arguments for '%s': expected %d, found %d", t.Name, n, m)
			}
			if n == 0 {
				return &types.Named{Data: data}, nil
			}
			args := make([]types.Type, m)
			for i, a := range t.Args {
				at, err := c.resolveType(a)
				if err != nil {
					return nil, err
				}
				args[i] = at
			}
			return &types.Named{Data: data, Args: args}, nil
		}
		return nil, diag.Errorf(t.Pos, "unknown type '%s'", t.Name)
	case *ast.FuncType:
		params := make([]types.Type, len(t.Params))
		for i, p := range t.Params {
			pt, err := c.resolveType(p)
			if err != nil {
				return nil, err
			}
			params[i] = pt
		}
		result, err := c.resolveType(t.Result)
		if err != nil {
			return nil, err
		}
		return &types.Func{Params: params, Result: result}, nil
	default:
		return nil, diag.Errorf(t.Position(), "unknown type '%T'", t)
	}
}

func (c *checker) checkFunc(
	ctx *funcCtx,
	params []*ast.Param,
	sig *types.Func,
	body *ast.BlockExpr,
) *diag.Error {
	ctx.scope = &scope{names: make(map[string]*Symbol)}
	for i, p := range params {
		if err := c.checkBindable(p.Name, p.Pos); err != nil {
			return err
		}
		if _, exists := ctx.scope.names[p.Name]; exists {
			return diag.Errorf(p.Pos, "duplicate parameter '%s'", p.Name)
		}
		sym := &Symbol{
			Kind: SymParam,
			Name: p.Name,
			Type: sig.Params[i],
			Pos:  p.Pos,
		}
		c.info.Params[p] = sym
		ctx.scope.names[p.Name] = sym
	}
	if sig.Result == nil {
		t, err := c.infer(ctx, body)
		if err != nil {
			return err
		}
		sig.Result = t
		return nil
	}
	return c.expect(ctx, body, sig.Result)
}

func (c *checker) pushScope(ctx *funcCtx) {
	ctx.scope = &scope{parent: ctx.scope, names: make(map[string]*Symbol)}
}

func (c *checker) popScope(ctx *funcCtx) {
	ctx.scope = ctx.scope.parent
}

// lookup walks scopes from the inside out: the current function, then
// enclosing functions, then the module's declarations, built-ins, and imports.
func (c *checker) lookup(ctx *funcCtx, name string) *Symbol {
	for s := ctx.scope; s != nil; s = s.parent {
		if found, ok := s.names[name]; ok {
			return found
		}
	}
	for p := ctx.parent; p != nil; p = p.parent {
		for s := p.scope; s != nil; s = s.parent {
			if found, ok := s.names[name]; ok {
				return found
			}
		}
	}
	return c.lookupGlobal(name)
}

// lookupGlobal returns the module's own declaration or built-in named name,
// or else the imported one.
func (c *checker) lookupGlobal(name string) *Symbol {
	if sym, ok := c.globals[name]; ok {
		return sym
	}
	if imp, ok := c.importedValues[name]; ok {
		return imp.sym
	}
	return nil
}

// lookupData returns the module's own type named name, or else the imported one.
func (c *checker) lookupData(name string) *types.Data {
	if data, ok := c.datas[name]; ok {
		return data
	}
	if imp, ok := c.importedDatas[name]; ok {
		return imp.data
	}
	return nil
}

// lookupCtor returns the constructor named name when lookupGlobal finds one.
func (c *checker) lookupCtor(name string) *types.Ctor {
	sym := c.lookupGlobal(name)
	if sym != nil && sym.Kind == SymCtor {
		return sym.Ctor
	}
	return nil
}

func (c *checker) expect(ctx *funcCtx, e ast.Expr, want types.Type) *diag.Error {
	got, err := c.infer(ctx, e)
	if err != nil {
		return err
	}
	if !unify(want, got) {
		return diag.Errorf(resultPos(e), "expected %s, found %s", want, got)
	}
	return nil
}

func resultPos(e ast.Expr) diag.Pos {
	if b, ok := e.(*ast.BlockExpr); ok {
		return resultPos(b.Result)
	}
	return e.Position()
}

func (c *checker) infer(ctx *funcCtx, e ast.Expr) (types.Type, *diag.Error) {
	typ, err := c.inferExpr(ctx, e)
	if err != nil {
		return nil, err
	}
	c.info.Types[e] = typ
	return typ, nil
}

func (c *checker) inferExpr(ctx *funcCtx, e ast.Expr) (types.Type, *diag.Error) {
	switch e := e.(type) {
	case *ast.IntLit:
		return types.Int, nil
	case *ast.StringLit:
		return types.String, nil
	case *ast.BoolLit:
		return types.Bool, nil
	case *ast.UnitLit:
		return types.Unit, nil
	case *ast.Ident:
		return c.inferIdent(ctx, e)
	case *ast.UnaryExpr:
		return c.inferUnary(ctx, e)
	case *ast.BinaryExpr:
		return c.inferBinary(ctx, e)
	case *ast.CallExpr:
		return c.inferCall(ctx, e)
	case *ast.IfExpr:
		return c.inferIf(ctx, e)
	case *ast.BlockExpr:
		return c.inferBlock(ctx, e)
	case *ast.FuncLit:
		return c.inferFuncLit(ctx, e)
	case *ast.MatchExpr:
		return c.inferMatch(ctx, e)
	default:
		return nil, diag.Errorf(e.Position(), "unhandled expression %T", e)
	}
}

func (c *checker) inferIdent(ctx *funcCtx, e *ast.Ident) (types.Type, *diag.Error) {
	sym := c.lookup(ctx, e.Name)
	if sym == nil {
		return nil, diag.Errorf(e.Pos, "undefined name '%s'", e.Name)
	}
	if sym.Kind == SymCtor && len(sym.Ctor.Fields) > 0 {
		return nil, diag.Errorf(e.Pos, "constructor '%s' cannot be used as a value; call it with its fields", sym.Name)
	}
	c.info.Uses[e] = sym
	if c.comp[sym] {
		c.compRefs = append(c.compRefs, compRef{id: e, sym: sym, owner: c.current})
		return sym.Type, nil
	}
	if sym.Type == nil {
		panic(fmt.Sprintf("typecheck: function '%s' is used before its type is inferred", sym.Name))
	}
	return c.instantiate(sym, e), nil
}

// newMeta creates a unification variable named name and remembers it so
// that checkSolved can report msg at pos if it stays unsolved.
func (c *checker) newMeta(name string, pos diag.Pos, msg string) *types.Meta {
	m := &types.Meta{Name: name}
	c.metas = append(c.metas, &pendingMeta{meta: m, pos: pos, msg: msg, owner: c.current})
	return m
}

// freshName returns the next of "t1", "t2", ... for the current top-level function.
func (c *checker) freshName() string {
	c.fresh++
	return "t" + strconv.Itoa(c.fresh)
}

// instantiate returns sym.Type with sym.TypeParams replaced by fresh metas
// and records the metas in Info.TypeArgs[id]. For a symbol without type
// parameters it returns sym.Type and records nothing.
func (c *checker) instantiate(sym *Symbol, id *ast.Ident) types.Type {
	if len(sym.TypeParams) == 0 {
		return sym.Type
	}
	args := make([]types.Type, len(sym.TypeParams))
	for i, tp := range sym.TypeParams {
		msg := fmt.Sprintf("cannot infer type argument '%s' of '%s'; add a type annotation", tp.Name, sym.Name)
		args[i] = c.newMeta(tp.Name, id.Pos, msg)
	}
	c.info.TypeArgs[id] = args
	return types.Subst(sym.Type, sym.TypeParams, args)
}

func (c *checker) inferUnary(ctx *funcCtx, e *ast.UnaryExpr) (types.Type, *diag.Error) {
	switch e.Op {
	case token.Minus:
		if err := c.expect(ctx, e.X, types.Int); err != nil {
			return nil, err
		}
		return types.Int, nil
	case token.Bang:
		if err := c.expect(ctx, e.X, types.Bool); err != nil {
			return nil, err
		}
		return types.Bool, nil
	default:
		return nil, diag.Errorf(e.Pos, "unhandled unary operator %s", e.Op)
	}
}

func (c *checker) inferBinary(ctx *funcCtx, e *ast.BinaryExpr) (types.Type, *diag.Error) {
	switch e.Op {
	case token.Plus, token.Minus, token.Star, token.Slash:
		if err := c.expect(ctx, e.X, types.Int); err != nil {
			return nil, err
		}
		if err := c.expect(ctx, e.Y, types.Int); err != nil {
			return nil, err
		}
		return types.Int, nil
	case token.PlusPlus:
		if err := c.expect(ctx, e.X, types.String); err != nil {
			return nil, err
		}
		if err := c.expect(ctx, e.Y, types.String); err != nil {
			return nil, err
		}
		return types.String, nil
	case token.Lt, token.LtEq, token.Gt, token.GtEq:
		if err := c.expect(ctx, e.X, types.Int); err != nil {
			return nil, err
		}
		if err := c.expect(ctx, e.Y, types.Int); err != nil {
			return nil, err
		}
		return types.Bool, nil
	case token.Eq, token.NotEq:
		t, err := c.infer(ctx, e.X)
		if err != nil {
			return nil, err
		}
		if _, ok := types.Prune(t).(*types.Meta); ok {
			if err := c.expect(ctx, e.Y, t); err != nil {
				return nil, err
			}
			if !comparable(t) {
				return nil, diag.Errorf(resultPos(e.X), "cannot compare values of type %s", t)
			}
			return types.Bool, nil
		}
		if !comparable(t) {
			return nil, diag.Errorf(resultPos(e.X), "cannot compare values of type %s", t)
		}
		if err := c.expect(ctx, e.Y, t); err != nil {
			return nil, err
		}
		return types.Bool, nil
	case token.AndAnd, token.OrOr:
		if err := c.expect(ctx, e.X, types.Bool); err != nil {
			return nil, err
		}
		if err := c.expect(ctx, e.Y, types.Bool); err != nil {
			return nil, err
		}
		return types.Bool, nil
	default:
		return nil, diag.Errorf(e.OpPos, "unhandled binary operator %s", e.Op)
	}
}

func comparable(t types.Type) bool {
	return types.Equal(t, types.Int) || types.Equal(t, types.Bool) || types.Equal(t, types.String)
}

func (c *checker) inferCall(ctx *funcCtx, e *ast.CallExpr) (types.Type, *diag.Error) {
	if id, ok := e.Fn.(*ast.Ident); ok {
		if sym := c.lookup(ctx, id.Name); sym != nil && sym.Kind == SymCtor {
			return c.inferCtorCall(ctx, e, id, sym)
		}
	}
	ft, err := c.infer(ctx, e.Fn)
	if err != nil {
		return nil, err
	}
	switch callee := types.Prune(ft).(type) {
	case *types.Func:
		if len(e.Args) != len(callee.Params) {
			return nil, diag.Errorf(e.LParen, "wrong number of arguments: expected %d, found %d", len(callee.Params), len(e.Args))
		}
		for i, arg := range e.Args {
			if err := c.expect(ctx, arg, callee.Params[i]); err != nil {
				return nil, err
			}
		}
		return callee.Result, nil
	case *types.Meta:
		argTypes := make([]types.Type, len(e.Args))
		for i, arg := range e.Args {
			at, err := c.infer(ctx, arg)
			if err != nil {
				return nil, err
			}
			argTypes[i] = at
		}
		res := c.newMeta(c.freshName(), e.LParen, "cannot infer the result type of this call; add a type annotation")
		want := &types.Func{Params: argTypes, Result: res}
		if !unify(want, callee) {
			return nil, diag.Errorf(resultPos(e.Fn), "expected %s, found %s", want, ft)
		}
		return res, nil
	default:
		return nil, diag.Errorf(resultPos(e.Fn), "cannot call non-function value of type %s", ft)
	}
}

func (c *checker) inferIf(ctx *funcCtx, e *ast.IfExpr) (types.Type, *diag.Error) {
	if err := c.expect(ctx, e.Cond, types.Bool); err != nil {
		return nil, err
	}
	tt, err := c.infer(ctx, e.Then)
	if err != nil {
		return nil, err
	}
	et, err := c.infer(ctx, e.Else)
	if err != nil {
		return nil, err
	}
	if !unify(tt, et) {
		return nil, diag.Errorf(resultPos(e.Else), "if branches have different types: %s and %s", tt, et)
	}
	return tt, nil
}

func (c *checker) inferBlock(ctx *funcCtx, e *ast.BlockExpr) (types.Type, *diag.Error) {
	c.pushScope(ctx)
	defer c.popScope(ctx)

	for _, l := range e.Lets {
		var t types.Type
		if l.Type != nil {
			want, err := c.resolveType(l.Type)
			if err != nil {
				return nil, err
			}
			if err := c.expect(ctx, l.Value, want); err != nil {
				return nil, err
			}
			t = want
		} else {
			var err *diag.Error
			t, err = c.infer(ctx, l.Value)
			if err != nil {
				return nil, err
			}
		}
		if err := c.checkBindable(l.Name, l.NamePos); err != nil {
			return nil, err
		}
		var tps []*types.TypeParam
		if generalizable(l.Value) {
			t, tps = c.generalize(ctx, t)
		}
		sym := &Symbol{
			Kind:       SymLocal,
			Name:       l.Name,
			Type:       t,
			Pos:        l.NamePos,
			TypeParams: tps,
		}
		c.info.Defs[l] = sym
		ctx.scope.names[l.Name] = sym
	}
	return c.infer(ctx, e.Result)
}

func (c *checker) inferFuncLit(ctx *funcCtx, e *ast.FuncLit) (types.Type, *diag.Error) {
	sig := &types.Func{Params: make([]types.Type, len(e.Params))}
	for i, p := range e.Params {
		if p.Type == nil {
			msg := fmt.Sprintf("cannot infer the type of parameter '%s'; add a type annotation", p.Name)
			sig.Params[i] = c.newMeta(c.freshName(), p.Pos, msg)
			continue
		}
		pt, err := c.resolveType(p.Type)
		if err != nil {
			return nil, err
		}
		sig.Params[i] = pt
	}
	if e.Result != nil {
		rt, err := c.resolveType(e.Result)
		if err != nil {
			return nil, err
		}
		sig.Result = rt
	}
	c.info.FuncLits[e] = sig
	child := &funcCtx{parent: ctx}
	if err := c.checkFunc(child, e.Params, sig, e.Body); err != nil {
		return nil, err
	}
	return sig, nil
}

// generalizable reports whether a let whose value is e may be generalized:
// e is an anonymous function or a name (ADR-0006).
func generalizable(e ast.Expr) bool {
	switch e.(type) {
	case *ast.FuncLit, *ast.Ident:
		return true
	default:
		return false
	}
}

// generalize turns the unsolved metas of t that do not occur in the type of
// any local symbol in scope, or in the type of a function in the current
// component, into new type parameters, by solving each such meta to its type
// parameter. It returns t resolved and the type parameters in order of first
// occurrence in t, or (t, nil) when there are none.
func (c *checker) generalize(ctx *funcCtx, t types.Type) (types.Type, []*types.TypeParam) {
	cand := types.Metas(t)
	if len(cand) == 0 {
		return t, nil
	}
	inEnv := make(map[*types.Meta]bool)
	for f := ctx; f != nil; f = f.parent {
		for s := f.scope; s != nil; s = s.parent {
			for _, sym := range s.names {
				for _, m := range types.Metas(sym.Type) {
					inEnv[m] = true
				}
			}
		}
	}
	for sym := range c.comp {
		for _, m := range types.Metas(sym.Type) {
			inEnv[m] = true
		}
	}
	var tps []*types.TypeParam
	for _, m := range cand {
		if inEnv[m] {
			continue
		}
		tp := &types.TypeParam{Name: m.Name}
		m.Solution = tp
		tps = append(tps, tp)
	}
	if len(tps) == 0 {
		return t, nil
	}
	return types.Resolve(t), tps
}

func ctorType(ctor *types.Ctor) types.Type {
	if len(ctor.Fields) == 0 {
		return types.SelfType(ctor.Data)
	}
	return &types.Func{Params: ctor.Fields, Result: types.SelfType(ctor.Data)}
}

func (c *checker) checkBindable(name string, pos diag.Pos) *diag.Error {
	if c.lookupCtor(name) != nil {
		return diag.Errorf(pos, "cannot use constructor name '%s' as a variable", name)
	}
	return nil
}

func (c *checker) inferCtorCall(
	ctx *funcCtx,
	e *ast.CallExpr,
	id *ast.Ident,
	sym *Symbol,
) (types.Type, *diag.Error) {
	ctor := sym.Ctor
	c.info.Uses[id] = sym
	if len(ctor.Fields) == 0 {
		return nil, diag.Errorf(e.LParen, "constructor '%s' has no fields; write it without parentheses", ctor.Name)
	}
	if len(e.Args) != len(ctor.Fields) {
		return nil, diag.Errorf(e.LParen, "wrong number of arguments: expected %d, found %d", len(ctor.Fields), len(e.Args))
	}
	ft := c.instantiate(sym, id).(*types.Func)
	c.info.Types[e.Fn] = ft
	for i, arg := range e.Args {
		if err := c.expect(ctx, arg, ft.Params[i]); err != nil {
			return nil, err
		}
	}
	return ft.Result, nil
}

func (c *checker) inferMatch(ctx *funcCtx, m *ast.MatchExpr) (types.Type, *diag.Error) {
	st, err := c.infer(ctx, m.Scrutinee)
	if err != nil {
		return nil, err
	}
	if mv, ok := types.Prune(st).(*types.Meta); ok {
		if err := c.inferScrutinee(mv, m.Arms); err != nil {
			return nil, err
		}
	}
	switch types.Prune(st).(type) {
	case *types.Func:
		return nil, diag.Errorf(resultPos(m.Scrutinee), "cannot match on values of type %s", st)
	case *types.TypeParam:
		return nil, diag.Errorf(resultPos(m.Scrutinee), "cannot match on values of type %s", st)
	case *types.Meta:
		return nil, diag.Errorf(resultPos(m.Scrutinee), "cannot infer the type of the matched value; add a type annotation")
	}
	if types.IsIO(st) {
		return nil, diag.Errorf(resultPos(m.Scrutinee), "cannot match on values of type %s", st)
	}
	st = types.Prune(st)

	var result types.Type
	for _, arm := range m.Arms {
		c.pushScope(ctx)
		t, err := c.inferMatchArm(ctx, arm, st)
		c.popScope(ctx)
		if err != nil {
			return nil, err
		}
		if result == nil {
			result = t
		} else if !unify(result, t) {
			return nil, diag.Errorf(resultPos(arm.Body), "match arms have different types: %s and %s", result, t)
		}
	}
	rows := make([][]spat, 0, len(m.Arms))
	for _, arm := range m.Arms {
		q := []spat{toSpat(arm.Pattern, c.info.CtorPats)}
		if !useful(rows, q) {
			return nil, diag.Errorf(arm.Pattern.Position(), "unreachable match arm")
		}
		rows = append(rows, q)
	}
	if msg := missingMessage(rows); msg != "" {
		return nil, diag.Errorf(m.Pos, "non-exhaustive match: missing %s", msg)
	}
	return result, nil
}

// inferScrutinee solves mv, the unsolved type of a matched value, from the
// first arm whose pattern is a constructor, integer, boolean or string
// pattern. It leaves mv unsolved when every pattern is '_' or a variable.
func (c *checker) inferScrutinee(mv *types.Meta, arms []*ast.MatchArm) *diag.Error {
	for _, arm := range arms {
		switch arm.Pattern.(type) {
		case *ast.CtorPat, *ast.IntPat, *ast.BoolPat, *ast.StrPat:
			return c.solveMeta(mv, arm.Pattern)
		}
	}
	return nil
}

// solveMeta solves mv from a constructor, integer, boolean or string pattern.
func (c *checker) solveMeta(mv *types.Meta, p ast.Pattern) *diag.Error {
	switch p := p.(type) {
	case *ast.CtorPat:
		ctor := c.lookupCtor(p.Name)
		if ctor == nil {
			return diag.Errorf(p.Pos, "unknown constructor '%s'", p.Name)
		}
		var args []types.Type
		for _, tp := range ctor.Data.Params {
			msg := fmt.Sprintf("cannot infer type argument '%s' of '%s'; add a type annotation", tp.Name, ctor.Name)
			args = append(args, c.newMeta(tp.Name, p.Pos, msg))
		}
		mv.Solution = &types.Named{Data: ctor.Data, Args: args}
		return nil
	case *ast.IntPat:
		mv.Solution = types.Int
		return nil
	case *ast.BoolPat:
		mv.Solution = types.Bool
		return nil
	case *ast.StrPat:
		mv.Solution = types.String
		return nil
	default:
		return nil
	}
}

func (c *checker) inferMatchArm(ctx *funcCtx, arm *ast.MatchArm, st types.Type) (types.Type, *diag.Error) {
	seen := make(map[string]bool)
	if err := c.checkPattern(ctx, arm.Pattern, st, seen); err != nil {
		return nil, err
	}
	return c.infer(ctx, arm.Body)
}

func (c *checker) checkPattern(
	ctx *funcCtx,
	p ast.Pattern,
	st types.Type,
	seen map[string]bool,
) *diag.Error {
	st = types.Prune(st)
	if mv, ok := st.(*types.Meta); ok {
		switch p.(type) {
		case *ast.CtorPat, *ast.IntPat, *ast.BoolPat, *ast.StrPat:
			if err := c.solveMeta(mv, p); err != nil {
				return err
			}
			st = types.Prune(st)
		}
	}
	switch p := p.(type) {
	case *ast.WildcardPat:
		return nil
	case *ast.VarPat:
		if seen[p.Name] {
			return diag.Errorf(p.Pos, "duplicate variable '%s' in pattern", p.Name)
		}
		seen[p.Name] = true
		if err := c.checkBindable(p.Name, p.Pos); err != nil {
			return err
		}
		c.bindPatVar(ctx, p, st)
		return nil
	case *ast.IntPat:
		if !unify(st, types.Int) {
			return diag.Errorf(p.Pos, "expected %s, found Int", st)
		}
		return nil
	case *ast.StrPat:
		if !unify(st, types.String) {
			return diag.Errorf(p.Pos, "expected %s, found String", st)
		}
		return nil
	case *ast.BoolPat:
		if !unify(st, types.Bool) {
			return diag.Errorf(p.Pos, "expected %s, found Bool", st)
		}
		return nil
	case *ast.CtorPat:
		ctor := c.lookupCtor(p.Name)
		if ctor == nil {
			return diag.Errorf(p.Pos, "unknown constructor '%s'", p.Name)
		}
		named, ok := st.(*types.Named)
		if !ok || named.Data != ctor.Data {
			return diag.Errorf(p.Pos, "expected %s, found %s", st, dataDisplay(ctor.Data))
		}
		if len(p.Args) != len(ctor.Fields) {
			return diag.Errorf(p.Pos, "wrong number of fields in pattern '%s': expected %d, found %d", p.Name, len(ctor.Fields), len(p.Args))
		}
		for i, arg := range p.Args {
			field := types.Subst(ctor.Fields[i], ctor.Data.Params, named.Args)
			if err := c.checkPattern(ctx, arg, field, seen); err != nil {
				return err
			}
		}
		c.info.CtorPats[p] = ctor
		return nil
	default:
		return diag.Errorf(p.Position(), "unhandled pattern %T", p)
	}
}

func (c *checker) bindPatVar(ctx *funcCtx, p *ast.VarPat, typ types.Type) {
	sym := &Symbol{
		Kind: SymLocal,
		Name: p.Name,
		Type: typ,
		Pos:  p.Pos,
	}
	c.info.PatVars[p] = sym
	ctx.scope.names[p.Name] = sym
}

func (c *checker) checkSolved() *diag.Error {
	for _, pm := range c.metas {
		if _, ok := types.Prune(pm.meta).(*types.Meta); ok {
			return diag.Errorf(pm.pos, "%s", pm.msg)
		}
	}
	return nil
}

func (c *checker) resolveInfo() {
	for e, t := range c.info.Types {
		c.info.Types[e] = types.Resolve(t)
	}
	for _, sym := range c.info.Defs {
		sym.Type = types.Resolve(sym.Type)
	}
	for _, sym := range c.info.PatVars {
		sym.Type = types.Resolve(sym.Type)
	}
	for _, sym := range c.info.Params {
		sym.Type = types.Resolve(sym.Type)
	}
	for lit, sig := range c.info.FuncLits {
		c.info.FuncLits[lit] = types.Resolve(sig).(*types.Func)
	}
	for _, args := range c.info.TypeArgs {
		for i := range args {
			args[i] = types.Resolve(args[i])
		}
	}
	for _, sym := range c.info.Funcs {
		sym.Type = types.Resolve(sym.Type)
	}
}

// dataDisplay formats d for an error message: its name, followed by
// "[?P1, ?P2, ...]" when it has parameters (e.g. "Option[?T]").
func dataDisplay(d *types.Data) string {
	if len(d.Params) == 0 {
		return d.Name
	}
	var b strings.Builder
	b.WriteString(d.Name)
	b.WriteByte('[')
	for i, p := range d.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('?')
		b.WriteString(p.Name)
	}
	b.WriteByte(']')
	return b.String()
}
