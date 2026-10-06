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
			name: "import and pub keywords",
			src:  "import pub imported public",
			want: []token.Token{
				tok(token.Import, "import", 1, 1),
				tok(token.Pub, "pub", 1, 8),
				tok(token.Ident, "imported", 1, 12),
				tok(token.Ident, "public", 1, 21),
				eof(1, 27),
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
			name: "brackets",
			src:  "[ ]",
			want: []token.Token{
				tok(token.LBracket, "[", 1, 1),
				tok(token.RBracket, "]", 1, 3),
				eof(1, 4),
			},
		},
		{
			name: "type application",
			src:  "Option[Int]",
			want: []token.Token{
				tok(token.Ident, "Option", 1, 1),
				tok(token.LBracket, "[", 1, 7),
				tok(token.Ident, "Int", 1, 8),
				tok(token.RBracket, "]", 1, 11),
				eof(1, 12),
			},
		},
		{
			name: "brackets with comma",
			src:  "a[b,c]",
			want: []token.Token{
				tok(token.Ident, "a", 1, 1),
				tok(token.LBracket, "[", 1, 2),
				tok(token.Ident, "b", 1, 3),
				tok(token.Comma, ",", 1, 4),
				tok(token.Ident, "c", 1, 5),
				tok(token.RBracket, "]", 1, 6),
				eof(1, 7),
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
		{
			name: "empty string",
			src:  `""`,
			want: []token.Token{
				tok(token.String, "", 1, 1),
				eof(1, 3),
			},
		},
		{
			name: "escape n",
			src:  `"\n"`,
			want: []token.Token{
				tok(token.String, "\n", 1, 1),
				eof(1, 5),
			},
		},
		{
			name: "escape t",
			src:  `"\t"`,
			want: []token.Token{
				tok(token.String, "\t", 1, 1),
				eof(1, 5),
			},
		},
		{
			name: "escape r",
			src:  `"\r"`,
			want: []token.Token{
				tok(token.String, "\r", 1, 1),
				eof(1, 5),
			},
		},
		{
			name: "escape backslash",
			src:  `"\\"`,
			want: []token.Token{
				tok(token.String, `\`, 1, 1),
				eof(1, 5),
			},
		},
		{
			name: "escape quote",
			src:  `"\""`,
			want: []token.Token{
				tok(token.String, `"`, 1, 1),
				eof(1, 5),
			},
		},
		{
			name: "escape nul",
			src:  `"\0"`,
			want: []token.Token{
				tok(token.String, "\x00", 1, 1),
				eof(1, 5),
			},
		},
		{
			name: "escape hex 41",
			src:  `"\x41"`,
			want: []token.Token{
				tok(token.String, "A", 1, 1),
				eof(1, 7),
			},
		},
		{
			name: "escape hex ff",
			src:  `"\xff"`,
			want: []token.Token{
				tok(token.String, "\xff", 1, 1),
				eof(1, 7),
			},
		},
		{
			name: "escape hex uppercase",
			src:  `"\xFF"`,
			want: []token.Token{
				tok(token.String, "\xff", 1, 1),
				eof(1, 7),
			},
		},
		{
			name: "utf-8 string",
			src:  "\"é\"",
			want: []token.Token{
				tok(token.String, "\xc3\xa9", 1, 1),
				eof(1, 4),
			},
		},
		{
			name: "plus plus",
			src:  "++",
			want: []token.Token{
				tok(token.PlusPlus, "++", 1, 1),
				eof(1, 3),
			},
		},
		{
			name: "plus plus separated by space",
			src:  "+ +",
			want: []token.Token{
				tok(token.Plus, "+", 1, 1),
				tok(token.Plus, "+", 1, 3),
				eof(1, 4),
			},
		},
		{
			name: "percent",
			src:  "a%b",
			want: []token.Token{
				tok(token.Ident, "a", 1, 1),
				tok(token.Percent, "%", 1, 2),
				tok(token.Ident, "b", 1, 3),
				eof(1, 4),
			},
		},
		{
			name: "character a",
			src:  `'a'`,
			want: []token.Token{
				tok(token.Char, "a", 1, 1),
				eof(1, 4),
			},
		},
		{
			name: "character newline escape",
			src:  `'\n'`,
			want: []token.Token{
				tok(token.Char, "\n", 1, 1),
				eof(1, 5),
			},
		},
		{
			name: "character quote escape",
			src:  `'\''`,
			want: []token.Token{
				tok(token.Char, "'", 1, 1),
				eof(1, 5),
			},
		},
		{
			name: "character hex",
			src:  `'\x41'`,
			want: []token.Token{
				tok(token.Char, "A", 1, 1),
				eof(1, 7),
			},
		},
		{
			name: "character double quote",
			src:  `'"'`,
			want: []token.Token{
				tok(token.Char, `"`, 1, 1),
				eof(1, 4),
			},
		},
		{
			name: "string escaped single quote",
			src:  `"\'"`,
			want: []token.Token{
				tok(token.String, "'", 1, 1),
				eof(1, 5),
			},
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
		{
			name: "unterminated string",
			src:  []byte("  \"abc"),
			want: "1:3: unterminated string literal",
		},
		{
			name: "newline in string",
			src:  []byte(" \"a\n\""),
			want: "1:2: newline in string literal",
		},
		{
			name: "invalid escape",
			src:  []byte(`"\q"`),
			want: `1:2: invalid escape sequence '\q'`,
		},
		{
			name: "invalid hex escape",
			src:  []byte(`"\x"`),
			want: `1:2: invalid escape sequence '\x'`,
		},
		{
			name: "invalid hex escape one digit",
			src:  []byte(`"\x4"`),
			want: `1:2: invalid escape sequence '\x'`,
		},
		{
			name: "invalid hex escape non-hex",
			src:  []byte(`"\xGG"`),
			want: `1:2: invalid escape sequence '\x'`,
		},
		{
			name: "unterminated character at eof",
			src:  []byte("'a"),
			want: "1:1: unterminated character literal",
		},
		{
			name: "unterminated character with newline",
			src:  []byte("'a\n'"),
			want: "1:1: unterminated character literal",
		},
		{
			name: "empty character",
			src:  []byte("''"),
			want: "1:1: empty character literal",
		},
		{
			name: "character two bytes",
			src:  []byte("'ab'"),
			want: "1:1: character literal must be a single byte",
		},
		{
			name: "character multibyte",
			src:  []byte("'é'"),
			want: "1:1: character literal must be a single byte",
		},
		{
			name: "invalid character escape",
			src:  []byte(`'\q'`),
			want: `1:2: invalid escape sequence '\q'`,
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
