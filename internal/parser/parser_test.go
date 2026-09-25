package parser

import (
	"testing"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/diag"
)

func TestParseExpr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "precedence add mul", src: "1 + 2 * 3", want: "(+ 1 (* 2 3))"},
		{name: "precedence or and", src: "a || b && c", want: "(|| a (&& b c))"},
		{name: "precedence eq rel", src: "a == b < c", want: "(== a (< b c))"},
		{name: "precedence and eq", src: "a && b == c", want: "(&& a (== b c))"},
		{name: "left assoc minus", src: "1 - 2 - 3", want: "(- (- 1 2) 3)"},
		{name: "left assoc div mul", src: "a / b * c", want: "(* (/ a b) c)"},
		{name: "unary minus binds tighter than mul", src: "-x * y", want: "(* (neg x) y)"},
		{name: "stacked not", src: "!!a", want: "(not (not a))"},
		{name: "stacked minus", src: "- -1", want: "(neg (neg 1))"},
		{name: "parentheses", src: "(1 + 2) * 3", want: "(* (+ 1 2) 3)"},
		{name: "call no args", src: "f()", want: "(call f)"},
		{name: "call args", src: "f(1, x + 2)", want: "(call f 1 (+ x 2))"},
		{name: "chained call", src: "f(1)(2)", want: "(call (call f 1) 2)"},
		{name: "unary call", src: "-f(x)", want: "(neg (call f x))"},
		{name: "if else", src: "if a { 1 } else { 2 }", want: "(if a (block 1) (block 2))"},
		{
			name: "else if chain",
			src:  "if a { 1 } else if b { 2 } else { 3 }",
			want: "(if a (block 1) (if b (block 2) (block 3)))",
		},
		{
			name: "if condition comparison",
			src:  "if a < b { 1 } else { 2 }",
			want: "(if (< a b) (block 1) (block 2))",
		},
		{
			name: "block lets",
			src:  "{ let x = 1 let y: Int = x + 1 y }",
			want: "(block (let x 1) (let y : Int (+ x 1)) y)",
		},
		{
			name: "nested block",
			src:  "{ let x = 1 { let y = 2 y } }",
			want: "(block (let x 1) (block (let y 2) y))",
		},
		{
			name: "shadowing",
			src:  "{ let x = 1 let x = 2 x }",
			want: "(block (let x 1) (let x 2) x)",
		},
		{
			name: "func lit",
			src:  "fn(x: Int) -> Int { x * 2 }",
			want: "(fn ((x Int)) Int (block (* x 2)))",
		},
		{
			name: "func lit no params",
			src:  "fn() -> Int { 1 }",
			want: "(fn () Int (block 1))",
		},
		{
			name: "func lit function param",
			src:  "fn(f: Int -> Int) -> Int { f(1) }",
			want: "(fn ((f (-> (Int) Int))) Int (block (call f 1)))",
		},
		{
			name: "let value spans lines",
			src:  "{\nlet value =\n if c {\n 10 } else { 20 }\nvalue\n}",
			want: "(block (let value (if c (block 10) (block 20))) value)",
		},
		{name: "leading zeros", src: "007", want: "7"},
		{name: "max int64", src: "9223372036854775807", want: "9223372036854775807"},
		{
			name: "match int and wildcard",
			src:  "match x { 0 => 1, _ => 2 }",
			want: "(match x (=> 0 1) (=> _ 2))",
		},
		{
			name: "match trailing comma",
			src:  "match x { 0 => 1, _ => 2, }",
			want: "(match x (=> 0 1) (=> _ 2))",
		},
		{
			name: "match one arm",
			src:  "match x { y => y }",
			want: "(match x (=> y y))",
		},
		{
			name: "match constructor patterns",
			src:  "match xs { Nil => 0, Cons(x, _) => x }",
			want: "(match xs (=> Nil 0) (=> (Cons x _) x))",
		},
		{
			name: "match bool patterns",
			src:  "match b { true => 1, false => 0 }",
			want: "(match b (=> true 1) (=> false 0))",
		},
		{
			name: "match arm body is match",
			src:  "match x { 0 => match y { _ => 1 } }",
			want: "(match x (=> 0 (match y (=> _ 1))))",
		},
		{
			name: "match arm body is if",
			src:  "match x { _ => if c { 1 } else { 2 } }",
			want: "(match x (=> _ (if c (block 1) (block 2))))",
		},
		{
			name: "match arm body is block",
			src:  "match x { _ => { 1 } }",
			want: "(match x (=> _ (block 1)))",
		},
		{
			name: "match arm body is binary",
			src:  "match x { y => y + 1 }",
			want: "(match x (=> y (+ y 1)))",
		},
		{
			name: "match scrutinee is call",
			src:  "match f(x) { _ => 0 }",
			want: "(match (call f x) (=> _ 0))",
		},
		{
			name: "match in binary",
			src:  "1 + match x { _ => 2 }",
			want: "(+ 1 (match x (=> _ 2)))",
		},
		{
			name: "constructor value",
			src:  "Cons(1, Nil)",
			want: "(call Cons 1 Nil)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseExpr([]byte(tt.src))
			if err != nil {
				t.Fatalf("ParseExpr(%q) error = %v", tt.src, err)
			}
			if formatted := ast.FormatExpr(got); formatted != tt.want {
				t.Errorf("FormatExpr() = %q, want %q", formatted, tt.want)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		want  string
		types int
		funcs int
	}{
		{
			name: "add",
			src: `fn add(a: Int, b: Int) -> Int {
    a + b
}
`,
			want:  "(fn add ((a Int) (b Int)) Int (block (+ a b)))",
			funcs: 1,
		},
		{
			name: "abs",
			src: `fn abs(x: Int) -> Int {
    if x < 0 {
        -x
    } else {
        x
    }
}
`,
			want:  "(fn abs ((x Int)) Int (block (if (< x 0) (block (neg x)) (block x))))",
			funcs: 1,
		},
		{
			name: "fib and main",
			src: `fn fib(n: Int) -> Int {
    if n <= 1 {
        n
    } else {
        fib(n - 1) + fib(n - 2)
    }
}

fn main() -> Int {
    fib(10)
}
`,
			want: "(fn fib ((n Int)) Int (block (if (<= n 1) (block n) (block (+ (call fib (- n 1)) (call fib (- n 2)))))))\n" +
				"(fn main () Int (block (call fib 10)))",
			funcs: 2,
		},
		{
			name: "calc",
			src: `fn calc(x: Int) -> Int {
    let doubled = x * 2
    let added = doubled + 10

    added
}
`,
			want:  "(fn calc ((x Int)) Int (block (let doubled (* x 2)) (let added (+ doubled 10)) added))",
			funcs: 1,
		},
		{
			name: "apply",
			src: `fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}
`,
			want:  "(fn apply ((f (-> (Int) Int)) (x Int)) Int (block (call f x)))",
			funcs: 1,
		},
		{
			name:  "param type Int to Int",
			src:   "fn f(f: Int -> Int) -> Int { 1 }",
			want:  "(fn f ((f (-> (Int) Int))) Int (block 1))",
			funcs: 1,
		},
		{
			name:  "param type pair to Int",
			src:   "fn f(f: (Int, Int) -> Int) -> Int { 1 }",
			want:  "(fn f ((f (-> (Int Int) Int))) Int (block 1))",
			funcs: 1,
		},
		{
			name:  "param type unit to Int",
			src:   "fn f(f: () -> Int) -> Int { 1 }",
			want:  "(fn f ((f (-> () Int))) Int (block 1))",
			funcs: 1,
		},
		{
			name:  "param type higher order",
			src:   "fn f(f: (Int -> Int) -> Int) -> Int { 1 }",
			want:  "(fn f ((f (-> ((-> (Int) Int)) Int))) Int (block 1))",
			funcs: 1,
		},
		{
			name:  "param type right associative",
			src:   "fn f(f: Int -> Int -> Int) -> Int { 1 }",
			want:  "(fn f ((f (-> (Int) (-> (Int) Int)))) Int (block 1))",
			funcs: 1,
		},
		{
			name:  "param type grouped single",
			src:   "fn f(f: (Int) -> Int) -> Int { 1 }",
			want:  "(fn f ((f (-> (Int) Int))) Int (block 1))",
			funcs: 1,
		},
		{
			name:  "grouped named type",
			src:   "fn f(x: (Int)) -> (Int) { x }",
			want:  "(fn f ((x Int)) Int (block x))",
			funcs: 1,
		},
		{
			name: "comments",
			src: `// comment before
fn add(a: Int, b: Int) -> Int { // trailing
    // inside
    a + b
}
// after
`,
			want:  "(fn add ((a Int) (b Int)) Int (block (+ a b)))",
			funcs: 1,
		},
		{
			name:  "empty",
			src:   "",
			want:  "",
			funcs: 0,
		},
		{
			name:  "int list with leading bar",
			src:   "type IntList = | Nil | Cons(Int, IntList)",
			want:  "(type IntList (Nil) (Cons Int IntList))",
			types: 1,
		},
		{
			name:  "shape without leading bar",
			src:   "type Shape = Circle(Int) | Rect(Int, Int)",
			want:  "(type Shape (Circle Int) (Rect Int Int))",
			types: 1,
		},
		{
			name:  "one constructor",
			src:   "type Unit = Unit",
			want:  "(type Unit (Unit))",
			types: 1,
		},
		{
			name:  "function type field",
			src:   "type Box = Box(Int -> Int)",
			want:  "(type Box (Box (-> (Int) Int)))",
			types: 1,
		},
		{
			name: "fn type fn",
			src: `fn a() -> Int { 1 }
type T = A
fn b() -> Int { 2 }
`,
			want: `(type T (A))
(fn a () Int (block 1))
(fn b () Int (block 2))`,
			types: 1,
			funcs: 2,
		},
		{
			name: "area and sum",
			src: `type Shape =
    | Circle(Int)
    | Rect(Int, Int)

type IntList =
    | Nil
    | Cons(Int, IntList)

fn area(s: Shape) -> Int {
    match s {
        Circle(r) => 3 * r * r,
        Rect(w, h) => w * h,
    }
}

fn sum(xs: IntList) -> Int {
    match xs {
        Nil => 0,
        Cons(x, rest) => x + sum(rest),
    }
}
`,
			want: `(type Shape (Circle Int) (Rect Int Int))
(type IntList (Nil) (Cons Int IntList))
(fn area ((s Shape)) Int (block (match s (=> (Circle r) (* (* 3 r) r)) (=> (Rect w h) (* w h)))))
(fn sum ((xs IntList)) Int (block (match xs (=> Nil 0) (=> (Cons x rest) (+ x (call sum rest))))))`,
			types: 2,
			funcs: 2,
		},
		{
			name:  "option type param",
			src:   "type Option[T] = | None | Some(T)",
			want:  "(type Option[T] (None) (Some T))",
			types: 1,
		},
		{
			name:  "pair type params",
			src:   "type Pair[A, B] = Pair(A, B)",
			want:  "(type Pair[A B] (Pair A B))",
			types: 1,
		},
		{
			name:  "list type application",
			src:   "type List[T] = Nil | Cons(T, List[T])",
			want:  "(type List[T] (Nil) (Cons T List[T]))",
			types: 1,
		},
		{
			name:  "box function type argument",
			src:   "type Box[T] = Box(T -> T)",
			want:  "(type Box[T] (Box (-> (T) T)))",
			types: 1,
		},
		{
			name:  "identity type param",
			src:   "fn identity[T](x: T) -> T { x }",
			want:  "(fn identity[T] ((x T)) T (block x))",
			funcs: 1,
		},
		{
			name:  "map type params",
			src:   "fn map[T, U](xs: List[T], f: T -> U) -> List[U] { xs }",
			want:  "(fn map[T U] ((xs List[T]) (f (-> (T) U))) List[U] (block xs))",
			funcs: 1,
		},
		{
			name:  "nested type application param",
			src:   "fn f(x: Option[List[Int]]) -> Int { 0 }",
			want:  "(fn f ((x Option[List[Int]])) Int (block 0))",
			funcs: 1,
		},
		{
			name:  "function type inside application",
			src:   "fn f(x: Option[Int -> Int]) -> Int { 0 }",
			want:  "(fn f ((x Option[(-> (Int) Int)])) Int (block 0))",
			funcs: 1,
		},
		{
			name:  "application before arrow",
			src:   "fn f(g: Option[Int] -> Int) -> Int { 0 }",
			want:  "(fn f ((g (-> (Option[Int]) Int))) Int (block 0))",
			funcs: 1,
		},
		{
			name:  "application in tuple function type",
			src:   "fn f(g: (Option[Int], Bool) -> Int) -> Int { 0 }",
			want:  "(fn f ((g (-> (Option[Int] Bool) Int))) Int (block 0))",
			funcs: 1,
		},
		{
			name:  "let annotation type application",
			src:   "fn f() -> Int { let x: Option[Int] = None x }",
			want:  "(fn f () Int (block (let x : Option[Int] None) x))",
			funcs: 1,
		},
		{
			name:  "func lit type application",
			src:   "fn f() -> Int { let g = fn(x: List[Int]) -> Int { 0 } 1 }",
			want:  "(fn f () Int (block (let g (fn ((x List[Int])) Int (block 0))) 1))",
			funcs: 1,
		},
		{
			name: "option list and map",
			src: `type Option[T] =
    | None
    | Some(T)

type List[T] =
    | Nil
    | Cons(T, List[T])

fn map[T, U](xs: List[T], f: T -> U) -> List[U] {
    match xs {
        Nil => Nil,
        Cons(x, rest) => Cons(f(x), map(rest, f)),
    }
}
`,
			want: `(type Option[T] (None) (Some T))
(type List[T] (Nil) (Cons T List[T]))
(fn map[T U] ((xs List[T]) (f (-> (T) U))) List[U] (block (match xs (=> Nil Nil) (=> (Cons x rest) (call Cons (call f x) (call map rest f))))))`,
			types: 2,
			funcs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseFile([]byte(tt.src))
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}
			if got == nil {
				t.Fatal("ParseFile() file = nil")
			}
			if len(got.Types) != tt.types {
				t.Errorf("len(Types) = %d, want %d", len(got.Types), tt.types)
			}
			if len(got.Funcs) != tt.funcs {
				t.Errorf("len(Funcs) = %d, want %d", len(got.Funcs), tt.funcs)
			}
			if formatted := ast.FormatFile(got); formatted != tt.want {
				t.Errorf("FormatFile() = %q, want %q", formatted, tt.want)
			}
		})
	}
}

func TestParseError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		expr bool
		want string
	}{
		{
			name: "assign",
			src:  "fn main() -> Int { let x = 10 x = 20 x }",
			want: "1:33: cannot assign: values are immutable",
		},
		{
			name: "if without else",
			src:  "fn f() -> Int { if true { 1 } }",
			want: "1:31: expected 'else', found '}'",
		},
		{
			name: "block without result",
			src:  "fn f() -> Int { let x = 1 }",
			want: "1:27: expected expression, found '}'",
		},
		{
			name: "param list arrow",
			src:  "fn f( -> Int { 1 }",
			want: "1:7: expected ')', found '->'",
		},
		{
			name: "top level let",
			src:  "let x = 1",
			want: "1:1: expected 'fn' or 'type', found 'let'",
		},
		{
			name: "missing result type",
			src:  "fn f() -> { 1 }",
			want: "1:11: expected type, found '{'",
		},
		{
			name: "tuple type without arrow",
			src:  "fn f(x: (Int, Int)) -> Int { 1 }",
			want: "1:19: expected '->', found ')'",
		},
		{
			name: "binary missing rhs",
			src:  "fn f() -> Int { 1 + }",
			want: "1:21: expected expression, found '}'",
		},
		{
			name: "call trailing comma",
			src:  "fn f() -> Int { f(1,) }",
			want: "1:21: expected expression, found ')'",
		},
		{
			name: "fn name is integer",
			src:  "fn 1() -> Int { 1 }",
			want: "1:4: expected identifier, found integer literal 1",
		},
		{
			name: "trailing brace",
			src:  "fn f() -> Int { 1 } }",
			want: "1:21: expected 'fn' or 'type', found '}'",
		},
		{
			name: "lexer error",
			src:  "fn f() -> Int { # }",
			want: "1:17: unexpected character '#'",
		},
		{
			name: "expr trailing token",
			src:  "1 2",
			expr: true,
			want: "1:3: expected end of file, found integer literal 2",
		},
		{
			name: "type name lowercase",
			src:  "type shape = Circle",
			want: "1:6: type name 'shape' must start with an uppercase letter",
		},
		{
			name: "constructor name lowercase",
			src:  "type Shape = circle",
			want: "1:14: constructor name 'circle' must start with an uppercase letter",
		},
		{
			name: "type without constructor",
			src:  "type T =",
			want: "1:9: expected identifier, found end of file",
		},
		{
			name: "empty constructor parentheses",
			src:  "type T = A()",
			want: "1:12: expected type, found ')'",
		},
		{
			name: "trailing constructor bar",
			src:  "type T = A | ",
			want: "1:14: expected identifier, found end of file",
		},
		{
			name: "match without arms",
			src:  "match x { }",
			expr: true,
			want: "1:11: expected pattern, found '}'",
		},
		{
			name: "match missing comma",
			src:  "match x { 0 => 1 1 => 2 }",
			expr: true,
			want: "1:18: expected ',' or '}', found integer literal 1",
		},
		{
			name: "match missing fat arrow",
			src:  "match x { 0 1 }",
			expr: true,
			want: "1:13: expected '=>', found integer literal 1",
		},
		{
			name: "negative int pattern",
			src:  "match x { -1 => 0 }",
			expr: true,
			want: "1:11: expected pattern, found '-'",
		},
		{
			name: "nested constructor pattern",
			src:  "match x { Cons(Nil, _) => 0 }",
			expr: true,
			want: "1:16: nested patterns are not supported in v0.2",
		},
		{
			name: "nested int pattern",
			src:  "match x { Cons(1, _) => 0 }",
			expr: true,
			want: "1:16: nested patterns are not supported in v0.2",
		},
		{
			name: "empty constructor pattern",
			src:  "match x { Nil() => 0 }",
			expr: true,
			want: "1:15: expected pattern, found ')'",
		},
		{
			name: "var pattern called",
			src:  "match x { f(y) => 0 }",
			expr: true,
			want: "1:12: expected '=>', found '('",
		},
		{
			name: "match double comma",
			src:  "match x { _ => 0 ,, }",
			expr: true,
			want: "1:19: expected pattern, found ','",
		},
		{
			name: "underscore expression",
			src:  "_ + 1",
			expr: true,
			want: "1:1: expected expression, found '_'",
		},
		{
			name: "let underscore",
			src:  "{ let _ = 1 1 }",
			expr: true,
			want: "1:7: expected identifier, found '_'",
		},
		{
			name: "empty type param list",
			src:  "fn f[]() -> Int { 1 }",
			want: "1:6: expected identifier, found ']'",
		},
		{
			name: "lowercase type param",
			src:  "fn f[t]() -> Int { 1 }",
			want: "1:6: type parameter name 't' must start with an uppercase letter",
		},
		{
			name: "trailing comma in type params",
			src:  "fn f[T,]() -> Int { 1 }",
			want: "1:8: expected identifier, found ']'",
		},
		{
			name: "missing comma in type params",
			src:  "fn f[T U]() -> Int { 1 }",
			want: `1:8: expected ']', found identifier "U"`,
		},
		{
			name: "empty type param list on type",
			src:  "type T[] = A",
			want: "1:8: expected identifier, found ']'",
		},
		{
			name: "lowercase type param on type",
			src:  "type Box[a] = B(a)",
			want: "1:10: type parameter name 'a' must start with an uppercase letter",
		},
		{
			name: "type params without equals",
			src:  "type Option[T] Some(T)",
			want: `1:16: expected '=', found identifier "Some"`,
		},
		{
			name: "empty type arguments",
			src:  "fn f(x: Option[]) -> Int { 1 }",
			want: "1:16: expected type, found ']'",
		},
		{
			name: "unclosed type arguments",
			src:  "fn f(x: Option[Int) -> Int { 1 }",
			want: "1:19: expected ']', found ')'",
		},
		{
			name: "missing comma in type arguments",
			src:  "fn f(x: Pair[Int Bool]) -> Int { 1 }",
			want: `1:18: expected ']', found identifier "Bool"`,
		},
		{
			name: "explicit type arguments on call",
			src:  "identity[Int](1)",
			expr: true,
			want: "1:9: explicit type arguments are not supported in v0.3",
		},
		{
			name: "explicit type arguments after call",
			src:  "f(x)[Int]",
			expr: true,
			want: "1:5: explicit type arguments are not supported in v0.3",
		},
		{
			name: "func lit type params",
			src:  "fn[T](x: T) -> T { x }",
			expr: true,
			want: "1:3: expected '(', found '['",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var err error
			if tt.expr {
				_, err = ParseExpr([]byte(tt.src))
			} else {
				_, err = ParseFile([]byte(tt.src))
			}
			requireDiag(t, err, tt.want)
		})
	}
}

func TestTypeParamsAbsent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		check func(t *testing.T, file *ast.File)
	}{
		{
			name: "type decl",
			src:  "type IntList = Nil | Cons(Int, IntList)",
			check: func(t *testing.T, file *ast.File) {
				t.Helper()
				decl := file.Types[0]
				if decl.TypeParams != nil {
					t.Errorf("TypeParams = %#v, want nil", decl.TypeParams)
				}
				for _, field := range decl.Ctors[1].Fields {
					named, ok := field.(*ast.NamedType)
					if !ok {
						t.Fatalf("field type = %T, want *ast.NamedType", field)
					}
					if named.Args != nil {
						t.Errorf("NamedType %s Args = %#v, want nil", named.Name, named.Args)
					}
				}
			},
		},
		{
			name: "func decl",
			src:  "fn f(x: Int) -> Int { x }",
			check: func(t *testing.T, file *ast.File) {
				t.Helper()
				fn := file.Funcs[0]
				if fn.TypeParams != nil {
					t.Errorf("TypeParams = %#v, want nil", fn.TypeParams)
				}
				for _, typ := range []ast.TypeExpr{fn.Params[0].Type, fn.Result} {
					named, ok := typ.(*ast.NamedType)
					if !ok {
						t.Fatalf("type = %T, want *ast.NamedType", typ)
					}
					if named.Args != nil {
						t.Errorf("NamedType %s Args = %#v, want nil", named.Name, named.Args)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, err := ParseFile([]byte(tt.src))
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}
			tt.check(t, file)
		})
	}
}

func TestTypeParamNameAndPos(t *testing.T) {
	t.Parallel()

	const src = "fn map[T, U](xs: List[T], f: T -> U) -> List[U] { xs }"
	tests := []struct {
		name string
		idx  int
		want string
		line int
		col  int
	}{
		{name: "T", idx: 0, want: "T", line: 1, col: 8},
		{name: "U", idx: 1, want: "U", line: 1, col: 11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, err := ParseFile([]byte(src))
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}
			params := file.Funcs[0].TypeParams
			if params == nil {
				t.Fatal("TypeParams = nil, want two type parameters")
			}
			if len(params) != 2 {
				t.Fatalf("len(TypeParams) = %d, want 2", len(params))
			}
			got := params[tt.idx]
			if got.Name != tt.want {
				t.Errorf("Name = %q, want %q", got.Name, tt.want)
			}
			if got.Pos != (diag.Pos{Line: tt.line, Col: tt.col}) {
				t.Errorf("Pos = %s, want %d:%d", got.Pos, tt.line, tt.col)
			}
		})
	}
}

func requireDiag(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want %q", want)
	}
	if _, ok := err.(*diag.Error); !ok {
		t.Fatalf("error type = %T, want *diag.Error", err)
	}
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}
