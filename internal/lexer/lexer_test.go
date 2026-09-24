package lexer

import (
	"testing"

	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/token"
)

func TestTokenize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []token.Token
	}{
		{
			name: "empty",
			src:  "",
			want: []token.Token{eof(1, 1)},
		},
		{
			name: "keywords and identifiers",
			src:  "fn let if else true false type match _ foo _bar __ x1",
			want: []token.Token{
				tok(token.Fn, "fn", 1, 1),
				tok(token.Let, "let", 1, 4),
				tok(token.If, "if", 1, 8),
				tok(token.Else, "else", 1, 11),
				tok(token.True, "true", 1, 16),
				tok(token.False, "false", 1, 21),
				tok(token.Type, "type", 1, 27),
				tok(token.Match, "match", 1, 32),
				tok(token.Underscore, "_", 1, 38),
				tok(token.Ident, "foo", 1, 40),
				tok(token.Ident, "_bar", 1, 44),
				tok(token.Ident, "__", 1, 49),
				tok(token.Ident, "x1", 1, 52),
				eof(1, 54),
			},
		},
		{
			name: "integers",
			src:  "0 42 007 9223372036854775807",
			want: []token.Token{
				tok(token.Int, "0", 1, 1),
				tok(token.Int, "42", 1, 3),
				tok(token.Int, "007", 1, 6),
				tok(token.Int, "9223372036854775807", 1, 10),
				eof(1, 29),
			},
		},
		{
			name: "symbols",
			src:  "( ) { } , : -> = + - * / == != < <= > >= && || !",
			want: []token.Token{
				tok(token.LParen, "(", 1, 1),
				tok(token.RParen, ")", 1, 3),
				tok(token.LBrace, "{", 1, 5),
				tok(token.RBrace, "}", 1, 7),
				tok(token.Comma, ",", 1, 9),
				tok(token.Colon, ":", 1, 11),
				tok(token.Arrow, "->", 1, 13),
				tok(token.Assign, "=", 1, 16),
				tok(token.Plus, "+", 1, 18),
				tok(token.Minus, "-", 1, 20),
				tok(token.Star, "*", 1, 22),
				tok(token.Slash, "/", 1, 24),
				tok(token.Eq, "==", 1, 26),
				tok(token.NotEq, "!=", 1, 29),
				tok(token.Lt, "<", 1, 32),
				tok(token.LtEq, "<=", 1, 34),
				tok(token.Gt, ">", 1, 37),
				tok(token.GtEq, ">=", 1, 39),
				tok(token.AndAnd, "&&", 1, 42),
				tok(token.OrOr, "||", 1, 45),
				tok(token.Bang, "!", 1, 48),
				eof(1, 49),
			},
		},
		{
			name: "fat arrow and bar",
			src:  "=> | = == || ->",
			want: []token.Token{
				tok(token.FatArrow, "=>", 1, 1),
				tok(token.Bar, "|", 1, 4),
				tok(token.Assign, "=", 1, 6),
				tok(token.Eq, "==", 1, 8),
				tok(token.OrOr, "||", 1, 11),
				tok(token.Arrow, "->", 1, 14),
				eof(1, 16),
			},
		},
		{
			name: "fat arrow between idents",
			src:  "a=>b",
			want: []token.Token{
				tok(token.Ident, "a", 1, 1),
				tok(token.FatArrow, "=>", 1, 2),
				tok(token.Ident, "b", 1, 4),
				eof(1, 5),
			},
		},
		{
			name: "bar between idents",
			src:  "a|b",
			want: []token.Token{
				tok(token.Ident, "a", 1, 1),
				tok(token.Bar, "|", 1, 2),
				tok(token.Ident, "b", 1, 3),
				eof(1, 4),
			},
		},
		{
			name: "lone bar",
			src:  "a | b",
			want: []token.Token{
				tok(token.Ident, "a", 1, 1),
				tok(token.Bar, "|", 1, 3),
				tok(token.Ident, "b", 1, 5),
				eof(1, 6),
			},
		},
		{
			name: "arrow between idents",
			src:  "a->b",
			want: []token.Token{
				tok(token.Ident, "a", 1, 1),
				tok(token.Arrow, "->", 1, 2),
				tok(token.Ident, "b", 1, 4),
				eof(1, 5),
			},
		},
		{
			name: "minus between idents",
			src:  "a-b",
			want: []token.Token{
				tok(token.Ident, "a", 1, 1),
				tok(token.Minus, "-", 1, 2),
				tok(token.Ident, "b", 1, 3),
				eof(1, 4),
			},
		},
		{
			name: "minus and gt separated by space",
			src:  "a- >b",
			want: []token.Token{
				tok(token.Ident, "a", 1, 1),
				tok(token.Minus, "-", 1, 2),
				tok(token.Gt, ">", 1, 4),
				tok(token.Ident, "b", 1, 5),
				eof(1, 6),
			},
		},
		{
			name: "multiline tab comment and multibyte",
			src:  "fn\n\tx //あ\n+",
			want: []token.Token{
				tok(token.Fn, "fn", 1, 1),
				tok(token.Ident, "x", 2, 2),
				tok(token.Plus, "+", 3, 1),
				eof(3, 2),
			},
		},
		{
			name: "comment only next column",
			src:  "// only",
			want: []token.Token{eof(1, 8)},
		},
		{
			name: "comment only with newline",
			src:  "// only\n",
			want: []token.Token{eof(2, 1)},
		},
		{
			name: "multibyte comment column",
			src:  "a//あ",
			want: []token.Token{
				tok(token.Ident, "a", 1, 1),
				eof(1, 5),
			},
		},
		{
			name: "multibyte comment at eof",
			src:  "//あ",
			want: []token.Token{eof(1, 4)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Tokenize([]byte(tt.src))
			if err != nil {
				t.Fatalf("Tokenize(%q) error = %v", tt.src, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Tokenize(%q) tokens = %d, want %d\n got %#v\nwant %#v", tt.src, len(got), len(tt.want), got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("token %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTokenizeError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  []byte
		want string
	}{
		{
			name: "hash",
			src:  []byte("#"),
			want: "1:1: unexpected character '#'",
		},
		{
			name: "lone ampersand",
			src:  []byte("a & b"),
			want: "1:3: unexpected character '&'",
		},
		{
			name: "multibyte character",
			src:  []byte("あ"),
			want: "1:1: unexpected character 'あ'",
		},
		{
			name: "integer out of range",
			src:  []byte("9223372036854775808"),
			want: "1:1: integer literal out of range: 9223372036854775808",
		},
		{
			name: "invalid integer literal",
			src:  []byte("12abc"),
			want: `1:1: invalid integer literal "12abc"`,
		},
		{
			name: "invalid utf-8",
			src:  []byte{0xff},
			want: "1:1: invalid UTF-8 encoding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Tokenize(tt.src)
			requireDiag(t, err, tt.want)
		})
	}
}

func tok(kind token.Kind, text string, line, col int) token.Token {
	return token.Token{Kind: kind, Text: text, Pos: diag.Pos{Line: line, Col: col}}
}

func eof(line, col int) token.Token {
	return tok(token.EOF, "", line, col)
}

func requireDiag(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want %q", want)
	}
	if _, ok := err.(*diag.Error); !ok {
		t.Fatalf("error type = %T, want *diag.Error", err)
	}
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}
