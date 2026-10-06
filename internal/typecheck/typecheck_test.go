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

func TestCheckClosureSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		check func(*testing.T, *ast.File, *Info)
	}{
		{
			name: "capture parameter",
			src: `fn f(x: Int) -> Int {
    let g = fn() -> Int { x }
    g()
}
fn main() -> Int { f(1) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				fn := funcByName(t, file, "f")
				lit := fn.Body.Lets[0].Value.(*ast.FuncLit)
				id := lit.Body.Result.(*ast.Ident)
				if info.Uses[id] != info.Params[fn.Params[0]] {
					t.Error("x does not use the outer parameter")
				}
			},
		},
		{
			name: "capture let",
			src: `fn main() -> Int {
    let x = 1
    let g = fn() -> Int { x }
    g()
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				lit := mainFn.Body.Lets[1].Value.(*ast.FuncLit)
				id := lit.Body.Result.(*ast.Ident)
				if info.Uses[id] != info.Defs[mainFn.Body.Lets[0]] {
					t.Error("x does not use the outer let")
				}
			},
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
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				lit := mainFn.Body.Lets[1].Value.(*ast.FuncLit)
				id := lit.Body.Result.(*ast.Ident)
				if info.Uses[id] != info.Defs[mainFn.Body.Lets[0]] {
					t.Error("id does not use the let")
				}
				if info.Uses[id] == info.Funcs[funcByName(t, file, "id")] {
					t.Error("id uses the top-level function")
				}
			},
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
			check: func(t *testing.T, file *ast.File, info *Info) {
				fn := funcByName(t, file, "f")
				m := fn.Body.Result.(*ast.MatchExpr)
				pat := m.Arms[0].Pattern.(*ast.VarPat)
				arm := m.Arms[0].Body.(*ast.BlockExpr)
				lit := arm.Lets[0].Value.(*ast.FuncLit)
				inner := lit.Body.Result.(*ast.MatchExpr)
				id := inner.Arms[0].Body.(*ast.Ident)
				if info.Uses[id] != info.PatVars[pat] {
					t.Error("x does not use the pattern variable")
				}
			},
		},
		{
			name: "capture a type parameter value",
			src: `fn f[T](x: T) -> Int {
    let g = fn(y: Int) -> T { x }
    0
}
fn main() -> Int { 0 }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				fn := funcByName(t, file, "f")
				lit := fn.Body.Lets[0].Value.(*ast.FuncLit)
				id := lit.Body.Result.(*ast.Ident)
				if info.Uses[id] != info.Params[fn.Params[0]] {
					t.Error("x does not use the outer parameter")
				}
			},
		},
		{
			name: "capture a generalized let",
			src:  "fn main() -> Int { let id = fn(x) { x } let g = fn(y) { id(y) } 0 }\n",
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				lit := mainFn.Body.Lets[1].Value.(*ast.FuncLit)
				call := lit.Body.Result.(*ast.CallExpr)
				id := call.Fn.(*ast.Ident)
				if info.Uses[id] != info.Defs[mainFn.Body.Lets[0]] {
					t.Error("id does not use the generalized let")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, info := mustCheck(t, tt.src)
			assertComplete(t, file, info)
			tt.check(t, file, info)
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
			src:  "fn f(x: Text) -> Int { 1 }",
			want: "1:9: unknown type 'Text'",
		},
		{
			name: "unknown result type",
			src:  "fn f() -> Text { 1 }",
			want: "1:11: unknown type 'Text'",
		},
		{
			name: "unknown let type",
			src:  "fn main() -> Int { let x: Text = 1 x }",
			want: "1:27: unknown type 'Text'",
		},
		{
			name: "unknown type inside function type",
			src:  "fn f(g: Int -> Text) -> Int { 1 }",
			want: "1:16: unknown type 'Text'",
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
			name: "missing main",
			src:  "fn foo() -> Int { 1 }",
			want: "1:1: missing function 'main'",
		},
		{
			name: "main with parameter",
			src:  "fn main(x: Int) -> Int { x }",
			want: "1:4: function 'main' must have type () -> Int or () -> IO[Unit], found Int -> Int",
		},
		{
			name: "main returns bool",
			src:  "fn main() -> Bool { true }",
			want: "1:4: function 'main' must have type () -> Int or () -> IO[Unit], found () -> Bool",
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
			src: `fn f(x: Text) -> Int { 1 }
fn g() -> Int { true }
`,
			want: "1:9: unknown type 'Text'",
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
			want: "4:5: non-exhaustive match: missing Cons(_, _)",
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
			want: "4:5: non-exhaustive match: missing Rect(_, _), Tri(_, _)",
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
			want: "3:5: non-exhaustive match: missing false",
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
			want: "3:5: non-exhaustive match: missing _",
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
			name:     "later use instantiates a generalized None",
			src:      genericPrelude + "fn main() -> Int { let o = None match o { Some(n) => n + 1, None => 0 } }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				mainFn := funcByName(t, file, "main")
				sym := info.Defs[mainFn.Body.Lets[0]]
				if got := sym.Type.String(); got != "Option[T]" {
					t.Errorf("o type = %s, want Option[T]", got)
				}
				if len(sym.TypeParams) != 1 || sym.TypeParams[0].Name != "T" {
					t.Errorf("o type params = %v, want [T]", sym.TypeParams)
				}
				match := mainFn.Body.Result.(*ast.MatchExpr)
				wantTypeArgs(t, info, match.Scrutinee.(*ast.Ident), "Int")
				pat := match.Arms[0].Pattern.(*ast.CtorPat)
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
			name:     "let generalizes a generic function",
			src:      genericPrelude + "fn main() -> Int { let f = identity f(5) }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				body := funcByName(t, file, "main").Body
				sym := info.Defs[body.Lets[0]]
				if got := sym.Type.String(); got != "T -> T" {
					t.Errorf("f type = %s, want T -> T", got)
				}
				if len(sym.TypeParams) != 1 {
					t.Errorf("f type params = %d, want 1", len(sym.TypeParams))
				}
				wantTypeArgs(t, info, body.Result.(*ast.CallExpr).Fn.(*ast.Ident), "Int")
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
			src:  "type Option[T] = | None | Some(T)\nfn identity[T](x: T) -> T { x }\nfn main() -> Int { let x = identity(None) 0 }\n",
			want: "3:37: cannot infer type argument 'T' of 'None'; add a type annotation",
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
			name: "occurs check on a let binding",
			src:  "fn identity[T](x: T) -> T { x }\nfn main() -> Int { let f = identity(identity) f(f) }\n",
			want: "2:49: expected ?T, found ?T -> ?T",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireError(t, tt.src, tt.want)
		})
	}
}

func TestCheckInferSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		wantMain string
		check    func(*testing.T, *ast.File, *Info)
	}{
		{
			name:     "let generalizes a generic function at two types",
			src:      genericPrelude + "fn main() -> Int { let f = identity if f(true) { f(1) } else { 0 } }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				body := funcByName(t, file, "main").Body
				wantScheme(t, info, body.Lets[0], "T -> T", "T")
				sym := info.Defs[body.Lets[0]]
				ft := sym.Type.(*types.Func)
				if ft.Params[0] != sym.TypeParams[0] {
					t.Errorf("parameter type = %s, want the let's type parameter", ft.Params[0])
				}
				ids := findIdents(body, "f")
				if len(ids) != 2 {
					t.Fatalf("f idents = %d, want 2", len(ids))
				}
				wantTypeArgs(t, info, ids[0], "Bool")
				wantTypeArgs(t, info, ids[1], "Int")
			},
		},
		{
			name:     "let generalizes an anonymous function",
			src:      "fn main() -> Int { let id = fn(x) { x } if id(true) { id(1) } else { 0 } }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				body := funcByName(t, file, "main").Body
				wantScheme(t, info, body.Lets[0], "t1 -> t1", "t1")
				lit := body.Lets[0].Value.(*ast.FuncLit)
				if got := info.FuncLits[lit].String(); got != "t1 -> t1" {
					t.Errorf("func lit type = %s, want t1 -> t1", got)
				}
				if got := info.Params[lit.Params[0]].Type.String(); got != "t1" {
					t.Errorf("x type = %s, want t1", got)
				}
				ids := findIdents(body, "id")
				if len(ids) != 2 {
					t.Fatalf("id idents = %d, want 2", len(ids))
				}
				wantTypeArgs(t, info, ids[0], "Bool")
				wantTypeArgs(t, info, ids[1], "Int")
			},
		},
		{
			name:     "let generalizes None",
			src:      genericPrelude + "fn main() -> Int { let n = None let a: Option[Int] = n let b: Option[Bool] = n 0 }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				lets := funcByName(t, file, "main").Body.Lets
				wantScheme(t, info, lets[0], "Option[T]", "T")
				wantScheme(t, info, lets[1], "Option[Int]")
				wantScheme(t, info, lets[2], "Option[Bool]")
				wantTypeArgs(t, info, lets[1].Value.(*ast.Ident), "Int")
				wantTypeArgs(t, info, lets[2].Value.(*ast.Ident), "Bool")
			},
		},
		{
			name:     "unused generalized let",
			src:      "fn main() -> Int { let f = fn(x) { 0 } 1 }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantScheme(t, info, funcByName(t, file, "main").Body.Lets[0], "t1 -> Int", "t1")
			},
		},
		{
			name:     "parameter inferred from its use",
			src:      "fn main() -> Int { let inc = fn(x) { x + 1 } inc(41) }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				wantScheme(t, info, letStmt, "Int -> Int")
				lit := letStmt.Value.(*ast.FuncLit)
				if got := info.FuncLits[lit].String(); got != "Int -> Int" {
					t.Errorf("func lit type = %s, want Int -> Int", got)
				}
			},
		},
		{
			name: "parameter inferred from the expected function type",
			src: genericPrelude + `fn map[T, U](xs: List[T], f: T -> U) -> List[U] { match xs { Nil => Nil, Cons(x, r) => Cons(f(x), map(r, f)) } }
fn main() -> Int { let ys = map(Cons(1, Nil), fn(x) { x > 0 }) 0 }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				lit := letStmt.Value.(*ast.CallExpr).Args[1].(*ast.FuncLit)
				if got := info.FuncLits[lit].String(); got != "Int -> Bool" {
					t.Errorf("func lit type = %s, want Int -> Bool", got)
				}
				wantScheme(t, info, letStmt, "List[Bool]")
			},
		},
		{
			name:     "partially annotated anonymous function",
			src:      "fn main() -> Int { let add = fn(a: Int, b) -> Int { a + b } add(1, 2) }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantScheme(t, info, funcByName(t, file, "main").Body.Lets[0], "(Int, Int) -> Int")
			},
		},
		{
			name: "match infers the scrutinee from a constructor pattern",
			src: genericPrelude + `fn main() -> Int { let get = fn(o) { match o { Some(n) => n, None => 0 } } get(Some(5)) }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				wantScheme(t, info, letStmt, "Option[Int] -> Int")
				m := letStmt.Value.(*ast.FuncLit).Body.Result.(*ast.MatchExpr)
				wantType(t, info, m.Scrutinee, "Option[Int]")
			},
		},
		{
			name:     "match infers the scrutinee from a literal pattern",
			src:      "fn main() -> Int { let isZero = fn(n) { match n { 0 => true, _ => false } } if isZero(0) { 1 } else { 2 } }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantScheme(t, info, funcByName(t, file, "main").Body.Lets[0], "Int -> Bool")
			},
		},
		{
			name:     "call infers a function parameter",
			src:      "fn main() -> Int { let applyOne = fn(f) { f(1) } applyOne(fn(x) { x * 2 }) }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				body := funcByName(t, file, "main").Body
				wantScheme(t, info, body.Lets[0], "(Int -> t2) -> t2", "t2")
				call := body.Result.(*ast.CallExpr)
				wantTypeArgs(t, info, call.Fn.(*ast.Ident), "Int")
				lit := call.Args[0].(*ast.FuncLit)
				if got := info.FuncLits[lit].String(); got != "Int -> Int" {
					t.Errorf("func lit type = %s, want Int -> Int", got)
				}
			},
		},
		{
			name: "call an unsolved meta makes it a function",
			src: `fn nothing[T]() -> T { nothing() }
fn main() -> Int { nothing()(1) }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				ids := findIdents(funcByName(t, file, "main").Body, "nothing")
				if len(ids) != 1 {
					t.Fatalf("nothing idents = %d, want 1", len(ids))
				}
				wantTypeArgs(t, info, ids[0], "Int -> Int")
			},
		},
		{
			name:     "comparison infers the left operand from the right",
			src:      "fn main() -> Int { let eqOne = fn(x) { x == 1 } if eqOne(1) { 1 } else { 0 } }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantScheme(t, info, funcByName(t, file, "main").Body.Lets[0], "Int -> Bool")
			},
		},
		{
			name: "generalized let inside a generic function",
			src: `fn twice[T](x: T) -> T { let id = fn(y) { y } id(id(x)) }
fn main() -> Int { twice(1) }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				twice := funcByName(t, file, "twice")
				wantScheme(t, info, twice.Body.Lets[0], "t1 -> t1", "t1")
				ids := findIdents(twice.Body, "id")
				if len(ids) != 2 {
					t.Fatalf("id idents = %d, want 2", len(ids))
				}
				tp := info.Funcs[twice].TypeParams[0]
				for i, id := range ids {
					args := info.TypeArgs[id]
					if len(args) != 1 {
						t.Errorf("id %d type args = %d, want 1", i, len(args))
						continue
					}
					got, ok := args[0].(*types.TypeParam)
					if !ok || got != tp {
						t.Errorf("id %d type arg = %v, want twice's type parameter", i, args[0])
					}
				}
			},
		},
		{
			name:     "generalized let of a generalized let",
			src:      genericPrelude + "fn main() -> Int { let f = identity let g = f if g(true) { g(2) } else { 0 } }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				lets := funcByName(t, file, "main").Body.Lets
				wantScheme(t, info, lets[1], "T -> T", "T")
				arg := info.TypeArgs[lets[1].Value.(*ast.Ident)]
				if len(arg) != 1 || arg[0] != info.Defs[lets[1]].TypeParams[0] {
					t.Errorf("f type arg = %v, want g's type parameter", arg)
				}
			},
		},
		{
			name:     "let of a call is not generalized",
			src:      genericPrelude + "fn main() -> Int { let g = identity(fn(x) { x }) g(1) }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantScheme(t, info, funcByName(t, file, "main").Body.Lets[0], "Int -> Int")
			},
		},
		{
			name: "metas in the environment are not generalized",
			src: genericPrelude + `fn main() -> Int { let o = identity(None) let p = o match p { Some(n) => n, None => 0 } }
`,
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				lets := funcByName(t, file, "main").Body.Lets
				wantScheme(t, info, lets[0], "Option[Int]")
				wantScheme(t, info, lets[1], "Option[Int]")
			},
		},
		{
			name:     "parameter of an enclosing anonymous function is in the environment",
			src:      "fn main() -> Int { let k = fn(x) { let y = x y } k(1) }\n",
			wantMain: "Int",
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				wantScheme(t, info, letStmt, "t1 -> t1", "t1")
				wantScheme(t, info, letStmt.Value.(*ast.FuncLit).Body.Lets[0], "t1")
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

func TestCheckInferError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "generalized function called with a wrong arity",
			src:  "fn main() -> Int { let id = fn(x) { x } id(1, 2) }\n",
			want: "1:43: wrong number of arguments: expected 1, found 2",
		},
		{
			name: "monomorphic let used at two types",
			src:  "fn identity[T](x: T) -> T { x }\nfn main() -> Int { let g = identity(fn(x) { x }) if g(true) { g(1) } else { 0 } }\n",
			want: "2:65: expected Bool, found Int",
		},
		{
			name: "unsolved parameter",
			src:  "fn main() -> Int { let g = if true { fn(x) { 0 } } else { fn(y) { 1 } } 0 }\n",
			want: "1:41: cannot infer the type of parameter 'x'; add a type annotation",
		},
		{
			name: "unsolved call result",
			src:  "fn main() -> Int { let g = if true { fn(f) { f(1) } } else { fn(h) { h(2) } } 0 }\n",
			want: "1:47: cannot infer the result type of this call; add a type annotation",
		},
		{
			name: "occurs check when calling a parameter",
			src:  "fn main() -> Int { let g = fn(f) { f(f) } 0 }\n",
			want: "1:36: expected ?t1 -> ?t2, found ?t1",
		},
		{
			name: "compare two unannotated parameters",
			src:  "fn main() -> Int { let eq = fn(x, y) { x == y } 0 }\n",
			want: "1:40: cannot compare values of type ?t2",
		},
		{
			name: "match on an unannotated parameter with only a wildcard",
			src:  "fn main() -> Int { let f = fn(x) { match x { _ => 0 } } 0 }\n",
			want: "1:42: cannot infer the type of the matched value; add a type annotation",
		},
		{
			name: "match infers from an unknown constructor",
			src:  "fn main() -> Int { let f = fn(x) { match x { Foo => 0, _ => 1 } } 0 }\n",
			want: "1:46: unknown constructor 'Foo'",
		},
		{
			name: "pattern of another data type after inference",
			src:  "type Option[T] = | None | Some(T)\ntype List[T] = | Nil | Cons(T, List[T])\nfn main() -> Int { let f = fn(o) { match o { Some(n) => n, Nil => 0 } } 0 }\n",
			want: "3:60: expected Option[?T], found List[?T]",
		},
		{
			name: "literal pattern after a constructor pattern",
			src:  "type Option[T] = | None | Some(T)\nfn main() -> Int { let f = fn(o) { match o { Some(n) => n, 0 => 0 } } 0 }\n",
			want: "2:60: expected Option[?T], found Int",
		},
		{
			name: "unannotated parameter used as Int and Bool",
			src:  "fn main() -> Int { let f = fn(x) { if x { x + 1 } else { 0 } } 0 }\n",
			want: "1:43: expected Int, found Bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireError(t, tt.src, tt.want)
		})
	}
}

func TestCheckTopLevelInferSuccess(t *testing.T) {
	t.Parallel()

	list := `type List[T] =
    | Nil
    | Cons(T, List[T])
`

	tests := []struct {
		name  string
		src   string
		check func(*testing.T, *ast.File, *Info)
	}{
		{
			name: "identity",
			src: `fn identity(x) { x }
fn main() { identity(1) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "identity"), "t1 -> t1", "t1")
			},
		},
		{
			name: "identity at two types",
			src: `fn identity(x) { x }
fn main() { if identity(true) { identity(1) } else { 0 } }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "identity"), "t1 -> t1", "t1")
				iff := funcByName(t, file, "main").Body.Result.(*ast.IfExpr)
				wantTypeArgs(t, info, iff.Cond.(*ast.CallExpr).Fn.(*ast.Ident), "Bool")
				wantTypeArgs(t, info, iff.Then.Result.(*ast.CallExpr).Fn.(*ast.Ident), "Int")
			},
		},
		{
			name: "parameter inferred as Int",
			src: `fn inc(x) { x + 1 }
fn main() { inc(1) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "inc"), "Int -> Int")
			},
		},
		{
			name: "partial annotation",
			src: `fn f(x: Int, y) { y }
fn main() { if f(1, true) { 1 } else { 0 } }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "f"), "(Int, t1) -> t1", "t1")
			},
		},
		{
			name: "calls an annotated function",
			src: `fn add(a: Int, b: Int) -> Int { a + b }
fn f(x) { add(x, 1) }
fn main() { f(1) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "f"), "Int -> Int")
				wantFuncScheme(t, info, funcByName(t, file, "add"), "(Int, Int) -> Int")
			},
		},
		{
			name: "annotated function calls an inferred function",
			src: `fn identity(x) { x }
fn main() -> Int { identity(1) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "identity"), "t1 -> t1", "t1")
			},
		},
		{
			name: "reference before the declaration",
			src: `fn main() { identity(1) }
fn identity(x) { x }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "identity"), "t1 -> t1", "t1")
			},
		},
		{
			name: "self recursion",
			src: list + `fn length(xs) { match xs { Nil => 0, Cons(_, r) => 1 + length(r) } }
fn main() { length(Cons(1, Nil)) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				length := funcByName(t, file, "length")
				wantFuncScheme(t, info, length, "List[T] -> Int", "T")
				ids := findIdents(length.Body, "length")
				if len(ids) != 1 {
					t.Fatalf("length references = %d, want 1", len(ids))
				}
				args := info.TypeArgs[ids[0]]
				tps := info.Funcs[length].TypeParams
				if len(args) != len(tps) {
					t.Fatalf("type args = %d, want %d", len(args), len(tps))
				}
				for i := range tps {
					if args[i] != tps[i] {
						t.Errorf("type arg %d = %v, want length's type parameter", i, args[i])
					}
				}
			},
		},
		{
			name: "mutual recursion",
			src: `fn isEven(n) { if n == 0 { true } else { isOdd(n - 1) } }
fn isOdd(n) { if n == 0 { false } else { isEven(n - 1) } }
fn main() { if isEven(4) { 1 } else { 0 } }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "isEven"), "Int -> Bool")
				wantFuncScheme(t, info, funcByName(t, file, "isOdd"), "Int -> Bool")
			},
		},
		{
			name: "mutual recursion shares type parameters",
			src: `fn f(x) { g(x) }
fn g(y) { f(y) }
fn main() { f(1) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				fs := info.Funcs[funcByName(t, file, "f")]
				gs := info.Funcs[funcByName(t, file, "g")]
				if len(fs.TypeParams) == 0 || len(fs.TypeParams) != len(gs.TypeParams) {
					t.Fatalf("type params f = %d, g = %d", len(fs.TypeParams), len(gs.TypeParams))
				}
				for i := range fs.TypeParams {
					if fs.TypeParams[i] != gs.TypeParams[i] {
						t.Errorf("type param %d is not shared", i)
					}
				}
			},
		},
		{
			name: "higher-order function",
			src: `fn twice(f, x) { f(f(x)) }
fn main() { twice(fn(n) { n + 1 }, 0) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "twice"), "(t2 -> t2, t2) -> t2", "t2")
			},
		},
		{
			name: "inferred function outside the component is polymorphic",
			src: `fn identity(x) { x }
fn pair(x) { identity(x) }
fn main() { if pair(true) { identity(1) } else { pair(0) } }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "identity"), "t1 -> t1", "t1")
				wantFuncScheme(t, info, funcByName(t, file, "pair"), "t1 -> t1", "t1")
				idTP := info.Funcs[funcByName(t, file, "identity")].TypeParams[0]
				pairTP := info.Funcs[funcByName(t, file, "pair")].TypeParams[0]
				if idTP == pairTP {
					t.Error("identity and pair share a type parameter")
				}
			},
		},
		{
			name: "inferred main",
			src:  "fn main() { 42 }\n",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "main"), "() -> Int")
			},
		},
		{
			name: "let does not generalize a component type",
			src: `fn f(x) { let g = f g(x) }
fn main() { f(1) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				sym := info.Defs[funcByName(t, file, "f").Body.Lets[0]]
				if len(sym.TypeParams) != 0 {
					t.Errorf("g type params = %d, want 0", len(sym.TypeParams))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, info := mustCheck(t, tt.src)
			assertComplete(t, file, info)
			if tt.check != nil {
				tt.check(t, file, info)
			}
		})
	}
}

func TestCheckTopLevelInferError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "type parameters require annotations",
			src:  "fn f[T](x) -> T { x }\n",
			want: "1:6: function 'f' declares type parameters, so every parameter and its result need a type annotation",
		},
		{
			name: "polymorphic recursion",
			src: `type List[T] = | Nil | Cons(T, List[T])
fn f(x) { if true { x } else { f(Cons(x, Nil)) } }
fn main() { 0 }
`,
			want: "2:34: expected ?T, found List[?T]",
		},
		{
			name: "unsolved meta used only in the body",
			src: `type List[T] = | Nil | Cons(T, List[T])
fn length[T](xs: List[T]) -> Int { match xs { Nil => 0, Cons(_, r) => 1 + length(r) } }
fn f(x) { let n = length(Nil) x }
fn main() { f(1) }
`,
			want: "3:19: cannot infer type argument 'T' of 'length'; add a type annotation",
		},
		{
			name: "other function's type parameter",
			src: `type Option[T] = | None | Some(T)
fn f(x) { g(x, None) }
fn g(p, q) { f(p) }
fn main() { f(1) }
`,
			want: "2:16: cannot infer type argument 'T' of 'None'; add a type annotation",
		},
		{
			name: "main with a parameter",
			src:  "fn main(x) { 1 }\n",
			want: "1:4: function 'main' must have type () -> Int or () -> IO[Unit], found ?t1 -> ?t2",
		},
		{
			name: "main returning Bool",
			src:  "fn main() { true }\n",
			want: "1:4: function 'main' must have type () -> Int or () -> IO[Unit], found () -> Bool",
		},
		{
			name: "argument type mismatch",
			src: `fn inc(x) { x + 1 }
fn main() { inc(true) }
`,
			want: "2:17: expected Int, found Bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireError(t, tt.src, tt.want)
		})
	}
}

func TestCheckNestedPatternSuccess(t *testing.T) {
	t.Parallel()

	listOpt := `type Option[T] =
    | None
    | Some(T)

type List[T] =
    | Nil
    | Cons(T, List[T])
`

	tests := []struct {
		name  string
		src   string
		check func(*testing.T, *ast.File, *Info)
	}{
		{
			name: "nested constructors",
			src: listOpt + `fn main() -> Int {
    match Cons(1, Cons(2, Nil)) {
        Cons(_, Cons(_, Nil)) => 1,
        _ => 0,
    }
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				list := info.Datas[typeByName(t, file, "List")]
				m := funcByName(t, file, "main").Body.Result.(*ast.MatchExpr)
				outer := m.Arms[0].Pattern.(*ast.CtorPat)
				inner := outer.Args[1].(*ast.CtorPat)
				nilPat := inner.Args[1].(*ast.CtorPat)
				if info.CtorPats[outer] != list.Ctors[1] || info.CtorPats[outer].Name != "Cons" {
					t.Errorf("outer Cons = %+v", info.CtorPats[outer])
				}
				if info.CtorPats[inner] != list.Ctors[1] || info.CtorPats[inner].Name != "Cons" {
					t.Errorf("inner Cons = %+v", info.CtorPats[inner])
				}
				if info.CtorPats[nilPat] != list.Ctors[0] || info.CtorPats[nilPat].Name != "Nil" {
					t.Errorf("Nil = %+v", info.CtorPats[nilPat])
				}
			},
		},
		{
			name: "nested literals",
			src: listOpt + `fn main() -> Int {
    match Some(true) {
        Some(true) => 1,
        Some(false) => 0,
        None => 2,
    }
}
`,
		},
		{
			name: "nested variable types",
			src: listOpt + `fn main() -> Int {
    match Cons(1, Cons(2, Nil)) {
        Cons(x, Cons(y, _)) => x + y,
        _ => 0,
    }
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				m := funcByName(t, file, "main").Body.Result.(*ast.MatchExpr)
				outer := m.Arms[0].Pattern.(*ast.CtorPat)
				x := outer.Args[0].(*ast.VarPat)
				inner := outer.Args[1].(*ast.CtorPat)
				y := inner.Args[0].(*ast.VarPat)
				if info.PatVars[x] == nil || info.PatVars[x].Type.String() != "Int" {
					t.Errorf("x = %+v", info.PatVars[x])
				}
				if info.PatVars[y] == nil || info.PatVars[y].Type.String() != "Int" {
					t.Errorf("y = %+v", info.PatVars[y])
				}
			},
		},
		{
			name: "infer scrutinee from a nested pattern",
			src: listOpt + `fn f(p) {
    match p {
        Some(Cons(x, _)) => x,
        _ => 0,
    }
}
fn main() { f(Some(Cons(1, Nil))) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				f := funcByName(t, file, "f")
				wantFuncScheme(t, info, f, "Option[List[Int]] -> Int")
				m := f.Body.Result.(*ast.MatchExpr)
				some := m.Arms[0].Pattern.(*ast.CtorPat)
				cons := some.Args[0].(*ast.CtorPat)
				x := cons.Args[0].(*ast.VarPat)
				if info.CtorPats[some] == nil || info.CtorPats[some].Name != "Some" {
					t.Errorf("Some = %+v", info.CtorPats[some])
				}
				if info.CtorPats[cons] == nil || info.CtorPats[cons].Name != "Cons" {
					t.Errorf("Cons = %+v", info.CtorPats[cons])
				}
				if info.PatVars[x] == nil || info.PatVars[x].Type.String() != "Int" {
					t.Errorf("x = %+v", info.PatVars[x])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, info := mustCheck(t, tt.src)
			assertComplete(t, file, info)
			if tt.check != nil {
				tt.check(t, file, info)
			}
		})
	}
}

func TestCheckNestedPatternError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "duplicate nested variable",
			src: `type List[T] = Nil | Cons(T, List[T])
fn main() -> Int {
    match Cons(1, Cons(2, Nil)) {
        Cons(x, Cons(x, _)) => x,
        _ => 0,
    }
}
`,
			want: "4:22: duplicate variable 'x' in pattern",
		},
		{
			name: "nested type mismatch",
			src: `type List[T] = Nil | Cons(T, List[T])
fn main() -> Int {
    match Cons(1, Nil) {
        Cons(true, _) => 0,
        _ => 1,
    }
}
`,
			want: "4:14: expected Int, found Bool",
		},
		{
			name: "nested unknown constructor",
			src: `type List[T] = Nil | Cons(T, List[T])
fn main() -> Int {
    match Cons(1, Nil) {
        Cons(Foo, _) => 0,
        _ => 1,
    }
}
`,
			want: "4:14: unknown constructor 'Foo'",
		},
		{
			name: "nested wrong field count",
			src: `type List[T] = Nil | Cons(T, List[T])
fn main() -> Int {
    match Cons(1, Nil) {
        Cons(x, Cons(y)) => x,
        _ => 0,
    }
}
`,
			want: "4:17: wrong number of fields in pattern 'Cons': expected 2, found 1",
		},
		{
			name: "unreachable nested constructor",
			src: `type List[T] = Nil | Cons(T, List[T])
fn main() -> Int {
    match Cons(1, Nil) {
        Cons(_, _) => 0,
        Cons(_, Nil) => 1,
        _ => 2,
    }
}
`,
			want: "5:9: unreachable match arm",
		},
		{
			name: "non-exhaustive nested constructor",
			src: `type List[T] = Nil | Cons(T, List[T])
fn main() -> Int {
    match Cons(1, Nil) {
        Nil => 0,
        Cons(_, Nil) => 1,
    }
}
`,
			want: "3:5: non-exhaustive match: missing Cons(_, Cons(_, _))",
		},
		{
			name: "literal against a type parameter",
			src: `type Option[T] = None | Some(T)
fn f[T](o: Option[T]) -> Int {
    match o {
        Some(1) => 1,
        _ => 0,
    }
}
fn main() -> Int { 0 }
`,
			want: "4:14: expected T, found Int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireError(t, tt.src, tt.want)
		})
	}
}

func TestCheckStringSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		check func(*testing.T, *ast.File, *Info)
	}{
		{
			name: "literal type",
			src: `fn main() -> Int {
    let s = "hi"
    0
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				if got := info.Defs[letStmt].Type.String(); got != "String" {
					t.Errorf("let s type = %s, want String", got)
				}
				wantType(t, info, letStmt.Value, "String")
			},
		},
		{
			name: "concat",
			src: `fn main() -> Int {
    let s = "a" ++ "b" ++ "c"
    0
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				wantType(t, info, letStmt.Value, "String")
			},
		},
		{
			name: "equality",
			src: `fn main() -> Int {
    if "a" == "b" { 1 } else if "a" != "b" { 2 } else { 0 }
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				ifExpr := funcByName(t, file, "main").Body.Result.(*ast.IfExpr)
				wantType(t, info, ifExpr.Cond, "Bool")
				wantType(t, info, ifExpr.Else.(*ast.IfExpr).Cond, "Bool")
			},
		},
		{
			name: "equality infers string",
			src: `fn eq(s) { s == "a" }
fn ne(s) { s != "b" }
fn main() -> Int { 0 }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "eq"), "String -> Bool")
				wantFuncScheme(t, info, funcByName(t, file, "ne"), "String -> Bool")
			},
		},
		{
			name: "built-in calls",
			src: `fn main() -> Int {
    let n = stringLength("abc")
    let b = stringByteAt("abc", 0)
    let s = stringSlice("abc", 0, 1)
    let c = stringFromByte(65)
    let d = stringCompare("a", "b")
    let t = intToString(1)
    n + b
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				lets := funcByName(t, file, "main").Body.Lets
				want := []struct {
					typ string
					b   Builtin
				}{
					{"Int", BuiltinStringLength},
					{"Int", BuiltinStringByteAt},
					{"String", BuiltinStringSlice},
					{"String", BuiltinStringFromByte},
					{"Int", BuiltinStringCompare},
					{"String", BuiltinIntToString},
				}
				if len(lets) != len(want) {
					t.Fatalf("lets = %d, want %d", len(lets), len(want))
				}
				for i, w := range want {
					wantType(t, info, lets[i].Value, w.typ)
					call := lets[i].Value.(*ast.CallExpr)
					sym := info.Uses[call.Fn.(*ast.Ident)]
					if sym == nil || sym.Kind != SymBuiltin || sym.Builtin != w.b {
						t.Errorf("call %d symbol = %+v, want built-in %d", i, sym, w.b)
					}
				}
			},
		},
		{
			name: "built-in as a value",
			src: `fn apply(f: String -> Int, s: String) -> Int { f(s) }
fn main() -> Int { apply(stringLength, "abc") }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				call := funcByName(t, file, "main").Body.Result.(*ast.CallExpr)
				id := call.Args[0].(*ast.Ident)
				sym := info.Uses[id]
				if sym == nil || sym.Kind != SymBuiltin || sym.Builtin != BuiltinStringLength {
					t.Fatalf("stringLength symbol = %+v", sym)
				}
				if sym.Type.String() != "String -> Int" {
					t.Errorf("stringLength type = %s, want String -> Int", sym.Type)
				}
				wantType(t, info, id, "String -> Int")
			},
		},
		{
			name: "built-in call infers the argument",
			src: `fn lenOf(s) { stringLength(s) }
fn main() -> Int { 0 }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "lenOf"), "String -> Int")
			},
		},
		{
			name: "local shadows a built-in",
			src: `fn main() -> Int {
    let stringLength = 1
    stringLength
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				body := funcByName(t, file, "main").Body
				sym := info.Uses[body.Result.(*ast.Ident)]
				if sym == nil || sym.Kind != SymLocal || sym != info.Defs[body.Lets[0]] {
					t.Fatalf("use = %+v, want the let", sym)
				}
				if sym.Type.String() != "Int" {
					t.Errorf("type = %s, want Int", sym.Type)
				}
			},
		},
		{
			name: "parameter shadows a built-in",
			src: `fn f(stringLength: Int) -> Int { stringLength }
fn main() -> Int { f(1) }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				fn := funcByName(t, file, "f")
				sym := info.Uses[fn.Body.Result.(*ast.Ident)]
				if sym == nil || sym.Kind != SymParam || sym != info.Params[fn.Params[0]] {
					t.Fatalf("use = %+v, want the parameter", sym)
				}
			},
		},
		{
			name: "string pattern",
			src: `fn main() -> Int {
    match "fn" {
        "fn" => 1,
        _ => 0,
    }
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				m := funcByName(t, file, "main").Body.Result.(*ast.MatchExpr)
				wantType(t, info, m.Scrutinee, "String")
			},
		},
		{
			name: "string pattern infers the scrutinee",
			src: `fn g(s) {
    match s {
        "a" => 1,
        _ => 0,
    }
}
fn main() -> Int { 0 }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "g"), "String -> Int")
			},
		},
		{
			name: "nested string pattern",
			src: `type Box = Box(String)
fn main() -> Int {
    match Box("hi") {
        Box("hi") => 1,
        _ => 0,
    }
}
`,
		},
		{
			name: "concat infers the argument",
			src: `fn f(s) { s ++ "x" }
fn main() -> Int { 0 }
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "f"), "String -> String")
			},
		},
		{
			name: "annotated let",
			src: `fn main() -> Int {
    let s: String = "x"
    0
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				letStmt := funcByName(t, file, "main").Body.Lets[0]
				if got := info.Defs[letStmt].Type.String(); got != "String" {
					t.Errorf("let s type = %s, want String", got)
				}
				wantType(t, info, letStmt.Value, "String")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file, info := mustCheck(t, tt.src)
			assertComplete(t, file, info)
			if tt.check != nil {
				tt.check(t, file, info)
			}
		})
	}
}

func TestCheckStringError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "concat string and int",
			src:  `fn main() -> Int { "a" ++ 1 }`,
			want: "1:27: expected String, found Int",
		},
		{
			name: "concat ints",
			src:  `fn main() -> Int { 1 ++ 2 }`,
			want: "1:20: expected String, found Int",
		},
		{
			name: "compare string and int",
			src:  `fn main() -> Int { "a" == 1 }`,
			want: "1:27: expected String, found Int",
		},
		{
			name: "add strings",
			src:  `fn main() -> Int { "a" + "b" }`,
			want: "1:20: expected Int, found String",
		},
		{
			name: "stringLength wrong type",
			src:  `fn main() -> Int { stringLength(1) }`,
			want: "1:33: expected String, found Int",
		},
		{
			name: "stringLength wrong arity",
			src:  `fn main() -> Int { stringLength() }`,
			want: "1:32: wrong number of arguments: expected 1, found 0",
		},
		{
			name: "stringByteAt wrong arity",
			src:  `fn main() -> Int { stringByteAt("a") }`,
			want: "1:32: wrong number of arguments: expected 2, found 1",
		},
		{
			name: "stringSlice wrong arity",
			src:  `fn main() -> Int { stringSlice("a", 0) }`,
			want: "1:31: wrong number of arguments: expected 3, found 2",
		},
		{
			name: "stringFromByte wrong type",
			src:  `fn main() -> Int { stringFromByte("a") }`,
			want: "1:35: expected Int, found String",
		},
		{
			name: "function conflicts with built-in",
			src: `fn stringLength(s: String) -> Int { 0 }
fn main() -> Int { 1 }
`,
			want: "1:4: function 'stringLength' conflicts with built-in function 'stringLength'",
		},
		{
			name: "redefine String",
			src:  "type String = S\n",
			want: "1:6: cannot redefine built-in type 'String'",
		},
		{
			name: "type parameter String",
			src:  "fn f[String](x: String) -> String { x }\n",
			want: "1:6: type parameter 'String' conflicts with type 'String'",
		},
		{
			name: "type argument on String",
			src:  "fn f(x: String[Int]) -> Int { 0 }\n",
			want: "1:9: wrong number of type arguments for 'String': expected 0, found 1",
		},
		{
			name: "non-exhaustive string match",
			src: `fn main() -> Int {
    match "a" {
        "a" => 1,
    }
}
`,
			want: "2:5: non-exhaustive match: missing _",
		},
		{
			name: "unreachable string arm",
			src: `fn main() -> Int {
    match "a" {
        "a" => 1,
        "a" => 2,
        _ => 0,
    }
}
`,
			want: "4:9: unreachable match arm",
		},
		{
			name: "string pattern on int",
			src: `fn main() -> Int {
    match 1 {
        "a" => 0,
        _ => 1,
    }
}
`,
			want: "3:9: expected Int, found String",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireError(t, tt.src, tt.want)
		})
	}

	t.Run("constructor conflicts with built-in", func(t *testing.T) {
		t.Parallel()

		file := &ast.File{
			Types: []*ast.TypeDecl{{
				Name:    "Box",
				NamePos: diag.Pos{Line: 1, Col: 6},
				Ctors: []*ast.CtorDecl{{
					Pos:  diag.Pos{Line: 1, Col: 12},
					Name: "intToString",
				}},
			}},
		}
		info, err := Check(file)
		if info != nil {
			t.Fatal("Check() info != nil, want nil")
		}
		const want = "1:12: constructor 'intToString' conflicts with built-in function 'intToString'"
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})
}

func TestCheckIOSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		src   string
		check func(*testing.T, *ast.File, *Info)
	}{
		{
			name: "unit literal",
			src: `fn id(u: Unit) -> Unit { u }
fn main() -> Int {
    let u: Unit = ()
    let v = ()
    0
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				id := funcByName(t, file, "id")
				if got := info.Params[id.Params[0]].Type.String(); got != "Unit" {
					t.Errorf("parameter u = %s, want Unit", got)
				}
				wantType(t, info, id.Body.Result, "Unit")
				lets := funcByName(t, file, "main").Body.Lets
				wantType(t, info, lets[0].Value, "Unit")
				wantType(t, info, lets[1].Value, "Unit")
				if got := info.Defs[lets[0]].Type.String(); got != "Unit" {
					t.Errorf("let u = %s, want Unit", got)
				}
				if got := info.Defs[lets[1]].Type.String(); got != "Unit" {
					t.Errorf("let v = %s, want Unit", got)
				}
				if info.MainIO {
					t.Error("MainIO = true, want false")
				}
			},
		},
		{
			name: "pure of int",
			src: `fn main() -> Int {
    let m = pure(1)
    0
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				call := funcByName(t, file, "main").Body.Lets[0].Value.(*ast.CallExpr)
				wantType(t, info, call, "IO[Int]")
				id := call.Fn.(*ast.Ident)
				wantTypeArgs(t, info, id, "Int")
				sym := info.Uses[id]
				if sym == nil || sym.Kind != SymBuiltin || sym.Builtin != BuiltinPure {
					t.Fatalf("pure symbol = %+v", sym)
				}
				if sym.Type.String() != "T -> IO[T]" {
					t.Errorf("pure type = %s, want T -> IO[T]", sym.Type)
				}
				if len(sym.TypeParams) != 1 || sym.TypeParams[0].Name != "T" {
					t.Errorf("pure type params = %v", sym.TypeParams)
				}
			},
		},
		{
			name: "bind infers the argument",
			src: `fn main() -> IO[Unit] {
    bind(readStdin(), fn(s) { print(s) })
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				call := funcByName(t, file, "main").Body.Result.(*ast.CallExpr)
				wantType(t, info, call, "IO[Unit]")
				wantTypeArgs(t, info, call.Fn.(*ast.Ident), "String", "Unit")
				lit := call.Args[1].(*ast.FuncLit)
				if got := info.Params[lit.Params[0]].Type.String(); got != "String" {
					t.Errorf("s = %s, want String", got)
				}
				if !info.MainIO {
					t.Error("MainIO = false, want true")
				}
			},
		},
		{
			name: "built-in calls",
			src: `fn main() -> Int {
    let a = pure(1)
    let b = bind(readStdin(), fn(s) { print(s) })
    let c = print("x")
    let d = eprint("x")
    let e = readStdin()
    let f = readFile("p")
    let g = fileExists("p")
    let h = writeFile("p", "x")
    let i = argCount()
    let j = argAt(0)
    let k = exit(0)
    0
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				lets := funcByName(t, file, "main").Body.Lets
				want := []struct {
					typ string
					b   Builtin
				}{
					{"IO[Int]", BuiltinPure},
					{"IO[Unit]", BuiltinBind},
					{"IO[Unit]", BuiltinPrint},
					{"IO[Unit]", BuiltinEPrint},
					{"IO[String]", BuiltinReadStdin},
					{"IO[String]", BuiltinReadFile},
					{"IO[Bool]", BuiltinFileExists},
					{"IO[Unit]", BuiltinWriteFile},
					{"IO[Int]", BuiltinArgCount},
					{"IO[String]", BuiltinArgAt},
					{"IO[Unit]", BuiltinExit},
				}
				if len(lets) != len(want) {
					t.Fatalf("lets = %d, want %d", len(lets), len(want))
				}
				for i, w := range want {
					wantType(t, info, lets[i].Value, w.typ)
					call := lets[i].Value.(*ast.CallExpr)
					sym := info.Uses[call.Fn.(*ast.Ident)]
					if sym == nil || sym.Kind != SymBuiltin || sym.Builtin != w.b {
						t.Errorf("call %d symbol = %+v, want built-in %d", i, sym, w.b)
					}
				}
				lit := lets[1].Value.(*ast.CallExpr).Args[1].(*ast.FuncLit)
				if got := info.Params[lit.Params[0]].Type.String(); got != "String" {
					t.Errorf("s = %s, want String", got)
				}
			},
		},
		{
			name: "pure as a value",
			src: `fn main() -> Int {
    let p = pure
    let a = p(1)
    let b = p(true)
    0
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				body := funcByName(t, file, "main").Body
				pureID := body.Lets[0].Value.(*ast.Ident)
				pureSym := info.Uses[pureID]
				if pureSym == nil || pureSym.Kind != SymBuiltin || pureSym.Builtin != BuiltinPure {
					t.Fatalf("pure symbol = %+v", pureSym)
				}
				p := info.Defs[body.Lets[0]]
				if p.Kind != SymLocal || len(p.TypeParams) != 1 {
					t.Fatalf("let p = %+v", p)
				}
				if p.Type.String() != "T -> IO[T]" {
					t.Errorf("let p type = %s, want T -> IO[T]", p.Type)
				}
				wantType(t, info, body.Lets[1].Value, "IO[Int]")
				wantType(t, info, body.Lets[2].Value, "IO[Bool]")
				ids := findIdents(body, "p")
				if len(ids) != 2 {
					t.Fatalf("uses of p = %d, want 2", len(ids))
				}
				wantTypeArgs(t, info, ids[0], "Int")
				wantTypeArgs(t, info, ids[1], "Bool")
				if info.Uses[ids[0]] != p || info.Uses[ids[1]] != p {
					t.Error("uses of p do not resolve to the let")
				}
			},
		},
		{
			name: "annotated main returns IO",
			src: `fn main() -> IO[Unit] {
    print("hi")
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "main"), "() -> IO[Unit]")
				if !info.MainIO {
					t.Error("MainIO = false, want true")
				}
			},
		},
		{
			name: "inferred main returns IO",
			src:  "fn main() { print(\"hi\") }\n",
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "main"), "() -> IO[Unit]")
				if !info.MainIO {
					t.Error("MainIO = false, want true")
				}
			},
		},
		{
			name: "recursive IO function",
			src: `fn loop(n: Int) -> IO[Unit] {
    if n <= 0 {
        pure(())
    } else {
        loop(n - 1)
    }
}
fn main() -> IO[Unit] {
    loop(1)
}
`,
			check: func(t *testing.T, file *ast.File, info *Info) {
				wantFuncScheme(t, info, funcByName(t, file, "loop"), "Int -> IO[Unit]")
				if !info.MainIO {
					t.Error("MainIO = false, want true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file, info := mustCheck(t, tt.src)
			assertComplete(t, file, info)
			if tt.check != nil {
				tt.check(t, file, info)
			}
		})
	}
}

func TestCheckIOError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "IO without a type argument",
			src:  "fn f() -> IO { 1 }\n",
			want: "1:11: wrong number of type arguments for 'IO': expected 1, found 0",
		},
		{
			name: "IO with two type arguments",
			src:  "fn f() -> IO[Int, Bool] { 1 }\n",
			want: "1:11: wrong number of type arguments for 'IO': expected 1, found 2",
		},
		{
			name: "type argument on Unit",
			src:  "fn f(x: Unit[Int]) -> Int { 0 }\n",
			want: "1:9: wrong number of type arguments for 'Unit': expected 0, found 1",
		},
		{
			name: "redefine IO",
			src:  "type IO = Mk\n",
			want: "1:6: cannot redefine built-in type 'IO'",
		},
		{
			name: "redefine Unit",
			src:  "type Unit = U\n",
			want: "1:6: cannot redefine built-in type 'Unit'",
		},
		{
			name: "type parameter Unit",
			src:  "fn f[Unit](x: Int) -> Int { x }\n",
			want: "1:6: type parameter 'Unit' conflicts with type 'Unit'",
		},
		{
			name: "type parameter IO",
			src:  "fn f[IO](x: Int) -> Int { x }\n",
			want: "1:6: type parameter 'IO' conflicts with type 'IO'",
		},
		{
			name: "compare unit",
			src:  "fn main() -> Int { () == () }\n",
			want: "1:20: cannot compare values of type Unit",
		},
		{
			name: "compare IO",
			src:  "fn main() -> Int { pure(1) == pure(1) }\n",
			want: "1:20: cannot compare values of type IO[Int]",
		},
		{
			name: "match on IO",
			src:  "fn main() -> Int { match pure(1) { _ => 0 } }\n",
			want: "1:26: cannot match on values of type IO[Int]",
		},
		{
			name: "main returns IO of Int",
			src:  "fn main() -> IO[Int] { pure(1) }\n",
			want: "1:4: function 'main' must have type () -> Int or () -> IO[Unit], found () -> IO[Int]",
		},
		{
			name: "main returns Bool",
			src:  "fn main() -> Bool { true }\n",
			want: "1:4: function 'main' must have type () -> Int or () -> IO[Unit], found () -> Bool",
		},
		{
			name: "inferred main returns Bool",
			src:  "fn main() { true }\n",
			want: "1:4: function 'main' must have type () -> Int or () -> IO[Unit], found () -> Bool",
		},
		{
			name: "bind expects IO",
			src: `fn f(x: Int) -> IO[Int] { pure(x) }
fn main() -> IO[Unit] { bind(1, f) }
`,
			want: "2:30: expected IO[?A], found Int",
		},
		{
			name: "print expects String",
			src:  "fn main() -> IO[Unit] { print(1) }\n",
			want: "1:31: expected String, found Int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requireError(t, tt.src, tt.want)
		})
	}
}

// wantFuncScheme checks that d's symbol has type want and type parameters
// named names, in order. names is empty for a monomorphic function.
func wantFuncScheme(
	t *testing.T,
	info *Info,
	d *ast.FuncDecl,
	want string,
	names ...string,
) {
	t.Helper()

	sym := info.Funcs[d]
	if sym == nil || sym.Type == nil {
		t.Fatalf("missing type for %s", d.Name)
	}
	if sym.Type.String() != want {
		t.Errorf("%s type = %s, want %s", d.Name, sym.Type, want)
	}
	if len(sym.TypeParams) != len(names) {
		t.Errorf("%s type params = %d, want %d", d.Name, len(sym.TypeParams), len(names))
		return
	}
	for i, name := range names {
		if sym.TypeParams[i].Name != name {
			t.Errorf("%s type param %d = %s, want %s", d.Name, i, sym.TypeParams[i].Name, name)
		}
	}
}

// wantScheme checks that the let's symbol has type want and type parameters
// named names, in order. names is empty for a monomorphic let.
func wantScheme(
	t *testing.T,
	info *Info,
	l *ast.LetStmt,
	want string,
	names ...string,
) {
	t.Helper()

	sym := info.Defs[l]
	if sym == nil {
		t.Fatalf("missing symbol for let %s", l.Name)
	}
	if sym.Type.String() != want {
		t.Errorf("let %s type = %s, want %s", l.Name, sym.Type, want)
	}
	if len(sym.TypeParams) != len(names) {
		t.Errorf("let %s type params = %d, want %d", l.Name, len(sym.TypeParams), len(names))
		return
	}
	for i, name := range names {
		if sym.TypeParams[i].Name != name {
			t.Errorf("let %s type param %d = %s, want %s", l.Name, i, sym.TypeParams[i].Name, name)
		}
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
	case *ast.IntLit, *ast.BoolLit, *ast.StringLit, *ast.UnitLit, *ast.Ident:
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
			switch sig.String() {
			case "() -> Int":
				if info.MainIO {
					t.Errorf("MainIO = true, want false")
				}
			case "() -> IO[Unit]":
				if !info.MainIO {
					t.Errorf("MainIO = false, want true")
				}
			default:
				t.Errorf("main type = %s, want () -> Int or () -> IO[Unit]", sig)
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
	case *ast.IntLit, *ast.BoolLit, *ast.StringLit, *ast.UnitLit:
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
	case *ast.WildcardPat, *ast.IntPat, *ast.BoolPat, *ast.StrPat:
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

type programMod struct {
	path string
	file string
	src  string
}

func TestCheckProgram(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			mods  []programMod
			check func(*testing.T, []*Module, *Info)
		}{
			{
				name: "call a public function",
				mods: []programMod{
					{path: "lib", file: "lib.cero", src: "pub fn inc(n: Int) -> Int { n + 1 }\n"},
					{path: "app", file: "app.cero", src: "import \"lib\"\nfn main() -> Int { inc(41) }\n"},
				},
				check: func(t *testing.T, mods []*Module, info *Info) {
					lib := modByPath(t, mods, "lib")
					app := modByPath(t, mods, "app")
					inc := funcByName(t, lib.Ast, "inc")
					call := funcByName(t, app.Ast, "main").Body.Result.(*ast.CallExpr)
					id := call.Fn.(*ast.Ident)
					if info.Uses[id] != info.Funcs[inc] {
						t.Errorf("inc resolved to %#v, want lib.inc", info.Uses[id])
					}
					wantType(t, info, call, "Int")
				},
			},
			{
				name: "public type and constructors",
				mods: []programMod{
					{
						path: "shapes",
						file: "shapes.cero",
						src:  "pub type Shape = | Circle(Int) | Rect(Int, Int)\n",
					},
					{
						path: "app",
						file: "app.cero",
						src: `import "shapes"
fn area(s: Shape) -> Int {
    match s {
        Circle(r) => r,
        Rect(w, h) => w * h
    }
}
fn classify(s) {
    match s {
        Circle(_) => 1,
        Rect(_, _) => 2
    }
}
fn main() -> Int { area(Circle(1)) + classify(Rect(2, 3)) }
`,
					},
				},
				check: func(t *testing.T, mods []*Module, info *Info) {
					shapes := modByPath(t, mods, "shapes")
					app := modByPath(t, mods, "app")
					data := info.Datas[typeByName(t, shapes.Ast, "Shape")]
					area := funcByName(t, app.Ast, "area")
					named, ok := info.Params[area.Params[0]].Type.(*types.Named)
					if !ok || named.Data != data {
						t.Errorf("area parameter type = %s, want Shape", info.Params[area.Params[0]].Type)
					}
					match := area.Body.Result.(*ast.MatchExpr)
					if info.CtorPats[match.Arms[0].Pattern.(*ast.CtorPat)] != data.Ctors[0] {
						t.Error("Circle pattern did not resolve to Shape.Circle")
					}
					if info.CtorPats[match.Arms[1].Pattern.(*ast.CtorPat)] != data.Ctors[1] {
						t.Error("Rect pattern did not resolve to Shape.Rect")
					}
					classify := funcByName(t, app.Ast, "classify")
					if got := info.Funcs[classify].Type.String(); got != "Shape -> Int" {
						t.Errorf("classify type = %s, want Shape -> Int", got)
					}
					sum := funcByName(t, app.Ast, "main").Body.Result.(*ast.BinaryExpr)
					circle := sum.X.(*ast.CallExpr).Args[0].(*ast.CallExpr)
					rect := sum.Y.(*ast.CallExpr).Args[0].(*ast.CallExpr)
					if info.Uses[circle.Fn.(*ast.Ident)].Ctor != data.Ctors[0] {
						t.Error("Circle call did not resolve to Shape.Circle")
					}
					if info.Uses[rect.Fn.(*ast.Ident)].Ctor != data.Ctors[1] {
						t.Error("Rect call did not resolve to Shape.Rect")
					}
				},
			},
			{
				name: "public polymorphic function at two types",
				mods: []programMod{
					{path: "lib", file: "lib.cero", src: "pub fn id[T](x: T) -> T { x }\n"},
					{
						path: "app",
						file: "app.cero",
						src: `import "lib"
fn main() -> Int {
    if id(true) { id(1) } else { 0 }
}
`,
					},
				},
				check: func(t *testing.T, mods []*Module, info *Info) {
					idFn := funcByName(t, modByPath(t, mods, "lib").Ast, "id")
					wantFuncScheme(t, info, idFn, "T -> T", "T")
					assertImportedID(t, mods, info, idFn)
				},
			},
			{
				name: "public inferred function",
				mods: []programMod{
					{path: "lib", file: "lib.cero", src: "pub fn id(x) { x }\n"},
					{
						path: "app",
						file: "app.cero",
						src: `import "lib"
fn main() -> Int {
    if id(true) { id(1) } else { 0 }
}
`,
					},
				},
				check: func(t *testing.T, mods []*Module, info *Info) {
					idFn := funcByName(t, modByPath(t, mods, "lib").Ast, "id")
					wantFuncScheme(t, info, idFn, "t1 -> t1", "t1")
					assertImportedID(t, mods, info, idFn)
				},
			},
			{
				name: "import through one module",
				mods: []programMod{
					{path: "c", file: "c.cero", src: "pub fn fromC() -> Int { 1 }\n"},
					{path: "b", file: "b.cero", src: "import \"c\"\npub fn fromB() -> Int { fromC() }\n"},
					{path: "a", file: "a.cero", src: "import \"b\"\nfn main() -> Int { fromB() }\n"},
				},
				check: func(t *testing.T, mods []*Module, info *Info) {
					a := modByPath(t, mods, "a")
					b := modByPath(t, mods, "b")
					c := modByPath(t, mods, "c")
					fromB := funcByName(t, b.Ast, "fromB")
					fromC := funcByName(t, c.Ast, "fromC")
					call := funcByName(t, a.Ast, "main").Body.Result.(*ast.CallExpr)
					if info.Uses[call.Fn.(*ast.Ident)] != info.Funcs[fromB] {
						t.Error("fromB did not resolve to b.fromB")
					}
					ids := findIdents(fromB.Body, "fromC")
					if len(ids) != 1 || info.Uses[ids[0]] != info.Funcs[fromC] {
						t.Error("fromC did not resolve to c.fromC")
					}
				},
			},
			{
				name: "own declaration shadows an import",
				mods: []programMod{
					{path: "lib", file: "lib.cero", src: "pub fn value() -> Int { 1 }\n"},
					{
						path: "app",
						file: "app.cero",
						src: `import "lib"
fn value() -> Int { 2 }
fn main() -> Int { value() }
`,
					},
				},
				check: func(t *testing.T, mods []*Module, info *Info) {
					lib := modByPath(t, mods, "lib")
					app := modByPath(t, mods, "app")
					own := funcByName(t, app.Ast, "value")
					imported := funcByName(t, lib.Ast, "value")
					call := funcByName(t, app.Ast, "main").Body.Result.(*ast.CallExpr)
					got := info.Uses[call.Fn.(*ast.Ident)]
					if got != info.Funcs[own] {
						t.Errorf("value resolved to %#v, want the entry's value", got)
					}
					if got == info.Funcs[imported] {
						t.Error("value resolved to the imported function")
					}
				},
			},
			{
				name: "shared dependency is not re-exported",
				mods: []programMod{
					{path: "d", file: "d.cero", src: "pub fn shared() -> Int { 1 }\n"},
					{path: "b", file: "b.cero", src: "import \"d\"\npub fn fromB() -> Int { shared() }\n"},
					{path: "c", file: "c.cero", src: "import \"d\"\npub fn fromC() -> Int { shared() }\n"},
					{path: "a", file: "a.cero", src: "import \"b\"\nimport \"c\"\nfn main() -> Int { fromB() + fromC() }\n"},
				},
				check: func(t *testing.T, mods []*Module, info *Info) {
					b := modByPath(t, mods, "b")
					c := modByPath(t, mods, "c")
					d := modByPath(t, mods, "d")
					shared := funcByName(t, d.Ast, "shared")
					bUse := findIdents(funcByName(t, b.Ast, "fromB").Body, "shared")
					cUse := findIdents(funcByName(t, c.Ast, "fromC").Body, "shared")
					if len(bUse) != 1 || len(cUse) != 1 || info.Uses[bUse[0]] != info.Funcs[shared] || info.Uses[cUse[0]] != info.Funcs[shared] {
						t.Error("shared did not resolve to the same d.shared from both importers")
					}
				},
			},
			{
				name: "main in a non-entry module",
				mods: []programMod{
					{
						path: "helper",
						file: "helper.cero",
						src: `fn main() { true }
pub fn answer() -> Int {
    if main() { 1 } else { 0 }
}
`,
					},
					{path: "app", file: "app.cero", src: "import \"helper\"\nfn main() -> Int { answer() }\n"},
				},
				check: func(t *testing.T, mods []*Module, info *Info) {
					helper := modByPath(t, mods, "helper")
					libMain := funcByName(t, helper.Ast, "main")
					if got := info.Funcs[libMain].Type.String(); got != "() -> Bool" {
						t.Errorf("helper main type = %s, want () -> Bool", got)
					}
					if info.Main == info.Funcs[libMain] {
						t.Error("Main is the helper's main")
					}
					ids := findIdents(funcByName(t, helper.Ast, "answer").Body, "main")
					if len(ids) != 1 || info.Uses[ids[0]] != info.Funcs[libMain] {
						t.Error("answer did not call the helper's main")
					}
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				info, mods := checkProgramOK(t, tt.mods)
				tt.check(t, mods, info)
			})
		}
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			mods []programMod
			file string
			want string
		}{
			{
				name: "private function",
				mods: []programMod{
					{path: "lib", file: "lib.cero", src: "fn secret() -> Int { 1 }\n"},
					{path: "app", file: "app.cero", src: "import \"lib\"\nfn main() -> Int { secret() }\n"},
				},
				file: "app.cero",
				want: "app.cero:2:20: undefined name 'secret'",
			},
			{
				name: "private type",
				mods: []programMod{
					{path: "lib", file: "lib.cero", src: "type Hidden = | Hidden\n"},
					{path: "app", file: "app.cero", src: "import \"lib\"\nfn f(x: Hidden) -> Int { 0 }\nfn main() -> Int { 0 }\n"},
				},
				file: "app.cero",
				want: "app.cero:2:9: unknown type 'Hidden'",
			},
			{
				name: "private constructor",
				mods: []programMod{
					{path: "lib", file: "lib.cero", src: "type Hidden = | Hidden\n"},
					{
						path: "app",
						file: "app.cero",
						src: `import "lib"
fn main() -> Int {
    match 0 {
        Hidden => 0
    }
}
`,
					},
				},
				file: "app.cero",
				want: "app.cero:4:9: unknown constructor 'Hidden'",
			},
			{
				name: "import is not transitive",
				mods: []programMod{
					{path: "c", file: "c.cero", src: "pub fn fromC() -> Int { 1 }\n"},
					{path: "b", file: "b.cero", src: "import \"c\"\npub fn fromB() -> Int { fromC() }\n"},
					{path: "a", file: "a.cero", src: "import \"b\"\nfn main() -> Int { fromC() }\n"},
				},
				file: "a.cero",
				want: "a.cero:2:20: undefined name 'fromC'",
			},
			{
				name: "two imports export the same value",
				mods: []programMod{
					{path: "b", file: "b.cero", src: "pub fn f() -> Int { 1 }\n"},
					{path: "c", file: "c.cero", src: "pub fn f() -> Int { 2 }\n"},
					{path: "a", file: "a.cero", src: "import \"b\"\nimport \"c\"\nfn main() -> Int { 0 }\n"},
				},
				file: "a.cero",
				want: "a.cero:2:1: 'f' is imported from both 'b' and 'c'",
			},
			{
				name: "two imports export the same type",
				mods: []programMod{
					{path: "b", file: "b.cero", src: "pub type Box = | B\n"},
					{path: "c", file: "c.cero", src: "pub type Box = | C\n"},
					{path: "a", file: "a.cero", src: "import \"b\"\nimport \"c\"\nfn main() -> Int { 0 }\n"},
				},
				file: "a.cero",
				want: "a.cero:2:1: 'Box' is imported from both 'b' and 'c'",
			},
			{
				name: "duplicate import",
				mods: []programMod{
					{path: "b", file: "b.cero", src: "pub fn f() -> Int { 1 }\n"},
					{path: "a", file: "a.cero", src: "import \"b\"\nimport \"b\"\nfn main() -> Int { f() }\n"},
				},
				file: "a.cero",
				want: "a.cero:2:1: duplicate import 'b'",
			},
			{
				name: "type error in an imported module",
				mods: []programMod{
					{path: "b", file: "b.cero", src: "pub fn f() -> Int { true }\n"},
					{path: "a", file: "a.cero", src: "import \"b\"\nfn main() -> Int { f() }\n"},
				},
				file: "b.cero",
				want: "b.cero:1:21: expected Int, found Bool",
			},
			{
				name: "entry has no main",
				mods: []programMod{
					{path: "app", file: "app.cero", src: "fn f() -> Int { 0 }\n"},
				},
				file: "app.cero",
				want: "app.cero:1:1: missing function 'main'",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				mods := parseProgram(t, tt.mods)
				info, err := CheckProgram(mods)
				if info != nil {
					t.Fatal("CheckProgram() info != nil, want nil")
				}
				if err == nil {
					t.Fatalf("CheckProgram() error = nil, want %q", tt.want)
				}
				de, ok := err.(*diag.Error)
				if !ok {
					t.Fatalf("error type = %T, want *diag.Error", err)
				}
				if de.File != tt.file {
					t.Errorf("error file = %q, want %q", de.File, tt.file)
				}
				if de.Error() != tt.want {
					t.Errorf("error = %q, want %q", de.Error(), tt.want)
				}
			})
		}
	})
}

func checkProgramOK(t *testing.T, specs []programMod) (*Info, []*Module) {
	t.Helper()

	mods := parseProgram(t, specs)
	info, err := CheckProgram(mods)
	if err != nil {
		t.Fatalf("CheckProgram() error = %v", err)
	}
	if info == nil {
		t.Fatal("CheckProgram() info = nil")
	}
	entry := mods[len(mods)-1]
	mainDecl := funcByName(t, entry.Ast, "main")
	if info.Main != info.Funcs[mainDecl] {
		t.Errorf("Main = %v, want the entry module's main", info.Main)
	}
	if info.Main == nil || info.Main.Type.String() != "() -> Int" {
		t.Errorf("Main type = %v, want () -> Int", info.Main)
	}
	if info.FuncModule == nil {
		t.Fatal("FuncModule = nil")
	}
	for _, mod := range mods {
		for _, d := range mod.Ast.Funcs {
			if info.FuncModule[d] != mod {
				t.Errorf("FuncModule[%s] = %v, want module %s", d.Name, info.FuncModule[d], mod.Path)
			}
		}
	}
	return info, mods
}

func parseProgram(t *testing.T, specs []programMod) []*Module {
	t.Helper()

	mods := make([]*Module, len(specs))
	byPath := make(map[string]*Module, len(specs))
	for i, spec := range specs {
		file, err := parser.ParseFile([]byte(spec.src))
		if err != nil {
			t.Fatalf("ParseFile(%s) error = %v", spec.path, err)
		}
		m := &Module{Path: spec.path, File: spec.file, Ast: file}
		mods[i] = m
		byPath[spec.path] = m
	}
	for _, m := range mods {
		for _, imp := range m.Ast.Imports {
			dep, ok := byPath[imp.Path]
			if !ok {
				t.Fatalf("module %s imports %q, which is not in the program", m.Path, imp.Path)
			}
			m.Imports = append(m.Imports, dep)
		}
	}
	return mods
}

func modByPath(t *testing.T, mods []*Module, path string) *Module {
	t.Helper()

	for _, m := range mods {
		if m.Path == path {
			return m
		}
	}
	t.Fatalf("module %s not found", path)
	return nil
}

func assertImportedID(t *testing.T, mods []*Module, info *Info, idFn *ast.FuncDecl) {
	t.Helper()

	iff := funcByName(t, modByPath(t, mods, "app").Ast, "main").Body.Result.(*ast.IfExpr)
	condID := iff.Cond.(*ast.CallExpr).Fn.(*ast.Ident)
	thenID := iff.Then.Result.(*ast.CallExpr).Fn.(*ast.Ident)
	if info.Uses[condID] != info.Funcs[idFn] || info.Uses[thenID] != info.Funcs[idFn] {
		t.Error("id did not resolve to the imported function")
	}
	wantTypeArgs(t, info, condID, "Bool")
	wantTypeArgs(t, info, thenID, "Int")
}
