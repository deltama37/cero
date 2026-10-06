package wasm

import "github.com/deltama37/cero/internal/ir"

// strUse records which string helpers a module needs.
// str_alloc is emitted when any helper that calls it is emitted.
type strUse struct {
	byteAt   bool
	slice    bool
	fromByte bool
	compare  bool
	intToStr bool
	concat   bool
	eq       bool
}

func (u strUse) alloc() bool {
	return u.slice || u.fromByte || u.intToStr || u.concat
}

func (u strUse) count() int {
	n := 0
	if u.alloc() {
		n++
	}
	if u.byteAt {
		n++
	}
	if u.slice {
		n++
	}
	if u.fromByte {
		n++
	}
	if u.compare {
		n++
	}
	if u.intToStr {
		n++
	}
	if u.concat {
		n++
	}
	if u.eq {
		n++
	}
	return n
}

// strFuncIdx holds the function index of each string helper, or -1 when
// that helper is not emitted. Indices follow alloc and the new helpers.
type strFuncIdx struct {
	alloc    int
	byteAt   int
	slice    int
	fromByte int
	compare  int
	intToStr int
	concat   int
	eq       int
}

func strFuncIndices(nfuncs, nnews int, u strUse) strFuncIdx {
	idx := nfuncs + 1 + nnews
	take := func(used bool) int {
		if !used {
			return -1
		}
		i := idx
		idx++
		return i
	}
	return strFuncIdx{
		alloc:    take(u.alloc()),
		byteAt:   take(u.byteAt),
		slice:    take(u.slice),
		fromByte: take(u.fromByte),
		compare:  take(u.compare),
		intToStr: take(u.intToStr),
		concat:   take(u.concat),
		eq:       take(u.eq),
	}
}

func collectStrUse(m *ir.Module) strUse {
	var u strUse
	for _, fn := range m.Funcs {
		walk(fn.Body, func(e ir.Expr) {
			p, ok := e.(*ir.Prim)
			if !ok {
				return
			}
			switch p.Op {
			case ir.StrByteAt:
				u.byteAt = true
			case ir.StrSlice:
				u.slice = true
			case ir.StrFromByte:
				u.fromByte = true
			case ir.StrCompare:
				u.compare = true
			case ir.IntToString:
				u.intToStr = true
			case ir.StrConcat:
				u.concat = true
			case ir.StrEq:
				u.eq = true
			}
		})
	}
	return u
}

// strLayout is the static placement of string literals. Addresses start at
// heapStart. Each block is [length: i32 LE][0: i32][bytes][pad to 8].
type strLayout struct {
	addr map[string]int
	heap int
	data []byte
}

func layoutStrings(m *ir.Module) strLayout {
	lay := strLayout{
		addr: make(map[string]int),
		heap: heapStart,
	}
	for _, fn := range m.Funcs {
		walk(fn.Body, func(e ir.Expr) {
			s, ok := e.(*ir.StrConst)
			if !ok {
				return
			}
			if _, seen := lay.addr[s.Value]; seen {
				return
			}
			lay.addr[s.Value] = lay.heap
			block := strBlock(s.Value)
			lay.data = append(lay.data, block...)
			lay.heap += len(block)
		})
	}
	return lay
}

func strBlockSize(n int) int {
	return (n + 15) &^ 7
}

func strBlock(value string) []byte {
	n := len(value)
	block := make([]byte, strBlockSize(n))
	block[0] = byte(n)
	block[1] = byte(n >> 8)
	block[2] = byte(n >> 16)
	block[3] = byte(n >> 24)
	copy(block[8:], value)
	return block
}

func memoryMinPages(heap int) int {
	pages := (heap + 65535) / 65536
	if pages < 1 {
		return 1
	}
	return pages
}

func (e *encoder) appendStrHelpers(content []byte, strs strUse) []byte {
	add := func(body []byte) {
		content = appendUleb128(content, uint64(len(body)))
		content = append(content, body...)
	}
	if strs.alloc() {
		add(encodeStrAlloc(e.allocIdx))
	}
	if strs.byteAt {
		add(encodeStrByteAt())
	}
	if strs.slice {
		add(encodeStrSlice(e.strFn.alloc))
	}
	if strs.fromByte {
		add(encodeStrFromByte(e.strFn.alloc))
	}
	if strs.compare {
		add(encodeStrCompare())
	}
	if strs.intToStr {
		add(encodeIntToString(e.strFn.alloc))
	}
	if strs.concat {
		add(encodeStrConcat(e.strFn.alloc))
	}
	if strs.eq {
		add(encodeStrEq())
	}
	return content
}

// encodeStrAlloc is (i32 n) -> i32.
// p = alloc((n + 15) & ~7); store length and a zero word; return p.
// Local p = 1.
func encodeStrAlloc(allocIdx int) []byte {
	a := asm{b: []byte{0x01, 0x01, valI32}}
	a.localGet(0)
	a.i32(15)
	a.raw(opI32Add)
	a.i32(-8)
	a.raw(opI32And)
	a.call(allocIdx)
	a.localSet(1)
	a.localGet(1)
	a.localGet(0)
	a.storeI32(0)
	a.localGet(1)
	a.i32(0)
	a.storeI32(4)
	a.localGet(1)
	a.end()
	return a.b
}

// encodeStrByteAt is (i32 s, i64 i) -> i64.
// An index outside 0 <= i < length traps. The byte is zero-extended to i64.
func encodeStrByteAt() []byte {
	a := asm{b: []byte{0x00}}
	a.localGet(1)
	a.localGet(0)
	a.loadI32(0)
	a.raw(opI64ExtendI32U)
	a.raw(opI64GeU)
	a.raw(opIf, blockEmpty)
	a.raw(opUnreachable)
	a.end()
	a.localGet(0)
	a.localGet(1)
	a.raw(opI32WrapI64)
	a.raw(opI32Add)
	a.i32(8)
	a.raw(opI32Add)
	a.load8UI64(0)
	a.end()
	return a.b
}

// encodeStrSlice is (i32 s, i64 a, i64 b) -> i32.
// Traps unless 0 <= a <= b <= length. Copies [a, b) into a fresh block.
// Locals: n = 3, p = 4.
func encodeStrSlice(strAlloc int) []byte {
	a := asm{b: []byte{0x01, 0x02, valI32}}
	a.localGet(1)
	a.i64(0)
	a.raw(opI64LtS)
	a.trapIf()
	a.localGet(1)
	a.localGet(2)
	a.raw(opI64GtS)
	a.trapIf()
	a.localGet(2)
	a.localGet(0)
	a.loadI32(0)
	a.raw(opI64ExtendI32U)
	a.raw(opI64GtS)
	a.trapIf()
	a.localGet(2)
	a.localGet(1)
	a.raw(opI64Sub)
	a.raw(opI32WrapI64)
	a.localSet(3)
	a.localGet(3)
	a.call(strAlloc)
	a.localSet(4)
	a.localGet(4)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(0)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(1)
	a.raw(opI32WrapI64)
	a.raw(opI32Add)
	a.localGet(3)
	a.memCopy()
	a.localGet(4)
	a.end()
	return a.b
}

// encodeStrFromByte is (i64 b) -> i32.
// Traps unless b is in 0..255. Local p = 1.
func encodeStrFromByte(strAlloc int) []byte {
	a := asm{b: []byte{0x01, 0x01, valI32}}
	a.localGet(0)
	a.i64(255)
	a.raw(opI64GtU)
	a.trapIf()
	a.i32(1)
	a.call(strAlloc)
	a.localSet(1)
	a.localGet(1)
	a.localGet(0)
	a.raw(opI32WrapI64)
	a.store8(8)
	a.localGet(1)
	a.end()
	return a.b
}

// encodeStrCompare is (i32 a, i32 b) -> i64.
// Bytes are compared unsigned up to the shorter length, then the lengths.
// Locals: la = 2, lb = 3, n = 4, i = 5, ba = 6, bb = 7.
func encodeStrCompare() []byte {
	a := asm{b: []byte{0x01, 0x06, valI32}}
	a.localGet(0)
	a.loadI32(0)
	a.localSet(2)
	a.localGet(1)
	a.loadI32(0)
	a.localSet(3)
	a.localGet(2)
	a.localGet(3)
	a.raw(opI32LtU)
	a.raw(opIf, valI32)
	a.localGet(2)
	a.raw(opElse)
	a.localGet(3)
	a.end()
	a.localSet(4)
	a.i32(0)
	a.localSet(5)
	a.raw(opBlock, valI64)
	a.raw(opLoop, blockEmpty)
	a.localGet(5)
	a.localGet(4)
	a.raw(opI32GeU)
	a.raw(opIf, blockEmpty)
	a.localGet(2)
	a.localGet(3)
	a.raw(opI32LtU)
	a.raw(opIf, valI64)
	a.i64(-1)
	a.raw(opElse)
	a.localGet(2)
	a.localGet(3)
	a.raw(opI32GtU)
	a.raw(opIf, valI64)
	a.i64(1)
	a.raw(opElse)
	a.i64(0)
	a.end()
	a.end()
	a.raw(opBr, 2)
	a.end()
	a.byteAt(0, 5, 6)
	a.byteAt(1, 5, 7)
	a.localGet(6)
	a.localGet(7)
	a.raw(opI32Ne)
	a.raw(opIf, blockEmpty)
	a.localGet(6)
	a.localGet(7)
	a.raw(opI32LtU)
	a.raw(opIf, valI64)
	a.i64(-1)
	a.raw(opElse)
	a.i64(1)
	a.end()
	a.raw(opBr, 2)
	a.end()
	a.localGet(5)
	a.i32(1)
	a.raw(opI32Add)
	a.localSet(5)
	a.raw(opBr, 0)
	a.end()
	a.i64(0)
	a.end()
	a.end()
	return a.b
}

// encodeIntToString is (i64 n) -> i32.
// A negative value gets a leading '-'. The magnitude is divided as unsigned
// so the most negative Int is rendered. Digits are counted, then written
// from the end.
// Locals: u = 1, tmp = 2, neg = 3, digits = 4, length = 5, p = 6, pos = 7.
func encodeIntToString(strAlloc int) []byte {
	a := asm{b: []byte{0x02, 0x02, valI64, 0x05, valI32}}
	a.localGet(0)
	a.i64(0)
	a.raw(opI64LtS)
	a.raw(opIf, valI32)
	a.i32(1)
	a.raw(opElse)
	a.i32(0)
	a.end()
	a.localSet(3)
	a.localGet(3)
	a.raw(opIf, valI64)
	a.i64(0)
	a.localGet(0)
	a.raw(opI64Sub)
	a.raw(opElse)
	a.localGet(0)
	a.end()
	a.localSet(1)
	a.i32(0)
	a.localSet(4)
	a.localGet(1)
	a.localSet(2)
	a.raw(opBlock, blockEmpty)
	a.raw(opLoop, blockEmpty)
	a.localGet(4)
	a.i32(1)
	a.raw(opI32Add)
	a.localSet(4)
	a.localGet(2)
	a.i64(10)
	a.raw(opI64DivU)
	a.localTee(2)
	a.raw(opI64Eqz)
	a.raw(opBrIf, 1)
	a.raw(opBr, 0)
	a.end()
	a.end()
	a.localGet(4)
	a.localGet(3)
	a.raw(opI32Add)
	a.localSet(5)
	a.localGet(5)
	a.call(strAlloc)
	a.localSet(6)
	a.localGet(5)
	a.localSet(7)
	a.localGet(1)
	a.localSet(2)
	a.raw(opBlock, blockEmpty)
	a.raw(opLoop, blockEmpty)
	a.localGet(7)
	a.i32(1)
	a.raw(opI32Sub)
	a.localSet(7)
	a.localGet(6)
	a.localGet(7)
	a.raw(opI32Add)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(2)
	a.i64(10)
	a.raw(opI64RemU)
	a.raw(opI32WrapI64)
	a.i32('0')
	a.raw(opI32Add)
	a.store8(0)
	a.localGet(2)
	a.i64(10)
	a.raw(opI64DivU)
	a.localSet(2)
	a.localGet(7)
	a.localGet(3)
	a.raw(opI32GtU)
	a.raw(opBrIf, 0)
	a.end()
	a.end()
	a.localGet(3)
	a.raw(opIf, blockEmpty)
	a.localGet(6)
	a.i32('-')
	a.store8(8)
	a.end()
	a.localGet(6)
	a.end()
	return a.b
}

// encodeStrConcat is (i32 a, i32 b) -> i32.
// Locals: la = 2, lb = 3, p = 4.
func encodeStrConcat(strAlloc int) []byte {
	a := asm{b: []byte{0x01, 0x03, valI32}}
	a.localGet(0)
	a.loadI32(0)
	a.localSet(2)
	a.localGet(1)
	a.loadI32(0)
	a.localSet(3)
	a.localGet(2)
	a.localGet(3)
	a.raw(opI32Add)
	a.call(strAlloc)
	a.localSet(4)
	a.localGet(4)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(0)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(2)
	a.memCopy()
	a.localGet(4)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(2)
	a.raw(opI32Add)
	a.localGet(1)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(3)
	a.memCopy()
	a.localGet(4)
	a.end()
	return a.b
}

// encodeStrEq is (i32 a, i32 b) -> i32.
// The same address is equal. Different lengths are not. Otherwise the bytes
// are compared. Locals: la = 2, i = 3.
func encodeStrEq() []byte {
	a := asm{b: []byte{0x01, 0x02, valI32}}
	a.localGet(0)
	a.localGet(1)
	a.raw(opI32Eq)
	a.raw(opIf, valI32)
	a.i32(1)
	a.raw(opElse)
	a.localGet(0)
	a.loadI32(0)
	a.localTee(2)
	a.localGet(1)
	a.loadI32(0)
	a.raw(opI32Ne)
	a.raw(opIf, valI32)
	a.i32(0)
	a.raw(opElse)
	a.i32(0)
	a.localSet(3)
	a.raw(opBlock, valI32)
	a.raw(opLoop, blockEmpty)
	a.localGet(3)
	a.localGet(2)
	a.raw(opI32GeU)
	a.raw(opIf, blockEmpty)
	a.i32(1)
	a.raw(opBr, 2)
	a.end()
	a.localGet(0)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(3)
	a.raw(opI32Add)
	a.load8U(0)
	a.localGet(1)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(3)
	a.raw(opI32Add)
	a.load8U(0)
	a.raw(opI32Ne)
	a.raw(opIf, blockEmpty)
	a.i32(0)
	a.raw(opBr, 2)
	a.end()
	a.localGet(3)
	a.i32(1)
	a.raw(opI32Add)
	a.localSet(3)
	a.raw(opBr, 0)
	a.end()
	a.i32(1)
	a.end()
	a.end()
	a.end()
	a.end()
	return a.b
}

// byteAt loads the unsigned byte at base+8+index into dst.
func (a *asm) byteAt(base, index, dst int) {
	a.localGet(base)
	a.i32(8)
	a.raw(opI32Add)
	a.localGet(index)
	a.raw(opI32Add)
	a.load8U(0)
	a.localSet(dst)
}

func (a *asm) trapIf() {
	a.raw(opIf, blockEmpty)
	a.raw(opUnreachable)
	a.end()
}

type asm struct {
	b []byte
}

func (a *asm) raw(xs ...byte) { a.b = append(a.b, xs...) }

func (a *asm) u32(v int) { a.b = appendUleb128(a.b, uint64(v)) }

func (a *asm) i32(v int64) {
	a.raw(opI32Const)
	a.b = appendSleb128(a.b, v)
}

func (a *asm) i64(v int64) {
	a.raw(opI64Const)
	a.b = appendSleb128(a.b, v)
}

func (a *asm) localGet(i int) {
	a.raw(opLocalGet)
	a.u32(i)
}

func (a *asm) localSet(i int) {
	a.raw(opLocalSet)
	a.u32(i)
}

func (a *asm) localTee(i int) {
	a.raw(opLocalTee)
	a.u32(i)
}

func (a *asm) call(i int) {
	a.raw(opCall)
	a.u32(i)
}

func (a *asm) loadI32(off int) {
	a.raw(opI32Load, alignI32)
	a.u32(off)
}

func (a *asm) storeI32(off int) {
	a.raw(opI32Store, alignI32)
	a.u32(off)
}

func (a *asm) load8U(off int) {
	a.raw(opI32Load8U, alignI8)
	a.u32(off)
}

func (a *asm) load8UI64(off int) {
	a.raw(opI64Load8U, alignI8)
	a.u32(off)
}

func (a *asm) store8(off int) {
	a.raw(opI32Store8, alignI8)
	a.u32(off)
}

func (a *asm) memCopy() { a.raw(0xfc, 0x0a, 0x00, 0x00) }

func (a *asm) end() { a.raw(opEnd) }
