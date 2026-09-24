// Package diag defines source positions and compile errors.
package diag

import "fmt"

// Pos is a 1-based source position. Col counts Unicode code points.
type Pos struct {
	Line int
	Col  int
}

// String returns "line:col".
func (p Pos) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

// Error is a compile error at a source position.
// File is empty until the driver fills it in.
type Error struct {
	File string
	Pos  Pos
	Msg  string
}

// Error returns "file:line:col: msg", or "line:col: msg" when File is empty.
func (e *Error) Error() string {
	if e.File == "" {
		return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
	}
	return fmt.Sprintf("%s:%s: %s", e.File, e.Pos, e.Msg)
}

// Errorf builds an *Error with an empty File.
func Errorf(pos Pos, format string, args ...any) *Error {
	return &Error{
		Pos: pos,
		Msg: fmt.Sprintf(format, args...),
	}
}
