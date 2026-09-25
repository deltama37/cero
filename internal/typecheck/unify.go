package typecheck

import "github.com/deltama37/cero/internal/types"

// unify makes a and b equal by solving unification variables and reports
// whether it succeeded. On failure some variables may already have been
// solved; the caller reports an error and stops.
func unify(a, b types.Type) bool {
	a, b = types.Prune(a), types.Prune(b)
	if ma, ok := a.(*types.Meta); ok {
		if mb, ok := b.(*types.Meta); ok && ma == mb {
			return true
		}
		if types.Mentions(b, a) {
			return false
		}
		ma.Solution = b
		return true
	}
	if mb, ok := b.(*types.Meta); ok {
		if types.Mentions(a, b) {
			return false
		}
		mb.Solution = a
		return true
	}
	switch a := a.(type) {
	case types.IntType:
		_, ok := b.(types.IntType)
		return ok
	case types.BoolType:
		_, ok := b.(types.BoolType)
		return ok
	case *types.TypeParam:
		tb, ok := b.(*types.TypeParam)
		return ok && a == tb
	case *types.Func:
		fb, ok := b.(*types.Func)
		if !ok || len(fb.Params) != len(a.Params) {
			return false
		}
		for i := range a.Params {
			if !unify(a.Params[i], fb.Params[i]) {
				return false
			}
		}
		return unify(a.Result, fb.Result)
	case *types.Named:
		nb, ok := b.(*types.Named)
		if !ok || nb.Data != a.Data || len(nb.Args) != len(a.Args) {
			return false
		}
		for i := range a.Args {
			if !unify(a.Args[i], nb.Args[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
