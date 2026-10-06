// Package lexer tokenizes Cero source.
package lexer

import (
	"strconv"
	"unicode/utf8"

	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/token"
)

// Tokenize splits src into tokens. The last token is always EOF, positioned
// just after the last character (line/col of the next character that would appear).
func Tokenize(src []byte) ([]token.Token, error) {
	if !utf8.Valid(src) {
		return nil, diag.Errorf(diag.Pos{Line: 1, Col: 1}, "invalid UTF-8 encoding")
	}

	l := &lexer{src: src, line: 1, col: 1}
	var toks []token.Token
	for {
		l.skipSpaceAndComments()
		if l.done() {
			break
		}
		tok, err := l.scan()
		if err != nil {
			return nil, err
		}
		toks = append(toks, tok)
	}
	toks = append(toks, token.Token{
		Kind: token.EOF,
		Pos:  diag.Pos{Line: l.line, Col: l.col},
	})
	return toks, nil
}

type lexer struct {
	src  []byte
	i    int
	line int
	col  int
}

func (l *lexer) done() bool {
	return l.i >= len(l.src)
}

func (l *lexer) peek() rune {
	if l.done() {
		return 0
	}
	r, _ := utf8.DecodeRune(l.src[l.i:])
	return r
}

func (l *lexer) peekAhead() rune {
	if l.done() {
		return 0
	}
	_, size := utf8.DecodeRune(l.src[l.i:])
	if l.i+size >= len(l.src) {
		return 0
	}
	r, _ := utf8.DecodeRune(l.src[l.i+size:])
	return r
}

func (l *lexer) advance() rune {
	r, size := utf8.DecodeRune(l.src[l.i:])
	l.i += size
	if r == '\n' {
		l.line++
		l.col = 1
		return r
	}
	l.col++
	return r
}

func (l *lexer) skipSpaceAndComments() {
	for !l.done() {
		r := l.peek()
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			l.advance()
			continue
		}
		if r == '/' && l.peekAhead() == '/' {
			l.advance()
			l.advance()
			for !l.done() && l.peek() != '\n' {
				l.advance()
			}
			continue
		}
		return
	}
}

func (l *lexer) scan() (token.Token, error) {
	start := diag.Pos{Line: l.line, Col: l.col}
	r := l.peek()
	switch {
	case isIdentStart(r):
		return l.scanIdent(start), nil
	case isDigit(r):
		return l.scanNumber(start)
	case r == '"':
		return l.scanString(start)
	default:
		return l.scanSymbol(start)
	}
}

func (l *lexer) scanString(start diag.Pos) (token.Token, error) {
	l.advance()
	var buf []byte
	for {
		if l.done() {
			return token.Token{}, diag.Errorf(start, "unterminated string literal")
		}
		r := l.peek()
		if r == '"' {
			l.advance()
			return token.Token{Kind: token.String, Text: string(buf), Pos: start}, nil
		}
		if r == '\n' {
			return token.Token{}, diag.Errorf(start, "newline in string literal")
		}
		if r == '\\' {
			escPos := diag.Pos{Line: l.line, Col: l.col}
			l.advance()
			if l.done() {
				return token.Token{}, diag.Errorf(start, "unterminated string literal")
			}
			if l.peek() == '\n' {
				return token.Token{}, diag.Errorf(start, "newline in string literal")
			}
			b, err := l.scanEscape(escPos)
			if err != nil {
				return token.Token{}, err
			}
			buf = append(buf, b)
			continue
		}
		begin := l.i
		l.advance()
		buf = append(buf, l.src[begin:l.i]...)
	}
}

func (l *lexer) scanEscape(escPos diag.Pos) (byte, error) {
	switch l.peek() {
	case 'n':
		l.advance()
		return '\n', nil
	case 't':
		l.advance()
		return '\t', nil
	case 'r':
		l.advance()
		return '\r', nil
	case '\\':
		l.advance()
		return '\\', nil
	case '"':
		l.advance()
		return '"', nil
	case '0':
		l.advance()
		return 0, nil
	case 'x':
		l.advance()
		hi, ok := l.readHex()
		if !ok {
			return 0, diag.Errorf(escPos, "invalid escape sequence '\\x'")
		}
		lo, ok := l.readHex()
		if !ok {
			return 0, diag.Errorf(escPos, "invalid escape sequence '\\x'")
		}
		return hi<<4 | lo, nil
	default:
		r := l.peek()
		l.advance()
		return 0, diag.Errorf(escPos, "invalid escape sequence '\\%c'", r)
	}
}

func (l *lexer) readHex() (byte, bool) {
	if l.done() {
		return 0, false
	}
	v, ok := hexValue(l.peek())
	if !ok {
		return 0, false
	}
	l.advance()
	return v, true
}

func (l *lexer) scanIdent(start diag.Pos) token.Token {
	begin := l.i
	l.advance()
	for !l.done() && isIdentPart(l.peek()) {
		l.advance()
	}
	text := string(l.src[begin:l.i])
	kind := token.Ident
	if k, ok := token.Keywords[text]; ok {
		kind = k
	}
	return token.Token{Kind: kind, Text: text, Pos: start}
}

func (l *lexer) scanNumber(start diag.Pos) (token.Token, error) {
	begin := l.i
	for !l.done() && isDigit(l.peek()) {
		l.advance()
	}
	if !l.done() && isIdentPart(l.peek()) {
		for !l.done() && isIdentPart(l.peek()) {
			l.advance()
		}
		text := string(l.src[begin:l.i])
		return token.Token{}, diag.Errorf(start, "invalid integer literal %q", text)
	}
	text := string(l.src[begin:l.i])
	if _, err := strconv.ParseInt(text, 10, 64); err != nil {
		return token.Token{}, diag.Errorf(start, "integer literal out of range: %s", text)
	}
	return token.Token{Kind: token.Int, Text: text, Pos: start}, nil
}

func (l *lexer) scanSymbol(start diag.Pos) (token.Token, error) {
	r := l.peek()
	next := l.peekAhead()
	if kind, text, ok := matchTwo(r, next); ok {
		l.advance()
		l.advance()
		return token.Token{Kind: kind, Text: text, Pos: start}, nil
	}
	if kind, text, ok := matchOne(r); ok {
		l.advance()
		return token.Token{Kind: kind, Text: text, Pos: start}, nil
	}
	return token.Token{}, diag.Errorf(start, "unexpected character %q", r)
}

func matchTwo(r, next rune) (token.Kind, string, bool) {
	switch {
	case r == '-' && next == '>':
		return token.Arrow, "->", true
	case r == '=' && next == '=':
		return token.Eq, "==", true
	case r == '=' && next == '>':
		return token.FatArrow, "=>", true
	case r == '!' && next == '=':
		return token.NotEq, "!=", true
	case r == '<' && next == '=':
		return token.LtEq, "<=", true
	case r == '>' && next == '=':
		return token.GtEq, ">=", true
	case r == '&' && next == '&':
		return token.AndAnd, "&&", true
	case r == '|' && next == '|':
		return token.OrOr, "||", true
	case r == '+' && next == '+':
		return token.PlusPlus, "++", true
	default:
		return 0, "", false
	}
}

func matchOne(r rune) (token.Kind, string, bool) {
	switch r {
	case '(':
		return token.LParen, "(", true
	case ')':
		return token.RParen, ")", true
	case '{':
		return token.LBrace, "{", true
	case '}':
		return token.RBrace, "}", true
	case '[':
		return token.LBracket, "[", true
	case ']':
		return token.RBracket, "]", true
	case ',':
		return token.Comma, ",", true
	case ':':
		return token.Colon, ":", true
	case '=':
		return token.Assign, "=", true
	case '|':
		return token.Bar, "|", true
	case '+':
		return token.Plus, "+", true
	case '-':
		return token.Minus, "-", true
	case '*':
		return token.Star, "*", true
	case '/':
		return token.Slash, "/", true
	case '<':
		return token.Lt, "<", true
	case '>':
		return token.Gt, ">", true
	case '!':
		return token.Bang, "!", true
	default:
		return 0, "", false
	}
}

func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || isDigit(r)
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func hexValue(r rune) (byte, bool) {
	switch {
	case r >= '0' && r <= '9':
		return byte(r - '0'), true
	case r >= 'a' && r <= 'f':
		return byte(r-'a') + 10, true
	case r >= 'A' && r <= 'F':
		return byte(r-'A') + 10, true
	default:
		return 0, false
	}
}
