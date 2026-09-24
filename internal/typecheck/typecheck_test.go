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
	default:
		panic(fmt.Sprintf("unhandled expression %T", e))
	}
}

func assertComplete(t *testing.T, file *ast.File, info *Info) {
	t.Helper()

	if info.Types == nil || info.Uses == nil || info.Defs == nil ||
		info.Params == nil || info.Funcs == nil || info.FuncLits == nil {
		t.Fatal("Check() returned a nil map")
	}

	var exprs, lets, params, lits, idents int
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
		walkExpr(t, info, fn.Body, &exprs, &lets, &params, &lits, &idents)
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
		if sym.Name != e.Name || !types.Equal(info.Types[e], sym.Type) {
			t.Errorf("use of %s = %+v, expr type %s", e.Name, sym, info.Types[e])
		}
	case *ast.UnaryExpr:
		walkExpr(t, info, e.X, exprs, lets, params, lits, idents)
	case *ast.BinaryExpr:
		walkExpr(t, info, e.X, exprs, lets, params, lits, idents)
		walkExpr(t, info, e.Y, exprs, lets, params, lits, idents)
	case *ast.CallExpr:
		walkExpr(t, info, e.Fn, exprs, lets, params, lits, idents)
		for _, arg := range e.Args {
			walkExpr(t, info, arg, exprs, lets, params, lits, idents)
		}
	case *ast.IfExpr:
		walkExpr(t, info, e.Cond, exprs, lets, params, lits, idents)
		walkExpr(t, info, e.Then, exprs, lets, params, lits, idents)
		walkExpr(t, info, e.Else, exprs, lets, params, lits, idents)
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
			walkExpr(t, info, letStmt.Value, exprs, lets, params, lits, idents)
		}
		walkExpr(t, info, e.Result, exprs, lets, params, lits, idents)
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
		walkExpr(t, info, e.Body, exprs, lets, params, lits, idents)
	default:
		t.Errorf("unhandled expression %T", e)
	}
}
