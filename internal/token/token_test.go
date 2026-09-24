package token

import "testing"

func TestKindString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind Kind
		want string
	}{
		{name: "EOF", kind: EOF, want: "end of file"},
		{name: "Ident", kind: Ident, want: "identifier"},
		{name: "Int", kind: Int, want: "integer literal"},
		{name: "Fn", kind: Fn, want: "'fn'"},
		{name: "Type", kind: Type, want: "'type'"},
		{name: "Match", kind: Match, want: "'match'"},
		{name: "Underscore", kind: Underscore, want: "'_'"},
		{name: "Arrow", kind: Arrow, want: "'->'"},
		{name: "FatArrow", kind: FatArrow, want: "'=>'"},
		{name: "Bar", kind: Bar, want: "'|'"},
		{name: "AndAnd", kind: AndAnd, want: "'&&'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.kind.String(); got != tt.want {
				t.Errorf("Kind.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
