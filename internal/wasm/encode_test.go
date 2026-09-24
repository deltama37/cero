package wasm

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/deltama37/cero/internal/ir"
	"github.com/deltama37/cero/internal/lower"
	"github.com/deltama37/cero/internal/parser"
	"github.com/deltama37/cero/internal/typecheck"
)

func TestEncodeGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []byte
	}{
		{
			name: "main returns 42",
			src:  "fn main() -> Int { 42 }\n",
			want: []byte{
				0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
				0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7e,
				0x03, 0x02, 0x01, 0x00,
				0x04, 0x05, 0x01, 0x70, 0x01, 0x00, 0x00,
				0x07, 0x08, 0x01, 0x04, 0x6d, 0x61, 0x69, 0x6e, 0x00, 0x00,
				0x0a, 0x06, 0x01, 0x04, 0x00, 0x42, 0x2a, 0x0b,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Encode(lowerModule(t, tt.src))
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Encode() =\n%s\nwant:\n%s", hex.Dump(got), hex.Dump(tt.want))
			}
		})
	}
}

func TestEncodeTypeDedup(t *testing.T) {
	t.Parallel()

	intResult := ir.Sig{Result: ir.Int}
	boolToInt := ir.Sig{Params: []ir.ValType{ir.Bool}, Result: ir.Int}
	funcToInt := ir.Sig{Params: []ir.ValType{ir.FuncRef}, Result: ir.Int}

	tests := []struct {
		name         string
		m            *ir.Module
		wantTypes    int
		wantFuncSec  []byte
		wantContains []byte
	}{
		{
			name: "identical signatures share one type",
			m: &ir.Module{
				Funcs: []*ir.Func{
					{Name: "a", Sig: intResult, Body: &ir.IntConst{Value: 1}},
					{Name: "main", Sig: intResult, Body: &ir.IntConst{Value: 2}},
				},
				Main: 1,
			},
			wantTypes:   1,
			wantFuncSec: []byte{0x02, 0x00, 0x00},
		},
		{
			name: "bool and funcref parameters share one type",
			m: &ir.Module{
				Funcs: []*ir.Func{
					{
						Name:   "fromBool",
						Sig:    boolToInt,
						Locals: []ir.ValType{ir.Bool},
						Body:   &ir.IntConst{Value: 1},
					},
					{
						Name:   "fromFunc",
						Sig:    funcToInt,
						Locals: []ir.ValType{ir.FuncRef},
						Body:   &ir.IntConst{Value: 2},
					},
				},
				Main: 0,
			},
			wantTypes:   1,
			wantFuncSec: []byte{0x02, 0x00, 0x00},
		},
		{
			name: "call_indirect signature matches a bool parameter",
			m: &ir.Module{
				Funcs: []*ir.Func{
					{
						Name:   "fromBool",
						Sig:    boolToInt,
						Locals: []ir.ValType{ir.Bool},
						Body:   &ir.IntConst{Value: 0},
					},
					{
						Name: "main",
						Sig:  intResult,
						Body: &ir.CallIndirect{
							Callee: &ir.FuncValue{Func: 0},
							Sig:    funcToInt,
							Args:   []ir.Expr{&ir.FuncValue{Func: 0}},
						},
					},
				},
				Table: []ir.FuncID{0},
				Main:  1,
			},
			wantTypes: 2,
			// i32.const 0, i32.const 0, call_indirect type 0 table 0.
			// Type 0 is (i32)->i64; a missed dedup would use type 2.
			wantContains: []byte{0x41, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Encode(tt.m)
			secs := moduleSections(t, got)
			if n := vecCount(t, secs[secType]); n != tt.wantTypes {
				t.Errorf("type count = %d, want %d", n, tt.wantTypes)
			}
			if tt.wantFuncSec != nil && !bytes.Equal(secs[secFunction], tt.wantFuncSec) {
				t.Errorf("function section = %x, want %x", secs[secFunction], tt.wantFuncSec)
			}
			if tt.wantContains != nil && !bytes.Contains(got, tt.wantContains) {
				t.Errorf("module missing %x\n%s", tt.wantContains, hex.Dump(got))
			}
		})
	}
}

func TestEncodeElementSection(t *testing.T) {
	t.Parallel()

	mainFn := func() *ir.Func {
		return &ir.Func{
			Name: "main",
			Sig:  ir.Sig{Result: ir.Int},
			Body: &ir.IntConst{Value: 1},
		}
	}

	tests := []struct {
		name        string
		m           *ir.Module
		wantPayload []byte
		wantPresent bool
	}{
		{
			name: "empty table omits the element section",
			m: &ir.Module{
				Funcs: []*ir.Func{mainFn()},
				Main:  0,
			},
		},
		{
			name: "non-empty table emits table order",
			m: &ir.Module{
				Funcs: []*ir.Func{mainFn(), mainFn(), mainFn()},
				Table: []ir.FuncID{2, 0},
				Main:  0,
			},
			wantPresent: true,
			wantPayload: []byte{0x01, 0x00, 0x41, 0x00, 0x0b, 0x02, 0x02, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			secs := moduleSections(t, Encode(tt.m))
			payload, ok := secs[secElement]
			if ok != tt.wantPresent {
				t.Fatalf("element section present = %v, want %v", ok, tt.wantPresent)
			}
			if tt.wantPresent && !bytes.Equal(payload, tt.wantPayload) {
				t.Errorf("element payload = %x, want %x", payload, tt.wantPayload)
			}
		})
	}
}

func TestEncodeLocalGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    *ir.Module
		want []byte
	}{
		{
			name: "Int Int Bool Int",
			m: &ir.Module{
				Funcs: []*ir.Func{{
					Name: "main",
					Sig:  ir.Sig{Result: ir.Int},
					Locals: []ir.ValType{
						ir.Int,
						ir.Int,
						ir.Bool,
						ir.Int,
					},
					Body: &ir.IntConst{Value: 0},
				}},
				Main: 0,
			},
			want: []byte{0x03, 0x02, 0x7e, 0x01, 0x7f, 0x01, 0x7e},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bodies := functionBodies(t, Encode(tt.m))
			if len(bodies) != 1 {
				t.Fatalf("bodies = %d, want 1", len(bodies))
			}
			if !bytes.HasPrefix(bodies[0], tt.want) {
				t.Errorf("locals = %x, want prefix %x", bodies[0], tt.want)
			}
		})
	}
}

func TestEncodePanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body ir.Expr
	}{
		{
			name: "funcref equality",
			body: &ir.Binary{
				Op: ir.Eq,
				X:  &ir.LocalGet{Local: 0, T: ir.FuncRef},
				Y:  &ir.LocalGet{Local: 0, T: ir.FuncRef},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &ir.Module{
				Funcs: []*ir.Func{{
					Name: "main",
					Sig:  ir.Sig{Result: ir.Bool},
					Body: tt.body,
				}},
			}
			defer func() {
				if recover() == nil {
					t.Fatal("Encode did not panic")
				}
			}()
			Encode(m)
		})
	}
}

func TestEncodeMemory(t *testing.T) {
	t.Parallel()

	const (
		nullary = `type U = U

fn main() -> Int {
    match U { U => 1 }
}
`
		withTable = `type U = U

fn id(n: Int) -> Int {
    n
}

fn main() -> Int {
    let f = id
    match U { U => f(1) }
}
`
		noData = `fn f(n: Int) -> Int {
    n
}

fn main() -> Int {
    f(1)
}
`
	)

	tests := []struct {
		name       string
		src        string
		wantMemory bool
		wantIDs    []byte
		wantFuncs  int // 0 means equal to the number of IR functions
	}{
		{
			name:       "v0.1 module has no memory",
			src:        "fn main() -> Int { 42 }\n",
			wantMemory: false,
			wantIDs:    []byte{secType, secFunction, secTable, secExport, secCode},
			wantFuncs:  1,
		},
		{
			name:       "function count matches IR without memory",
			src:        noData,
			wantMemory: false,
			wantIDs:    []byte{secType, secFunction, secTable, secExport, secCode},
			wantFuncs:  2,
		},
		{
			name:       "data module section order",
			src:        nullary,
			wantMemory: true,
			wantIDs:    []byte{secType, secFunction, secTable, secMemory, secGlobal, secExport, secCode},
		},
		{
			name:       "data module with a table",
			src:        withTable,
			wantMemory: true,
			wantIDs:    []byte{secType, secFunction, secTable, secMemory, secGlobal, secExport, secElement, secCode},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := lowerModule(t, tt.src)
			if got := usesMemory(m); got != tt.wantMemory {
				t.Fatalf("usesMemory() = %v, want %v", got, tt.wantMemory)
			}
			wasm := Encode(m)
			if got := sectionIDs(t, wasm); !bytes.Equal(got, tt.wantIDs) {
				t.Errorf("section ids = %v, want %v", got, tt.wantIDs)
			}
			secs := moduleSections(t, wasm)
			_, hasMem := secs[secMemory]
			_, hasGlobal := secs[secGlobal]
			if hasMem != tt.wantMemory || hasGlobal != tt.wantMemory {
				t.Errorf("memory present = %v, global present = %v, want %v", hasMem, hasGlobal, tt.wantMemory)
			}
			if tt.wantMemory {
				if !bytes.Contains(wasm, []byte{0x05, 0x06, 0x01, 0x01, 0x01, 0x80, 0x80, 0x02}) {
					t.Errorf("missing memory section bytes\n%s", hex.Dump(wasm))
				}
				if !bytes.Contains(wasm, []byte{0x06, 0x06, 0x01, 0x7f, 0x01, 0x41, 0x08, 0x0b}) {
					t.Errorf("missing global section bytes\n%s", hex.Dump(wasm))
				}
				if !bytes.Equal(secs[secMemory], []byte{0x01, 0x01, 0x01, 0x80, 0x80, 0x02}) {
					t.Errorf("memory payload = %x", secs[secMemory])
				}
				if !bytes.Equal(secs[secGlobal], []byte{0x01, 0x7f, 0x01, 0x41, 0x08, 0x0b}) {
					t.Errorf("global payload = %x", secs[secGlobal])
				}
			}
			wantFuncs := tt.wantFuncs
			if wantFuncs == 0 {
				wantFuncs = len(m.Funcs)
			}
			if !tt.wantMemory && len(functionBodies(t, wasm)) != wantFuncs {
				t.Errorf("function bodies = %d, want %d", len(functionBodies(t, wasm)), wantFuncs)
			}
			if !tt.wantMemory && vecCount(t, secs[secFunction]) != len(m.Funcs) {
				t.Errorf("function section count = %d, want %d", vecCount(t, secs[secFunction]), len(m.Funcs))
			}
		})
	}
}

func TestEncodeAlloc(t *testing.T) {
	t.Parallel()

	want := []byte{
		0x23, 0x00,
		0x21, 0x01,
		0x23, 0x00,
		0x20, 0x00,
		0x6a,
		0x24, 0x00,
		0x23, 0x00,
		0x3f, 0x00,
		0x41, 0x10,
		0x74,
		0x4b,
		0x04, 0x40,
		0x23, 0x00,
		0x3f, 0x00,
		0x41, 0x10,
		0x74,
		0x6b,
		0x41, 0xff, 0xff, 0x03,
		0x6a,
		0x41, 0x10,
		0x76,
		0x40, 0x00,
		0x41, 0x7f,
		0x46,
		0x04, 0x40,
		0x00,
		0x0b,
		0x0b,
		0x20, 0x01,
		0x0b,
	}

	tests := []struct {
		name string
		src  string
	}{
		{
			name: "alloc body",
			src: `type U = U

fn main() -> Int {
    match U { U => 1 }
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := lowerModule(t, tt.src)
			bodies := functionBodies(t, Encode(m))
			alloc := bodies[len(m.Funcs)]
			prefix := []byte{0x01, 0x01, 0x7f}
			if !bytes.HasPrefix(alloc, prefix) {
				t.Fatalf("alloc locals = %x, want prefix %x", alloc, prefix)
			}
			if got := alloc[len(prefix):]; !bytes.Equal(got, want) {
				t.Errorf("alloc instructions =\n%x\nwant:\n%x", got, want)
			}
		})
	}
}

func TestEncodeNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		src       string
		wantNews  int
		bodyIndex int // index among new helpers; -1 to skip the byte check
		allocIdx  byte
		wantInst  []byte
	}{
		{
			name: "no fields",
			src: `type U = U

fn main() -> Int {
    match U { U => 1 }
}
`,
			wantNews:  1,
			bodyIndex: 0,
			allocIdx:  1,
			wantInst: []byte{
				0x41, 0x08,
				0x10, 0x01,
				0x21, 0x01,
				0x20, 0x01, 0x20, 0x00, 0x36, 0x02, 0x00,
				0x20, 0x01,
				0x0b,
			},
		},
		{
			name: "Int and Ptr fields",
			src: `type List =
    | Nil
    | Cons(Int, List)

fn main() -> Int {
    match Cons(1, Nil) {
        Nil => 0,
        Cons(_, _) => 1,
    }
}
`,
			wantNews:  2,
			bodyIndex: 0,
			allocIdx:  1,
			wantInst: []byte{
				0x41, 0x18,
				0x10, 0x01,
				0x21, 0x03,
				0x20, 0x03, 0x20, 0x00, 0x36, 0x02, 0x00,
				0x20, 0x03, 0x20, 0x01, 0x37, 0x03, 0x08,
				0x20, 0x03, 0x20, 0x02, 0x36, 0x02, 0x10,
				0x20, 0x03,
				0x0b,
			},
		},
		{
			name: "A(Int) and B(Int) share one new",
			src: `type T =
    | A(Int)
    | B(Int)

fn main() -> Int {
    match A(1) {
        A(_) => match B(2) {
            B(_) => 1,
            A(_) => 0,
        },
        B(_) => 2,
    }
}
`,
			wantNews:  1,
			bodyIndex: -1,
		},
		{
			name: "A(Int) and C(Bool) get two new functions",
			src: `type T =
    | A(Int)
    | C(Bool)

fn main() -> Int {
    match A(1) {
        A(_) => match C(true) {
            C(_) => 1,
            A(_) => 0,
        },
        C(_) => 2,
    }
}
`,
			wantNews:  1 + 1,
			bodyIndex: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := lowerModule(t, tt.src)
			bodies := functionBodies(t, Encode(m))
			if got, want := len(bodies), len(m.Funcs)+1+tt.wantNews; got != want {
				t.Fatalf("function bodies = %d, want %d (IR + alloc + new)", got, want)
			}
			if tt.bodyIndex < 0 {
				return
			}
			body := bodies[len(m.Funcs)+1+tt.bodyIndex]
			prefix := []byte{0x01, 0x01, 0x7f}
			if !bytes.HasPrefix(body, prefix) {
				t.Fatalf("new locals = %x, want prefix %x", body, prefix)
			}
			got := body[len(prefix):]
			if !bytes.Equal(got, tt.wantInst) {
				t.Errorf("new instructions =\n%x\nwant:\n%x", got, tt.wantInst)
			}
			if got[3] != tt.allocIdx {
				t.Errorf("alloc index = %d, want %d", got[3], tt.allocIdx)
			}
		})
	}
}

func TestEncodeExportMain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		wantIdx byte
	}{
		{
			name: "main stays at its IR index",
			src: `type U = U

fn id(x: U) -> U {
    x
}

fn main() -> Int {
    match id(U) { U => 4 }
}
`,
			wantIdx: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := lowerModule(t, tt.src)
			if m.Main != ir.FuncID(tt.wantIdx) {
				t.Fatalf("IR main = %d, want %d", m.Main, tt.wantIdx)
			}
			payload := moduleSections(t, Encode(m))[secExport]
			want := []byte{0x01, 0x04, 'm', 'a', 'i', 'n', 0x00, tt.wantIdx}
			if !bytes.Equal(payload, want) {
				t.Errorf("export = %x, want %x", payload, want)
			}
		})
	}
}

func lowerModule(t *testing.T, src string) *ir.Module {
	t.Helper()

	file, err := parser.ParseFile([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info, err := typecheck.Check(file)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	return lower.Lower(file, info)
}

func moduleSections(t *testing.T, wasm []byte) map[byte][]byte {
	t.Helper()

	if !bytes.HasPrefix(wasm, wasmHeader) {
		t.Fatalf("missing wasm header:\n%s", hex.Dump(wasm))
	}
	rest := wasm[len(wasmHeader):]
	out := make(map[byte][]byte)
	for len(rest) > 0 {
		id := rest[0]
		rest = rest[1:]
		n, width, ok := readUleb(rest)
		if !ok || uint64(len(rest)) < uint64(width)+n {
			t.Fatalf("truncated section %d", id)
		}
		rest = rest[width:]
		out[id] = rest[:n]
		rest = rest[n:]
	}
	return out
}

func functionBodies(t *testing.T, wasm []byte) [][]byte {
	t.Helper()

	payload := moduleSections(t, wasm)[secCode]
	count, width, ok := readUleb(payload)
	if !ok {
		t.Fatal("truncated code section")
	}
	rest := payload[width:]
	bodies := make([][]byte, 0, count)
	for i := uint64(0); i < count; i++ {
		n, w, ok := readUleb(rest)
		if !ok || uint64(len(rest)) < uint64(w)+n {
			t.Fatalf("truncated function body %d", i)
		}
		rest = rest[w:]
		bodies = append(bodies, rest[:n])
		rest = rest[n:]
	}
	return bodies
}

func sectionIDs(t *testing.T, wasm []byte) []byte {
	t.Helper()

	if !bytes.HasPrefix(wasm, wasmHeader) {
		t.Fatalf("missing wasm header:\n%s", hex.Dump(wasm))
	}
	rest := wasm[len(wasmHeader):]
	var ids []byte
	for len(rest) > 0 {
		id := rest[0]
		ids = append(ids, id)
		rest = rest[1:]
		n, width, ok := readUleb(rest)
		if !ok || uint64(len(rest)) < uint64(width)+n {
			t.Fatalf("truncated section %d", id)
		}
		rest = rest[width+int(n):]
	}
	return ids
}

func vecCount(t *testing.T, payload []byte) int {
	t.Helper()

	n, _, ok := readUleb(payload)
	if !ok {
		t.Fatal("truncated vector")
	}
	return int(n)
}

func readUleb(b []byte) (uint64, int, bool) {
	var v uint64
	var shift uint
	for i, c := range b {
		if shift > 63 {
			return 0, 0, false
		}
		v |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return v, i + 1, true
		}
		shift += 7
	}
	return 0, 0, false
}
