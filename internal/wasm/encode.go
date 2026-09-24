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
	secMemory   byte = 5
	secGlobal   byte = 6
	secExport   byte = 7
	secElement  byte = 9
	secCode     byte = 10

	opUnreachable   byte = 0x00
	opEnd           byte = 0x0b
	opIf            byte = 0x04
	opElse          byte = 0x05
	opCall          byte = 0x10
	opCallIndirect  byte = 0x11
	opLocalGet      byte = 0x20
	opLocalSet      byte = 0x21
	opGlobalGet     byte = 0x23
	opGlobalSet     byte = 0x24
	opI32Load       byte = 0x28
	opI64Load       byte = 0x29
	opI32Store      byte = 0x36
	opI64Store      byte = 0x37
	opMemorySize    byte = 0x3f
	opMemoryGrow    byte = 0x40
	opI32Const      byte = 0x41
	opI64Const      byte = 0x42
	opI32Eqz        byte = 0x45
	opI32Eq         byte = 0x46
	opI32Ne         byte = 0x47
	opI32GtU        byte = 0x4b
	opI64Eq         byte = 0x51
	opI64Ne         byte = 0x52
	opI64LtS        byte = 0x53
	opI64GtS        byte = 0x55
	opI64LeS        byte = 0x57
	opI64GeS        byte = 0x59
	opI32Add        byte = 0x6a
	opI32Sub        byte = 0x6b
	opI32Shl        byte = 0x74
	opI32ShrU       byte = 0x76
	opI64Add        byte = 0x7c
	opI64Sub        byte = 0x7d
	opI64Mul        byte = 0x7e
	opI64DivS       byte = 0x7f
	valI32          byte = 0x7f
	valI64          byte = 0x7e
	typeFunc        byte = 0x60
	refFunc         byte = 0x70
	blockEmpty      byte = 0x40
	limitsMinMax    byte = 0x01
	mutVar          byte = 0x01
	externalFunc    byte = 0x00
	elemActiveFlags byte = 0x00
	alignI32        byte = 0x02
	alignI64        byte = 0x03

	// memoryMaxPages is 32768 so that page count << 16 stays inside an
	// unsigned i32 (ADR-0003).
	memoryMaxPages = 32768
	heapStart      = 8
)

var wasmHeader = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

// Encode returns the WebAssembly binary module for m.
func Encode(m *ir.Module) []byte {
	mem := usesMemory(m)
	var news [][]ir.ValType
	if mem {
		news = collectNews(m)
	}
	sigs, types := collectTypes(m, mem, news)
	e := &encoder{
		table:    m.Table,
		types:    types,
		allocIdx: len(m.Funcs),
		newIdx:   newFuncIndex(len(m.Funcs), news),
	}

	out := append([]byte(nil), wasmHeader...)
	out = append(out, encodeTypeSection(sigs)...)
	out = append(out, encodeFunctionSection(m, types, mem, news)...)
	out = append(out, encodeTableSection(m)...)
	if mem {
		out = append(out, encodeMemorySection()...)
		out = append(out, encodeGlobalSection()...)
	}
	out = append(out, encodeExportSection(m)...)
	if len(m.Table) > 0 {
		out = append(out, encodeElementSection(m)...)
	}
	out = append(out, e.encodeCodeSection(m, mem, news)...)
	return out
}

type encoder struct {
	table    []ir.FuncID
	types    map[string]int
	buf      []byte
	allocIdx int
	newIdx   map[string]int
}

// usesMemory reports whether any function body contains a Construct,
// Field or SwitchTag node.
func usesMemory(m *ir.Module) bool {
	for _, fn := range m.Funcs {
		found := false
		walk(fn.Body, func(e ir.Expr) {
			switch e.(type) {
			case *ir.Construct, *ir.Field, *ir.SwitchTag:
				found = true
			}
		})
		if found {
			return true
		}
	}
	return false
}

// collectTypes assigns type indices in first-seen order.
// Function signatures are scanned first, then each body in preorder
// so that CallIndirect signatures share indices with matching functions.
// Bool, FuncRef and Ptr are all i32, so they collapse to one wasm type.
// When the module uses memory, alloc's type and each new's type are added
// after the v0.1 scan, in auxiliary-function order.
func collectTypes(m *ir.Module, mem bool, news [][]ir.ValType) ([]ir.Sig, map[string]int) {
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
	if mem {
		for _, s := range auxiliarySigs(news) {
			add(s)
		}
	}
	return sigs, index
}

// collectNews returns one field-type list per distinct wasm value-type
// sequence of a Construct, in order of first appearance. Module.Funcs are
// scanned in order and each body is walked in preorder.
func collectNews(m *ir.Module) [][]ir.ValType {
	var news [][]ir.ValType
	seen := make(map[string]bool)
	for _, fn := range m.Funcs {
		walk(fn.Body, func(e ir.Expr) {
			c, ok := e.(*ir.Construct)
			if !ok {
				return
			}
			fields := make([]ir.ValType, len(c.Fields))
			for i, f := range c.Fields {
				fields[i] = f.Type()
			}
			key := fieldTypesKey(fields)
			if seen[key] {
				return
			}
			seen[key] = true
			news = append(news, fields)
		})
	}
	return news
}

// auxiliarySigs is alloc's signature followed by each new's signature.
// i32 is represented as ir.Ptr and i64 as ir.Int; sigKey compares wasm
// value types, so a Bool or FuncRef parameter shares an index with Ptr.
func auxiliarySigs(news [][]ir.ValType) []ir.Sig {
	out := make([]ir.Sig, 0, 1+len(news))
	out = append(out, ir.Sig{Params: []ir.ValType{ir.Ptr}, Result: ir.Ptr})
	for _, fields := range news {
		params := make([]ir.ValType, 1+len(fields))
		params[0] = ir.Ptr
		for i, ft := range fields {
			params[i+1] = wasmAsIR(ft)
		}
		out = append(out, ir.Sig{Params: params, Result: ir.Ptr})
	}
	return out
}

// newFuncIndex maps a Construct's wasm field-type sequence to the function
// index of its new helper. alloc occupies len(funcs); new helpers follow.
func newFuncIndex(nfuncs int, news [][]ir.ValType) map[string]int {
	idx := make(map[string]int, len(news))
	for i, fields := range news {
		idx[fieldTypesKey(fields)] = nfuncs + 1 + i
	}
	return idx
}

func fieldTypesKey(fields []ir.ValType) string {
	b := make([]byte, len(fields))
	for i, t := range fields {
		b[i] = wasmValType(t)
	}
	return string(b)
}

// wasmAsIR maps a field's wasm value type back to an IR type for sigKey:
// i64 stays Int, every i32 becomes Ptr.
func wasmAsIR(t ir.ValType) ir.ValType {
	if t == ir.Int {
		return ir.Int
	}
	return ir.Ptr
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
	case *ir.Construct:
		for _, field := range e.Fields {
			walk(field, visit)
		}
	case *ir.Field:
	case *ir.SwitchTag:
		for _, c := range e.Cases {
			walk(c.Body, visit)
		}
		if e.Default != nil {
			walk(e.Default, visit)
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
	case ir.Bool, ir.FuncRef, ir.Ptr:
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

func encodeFunctionSection(
	m *ir.Module,
	types map[string]int,
	mem bool,
	news [][]ir.ValType,
) []byte {
	n := len(m.Funcs)
	var aux []ir.Sig
	if mem {
		aux = auxiliarySigs(news)
		n += len(aux)
	}
	content := appendUleb128(nil, uint64(n))
	for _, fn := range m.Funcs {
		content = appendUleb128(content, uint64(types[sigKey(fn.Sig)]))
	}
	for _, s := range aux {
		content = appendUleb128(content, uint64(types[sigKey(s)]))
	}
	return section(secFunction, content)
}

func encodeMemorySection() []byte {
	content := appendUleb128(nil, 1)
	content = append(content, limitsMinMax)
	content = appendUleb128(content, 1)
	content = appendUleb128(content, memoryMaxPages)
	return section(secMemory, content)
}

func encodeGlobalSection() []byte {
	content := appendUleb128(nil, 1)
	content = append(content, valI32, mutVar, opI32Const)
	content = appendSleb128(content, heapStart)
	content = append(content, opEnd)
	return section(secGlobal, content)
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

func (e *encoder) encodeCodeSection(m *ir.Module, mem bool, news [][]ir.ValType) []byte {
	n := len(m.Funcs)
	if mem {
		n += 1 + len(news)
	}
	content := appendUleb128(nil, uint64(n))
	for _, fn := range m.Funcs {
		body := e.encodeBody(fn)
		content = appendUleb128(content, uint64(len(body)))
		content = append(content, body...)
	}
	if mem {
		alloc := encodeAllocBody()
		content = appendUleb128(content, uint64(len(alloc)))
		content = append(content, alloc...)
		for _, fields := range news {
			body := encodeNewBody(e.allocIdx, fields)
			content = appendUleb128(content, uint64(len(body)))
			content = append(content, body...)
		}
	}
	return section(secCode, content)
}

// encodeAllocBody is the bump allocator (i32) -> i32.
// Locals: argument size = 0, p = 1. The local declaration is 01 01 7F.
func encodeAllocBody() []byte {
	buf := []byte{0x01, 0x01, valI32}
	// p = heap
	buf = append(buf, opGlobalGet, 0x00, opLocalSet, 0x01)
	// heap = heap + size
	buf = append(buf, opGlobalGet, 0x00, opLocalGet, 0x00, opI32Add, opGlobalSet, 0x00)
	// if heap > memory.size * 65536
	buf = append(buf,
		opGlobalGet, 0x00,
		opMemorySize, 0x00,
		opI32Const, 0x10,
		opI32Shl,
		opI32GtU,
		opIf, blockEmpty,
	)
	// pages = (heap - memory.size * 65536 + 65535) >> 16
	buf = append(buf,
		opGlobalGet, 0x00,
		opMemorySize, 0x00,
		opI32Const, 0x10,
		opI32Shl,
		opI32Sub,
	)
	buf = append(buf, opI32Const)
	buf = appendSleb128(buf, 65535)
	buf = append(buf, opI32Add, opI32Const, 0x10, opI32ShrU, opMemoryGrow, 0x00)
	// if memory.grow failed: trap
	buf = append(buf, opI32Const, 0x7f, opI32Eq, opIf, blockEmpty, opUnreachable, opEnd)
	buf = append(buf, opEnd, opLocalGet, 0x01, opEnd)
	return buf
}

// encodeNewBody allocates a block, stores the tag and fields, and returns
// the address. Arguments: tag = 0, field i = i+1, local p = n+1.
func encodeNewBody(allocIdx int, fields []ir.ValType) []byte {
	n := len(fields)
	p := n + 1
	buf := []byte{0x01, 0x01, valI32}
	buf = append(buf, opI32Const)
	buf = appendSleb128(buf, int64(8+8*n))
	buf = append(buf, opCall)
	buf = appendUleb128(buf, uint64(allocIdx))
	buf = append(buf, opLocalSet)
	buf = appendUleb128(buf, uint64(p))
	// i32.store align=2 offset=0 (tag)
	buf = append(buf, opLocalGet)
	buf = appendUleb128(buf, uint64(p))
	buf = append(buf, opLocalGet)
	buf = appendUleb128(buf, 0)
	buf = append(buf, opI32Store, alignI32, 0x00)
	for i, ft := range fields {
		buf = append(buf, opLocalGet)
		buf = appendUleb128(buf, uint64(p))
		buf = append(buf, opLocalGet)
		buf = appendUleb128(buf, uint64(i+1))
		if ft == ir.Int {
			buf = append(buf, opI64Store, alignI64)
		} else {
			buf = append(buf, opI32Store, alignI32)
		}
		buf = appendUleb128(buf, uint64(8+8*i))
	}
	buf = append(buf, opLocalGet)
	buf = appendUleb128(buf, uint64(p))
	buf = append(buf, opEnd)
	return buf
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
	case *ir.Construct:
		e.buf = append(e.buf, opI32Const)
		e.buf = appendSleb128(e.buf, int64(x.Tag))
		fields := make([]ir.ValType, len(x.Fields))
		for i, field := range x.Fields {
			e.expr(field)
			fields[i] = field.Type()
		}
		idx, ok := e.newIdx[fieldTypesKey(fields)]
		if !ok {
			panic(fmt.Sprintf("wasm: no new function for constructor tag %d", x.Tag))
		}
		e.buf = append(e.buf, opCall)
		e.buf = appendUleb128(e.buf, uint64(idx))
	case *ir.Field:
		e.buf = append(e.buf, opLocalGet)
		e.buf = appendUleb128(e.buf, uint64(x.Local))
		offset := uint64(8 + 8*x.Index)
		if x.T == ir.Int {
			e.buf = append(e.buf, opI64Load, alignI64)
		} else {
			e.buf = append(e.buf, opI32Load, alignI32)
		}
		e.buf = appendUleb128(e.buf, offset)
	case *ir.SwitchTag:
		e.switchTag(x, 0)
	default:
		panic(fmt.Sprintf("wasm: unhandled expr %T", x))
	}
}

func (e *encoder) switchTag(x *ir.SwitchTag, i int) {
	if x.Default == nil && i == len(x.Cases)-1 {
		e.expr(x.Cases[i].Body)
		return
	}
	if i == len(x.Cases) {
		e.expr(x.Default)
		return
	}
	e.buf = append(e.buf, opLocalGet)
	e.buf = appendUleb128(e.buf, uint64(x.Local))
	e.buf = append(e.buf, opI32Load, alignI32, 0x00)
	e.buf = append(e.buf, opI32Const)
	e.buf = appendSleb128(e.buf, int64(x.Cases[i].Tag))
	e.buf = append(e.buf, opI32Eq)
	e.buf = append(e.buf, opIf, wasmValType(x.T))
	e.expr(x.Cases[i].Body)
	e.buf = append(e.buf, opElse)
	e.switchTag(x, i+1)
	e.buf = append(e.buf, opEnd)
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
