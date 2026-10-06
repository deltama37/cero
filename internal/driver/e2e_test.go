package driver

import (
	"bytes"
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
		{
			name: "nested patterns",
			file: "examples/nested_patterns.cero",
			want: "42",
		},
		{
			name: "second element",
			src: `type List[T] =
    | Nil
    | Cons(T, List[T])

fn second(xs: List[Int]) -> Int {
    match xs {
        Cons(_, Cons(y, _)) => y,
        _ => 0,
    }
}

fn main() -> Int {
    second(Cons(10, Cons(32, Cons(1, Nil))))
}
`,
			want: "32",
		},
		{
			name: "pair of booleans",
			src: `type Pair[A, B] = Pair(A, B)

fn both(p: Pair[Bool, Bool]) -> Int {
    match p {
        Pair(true, true) => 3,
        Pair(true, false) => 2,
        Pair(false, true) => 1,
        Pair(false, false) => 0,
    }
}

fn main() -> Int {
    both(Pair(true, true)) + both(Pair(true, false)) + both(Pair(false, true)) + both(Pair(false, false))
}
`,
			want: "6",
		},
		{
			name: "three nested constructors",
			src: `type List[T] =
    | Nil
    | Cons(T, List[T])

type Option[T] =
    | None
    | Some(T)

fn dig(o: Option[List[Int]]) -> Int {
    match o {
        Some(Cons(_, Cons(x, _))) => x,
        _ => 0,
    }
}

fn main() -> Int {
    dig(Some(Cons(1, Cons(41, Nil))))
}
`,
			want: "41",
		},
		{
			name: "tail recursive sum of a million pairs",
			src: `type List[T] =
    | Nil
    | Cons(T, List[T])

fn build(n: Int, acc: List[Int]) -> List[Int] {
    if n == 0 {
        acc
    } else {
        build(n - 1, Cons(n, acc))
    }
}

fn sumPairs(xs: List[Int], acc: Int) -> Int {
    match xs {
        Cons(x, Cons(y, rest)) => sumPairs(rest, acc + x + y),
        _ => acc,
    }
}

fn main() -> Int {
    sumPairs(build(1000000, Nil), 0)
}
`,
			want: "500000500000",
		},
		{
			name: "nested integer literal",
			src: `type Option[T] =
    | None
    | Some(T)

fn f(o: Option[Int]) -> Int {
    match o {
        Some(7) => 11,
        Some(_) => 2,
        None => 0,
    }
}

fn main() -> Int {
    f(Some(7)) + f(Some(8)) + f(None)
}
`,
			want: "13",
		},
		{
			name: "strings example",
			file: "examples/strings.cero",
			want: "42",
		},
		{
			name: "conveniences example",
			file: "examples/conveniences.cero",
			want: "42",
		},
		{
			name: "remainder signs",
			src: `fn main() -> Int {
    (7 % 3) * 100 + (-7 % 3) * 10 + (7 % -3)
}
`,
			want: "91",
		},
		{
			name: "character a plus one",
			src:  "fn main() -> Int { 'a' + 1 }\n",
			want: "98",
		},
		{
			name: "character newline",
			src:  "fn main() -> Int { '\\n' }\n",
			want: "10",
		},
		{
			name: "bit and",
			src:  "fn main() -> Int { bitAnd(12, 10) }\n",
			want: "8",
		},
		{
			name: "bit or",
			src:  "fn main() -> Int { bitOr(12, 10) }\n",
			want: "14",
		},
		{
			name: "bit xor",
			src:  "fn main() -> Int { bitXor(12, 10) }\n",
			want: "6",
		},
		{
			name: "shift left",
			src:  "fn main() -> Int { shiftLeft(1, 62) }\n",
			want: "4611686018427387904",
		},
		{
			name: "shift right",
			src:  "fn main() -> Int { shiftRight(-8, 1) }\n",
			want: "-4",
		},
		{
			name: "shift right unsigned",
			src:  "fn main() -> Int { shiftRightUnsigned(-1, 60) }\n",
			want: "15",
		},
		{
			name: "shift by 64 leaves the value",
			src: `fn main() -> Int {
    if shiftLeft(42, 64) == 42 && shiftRight(-8, 64) == -8 && shiftRightUnsigned(15, 64) == 15 {
        1
    } else {
        0
    }
}
`,
			want: "1",
		},
		{
			name: "signed leb128 of -123456",
			src: `fn slebSum(n: Int) -> Int {
    let b = bitAnd(n, 127)
    let next = shiftRight(n, 7)
    if (next == 0 && bitAnd(b, 64) == 0) || (next == -1 && bitAnd(b, 64) != 0) {
        b
    } else {
        bitOr(b, 128) + slebSum(next)
    }
}

fn main() -> Int {
    slebSum(-123456)
}
`,
			want: "499",
		},
		{
			name: "option bind with let bang",
			src: `type Option[T] =
    | None
    | Some(T)

fn bind[A, B](m: Option[A], f: A -> Option[B]) -> Option[B] {
    match m {
        None => None,
        Some(x) => f(x),
    }
}

fn main() -> Int {
    let r = {
        let! x = Some(20)
        let! y = Some(x + 22)
        Some(y)
    }
    match r {
        Some(n) => n,
        None => 0,
    }
}
`,
			want: "42",
		},
		{
			name: "let pair pattern",
			src: `type Pair[A, B] = Pair(A, B)
fn main() -> Int {
    let Pair(a, b) = Pair(19, 23)
    a + b
}
`,
			want: "42",
		},
		{
			name: "string concatenation length",
			src:  "fn main() -> Int { stringLength(\"hello\" ++ \", \" ++ \"world\") }\n",
			want: "12",
		},
		{
			name: "string byte at",
			src:  "fn main() -> Int { stringByteAt(\"A\", 0) }\n",
			want: "65",
		},
		{
			name: "string escapes",
			src:  "fn main() -> Int { stringLength(\"a\\n\\t\\\\\\\"\\0\\x41\") }\n",
			want: "7",
		},
		{
			name: "utf-8 byte length",
			src:  "fn main() -> Int { stringLength(\"é\") }\n",
			want: "2",
		},
		{
			name: "string slice",
			src: `fn main() -> Int {
    if stringSlice("hello", 1, 4) == "ell" { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "string compare",
			src: `fn main() -> Int {
    let ab = stringCompare("a", "b")
    let ba = stringCompare("b", "a")
    let eq = stringCompare("ab", "ab")
    let pre = stringCompare("a", "ab")
    ab + 1 + (ba + 1) * 2 + (eq + 1) * 4 + (pre + 1) * 8
}
`,
			want: "8",
		},
		{
			name: "int to string",
			src: `fn bit(b: Bool, n: Int) -> Int {
    if b { n } else { 0 }
}

fn main() -> Int {
    let z = intToString(0)
    let p = intToString(42)
    let n = intToString(-7)
    let m = intToString(-9223372036854775807 - 1)
    bit(z == "0", 1) + bit(stringLength(z) == 1, 2) + bit(p == "42", 4) + bit(stringLength(p) == 2, 8) + bit(n == "-7", 16) + bit(stringLength(n) == 2, 32) + bit(m == "-9223372036854775808", 64) + bit(stringLength(m) == 20, 128)
}
`,
			want: "255",
		},
		{
			name: "string from byte",
			src: `fn main() -> Int {
    if stringFromByte(104) ++ "i" == "hi" { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "empty string",
			src: `fn main() -> Int {
    let e = ""
    if e == "" && stringLength(e) == 0 && e ++ "a" == "a" && "b" ++ e == "b" { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "empty slice",
			src: `fn main() -> Int {
    if stringSlice("hello", 2, 2) == "" && stringSlice("hello", 0, 0) == "" && stringSlice("", 0, 0) == "" && stringLength(stringSlice("ab", 1, 1)) == 0 { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "byte boundaries",
			src: `fn main() -> Int {
    let hi = stringFromByte(255)
    let z = stringFromByte(0)
    if stringLength(hi) == 1 && stringByteAt(hi, 0) == 255 && stringLength(z) == 1 && stringByteAt(z, 0) == 0 { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "string match",
			src: `fn classify(w: String) -> Int {
    match w {
        "fn" => 1,
        "let" => 2,
        "match" => 3,
        _ => 0,
    }
}

fn main() -> Int {
    classify("fn") * 100 + classify("let") * 10 + classify("nope")
}
`,
			want: "120",
		},
		{
			name: "built-in passed as a value",
			src: `fn apply(f: String -> Int, s: String) -> Int {
    f(s)
}

fn main() -> Int {
    apply(stringLength, "abc")
}
`,
			want: "3",
		},
		{
			name: "ten thousand concatenations",
			src: `fn grow(n: Int, acc: String) -> String {
    if n == 0 {
        acc
    } else {
        grow(n - 1, acc ++ "x")
    }
}

fn main() -> Int {
    stringLength(grow(10000, ""))
}
`,
			want: "10000",
		},
		{
			name: "allocate past 2GiB",
			src: `fn double(s: String, n: Int) -> String {
    if n == 0 {
        s
    } else {
        double(s ++ s, n - 1)
    }
}

fn burn(chunk: String, n: Int) -> Int {
    let s = chunk ++ "x"
    if n == 1 {
        stringLength(s)
    } else {
        burn(chunk, n - 1)
    }
}

fn main() -> Int {
    burn(double("a", 25), 66)
}
`,
			want: "33554433",
		},
		{
			name: "literal longer than 64KiB",
			src:  "fn main() -> Int { stringLength(\"" + strings.Repeat("a", 70000) + "\") }\n",
			want: "70000",
		},
		{
			name: "std list length",
			src: `import "std/list"
fn main() -> Int {
    let empty: List[Int] = Nil
    length(Cons(1, Cons(2, Cons(3, Nil)))) * 10 + length(empty)
}
`,
			want: "30",
		},
		{
			name: "std list map",
			src: `import "std/list"
fn pack(xs: List[Int]) -> Int {
    foldl(xs, 0, fn(acc: Int, x: Int) -> Int { acc * 10 + x })
}
fn main() -> Int {
    pack(map(Cons(1, Cons(2, Cons(3, Nil))), fn(x: Int) -> Int { x + 1 }))
}
`,
			want: "234",
		},
		{
			name: "std list filter",
			src: `import "std/list"
fn pack(xs: List[Int]) -> Int {
    foldl(xs, 0, fn(acc: Int, x: Int) -> Int { acc * 10 + x })
}
fn main() -> Int {
    pack(filter(Cons(1, Cons(2, Cons(3, Cons(4, Nil)))), fn(x: Int) -> Bool { x / 2 * 2 == x }))
}
`,
			want: "24",
		},
		{
			name: "std list foldl",
			src: `import "std/list"
fn main() -> Int {
    foldl(Cons(1, Cons(2, Cons(3, Nil))), 0, fn(acc: Int, x: Int) -> Int { acc - x })
}
`,
			want: "-6",
		},
		{
			name: "std list foldr order",
			src: `import "std/list"
fn main() -> Int {
    foldr(Cons(1, Cons(2, Cons(3, Nil))), 0, fn(x: Int, acc: Int) -> Int { x - acc })
}
`,
			want: "2",
		},
		{
			name: "std list reverse",
			src: `import "std/list"
fn pack(xs: List[Int]) -> Int {
    foldl(xs, 0, fn(acc: Int, x: Int) -> Int { acc * 10 + x })
}
fn main() -> Int {
    pack(reverse(Cons(1, Cons(2, Cons(3, Nil)))))
}
`,
			want: "321",
		},
		{
			name: "std list append",
			src: `import "std/list"
fn pack(xs: List[Int]) -> Int {
    foldl(xs, 0, fn(acc: Int, x: Int) -> Int { acc * 10 + x })
}
fn main() -> Int {
    pack(append(Cons(1, Cons(2, Nil)), Cons(3, Cons(4, Nil))))
}
`,
			want: "1234",
		},
		{
			name: "std list concat",
			src: `import "std/list"
fn pack(xs: List[Int]) -> Int {
    foldl(xs, 0, fn(acc: Int, x: Int) -> Int { acc * 10 + x })
}
fn main() -> Int {
    pack(concat(Cons(Cons(1, Cons(2, Nil)), Cons(Cons(3, Nil), Nil))))
}
`,
			want: "123",
		},
		{
			name: "std list any",
			src: `import "std/list"
fn b(x: Bool) -> Int { if x { 1 } else { 0 } }
fn main() -> Int {
    b(any(Cons(1, Cons(2, Nil)), fn(x: Int) -> Bool { x == 2 })) * 100 + b(any(Cons(1, Nil), fn(x: Int) -> Bool { x == 2 })) * 10 + b(any(Nil, fn(x: Int) -> Bool { x == 1 }))
}
`,
			want: "100",
		},
		{
			name: "std list all",
			src: `import "std/list"
fn b(x: Bool) -> Int { if x { 1 } else { 0 } }
fn main() -> Int {
    b(all(Cons(1, Cons(2, Nil)), fn(x: Int) -> Bool { x > 0 })) * 100 + b(all(Cons(0, Nil), fn(x: Int) -> Bool { x > 0 })) * 10 + b(all(Nil, fn(x: Int) -> Bool { x > 0 }))
}
`,
			want: "101",
		},
		{
			name: "std list range empty",
			src: `import "std/list"
fn pack(xs: List[Int]) -> Int {
    foldl(xs, 0, fn(acc: Int, x: Int) -> Int { acc * 10 + x })
}
fn main() -> Int {
    if length(range(3, 3)) == 0 && length(range(5, 2)) == 0 && pack(range(1, 4)) == 123 { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "std list sum",
			src: `import "std/list"
fn main() -> Int { sum(range(1, 5)) }
`,
			want: "10",
		},
		{
			name: "std list take and drop boundaries",
			src: `import "std/list"
fn pack(xs: List[Int]) -> Int {
    foldl(xs, 0, fn(acc: Int, x: Int) -> Int { acc * 10 + x })
}
fn main() -> Int {
    let xs = Cons(1, Cons(2, Cons(3, Cons(4, Nil))))
    if pack(take(xs, 0)) == 0 && pack(take(xs, -3)) == 0 && pack(take(xs, 2)) == 12 && pack(take(xs, 9)) == 1234 && pack(drop(xs, 0)) == 1234 && pack(drop(xs, -1)) == 1234 && pack(drop(xs, 2)) == 34 && pack(drop(xs, 9)) == 0 {
        1
    } else {
        0
    }
}
`,
			want: "1",
		},
		{
			name: "std option withDefault",
			src: `import "std/option"
fn main() -> Int { withDefault(Some(41), 0) + withDefault(None, 1) }
`,
			want: "42",
		},
		{
			name: "std option mapOption",
			src: `import "std/option"
fn main() -> Int {
    withDefault(mapOption(Some(21), fn(x: Int) -> Int { x * 2 }), 0) + withDefault(mapOption(None, fn(x: Int) -> Int { x }), 0)
}
`,
			want: "42",
		},
		{
			name: "std option andThen",
			src: `import "std/option"
fn main() -> Int {
    withDefault(andThen(Some(20), fn(x: Int) -> Option[Int] { Some(x + 1) }), 0) + withDefault(andThen(None, fn(x: Int) -> Option[Int] { Some(x) }), 5) + withDefault(andThen(Some(1), fn(x: Int) -> Option[Int] { None }), 16)
}
`,
			want: "42",
		},
		{
			name: "std option isSome and isNone",
			src: `import "std/option"
fn main() -> Int {
    let missing: Option[Int] = None
    if isSome(Some(0)) && !isSome(missing) && isNone(missing) && !isNone(Some(0)) { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "std pair fst and snd",
			src: `import "std/pair"
fn main() -> Int { fst(Pair(40, 1)) + snd(Pair(0, 2)) }
`,
			want: "42",
		},
		{
			name: "std string join empty and several",
			src: `import "std/list"
import "std/string"
fn main() -> Int {
    let empty: List[String] = Nil
    if join(empty, ",") == "" && join(Cons("a", Nil), ",") == "a" && join(Cons("a", Cons("b", Cons("c", Nil))), "-") == "a-b-c" { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "std string startsWith",
			src: `import "std/string"
fn main() -> Int {
    if startsWith("hello", "hel") && startsWith("hello", "") && !startsWith("hello", "hi") && !startsWith("ab", "abc") { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "std string endsWith",
			src: `import "std/string"
fn main() -> Int {
    if endsWith("hello", "llo") && endsWith("hello", "") && !endsWith("hello", "hi") && !endsWith("ab", "cab") { 1 } else { 0 }
}
`,
			want: "1",
		},
		{
			name: "std string isDigit isAlpha isSpace",
			src: `import "std/string"
fn byte(s: String) -> Int { stringByteAt(s, 0) }
fn main() -> Int {
    if isDigit(byte("0")) && isDigit(byte("9")) && !isDigit(byte("a")) && !isDigit(byte("/")) && isAlpha(byte("A")) && isAlpha(byte("Z")) && isAlpha(byte("a")) && isAlpha(byte("z")) && !isAlpha(byte("0")) && !isAlpha(byte(" ")) && isSpace(byte(" ")) && isSpace(byte("\t")) && isSpace(byte("\n")) && isSpace(byte("\r")) && !isSpace(byte("a")) && !isSpace(byte("0")) {
        1
    } else {
        0
    }
}
`,
			want: "1",
		},
		{
			name: "std list million elements",
			src: `import "std/list"
fn main() -> Int {
    let xs = range(0, 1000000)
    let ys = filter(map(xs, fn(x: Int) -> Int { x + 1 }), fn(x: Int) -> Bool { x > 0 })
    if length(ys) == 1000000 && foldr(xs, 0, fn(x: Int, acc: Int) -> Int { acc + 1 }) == 1000000 && length(xs) == 1000000 {
        1
    } else {
        0
    }
}
`,
			want: "1",
		},
		{
			name: "modules example",
			file: "examples/modules/main.cero",
			want: "42",
		},
		{
			name: "stdlib example",
			file: "examples/stdlib.cero",
			want: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := []byte(tt.src)
			filename := "main.cero"
			if tt.file != "" {
				filename = filepath.Join(root, tt.file)
				var err error
				src, err = os.ReadFile(filename)
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

func TestE2ERuntimeTrap(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("wasmtime"); err != nil {
		t.Skip("wasmtime not found")
	}

	tests := []struct {
		name string
		src  string
	}{
		{
			name: "stringByteAt out of range",
			src:  "fn main() -> Int { stringByteAt(\"A\", 1) }\n",
		},
		{
			name: "stringByteAt on an empty string",
			src:  "fn main() -> Int { stringByteAt(\"\", 0) }\n",
		},
		{
			name: "stringFromByte above 255",
			src:  "fn main() -> Int { stringLength(stringFromByte(256)) }\n",
		},
		{
			name: "stringSlice end past the length",
			src:  "fn main() -> Int { stringLength(stringSlice(\"ab\", 0, 3)) }\n",
		},
		{
			name: "remainder by zero",
			src:  "fn main() -> Int { 0 % 0 }\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wasm, err := Compile("main.cero", []byte(tt.src))
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			path := filepath.Join(t.TempDir(), "main.wasm")
			if err := os.WriteFile(path, wasm, 0o644); err != nil {
				t.Fatalf("write wasm: %v", err)
			}
			cmd := exec.Command("wasmtime", "run", "--invoke", "main", path)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("wasmtime succeeded, stdout = %q", out)
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() == 0 {
				t.Fatalf("wasmtime error = %v, output = %q", err, out)
			}
		})
	}
}

func TestE2EIO(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("wasmtime"); err != nil {
		t.Skip("wasmtime not found")
	}

	root := moduleRoot(t)
	large := strings.Repeat("0123456789", 7000) + "END"
	if len(large) <= 64*1024 {
		t.Fatal("large payload is not bigger than 64KiB")
	}

	tests := []struct {
		name         string
		src          string
		file         string
		args         []string
		stdin        string
		files        map[string]string
		invoke       bool
		wantStdout   string
		checkStdout  bool
		wantNewlines int
		wantStderr   string
		stderrHas    string
		wantExit     int
		wantFile     string
		wantBody     string
	}{
		{
			name:        "hello",
			file:        "examples/io/hello.cero",
			checkStdout: true,
			wantStdout:  "Hello, Cero!\n",
		},
		{
			name: "cat two files",
			file: "examples/io/cat.cero",
			args: []string{"a.txt", "b.txt"},
			files: map[string]string{
				"a.txt": "foo",
				"b.txt": "bar",
			},
			checkStdout: true,
			wantStdout:  "foobar",
		},
		{
			name:        "cat stdin",
			file:        "examples/io/cat.cero",
			stdin:       "from stdin",
			checkStdout: true,
			wantStdout:  "from stdin",
		},
		{
			name:      "cat missing file",
			file:      "examples/io/cat.cero",
			args:      []string{"missing.txt"},
			stderrHas: "missing.txt",
			wantExit:  1,
		},
		{
			name:        "wc",
			file:        "examples/io/wc.cero",
			stdin:       "hello world\n",
			checkStdout: true,
			wantStdout:  "1 2 12\n",
		},
		{
			name: "let bang sequences IO",
			src: `fn main() -> IO[Unit] {
    let! n = pure(40)
    let! m = pure(n + 2)
    print(intToString(m))
}
`,
			checkStdout: true,
			wantStdout:  "42",
		},
		{
			name: "pure and bind",
			src: `fn main() {
    bind(pure(41), fn(n: Int) -> IO[Unit] { print(intToString(n + 1)) })
}
`,
			checkStdout: true,
			wantStdout:  "42",
		},
		{
			name: "exit stops later actions",
			src: `fn main() {
    bind(exit(3), fn(ignored: Unit) -> IO[Unit] { print("later") })
}
`,
			checkStdout: true,
			wantExit:    3,
		},
		{
			name: "writeFile then readFile",
			src: `fn main() {
    bind(writeFile("out.txt", "hello"), fn(ignored: Unit) -> IO[Unit] {
        bind(readFile("out.txt"), fn(s: String) -> IO[Unit] { print(s) })
    })
}
`,
			checkStdout: true,
			wantStdout:  "hello",
			wantFile:    "out.txt",
			wantBody:    "hello",
		},
		{
			name: "readFile missing",
			src: `fn main() {
    bind(readFile("missing.txt"), fn(s: String) -> IO[Unit] { print(s) })
}
`,
			wantStderr: "error: cannot read file missing.txt\n",
			wantExit:   1,
		},
		{
			name: "fileExists",
			src: `fn main() {
    bind(fileExists("yes.txt"), fn(a: Bool) -> IO[Unit] {
        bind(fileExists("no.txt"), fn(b: Bool) -> IO[Unit] {
            print((if a { "Y" } else { "N" }) ++ (if b { "Y" } else { "N" }))
        })
    })
}
`,
			files:       map[string]string{"yes.txt": "here"},
			checkStdout: true,
			wantStdout:  "YN",
		},
		{
			name: "argCount and argAt",
			src: `fn main() {
    bind(argCount(), fn(n: Int) -> IO[Unit] {
        bind(argAt(0), fn(a: String) -> IO[Unit] {
            bind(argAt(1), fn(b: String) -> IO[Unit] {
                print(intToString(n) ++ " " ++ a ++ " " ++ b)
            })
        })
    })
}
`,
			args:        []string{"foo", "bar"},
			checkStdout: true,
			wantStdout:  "2 foo bar",
		},
		{
			name: "argAt out of range",
			src: `fn main() {
    bind(argAt(0), fn(s: String) -> IO[Unit] { print(s) })
}
`,
			wantExit: -1,
		},
		{
			name: "million binds",
			src: `fn loop(n: Int, acc: Int) -> IO[Unit] {
    if n == 0 {
        print(intToString(acc))
    } else {
        bind(pure(acc + 1), fn(a: Int) -> IO[Unit] { loop(n - 1, a) })
    }
}

fn main() {
    loop(1000000, 0)
}
`,
			checkStdout: true,
			wantStdout:  "1000000",
		},
		{
			name: "forEach prints 100000 lines",
			src: `import "std/list"
import "std/io"

fn main() {
    forEach(range(0, 100000), fn(n: Int) -> IO[Unit] { print("\n") })
}
`,
			wantNewlines: 100000,
		},
		{
			name: "stdin over 64KiB",
			src: `fn main() {
    bind(readStdin(), fn(s: String) -> IO[Unit] { print(s) })
}
`,
			stdin:       large,
			checkStdout: true,
			wantStdout:  large,
		},
		{
			name: "write and read over 64KiB",
			src: `fn main() {
    bind(readStdin(), fn(s: String) -> IO[Unit] {
        bind(writeFile("big.txt", s), fn(ignored: Unit) -> IO[Unit] {
            bind(readFile("big.txt"), fn(t: String) -> IO[Unit] { print(t) })
        })
    })
}
`,
			stdin:       large,
			checkStdout: true,
			wantStdout:  large,
			wantFile:    "big.txt",
			wantBody:    large,
		},
		{
			name: "std io then mapIO args",
			src: `import "std/list"
import "std/io"

fn main() {
    bind(mapIO(pure(21), fn(n: Int) -> Int { n * 2 }), fn(n: Int) -> IO[Unit] {
        then(
            print(intToString(n)),
            bind(args(), fn(xs: List[String]) -> IO[Unit] {
                match xs {
                    Nil => print("empty"),
                    Cons(s, _) => print(":" ++ intToString(length(xs)) ++ ":" ++ s),
                }
            })
        )
    })
}
`,
			args:        []string{"a", "b"},
			checkStdout: true,
			wantStdout:  "42:2:a",
		},
		{
			name: "readStdinChunk copies stdin",
			src: `fn copy() -> IO[Unit] {
    let! s = readStdinChunk(4)
    if s == "" {
        pure(())
    } else {
        let! _ = print("[" ++ s ++ "]")
        copy()
    }
}

fn main() {
    copy()
}
`,
			stdin:       "hello!",
			checkStdout: true,
			wantStdout:  "[hell][o!]",
		},
		{
			name: "readStdinChunk zero",
			src: `fn main() {
    bind(readStdinChunk(0), fn(s: String) -> IO[Unit] { print(s) })
}
`,
			wantExit: -1,
		},
		{
			name: "building IO does not run it",
			src: `fn main() -> Int {
    let ignored = print("no")
    7
}
`,
			invoke:      true,
			checkStdout: true,
			wantStdout:  "7\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := []byte(tt.src)
			filename := "main.cero"
			if tt.file != "" {
				filename = filepath.Join(root, tt.file)
				var err error
				src, err = os.ReadFile(filename)
				if err != nil {
					t.Fatalf("read %s: %v", tt.file, err)
				}
			}
			out, err := CompileProgram(filename, src, os.ReadFile)
			if err != nil {
				t.Fatalf("CompileProgram() error = %v", err)
			}
			if out.Command == tt.invoke {
				t.Fatalf("Command = %v, want %v", out.Command, !tt.invoke)
			}
			dir := t.TempDir()
			for name, body := range tt.files {
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			wasmPath := filepath.Join(dir, "main.wasm")
			if err := os.WriteFile(wasmPath, out.Wasm, 0o644); err != nil {
				t.Fatalf("write wasm: %v", err)
			}
			var cmd *exec.Cmd
			if tt.invoke {
				cmd = exec.Command("wasmtime", "run", "--invoke", "main", wasmPath)
			} else {
				cmdArgs := append([]string{"run", "--dir=.", wasmPath}, tt.args...)
				cmd = exec.Command("wasmtime", cmdArgs...)
				cmd.Dir = dir
				cmd.Stdin = strings.NewReader(tt.stdin)
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()
			code := 0
			if err != nil {
				exit, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("wasmtime: %v\nstderr:\n%s", err, stderr.String())
				}
				code = exit.ExitCode()
			}
			if tt.wantExit == -1 {
				if code == 0 {
					t.Errorf("exit = 0, want non-zero\nstdout: %q\nstderr: %q", stdout.String(), stderr.String())
				}
			} else if code != tt.wantExit {
				t.Errorf("exit = %d, want %d\nstdout: %q\nstderr: %q", code, tt.wantExit, stdout.String(), stderr.String())
			}
			if tt.checkStdout && stdout.String() != tt.wantStdout {
				got := stdout.String()
				if len(got) > 200 {
					got = got[:200] + "..."
				}
				want := tt.wantStdout
				if len(want) > 200 {
					want = want[:200] + "..."
				}
				t.Errorf("stdout = %q, want %q\nstderr: %q", got, want, stderr.String())
			}
			if tt.wantNewlines != 0 {
				if got := strings.Count(stdout.String(), "\n"); got != tt.wantNewlines || len(stdout.String()) != tt.wantNewlines {
					t.Errorf("newlines = %d, len = %d, want %d newlines and nothing else\nstderr: %q", got, len(stdout.String()), tt.wantNewlines, stderr.String())
				}
			}
			if tt.wantStderr != "" && stderr.String() != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr.String(), tt.wantStderr)
			}
			if tt.stderrHas != "" && !strings.Contains(stderr.String(), tt.stderrHas) {
				t.Errorf("stderr = %q, want to contain %q", stderr.String(), tt.stderrHas)
			}
			if strings.Contains(stdout.String(), "no") && tt.invoke {
				t.Errorf("stdout = %q, IO action ran", stdout.String())
			}
			if tt.wantFile != "" {
				body, err := os.ReadFile(filepath.Join(dir, tt.wantFile))
				if err != nil {
					t.Fatalf("read %s: %v", tt.wantFile, err)
				}
				if string(body) != tt.wantBody {
					t.Errorf("file %s = %q, want %q", tt.wantFile, body, tt.wantBody)
				}
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
