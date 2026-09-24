package ir

import (
	"strings"
	"testing"
)

func TestFormatExpr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr Expr
		want string
	}{
		{name: "int", expr: &IntConst{Value: 42}, want: "42"},
		{name: "negative int", expr: &IntConst{Value: -5}, want: "-5"},
		{name: "zero", expr: &IntConst{Value: 0}, want: "0"},
		{name: "true", expr: &BoolConst{Value: true}, want: "true"},
		{name: "false", expr: &BoolConst{Value: false}, want: "false"},
		{name: "local", expr: &LocalGet{Local: 1, T: Int}, want: "(local 1)"},
		{name: "func ref", expr: &FuncValue{Func: 3}, want: "(func.ref 3)"},
		{name: "neg", expr: &Unary{Op: Neg, X: &IntConst{Value: 1}}, want: "(neg 1)"},
		{name: "not", expr: &Unary{Op: Not, X: &BoolConst{Value: false}}, want: "(not false)"},
		{
			name: "add",
			expr: &Binary{Op: Add, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(add 1 2)",
		},
		{
			name: "sub",
			expr: &Binary{Op: Sub, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(sub 1 2)",
		},
		{
			name: "mul",
			expr: &Binary{Op: Mul, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(mul 1 2)",
		},
		{
			name: "div",
			expr: &Binary{Op: Div, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(div 1 2)",
		},
		{
			name: "eq",
			expr: &Binary{Op: Eq, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(eq 1 2)",
		},
		{
			name: "ne",
			expr: &Binary{Op: Ne, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(ne 1 2)",
		},
		{
			name: "lt",
			expr: &Binary{Op: Lt, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(lt 1 2)",
		},
		{
			name: "le",
			expr: &Binary{Op: Le, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(le 1 2)",
		},
		{
			name: "gt",
			expr: &Binary{Op: Gt, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(gt 1 2)",
		},
		{
			name: "ge",
			expr: &Binary{Op: Ge, X: &IntConst{Value: 1}, Y: &IntConst{Value: 2}},
			want: "(ge 1 2)",
		},
		{
			name: "if",
			expr: &If{
				Cond: &BoolConst{Value: true},
				Then: &IntConst{Value: 1},
				Else: &IntConst{Value: 0},
				T:    Int,
			},
			want: "(if true 1 0)",
		},
		{
			name: "block",
			expr: &Block{
				Lets: []*Let{
					{Local: 0, Value: &IntConst{Value: 1}},
					{Local: 1, Value: &Binary{Op: Add, X: &LocalGet{Local: 0, T: Int}, Y: &IntConst{Value: 1}}},
				},
				Result: &LocalGet{Local: 1, T: Int},
			},
			want: "(block (let 0 1) (let 1 (add (local 0) 1)) (local 1))",
		},
		{
			name: "call",
			expr: &Call{
				Func: 0,
				Args: []Expr{&LocalGet{Local: 0, T: Int}, &IntConst{Value: 1}},
				T:    Int,
			},
			want: "(call 0 (local 0) 1)",
		},
		{
			name: "call no args",
			expr: &Call{Func: 2, T: Int},
			want: "(call 2)",
		},
		{
			name: "call indirect",
			expr: &CallIndirect{
				Callee: &LocalGet{Local: 0, T: FuncRef},
				Sig:    Sig{Params: []ValType{Int}, Result: Int},
				Args:   []Expr{&LocalGet{Local: 1, T: Int}},
			},
			want: "(call.indirect (sig (Int) Int) (local 0) (local 1))",
		},
		{
			name: "call indirect no args",
			expr: &CallIndirect{
				Callee: &FuncValue{Func: 0},
				Sig:    Sig{Result: Int},
			},
			want: "(call.indirect (sig () Int) (func.ref 0))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := FormatExpr(tt.expr); got != tt.want {
				t.Errorf("FormatExpr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatSig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sig  Sig
		want string
	}{
		{
			name: "two ints",
			sig:  Sig{Params: []ValType{Int, Int}, Result: Int},
			want: "(sig (Int Int) Int)",
		},
		{
			name: "no params",
			sig:  Sig{Result: Int},
			want: "(sig () Int)",
		},
		{
			name: "one int",
			sig:  Sig{Params: []ValType{Int}, Result: Bool},
			want: "(sig (Int) Bool)",
		},
		{
			name: "funcref param and result",
			sig:  Sig{Params: []ValType{FuncRef, Int}, Result: FuncRef},
			want: "(sig (FuncRef Int) FuncRef)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := FormatSig(tt.sig); got != tt.want {
				t.Errorf("FormatSig() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	t.Parallel()

	add := &Func{
		Name:   "add",
		Sig:    Sig{Params: []ValType{Int, Int}, Result: Int},
		Locals: []ValType{Int, Int},
		Body: &Binary{
			Op: Add,
			X:  &LocalGet{Local: 0, T: Int},
			Y:  &LocalGet{Local: 1, T: Int},
		},
	}
	mainConst := &Func{
		Name: "main",
		Sig:  Sig{Result: Int},
		Body: &IntConst{Value: 42},
	}
	rich := &Func{
		Name:   "main",
		Sig:    Sig{Result: Int},
		Locals: []ValType{Int, Bool, FuncRef},
		Body: &Block{
			Lets: []*Let{
				{Local: 0, Value: &Unary{Op: Neg, X: &IntConst{Value: -5}}},
				{Local: 1, Value: &Unary{Op: Not, X: &BoolConst{Value: true}}},
				{Local: 2, Value: &FuncValue{Func: 0}},
			},
			Result: &If{
				Cond: &LocalGet{Local: 1, T: Bool},
				Then: &Call{
					Func: 0,
					Args: []Expr{&LocalGet{Local: 0, T: Int}, &IntConst{Value: 1}},
					T:    Int,
				},
				Else: &CallIndirect{
					Callee: &LocalGet{Local: 2, T: FuncRef},
					Sig:    Sig{Params: []ValType{Int, Int}, Result: Int},
					Args:   []Expr{&LocalGet{Local: 0, T: Int}, &IntConst{Value: 2}},
				},
				T: Int,
			},
		},
	}

	tests := []struct {
		name   string
		module *Module
		want   string
	}{
		{
			name: "add example and empty table",
			module: &Module{
				Funcs: []*Func{add},
			},
			want: lines(
				"(func 0 add (sig (Int Int) Int) (locals Int Int) (add (local 0) (local 1)))",
				"(table)",
				"(main 0)",
			),
		},
		{
			name: "empty locals",
			module: &Module{
				Funcs: []*Func{mainConst},
			},
			want: lines(
				"(func 0 main (sig () Int) (locals) 42)",
				"(table)",
				"(main 0)",
			),
		},
		{
			name: "table and main index",
			module: &Module{
				Funcs: []*Func{add, rich},
				Table: []FuncID{0, 2},
				Main:  1,
			},
			want: lines(
				"(func 0 add (sig (Int Int) Int) (locals Int Int) (add (local 0) (local 1)))",
				"(func 1 main (sig () Int) (locals Int Bool FuncRef) (block (let 0 (neg -5)) (let 1 (not true)) (let 2 (func.ref 0)) (if (local 1) (call 0 (local 0) 1) (call.indirect (sig (Int Int) Int) (local 2) (local 0) 2))))",
				"(table 0 2)",
				"(main 1)",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Format(tt.module); got != tt.want {
				t.Errorf("Format() =\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestType(t *testing.T) {
	t.Parallel()

	intExpr := &IntConst{Value: 1}
	boolExpr := &BoolConst{Value: true}
	tests := []struct {
		name string
		expr Expr
		want ValType
	}{
		{name: "int const", expr: intExpr, want: Int},
		{name: "bool const", expr: boolExpr, want: Bool},
		{name: "local get", expr: &LocalGet{Local: 0, T: Bool}, want: Bool},
		{name: "func value", expr: &FuncValue{Func: 1}, want: FuncRef},
		{name: "neg", expr: &Unary{Op: Neg, X: intExpr}, want: Int},
		{name: "not", expr: &Unary{Op: Not, X: boolExpr}, want: Bool},
		{name: "add", expr: &Binary{Op: Add, X: intExpr, Y: intExpr}, want: Int},
		{name: "sub", expr: &Binary{Op: Sub, X: intExpr, Y: intExpr}, want: Int},
		{name: "mul", expr: &Binary{Op: Mul, X: intExpr, Y: intExpr}, want: Int},
		{name: "div", expr: &Binary{Op: Div, X: intExpr, Y: intExpr}, want: Int},
		{name: "eq", expr: &Binary{Op: Eq, X: intExpr, Y: intExpr}, want: Bool},
		{name: "ne", expr: &Binary{Op: Ne, X: boolExpr, Y: boolExpr}, want: Bool},
		{name: "lt", expr: &Binary{Op: Lt, X: intExpr, Y: intExpr}, want: Bool},
		{name: "le", expr: &Binary{Op: Le, X: intExpr, Y: intExpr}, want: Bool},
		{name: "gt", expr: &Binary{Op: Gt, X: intExpr, Y: intExpr}, want: Bool},
		{name: "ge", expr: &Binary{Op: Ge, X: intExpr, Y: intExpr}, want: Bool},
		{
			name: "if",
			expr: &If{Cond: boolExpr, Then: intExpr, Else: intExpr, T: FuncRef},
			want: FuncRef,
		},
		{name: "block", expr: &Block{Result: boolExpr}, want: Bool},
		{name: "call", expr: &Call{T: Bool}, want: Bool},
		{
			name: "call indirect",
			expr: &CallIndirect{Sig: Sig{Result: FuncRef}},
			want: FuncRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.expr.Type(); got != tt.want {
				t.Errorf("Type() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestValTypeString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  ValType
		want string
	}{
		{name: "Int", typ: Int, want: "Int"},
		{name: "Bool", typ: Bool, want: "Bool"},
		{name: "FuncRef", typ: FuncRef, want: "FuncRef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.typ.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func lines(parts ...string) string {
	return strings.Join(parts, "\n")
}
