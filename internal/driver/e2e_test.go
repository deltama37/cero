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
