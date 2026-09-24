// Package lower lowers a type-checked Cero AST to IR.
package lower

import (
	"fmt"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/ir"
	"github.com/deltama37/cero/internal/token"
	"github.com/deltama37/cero/internal/typecheck"
	"github.com/deltama37/cero/internal/types"
)

// Lower converts a type-checked file into an IR module.
// It panics if info is inconsistent with file (a type checker bug).
func Lower(file *ast.File, info *typecheck.Info) *ir.Module {
	l := &lowerer{
		info:   info,
		module: &ir.Module{Funcs: make([]*ir.Func, 0, len(file.Funcs))},
		declID: make(map[*ast.FuncDecl]ir.FuncID, len(file.Funcs)),
	}

	foundMain := false
	for i, d := range file.Funcs {
		sym := info.Funcs[d]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing symbol for function '%s'", d.Name))
		}
		id := ir.FuncID(i)
		l.module.Funcs = append(l.module.Funcs, &ir.Func{
			Name: d.Name,
			Sig:  l.sigOf(sym.Type),
		})
		l.declID[d] = id
		if d.Name == "main" {
			l.module.Main = id
			foundMain = true
		}
	}
	if !foundMain {
		panic("lower: missing function 'main'")
	}

	for _, d := range file.Funcs {
		l.lowerFunc(l.module.Funcs[l.declID[d]], d.Params, d.Body)
	}
	return l.module
}

type lowerer struct {
	info    *typecheck.Info
	module  *ir.Module
	declID  map[*ast.FuncDecl]ir.FuncID
	lambdas int
}

func (l *lowerer) lowerFunc(fn *ir.Func, params []*ast.Param, body *ast.BlockExpr) {
	locals := make(map[*typecheck.Symbol]ir.LocalID, len(params))
	fn.Locals = make([]ir.ValType, len(params))
	for i, p := range params {
		sym := l.info.Params[p]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing parameter symbol '%s' at %s", p.Name, p.Pos))
		}
		locals[sym] = ir.LocalID(i)
		fn.Locals[i] = l.valType(sym.Type)
	}
	fn.Body = l.lowerExpr(fn, locals, body)
	if got, want := fn.Body.Type(), fn.Sig.Result; got != want {
		panic(fmt.Sprintf("lower: %s body has type %s, signature result is %s", fn.Name, got, want))
	}
}

func (l *lowerer) lowerExpr(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e ast.Expr,
) ir.Expr {
	result := l.convert(fn, locals, e)
	if got, want := result.Type(), l.valType(l.mustType(e)); got != want {
		panic(fmt.Sprintf("lower: %T at %s has IR type %s, type checker has %s", e, e.Position(), got, want))
	}
	return result
}

func (l *lowerer) convert(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e ast.Expr,
) ir.Expr {
	switch e := e.(type) {
	case *ast.IntLit:
		return &ir.IntConst{Value: e.Value}
	case *ast.BoolLit:
		return &ir.BoolConst{Value: e.Value}
	case *ast.Ident:
		return l.lowerIdent(e, locals)
	case *ast.UnaryExpr:
		return l.lowerUnary(fn, locals, e)
	case *ast.BinaryExpr:
		return l.lowerBinary(fn, locals, e)
	case *ast.CallExpr:
		return l.lowerCall(fn, locals, e)
	case *ast.IfExpr:
		return l.lowerIf(fn, locals, e)
	case *ast.BlockExpr:
		return l.lowerBlock(fn, locals, e)
	case *ast.FuncLit:
		return l.lowerFuncLit(e)
	default:
		panic(fmt.Sprintf("lower: unhandled expression %T", e))
	}
}

func (l *lowerer) lowerIdent(e *ast.Ident, locals map[*typecheck.Symbol]ir.LocalID) ir.Expr {
	sym := l.info.Uses[e]
	if sym == nil {
		panic(fmt.Sprintf("lower: missing symbol for '%s' at %s", e.Name, e.Pos))
	}
	switch sym.Kind {
	case typecheck.SymParam, typecheck.SymLocal:
		id, ok := locals[sym]
		if !ok {
			panic(fmt.Sprintf("lower: no local for '%s' at %s", e.Name, e.Pos))
		}
		return &ir.LocalGet{Local: id, T: l.valType(l.mustType(e))}
	case typecheck.SymFunc:
		id, ok := l.declID[sym.Decl]
		if !ok {
			panic(fmt.Sprintf("lower: no function id for '%s' at %s", e.Name, e.Pos))
		}
		l.addTable(id)
		return &ir.FuncValue{Func: id}
	default:
		panic(fmt.Sprintf("lower: unhandled symbol kind %d for '%s'", sym.Kind, e.Name))
	}
}

func (l *lowerer) lowerUnary(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.UnaryExpr,
) ir.Expr {
	x := l.lowerExpr(fn, locals, e.X)
	switch e.Op {
	case token.Minus:
		return &ir.Unary{Op: ir.Neg, X: x}
	case token.Bang:
		return &ir.Unary{Op: ir.Not, X: x}
	default:
		panic(fmt.Sprintf("lower: unhandled unary operator %s at %s", e.Op, e.Pos))
	}
}

func (l *lowerer) lowerBinary(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.BinaryExpr,
) ir.Expr {
	x := l.lowerExpr(fn, locals, e.X)
	switch e.Op {
	case token.AndAnd:
		return &ir.If{
			Cond: x,
			Then: l.lowerExpr(fn, locals, e.Y),
			Else: &ir.BoolConst{Value: false},
			T:    ir.Bool,
		}
	case token.OrOr:
		return &ir.If{
			Cond: x,
			Then: &ir.BoolConst{Value: true},
			Else: l.lowerExpr(fn, locals, e.Y),
			T:    ir.Bool,
		}
	}
	y := l.lowerExpr(fn, locals, e.Y)
	return &ir.Binary{Op: binOp(e), X: x, Y: y}
}

func (l *lowerer) lowerCall(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.CallExpr,
) ir.Expr {
	if id, ok := e.Fn.(*ast.Ident); ok {
		sym := l.info.Uses[id]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing symbol for '%s' at %s", id.Name, id.Pos))
		}
		if sym.Kind == typecheck.SymFunc {
			fid, ok := l.declID[sym.Decl]
			if !ok {
				panic(fmt.Sprintf("lower: no function id for '%s' at %s", id.Name, id.Pos))
			}
			return &ir.Call{
				Func: fid,
				Args: l.lowerArgs(fn, locals, e.Args),
				T:    l.valType(l.mustType(e)),
			}
		}
	}
	ft, ok := l.mustType(e.Fn).(*types.Func)
	if !ok {
		panic(fmt.Sprintf("lower: callee type at %s is %s, want a function", e.Fn.Position(), l.mustType(e.Fn)))
	}
	return &ir.CallIndirect{
		Callee: l.lowerExpr(fn, locals, e.Fn),
		Sig:    l.sigOf(ft),
		Args:   l.lowerArgs(fn, locals, e.Args),
	}
}

func (l *lowerer) lowerIf(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.IfExpr,
) ir.Expr {
	return &ir.If{
		Cond: l.lowerExpr(fn, locals, e.Cond),
		Then: l.lowerExpr(fn, locals, e.Then),
		Else: l.lowerExpr(fn, locals, e.Else),
		T:    l.valType(l.mustType(e)),
	}
}

func (l *lowerer) lowerBlock(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	e *ast.BlockExpr,
) ir.Expr {
	if len(e.Lets) == 0 {
		return l.lowerExpr(fn, locals, e.Result)
	}
	lets := make([]*ir.Let, len(e.Lets))
	for i, stmt := range e.Lets {
		value := l.lowerExpr(fn, locals, stmt.Value)
		sym := l.info.Defs[stmt]
		if sym == nil {
			panic(fmt.Sprintf("lower: missing symbol for let '%s' at %s", stmt.Name, stmt.NamePos))
		}
		id := ir.LocalID(len(fn.Locals))
		locals[sym] = id
		fn.Locals = append(fn.Locals, l.valType(sym.Type))
		lets[i] = &ir.Let{Local: id, Value: value}
	}
	return &ir.Block{
		Lets:   lets,
		Result: l.lowerExpr(fn, locals, e.Result),
	}
}

func (l *lowerer) lowerFuncLit(e *ast.FuncLit) ir.Expr {
	sig := l.info.FuncLits[e]
	if sig == nil {
		panic(fmt.Sprintf("lower: missing signature for anonymous function at %s", e.Pos))
	}
	id := ir.FuncID(len(l.module.Funcs))
	fn := &ir.Func{
		Name: fmt.Sprintf("lambda$%d", l.lambdas),
		Sig:  l.sigOf(sig),
	}
	l.lambdas++
	l.module.Funcs = append(l.module.Funcs, fn)
	l.lowerFunc(fn, e.Params, e.Body)
	l.addTable(id)
	return &ir.FuncValue{Func: id}
}

func (l *lowerer) lowerArgs(
	fn *ir.Func,
	locals map[*typecheck.Symbol]ir.LocalID,
	args []ast.Expr,
) []ir.Expr {
	out := make([]ir.Expr, len(args))
	for i, arg := range args {
		out[i] = l.lowerExpr(fn, locals, arg)
	}
	return out
}

// addTable appends id unless it is already present.
// Callers add an id when a FuncRef value is created, after nested
// expressions have been lowered, so earlier uses come first.
func (l *lowerer) addTable(id ir.FuncID) {
	for _, existing := range l.module.Table {
		if existing == id {
			return
		}
	}
	l.module.Table = append(l.module.Table, id)
}

func (l *lowerer) mustType(e ast.Expr) types.Type {
	t, ok := l.info.Types[e]
	if !ok || t == nil {
		panic(fmt.Sprintf("lower: missing type for %T at %s", e, e.Position()))
	}
	return t
}

func (l *lowerer) valType(t types.Type) ir.ValType {
	switch t.(type) {
	case types.IntType:
		return ir.Int
	case types.BoolType:
		return ir.Bool
	case *types.Func:
		return ir.FuncRef
	default:
		panic(fmt.Sprintf("lower: unhandled type %T", t))
	}
}

func (l *lowerer) sigOf(t types.Type) ir.Sig {
	ft, ok := t.(*types.Func)
	if !ok {
		panic(fmt.Sprintf("lower: expected function type, found %T", t))
	}
	params := make([]ir.ValType, len(ft.Params))
	for i, p := range ft.Params {
		params[i] = l.valType(p)
	}
	return ir.Sig{Params: params, Result: l.valType(ft.Result)}
}

func binOp(e *ast.BinaryExpr) ir.BinOp {
	switch e.Op {
	case token.Plus:
		return ir.Add
	case token.Minus:
		return ir.Sub
	case token.Star:
		return ir.Mul
	case token.Slash:
		return ir.Div
	case token.Eq:
		return ir.Eq
	case token.NotEq:
		return ir.Ne
	case token.Lt:
		return ir.Lt
	case token.LtEq:
		return ir.Le
	case token.Gt:
		return ir.Gt
	case token.GtEq:
		return ir.Ge
	default:
		panic(fmt.Sprintf("lower: unhandled binary operator %s at %s", e.Op, e.OpPos))
	}
}
