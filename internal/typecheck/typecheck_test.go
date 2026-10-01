package typecheck

import (
	"fmt"
	"testing"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/parser"
	"github.com/deltama37/cero/internal/types"
)

func TestCheckSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		wantMain string
		check    func(*testing.T, *ast.File, *Info)
	}{
		{
			name: "add",
			src: `fn add(a: Int, b: Int) -> Int {
    a + b
}

fn main() -> Int {
    add(1, 2)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantType(t, info, funcByName(t, file, "add").Body.Result, "Int")
			},
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

fn main() -> Int {
    abs(-1)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantType(t, info, funcByName(t, file, "abs").Body.Result, "Int")
			},
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
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				fib := funcByName(t, file, "fib")
				wantType(t, info, fib.Body.Result, "Int")
				ids := findIdents(fib.Body, "fib")
				if len(ids) != 2 {
					t.Fatalf("fib references = %d, want 2", len(ids))
				}
				for _, id := range ids {
					if info.Uses[id] != info.Funcs[fib] {
						t.Errorf("recursive fib resolved to %#v", info.Uses[id])
					}
				}
			},
		},
		{
			name: "calc",
			src: `fn calc(x: Int) -> Int {
    let doubled = x * 2
    let added = doubled + 10

    added
}

fn main() -> Int {
    calc(1)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantType(t, info, funcByName(t, file, "calc").Body.Result, "Int")
			},
		},
		{
			name: "apply",
			src: `fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn inc(n: Int) -> Int {
    n + 1
}

fn main() -> Int {
    apply(inc, 1)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				apply := funcByName(t, file, "apply")
				if got := info.Params[apply.Params[0]].Type.String(); got != "Int -> Int" {
					t.Errorf("param f type = %s, want Int -> Int", got)
				}
				wantType(t, info, apply.Body.Result, "Int")
			},
		},
		{
			name: "let double",
			src: `fn main() -> Int {
    let double = fn(x: Int) -> Int { x * 2 }
    double(21)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				lit, ok := mainFn.Body.Lets[0].Value.(*ast.FuncLit)
				if !ok {
					t.Fatalf("double value type = %T, want *ast.FuncLit", mainFn.Body.Lets[0].Value)
				}
				wantType(t, info, lit, "Int -> Int")
				if got := info.Defs[mainFn.Body.Lets[0]].Type.String(); got != "Int -> Int" {
					t.Errorf("double type = %s, want Int -> Int", got)
				}
				if got := info.FuncLits[lit].String(); got != "Int -> Int" {
					t.Errorf("signature = %s, want Int -> Int", got)
				}
			},
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
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				isEven := funcByName(t, file, "isEven")
				isOdd := funcByName(t, file, "isOdd")
				odds := findIdents(isEven.Body, "isOdd")
				evens := findIdents(isOdd.Body, "isEven")
				if len(odds) != 1 || info.Uses[odds[0]] != info.Funcs[isOdd] {
					t.Errorf("isOdd from isEven resolved to %#v", usesOf(info, odds))
				}
				if len(evens) != 1 || info.Uses[evens[0]] != info.Funcs[isEven] {
					t.Errorf("isEven from isOdd resolved to %#v", usesOf(info, evens))
				}
			},
		},
		{
			name: "shadowing",
			src: `fn shadow() -> Bool {
    let x = 1
    let x = x == 1
    x
}

fn main() -> Int {
    if shadow() { 1 } else { 0 }
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				shadow := funcByName(t, file, "shadow")
				first := info.Defs[shadow.Body.Lets[0]]
				second := info.Defs[shadow.Body.Lets[1]]
				if first == nil || second == nil || first == second {
					t.Fatalf("shadow symbols first=%v second=%v", first, second)
				}
				if first.Type.String() != "Int" || second.Type.String() != "Bool" {
					t.Fatalf("let types = %s, %s", first.Type, second.Type)
				}
				bin, ok := shadow.Body.Lets[1].Value.(*ast.BinaryExpr)
				if !ok {
					t.Fatalf("second value type = %T", shadow.Body.Lets[1].Value)
				}
				rhs, ok := bin.X.(*ast.Ident)
				if !ok {
					t.Fatalf("rhs type = %T", bin.X)
				}
				if info.Uses[rhs] != first {
					t.Error("right-hand side x does not use the outer let")
				}
				result, ok := shadow.Body.Result.(*ast.Ident)
				if !ok {
					t.Fatalf("result type = %T", shadow.Body.Result)
				}
				if info.Uses[result] != second {
					t.Error("result x does not use the inner let")
				}
				wantType(t, info, result, "Bool")
			},
		},
		{
			name: "outer scope",
			src: `fn main() -> Int {
    let x = 1
    let y = {
        let z = 2
        x + z
    }
    y
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				outer := info.Defs[mainFn.Body.Lets[0]]
				block, ok := mainFn.Body.Lets[1].Value.(*ast.BlockExpr)
				if !ok {
					t.Fatalf("y value type = %T", mainFn.Body.Lets[1].Value)
				}
				add, ok := block.Result.(*ast.BinaryExpr)
				if !ok {
					t.Fatalf("inner result type = %T", block.Result)
				}
				x, ok := add.X.(*ast.Ident)
				if !ok {
					t.Fatalf("left type = %T", add.X)
				}
				z, ok := add.Y.(*ast.Ident)
				if !ok {
					t.Fatalf("right type = %T", add.Y)
				}
				if info.Uses[x] != outer {
					t.Error("nested x does not use the outer let")
				}
				if info.Uses[z] != info.Defs[block.Lets[0]] {
					t.Error("z does not use the inner let")
				}
				wantType(t, info, block, "Int")
			},
		},
		{
			name: "function returning function",
			src: `fn inc(x: Int) -> Int {
    x + 1
}

fn pick(b: Bool) -> Int -> Int {
    if b { inc } else { fn(x: Int) -> Int { x - 1 } }
}

fn main() -> Int {
    pick(true)(1)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				pick := funcByName(t, file, "pick")
				wantType(t, info, pick.Body.Result, "Int -> Int")
				call, ok := funcByName(t, file, "main").Body.Result.(*ast.CallExpr)
				if !ok {
					t.Fatal("main result is not a call")
				}
				inner, ok := call.Fn.(*ast.CallExpr)
				if !ok {
					t.Fatalf("callee type = %T", call.Fn)
				}
				wantType(t, info, inner, "Int -> Int")
				wantType(t, info, call, "Int")
			},
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
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				inc := funcByName(t, file, "inc")
				mainFn := funcByName(t, file, "main")
				lit, ok := mainFn.Body.Lets[0].Value.(*ast.FuncLit)
				if !ok {
					t.Fatalf("f value type = %T", mainFn.Body.Lets[0].Value)
				}
				call, ok := lit.Body.Result.(*ast.CallExpr)
				if !ok {
					t.Fatalf("func lit result type = %T", lit.Body.Result)
				}
				id, ok := call.Fn.(*ast.Ident)
				if !ok {
					t.Fatalf("callee type = %T", call.Fn)
				}
				if info.Uses[id] != info.Funcs[inc] {
					t.Error("inc inside the anonymous function does not use the top-level function")
				}
			},
		},
		{
			name: "annotated let",
			src: `fn inc(n: Int) -> Int {
    n + 1
}

fn main() -> Int {
    let f: Int -> Int = inc
    f(1)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				inc := funcByName(t, file, "inc")
				mainFn := funcByName(t, file, "main")
				if got := info.Defs[mainFn.Body.Lets[0]].Type.String(); got != "Int -> Int" {
					t.Errorf("f type = %s, want Int -> Int", got)
				}
				id, ok := mainFn.Body.Lets[0].Value.(*ast.Ident)
				if !ok {
					t.Fatalf("f value type = %T", mainFn.Body.Lets[0].Value)
				}
				if info.Uses[id] != info.Funcs[inc] {
					t.Error("inc does not use the top-level function")
				}
			},
		},
		{
			name: "bool equality",
			src: `fn main() -> Int {
    if true == false { 1 } else { 0 }
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				ife, ok := funcByName(t, file, "main").Body.Result.(*ast.IfExpr)
				if !ok {
					t.Fatal("main result is not an if")
				}
				wantType(t, info, ife.Cond, "Bool")
			},
		},
		{
			name: "local shadows function",
			src: `fn fib(n: Int) -> Int {
    if n <= 1 {
        n
    } else {
        fib(n - 1) + fib(n - 2)
    }
}

fn main() -> Int {
    let fib = 1
    fib + 1
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				fib := funcByName(t, file, "fib")
				for _, id := range findIdents(fib.Body, "fib") {
					if info.Uses[id] != info.Funcs[fib] || info.Uses[id].Kind != SymFunc {
						t.Error("fib inside the function does not use the function symbol")
					}
				}
				mainFn := funcByName(t, file, "main")
				bin, ok := mainFn.Body.Result.(*ast.BinaryExpr)
				if !ok {
					t.Fatalf("main result type = %T", mainFn.Body.Result)
				}
				id, ok := bin.X.(*ast.Ident)
				if !ok {
					t.Fatalf("left type = %T", bin.X)
				}
				local := info.Defs[mainFn.Body.Lets[0]]
				if info.Uses[id] != local || local.Kind != SymLocal {
					t.Error("fib in main does not use the local")
				}
				wantType(t, info, bin, "Int")
			},
		},
		{
			name: "uses of shadowed lets",
			src: `fn main() -> Int {
    let x = 1
    let y = x
    let x = 2
    let z = x
    z
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				lets := mainFn.Body.Lets
				x1 := info.Defs[lets[0]]
				x2 := info.Defs[lets[2]]
				yVal, ok := lets[1].Value.(*ast.Ident)
				if !ok {
					t.Fatalf("y value type = %T", lets[1].Value)
				}
				zVal, ok := lets[3].Value.(*ast.Ident)
				if !ok {
					t.Fatalf("z value type = %T", lets[3].Value)
				}
				if x1 == nil || x2 == nil || x1 == x2 {
					t.Fatalf("let symbols x1=%v x2=%v", x1, x2)
				}
				if info.Uses[yVal] != x1 {
					t.Error("y does not refer to the first x")
				}
				if info.Uses[zVal] != x2 {
					t.Error("z does not refer to the second x")
				}
			},
		},
		{
			name: "operators",
			src: `fn main() -> Int {
    let n = 1 + 2 - 3 * 4 / 5
    let b = n < 1 && n <= 1 || n > 0 && n >= 0
    let eq = n == 0 || n != 1
    let flag = !false && (true || false)
    if b && eq || flag && -n < 1 { n } else { 0 }
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				lets := funcByName(t, file, "main").Body.Lets
				got := []string{
					info.Defs[lets[0]].Type.String(),
					info.Defs[lets[1]].Type.String(),
					info.Defs[lets[2]].Type.String(),
					info.Defs[lets[3]].Type.String(),
				}
				want := []string{"Int", "Bool", "Bool", "Bool"}
				for i := range want {
					if got[i] != want[i] {
						t.Errorf("let %d type = %s, want %s", i, got[i], want[i])
					}
				}
			},
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
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantType(t, info, funcByName(t, file, "sign").Body.Result, "Int")
			},
		},
		{
			name: "let sees outer parameter",
			src: `fn f(x: Int) -> Int {
    let x = x + 1
    x
}

fn main() -> Int {
    f(1)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				fn := funcByName(t, file, "f")
				param := info.Params[fn.Params[0]]
				local := info.Defs[fn.Body.Lets[0]]
				bin, ok := fn.Body.Lets[0].Value.(*ast.BinaryExpr)
				if !ok {
					t.Fatalf("let value type = %T", fn.Body.Lets[0].Value)
				}
				rhs, ok := bin.X.(*ast.Ident)
				if !ok {
					t.Fatalf("rhs type = %T", bin.X)
				}
				if info.Uses[rhs] != param {
					t.Error("right-hand side x does not use the parameter")
				}
				result, ok := fn.Body.Result.(*ast.Ident)
				if !ok {
					t.Fatalf("result type = %T", fn.Body.Result)
				}
				if info.Uses[result] != local {
					t.Error("result x does not use the let")
				}
			},
		},
		{
			name: "parameter shadows outer let",
			src: `fn main() -> Int {
    let x = 1
    let f = fn(x: Int) -> Int { x + 1 }
    f(2)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				lit, ok := mainFn.Body.Lets[1].Value.(*ast.FuncLit)
				if !ok {
					t.Fatalf("f value type = %T", mainFn.Body.Lets[1].Value)
				}
				bin, ok := lit.Body.Result.(*ast.BinaryExpr)
				if !ok {
					t.Fatalf("func lit result type = %T", lit.Body.Result)
				}
				id, ok := bin.X.(*ast.Ident)
				if !ok {
					t.Fatalf("left type = %T", bin.X)
				}
				if info.Uses[id] != info.Params[lit.Params[0]] {
					t.Error("inner x does not use the parameter")
				}
				if info.Uses[id] == info.Defs[mainFn.Body.Lets[0]] {
					t.Error("inner x captured the outer let")
				}
			},
		},
		{
			name: "zero argument call",
			src: `fn main() -> Int {
    let f = fn() -> Int { 1 }
    f()
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				lit := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.FuncLit)
				wantType(t, info, lit, "() -> Int")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, info := mustCheck(t, tt.src)
			assertComplete(t, file, info)
			if tt.wantMain != "" {
				mainFn := funcByName(t, file, "main")
				wantType(t, info, mainFn.Body.Result, tt.wantMain)
			}
			if tt.check != nil {
				tt.check(t, file, info)
			}
		})
	}
}

func TestCheckError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "annotated let",
			src:  "fn main() -> Int { let x: Int = true x }",
			want: "1:33: expected Int, found Bool",
		},
		{
			name: "addition",
			src:  "fn main() -> Int { 1 + false }",
			want: "1:24: expected Int, found Bool",
		},
		{
			name: "if branches",
			src: `fn main() -> Int {
    if true {
        10
    } else {
        false
    }
}
`,
			want: "5:9: if branches have different types: Int and Bool",
		},
		{
			name: "if condition",
			src:  "fn main() -> Int { if 1 { 1 } else { 2 } }",
			want: "1:23: expected Bool, found Int",
		},
		{
			name: "not",
			src:  "fn main() -> Int { !1 }",
			want: "1:21: expected Bool, found Int",
		},
		{
			name: "negate",
			src:  "fn main() -> Int { -true }",
			want: "1:21: expected Int, found Bool",
		},
		{
			name: "and",
			src:  "fn main() -> Int { 1 && true }",
			want: "1:20: expected Bool, found Int",
		},
		{
			name: "less",
			src:  "fn main() -> Int { 1 < true }",
			want: "1:24: expected Int, found Bool",
		},
		{
			name: "equal int and bool",
			src:  "fn main() -> Int { 1 == true }",
			want: "1:25: expected Int, found Bool",
		},
		{
			name: "compare functions",
			src: `fn id(x: Int) -> Int { x }
fn main() -> Int { if id == id { 1 } else { 0 } }
`,
			want: "2:23: cannot compare values of type Int -> Int",
		},
		{
			name: "undefined name",
			src:  "fn main() -> Int { missing }",
			want: "1:20: undefined name 'missing'",
		},
		{
			name: "unknown param type",
			src:  "fn f(x: String) -> Int { 1 }",
			want: "1:9: unknown type 'String'",
		},
		{
			name: "unknown result type",
			src:  "fn f() -> String { 1 }",
			want: "1:11: unknown type 'String'",
		},
		{
			name: "unknown let type",
			src:  "fn main() -> Int { let x: String = 1 x }",
			want: "1:27: unknown type 'String'",
		},
		{
			name: "unknown type inside function type",
			src:  "fn f(g: Int -> String) -> Int { 1 }",
			want: "1:16: unknown type 'String'",
		},
		{
			name: "duplicate function",
			src: `fn f() -> Int { 1 }
fn f() -> Int { 2 }
fn main() -> Int { f() }
`,
			want: "2:4: duplicate function 'f'",
		},
		{
			name: "duplicate parameter",
			src: `fn f(x: Int, x: Int) -> Int { x }
fn main() -> Int { 1 }
`,
			want: "1:14: duplicate parameter 'x'",
		},
		{
			name: "wrong arity",
			src: `fn add(a: Int, b: Int) -> Int { a + b }
fn main() -> Int { add(true) }
`,
			want: "2:23: wrong number of arguments: expected 2, found 1",
		},
		{
			name: "wrong argument type",
			src: `fn add(a: Int, b: Int) -> Int { a + b }
fn main() -> Int { add(true, false) }
`,
			want: "2:24: expected Int, found Bool",
		},
		{
			name: "call non-function",
			src:  "fn main() -> Int { 1(2) }",
			want: "1:20: cannot call non-function value of type Int",
		},
		{
			name: "nested block result",
			src: `fn main() -> Int {
    {
        {
            true
        }
    }
}
`,
			want: "4:13: expected Int, found Bool",
		},
		{
			name: "capture parameter",
			src: `fn f(x: Int) -> Int {
    let g = fn() -> Int { x }
    g()
}
fn main() -> Int { f(1) }
`,
			want: "2:27: cannot capture 'x' in anonymous function: closures are not supported in v0.1",
		},
		{
			name: "capture let",
			src: `fn main() -> Int {
    let x = 1
    let g = fn() -> Int { x }
    g()
}
`,
			want: "3:27: cannot capture 'x' in anonymous function: closures are not supported in v0.1",
		},
		{
			name: "capture shadowed function",
			src: `fn id(x: Int) -> Int { x }
fn main() -> Int {
    let id = 1
    let f = fn() -> Int { id }
    f()
}
`,
			want: "4:27: cannot capture 'id' in anonymous function: closures are not supported in v0.1",
		},
		{
			name: "missing main",
			src:  "fn foo() -> Int { 1 }",
			want: "1:1: missing function 'main'",
		},
		{
			name: "main with parameter",
			src:  "fn main(x: Int) -> Int { x }",
			want: "1:4: function 'main' must have type () -> Int, found Int -> Int",
		},
		{
			name: "main returns bool",
			src:  "fn main() -> Bool { true }",
			want: "1:4: function 'main' must have type () -> Int, found () -> Bool",
		},
		{
			name: "body error before missing main",
			src:  "fn foo() -> Int { true }",
			want: "1:19: expected Int, found Bool",
		},
		{
			name: "body error before main signature",
			src:  "fn main() -> Bool { 1 }",
			want: "1:21: expected Bool, found Int",
		},
		{
			name: "duplicate function before body error",
			src: `fn f() -> Int { true }
fn f() -> Int { 1 }
fn main() -> Int { 1 }
`,
			want: "2:4: duplicate function 'f'",
		},
		{
			name: "signature error before body error",
			src: `fn f(x: String) -> Int { 1 }
fn g() -> Int { true }
`,
			want: "1:9: unknown type 'String'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireError(t, tt.src, tt.want)
		})
	}
}

func TestCheckDataSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		wantMain string
		check    func(*testing.T, *ast.File, *Info)
	}{
		{
			name: "shape area and intlist sum",
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

fn main() -> Int {
    area(Circle(2)) + sum(Cons(1, Cons(2, Nil)))
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantType(t, info, funcByName(t, file, "area").Body.Result, "Int")
				wantType(t, info, funcByName(t, file, "sum").Body.Result, "Int")
			},
		},
		{
			name: "type declared after function",
			src: `fn sum(xs: IntList) -> Int {
    match xs {
        Nil => 0,
        Cons(x, r) => x + sum(r),
    }
}

fn main() -> Int {
    sum(Cons(1, Nil))
}

type IntList =
    | Nil
    | Cons(Int, IntList)
`,
			wantMain: "Int",
		},
		{
			name: "mutually recursive types",
			src: `type A =
    | A0
    | A1(B)

type B =
    | B0(A)

fn main() -> Int {
    match A1(B0(A0)) {
        A0 => 0,
        A1(b) => match b {
            B0(a) => match a {
                A0 => 1,
                A1(_) => 2,
            },
        },
    }
}
`,
			wantMain: "Int",
		},
		{
			name: "function field called from match",
			src: `type Box =
    | Box(Int -> Int)

fn main() -> Int {
    let b = Box(fn(x: Int) -> Int { x + 1 })
    match b {
        Box(f) => f(1),
    }
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				m := funcByName(t, file, "main").Body.Result.(*ast.MatchExpr)
				wantType(t, info, m, "Int")
				f := m.Arms[0].Pattern.(*ast.CtorPat).Args[0].(*ast.VarPat)
				if got := info.PatVars[f].Type.String(); got != "Int -> Int" {
					t.Errorf("f type = %s, want Int -> Int", got)
				}
			},
		},
		{
			name: "nil expression type",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn main() -> Int {
    let xs = Nil
    0
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				id := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.Ident)
				wantType(t, info, id, "IntList")
				if info.Uses[id].Kind != SymCtor || info.Uses[id].Ctor.Data.Name != "IntList" {
					t.Errorf("Nil symbol = %+v", info.Uses[id])
				}
			},
		},
		{
			name: "cons expression type",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn main() -> Int {
    let xs = Cons(1, Nil)
    0
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				call := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.CallExpr)
				wantType(t, info, call, "IntList")
				id := call.Fn.(*ast.Ident)
				wantType(t, info, id, "(Int, IntList) -> IntList")
				if info.Uses[id].Kind != SymCtor {
					t.Errorf("Cons kind = %v", info.Uses[id].Kind)
				}
				wantType(t, info, call.Args[1], "IntList")
			},
		},
		{
			name: "match type is arm type",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn main() -> Int {
    let xs = Nil
    let b = match xs {
        Nil => true,
        Cons(x, r) => false,
    }
    if b { 1 } else { 0 }
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				m := funcByName(t, file, "main").Body.Lets[1].Value.(*ast.MatchExpr)
				wantType(t, info, m, "Bool")
			},
		},
		{
			name: "integer patterns",
			src: `fn f(n: Int) -> Int {
    match n {
        0 => 1,
        1 => 1,
        _ => 2,
    }
}

fn main() -> Int {
    f(0)
}
`,
			wantMain: "Int",
		},
		{
			name: "boolean patterns",
			src: `fn f(b: Bool) -> Int {
    match b {
        true => 1,
        false => 0,
    }
}

fn main() -> Int {
    f(true)
}
`,
			wantMain: "Int",
		},
		{
			name: "variable pattern",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn f(xs: IntList) -> Int {
    match xs {
        Nil => 0,
        other => 1,
    }
}

fn main() -> Int {
    f(Nil)
}
`,
			wantMain: "Int",
		},
		{
			name: "pattern variable shadows outer",
			src: `fn main() -> Int {
    let x = 1
    let y = match x {
        0 => x,
        x => x,
    }
    x + y
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				outer := info.Defs[mainFn.Body.Lets[0]]
				m := mainFn.Body.Lets[1].Value.(*ast.MatchExpr)
				scrut := m.Scrutinee.(*ast.Ident)
				arm0 := m.Arms[0].Body.(*ast.Ident)
				pat := m.Arms[1].Pattern.(*ast.VarPat)
				arm1 := m.Arms[1].Body.(*ast.Ident)
				after := mainFn.Body.Result.(*ast.BinaryExpr).X.(*ast.Ident)
				if info.Uses[scrut] != outer || info.Uses[arm0] != outer || info.Uses[after] != outer {
					t.Error("x outside the variable pattern does not use the outer let")
				}
				if info.Uses[arm1] != info.PatVars[pat] || info.PatVars[pat] == outer {
					t.Error("x in the variable arm does not use the pattern variable")
				}
			},
		},
		{
			name: "match inside anonymous function",
			src: `fn main() -> Int {
    let f = fn(n: Int) -> Int {
        match n {
            x => x,
        }
    }
    f(1)
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				lit := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.FuncLit)
				m := lit.Body.Result.(*ast.MatchExpr)
				pat := m.Arms[0].Pattern.(*ast.VarPat)
				body := m.Arms[0].Body.(*ast.Ident)
				if info.Uses[body] != info.PatVars[pat] || info.Uses[body].Kind != SymLocal {
					t.Error("pattern variable inside the anonymous function was treated as a capture")
				}
			},
		},
		{
			name: "ctor pats and pat vars",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn main() -> Int {
    let xs = Nil
    match xs {
        Nil => 0,
        Cons(x, r) => x,
    }
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				data := info.Datas[typeByName(t, file, "IntList")]
				m := funcByName(t, file, "main").Body.Result.(*ast.MatchExpr)
				nilPat := m.Arms[0].Pattern.(*ast.CtorPat)
				consPat := m.Arms[1].Pattern.(*ast.CtorPat)
				if info.CtorPats[nilPat] != data.Ctors[0] || info.CtorPats[nilPat].Index != 0 {
					t.Errorf("Nil pattern = %+v", info.CtorPats[nilPat])
				}
				if info.CtorPats[consPat] != data.Ctors[1] || info.CtorPats[consPat].Index != 1 {
					t.Errorf("Cons pattern = %+v", info.CtorPats[consPat])
				}
				x := consPat.Args[0].(*ast.VarPat)
				r := consPat.Args[1].(*ast.VarPat)
				if info.PatVars[x].Kind != SymLocal || info.PatVars[x].Type.String() != "Int" {
					t.Errorf("x = %+v", info.PatVars[x])
				}
				if info.PatVars[r].Kind != SymLocal || info.PatVars[r].Type.String() != "IntList" {
					t.Errorf("r = %+v", info.PatVars[r])
				}
				if info.Uses[m.Arms[1].Body.(*ast.Ident)] != info.PatVars[x] {
					t.Error("arm body does not use the pattern variable x")
				}
			},
		},
		{
			name: "data type annotations",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn id(xs: IntList) -> IntList {
    let ys: IntList = xs
    ys
}

fn main() -> Int {
    let zs: IntList = id(Nil)
    match zs {
        Nil => 0,
        Cons(x, r) => x,
    }
}
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				idFn := funcByName(t, file, "id")
				if got := info.Params[idFn.Params[0]].Type.String(); got != "IntList" {
					t.Errorf("param type = %s, want IntList", got)
				}
				if got := info.Funcs[idFn].Type.String(); got != "IntList -> IntList" {
					t.Errorf("id type = %s, want IntList -> IntList", got)
				}
				if got := info.Defs[idFn.Body.Lets[0]].Type.String(); got != "IntList" {
					t.Errorf("ys type = %s, want IntList", got)
				}
				mainFn := funcByName(t, file, "main")
				if got := info.Defs[mainFn.Body.Lets[0]].Type.String(); got != "IntList" {
					t.Errorf("zs type = %s, want IntList", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, info := mustCheck(t, tt.src)
			assertComplete(t, file, info)
			if tt.wantMain != "" {
				mainFn := funcByName(t, file, "main")
				wantType(t, info, mainFn.Body.Result, tt.wantMain)
			}
			if tt.check != nil {
				tt.check(t, file, info)
			}
		})
	}
}

func TestCheckDataError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "redefine built-in type",
			src:  "type Int = A\n",
			want: "1:6: cannot redefine built-in type 'Int'",
		},
		{
			name: "duplicate type",
			src: `type T = | A
type T = | B
`,
			want: "2:6: duplicate type 'T'",
		},
		{
			name: "duplicate constructor",
			src: `type T = | A
type U = | A
`,
			want: "2:12: duplicate constructor 'A'",
		},
		{
			name: "function conflicts with constructor",
			src: `fn Nil() -> Int { 1 }
type L = Nil
fn main() -> Int { 1 }
`,
			want: "1:4: function 'Nil' conflicts with constructor 'Nil'",
		},
		{
			name: "unknown field type",
			src: `type T = | A(Foo)
fn main() -> Int { 1 }
`,
			want: "1:14: unknown type 'Foo'",
		},
		{
			name: "parameter uses constructor name",
			src: `type L = Nil
fn f(Nil: Int) -> Int { 1 }
fn main() -> Int { 1 }
`,
			want: "2:6: cannot use constructor name 'Nil' as a variable",
		},
		{
			name: "let uses constructor name",
			src: `type L = Nil
fn main() -> Int {
    let Nil = 1
    1
}
`,
			want: "3:9: cannot use constructor name 'Nil' as a variable",
		},
		{
			name: "constructor used as value",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let f = Cons
    1
}
`,
			want: "3:13: constructor 'Cons' cannot be used as a value; call it with its fields",
		},
		{
			name: "nullary constructor call",
			src: `type L = Nil
fn main() -> Int { Nil() }
`,
			want: "2:23: constructor 'Nil' has no fields; write it without parentheses",
		},
		{
			name: "constructor arity",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int { Cons(1) }
`,
			want: "2:24: wrong number of arguments: expected 2, found 1",
		},
		{
			name: "constructor argument type",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int { Cons(true, Nil) }
`,
			want: "2:25: expected Int, found Bool",
		},
		{
			name: "compare data types",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int { if Nil == Nil { 1 } else { 0 } }
`,
			want: "2:23: cannot compare values of type IntList",
		},
		{
			name: "match on function",
			src: `fn id(x: Int) -> Int { x }
fn main() -> Int {
    match id {
        _ => 0,
    }
}
`,
			want: "3:11: cannot match on values of type Int -> Int",
		},
		{
			name: "int pattern on data",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        0 => 1,
        _ => 2,
    }
}
`,
			want: "5:9: expected IntList, found Int",
		},
		{
			name: "bool pattern on int",
			src: `fn main() -> Int {
    let n = 1
    match n {
        true => 1,
        _ => 2,
    }
}
`,
			want: "4:9: expected Int, found Bool",
		},
		{
			name: "constructor pattern on int",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let n = 1
    match n {
        Nil => 1,
        _ => 2,
    }
}
`,
			want: "5:9: expected Int, found IntList",
		},
		{
			name: "constructor of another type",
			src: `type Shape = | Circle(Int) | Rect(Int, Int)
type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let s = Circle(1)
    match s {
        Nil => 1,
        _ => 2,
    }
}
`,
			want: "6:9: expected Shape, found IntList",
		},
		{
			name: "unknown constructor",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        Foo => 1,
        _ => 2,
    }
}
`,
			want: "5:9: unknown constructor 'Foo'",
		},
		{
			name: "too many fields in nil pattern",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        Nil(x) => 1,
        _ => 2,
    }
}
`,
			want: "5:9: wrong number of fields in pattern 'Nil': expected 0, found 1",
		},
		{
			name: "too few fields in cons pattern",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        Cons(x) => 1,
        _ => 2,
    }
}
`,
			want: "5:9: wrong number of fields in pattern 'Cons': expected 2, found 1",
		},
		{
			name: "duplicate pattern variable",
			src: `type Pair = | Pair(Int, Int)
fn main() -> Int {
    match Pair(1, 2) {
        Pair(x, x) => x,
    }
}
`,
			want: "4:17: duplicate variable 'x' in pattern",
		},
		{
			name: "unreachable after wildcard",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        _ => 0,
        Nil => 1,
    }
}
`,
			want: "6:9: unreachable match arm",
		},
		{
			name: "unreachable after variable",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        y => 0,
        _ => 1,
    }
}
`,
			want: "6:9: unreachable match arm",
		},
		{
			name: "unreachable repeated constructor",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        Nil => 0,
        Nil => 1,
        _ => 2,
    }
}
`,
			want: "6:9: unreachable match arm",
		},
		{
			name: "unreachable wildcard after constructors",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        Nil => 0,
        Cons(x, r) => 1,
        _ => 2,
    }
}
`,
			want: "7:9: unreachable match arm",
		},
		{
			name: "unreachable wildcard after booleans",
			src: `fn main() -> Int {
    let b = true
    match b {
        true => 0,
        false => 1,
        _ => 2,
    }
}
`,
			want: "6:9: unreachable match arm",
		},
		{
			name: "unreachable repeated integer",
			src: `fn main() -> Int {
    let n = 1
    match n {
        1 => 0,
        1 => 1,
        _ => 2,
    }
}
`,
			want: "5:9: unreachable match arm",
		},
		{
			name: "non-exhaustive constructor",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        Nil => 0,
    }
}
`,
			want: "4:5: non-exhaustive match: Cons",
		},
		{
			name: "non-exhaustive constructors in order",
			src: `type Shape = | Circle(Int) | Rect(Int, Int) | Tri(Int, Int)
fn main() -> Int {
    let s = Circle(1)
    match s {
        Circle(r) => r,
    }
}
`,
			want: "4:5: non-exhaustive match: Rect, Tri",
		},
		{
			name: "non-exhaustive bool",
			src: `fn main() -> Int {
    let b = true
    match b {
        true => 1,
    }
}
`,
			want: "3:5: non-exhaustive match: false",
		},
		{
			name: "non-exhaustive int",
			src: `fn main() -> Int {
    let n = 0
    match n {
        0 => 1,
    }
}
`,
			want: "3:5: non-exhaustive match: Int values need a '_' or variable pattern",
		},
		{
			name: "match arm types differ",
			src: `type IntList = | Nil | Cons(Int, IntList)
fn main() -> Int {
    let xs = Nil
    match xs {
        Nil => 0,
        Cons(x, r) => true,
    }
}
`,
			want: "6:23: match arms have different types: Int and Bool",
		},
		{
			name: "pattern error before body error",
			src: `fn main() -> Int {
    let n = 1
    match n {
        true => missing,
        _ => 0,
    }
}
`,
			want: "4:9: expected Int, found Bool",
		},
		{
			name: "unreachable before exhaustiveness",
			src: `fn main() -> Int {
    let b = true
    match b {
        _ => 0,
        true => 1,
    }
}
`,
			want: "5:9: unreachable match arm",
		},
		{
			name: "capture pattern variable",
			src: `fn f(n: Int) -> Int {
    match n {
        x => {
            let g = fn(m: Int) -> Int {
                match m {
                    _ => x,
                }
            }
            g(1)
        },
    }
}
fn main() -> Int { f(1) }
`,
			want: "6:26: cannot capture 'x' in anonymous function: closures are not supported in v0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireError(t, tt.src, tt.want)
		})
	}
}

const genericPrelude = `type Option[T] =
    | None
    | Some(T)

type List[T] =
    | Nil
    | Cons(T, List[T])

type Pair[A, B] = Pair(A, B)

fn identity[T](x: T) -> T { x }
`

func TestCheckGenericSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		wantMain string
		check    func(*testing.T, *ast.File, *Info)
	}{
		{
			name:     "identity at two types",
			src:      genericPrelude + "fn main() -> Int { if identity(true) { identity(1) } else { 0 } }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				iff := funcByName(t, file, "main").Body.Result.(*ast.IfExpr)
				cond := iff.Cond.(*ast.CallExpr)
				then := iff.Then.Result.(*ast.CallExpr)
				wantType(t, info, cond, "Bool")
				wantType(t, info, then, "Int")
				wantTypeArgs(t, info, cond.Fn.(*ast.Ident), "Bool")
				wantTypeArgs(t, info, then.Fn.(*ast.Ident), "Int")
			},
		},
		{
			name:     "constructor type arguments",
			src:      genericPrelude + "fn main() -> Int { let o = Some(1) 0 }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				call := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.CallExpr)
				wantType(t, info, call, "Option[Int]")
				id := call.Fn.(*ast.Ident)
				wantType(t, info, id, "Int -> Option[Int]")
				wantTypeArgs(t, info, id, "Int")
			},
		},
		{
			name:     "annotation determines None",
			src:      genericPrelude + "fn main() -> Int { let o: Option[Int] = None 0 }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				id := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.Ident)
				wantType(t, info, id, "Option[Int]")
				wantTypeArgs(t, info, id, "Int")
			},
		},
		{
			name:     "later use determines None",
			src:      genericPrelude + "fn main() -> Int { let o = None match o { Some(n) => n + 1, None => 0 } }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				if got := info.Defs[mainFn.Body.Lets[0]].Type.String(); got != "Option[Int]" {
					t.Errorf("o type = %s, want Option[Int]", got)
				}
				pat := mainFn.Body.Result.(*ast.MatchExpr).Arms[0].Pattern.(*ast.CtorPat)
				n := pat.Args[0].(*ast.VarPat)
				if got := info.PatVars[n].Type.String(); got != "Int" {
					t.Errorf("n type = %s, want Int", got)
				}
			},
		},
		{
			name: "argument type determines None",
			src: genericPrelude + `fn get(o: Option[Int]) -> Int { 0 }
fn main() -> Int { get(None) }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				call := funcByName(t, file, "main").Body.Result.(*ast.CallExpr)
				wantTypeArgs(t, info, call.Args[0].(*ast.Ident), "Int")
			},
		},
		{
			name:     "type argument chain",
			src:      genericPrelude + "fn main() -> Int { let p = Pair(Some(true), Cons(1, Nil)) 0 }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				if got := info.Defs[letStmt].Type.String(); got != "Pair[Option[Bool], List[Int]]" {
					t.Errorf("p type = %s, want Pair[Option[Bool], List[Int]]", got)
				}
			},
		},
		{
			name: "recursive generic function",
			src: genericPrelude + `fn length[T](xs: List[T]) -> Int { match xs { Nil => 0, Cons(_, r) => 1 + length(r) } }
fn main() -> Int { length(Cons(1, Nil)) }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				lengthFn := funcByName(t, file, "length")
				arm := lengthFn.Body.Result.(*ast.MatchExpr).Arms[1]
				id := arm.Body.(*ast.BinaryExpr).Y.(*ast.CallExpr).Fn.(*ast.Ident)
				wantTypeArgs(t, info, id, "T")
				got, ok := info.TypeArgs[id][0].(*types.TypeParam)
				if !ok || got != info.Funcs[lengthFn].TypeParams[0] {
					t.Errorf("length type arg = %v, want the function's type parameter", info.TypeArgs[id])
				}
				mainCall := funcByName(t, file, "main").Body.Result.(*ast.CallExpr)
				wantTypeArgs(t, info, mainCall.Fn.(*ast.Ident), "Int")
			},
		},
		{
			name: "polymorphic recursion",
			src: genericPrelude + `fn depth[T](x: T, n: Int) -> Int { if n == 0 { 0 } else { 1 + depth(Cons(x, Nil), n - 1) } }
fn main() -> Int { depth(7, 3) }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				iff := funcByName(t, file, "depth").Body.Result.(*ast.IfExpr)
				call := iff.Else.(*ast.BlockExpr).Result.(*ast.BinaryExpr).Y.(*ast.CallExpr)
				wantTypeArgs(t, info, call.Fn.(*ast.Ident), "List[T]")
			},
		},
		{
			name: "type parameter in the body",
			src: genericPrelude + `fn twice[T](x: T) -> T { let f = fn(v: T) -> T { v } let y: T = f(x) f(y) }
fn main() -> Int { twice(1) }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				body := funcByName(t, file, "twice").Body
				lit := body.Lets[0].Value.(*ast.FuncLit)
				if got := info.FuncLits[lit].String(); got != "T -> T" {
					t.Errorf("func lit type = %s, want T -> T", got)
				}
				if got := info.Defs[body.Lets[1]].Type.String(); got != "T" {
					t.Errorf("y type = %s, want T", got)
				}
			},
		},
		{
			name: "pass a generic function as a value",
			src: genericPrelude + `fn apply(f: Int -> Int, x: Int) -> Int { f(x) }
fn main() -> Int { apply(identity, 7) }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				id := funcByName(t, file, "main").Body.Result.(*ast.CallExpr).Args[0].(*ast.Ident)
				wantType(t, info, id, "Int -> Int")
				wantTypeArgs(t, info, id, "Int")
			},
		},
		{
			name:     "let is monomorphic",
			src:      genericPrelude + "fn main() -> Int { let f = identity f(5) }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				if got := info.Defs[letStmt].Type.String(); got != "Int -> Int" {
					t.Errorf("f type = %s, want Int -> Int", got)
				}
			},
		},
		{
			name: "type parameter only in the result",
			src: genericPrelude + `fn none[T]() -> Option[T] { None }
fn main() -> Int { let o: Option[Bool] = none() 0 }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				call := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.CallExpr)
				wantTypeArgs(t, info, call.Fn.(*ast.Ident), "Bool")
			},
		},
		{
			name: "project a generic field",
			src: genericPrelude + `fn fst[A, B](p: Pair[A, B]) -> A { match p { Pair(a, _) => a } }
fn main() -> Int { fst(Pair(3, true)) }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				pat := funcByName(t, file, "fst").Body.Result.(*ast.MatchExpr).Arms[0].Pattern.(*ast.CtorPat)
				a := pat.Args[0].(*ast.VarPat)
				if got := info.PatVars[a].Type.String(); got != "A" {
					t.Errorf("a type = %s, want A", got)
				}
				call := funcByName(t, file, "main").Body.Result.(*ast.CallExpr)
				wantTypeArgs(t, info, call.Fn.(*ast.Ident), "Int", "Bool")
			},
		},
		{
			name: "phantom type",
			src: `type Tag[T] = Tag
fn main() -> Int { let t: Tag[Int] = Tag 0 }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				id := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.Ident)
				wantType(t, info, id, "Tag[Int]")
			},
		},
		{
			name:     "nested type application",
			src:      genericPrelude + "fn main() -> Int { let x: Option[List[Int]] = Some(Nil) 0 }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				call := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.CallExpr)
				wantType(t, info, call.Args[0].(*ast.Ident), "List[Int]")
			},
		},
		{
			name: "non-regular data type",
			src: genericPrelude + `type Nest[T] = | Flat(T) | Deep(Nest[Pair[T, T]])
fn main() -> Int { let n = Deep(Flat(Pair(1, 2))) 0 }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				if got := info.Defs[letStmt].Type.String(); got != "Nest[Int]" {
					t.Errorf("n type = %s, want Nest[Int]", got)
				}
			},
		},
		{
			name: "type declared after its use",
			src: `fn main() -> Int { let o: Opt[Int] = Non 0 }
type Opt[T] = | Non | Som(T)
`,
			wantMain: "Int",
		},
		{
			name:     "type parameter info",
			src:      genericPrelude + "fn main() -> Int { let o = Some(1) 0 }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				idFn := funcByName(t, file, "identity")
				tps := info.Funcs[idFn].TypeParams
				if len(tps) != 1 || tps[0].Name != "T" {
					t.Errorf("identity type params = %v, want [T]", tps)
				}
				pair := info.Datas[typeByName(t, file, "Pair")]
				if len(pair.Params) != 2 || pair.Params[0].Name != "A" || pair.Params[1].Name != "B" {
					t.Errorf("Pair params = %v, want [A, B]", pair.Params)
				}
				some := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.CallExpr).Fn.(*ast.Ident)
				opt := info.Datas[typeByName(t, file, "Option")]
				got := info.Uses[some].TypeParams
				if len(got) != 1 || got[0] != opt.Params[0] {
					t.Errorf("Some type param = %v, want Option's T", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, info := mustCheck(t, tt.src)
			assertComplete(t, file, info)
			if tt.wantMain != "" {
				mainFn := funcByName(t, file, "main")
				wantType(t, info, mainFn.Body.Result, tt.wantMain)
			}
			if tt.check != nil {
				tt.check(t, file, info)
			}
		})
	}
}

func TestCheckGenericError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "type parameter conflicts with Int",
			src:  "type Option[Int] = Some(Int)\n",
			want: "1:13: type parameter 'Int' conflicts with type 'Int'",
		},
		{
			name: "type parameter conflicts with a data type",
			src:  "type A = A0\nfn f[A](x: A) -> A { x }\n",
			want: "2:6: type parameter 'A' conflicts with type 'A'",
		},
		{
			name: "type parameter conflicts with a later type",
			src:  "fn f[T](x: T) -> T { x }\ntype T = T0\n",
			want: "1:6: type parameter 'T' conflicts with type 'T'",
		},
		{
			name: "duplicate function type parameter",
			src:  "fn f[T, T](x: T) -> T { x }\n",
			want: "1:9: duplicate type parameter 'T'",
		},
		{
			name: "duplicate data type parameter",
			src:  "type P[T, T] = P(T)\n",
			want: "1:11: duplicate type parameter 'T'",
		},
		{
			name: "unused type parameter",
			src:  "fn f[T](x: Int) -> Int { x }\n",
			want: "1:6: type parameter 'T' is not used in the signature of 'f'",
		},
		{
			name: "unused type parameter of main",
			src:  "fn main[T]() -> Int { 0 }\n",
			want: "1:9: type parameter 'T' is not used in the signature of 'main'",
		},
		{
			name: "main has type parameters",
			src:  "type Option[T] = | None | Some(T)\nfn main[T]() -> Option[T] { None }\n",
			want: "2:4: function 'main' cannot have type parameters",
		},
		{
			name: "missing type argument",
			src:  "type Option[T] = | None | Some(T)\nfn f(x: Option) -> Int { 0 }\n",
			want: "2:9: wrong number of type arguments for 'Option': expected 1, found 0",
		},
		{
			name: "type argument on Int",
			src:  "fn f(x: Int[Bool]) -> Int { 0 }\n",
			want: "1:9: wrong number of type arguments for 'Int': expected 0, found 1",
		},
		{
			name: "type argument on a type parameter",
			src:  "fn f[T](x: T[Int]) -> Int { 0 }\n",
			want: "1:12: wrong number of type arguments for 'T': expected 0, found 1",
		},
		{
			name: "missing type argument in a field",
			src:  "type List[T] = | Nil | Cons(T, List)\n",
			want: "1:32: wrong number of type arguments for 'List': expected 1, found 0",
		},
		{
			name: "type argument on a monomorphic type",
			src:  "type Box = Box(Int)\nfn f(x: Box[Int]) -> Int { 0 }\n",
			want: "2:9: wrong number of type arguments for 'Box': expected 0, found 1",
		},
		{
			name: "unknown type argument",
			src:  "type P[A, B] = P(A, B)\nfn f(x: P[Int, Foo]) -> Int { 0 }\n",
			want: "2:16: unknown type 'Foo'",
		},
		{
			name: "type parameter out of scope",
			src:  "fn f[T](x: T) -> T { x }\nfn g(y: T) -> Int { 0 }\n",
			want: "2:9: unknown type 'T'",
		},
		{
			name: "unknown field type parameter",
			src:  "type Box = Box(T)\n",
			want: "1:16: unknown type 'T'",
		},
		{
			name: "identity calls disagree",
			src:  "fn identity[T](x: T) -> T { x }\nfn main() -> Int { identity(1) + identity(true) }\n",
			want: "2:34: expected Int, found Bool",
		},
		{
			name: "body does not match Int",
			src:  "fn f[T](x: T) -> Int { x }\n",
			want: "1:24: expected Int, found T",
		},
		{
			name: "addition on a type parameter",
			src:  "fn f[T](x: T) -> T { x + 1 }\n",
			want: "1:22: expected Int, found T",
		},
		{
			name: "return the wrong type parameter",
			src:  "fn f[T, U](x: T) -> U { x }\n",
			want: "1:25: expected U, found T",
		},
		{
			name: "compare type parameters",
			src:  "fn f[T](x: T, y: T) -> Bool { x == y }\n",
			want: "1:31: cannot compare values of type T",
		},
		{
			name: "match on a type parameter",
			src:  "fn f[T](x: T) -> Int { match x { _ => 0 } }\n",
			want: "1:30: cannot match on values of type T",
		},
		{
			name: "unsolved None",
			src:  "type Option[T] = | None | Some(T)\nfn main() -> Int { let x = None 0 }\n",
			want: "2:28: cannot infer type argument 'T' of 'None'; add a type annotation",
		},
		{
			name: "unsolved length",
			src: `type List[T] = | Nil | Cons(T, List[T])
fn length[T](xs: List[T]) -> Int { 0 }
fn main() -> Int { length(Nil) }
`,
			want: "3:20: cannot infer type argument 'T' of 'length'; add a type annotation",
		},
		{
			name: "unsolved none",
			src: `type Option[T] = | None | Some(T)
fn none[T]() -> Option[T] { None }
fn main() -> Int { match none() { _ => 0 } }
`,
			want: "3:26: cannot infer type argument 'T' of 'none'; add a type annotation",
		},
		{
			name: "match on an unsolved meta",
			src:  "fn nothing[T]() -> T { nothing() }\nfn main() -> Int { match nothing() { _ => 0 } }\n",
			want: "2:26: cannot infer the type of the matched value; add a type annotation",
		},
		{
			name: "call an unsolved meta",
			src:  "fn nothing[T]() -> T { nothing() }\nfn main() -> Int { nothing()(1) }\n",
			want: "2:20: cannot call non-function value of type ?T",
		},
		{
			name: "occurs check on a let binding",
			src:  "fn identity[T](x: T) -> T { x }\nfn main() -> Int { let f = identity f(f) }\n",
			want: "2:39: expected ?T, found ?T -> ?T",
		},
		{
			name: "option argument mismatch",
			src:  "type Option[T] = | None | Some(T)\nfn main() -> Int { let x: Option[Bool] = Some(1) 0 }\n",
			want: "2:42: expected Option[Bool], found Option[Int]",
		},
		{
			name: "if branches of options",
			src:  "type Option[T] = | None | Some(T)\nfn f(b: Bool) -> Int { let x = if b { Some(1) } else { Some(true) } 0 }\n",
			want: "2:56: if branches have different types: Option[Int] and Option[Bool]",
		},
		{
			name: "match arms of option and int",
			src:  "type Option[T] = | None | Some(T)\nfn f(b: Bool) -> Int { match b { true => None, false => 1 } }\n",
			want: "2:57: match arms have different types: Option[?T] and Int",
		},
		{
			name: "constructor of another generic type",
			src: `type Option[T] = | None | Some(T)
type List[T] = | Nil | Cons(T, List[T])
fn f(o: Option[Int]) -> Int { match o { Nil => 0, _ => 1 } }
`,
			want: "3:41: expected Option[Int], found List[?T]",
		},
		{
			name: "wrong number of pattern fields",
			src:  "type Option[T] = | None | Some(T)\nfn f(o: Option[Int]) -> Int { match o { Some(x, y) => 0, _ => 1 } }\n",
			want: "2:41: wrong number of fields in pattern 'Some': expected 1, found 2",
		},
		{
			name: "generic function argument arity",
			src:  "fn apply2[T](f: (T, T) -> T, x: T) -> T { f(x, x) }\nfn main() -> Int { apply2(fn(a: Int) -> Int { a }, 1) }\n",
			want: "2:27: expected (?T, ?T) -> ?T, found Int -> Int",
		},
		{
			name: "capture a type parameter value",
			src:  "fn f[T](x: T) -> Int { let g = fn(y: Int) -> T { x } 0 }\n",
			want: "1:50: cannot capture 'x' in anonymous function: closures are not supported in v0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireError(t, tt.src, tt.want)
		})
	}
}

func mustCheck(t *testing.T, src string) (*ast.File, *Info) {
	t.Helper()

	file, err := parser.ParseFile([]byte(src))
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	info, err := Check(file)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if info == nil {
		t.Fatal("Check() info = nil")
	}
	return file, info
}

func requireError(t *testing.T, src, want string) {
	t.Helper()

	file, err := parser.ParseFile([]byte(src))
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	info, err := Check(file)
	if info != nil {
		t.Fatal("Check() info != nil, want nil")
	}
	if err == nil {
		t.Fatalf("Check() error = nil, want %q", want)
	}
	if _, ok := err.(*diag.Error); !ok {
		t.Fatalf("error type = %T, want *diag.Error", err)
	}
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func funcByName(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()

	for _, fn := range file.Funcs {
		if fn.Name == name {
			return fn
		}
	}
	t.Fatalf("function %s not found", name)
	return nil
}

func typeByName(t *testing.T, file *ast.File, name string) *ast.TypeDecl {
	t.Helper()

	for _, td := range file.Types {
		if td.Name == name {
			return td
		}
	}
	t.Fatalf("type %s not found", name)
	return nil
}

func wantType(t *testing.T, info *Info, e ast.Expr, want string) {
	t.Helper()

	got, ok := info.Types[e]
	if !ok {
		t.Fatalf("missing type for %T", e)
	}
	if got.String() != want {
		t.Errorf("type = %s, want %s", got, want)
	}
}

func wantTypeArgs(t *testing.T, info *Info, id *ast.Ident, want ...string) {
	t.Helper()

	args, ok := info.TypeArgs[id]
	if !ok {
		t.Fatalf("missing type args for %s", id.Name)
	}
	if len(args) != len(want) {
		t.Fatalf("type args for %s = %d, want %d", id.Name, len(args), len(want))
	}
	for i, a := range args {
		if a.String() != want[i] {
			t.Errorf("type arg %d of %s = %s, want %s", i, id.Name, a, want[i])
		}
	}
}

func usesOf(info *Info, ids []*ast.Ident) []*Symbol {
	out := make([]*Symbol, len(ids))
	for i, id := range ids {
		out[i] = info.Uses[id]
	}
	return out
}

func findIdents(e ast.Expr, name string) []*ast.Ident {
	var out []*ast.Ident
	visitExprs(e, func(e ast.Expr) {
		id, ok := e.(*ast.Ident)
		if ok && id.Name == name {
			out = append(out, id)
		}
	})
	return out
}

func visitExprs(e ast.Expr, fn func(ast.Expr)) {
	if e == nil {
		return
	}
	fn(e)
	switch e := e.(type) {
	case *ast.IntLit, *ast.BoolLit, *ast.Ident:
	case *ast.UnaryExpr:
		visitExprs(e.X, fn)
	case *ast.BinaryExpr:
		visitExprs(e.X, fn)
		visitExprs(e.Y, fn)
	case *ast.CallExpr:
		visitExprs(e.Fn, fn)
		for _, arg := range e.Args {
			visitExprs(arg, fn)
		}
	case *ast.IfExpr:
		visitExprs(e.Cond, fn)
		visitExprs(e.Then, fn)
		visitExprs(e.Else, fn)
	case *ast.BlockExpr:
		for _, letStmt := range e.Lets {
			visitExprs(letStmt.Value, fn)
		}
		visitExprs(e.Result, fn)
	case *ast.FuncLit:
		visitExprs(e.Body, fn)
	case *ast.MatchExpr:
		visitExprs(e.Scrutinee, fn)
		for _, arm := range e.Arms {
			visitExprs(arm.Body, fn)
		}
	default:
		panic(fmt.Sprintf("unhandled expression %T", e))
	}
}

func assertComplete(t *testing.T, file *ast.File, info *Info) {
	t.Helper()

	if info.Types == nil || info.Uses == nil || info.Defs == nil ||
		info.Params == nil || info.Funcs == nil || info.FuncLits == nil ||
		info.Datas == nil || info.CtorPats == nil || info.PatVars == nil ||
		info.TypeArgs == nil {
		t.Fatal("Check() returned a nil map")
	}
	if len(info.Datas) != len(file.Types) {
		t.Errorf("len(Datas) = %d, want %d", len(info.Datas), len(file.Types))
	}
	for _, td := range file.Types {
		data := info.Datas[td]
		if data == nil || data.Name != td.Name || len(data.Ctors) != len(td.Ctors) {
			t.Errorf("data for %s = %+v", td.Name, data)
			continue
		}
		if len(data.Params) != len(td.TypeParams) {
			t.Errorf("type %s params = %d, want %d", td.Name, len(data.Params), len(td.TypeParams))
		}
		for i, p := range td.TypeParams {
			if i >= len(data.Params) || data.Params[i] == nil || data.Params[i].Name != p.Name {
				t.Errorf("type %s param %d name = %v, want %s", td.Name, i, data.Params, p.Name)
			}
		}
		for i, ctor := range data.Ctors {
			if ctor == nil || ctor.Index != i || ctor.Data != data || ctor.Name != td.Ctors[i].Name {
				t.Errorf("%s constructor %d = %+v", td.Name, i, ctor)
			}
			if ctor != nil && len(ctor.Fields) != len(td.Ctors[i].Fields) {
				t.Errorf("%s constructor %s fields = %d, want %d", td.Name, ctor.Name, len(ctor.Fields), len(td.Ctors[i].Fields))
			}
		}
	}

	var exprs, lets, params, lits, idents, typeArgs int
	var mainFound bool
	if len(info.Funcs) != len(file.Funcs) {
		t.Errorf("len(Funcs) = %d, want %d", len(info.Funcs), len(file.Funcs))
	}
	for _, fn := range file.Funcs {
		sym := info.Funcs[fn]
		if sym == nil {
			t.Errorf("missing symbol for %s", fn.Name)
			continue
		}
		if sym.Kind != SymFunc || sym.Name != fn.Name || sym.Pos != fn.NamePos || sym.Decl != fn {
			t.Errorf("symbol for %s = %+v", fn.Name, sym)
		}
		sig, ok := sym.Type.(*types.Func)
		if !ok {
			t.Errorf("%s type = %T, want *types.Func", fn.Name, sym.Type)
			continue
		}
		if fn.Name == "main" {
			mainFound = true
			if sig.String() != "() -> Int" {
				t.Errorf("main type = %s, want () -> Int", sig)
			}
		}
		params += checkParams(t, info, fn.Params, sig.Params)
		walkExpr(t, info, fn.Body, &exprs, &lets, &params, &lits, &idents, &typeArgs)
	}
	if !mainFound {
		t.Error("missing function main")
	}
	if exprs != len(info.Types) {
		t.Errorf("expressions = %d, len(Types) = %d", exprs, len(info.Types))
	}
	if lets != len(info.Defs) {
		t.Errorf("lets = %d, len(Defs) = %d", lets, len(info.Defs))
	}
	if params != len(info.Params) {
		t.Errorf("params = %d, len(Params) = %d", params, len(info.Params))
	}
	if lits != len(info.FuncLits) {
		t.Errorf("func lits = %d, len(FuncLits) = %d", lits, len(info.FuncLits))
	}
	if idents != len(info.Uses) {
		t.Errorf("idents = %d, len(Uses) = %d", idents, len(info.Uses))
	}
	if typeArgs != len(info.TypeArgs) {
		t.Errorf("type args = %d, len(TypeArgs) = %d", typeArgs, len(info.TypeArgs))
	}
	assertNoMeta(t, info)
}

func assertNoMeta(t *testing.T, info *Info) {
	t.Helper()

	for e, typ := range info.Types {
		if containsMeta(typ) {
			t.Errorf("type of %T contains a meta: %s", e, typ)
		}
	}
	for stmt, sym := range info.Defs {
		if sym != nil && containsMeta(sym.Type) {
			t.Errorf("def %s contains a meta: %s", stmt.Name, sym.Type)
		}
	}
	for pat, sym := range info.PatVars {
		if sym != nil && containsMeta(sym.Type) {
			t.Errorf("pattern variable %s contains a meta: %s", pat.Name, sym.Type)
		}
	}
	for param, sym := range info.Params {
		if sym != nil && containsMeta(sym.Type) {
			t.Errorf("param %s contains a meta: %s", param.Name, sym.Type)
		}
	}
	for lit, sig := range info.FuncLits {
		if containsMeta(sig) {
			t.Errorf("func lit at %s contains a meta: %s", lit.Pos, sig)
		}
	}
	for id, args := range info.TypeArgs {
		for i, arg := range args {
			if containsMeta(arg) {
				t.Errorf("type arg %d of %s contains a meta: %s", i, id.Name, arg)
			}
		}
	}
}

func containsMeta(t types.Type) bool {
	switch t := t.(type) {
	case *types.Meta:
		return true
	case *types.Func:
		for _, p := range t.Params {
			if containsMeta(p) {
				return true
			}
		}
		return containsMeta(t.Result)
	case *types.Named:
		for _, a := range t.Args {
			if containsMeta(a) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func checkParams(
	t *testing.T,
	info *Info,
	params []*ast.Param,
	want []types.Type,
) int {
	t.Helper()

	for i, p := range params {
		sym := info.Params[p]
		if sym == nil {
			t.Errorf("missing param %s", p.Name)
			continue
		}
		if sym.Kind != SymParam || sym.Name != p.Name || sym.Pos != p.Pos || sym.Decl != nil {
			t.Errorf("param %s symbol = %+v", p.Name, sym)
		}
		if i < len(want) && !types.Equal(sym.Type, want[i]) {
			t.Errorf("param %s type = %s, want %s", p.Name, sym.Type, want[i])
		}
	}
	return len(params)
}

func walkExpr(
	t *testing.T,
	info *Info,
	e ast.Expr,
	exprs *int,
	lets *int,
	params *int,
	lits *int,
	idents *int,
	typeArgs *int,
) {
	t.Helper()

	if e == nil {
		t.Fatal("nil expression")
	}
	if _, ok := info.Types[e]; !ok {
		t.Errorf("missing type for %T", e)
	}
	*exprs++

	switch e := e.(type) {
	case *ast.IntLit, *ast.BoolLit:
	case *ast.Ident:
		*idents++
		sym := info.Uses[e]
		if sym == nil {
			t.Errorf("missing use of %s", e.Name)
			return
		}
		if sym.Name != e.Name {
			t.Errorf("use of %s has name %s", e.Name, sym.Name)
		}
		if len(sym.TypeParams) > 0 {
			args, ok := info.TypeArgs[e]
			if !ok {
				t.Errorf("missing type args for %s", e.Name)
			} else if len(args) != len(sym.TypeParams) {
				t.Errorf("type args for %s = %d, want %d", e.Name, len(args), len(sym.TypeParams))
			} else if !types.Equal(info.Types[e], types.Subst(sym.Type, sym.TypeParams, args)) {
				t.Errorf("use of %s = %+v, expr type %s", e.Name, sym, info.Types[e])
			}
		} else if _, ok := info.TypeArgs[e]; ok {
			t.Errorf("unexpected type args for %s", e.Name)
		} else if !types.Equal(info.Types[e], sym.Type) {
			t.Errorf("use of %s = %+v, expr type %s", e.Name, sym, info.Types[e])
		}
		if _, ok := info.TypeArgs[e]; ok {
			*typeArgs++
		}
		if sym.Kind == SymCtor && (sym.Ctor == nil || sym.Decl != nil) {
			t.Errorf("constructor %s = %+v", e.Name, sym)
		}
	case *ast.UnaryExpr:
		walkExpr(t, info, e.X, exprs, lets, params, lits, idents, typeArgs)
	case *ast.BinaryExpr:
		walkExpr(t, info, e.X, exprs, lets, params, lits, idents, typeArgs)
		walkExpr(t, info, e.Y, exprs, lets, params, lits, idents, typeArgs)
	case *ast.CallExpr:
		walkExpr(t, info, e.Fn, exprs, lets, params, lits, idents, typeArgs)
		for _, arg := range e.Args {
			walkExpr(t, info, arg, exprs, lets, params, lits, idents, typeArgs)
		}
	case *ast.IfExpr:
		walkExpr(t, info, e.Cond, exprs, lets, params, lits, idents, typeArgs)
		walkExpr(t, info, e.Then, exprs, lets, params, lits, idents, typeArgs)
		walkExpr(t, info, e.Else, exprs, lets, params, lits, idents, typeArgs)
	case *ast.BlockExpr:
		for _, letStmt := range e.Lets {
			*lets++
			sym := info.Defs[letStmt]
			if sym == nil {
				t.Errorf("missing def %s", letStmt.Name)
			} else if sym.Kind != SymLocal || sym.Name != letStmt.Name || sym.Pos != letStmt.NamePos || sym.Decl != nil {
				t.Errorf("let %s symbol = %+v", letStmt.Name, sym)
			} else if vt, ok := info.Types[letStmt.Value]; ok && !types.Equal(sym.Type, vt) {
				t.Errorf("let %s type = %s, value type = %s", letStmt.Name, sym.Type, vt)
			}
			walkExpr(t, info, letStmt.Value, exprs, lets, params, lits, idents, typeArgs)
		}
		walkExpr(t, info, e.Result, exprs, lets, params, lits, idents, typeArgs)
		if rt, ok := info.Types[e.Result]; ok && !types.Equal(info.Types[e], rt) {
			t.Errorf("block type = %s, result type = %s", info.Types[e], rt)
		}
	case *ast.FuncLit:
		*lits++
		sig := info.FuncLits[e]
		if sig == nil {
			t.Error("missing func lit signature")
		} else if !types.Equal(info.Types[e], sig) {
			t.Errorf("func lit type = %s, signature = %s", info.Types[e], sig)
		}
		if sig != nil {
			*params += checkParams(t, info, e.Params, sig.Params)
		}
		walkExpr(t, info, e.Body, exprs, lets, params, lits, idents, typeArgs)
	case *ast.MatchExpr:
		walkExpr(t, info, e.Scrutinee, exprs, lets, params, lits, idents, typeArgs)
		for _, arm := range e.Arms {
			walkPattern(t, info, arm.Pattern, info.Types[e.Scrutinee])
			walkExpr(t, info, arm.Body, exprs, lets, params, lits, idents, typeArgs)
			if bt, ok := info.Types[arm.Body]; ok && !types.Equal(info.Types[e], bt) {
				t.Errorf("match type = %s, arm type = %s", info.Types[e], bt)
			}
		}
	default:
		t.Errorf("unhandled expression %T", e)
	}
}

func walkPattern(
	t *testing.T,
	info *Info,
	p ast.Pattern,
	want types.Type,
) {
	t.Helper()

	switch p := p.(type) {
	case *ast.WildcardPat, *ast.IntPat, *ast.BoolPat:
	case *ast.VarPat:
		sym := info.PatVars[p]
		if sym == nil {
			t.Errorf("missing pattern variable %s", p.Name)
			return
		}
		if sym.Kind != SymLocal || sym.Name != p.Name || sym.Pos != p.Pos || sym.Decl != nil || sym.Ctor != nil {
			t.Errorf("pattern variable %s = %+v", p.Name, sym)
		}
		if want != nil && !types.Equal(sym.Type, want) {
			t.Errorf("pattern variable %s type = %s, want %s", p.Name, sym.Type, want)
		}
	case *ast.CtorPat:
		ctor := info.CtorPats[p]
		if ctor == nil {
			t.Errorf("missing constructor pattern %s", p.Name)
			return
		}
		if ctor.Name != p.Name {
			t.Errorf("constructor pattern %s resolved to %s", p.Name, ctor.Name)
		}
		if want != nil {
			named, ok := want.(*types.Named)
			if !ok || named.Data != ctor.Data {
				t.Errorf("constructor pattern %s data = %s, want %s", p.Name, ctor.Data.Name, want)
			}
		}
		for i, arg := range p.Args {
			var field types.Type
			if named, ok := want.(*types.Named); ok && i < len(ctor.Fields) {
				field = types.Subst(ctor.Fields[i], ctor.Data.Params, named.Args)
			}
			walkPattern(t, info, arg, field)
		}
	default:
		t.Errorf("unhandled pattern %T", p)
	}
}
