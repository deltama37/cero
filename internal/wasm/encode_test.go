package wasm

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
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
			name: "funcref parameter is i64 and does not share a type with bool",
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
			wantTypes:   2,
			wantFuncSec: []byte{0x02, 0x00, 0x01},
		},
		{
			name: "call_indirect shares a type with the table function",
			m: &ir.Module{
				Funcs: []*ir.Func{
					{
						Name:   "f",
						Sig:    ir.Sig{Params: []ir.ValType{ir.Int, ir.Ptr}, Result: ir.Int},
						Locals: []ir.ValType{ir.Int, ir.Ptr},
						Body:   &ir.LocalGet{Local: 0, T: ir.Int},
					},
					{
						Name: "main",
						Sig:  intResult,
						Body: &ir.CallIndirect{
							Callee: &ir.FuncValue{Func: 0},
							Sig:    ir.Sig{Params: []ir.ValType{ir.Int}, Result: ir.Int},
							Args:   []ir.Expr{&ir.IntConst{Value: 1}},
						},
					},
				},
				Table: []ir.FuncID{0},
				Main:  1,
			},
			wantTypes: 2,
			// i64.const 1, i64.const 0, local.tee 0, unpack, call_indirect type 0.
			// A missed share would use type 2.
			wantContains: []byte{
				0x42, 0x01, 0x42, 0x00, 0x22, 0x00, 0x42, 0x20, 0x88, 0xa7, 0x20, 0x00, 0xa7, 0x11, 0x00, 0x00,
			},
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

func TestEncodeTailCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  ir.Expr
		table []ir.FuncID
		want  string
	}{
		{
			name: "direct tail call",
			body: &ir.Call{
				Func: 0,
				Args: []ir.Expr{&ir.IntConst{Value: 7}},
				T:    ir.Int,
				Tail: true,
			},
			want: "00 42 07 12 00 0b",
		},
		{
			name: "direct call not in tail position",
			body: &ir.Call{
				Func: 0,
				Args: []ir.Expr{&ir.IntConst{Value: 7}},
				T:    ir.Int,
			},
			want: "00 42 07 10 00 0b",
		},
		{
			name: "indirect tail call",
			body: &ir.CallIndirect{
				Callee: &ir.FuncValue{Func: 0},
				Sig:    ir.Sig{Params: []ir.ValType{ir.Int}, Result: ir.Int},
				Args:   []ir.Expr{&ir.IntConst{Value: 7}},
				Tail:   true,
			},
			table: []ir.FuncID{0},
			want:  "01 01 7e 42 07 42 00 22 00 42 20 88 a7 20 00 a7 13 02 00 0b",
		},
		{
			name: "indirect call not in tail position",
			body: &ir.CallIndirect{
				Callee: &ir.FuncValue{Func: 0},
				Sig:    ir.Sig{Params: []ir.ValType{ir.Int}, Result: ir.Int},
				Args:   []ir.Expr{&ir.IntConst{Value: 7}},
			},
			table: []ir.FuncID{0},
			want:  "01 01 7e 42 07 42 00 22 00 42 20 88 a7 20 00 a7 11 02 00 0b",
		},
		{
			name: "tail call inside if",
			body: &ir.If{
				Cond: &ir.BoolConst{Value: true},
				Then: &ir.Call{
					Func: 0,
					Args: []ir.Expr{&ir.IntConst{Value: 1}},
					T:    ir.Int,
					Tail: true,
				},
				Else: &ir.IntConst{Value: 0},
				T:    ir.Int,
			},
			want: "00 41 01 04 7e 42 01 12 00 05 42 00 0b 0b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &ir.Module{
				Funcs: []*ir.Func{
					{
						Name:   "f",
						Sig:    ir.Sig{Params: []ir.ValType{ir.Int}, Result: ir.Int},
						Locals: []ir.ValType{ir.Int},
						Body:   &ir.LocalGet{Local: 0, T: ir.Int},
					},
					{
						Name: "main",
						Sig:  ir.Sig{Result: ir.Int},
						Body: tt.body,
					},
				},
				Table: tt.table,
				Main:  1,
			}
			bodies := functionBodies(t, Encode(m))
			got := bodies[1]
			want, err := hex.DecodeString(strings.ReplaceAll(tt.want, " ", ""))
			if err != nil {
				t.Fatalf("decode want: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("main body = %x, want %x", got, want)
			}
		})
	}
}

func TestEncodeFuncRefIsI64(t *testing.T) {
	t.Parallel()

	m := &ir.Module{
		Funcs: []*ir.Func{{
			Name: "main",
			Sig:  ir.Sig{Params: []ir.ValType{ir.FuncRef}, Result: ir.FuncRef},
			Locals: []ir.ValType{
				ir.FuncRef,
				ir.FuncRef,
			},
			Body: &ir.LocalGet{Local: 1, T: ir.FuncRef},
		}},
		Main: 0,
	}
	wasm := Encode(m)
	secs := moduleSections(t, wasm)
	wantType := []byte{0x01, 0x60, 0x01, 0x7e, 0x01, 0x7e}
	if !bytes.Equal(secs[secType], wantType) {
		t.Errorf("type section = %x, want %x", secs[secType], wantType)
	}
	bodies := functionBodies(t, wasm)
	wantBody := []byte{0x01, 0x01, 0x7e, 0x20, 0x01, 0x0b}
	if !bytes.Equal(bodies[0], wantBody) {
		t.Errorf("body = %x, want %x", bodies[0], wantBody)
	}
}

func TestEncodeFuncValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		sig    ir.Sig
		locals []ir.ValType
		body   ir.Expr
		want   string
	}{
		{
			name: "without an environment",
			sig:  ir.Sig{Result: ir.FuncRef},
			body: &ir.FuncValue{Func: 0},
			want: "00 42 00 0b",
		},
		{
			name:   "with an environment",
			sig:    ir.Sig{Params: []ir.ValType{ir.Ptr}, Result: ir.FuncRef},
			locals: []ir.ValType{ir.Ptr},
			body: &ir.FuncValue{
				Func: 0,
				Env:  &ir.LocalGet{Local: 0, T: ir.Ptr},
			},
			want: "00 20 00 ad 42 20 86 42 00 84 0b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &ir.Module{
				Funcs: []*ir.Func{
					{
						Name: "f",
						Sig:  ir.Sig{Result: ir.Int},
						Body: &ir.IntConst{Value: 0},
					},
					{
						Name:   "main",
						Sig:    tt.sig,
						Locals: tt.locals,
						Body:   tt.body,
					},
				},
				Table: []ir.FuncID{0},
				Main:  1,
			}
			bodies := functionBodies(t, Encode(m))
			want, err := hex.DecodeString(strings.ReplaceAll(tt.want, " ", ""))
			if err != nil {
				t.Fatalf("decode want: %v", err)
			}
			if !bytes.Equal(bodies[1], want) {
				t.Errorf("main body = %x, want %x", bodies[1], want)
			}
		})
	}
}

func TestEncodeFuncRefField(t *testing.T) {
	t.Parallel()

	m := &ir.Module{
		Funcs: []*ir.Func{
			{
				Name:   "id",
				Sig:    ir.Sig{Params: []ir.ValType{ir.Int}, Result: ir.Int},
				Locals: []ir.ValType{ir.Int},
				Body:   &ir.LocalGet{Local: 0, T: ir.Int},
			},
			{
				Name:   "main",
				Sig:    ir.Sig{Result: ir.FuncRef},
				Locals: []ir.ValType{ir.Ptr},
				Body: &ir.Block{
					Lets: []*ir.Let{{
						Local: 0,
						Value: &ir.Construct{
							Tag:    0,
							Fields: []ir.Expr{&ir.FuncValue{Func: 0}},
						},
					}},
					Result: &ir.Field{Local: 0, Index: 0, T: ir.FuncRef},
				},
			},
		},
		Table: []ir.FuncID{0},
		Main:  1,
	}
	wasm := Encode(m)
	bodies := functionBodies(t, wasm)
	newBody := bodies[len(m.Funcs)+1]
	store := []byte{0x20, 0x02, 0x20, 0x01, 0x37, 0x03, 0x08}
	if !bytes.Contains(newBody, store) {
		t.Errorf("new body missing i64.store %x\n%x", store, newBody)
	}
	load := []byte{0x20, 0x00, 0x29, 0x03, 0x08}
	if !bytes.Contains(bodies[1], load) {
		t.Errorf("main body missing i64.load %x\n%x", load, bodies[1])
	}
}

func TestEncodePanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body ir.Expr
		want string
	}{
		{
			name: "funcref equality",
			body: &ir.Binary{
				Op: ir.Eq,
				X:  &ir.LocalGet{Local: 0, T: ir.FuncRef},
				Y:  &ir.LocalGet{Local: 0, T: ir.FuncRef},
			},
			want: "wasm: unexpected binary operand type FuncRef",
		},
		{
			name: "tail call result differs from function result",
			body: &ir.Call{Func: 0, T: ir.Int, Tail: true},
			want: "wasm: tail call returns Int, function returns Bool",
		},
		{
			name: "tail call indirect result differs from function result",
			body: &ir.CallIndirect{
				Callee: &ir.LocalGet{Local: 0, T: ir.FuncRef},
				Sig:    ir.Sig{Result: ir.Int},
				Tail:   true,
			},
			want: "wasm: tail call returns Int, function returns Bool",
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
				r := recover()
				if r == nil {
					t.Fatal("Encode did not panic")
				}
				msg := fmt.Sprint(r)
				if !strings.Contains(msg, tt.want) {
					t.Fatalf("panic = %q, want it to contain %q", msg, tt.want)
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

func TestEncodeString(t *testing.T) {
	t.Parallel()

	t.Run("layout, dedup, and inline length", func(t *testing.T) {
		t.Parallel()

		m := lowerModule(t, `fn main() -> Int {
    stringLength("ab") + stringLength("c") + stringLength("ab")
}
`)
		wasm := Encode(m)
		if got, want := sectionIDs(t, wasm), []byte{
			secType, secFunction, secTable, secMemory, secGlobal, secExport, secCode, secData,
		}; !bytes.Equal(got, want) {
			t.Fatalf("section ids = %v, want %v", got, want)
		}
		secs := moduleSections(t, wasm)
		wantData := []byte{0x01, 0x00, opI32Const, 0x08, opEnd}
		wantData = appendUleb128(wantData, 32)
		wantData = append(wantData,
			0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			'a', 'b', 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			'c', 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		)
		if !bytes.Equal(secs[secData], wantData) {
			t.Errorf("data = %x\nwant %x", secs[secData], wantData)
		}
		if got := globalHeap(t, secs[secGlobal]); got != 40 {
			t.Errorf("heap = %d, want 40", got)
		}
		if got := memoryMin(t, secs[secMemory]); got != 1 {
			t.Errorf("min pages = %d, want 1", got)
		}
		wantBody := []byte{
			0x00,
			opI32Const, 0x08, opI32Load, alignI32, 0x00, opI64ExtendI32U,
			opI32Const, 0x18, opI32Load, alignI32, 0x00, opI64ExtendI32U,
			opI64Add,
			opI32Const, 0x08, opI32Load, alignI32, 0x00, opI64ExtendI32U,
			opI64Add,
			opEnd,
		}
		if got := functionBodies(t, wasm)[0]; !bytes.Equal(got, wantBody) {
			t.Errorf("main =\n%x\nwant:\n%x", got, wantBody)
		}
		if got, want := len(functionBodies(t, wasm)), len(m.Funcs)+1; got != want {
			t.Errorf("function bodies = %d, want %d (no string helper)", got, want)
		}
	})

	helpers := []struct {
		name string
		src  string
		want []int
		data bool
		heap int64
	}{
		{
			name: "length only",
			src:  "fn main() -> Int { stringLength(\"hi\") }\n",
			want: []int{0, 1},
			data: true,
			heap: 24,
		},
		{
			name: "byte_at does not pull str_alloc",
			src:  "fn main() -> Int { stringByteAt(\"A\", 0) }\n",
			want: []int{0, 1, 2},
			data: true,
			heap: 24,
		},
		{
			name: "eq does not pull str_alloc",
			src:  "fn main() -> Int { if \"a\" == \"b\" { 1 } else { 0 } }\n",
			want: []int{0, 1, 2},
			data: true,
			heap: 40,
		},
		{
			name: "compare does not pull str_alloc",
			src:  "fn main() -> Int { stringCompare(\"a\", \"b\") }\n",
			want: []int{0, 1, 2},
			data: true,
			heap: 40,
		},
		{
			name: "concat pulls str_alloc",
			src:  "fn main() -> Int { stringLength(\"a\" ++ \"b\") }\n",
			want: []int{0, 1, 1, 2},
			data: true,
			heap: 40,
		},
		{
			name: "intToString without a literal",
			src:  "fn main() -> Int { stringLength(intToString(0)) }\n",
			want: []int{0, 1, 1, 2},
			data: false,
			heap: 8,
		},
		{
			name: "every helper, shared type indices",
			src: `fn main() -> Int {
    let s = "a" ++ "b"
    let n = stringLength(s) + stringByteAt(s, 0) + stringCompare(s, "ab")
    let t = stringSlice(s, 0, 1)
    let u = stringFromByte(65)
    let v = intToString(n)
    if t == u { stringLength(v) } else { 0 }
}
`,
			want: []int{0, 1, 1, 2, 3, 4, 5, 4, 6, 6},
			data: true,
			heap: 8 + 16 + 16 + 16,
		},
	}

	for _, tt := range helpers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wasm := Encode(lowerModule(t, tt.src))
			secs := moduleSections(t, wasm)
			if got := funcTypeIndices(t, secs[secFunction]); !intSlicesEqual(got, tt.want) {
				t.Errorf("function types = %v, want %v", got, tt.want)
			}
			_, hasData := secs[secData]
			if hasData != tt.data {
				t.Errorf("data section present = %v, want %v", hasData, tt.data)
			}
			if got := globalHeap(t, secs[secGlobal]); got != tt.heap {
				t.Errorf("heap = %d, want %d", got, tt.heap)
			}
			if tt.name == "every helper, shared type indices" {
				wantTypes := []byte{
					0x07,
					typeFunc, 0x00, 0x01, valI64,
					typeFunc, 0x01, valI32, 0x01, valI32,
					typeFunc, 0x02, valI32, valI64, 0x01, valI64,
					typeFunc, 0x03, valI32, valI64, valI64, 0x01, valI32,
					typeFunc, 0x01, valI64, 0x01, valI32,
					typeFunc, 0x02, valI32, valI32, 0x01, valI64,
					typeFunc, 0x02, valI32, valI32, 0x01, valI32,
				}
				if !bytes.Equal(secs[secType], wantTypes) {
					t.Errorf("types =\n%x\nwant:\n%x", secs[secType], wantTypes)
				}
			}
		})
	}

	t.Run("literal larger than 64KiB", func(t *testing.T) {
		t.Parallel()

		const n = 65537
		src := "fn main() -> Int { stringLength(\"" + strings.Repeat("a", n) + "\") }\n"
		wasm := Encode(lowerModule(t, src))
		secs := moduleSections(t, wasm)
		size := (n + 15) &^ 7
		heap := int64(8 + size)
		pages := int((heap + 65535) / 65536)
		if pages < 2 {
			t.Fatalf("pages = %d, want at least 2", pages)
		}
		if got := globalHeap(t, secs[secGlobal]); got != heap {
			t.Errorf("heap = %d, want %d", got, heap)
		}
		if got := memoryMin(t, secs[secMemory]); got != pages {
			t.Errorf("min pages = %d, want %d", got, pages)
		}
		if _, ok := secs[secData]; !ok {
			t.Fatal("missing data section")
		}
	})

	t.Run("string helpers follow new", func(t *testing.T) {
		t.Parallel()

		wasm := Encode(lowerModule(t, `type Box = Box(Int)

fn main() -> Int {
    match Box(1) {
        Box(_) => stringLength("a" ++ "b"),
    }
}
`))
		got := funcTypeIndices(t, moduleSections(t, wasm)[secFunction])
		want := []int{0, 1, 2, 1, 3}
		if !intSlicesEqual(got, want) {
			t.Errorf("function types = %v, want %v", got, want)
		}
	})
}

func funcTypeIndices(t *testing.T, payload []byte) []int {
	t.Helper()

	n, width, ok := readUleb(payload)
	if !ok {
		t.Fatal("truncated function section")
	}
	rest := payload[width:]
	out := make([]int, 0, n)
	for i := uint64(0); i < n; i++ {
		v, w, ok := readUleb(rest)
		if !ok {
			t.Fatalf("truncated type index %d", i)
		}
		rest = rest[w:]
		out = append(out, int(v))
	}
	if len(rest) != 0 {
		t.Fatalf("function section has %d trailing bytes", len(rest))
	}
	return out
}

func globalHeap(t *testing.T, payload []byte) int64 {
	t.Helper()

	if len(payload) < 6 || payload[0] != 0x01 || payload[1] != valI32 || payload[2] != 0x01 || payload[3] != opI32Const {
		t.Fatalf("global = %x", payload)
	}
	v, width, ok := readSleb(payload[4:])
	if !ok || 4+width >= len(payload) || payload[4+width] != opEnd {
		t.Fatalf("global init = %x", payload)
	}
	return v
}

func memoryMin(t *testing.T, payload []byte) int {
	t.Helper()

	if len(payload) < 3 || payload[0] != 0x01 || payload[1] != limitsMinMax {
		t.Fatalf("memory = %x", payload)
	}
	n, _, ok := readUleb(payload[2:])
	if !ok {
		t.Fatalf("memory min = %x", payload)
	}
	return int(n)
}

func readSleb(b []byte) (int64, int, bool) {
	var v int64
	var shift uint
	for i, c := range b {
		if shift > 63 {
			return 0, 0, false
		}
		v |= int64(c&0x7f) << shift
		shift += 7
		if c&0x80 == 0 {
			if shift < 64 && c&0x40 != 0 {
				v |= -1 << shift
			}
			return v, i + 1, true
		}
	}
	return 0, 0, false
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

func TestEncodeIO(t *testing.T) {
	t.Parallel()

	t.Run("module without IO is unchanged", func(t *testing.T) {
		t.Parallel()

		got := Encode(lowerModule(t, "fn main() -> Int { 42 }\n"))
		want := []byte{
			0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
			0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7e,
			0x03, 0x02, 0x01, 0x00,
			0x04, 0x05, 0x01, 0x70, 0x01, 0x00, 0x00,
			0x07, 0x08, 0x01, 0x04, 0x6d, 0x61, 0x69, 0x6e, 0x00, 0x00,
			0x0a, 0x06, 0x01, 0x04, 0x00, 0x42, 0x2a, 0x0b,
		}
		if !bytes.Equal(got, want) {
			t.Errorf("Encode() =\n%s\nwant:\n%s", hex.Dump(got), hex.Dump(want))
		}
		if _, ok := moduleSections(t, got)[secImport]; ok {
			t.Error("import section present")
		}
	})

	t.Run("import section lists the WASI functions", func(t *testing.T) {
		t.Parallel()

		wasm := Encode(lowerModule(t, "fn main() -> IO[Unit] { print(\"x\") }\n"))
		secs := moduleSections(t, wasm)
		got := importFuncs(t, secs[secImport])
		wantNames := []string{
			"fd_write", "fd_read", "path_open", "fd_close",
			"args_sizes_get", "args_get", "proc_exit",
		}
		if len(got) != len(wantNames) {
			t.Fatalf("imports = %d, want %d", len(got), len(wantNames))
		}
		types := funcTypes(t, secs[secType])
		for i, name := range wantNames {
			if got[i].module != "wasi_snapshot_preview1" || got[i].name != name {
				t.Errorf("import %d = %s.%s, want wasi_snapshot_preview1.%s", i, got[i].module, got[i].name, name)
			}
			if got[i].typeIndex < 0 || got[i].typeIndex >= len(types) {
				t.Fatalf("import %s type index %d out of range", name, got[i].typeIndex)
			}
			if !funcTypeEqual(types[got[i].typeIndex], wasiFuncTypes[i]) {
				t.Errorf("import %s type = %+v, want %+v", name, types[got[i].typeIndex], wasiFuncTypes[i])
			}
		}
	})

	t.Run("calls, elements and main shift by the import count", func(t *testing.T) {
		t.Parallel()

		m := lowerModule(t, `fn f() -> Int { 1 }

fn main() -> IO[Unit] {
    let n = f()
    print("x")
}
`)
		wasm := Encode(m)
		secs := moduleSections(t, wasm)
		bodies := functionBodies(t, wasm)
		main := bodies[int(m.Main)]
		// call f is call of IR function 0, which lives at index 7.
		if !bytes.Contains(main, []byte{opCall, byte(int(0) + wasiImportCount)}) {
			t.Errorf("main = %x, want a call of function %d", main, wasiImportCount)
		}
		gotElems := elementFuncIndices(t, secs[secElement])
		wantElems := make([]int, len(m.Table))
		for i, id := range m.Table {
			wantElems[i] = int(id) + wasiImportCount
		}
		if !intSlicesEqual(gotElems, wantElems) {
			t.Errorf("element functions = %v, want %v", gotElems, wantElems)
		}
		ex := exportMap(t, secs[secExport])
		if _, ok := ex["main"]; ok {
			t.Error("command exports main")
		}
		start, ok := ex["_start"]
		if !ok || start.kind != externalFunc {
			t.Fatalf("export _start = %+v, missing function export", start)
		}
		mem, ok := ex["memory"]
		if !ok || mem.kind != 0x02 || mem.index != 0 {
			t.Fatalf("export memory = %+v, want memory 0", mem)
		}
		if _, ok := secs[secMemory]; !ok {
			t.Error("command has no memory section")
		}
	})

	t.Run("non-command IO exports main and imports WASI", func(t *testing.T) {
		t.Parallel()

		m := lowerModule(t, `fn main() -> Int {
    let x = print("x")
    0
}
`)
		if m.Command {
			t.Fatal("Command = true, want false")
		}
		wasm := Encode(m)
		secs := moduleSections(t, wasm)
		if _, ok := secs[secImport]; !ok {
			t.Fatal("missing import section")
		}
		if len(importFuncs(t, secs[secImport])) != wasiImportCount {
			t.Fatalf("imports = %d, want %d", len(importFuncs(t, secs[secImport])), wasiImportCount)
		}
		ex := exportMap(t, secs[secExport])
		if _, ok := ex["_start"]; ok {
			t.Error("non-command exports _start")
		}
		main, ok := ex["main"]
		if !ok || main.kind != externalFunc || main.index != int(m.Main)+wasiImportCount {
			t.Fatalf("export main = %+v, want function %d", main, int(m.Main)+wasiImportCount)
		}
	})
}

type wasmFuncType struct {
	params  []byte
	results []byte
}

var wasiFuncTypes = []wasmFuncType{
	{params: []byte{valI32, valI32, valI32, valI32}, results: []byte{valI32}},
	{params: []byte{valI32, valI32, valI32, valI32}, results: []byte{valI32}},
	{params: []byte{valI32, valI32, valI32, valI32, valI32, valI64, valI64, valI32, valI32}, results: []byte{valI32}},
	{params: []byte{valI32}, results: []byte{valI32}},
	{params: []byte{valI32, valI32}, results: []byte{valI32}},
	{params: []byte{valI32, valI32}, results: []byte{valI32}},
	{params: []byte{valI32}},
}

type wasmImport struct {
	module    string
	name      string
	typeIndex int
}

type wasmExport struct {
	kind  byte
	index int
}

func funcTypes(t *testing.T, payload []byte) []wasmFuncType {
	t.Helper()

	n, width, ok := readUleb(payload)
	if !ok {
		t.Fatal("truncated type section")
	}
	rest := payload[width:]
	out := make([]wasmFuncType, 0, n)
	for i := uint64(0); i < n; i++ {
		if len(rest) == 0 || rest[0] != typeFunc {
			t.Fatalf("type %d form = %v", i, rest)
		}
		rest = rest[1:]
		np, w, ok := readUleb(rest)
		if !ok || uint64(len(rest)) < uint64(w)+np {
			t.Fatalf("truncated type %d params", i)
		}
		rest = rest[w:]
		params := append([]byte(nil), rest[:np]...)
		rest = rest[np:]
		nr, w, ok := readUleb(rest)
		if !ok || uint64(len(rest)) < uint64(w)+nr {
			t.Fatalf("truncated type %d results", i)
		}
		rest = rest[w:]
		results := append([]byte(nil), rest[:nr]...)
		rest = rest[nr:]
		out = append(out, wasmFuncType{params: params, results: results})
	}
	return out
}

func funcTypeEqual(a, b wasmFuncType) bool {
	return bytes.Equal(a.params, b.params) && bytes.Equal(a.results, b.results)
}

func importFuncs(t *testing.T, payload []byte) []wasmImport {
	t.Helper()

	n, width, ok := readUleb(payload)
	if !ok {
		t.Fatal("truncated import section")
	}
	rest := payload[width:]
	out := make([]wasmImport, 0, n)
	for i := uint64(0); i < n; i++ {
		module, rest2 := readName(t, rest)
		name, rest2 := readName(t, rest2)
		rest = rest2
		if len(rest) == 0 || rest[0] != externalFunc {
			t.Fatalf("import %d kind = %v", i, rest)
		}
		rest = rest[1:]
		idx, w, ok := readUleb(rest)
		if !ok {
			t.Fatalf("truncated import %d type", i)
		}
		rest = rest[w:]
		out = append(out, wasmImport{module: module, name: name, typeIndex: int(idx)})
	}
	return out
}

func exportMap(t *testing.T, payload []byte) map[string]wasmExport {
	t.Helper()

	n, width, ok := readUleb(payload)
	if !ok {
		t.Fatal("truncated export section")
	}
	rest := payload[width:]
	out := make(map[string]wasmExport, n)
	for i := uint64(0); i < n; i++ {
		name, rest2 := readName(t, rest)
		rest = rest2
		if len(rest) == 0 {
			t.Fatalf("truncated export %d", i)
		}
		kind := rest[0]
		rest = rest[1:]
		idx, w, ok := readUleb(rest)
		if !ok {
			t.Fatalf("truncated export %d index", i)
		}
		rest = rest[w:]
		out[name] = wasmExport{kind: kind, index: int(idx)}
	}
	return out
}

func elementFuncIndices(t *testing.T, payload []byte) []int {
	t.Helper()

	prefix := []byte{0x01, 0x00, opI32Const, 0x00, opEnd}
	if !bytes.HasPrefix(payload, prefix) {
		t.Fatalf("element payload = %x, want prefix %x", payload, prefix)
	}
	rest := payload[len(prefix):]
	n, width, ok := readUleb(rest)
	if !ok {
		t.Fatal("truncated element functions")
	}
	rest = rest[width:]
	out := make([]int, 0, n)
	for i := uint64(0); i < n; i++ {
		v, w, ok := readUleb(rest)
		if !ok {
			t.Fatalf("truncated element function %d", i)
		}
		rest = rest[w:]
		out = append(out, int(v))
	}
	if len(rest) != 0 {
		t.Fatalf("element section has %d trailing bytes", len(rest))
	}
	return out
}

func readName(t *testing.T, b []byte) (string, []byte) {
	t.Helper()

	n, width, ok := readUleb(b)
	if !ok || uint64(len(b)) < uint64(width)+n {
		t.Fatal("truncated name")
	}
	return string(b[width : width+int(n)]), b[width+int(n):]
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
