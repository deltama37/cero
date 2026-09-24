package lower

import (
	"strings"
	"testing"

	"github.com/deltama37/cero/internal/ir"
	"github.com/deltama37/cero/internal/parser"
	"github.com/deltama37/cero/internal/typecheck"
)

func TestLower(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "main returns constant",
			src:  "fn main() -> Int { 42 }\n",
			want: lines(
				"(func 0 main (sig () Int) (locals) 42)",
				"(table)",
				"(main 0)",
			),
		},
		{
			name: "fib recursion and main not first",
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
			want: lines(
				"(func 0 fib (sig (Int) Int) (locals Int) (if (le (local 0) 1) (local 0) (add (call 0 (sub (local 0) 1)) (call 0 (sub (local 0) 2)))))",
				"(func 1 main (sig () Int) (locals) (call 0 10))",
				"(table)",
				"(main 1)",
			),
		},
		{
			name: "calc locals",
			src: `fn calc(x: Int) -> Int {
    let doubled = x * 2
    let added = doubled + 10
    added
}

fn main() -> Int {
    calc(1)
}
`,
			want: lines(
				"(func 0 calc (sig (Int) Int) (locals Int Int Int) (block (let 1 (mul (local 0) 2)) (let 2 (add (local 1) 10)) (local 2)))",
				"(func 1 main (sig () Int) (locals) (call 0 1))",
				"(table)",
				"(main 1)",
			),
		},
		{
			name: "shadowing",
			src: `fn main() -> Int {
    let x = 1
    let x = x + 1
    x
}
`,
			want: lines(
				"(func 0 main (sig () Int) (locals Int Int) (block (let 0 1) (let 1 (add (local 0) 1)) (local 1)))",
				"(table)",
				"(main 0)",
			),
		},
		{
			name: "parameter shadowed by let",
			src: `fn f(x: Int) -> Int {
    let x = x + 1
    x
}

fn main() -> Int {
    f(1)
}
`,
			want: lines(
				"(func 0 f (sig (Int) Int) (locals Int Int) (block (let 1 (add (local 0) 1)) (local 1)))",
				"(func 1 main (sig () Int) (locals) (call 0 1))",
				"(table)",
				"(main 1)",
			),
		},
		{
			name: "nested blocks without let",
			src: `fn main() -> Int {
    let x = { { 1 } }
    { x }
}
`,
			want: lines(
				"(func 0 main (sig () Int) (locals Int) (block (let 0 1) (local 0)))",
				"(table)",
				"(main 0)",
			),
		},
		{
			name: "let value lowered before local is allocated",
			src: `fn main() -> Int {
    let x = {
        let y = 1
        y + 1
    }
    x
}
`,
			want: lines(
				"(func 0 main (sig () Int) (locals Int Int) (block (let 1 (block (let 0 1) (add (local 0) 1))) (local 1)))",
				"(table)",
				"(main 0)",
			),
		},
		{
			name: "short-circuit and unary",
			src: `fn main() -> Int {
    if true && false || !true {
        -1
    } else {
        0
    }
}
`,
			want: lines(
				"(func 0 main (sig () Int) (locals) (if (if (if true false false) true (not true)) (neg 1) 0))",
				"(table)",
				"(main 0)",
			),
		},
		{
			name: "bool equality",
			src: `fn main() -> Int {
    if true == false { 1 } else { 0 }
}
`,
			want: lines(
				"(func 0 main (sig () Int) (locals) (if (eq true false) 1 0))",
				"(table)",
				"(main 0)",
			),
		},
		{
			name: "arithmetic and comparisons",
			src: `fn main() -> Int {
    let n = 8 / 2
    if n < 5 && n <= 5 && n > 3 && n >= 3 && n == 4 && n != 5 {
        n
    } else {
        0
    }
}
`,
			want: lines(
				"(func 0 main (sig () Int) (locals Int) (block (let 0 (div 8 2)) (if (if (if (if (if (if (lt (local 0) 5) (le (local 0) 5) false) (gt (local 0) 3) false) (ge (local 0) 3) false) (eq (local 0) 4) false) (ne (local 0) 5) false) (local 0) 0)))",
				"(table)",
				"(main 0)",
			),
		},
		{
			name: "else if",
			src: `fn sign(n: Int) -> Int {
    if n < 0 {
        -1
    } else if n == 0 {
        0
    } else {
        1
    }
}

fn main() -> Int {
    sign(2)
}
`,
			want: lines(
				"(func 0 sign (sig (Int) Int) (locals Int) (if (lt (local 0) 0) (neg 1) (if (eq (local 0) 0) 0 1)))",
				"(func 1 main (sig () Int) (locals) (call 0 2))",
				"(table)",
				"(main 1)",
			),
		},
		{
			name: "top-level function used as a value",
			src: `fn double(x: Int) -> Int {
    x * 2
}

fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    apply(double, 3)
}
`,
			want: lines(
				"(func 0 double (sig (Int) Int) (locals Int) (mul (local 0) 2))",
				"(func 1 apply (sig (FuncRef Int) Int) (locals FuncRef Int) (call.indirect (sig (Int) Int) (local 0) (local 1)))",
				"(func 2 main (sig () Int) (locals) (call 1 (func.ref 0) 3))",
				"(table 0)",
				"(main 2)",
			),
		},
		{
			name: "function value recorded once",
			src: `fn double(x: Int) -> Int {
    x * 2
}

fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    apply(double, apply(double, 3))
}
`,
			want: lines(
				"(func 0 double (sig (Int) Int) (locals Int) (mul (local 0) 2))",
				"(func 1 apply (sig (FuncRef Int) Int) (locals FuncRef Int) (call.indirect (sig (Int) Int) (local 0) (local 1)))",
				"(func 2 main (sig () Int) (locals) (call 1 (func.ref 0) (call 1 (func.ref 0) 3)))",
				"(table 0)",
				"(main 2)",
			),
		},
		{
			name: "anonymous function",
			src: `fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    apply(fn(x: Int) -> Int { x * 2 }, 3)
}
`,
			want: lines(
				"(func 0 apply (sig (FuncRef Int) Int) (locals FuncRef Int) (call.indirect (sig (Int) Int) (local 0) (local 1)))",
				"(func 1 main (sig () Int) (locals) (call 0 (func.ref 2) 3))",
				"(func 2 lambda$0 (sig (Int) Int) (locals Int) (mul (local 0) 2))",
				"(table 2)",
				"(main 1)",
			),
		},
		{
			name: "nested anonymous functions",
			src: `fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    apply(fn(x: Int) -> Int {
        apply(fn(y: Int) -> Int { y + 1 }, x)
    }, 1)
}
`,
			want: lines(
				"(func 0 apply (sig (FuncRef Int) Int) (locals FuncRef Int) (call.indirect (sig (Int) Int) (local 0) (local 1)))",
				"(func 1 main (sig () Int) (locals) (call 0 (func.ref 2) 1))",
				"(func 2 lambda$0 (sig (Int) Int) (locals Int) (call 0 (func.ref 3) (local 0)))",
				"(func 3 lambda$1 (sig (Int) Int) (locals Int) (add (local 0) 1))",
				"(table 3 2)",
				"(main 1)",
			),
		},
		{
			name: "call function parameter",
			src: `fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    0
}
`,
			want: lines(
				"(func 0 apply (sig (FuncRef Int) Int) (locals FuncRef Int) (call.indirect (sig (Int) Int) (local 0) (local 1)))",
				"(func 1 main (sig () Int) (locals) 0)",
				"(table)",
				"(main 1)",
			),
		},
		{
			name: "function returning a function",
			src: `fn inc(x: Int) -> Int {
    x + 1
}

fn dec(x: Int) -> Int {
    x - 1
}

fn pick(b: Bool) -> Int -> Int {
    if b { inc } else { dec }
}

fn main() -> Int {
    pick(true)(3)
}
`,
			want: lines(
				"(func 0 inc (sig (Int) Int) (locals Int) (add (local 0) 1))",
				"(func 1 dec (sig (Int) Int) (locals Int) (sub (local 0) 1))",
				"(func 2 pick (sig (Bool) FuncRef) (locals Bool) (if (local 0) (func.ref 0) (func.ref 1)))",
				"(func 3 main (sig () Int) (locals) (call.indirect (sig (Int) Int) (call 2 true) 3))",
				"(table 0 1)",
				"(main 3)",
			),
		},
		{
			name: "mutual recursion",
			src: `fn isEven(n: Int) -> Bool {
    if n == 0 {
        true
    } else {
        isOdd(n - 1)
    }
}

fn isOdd(n: Int) -> Bool {
    if n == 0 {
        false
    } else {
        isEven(n - 1)
    }
}

fn main() -> Int {
    if isEven(4) { 1 } else { 0 }
}
`,
			want: lines(
				"(func 0 isEven (sig (Int) Bool) (locals Int) (if (eq (local 0) 0) true (call 1 (sub (local 0) 1))))",
				"(func 1 isOdd (sig (Int) Bool) (locals Int) (if (eq (local 0) 0) false (call 0 (sub (local 0) 1))))",
				"(func 2 main (sig () Int) (locals) (if (call 0 4) 1 0))",
				"(table)",
				"(main 2)",
			),
		},
		{
			name: "anonymous function calls top-level",
			src: `fn inc(n: Int) -> Int {
    n + 1
}

fn main() -> Int {
    let f = fn(n: Int) -> Int { inc(n) }
    f(1)
}
`,
			want: lines(
				"(func 0 inc (sig (Int) Int) (locals Int) (add (local 0) 1))",
				"(func 1 main (sig () Int) (locals FuncRef) (block (let 0 (func.ref 2)) (call.indirect (sig (Int) Int) (local 0) 1)))",
				"(func 2 lambda$0 (sig (Int) Int) (locals Int) (call 0 (local 0)))",
				"(table 2)",
				"(main 1)",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile([]byte(tt.src))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			info, err := typecheck.Check(file)
			if err != nil {
				t.Fatalf("typecheck: %v", err)
			}
			got := ir.Format(Lower(file, info))
			if got != tt.want {
				t.Errorf("IR mismatch\ngot:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func lines(parts ...string) string {
	return strings.Join(parts, "\n")
}
