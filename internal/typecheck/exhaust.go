package typecheck

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/types"
)

type spatKind int

const (
	spWild spatKind = iota // '_' or a variable
	spCtor
	spInt
	spBool
)

// spat is a pattern reduced to what usefulness needs.
type spat struct {
	kind spatKind
	ctor *types.Ctor // spCtor
	args []spat      // spCtor: one per field
	ival int64       // spInt
	bval bool        // spBool
}

// headSet is the set of non-wildcard heads in one column.
// kind stays spWild when the column has no such head.
type headSet struct {
	kind      spatKind
	data      *types.Data
	ctors     map[*types.Ctor]bool
	trueSeen  bool
	falseSeen bool
}

// toSpat converts p; ctors maps constructor patterns to their constructors
// (Info.CtorPats).
func toSpat(p ast.Pattern, ctors map[*ast.CtorPat]*types.Ctor) spat {
	switch p := p.(type) {
	case *ast.WildcardPat, *ast.VarPat:
		return spat{kind: spWild}
	case *ast.IntPat:
		return spat{kind: spInt, ival: p.Value}
	case *ast.BoolPat:
		return spat{kind: spBool, bval: p.Value}
	case *ast.CtorPat:
		ctor := ctors[p]
		if ctor == nil {
			panic(fmt.Sprintf("typecheck: missing constructor for pattern '%s'", p.Name))
		}
		args := make([]spat, len(p.Args))
		for i, arg := range p.Args {
			args[i] = toSpat(arg, ctors)
		}
		return spat{kind: spCtor, ctor: ctor, args: args}
	default:
		panic(fmt.Sprintf("typecheck: unhandled pattern %T", p))
	}
}

// useful reports whether some value matched by q is matched by no row of rows.
// Every row and q have the same length.
func useful(rows [][]spat, q []spat) bool {
	if len(q) == 0 {
		return len(rows) == 0
	}
	h := q[0]
	if h.kind != spWild {
		return useful(specialize(h, rows), specializeQuery(h, q))
	}
	sig := columnHeads(rows)
	if complete(sig) {
		for _, c := range headsOf(sig) {
			if useful(specialize(c, rows), specializeQuery(c, q)) {
				return true
			}
		}
		return false
	}
	return useful(defaultMatrix(rows), q[1:])
}

// witness returns n patterns that together match a value matched by no row
// of rows, or false when every value is matched.
func witness(rows [][]spat, n int) ([]spat, bool) {
	if n == 0 {
		if len(rows) == 0 {
			return []spat{}, true
		}
		return nil, false
	}
	sig := columnHeads(rows)
	if complete(sig) {
		for _, c := range headsOf(sig) {
			w, ok := witness(specialize(c, rows), arity(c)+n-1)
			if !ok {
				continue
			}
			a := arity(c)
			head := c
			if head.kind == spCtor {
				head.args = append([]spat(nil), w[:a]...)
			}
			rest := append([]spat(nil), w[a:]...)
			return append([]spat{head}, rest...), true
		}
		return nil, false
	}
	w, ok := witness(defaultMatrix(rows), n-1)
	if !ok {
		return nil, false
	}
	return append([]spat{missingHead(sig)}, w...), true
}

// missingMessage returns "" when the one-column rows are exhaustive, and
// otherwise the text after "missing " in ADR-0009's format.
func missingMessage(rows [][]spat) string {
	sig := columnHeads(rows)
	if !hasWildcard(rows) && sig.kind != spWild && !complete(sig) {
		return formatMissing(sig)
	}
	w, ok := witness(rows, 1)
	if !ok {
		return ""
	}
	return formatSpat(w[0])
}

// formatSpat formats p in source syntax: "_", "42", "true", "Nil", "Cons(_, Nil)".
func formatSpat(p spat) string {
	switch p.kind {
	case spWild:
		return "_"
	case spInt:
		return strconv.FormatInt(p.ival, 10)
	case spBool:
		if p.bval {
			return "true"
		}
		return "false"
	case spCtor:
		if len(p.args) == 0 {
			return p.ctor.Name
		}
		parts := make([]string, len(p.args))
		for i, arg := range p.args {
			parts[i] = formatSpat(arg)
		}
		return p.ctor.Name + "(" + strings.Join(parts, ", ") + ")"
	default:
		panic(fmt.Sprintf("typecheck: unhandled pattern head %d", p.kind))
	}
}

func columnHeads(rows [][]spat) headSet {
	s := headSet{ctors: make(map[*types.Ctor]bool)}
	for _, row := range rows {
		h := row[0]
		if h.kind == spWild {
			continue
		}
		if s.kind == spWild {
			s.kind = h.kind
		} else if s.kind != h.kind {
			panic(fmt.Sprintf("typecheck: mixed pattern heads %d and %d", s.kind, h.kind))
		}
		switch h.kind {
		case spCtor:
			if h.ctor == nil {
				panic("typecheck: constructor pattern has no constructor")
			}
			if s.data == nil {
				s.data = h.ctor.Data
			} else if h.ctor.Data != s.data {
				panic("typecheck: mixed data types in one pattern column")
			}
			s.ctors[h.ctor] = true
		case spBool:
			if h.bval {
				s.trueSeen = true
			} else {
				s.falseSeen = true
			}
		case spInt:
		default:
			panic(fmt.Sprintf("typecheck: unhandled pattern head %d", h.kind))
		}
	}
	return s
}

func complete(s headSet) bool {
	switch s.kind {
	case spCtor:
		if s.data == nil || len(s.data.Ctors) == 0 {
			return false
		}
		for _, ctor := range s.data.Ctors {
			if !s.ctors[ctor] {
				return false
			}
		}
		return true
	case spBool:
		return s.trueSeen && s.falseSeen
	default:
		return false
	}
}

func headsOf(s headSet) []spat {
	switch s.kind {
	case spCtor:
		out := make([]spat, len(s.data.Ctors))
		for i, ctor := range s.data.Ctors {
			out[i] = spat{kind: spCtor, ctor: ctor}
		}
		return out
	case spBool:
		return []spat{
			{kind: spBool, bval: true},
			{kind: spBool, bval: false},
		}
	default:
		panic(fmt.Sprintf("typecheck: no head list for pattern kind %d", s.kind))
	}
}

func arity(h spat) int {
	switch h.kind {
	case spCtor:
		return len(h.ctor.Fields)
	case spInt, spBool:
		return 0
	default:
		panic(fmt.Sprintf("typecheck: arity of pattern kind %d", h.kind))
	}
}

func missingHead(s headSet) spat {
	switch s.kind {
	case spWild:
		return spat{kind: spWild}
	case spCtor:
		for _, ctor := range s.data.Ctors {
			if !s.ctors[ctor] {
				return wildArgs(ctor)
			}
		}
		panic("typecheck: constructor column is incomplete but has no missing constructor")
	case spBool:
		if !s.trueSeen {
			return spat{kind: spBool, bval: true}
		}
		return spat{kind: spBool, bval: false}
	case spInt:
		return spat{kind: spWild}
	default:
		panic(fmt.Sprintf("typecheck: unhandled pattern head %d", s.kind))
	}
}

func wildArgs(ctor *types.Ctor) spat {
	args := make([]spat, len(ctor.Fields))
	for i := range args {
		args[i] = spat{kind: spWild}
	}
	return spat{kind: spCtor, ctor: ctor, args: args}
}

func formatMissing(s headSet) string {
	switch s.kind {
	case spCtor:
		var parts []string
		for _, ctor := range s.data.Ctors {
			if s.ctors[ctor] {
				continue
			}
			parts = append(parts, formatSpat(wildArgs(ctor)))
		}
		return strings.Join(parts, ", ")
	case spBool:
		var parts []string
		if !s.trueSeen {
			parts = append(parts, "true")
		}
		if !s.falseSeen {
			parts = append(parts, "false")
		}
		return strings.Join(parts, ", ")
	case spInt:
		return "_"
	default:
		panic(fmt.Sprintf("typecheck: cannot format missing patterns of kind %d", s.kind))
	}
}

func hasWildcard(rows [][]spat) bool {
	for _, row := range rows {
		if row[0].kind == spWild {
			return true
		}
	}
	return false
}

// specialize returns S(h, rows): rows whose first head is h, with that
// head's arguments in front, plus wildcard rows expanded by h's arity.
func specialize(h spat, rows [][]spat) [][]spat {
	a := arity(h)
	var out [][]spat
	for _, row := range rows {
		head := row[0]
		rest := row[1:]
		if sameHead(head, h) {
			out = append(out, joinSpat(head.args, rest))
			continue
		}
		if head.kind == spWild {
			expanded := make([]spat, 0, a+len(rest))
			for i := 0; i < a; i++ {
				expanded = append(expanded, spat{kind: spWild})
			}
			expanded = append(expanded, rest...)
			out = append(out, expanded)
		}
	}
	return out
}

func specializeQuery(h spat, q []spat) []spat {
	spec := specialize(h, [][]spat{q})
	if len(spec) != 1 {
		panic("typecheck: specialization dropped the query pattern")
	}
	return spec[0]
}

func defaultMatrix(rows [][]spat) [][]spat {
	var out [][]spat
	for _, row := range rows {
		if row[0].kind == spWild {
			out = append(out, joinSpat(row[1:], nil))
		}
	}
	return out
}

func sameHead(a, b spat) bool {
	if a.kind != b.kind || a.kind == spWild {
		return false
	}
	switch a.kind {
	case spCtor:
		return a.ctor == b.ctor
	case spInt:
		return a.ival == b.ival
	case spBool:
		return a.bval == b.bval
	default:
		return false
	}
}

func joinSpat(a, b []spat) []spat {
	out := make([]spat, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
