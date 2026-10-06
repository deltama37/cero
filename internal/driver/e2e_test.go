package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2E(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("wasmtime"); err != nil {
		t.Skip("wasmtime not found")
	}

	root := moduleRoot(t)
	tests := []struct {
		name string
		src  string
		file string
		want string
	}{
		{
			name: "fib",
			file: "examples/fib.cero",
			want: "55",
		},
		{
			name: "higher order",
			file: "examples/higher_order.cero",
			want: "42",
		},
		{
			name: "abs",
			src: `fn abs(n: Int) -> Int {
    if n < 0 {
        -n
    } else {
        n
    }
}

fn main() -> Int {
    abs(-7) + abs(3)
}
`,
			want: "10",
		},
		{
			name: "calc",
			src: `fn calc(x: Int) -> Int {
    let doubled = x * 2
    let added = doubled + 10
    added
}

fn main() -> Int {
    calc(5)
}
`,
			want: "20",
		},
		{
			name: "apply anonymous double",
			src: `fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    let double = fn(x: Int) -> Int { x * 2 }
    apply(double, 21)
}
`,
			want: "42",
		},
		{
			name: "top-level function as a value",
			src: `fn inc(n: Int) -> Int {
    n + 1
}

fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    apply(inc, 41)
}
`,
			want: "42",
		},
		{
			name: "pick true",
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
			want: "4",
		},
		{
			name: "pick false",
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
    pick(false)(3)
}
`,
			want: "2",
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
    if isEven(10) { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "and short-circuit",
			src: `fn main() -> Int {
    if false && 1 / 0 == 0 { 1 } else { 2 }
}
`,
			want: "2",
		},
		{
			name: "or short-circuit",
			src: `fn main() -> Int {
    if true || 1 / 0 == 0 { 9 } else { 8 }
}
`,
			want: "9",
		},
		{
			name: "else if chain",
			src: `fn main() -> Int {
    let n = 2
    if n == 0 {
        10
    } else if n == 1 {
        20
    } else if n == 2 {
        30
    } else {
        40
    }
}
`,
			want: "30",
		},
		{
			name: "unary minus and division",
			src: `fn main() -> Int {
    -7 / 2
}
`,
			want: "-3",
		},
		{
			name: "bool equality and not",
			src: `fn toInt(b: Bool) -> Int {
    if b { 1 } else { 0 }
}

fn main() -> Int {
    toInt(true == true) + toInt(false == true) + toInt(true != false) + toInt(!false) + toInt(!true)
}
`,
			want: "3",
		},
		{
			name: "shadowing",
			src: `fn main() -> Int {
    let x = 1
    let x = x + 10
    let x = x * 4
    x
}
`,
			want: "44",
		},
		{
			name: "int wraparound",
			src: `fn main() -> Int {
    9223372036854775807 + 1
}
`,
			want: "-9223372036854775808",
		},
		{
			name: "deep recursion",
			src: `fn sum(n: Int) -> Int {
    if n == 0 {
        0
    } else {
        n + sum(n - 1)
    }
}

fn main() -> Int {
    sum(10000)
}
`,
			want: "50005000",
		},
		{
			name: "list",
			file: "examples/list.cero",
			want: "15",
		},
		{
			name: "shape area Circle(2) + Rect(3, 4)",
			src: `type Shape =
    | Circle(Int)
    | Rect(Int, Int)

fn area(s: Shape) -> Int {
    match s {
        Circle(r) => 3 * r * r,
        Rect(w, h) => w * h,
    }
}

fn main() -> Int {
    area(Circle(2)) + area(Rect(3, 4))
}
`,
			want: "24",
		},
		{
			name: "sum of range(1, 10000)",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn range(from: Int, to: Int) -> IntList {
    if from > to {
        Nil
    } else {
        Cons(from, range(from + 1, to))
    }
}

fn sum(xs: IntList) -> Int {
    match xs {
        Nil => 0,
        Cons(x, rest) => x + sum(rest),
    }
}

fn main() -> Int {
    sum(range(1, 10000))
}
`,
			want: "50005000",
		},
		{
			name: "length of range(1, 10000)",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn range(from: Int, to: Int) -> IntList {
    if from > to {
        Nil
    } else {
        Cons(from, range(from + 1, to))
    }
}

fn length(xs: IntList) -> Int {
    match xs {
        Nil => 0,
        Cons(_, rest) => 1 + length(rest),
    }
}

fn main() -> Int {
    length(range(1, 10000))
}
`,
			want: "10000",
		},
		{
			name: "Box(Int -> Int) calls inc(41)",
			src: `type Box = Box(Int -> Int)

fn inc(n: Int) -> Int {
    n + 1
}

fn apply(b: Box) -> Int {
    match b {
        Box(f) => f(41),
    }
}

fn main() -> Int {
    apply(Box(inc))
}
`,
			want: "42",
		},
		{
			name: "three constructors with Bool fields",
			src: `type Color =
    | Red
    | Green(Bool)
    | Blue(Bool, Int)

fn score(c: Color) -> Int {
    match c {
        Red => 1,
        Green(b) => if b { 2 } else { 3 },
        Blue(b, n) => if b { n } else { 0 },
    }
}

fn main() -> Int {
    score(Red) + score(Green(true)) + score(Blue(false, 9)) + score(Green(false))
}
`,
			want: "6",
		},
		{
			name: "wildcard and variable arms",
			src: `type U =
    | A
    | B

fn f(u: U) -> Int {
    match u {
        A => 5,
        _ => 7,
    }
}

fn g(u: U) -> Int {
    match u {
        A => 1,
        other => f(other),
    }
}

fn main() -> Int {
    f(A) + f(B) + g(B)
}
`,
			want: "19",
		},
		{
			name: "fib(10) written with integer patterns",
			src: `fn fib(n: Int) -> Int {
    match n {
        0 => 0,
        1 => 1,
        _ => fib(n - 1) + fib(n - 2),
    }
}

fn main() -> Int {
    fib(10)
}
`,
			want: "55",
		},
		{
			name: "boolean patterns true and false",
			src: `fn toInt(b: Bool) -> Int {
    match b {
        true => 1,
        false => 0,
    }
}

fn main() -> Int {
    toInt(true) * 10 + toInt(false) + toInt(2 == 2)
}
`,
			want: "11",
		},
		{
			name: "nested match on Cons rest",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn second(xs: IntList) -> Int {
    match xs {
        Nil => 0,
        Cons(_, rest) => match rest {
            Nil => 0,
            Cons(y, _) => y,
        },
    }
}

fn main() -> Int {
    second(Cons(1, Cons(2, Cons(3, Nil))))
}
`,
			want: "2",
		},
		{
			name: "match inside anonymous function",
			src: `fn apply(f: Int -> Int, x: Int) -> Int {
    f(x)
}

fn main() -> Int {
    apply(fn(n: Int) -> Int {
        match n {
            0 => 10,
            1 => 20,
            _ => n * 3,
        }
    }, 4)
}
`,
			want: "12",
		},
		{
			name: "mutually recursive Even and Odd",
			src: `type Even =
    | Zero
    | ESucc(Odd)

type Odd =
    | OSucc(Even)

fn evenVal(e: Even) -> Int {
    match e {
        Zero => 0,
        ESucc(o) => oddVal(o),
    }
}

fn oddVal(o: Odd) -> Int {
    match o {
        OSucc(e) => 1 + evenVal(e),
    }
}

fn main() -> Int {
    evenVal(ESucc(OSucc(ESucc(OSucc(Zero)))))
}
`,
			want: "2",
		},
		{
			name: "tail_sum example",
			file: "examples/tail_sum.cero",
			want: "50000005000000",
		},
		{
			name: "deep mutual tail recursion",
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
    if isEven(10000000) {
        1
    } else {
        0
    }
}
`,
			want: "1",
		},
		{
			name: "deep indirect tail call",
			src: `fn count(n: Int, acc: Int) -> Int {
    let next = fn(m: Int, a: Int) -> Int { count(m, a) }
    if n == 0 {
        acc
    } else {
        next(n - 1, acc + n)
    }
}

fn main() -> Int {
    count(10000000, 0)
}
`,
			want: "50000005000000",
		},
		{
			name: "deep tail call in match arm",
			src: `type IntList =
    | Nil
    | Cons(Int, IntList)

fn build(n: Int, acc: IntList) -> IntList {
    if n == 0 {
        acc
    } else {
        build(n - 1, Cons(n, acc))
    }
}

fn sum(xs: IntList, acc: Int) -> Int {
    match xs {
        Nil => acc,
        Cons(x, rest) => sum(rest, acc + x),
    }
}

fn main() -> Int {
    sum(build(1000000, Nil), 0)
}
`,
			want: "500000500000",
		},
		{
			name: "deep tail call in integer match",
			src: `fn down(n: Int, acc: Int) -> Int {
    match n {
        0 => acc,
        _ => down(n - 1, acc + 2),
    }
}

fn main() -> Int {
    down(10000000, 0)
}
`,
			want: "20000000",
		},
		{
			name: "deep tail call through && and ||",
			src: `fn allPositive(n: Int) -> Bool {
    n == 0 || (n > 0 && allPositive(n - 1))
}

fn main() -> Int {
    if allPositive(10000000) {
        1
    } else {
        0
    }
}
`,
			want: "1",
		},
		{
			name: "deep tail call after let",
			src: `fn go(n: Int, acc: Int) -> Int {
    if n == 0 {
        acc
    } else {
        let m = n - 1
        go(m, acc + n)
    }
}

fn main() -> Int {
    go(10000000, 0)
}
`,
			want: "50000005000000",
		},
		{
			name: "generic list example",
			file: "examples/generic_list.cero",
			want: "26",
		},
		{
			name: "option example",
			file: "examples/option.cero",
			want: "42",
		},
		{
			name: "two representations of identity",
			src: `fn identity[T](x: T) -> T { x }

fn main() -> Int {
    if identity(true) { identity(40) + 2 } else { 0 }
}
`,
			want: "42",
		},
		{
			name: "generic function passed as a value",
			src: `fn identity[T](x: T) -> T { x }

fn apply(f: Int -> Int, x: Int) -> Int { f(x) }

fn main() -> Int { apply(identity, 7) }
`,
			want: "7",
		},
		{
			name: "let bound to a generic function",
			src: `fn identity[T](x: T) -> T { x }

fn main() -> Int {
    let f = identity
    f(5) + 1
}
`,
			want: "6",
		},
		{
			name: "polymorphic recursion",
			src: `type List[T] =
    | Nil
    | Cons(T, List[T])

fn depth[T](x: T, n: Int) -> Int {
    if n == 0 { 0 } else { 1 + depth(Cons(x, Nil), n - 1) }
}

fn main() -> Int { depth(7, 10) }
`,
			want: "10",
		},
		{
			name: "Pair fields swapped between Int and Bool",
			src: `type Pair[A, B] = Pair(A, B)

fn fst[A, B](p: Pair[A, B]) -> A { match p { Pair(a, _) => a } }

fn snd[A, B](p: Pair[A, B]) -> B { match p { Pair(_, b) => b } }

fn main() -> Int {
    let p = Pair(3, true)
    let q = Pair(false, 20)
    fst(p) + (if snd(p) { 10 } else { 0 }) + (if fst(q) { 1000 } else { snd(q) })
}
`,
			want: "33",
		},
		{
			name: "lambda inside a generic function",
			src: `type Option[T] =
    | None
    | Some(T)

fn wrap[T](x: T) -> Option[T] {
    let mk = fn(v: T) -> Option[T] { Some(v) }
    mk(x)
}

fn getInt(o: Option[Int]) -> Int { match o { None => 0, Some(n) => n } }

fn getBool(o: Option[Bool]) -> Bool { match o { None => false, Some(b) => b } }

fn main() -> Int {
    if getBool(wrap(true)) { getInt(wrap(41)) + 1 } else { 0 }
}
`,
			want: "42",
		},
		{
			name: "long generic list",
			src: `type List[T] =
    | Nil
    | Cons(T, List[T])

fn range(from: Int, to: Int) -> List[Int] {
    if from > to { Nil } else { Cons(from, range(from + 1, to)) }
}

fn foldl[T, A](xs: List[T], acc: A, f: (A, T) -> A) -> A {
    match xs {
        Nil => acc,
        Cons(x, rest) => foldl(rest, f(acc, x), f),
    }
}

fn main() -> Int {
    foldl(range(1, 10000), 0, fn(a: Int, x: Int) -> Int { a + x })
}
`,
			want: "50005000",
		},
		{
			name: "nested type application",
			src: `type Option[T] =
    | None
    | Some(T)

type List[T] =
    | Nil
    | Cons(T, List[T])

fn sumSome(xs: List[Option[Int]]) -> Int {
    match xs {
        Nil => 0,
        Cons(o, rest) => match o { None => 0, Some(n) => n } + sumSome(rest),
    }
}

fn main() -> Int {
    sumSome(Cons(Some(1), Cons(None, Cons(Some(5), Nil))))
}
`,
			want: "6",
		},
		{
			name: "type parameter annotation in the body",
			src: `type Pair[A, B] = Pair(A, B)

fn dup[T](x: T) -> Pair[T, T] {
    let y: T = x
    Pair(x, y)
}

fn addPair(p: Pair[Int, Int]) -> Int { match p { Pair(a, b) => a + b } }

fn main() -> Int { addPair(dup(21)) }
`,
			want: "42",
		},
		{
			name: "non-regular data type and polymorphic recursion",
			src: `type Pair[A, B] = Pair(A, B)

type Nest[T] =
    | Flat(T)
    | Deep(Nest[Pair[T, T]])

fn size[T](n: Nest[T]) -> Int {
    match n {
        Flat(_) => 1,
        Deep(inner) => 2 * size(inner),
    }
}

fn main() -> Int {
    size(Deep(Deep(Flat(Pair(Pair(1, 2), Pair(3, 4))))))
}
`,
			want: "4",
		},
		{
			name: "deep tail recursion of specialized functions",
			src: `type Option[T] =
    | None
    | Some(T)

fn countdown[T](n: Int, x: T) -> T {
    if n == 0 { x } else { countdown(n - 1, x) }
}

fn loop[A](n: Int, acc: A, step: A -> A) -> A {
    if n == 0 { acc } else { loop(n - 1, step(acc), step) }
}

fn main() -> Int {
    let a = countdown(10000000, 30)
    let b = match countdown(10000000, Some(true)) {
        None => 0,
        Some(t) => if t { 2 } else { 0 },
    }
    a + b + loop(10000000, 0, fn(x: Int) -> Int { x + 1 }) - 9999990
}
`,
			want: "42",
		},
		{
			name: "let-polymorphic anonymous function",
			src:  "fn main() -> Int { let id = fn(x) { x } if id(true) { id(40) + 2 } else { 0 } }",
			want: "42",
		},
		{
			name: "let-polymorphic None",
			src: `type Option[T] = | None | Some(T)

fn orElse[T](o: Option[T], d: T) -> T { match o { None => d, Some(x) => x } }

fn main() -> Int {
    let none = None
    if orElse(none, true) { orElse(none, 41) + 1 } else { 0 }
}
`,
			want: "42",
		},
		{
			name: "match infers the scrutinee type",
			src: `type Option[T] = | None | Some(T)

fn main() -> Int {
    let get = fn(o) { match o { Some(n) => n, None => 0 } }
    get(Some(40)) + get(None) + 2
}
`,
			want: "42",
		},
		{
			name: "inference example",
			file: "examples/inference.cero",
			want: "42",
		},
		{
			name: "top-level inference example",
			file: "examples/toplevel_inference.cero",
			want: "42",
		},
		{
			name: "inferred mutual recursion",
			src: `fn isEven(n) {
    if n == 0 { true } else { isOdd(n - 1) }
}

fn isOdd(n) {
    if n == 0 { false } else { isEven(n - 1) }
}

fn main() {
    if isEven(10) { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "inferred polymorphic function at Int, Bool, and List",
			src: `type List[T] =
    | Nil
    | Cons(T, List[T])

fn id(x) { x }

fn length(xs) {
    match xs {
        Nil => 0,
        Cons(_, rest) => 1 + length(rest),
    }
}

fn main() {
    let n = id(40)
    let b = id(true)
    let xs = id(Cons(1, Nil))
    if b { n + length(xs) + 1 } else { 0 }
}
`,
			want: "42",
		},
		{
			name: "inferred tail recursion",
			src: `fn sum(n, acc) {
    if n == 0 { acc } else { sum(n - 1, acc + n) }
}

fn main() {
    sum(1000000, 0)
}
`,
			want: "500000500000",
		},
		{
			name: "closures example",
			file: "examples/closures.cero",
			want: "42",
		},
		{
			name: "makeAdder",
			src: `fn makeAdder(n: Int) -> Int -> Int {
    fn(x: Int) -> Int { x + n }
}

fn main() -> Int {
    makeAdder(40)(2)
}
`,
			want: "42",
		},
		{
			name: "map captures an outer value",
			src: `type List[T] =
    | Nil
    | Cons(T, List[T])

fn map[T, U](xs: List[T], f: T -> U) -> List[U] {
    match xs {
        Nil => Nil,
        Cons(x, rest) => Cons(f(x), map(rest, f)),
    }
}

fn foldl[T, A](xs: List[T], acc: A, f: (A, T) -> A) -> A {
    match xs {
        Nil => acc,
        Cons(x, rest) => foldl(rest, f(acc, x), f),
    }
}

fn main() -> Int {
    let k = 10
    let xs = Cons(1, Cons(2, Cons(3, Nil)))
    foldl(map(xs, fn(x) { x * k }), 0, fn(acc, x) { acc + x })
}
`,
			want: "60",
		},
		{
			name: "nested closure captures two levels out",
			src: `fn main() -> Int {
    let n = 40
    let f = fn(x: Int) -> Int {
        let g = fn(y: Int) -> Int { x + y + n }
        g(1)
    }
    f(1)
}
`,
			want: "42",
		},
		{
			name: "capture bool, function, data, and int",
			src: `type Box = Box(Int)

fn id(n: Int) -> Int { n }

fn main() -> Int {
    let flag = true
    let f = id
    let b = Box(10)
    let n = 2
    let g = fn(x: Int) -> Int {
        let m = match b {
            Box(v) => v,
        }
        if flag { f(m) + n + x } else { 0 }
    }
    g(30)
}
`,
			want: "42",
		},
		{
			name: "capturing generalized let at two types and from a closure",
			src: `fn main() -> Int {
    let y = 1
    let id = fn(x) { y }
    let fromBool = id(true)
    let fromInt = id(2)
    let h = fn() -> Int { id(0) }
    fromBool + fromInt + h() + 39
}
`,
			want: "42",
		},
		{
			name: "continuation passing a million closures",
			src: `fn count(n: Int, k: Int -> Int) -> Int {
    if n == 0 {
        k(0)
    } else {
        count(n - 1, fn(x: Int) -> Int { k(x + 1) })
    }
}

fn main() -> Int {
    count(1000000, fn(x: Int) -> Int { x })
}
`,
			want: "1000000",
		},
		{
			name: "call a function value stored in a data type",
			src: `type Box = Box(Int -> Int)

fn main() -> Int {
    let n = 40
    let b = Box(fn(x: Int) -> Int { x + n })
    match b {
        Box(f) => f(2),
    }
}
`,
			want: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := []byte(tt.src)
			filename := "main.cero"
			if tt.file != "" {
				filename = tt.file
				var err error
				src, err = os.ReadFile(filepath.Join(root, tt.file))
				if err != nil {
					t.Fatalf("read %s: %v", tt.file, err)
				}
			}
			wasm, err := Compile(filename, src)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			path := filepath.Join(t.TempDir(), "main.wasm")
			if err := os.WriteFile(path, wasm, 0o644); err != nil {
				t.Fatalf("write wasm: %v", err)
			}
			cmd := exec.Command("wasmtime", "run", "--invoke", "main", path)
			out, err := cmd.Output()
			if err != nil {
				stderr := ""
				if ee, ok := err.(*exec.ExitError); ok {
					stderr = string(ee.Stderr)
				}
				t.Fatalf("wasmtime: %v\nstderr:\n%s", err, stderr)
			}
			if got := strings.TrimSpace(string(out)); got != tt.want {
				t.Errorf("stdout = %q, want %q", got, tt.want)
			}
		})
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
