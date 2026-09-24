// Package wasm encodes Cero IR as a WebAssembly binary module.
package wasm

import (
	"fmt"

	"github.com/deltama37/cero/internal/ir"
)

const (
	secType     byte = 1
	secFunction byte = 3
	secTable    byte = 4
	secExport   byte = 7
	secElement  byte = 9
	secCode     byte = 10

	opEnd           byte = 0x0b
	opIf            byte = 0x04
	opElse          byte = 0x05
	opCall          byte = 0x10
	opCallIndirect  byte = 0x11
	opLocalGet      byte = 0x20
	opLocalSet      byte = 0x21
	opI32Const      byte = 0x41
	opI64Const      byte = 0x42
	opI32Eqz        byte = 0x45
	opI32Eq         byte = 0x46
	opI32Ne         byte = 0x47
	opI64Eq         byte = 0x51
	opI64Ne         byte = 0x52
	opI64LtS        byte = 0x53
	opI64GtS        byte = 0x55
	opI64LeS        byte = 0x57
	opI64GeS        byte = 0x59
	opI64Add        byte = 0x7c
	opI64Sub        byte = 0x7d
	opI64Mul        byte = 0x7e
	opI64DivS       byte = 0x7f
	valI32          byte = 0x7f
	valI64          byte = 0x7e
	typeFunc        byte = 0x60
	refFunc         byte = 0x70
	limitsMinMax    byte = 0x01
	externalFunc    byte = 0x00
	elemActiveFlags byte = 0x00
)

var wasmHeader = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

// Encode returns the WebAssembly binary module for m.
func Encode(m *ir.Module) []byte {
	sigs, types := collectTypes(m)
	e := &encoder{table: m.Table, types: types}

	out := append([]byte(nil), wasmHeader...)
	out = append(out, encodeTypeSection(sigs)...)
	out = append(out, encodeFunctionSection(m, types)...)
	out = append(out, encodeTableSection(m)...)
	out = append(out, encodeExportSection(m)...)
	if len(m.Table) > 0 {
		out = append(out, encodeElementSection(m)...)
	}
	out = append(out, e.encodeCodeSection(m)...)
	return out
}

type encoder struct {
	table []ir.FuncID
	types map[string]int
	buf   []byte
}

// collectTypes assigns type indices in first-seen order.
// Function signatures are scanned first, then each body in preorder
// so that CallIndirect signatures share indices with matching functions.
// Bool and FuncRef are both i32, so they collapse to one wasm type.
func collectTypes(m *ir.Module) ([]ir.Sig, map[string]int) {
	var sigs []ir.Sig
	index := make(map[string]int)
	add := func(s ir.Sig) {
		key := sigKey(s)
		if _, ok := index[key]; ok {
			return
		}
		index[key] = len(sigs)
		sigs = append(sigs, s)
	}
	for _, fn := range m.Funcs {
		add(fn.Sig)
	}
	for _, fn := range m.Funcs {
		walk(fn.Body, func(e ir.Expr) {
			if c, ok := e.(*ir.CallIndirect); ok {
				add(c.Sig)
			}
		})
	}
	return sigs, index
}

// walk visits e in preorder (the node, then children left to right).
// CallIndirect's children are the callee and then the arguments, which is
// the left-to-right order of the IR text form.
func walk(e ir.Expr, visit func(ir.Expr)) {
	if e == nil {
		return
	}
	visit(e)
	switch e := e.(type) {
	case *ir.IntConst, *ir.BoolConst, *ir.LocalGet, *ir.FuncValue:
	case *ir.Unary:
		walk(e.X, visit)
	case *ir.Binary:
		walk(e.X, visit)
		walk(e.Y, visit)
	case *ir.If:
		walk(e.Cond, visit)
		walk(e.Then, visit)
		walk(e.Else, visit)
	case *ir.Block:
		for _, let := range e.Lets {
			walk(let.Value, visit)
		}
		walk(e.Result, visit)
	case *ir.Call:
		for _, arg := range e.Args {
			walk(arg, visit)
		}
	case *ir.CallIndirect:
		walk(e.Callee, visit)
		for _, arg := range e.Args {
			walk(arg, visit)
		}
	default:
		panic(fmt.Sprintf("wasm: unhandled expr %T", e))
	}
}

func sigKey(s ir.Sig) string {
	b := make([]byte, 0, len(s.Params)+1)
	for _, p := range s.Params {
		b = append(b, wasmValType(p))
	}
	b = append(b, wasmValType(s.Result))
	return string(b)
}

func wasmValType(t ir.ValType) byte {
	switch t {
	case ir.Int:
		return valI64
	case ir.Bool, ir.FuncRef:
		return valI32
	default:
		panic(fmt.Sprintf("wasm: unknown value type %d", int(t)))
	}
}

func section(id byte, content []byte) []byte {
	out := appendUleb128([]byte{id}, uint64(len(content)))
	return append(out, content...)
}

func encodeTypeSection(sigs []ir.Sig) []byte {
	content := appendUleb128(nil, uint64(len(sigs)))
	for _, s := range sigs {
		content = append(content, typeFunc)
		content = appendUleb128(content, uint64(len(s.Params)))
		for _, p := range s.Params {
			content = append(content, wasmValType(p))
		}
		content = appendUleb128(content, 1)
		content = append(content, wasmValType(s.Result))
	}
	return section(secType, content)
}

func encodeFunctionSection(m *ir.Module, types map[string]int) []byte {
	content := appendUleb128(nil, uint64(len(m.Funcs)))
	for _, fn := range m.Funcs {
		content = appendUleb128(content, uint64(types[sigKey(fn.Sig)]))
	}
	return section(secFunction, content)
}

func encodeTableSection(m *ir.Module) []byte {
	n := uint64(len(m.Table))
	content := appendUleb128(nil, 1)
	content = append(content, refFunc, limitsMinMax)
	content = appendUleb128(content, n)
	content = appendUleb128(content, n)
	return section(secTable, content)
}

func encodeExportSection(m *ir.Module) []byte {
	const name = "main"
	content := appendUleb128(nil, 1)
	content = appendUleb128(content, uint64(len(name)))
	content = append(content, name...)
	content = append(content, externalFunc)
	content = appendUleb128(content, uint64(m.Main))
	return section(secExport, content)
}

func encodeElementSection(m *ir.Module) []byte {
	content := appendUleb128(nil, 1)
	content = append(content, elemActiveFlags, opI32Const, 0x00, opEnd)
	content = appendUleb128(content, uint64(len(m.Table)))
	for _, id := range m.Table {
		content = appendUleb128(content, uint64(id))
	}
	return section(secElement, content)
}

func (e *encoder) encodeCodeSection(m *ir.Module) []byte {
	content := appendUleb128(nil, uint64(len(m.Funcs)))
	for _, fn := range m.Funcs {
		body := e.encodeBody(fn)
		content = appendUleb128(content, uint64(len(body)))
		content = append(content, body...)
	}
	return section(secCode, content)
}

func (e *encoder) encodeBody(fn *ir.Func) []byte {
	e.buf = encodeLocals(fn)
	e.expr(fn.Body)
	e.buf = append(e.buf, opEnd)
	return e.buf
}

// encodeLocals groups non-parameter locals into runs of the same wasm type.
// No extra locals is a vector of length 0, the single byte 0x00.
func encodeLocals(fn *ir.Func) []byte {
	extras := fn.Locals[len(fn.Sig.Params):]
	type group struct {
		n int
		t byte
	}
	var groups []group
	for _, t := range extras {
		wt := wasmValType(t)
		if len(groups) > 0 && groups[len(groups)-1].t == wt {
			groups[len(groups)-1].n++
			continue
		}
		groups = append(groups, group{n: 1, t: wt})
	}
	content := appendUleb128(nil, uint64(len(groups)))
	for _, g := range groups {
		content = appendUleb128(content, uint64(g.n))
		content = append(content, g.t)
	}
	return content
}

func (e *encoder) expr(x ir.Expr) {
	switch x := x.(type) {
	case *ir.IntConst:
		e.buf = append(e.buf, opI64Const)
		e.buf = appendSleb128(e.buf, x.Value)
	case *ir.BoolConst:
		if x.Value {
			e.buf = append(e.buf, opI32Const, 0x01)
		} else {
			e.buf = append(e.buf, opI32Const, 0x00)
		}
	case *ir.LocalGet:
		e.buf = append(e.buf, opLocalGet)
		e.buf = appendUleb128(e.buf, uint64(x.Local))
	case *ir.FuncValue:
		e.buf = append(e.buf, opI32Const)
		e.buf = appendSleb128(e.buf, e.tableIndex(x.Func))
	case *ir.Unary:
		e.unary(x)
	case *ir.Binary:
		e.binary(x)
	case *ir.If:
		e.expr(x.Cond)
		e.buf = append(e.buf, opIf, wasmValType(x.T))
		e.expr(x.Then)
		e.buf = append(e.buf, opElse)
		e.expr(x.Else)
		e.buf = append(e.buf, opEnd)
	case *ir.Block:
		for _, let := range x.Lets {
			e.expr(let.Value)
			e.buf = append(e.buf, opLocalSet)
			e.buf = appendUleb128(e.buf, uint64(let.Local))
		}
		e.expr(x.Result)
	case *ir.Call:
		for _, arg := range x.Args {
			e.expr(arg)
		}
		e.buf = append(e.buf, opCall)
		e.buf = appendUleb128(e.buf, uint64(x.Func))
	case *ir.CallIndirect:
		for _, arg := range x.Args {
			e.expr(arg)
		}
		e.expr(x.Callee)
		e.buf = append(e.buf, opCallIndirect)
		e.buf = appendUleb128(e.buf, e.typeIndex(x.Sig))
		e.buf = append(e.buf, 0x00)
	default:
		panic(fmt.Sprintf("wasm: unhandled expr %T", x))
	}
}

func (e *encoder) unary(x *ir.Unary) {
	switch x.Op {
	case ir.Neg:
		e.buf = append(e.buf, opI64Const)
		e.buf = appendSleb128(e.buf, 0)
		e.expr(x.X)
		e.buf = append(e.buf, opI64Sub)
	case ir.Not:
		e.expr(x.X)
		e.buf = append(e.buf, opI32Eqz)
	default:
		panic(fmt.Sprintf("wasm: unknown unary operator %d", int(x.Op)))
	}
}

func (e *encoder) binary(x *ir.Binary) {
	e.expr(x.X)
	e.expr(x.Y)
	switch x.X.Type() {
	case ir.Int:
		e.buf = append(e.buf, intBinOp(x.Op))
	case ir.Bool:
		switch x.Op {
		case ir.Eq:
			e.buf = append(e.buf, opI32Eq)
		case ir.Ne:
			e.buf = append(e.buf, opI32Ne)
		default:
			panic(fmt.Sprintf("wasm: unexpected bool operator %d", int(x.Op)))
		}
	default:
		panic(fmt.Sprintf("wasm: unexpected binary operand type %s", x.X.Type()))
	}
}

func intBinOp(op ir.BinOp) byte {
	switch op {
	case ir.Add:
		return opI64Add
	case ir.Sub:
		return opI64Sub
	case ir.Mul:
		return opI64Mul
	case ir.Div:
		return opI64DivS
	case ir.Eq:
		return opI64Eq
	case ir.Ne:
		return opI64Ne
	case ir.Lt:
		return opI64LtS
	case ir.Gt:
		return opI64GtS
	case ir.Le:
		return opI64LeS
	case ir.Ge:
		return opI64GeS
	default:
		panic(fmt.Sprintf("wasm: unexpected int operator %d", int(op)))
	}
}

func (e *encoder) typeIndex(s ir.Sig) uint64 {
	i, ok := e.types[sigKey(s)]
	if !ok {
		panic(fmt.Sprintf("wasm: no type index for signature with %d params", len(s.Params)))
	}
	return uint64(i)
}

func (e *encoder) tableIndex(id ir.FuncID) int64 {
	for i, f := range e.table {
		if f == id {
			return int64(i)
		}
	}
	panic(fmt.Sprintf("wasm: function %d is not in the table", id))
}
