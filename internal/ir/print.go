package ir

import (
	"fmt"
	"strconv"
	"strings"
)

// Format renders m as one line per function, then the table, then main.
// Lines are joined with "\n" and there is no trailing newline.
func Format(m *Module) string {
	lines := make([]string, 0, len(m.Funcs)+2)
	for i, fn := range m.Funcs {
		lines = append(lines, formatFunc(FuncID(i), fn))
	}
	lines = append(lines, formatTable(m.Table))
	lines = append(lines, fmt.Sprintf("(main %d)", m.Main))
	return strings.Join(lines, "\n")
}

// FormatExpr renders a single expression.
func FormatExpr(e Expr) string {
	switch e := e.(type) {
	case *IntConst:
		return strconv.FormatInt(e.Value, 10)
	case *BoolConst:
		if e.Value {
			return "true"
		}
		return "false"
	case *LocalGet:
		return fmt.Sprintf("(local %d)", e.Local)
	case *FuncValue:
		if e.Env == nil {
			return fmt.Sprintf("(func.ref %d)", e.Func)
		}
		return fmt.Sprintf("(func.ref %d %s)", e.Func, FormatExpr(e.Env))
	case *Unary:
		return "(" + unaryName(e.Op) + " " + FormatExpr(e.X) + ")"
	case *Binary:
		return "(" + binaryName(e.Op) + " " + FormatExpr(e.X) + " " + FormatExpr(e.Y) + ")"
	case *If:
		return "(if " + FormatExpr(e.Cond) + " " + FormatExpr(e.Then) + " " + FormatExpr(e.Else) + ")"
	case *Block:
		parts := make([]string, 0, len(e.Lets)+1)
		for _, let := range e.Lets {
			parts = append(parts, "(let "+strconv.Itoa(int(let.Local))+" "+FormatExpr(let.Value)+")")
		}
		parts = append(parts, FormatExpr(e.Result))
		return "(block " + strings.Join(parts, " ") + ")"
	case *Call:
		name := "call"
		if e.Tail {
			name = "return.call"
		}
		parts := make([]string, 0, 1+len(e.Args))
		parts = append(parts, strconv.Itoa(int(e.Func)))
		for _, arg := range e.Args {
			parts = append(parts, FormatExpr(arg))
		}
		return "(" + name + " " + strings.Join(parts, " ") + ")"
	case *CallIndirect:
		name := "call.indirect"
		if e.Tail {
			name = "return.call.indirect"
		}
		parts := make([]string, 0, 2+len(e.Args))
		parts = append(parts, FormatSig(e.Sig), FormatExpr(e.Callee))
		for _, arg := range e.Args {
			parts = append(parts, FormatExpr(arg))
		}
		return "(" + name + " " + strings.Join(parts, " ") + ")"
	case *Construct:
		parts := make([]string, 0, 1+len(e.Fields))
		parts = append(parts, strconv.Itoa(e.Tag))
		for _, field := range e.Fields {
			parts = append(parts, FormatExpr(field))
		}
		return "(construct " + strings.Join(parts, " ") + ")"
	case *Field:
		return fmt.Sprintf("(field %d %d)", e.Local, e.Index)
	case *SwitchTag:
		parts := make([]string, 0, 1+len(e.Cases)+1)
		parts = append(parts, strconv.Itoa(int(e.Local)))
		for _, c := range e.Cases {
			parts = append(parts, "(case "+strconv.Itoa(c.Tag)+" "+FormatExpr(c.Body)+")")
		}
		if e.Default != nil {
			parts = append(parts, "(default "+FormatExpr(e.Default)+")")
		}
		return "(switch.tag " + strings.Join(parts, " ") + ")"
	case *StrConst:
		return "(str " + strconv.Quote(e.Value) + ")"
	case *Prim:
		parts := make([]string, 0, 1+len(e.Args))
		parts = append(parts, primName(e.Op))
		for _, arg := range e.Args {
			parts = append(parts, FormatExpr(arg))
		}
		return "(prim " + strings.Join(parts, " ") + ")"
	default:
		panic(fmt.Sprintf("ir.FormatExpr: unhandled type %T", e))
	}
}

// FormatSig renders a signature as "(sig (P...) R)".
func FormatSig(s Sig) string {
	params := make([]string, len(s.Params))
	for i, p := range s.Params {
		params[i] = p.String()
	}
	return "(sig (" + strings.Join(params, " ") + ") " + s.Result.String() + ")"
}

func formatFunc(id FuncID, fn *Func) string {
	return "(func " + strconv.Itoa(int(id)) + " " + fn.Name + " " + FormatSig(fn.Sig) + " " + formatLocals(fn.Locals) + " " + FormatExpr(fn.Body) + ")"
}

func formatLocals(locals []ValType) string {
	if len(locals) == 0 {
		return "(locals)"
	}
	parts := make([]string, len(locals))
	for i, t := range locals {
		parts[i] = t.String()
	}
	return "(locals " + strings.Join(parts, " ") + ")"
}

func formatTable(table []FuncID) string {
	if len(table) == 0 {
		return "(table)"
	}
	parts := make([]string, len(table))
	for i, id := range table {
		parts[i] = strconv.Itoa(int(id))
	}
	return "(table " + strings.Join(parts, " ") + ")"
}

func unaryName(op UnOp) string {
	switch op {
	case Neg:
		return "neg"
	case Not:
		return "not"
	default:
		panic(fmt.Sprintf("ir.FormatExpr: unknown unary operator %d", int(op)))
	}
}

func primName(op PrimOp) string {
	switch op {
	case StrLength:
		return "string.length"
	case StrByteAt:
		return "string.byte_at"
	case StrSlice:
		return "string.slice"
	case StrFromByte:
		return "string.from_byte"
	case StrCompare:
		return "string.compare"
	case IntToString:
		return "int.to_string"
	case StrConcat:
		return "string.concat"
	case StrEq:
		return "string.eq"
	case IOPrint:
		return "io.print"
	case IOEPrint:
		return "io.eprint"
	case IOReadStdin:
		return "io.read_stdin"
	case IOReadFile:
		return "io.read_file"
	case IOFileExists:
		return "io.file_exists"
	case IOWriteFile:
		return "io.write_file"
	case IOArgCount:
		return "io.arg_count"
	case IOArgAt:
		return "io.arg_at"
	case IOExit:
		return "io.exit"
	case BitAnd:
		return "int.and"
	case BitOr:
		return "int.or"
	case BitXor:
		return "int.xor"
	case ShiftLeft:
		return "int.shl"
	case ShiftRight:
		return "int.shr_s"
	case ShiftRightUnsigned:
		return "int.shr_u"
	case IOReadStdinChunk:
		return "io.read_stdin_chunk"
	default:
		panic(fmt.Sprintf("ir.FormatExpr: unknown prim %d", int(op)))
	}
}

func binaryName(op BinOp) string {
	switch op {
	case Add:
		return "add"
	case Sub:
		return "sub"
	case Mul:
		return "mul"
	case Div:
		return "div"
	case Rem:
		return "rem"
	case Eq:
		return "eq"
	case Ne:
		return "ne"
	case Lt:
		return "lt"
	case Le:
		return "le"
	case Gt:
		return "gt"
	case Ge:
		return "ge"
	default:
		panic(fmt.Sprintf("ir.FormatExpr: unknown binary operator %d", int(op)))
	}
}
