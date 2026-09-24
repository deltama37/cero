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
			want: "1:1: expected 'fn', found 'let'",
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
			want: "1:21: expected 'fn', found '}'",
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
