package ast

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/deltama37/cero/internal/token"
)

// FormatFile formats each function with FormatFunc and joins them with "\n".
func FormatFile(f *File) string {
	parts := make([]string, len(f.Funcs))
	for i, fn := range f.Funcs {
		parts[i] = FormatFunc(fn)
	}
	return strings.Join(parts, "\n")
}

// FormatFunc formats a function declaration as an S-expression.
func FormatFunc(d *FuncDecl) string {
	return "(fn " + d.Name + " (" + formatParams(d.Params) + ") " + FormatType(d.Result) + " " + FormatExpr(d.Body) + ")"
}

// FormatExpr formats an expression as an S-expression.
func FormatExpr(e Expr) string {
	switch e := e.(type) {
	case *IntLit:
		return strconv.FormatInt(e.Value, 10)
	case *BoolLit:
		if e.Value {
			return "true"
		}
		return "false"
	case *Ident:
		return e.Name
	case *UnaryExpr:
		return "(" + unaryName(e.Op) + " " + FormatExpr(e.X) + ")"
	case *BinaryExpr:
		return "(" + binaryName(e.Op) + " " + FormatExpr(e.X) + " " + FormatExpr(e.Y) + ")"
	case *CallExpr:
		parts := make([]string, 0, 1+len(e.Args))
		parts = append(parts, FormatExpr(e.Fn))
		for _, arg := range e.Args {
			parts = append(parts, FormatExpr(arg))
		}
		return "(call " + strings.Join(parts, " ") + ")"
	case *IfExpr:
		return "(if " + FormatExpr(e.Cond) + " " + FormatExpr(e.Then) + " " + FormatExpr(e.Else) + ")"
	case *BlockExpr:
		parts := make([]string, 0, len(e.Lets)+1)
		for _, letStmt := range e.Lets {
			parts = append(parts, formatLet(letStmt))
		}
		parts = append(parts, FormatExpr(e.Result))
		return "(block " + strings.Join(parts, " ") + ")"
	case *FuncLit:
		return "(fn (" + formatParams(e.Params) + ") " + FormatType(e.Result) + " " + FormatExpr(e.Body) + ")"
	default:
		panic(fmt.Sprintf("ast.FormatExpr: unhandled type %T", e))
	}
}

// FormatType formats a type as an S-expression.
func FormatType(t TypeExpr) string {
	switch t := t.(type) {
	case *NamedType:
		return t.Name
	case *FuncType:
		params := make([]string, len(t.Params))
		for i, param := range t.Params {
			params[i] = FormatType(param)
		}
		return "(-> (" + strings.Join(params, " ") + ") " + FormatType(t.Result) + ")"
	default:
		panic(fmt.Sprintf("ast.FormatType: unhandled type %T", t))
	}
}

func formatParams(params []*Param) string {
	parts := make([]string, len(params))
	for i, param := range params {
		parts[i] = formatParam(param)
	}
	return strings.Join(parts, " ")
}

func formatParam(param *Param) string {
	return "(" + param.Name + " " + FormatType(param.Type) + ")"
}

func formatLet(stmt *LetStmt) string {
	if stmt.Type == nil {
		return "(let " + stmt.Name + " " + FormatExpr(stmt.Value) + ")"
	}
	return "(let " + stmt.Name + " : " + FormatType(stmt.Type) + " " + FormatExpr(stmt.Value) + ")"
}

func unaryName(kind token.Kind) string {
	switch kind {
	case token.Minus:
		return "neg"
	case token.Bang:
		return "not"
	default:
		panic(fmt.Sprintf("ast.FormatExpr: unhandled unary operator %s", kind))
	}
}

func binaryName(kind token.Kind) string {
	switch kind {
	case token.Plus:
		return "+"
	case token.Minus:
		return "-"
	case token.Star:
		return "*"
	case token.Slash:
		return "/"
	case token.Eq:
		return "=="
	case token.NotEq:
		return "!="
	case token.Lt:
		return "<"
	case token.LtEq:
		return "<="
	case token.Gt:
		return ">"
	case token.GtEq:
		return ">="
	case token.AndAnd:
		return "&&"
	case token.OrOr:
		return "||"
	default:
		panic(fmt.Sprintf("ast.FormatExpr: unhandled binary operator %s", kind))
	}
}
