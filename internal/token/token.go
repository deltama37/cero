// Package token defines the lexical tokens of Cero source.
package token

import (
	"fmt"

	"github.com/deltama37/cero/internal/diag"
)

// Kind is the kind of a lexical token.
type Kind int

const (
	EOF   Kind = iota
	Ident      // foo
	Int        // 123

	// Keywords
	Fn         // fn
	Let        // let
	If         // if
	Else       // else
	True       // true
	False      // false
	Type       // type
	Match      // match
	Underscore // _

	// Punctuation
	LParen   // (
	RParen   // )
	LBrace   // {
	RBrace   // }
	Comma    // ,
	Colon    // :
	Arrow    // ->
	Assign   // =
	FatArrow // =>
	Bar      // |

	// Operators
	Plus   // +
	Minus  // -
	Star   // *
	Slash  // /
	Eq     // ==
	NotEq  // !=
	Lt     // <
	LtEq   // <=
	Gt     // >
	GtEq   // >=
	AndAnd // &&
	OrOr   // ||
	Bang   // !
)

// String returns the name used in error messages:
//
//	EOF    -> "end of file"
//	Ident  -> "identifier"
//	Int    -> "integer literal"
//	others -> the source text in single quotes, e.g. "'fn'", "'('", "'->'", "'&&'"
func (k Kind) String() string {
	switch k {
	case EOF:
		return "end of file"
	case Ident:
		return "identifier"
	case Int:
		return "integer literal"
	case Fn:
		return "'fn'"
	case Let:
		return "'let'"
	case If:
		return "'if'"
	case Else:
		return "'else'"
	case True:
		return "'true'"
	case False:
		return "'false'"
	case Type:
		return "'type'"
	case Match:
		return "'match'"
	case Underscore:
		return "'_'"
	case LParen:
		return "'('"
	case RParen:
		return "')'"
	case LBrace:
		return "'{'"
	case RBrace:
		return "'}'"
	case Comma:
		return "','"
	case Colon:
		return "':'"
	case Arrow:
		return "'->'"
	case Assign:
		return "'='"
	case FatArrow:
		return "'=>'"
	case Bar:
		return "'|'"
	case Plus:
		return "'+'"
	case Minus:
		return "'-'"
	case Star:
		return "'*'"
	case Slash:
		return "'/'"
	case Eq:
		return "'=='"
	case NotEq:
		return "'!='"
	case Lt:
		return "'<'"
	case LtEq:
		return "'<='"
	case Gt:
		return "'>'"
	case GtEq:
		return "'>='"
	case AndAnd:
		return "'&&'"
	case OrOr:
		return "'||'"
	case Bang:
		return "'!'"
	default:
		return fmt.Sprintf("token(%d)", int(k))
	}
}

// Keywords maps keyword spellings to their kinds ("fn" -> Fn, ...).
var Keywords = map[string]Kind{
	"fn":    Fn,
	"let":   Let,
	"if":    If,
	"else":  Else,
	"true":  True,
	"false": False,
	"type":  Type,
	"match": Match,
	"_":     Underscore,
}

// Token is a lexical token. Text is the source spelling, empty for EOF.
type Token struct {
	Kind Kind
	Text string   // source text; "" for EOF
	Pos  diag.Pos // position of the first character
}
