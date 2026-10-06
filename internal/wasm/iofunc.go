package wasm

import (
	"fmt"

	"github.com/deltama37/cero/internal/ir"
)

const (
	wasiFdWrite      = 0
	wasiFdRead       = 1
	wasiPathOpen     = 2
	wasiFdClose      = 3
	wasiArgsSizesGet = 4
	wasiArgsGet      = 5
	wasiProcExit     = 6
	wasiImportCount  = 7

	errReadFile  = "error: cannot read file "
	errWriteFile = "error: cannot write file "
	errNewline   = "\n"

	fdReadRight  int64 = 0x2
	fdWriteRight int64 = 0x40
	oCreatTrunc  int64 = 0x9
)

// ioUse records which IO helpers a module needs.
// A helper is emitted when a Prim uses it or when another emitted helper calls it.
type ioUse struct {
	print      bool
	eprint     bool
	readStdin  bool
	readFile   bool
	fileExists bool
	writeFile  bool
	argCount   bool
	argAt      bool
	exit       bool
}

func (u ioUse) any() bool {
	return u.print || u.eprint || u.readStdin || u.readFile || u.fileExists ||
		u.writeFile || u.argCount || u.argAt || u.exit
}

func (u ioUse) needWrite() bool {
	return u.print || u.eprint || u.readFile || u.writeFile
}

func (u ioUse) needReadFd() bool {
	return u.readStdin || u.readFile
}

func (u ioUse) needOpen() bool {
	return u.readFile || u.fileExists || u.writeFile
}

func (u ioUse) count() int {
	n := 0
	for _, h := range ioPlan(u) {
		if h.used {
			n++
		}
	}
	return n
}

type ioKind int

const (
	ioKindWrite ioKind = iota
	ioKindPrint
	ioKindEPrint
	ioKindReadFd
	ioKindReadStdin
	ioKindOpen
	ioKindReadFile
	ioKindFileExists
	ioKindWriteFile
	ioKindArgCount
	ioKindArgAt
	ioKindExit
)

type ioHelper struct {
	kind ioKind
	used bool
	sig  ir.Sig
}

func ioPlan(u ioUse) []ioHelper {
	i32 := ir.Ptr
	i64 := ir.Int
	return []ioHelper{
		{ioKindWrite, u.needWrite(), ir.Sig{Params: []ir.ValType{i32, i32}, Result: i32}},
		{ioKindPrint, u.print, ir.Sig{Params: []ir.ValType{i32}, Result: i32}},
		{ioKindEPrint, u.eprint, ir.Sig{Params: []ir.ValType{i32}, Result: i32}},
		{ioKindReadFd, u.needReadFd(), ir.Sig{Params: []ir.ValType{i32}, Result: i32}},
		{ioKindReadStdin, u.readStdin, ir.Sig{Result: i32}},
		{ioKindOpen, u.needOpen(), ir.Sig{Params: []ir.ValType{i32, i32, i64}, Result: i32}},
		{ioKindReadFile, u.readFile, ir.Sig{Params: []ir.ValType{i32}, Result: i32}},
		{ioKindFileExists, u.fileExists, ir.Sig{Params: []ir.ValType{i32}, Result: i32}},
		{ioKindWriteFile, u.writeFile, ir.Sig{Params: []ir.ValType{i32, i32}, Result: i32}},
		{ioKindArgCount, u.argCount, ir.Sig{Result: i64}},
		{ioKindArgAt, u.argAt, ir.Sig{Params: []ir.ValType{i64}, Result: i32}},
		{ioKindExit, u.exit, ir.Sig{Params: []ir.ValType{i64}, Result: i32}},
	}
}

func ioAuxSigs(u ioUse) []ir.Sig {
	var out []ir.Sig
	for _, h := range ioPlan(u) {
		if h.used {
			out = append(out, h.sig)
		}
	}
	return out
}

// ioFuncIdx holds the function index of each IO helper, or -1 when that
// helper is not emitted. Indices follow the string helpers. start is _start.
type ioFuncIdx struct {
	write      int
	print      int
	eprint     int
	readFd     int
	readStdin  int
	open       int
	readFile   int
	fileExists int
	writeFile  int
	argCount   int
	argAt      int
	exit       int
	start      int
}

func ioFuncIndices(start int, u ioUse, command bool) ioFuncIdx {
	idx := ioFuncIdx{
		write: -1, print: -1, eprint: -1, readFd: -1, readStdin: -1,
		open: -1, readFile: -1, fileExists: -1, writeFile: -1,
		argCount: -1, argAt: -1, exit: -1, start: -1,
	}
	n := start
	for _, h := range ioPlan(u) {
		if !h.used {
			continue
		}
		switch h.kind {
		case ioKindWrite:
			idx.write = n
		case ioKindPrint:
			idx.print = n
		case ioKindEPrint:
			idx.eprint = n
		case ioKindReadFd:
			idx.readFd = n
		case ioKindReadStdin:
			idx.readStdin = n
		case ioKindOpen:
			idx.open = n
		case ioKindReadFile:
			idx.readFile = n
		case ioKindFileExists:
			idx.fileExists = n
		case ioKindWriteFile:
			idx.writeFile = n
		case ioKindArgCount:
			idx.argCount = n
		case ioKindArgAt:
			idx.argAt = n
		case ioKindExit:
			idx.exit = n
		default:
			panic(fmt.Sprintf("wasm: unknown io helper %d", int(h.kind)))
		}
		n++
	}
	if command {
		idx.start = n
	}
	return idx
}

func collectIOUse(m *ir.Module) ioUse {
	var u ioUse
	for _, fn := range m.Funcs {
		walk(fn.Body, func(e ir.Expr) {
			p, ok := e.(*ir.Prim)
			if !ok {
				return
			}
			switch p.Op {
			case ir.IOPrint:
				u.print = true
			case ir.IOEPrint:
				u.eprint = true
			case ir.IOReadStdin:
				u.readStdin = true
			case ir.IOReadFile:
				u.readFile = true
			case ir.IOFileExists:
				u.fileExists = true
			case ir.IOWriteFile:
				u.writeFile = true
			case ir.IOArgCount:
				u.argCount = true
			case ir.IOArgAt:
				u.argAt = true
			case ir.IOExit:
				u.exit = true
			}
		})
	}
	return u
}

func wasiSigs() []ir.Sig {
	i32 := ir.Ptr
	i64 := ir.Int
	return []ir.Sig{
		{Params: []ir.ValType{i32, i32, i32, i32}, Result: i32},
		{Params: []ir.ValType{i32, i32, i32, i32, i32, i64, i64, i32, i32}, Result: i32},
		{Params: []ir.ValType{i32}, Result: i32},
		{Params: []ir.ValType{i32, i32}, Result: i32},
	}
}

func encodeImportSection(types map[string]int, procExitType int) []byte {
	i32 := ir.Ptr
	i64 := ir.Int
	type item struct {
		name string
		sig  ir.Sig
		void bool
	}
	items := []item{
		{"fd_write", ir.Sig{Params: []ir.ValType{i32, i32, i32, i32}, Result: i32}, false},
		{"fd_read", ir.Sig{Params: []ir.ValType{i32, i32, i32, i32}, Result: i32}, false},
		{"path_open", ir.Sig{Params: []ir.ValType{i32, i32, i32, i32, i32, i64, i64, i32, i32}, Result: i32}, false},
		{"fd_close", ir.Sig{Params: []ir.ValType{i32}, Result: i32}, false},
		{"args_sizes_get", ir.Sig{Params: []ir.ValType{i32, i32}, Result: i32}, false},
		{"args_get", ir.Sig{Params: []ir.ValType{i32, i32}, Result: i32}, false},
		{"proc_exit", ir.Sig{}, true},
	}
	content := appendUleb128(nil, uint64(len(items)))
	for _, it := range items {
		content = appendName(content, "wasi_snapshot_preview1")
		content = appendName(content, it.name)
		content = append(content, externalFunc)
		idx := procExitType
		if !it.void {
			idx = mustTypeIndex(types, it.sig)
		}
		content = appendUleb128(content, uint64(idx))
	}
	return section(secImport, content)
}

func encodeCommandExport(startIdx int) []byte {
	content := appendUleb128(nil, 2)
	content = appendName(content, "_start")
	content = append(content, externalFunc)
	content = appendUleb128(content, uint64(startIdx))
	content = appendName(content, "memory")
	content = append(content, 0x02)
	content = appendUleb128(content, 0)
	return section(secExport, content)
}

func appendName(buf []byte, name string) []byte {
	buf = appendUleb128(buf, uint64(len(name)))
	return append(buf, name...)
}

func (e *encoder) appendIOHelpers(content []byte, u ioUse) []byte {
	add := func(body []byte) {
		content = appendUleb128(content, uint64(len(body)))
		content = append(content, body...)
	}
	for _, h := range ioPlan(u) {
		if !h.used {
			continue
		}
		add(e.encodeIO(h.kind))
	}
	return content
}

func (e *encoder) encodeIO(kind ioKind) []byte {
	switch kind {
	case ioKindWrite:
		return encodeIOWrite(e.allocIdx)
	case ioKindPrint:
		return encodeIOToFd(e.ioFn.write, 1)
	case ioKindEPrint:
		return encodeIOToFd(e.ioFn.write, 2)
	case ioKindReadFd:
		return encodeIOReadFd(e.allocIdx)
	case ioKindReadStdin:
		return encodeIOReadStdin(e.ioFn.readFd)
	case ioKindOpen:
		return encodeIOOpen(e.allocIdx)
	case ioKindReadFile:
		return e.encodeIOReadFile()
	case ioKindFileExists:
		return e.encodeIOFileExists()
	case ioKindWriteFile:
		return e.encodeIOWriteFile()
	case ioKindArgCount:
		return encodeIOArgCount(e.allocIdx)
	case ioKindArgAt:
		return encodeIOArgAt(e.allocIdx)
	case ioKindExit:
		return encodeIOExit()
	default:
		panic(fmt.Sprintf("wasm: unknown io helper %d", int(kind)))
	}
}

func (e *encoder) strConstAddr(s string) int {
	addr, ok := e.strAddr[s]
	if !ok {
		panic(fmt.Sprintf("wasm: missing string %q", s))
	}
	return addr
}

// encodeIOWrite is (i32 fd, i32 s) -> i32.
// It repeats fd_write until every byte of s has been written.
// Locals: ptr = 2, left = 3, iov = 4, n = 5.
func encodeIOWrite(allocIdx int) []byte {
	const (
		fd   = 0
		s    = 1
		ptr  = 2
		left = 3
		iov  = 4
		n    = 5
	)
	a := asm{b: []byte{0x01, 0x04, valI32}}
	a.localGet(s)
	a.i32(8)
	a.add()
	a.localSet(ptr)
	a.localGet(s)
	a.loadI32(0)
	a.localSet(left)
	a.block()
	a.loop()
	a.localGet(left)
	a.eqz()
	a.brIf(1)
	a.i32(16)
	a.call(allocIdx)
	a.localSet(iov)
	a.localGet(iov)
	a.localGet(ptr)
	a.storeI32(0)
	a.localGet(iov)
	a.localGet(left)
	a.storeI32(4)
	a.localGet(iov)
	a.i32(0)
	a.storeI32(8)
	a.localGet(fd)
	a.localGet(iov)
	a.i32(1)
	a.localGet(iov)
	a.i32(8)
	a.add()
	a.call(wasiFdWrite)
	a.trapUnlessZero()
	a.localGet(iov)
	a.loadI32(8)
	a.localTee(n)
	a.eqz()
	a.ifVoid()
	a.unreachable()
	a.end()
	a.localGet(n)
	a.localGet(left)
	a.gtU()
	a.ifVoid()
	a.unreachable()
	a.end()
	a.localGet(ptr)
	a.localGet(n)
	a.add()
	a.localSet(ptr)
	a.localGet(left)
	a.localGet(n)
	a.sub()
	a.localSet(left)
	a.br(0)
	a.end()
	a.end()
	a.i32(0)
	a.end()
	return a.b
}

func encodeIOToFd(writeIdx, fd int) []byte {
	a := asm{b: []byte{0x00}}
	a.i32(int64(fd))
	a.localGet(0)
	a.call(writeIdx)
	a.end()
	return a.b
}

// encodeIOReadFd is (i32 fd) -> i32.
// It reads until nread is 0, doubling a string block from 4096 bytes.
// Locals: block = 1, cap = 2, len = 3, work = 4, nread = 5, grown = 6.
func encodeIOReadFd(allocIdx int) []byte {
	const (
		fd     = 0
		block  = 1
		cap    = 2
		length = 3
		work   = 4
		nread  = 5
		grown  = 6
	)
	a := asm{b: []byte{0x01, 0x06, valI32}}
	a.i32(4096)
	a.localSet(cap)
	a.blockSize(cap)
	a.call(allocIdx)
	a.localSet(block)
	a.i32(0)
	a.localSet(length)
	a.block()
	a.loop()
	a.localGet(length)
	a.localGet(cap)
	a.eq()
	a.ifVoid()
	a.localGet(cap)
	a.i32(1)
	a.shl()
	a.localSet(cap)
	a.blockSize(cap)
	a.call(allocIdx)
	a.localSet(grown)
	a.localGet(grown)
	a.i32(8)
	a.add()
	a.localGet(block)
	a.i32(8)
	a.add()
	a.localGet(length)
	a.memCopy()
	a.localGet(grown)
	a.localSet(block)
	a.end()
	a.i32(16)
	a.call(allocIdx)
	a.localSet(work)
	a.localGet(work)
	a.localGet(block)
	a.i32(8)
	a.add()
	a.localGet(length)
	a.add()
	a.storeI32(0)
	a.localGet(work)
	a.localGet(cap)
	a.localGet(length)
	a.sub()
	a.storeI32(4)
	a.localGet(work)
	a.i32(0)
	a.storeI32(8)
	a.localGet(fd)
	a.localGet(work)
	a.i32(1)
	a.localGet(work)
	a.i32(8)
	a.add()
	a.call(wasiFdRead)
	a.trapUnlessZero()
	a.localGet(work)
	a.loadI32(8)
	a.localTee(nread)
	a.eqz()
	a.brIf(1)
	a.localGet(length)
	a.localGet(nread)
	a.add()
	a.localSet(length)
	a.br(0)
	a.end()
	a.end()
	a.localGet(block)
	a.localGet(length)
	a.storeI32(0)
	a.localGet(block)
	a.i32(0)
	a.storeI32(4)
	a.localGet(block)
	a.end()
	return a.b
}

func encodeIOReadStdin(readFd int) []byte {
	a := asm{b: []byte{0x00}}
	a.i32(0)
	a.call(readFd)
	a.end()
	return a.b
}

// encodeIOOpen is (i32 path, i32 oflags, i64 rights) -> i32.
// Success returns the fd. Failure returns -1.
// Local slot = 3.
func encodeIOOpen(allocIdx int) []byte {
	const (
		path   = 0
		oflags = 1
		rights = 2
		slot   = 3
	)
	a := asm{b: []byte{0x01, 0x01, valI32}}
	a.i32(8)
	a.call(allocIdx)
	a.localSet(slot)
	a.i32(3)
	a.i32(0)
	a.localGet(path)
	a.i32(8)
	a.add()
	a.localGet(path)
	a.loadI32(0)
	a.localGet(oflags)
	a.localGet(rights)
	a.localGet(rights)
	a.i32(0)
	a.localGet(slot)
	a.call(wasiPathOpen)
	a.ifI32()
	a.i32(-1)
	a.else_()
	a.localGet(slot)
	a.loadI32(0)
	a.end()
	a.end()
	return a.b
}

// encodeIOReadFile is (i32 path) -> i32.
// Local fd = 1.
func (e *encoder) encodeIOReadFile() []byte {
	const (
		path = 0
		fd   = 1
	)
	a := asm{b: []byte{0x01, 0x01, valI32}}
	a.localGet(path)
	a.i32(0)
	a.i64(fdReadRight)
	a.call(e.ioFn.open)
	a.localTee(fd)
	a.i32(-1)
	a.eq()
	a.ifVoid()
	e.exitFile(&a, errReadFile)
	a.end()
	a.localGet(fd)
	a.call(e.ioFn.readFd)
	a.localGet(fd)
	a.call(wasiFdClose)
	a.drop()
	a.end()
	return a.b
}

func (e *encoder) encodeIOFileExists() []byte {
	const (
		path = 0
		fd   = 1
	)
	a := asm{b: []byte{0x01, 0x01, valI32}}
	a.localGet(path)
	a.i32(0)
	a.i64(fdReadRight)
	a.call(e.ioFn.open)
	a.localTee(fd)
	a.i32(-1)
	a.eq()
	a.ifI32()
	a.i32(0)
	a.else_()
	a.localGet(fd)
	a.call(wasiFdClose)
	a.drop()
	a.i32(1)
	a.end()
	a.end()
	return a.b
}

// encodeIOWriteFile is (i32 path, i32 s) -> i32.
// Local fd = 2.
func (e *encoder) encodeIOWriteFile() []byte {
	const (
		path = 0
		s    = 1
		fd   = 2
	)
	a := asm{b: []byte{0x01, 0x01, valI32}}
	a.localGet(path)
	a.i32(oCreatTrunc)
	a.i64(fdWriteRight)
	a.call(e.ioFn.open)
	a.localTee(fd)
	a.i32(-1)
	a.eq()
	a.ifVoid()
	e.exitFile(&a, errWriteFile)
	a.end()
	a.localGet(fd)
	a.localGet(s)
	a.call(e.ioFn.write)
	a.drop()
	a.localGet(fd)
	a.call(wasiFdClose)
	a.drop()
	a.i32(0)
	a.end()
	return a.b
}

// exitFile writes prefix ++ path ++ newline to stderr and proc_exit(1).
// path is local 0. The sequence ends in unreachable.
func (e *encoder) exitFile(a *asm, prefix string) {
	a.i32(2)
	a.i32(int64(e.strConstAddr(prefix)))
	a.localGet(0)
	a.call(e.strFn.concat)
	a.i32(int64(e.strConstAddr(errNewline)))
	a.call(e.strFn.concat)
	a.call(e.ioFn.write)
	a.drop()
	a.i32(1)
	a.call(wasiProcExit)
	a.unreachable()
}

// encodeIOArgCount is () -> i64. It returns args_sizes_get's argc minus 1.
// Local slot = 0.
func encodeIOArgCount(allocIdx int) []byte {
	const slot = 0
	a := asm{b: []byte{0x01, 0x01, valI32}}
	a.i32(8)
	a.call(allocIdx)
	a.localSet(slot)
	a.localGet(slot)
	a.localGet(slot)
	a.i32(4)
	a.add()
	a.call(wasiArgsSizesGet)
	a.trapUnlessZero()
	a.localGet(slot)
	a.loadI32(0)
	a.extendU()
	a.i64(1)
	a.sub64()
	a.end()
	return a.b
}

// encodeIOArgAt is (i64 i) -> i32.
// Locals: slot = 1, argc = 2, bufSize = 3, argv = 4, buf = 5, p = 6, n = 7, s = 8.
func encodeIOArgAt(allocIdx int) []byte {
	const (
		i       = 0
		slot    = 1
		argc    = 2
		bufSize = 3
		argv    = 4
		buf     = 5
		p       = 6
		n       = 7
		s       = 8
	)
	a := asm{b: []byte{0x01, 0x08, valI32}}
	a.i32(8)
	a.call(allocIdx)
	a.localSet(slot)
	a.localGet(slot)
	a.localGet(slot)
	a.i32(4)
	a.add()
	a.call(wasiArgsSizesGet)
	a.trapUnlessZero()
	a.localGet(slot)
	a.loadI32(0)
	a.localSet(argc)
	a.localGet(slot)
	a.loadI32(4)
	a.localSet(bufSize)
	a.localGet(i)
	a.i64(0)
	a.ltS64()
	a.ifVoid()
	a.unreachable()
	a.end()
	a.localGet(i)
	a.localGet(argc)
	a.extendU()
	a.i64(1)
	a.sub64()
	a.geS64()
	a.ifVoid()
	a.unreachable()
	a.end()
	a.localGet(argc)
	a.i32(4)
	a.mul()
	a.i32(7)
	a.add()
	a.i32(-8)
	a.and()
	a.call(allocIdx)
	a.localSet(argv)
	a.localGet(bufSize)
	a.i32(7)
	a.add()
	a.i32(-8)
	a.and()
	a.localTee(buf)
	a.eqz()
	a.ifI32()
	a.i32(8)
	a.else_()
	a.localGet(buf)
	a.end()
	a.call(allocIdx)
	a.localSet(buf)
	a.localGet(argv)
	a.localGet(buf)
	a.call(wasiArgsGet)
	a.trapUnlessZero()
	a.localGet(argv)
	a.localGet(i)
	a.wrap()
	a.i32(1)
	a.add()
	a.i32(4)
	a.mul()
	a.add()
	a.loadI32(0)
	a.localSet(p)
	a.i32(0)
	a.localSet(n)
	a.block()
	a.loop()
	a.localGet(p)
	a.localGet(n)
	a.add()
	a.load8U(0)
	a.eqz()
	a.brIf(1)
	a.localGet(n)
	a.i32(1)
	a.add()
	a.localSet(n)
	a.br(0)
	a.end()
	a.end()
	a.localGet(n)
	a.i32(15)
	a.add()
	a.i32(-8)
	a.and()
	a.call(allocIdx)
	a.localSet(s)
	a.localGet(s)
	a.localGet(n)
	a.storeI32(0)
	a.localGet(s)
	a.i32(0)
	a.storeI32(4)
	a.localGet(s)
	a.i32(8)
	a.add()
	a.localGet(p)
	a.localGet(n)
	a.memCopy()
	a.localGet(s)
	a.end()
	return a.b
}

func encodeIOExit() []byte {
	a := asm{b: []byte{0x00}}
	a.localGet(0)
	a.wrap()
	a.call(wasiProcExit)
	a.unreachable()
	a.end()
	return a.b
}

// encodeStart calls main, calls the resulting IO[Unit] closure, and drops
// the unit value. Local 0 is the i64 scratch used to unpack the closure.
func (e *encoder) encodeStart(mainIdx int) []byte {
	a := asm{b: []byte{0x01, 0x01, valI64}}
	a.call(mainIdx)
	a.localTee(0)
	a.i64(32)
	a.raw(opI64ShrU)
	a.wrap()
	a.localGet(0)
	a.wrap()
	a.raw(opCallIndirect)
	a.u32(int(e.typeIndex(ir.Sig{Params: []ir.ValType{ir.Ptr}, Result: ir.Ptr})))
	a.raw(0x00)
	a.drop()
	a.end()
	return a.b
}

func (a *asm) blockSize(local int) {
	a.localGet(local)
	a.i32(15)
	a.add()
	a.i32(-8)
	a.and()
}

func (a *asm) drop()        { a.raw(opDrop) }
func (a *asm) unreachable() { a.raw(opUnreachable) }
func (a *asm) add()         { a.raw(opI32Add) }
func (a *asm) sub()         { a.raw(opI32Sub) }
func (a *asm) mul()         { a.raw(opI32Mul) }
func (a *asm) and()         { a.raw(opI32And) }
func (a *asm) shl()         { a.raw(opI32Shl) }
func (a *asm) eqz()         { a.raw(opI32Eqz) }
func (a *asm) eq()          { a.raw(opI32Eq) }
func (a *asm) gtU()         { a.raw(opI32GtU) }
func (a *asm) br(depth int) { a.raw(opBr); a.u32(depth) }
func (a *asm) brIf(depth int) {
	a.raw(opBrIf)
	a.u32(depth)
}
func (a *asm) block()   { a.raw(opBlock, blockEmpty) }
func (a *asm) loop()    { a.raw(opLoop, blockEmpty) }
func (a *asm) ifVoid()  { a.raw(opIf, blockEmpty) }
func (a *asm) ifI32()   { a.raw(opIf, valI32) }
func (a *asm) else_()   { a.raw(opElse) }
func (a *asm) wrap()    { a.raw(opI32WrapI64) }
func (a *asm) extendU() { a.raw(opI64ExtendI32U) }
func (a *asm) sub64()   { a.raw(opI64Sub) }
func (a *asm) ltS64()   { a.raw(opI64LtS) }
func (a *asm) geS64()   { a.raw(opI64GeS) }

// trapUnlessZero consumes an i32 and traps when it is not zero.
func (a *asm) trapUnlessZero() {
	a.eqz()
	a.eqz()
	a.ifVoid()
	a.unreachable()
	a.end()
}
