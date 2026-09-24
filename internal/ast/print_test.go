package ast

import (
	"testing"

	"github.com/deltama37/cero/internal/token"
)

func TestFormat(t *testing.T) {
	t.Parallel()

	intType := &NamedType{Name: "Int"}
	boolType := &NamedType{Name: "Bool"}
	ident := func(name string) Expr { return &Ident{Name: name} }
	intLit := func(v int64) Expr { return &IntLit{Value: v} }
	block := func(result Expr, lets ...*LetStmt) *BlockExpr {
		return &BlockExpr{Lets: lets, Result: result}
	}

	add := &FuncDecl{
		Name: "add",
		Params: []*Param{
			{Name: "a", Type: intType},
			{Name: "b", Type: intType},
		},
		Result: intType,
		Body: block(&BinaryExpr{
			Op: token.Plus,
			X:  ident("a"),
			Y:  ident("b"),
		}),
	}
	id := &FuncDecl{
		Name:   "id",
		Result: boolType,
		Body:   block(&BoolLit{Value: false}),
	}
	nilCtor := &CtorDecl{Name: "Nil"}
	consCtor := &CtorDecl{
		Name:   "Cons",
		Fields: []TypeExpr{intType, &NamedType{Name: "IntList"}},
	}
	boxCtor := &CtorDecl{
		Name: "Box",
		Fields: []TypeExpr{&FuncType{
			Params: []TypeExpr{intType},
			Result: intType,
		}},
	}
	wild := &WildcardPat{}
	varX := &VarPat{Name: "x"}
	nilPat := &CtorPat{Name: "Nil"}
	consPat := &CtorPat{
		Name: "Cons",
		Args: []Pattern{&VarPat{Name: "x"}, &WildcardPat{}},
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "named type",
			got:  FormatType(intType),
			want: "Int",
		},
		{
			name: "function type two params",
			got: FormatType(&FuncType{
				Params: []TypeExpr{intType, intType},
				Result: boolType,
			}),
			want: "(-> (Int Int) Bool)",
		},
		{
			name: "function type no params",
			got: FormatType(&FuncType{
				Params: []TypeExpr{},
				Result: intType,
			}),
			want: "(-> () Int)",
		},
		{
			name: "int literal",
			got:  FormatExpr(intLit(42)),
			want: "42",
		},
		{
			name: "bool true",
			got:  FormatExpr(&BoolLit{Value: true}),
			want: "true",
		},
		{
			name: "bool false",
			got:  FormatExpr(&BoolLit{Value: false}),
			want: "false",
		},
		{
			name: "ident",
			got:  FormatExpr(ident("x")),
			want: "x",
		},
		{
			name: "unary neg",
			got:  FormatExpr(&UnaryExpr{Op: token.Minus, X: ident("x")}),
			want: "(neg x)",
		},
		{
			name: "unary not",
			got:  FormatExpr(&UnaryExpr{Op: token.Bang, X: ident("x")}),
			want: "(not x)",
		},
		{
			name: "binary nested",
			got: FormatExpr(&BinaryExpr{
				Op: token.Plus,
				X:  ident("a"),
				Y: &BinaryExpr{
					Op: token.Star,
					X:  ident("b"),
					Y:  ident("c"),
				},
			}),
			want: "(+ a (* b c))",
		},
		{
			name: "binary and",
			got: FormatExpr(&BinaryExpr{
				Op: token.AndAnd,
				X:  ident("a"),
				Y:  ident("b"),
			}),
			want: "(&& a b)",
		},
		{
			name: "call with args",
			got: FormatExpr(&CallExpr{
				Fn:   ident("f"),
				Args: []Expr{intLit(1), intLit(2)},
			}),
			want: "(call f 1 2)",
		},
		{
			name: "call without args",
			got:  FormatExpr(&CallExpr{Fn: ident("f")}),
			want: "(call f)",
		},
		{
			name: "if",
			got: FormatExpr(&IfExpr{
				Cond: ident("c"),
				Then: block(intLit(1)),
				Else: block(intLit(2)),
			}),
			want: "(if c (block 1) (block 2))",
		},
		{
			name: "block with let",
			got: FormatExpr(block(ident("x"), &LetStmt{
				Name:  "x",
				Value: intLit(1),
			})),
			want: "(block (let x 1) x)",
		},
		{
			name: "let with type",
			got: FormatExpr(block(ident("x"), &LetStmt{
				Name:  "x",
				Type:  intType,
				Value: intLit(1),
			})),
			want: "(block (let x : Int 1) x)",
		},
		{
			name: "func lit",
			got: FormatExpr(&FuncLit{
				Params: []*Param{{Name: "x", Type: intType}},
				Result: intType,
				Body:   block(ident("x")),
			}),
			want: "(fn ((x Int)) Int (block x))",
		},
		{
			name: "func decl",
			got:  FormatFunc(add),
			want: "(fn add ((a Int) (b Int)) Int (block (+ a b)))",
		},
		{
			name: "file joins functions",
			got:  FormatFile(&File{Funcs: []*FuncDecl{add, id}}),
			want: "(fn add ((a Int) (b Int)) Int (block (+ a b)))\n(fn id () Bool (block false))",
		},
		{
			name: "empty file",
			got:  FormatFile(&File{}),
			want: "",
		},
		{
			name: "type decl",
			got:  FormatTypeDecl(&TypeDecl{Name: "IntList", Ctors: []*CtorDecl{nilCtor, consCtor}}),
			want: "(type IntList (Nil) (Cons Int IntList))",
		},
		{
			name: "ctor no fields",
			got:  FormatTypeDecl(&TypeDecl{Name: "T", Ctors: []*CtorDecl{nilCtor}}),
			want: "(type T (Nil))",
		},
		{
			name: "ctor with fields",
			got:  FormatTypeDecl(&TypeDecl{Name: "IntList", Ctors: []*CtorDecl{consCtor}}),
			want: "(type IntList (Cons Int IntList))",
		},
		{
			name: "ctor function field",
			got:  FormatTypeDecl(&TypeDecl{Name: "Box", Ctors: []*CtorDecl{boxCtor}}),
			want: "(type Box (Box (-> (Int) Int)))",
		},
		{
			name: "wildcard pattern",
			got:  FormatPattern(wild),
			want: "_",
		},
		{
			name: "var pattern",
			got:  FormatPattern(varX),
			want: "x",
		},
		{
			name: "ctor pattern no args",
			got:  FormatPattern(nilPat),
			want: "Nil",
		},
		{
			name: "ctor pattern with args",
			got:  FormatPattern(consPat),
			want: "(Cons x _)",
		},
		{
			name: "int pattern",
			got:  FormatPattern(&IntPat{Value: 42}),
			want: "42",
		},
		{
			name: "bool pattern true",
			got:  FormatPattern(&BoolPat{Value: true}),
			want: "true",
		},
		{
			name: "bool pattern false",
			got:  FormatPattern(&BoolPat{Value: false}),
			want: "false",
		},
		{
			name: "match expr",
			got: FormatExpr(&MatchExpr{
				Scrutinee: ident("xs"),
				Arms: []*MatchArm{
					{Pattern: nilPat, Body: intLit(0)},
					{Pattern: consPat, Body: ident("x")},
				},
			}),
			want: "(match xs (=> Nil 0) (=> (Cons x _) x))",
		},
		{
			name: "match arm",
			got: FormatExpr(&MatchExpr{
				Scrutinee: ident("x"),
				Arms:      []*MatchArm{{Pattern: wild, Body: intLit(0)}},
			}),
			want: "(match x (=> _ 0))",
		},
		{
			name: "file types then funcs",
			got: FormatFile(&File{
				Types: []*TypeDecl{{Name: "Unit", Ctors: []*CtorDecl{{Name: "Unit"}}}},
				Funcs: []*FuncDecl{add, id},
			}),
			want: "(type Unit (Unit))\n(fn add ((a Int) (b Int)) Int (block (+ a b)))\n(fn id () Bool (block false))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("format = %q, want %q", tt.got, tt.want)
			}
		})
	}
}
