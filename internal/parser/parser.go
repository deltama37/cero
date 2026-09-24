// Package parser parses Cero v0.1 source into an AST.
package parser

import (
	"strconv"

	"github.com/deltama37/cero/internal/ast"
	"github.com/deltama37/cero/internal/diag"
	"github.com/deltama37/cero/internal/lexer"
	"github.com/deltama37/cero/internal/token"
)

// ParseFile tokenizes and parses a whole Cero source file.
func ParseFile(src []byte) (*ast.File, error) {
	p, err := newParser(src)
	if err != nil {
		return nil, err
	}
	var funcs []*ast.FuncDecl
	for p.cur().Kind != token.EOF {
		fn, err := p.parseFuncDecl()
		if err != nil {
			return nil, err
		}
		funcs = append(funcs, fn)
	}
	return &ast.File{Funcs: funcs}, nil
}

// ParseExpr parses a single expression that must span the whole input.
// Used by tests.
func ParseExpr(src []byte) (ast.Expr, error) {
	p, err := newParser(src)
	if err != nil {
		return nil, err
	}
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.cur().Kind != token.EOF {
		return nil, diag.Errorf(p.cur().Pos, "expected %s, found %s", token.EOF, describe(p.cur()))
	}
	return expr, nil
}

type parser struct {
	toks []token.Token
	pos  int
}

func newParser(src []byte) (*parser, error) {
	toks, err := lexer.Tokenize(src)
	if err != nil {
		return nil, err
	}
	return &parser{toks: toks}, nil
}

func (p *parser) cur() token.Token {
	return p.toks[p.pos]
}

func (p *parser) advance() {
	if p.cur().Kind != token.EOF {
		p.pos++
	}
}

func (p *parser) expect(kind token.Kind) error {
	if p.cur().Kind == kind {
		p.advance()
		return nil
	}
	if kind == token.RBrace && p.cur().Kind == token.Assign {
		return diag.Errorf(p.cur().Pos, "cannot assign: values are immutable")
	}
	return diag.Errorf(p.cur().Pos, "expected %s, found %s", kind, describe(p.cur()))
}

func describe(tok token.Token) string {
	switch tok.Kind {
	case token.Ident:
		return "identifier " + strconv.Quote(tok.Text)
	case token.Int:
		return "integer literal " + tok.Text
	default:
		return tok.Kind.String()
	}
}

func (p *parser) parseFuncDecl() (*ast.FuncDecl, error) {
	fnTok := p.cur()
	if err := p.expect(token.Fn); err != nil {
		return nil, err
	}
	if p.cur().Kind != token.Ident {
		return nil, diag.Errorf(p.cur().Pos, "expected %s, found %s", token.Ident, describe(p.cur()))
	}
	nameTok := p.cur()
	p.advance()

	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	if err := p.expect(token.Arrow); err != nil {
		return nil, err
	}
	result, err := p.parseType()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.FuncDecl{
		Pos:     fnTok.Pos,
		Name:    nameTok.Text,
		NamePos: nameTok.Pos,
		Params:  params,
		Result:  result,
		Body:    body,
	}, nil
}

func (p *parser) parseFuncLit() (*ast.FuncLit, error) {
	fnTok := p.cur()
	if err := p.expect(token.Fn); err != nil {
		return nil, err
	}
	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	if err := p.expect(token.Arrow); err != nil {
		return nil, err
	}
	result, err := p.parseType()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &ast.FuncLit{
		Pos:    fnTok.Pos,
		Params: params,
		Result: result,
		Body:   body,
	}, nil
}

func (p *parser) parseParamList() ([]*ast.Param, error) {
	if err := p.expect(token.LParen); err != nil {
		return nil, err
	}
	var params []*ast.Param
	if p.cur().Kind != token.RParen {
		// A parameter list is present only when an identifier follows.
		// Anything else is reported as a missing ')'.
		if p.cur().Kind != token.Ident {
			return nil, diag.Errorf(p.cur().Pos, "expected %s, found %s", token.RParen, describe(p.cur()))
		}
		for {
			param, err := p.parseParam()
			if err != nil {
				return nil, err
			}
			params = append(params, param)
			if p.cur().Kind != token.Comma {
				break
			}
			p.advance()
		}
	}
	if err := p.expect(token.RParen); err != nil {
		return nil, err
	}
	return params, nil
}

func (p *parser) parseParam() (*ast.Param, error) {
	if p.cur().Kind != token.Ident {
		return nil, diag.Errorf(p.cur().Pos, "expected %s, found %s", token.Ident, describe(p.cur()))
	}
	nameTok := p.cur()
	p.advance()
	if err := p.expect(token.Colon); err != nil {
		return nil, err
	}
	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}
	return &ast.Param{Pos: nameTok.Pos, Name: nameTok.Text, Type: typ}, nil
}

func (p *parser) parseType() (ast.TypeExpr, error) {
	start := p.cur().Pos

	var (
		atom    ast.TypeExpr
		list    []ast.TypeExpr
		hasList bool
	)

	switch p.cur().Kind {
	case token.Ident:
		atom = &ast.NamedType{Pos: p.cur().Pos, Name: p.cur().Text}
		p.advance()
	case token.LParen:
		p.advance()
		if p.cur().Kind == token.RParen {
			p.advance()
			list = []ast.TypeExpr{}
			hasList = true
			break
		}
		first, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if p.cur().Kind == token.Comma {
			list = []ast.TypeExpr{first}
			for p.cur().Kind == token.Comma {
				p.advance()
				next, err := p.parseType()
				if err != nil {
					return nil, err
				}
				list = append(list, next)
			}
			if err := p.expect(token.RParen); err != nil {
				return nil, err
			}
			hasList = true
			break
		}
		if err := p.expect(token.RParen); err != nil {
			return nil, err
		}
		atom = first
		list = []ast.TypeExpr{first}
		hasList = true
	default:
		return nil, diag.Errorf(p.cur().Pos, "expected type, found %s", describe(p.cur()))
	}

	if p.cur().Kind == token.Arrow {
		p.advance()
		result, err := p.parseType()
		if err != nil {
			return nil, err
		}
		params := list
		if !hasList {
			params = []ast.TypeExpr{atom}
		}
		return &ast.FuncType{Pos: start, Params: params, Result: result}, nil
	}
	if atom == nil {
		return nil, diag.Errorf(p.cur().Pos, "expected %s, found %s", token.Arrow, describe(p.cur()))
	}
	return atom, nil
}

func (p *parser) parseBlock() (*ast.BlockExpr, error) {
	lbrace := p.cur()
	if err := p.expect(token.LBrace); err != nil {
		return nil, err
	}
	var lets []*ast.LetStmt
	for p.cur().Kind == token.Let {
		letStmt, err := p.parseLet()
		if err != nil {
			return nil, err
		}
		lets = append(lets, letStmt)
	}
	result, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(token.RBrace); err != nil {
		return nil, err
	}
	return &ast.BlockExpr{Pos: lbrace.Pos, Lets: lets, Result: result}, nil
}

func (p *parser) parseLet() (*ast.LetStmt, error) {
	letTok := p.cur()
	if err := p.expect(token.Let); err != nil {
		return nil, err
	}
	if p.cur().Kind != token.Ident {
		return nil, diag.Errorf(p.cur().Pos, "expected %s, found %s", token.Ident, describe(p.cur()))
	}
	nameTok := p.cur()
	p.advance()

	var typ ast.TypeExpr
	if p.cur().Kind == token.Colon {
		p.advance()
		var err error
		typ, err = p.parseType()
		if err != nil {
			return nil, err
		}
	}
	if err := p.expect(token.Assign); err != nil {
		return nil, err
	}
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.LetStmt{
		Pos:     letTok.Pos,
		Name:    nameTok.Text,
		NamePos: nameTok.Pos,
		Type:    typ,
		Value:   value,
	}, nil
}

func (p *parser) parseExpr() (ast.Expr, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (ast.Expr, error) {
	return p.parseLeft(p.parseAnd, token.OrOr)
}

func (p *parser) parseAnd() (ast.Expr, error) {
	return p.parseLeft(p.parseEq, token.AndAnd)
}

func (p *parser) parseEq() (ast.Expr, error) {
	return p.parseLeft(p.parseRel, token.Eq, token.NotEq)
}

func (p *parser) parseRel() (ast.Expr, error) {
	return p.parseLeft(p.parseAdd, token.Lt, token.LtEq, token.Gt, token.GtEq)
}

func (p *parser) parseAdd() (ast.Expr, error) {
	return p.parseLeft(p.parseMul, token.Plus, token.Minus)
}

func (p *parser) parseMul() (ast.Expr, error) {
	return p.parseLeft(p.parseUnary, token.Star, token.Slash)
}

func (p *parser) parseLeft(next func() (ast.Expr, error), ops ...token.Kind) (ast.Expr, error) {
	left, err := next()
	if err != nil {
		return nil, err
	}
	for p.has(ops...) {
		op := p.cur()
		p.advance()
		right, err := next()
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{
			Pos:   left.Position(),
			OpPos: op.Pos,
			Op:    op.Kind,
			X:     left,
			Y:     right,
		}
	}
	return left, nil
}

func (p *parser) has(kinds ...token.Kind) bool {
	cur := p.cur().Kind
	for _, kind := range kinds {
		if cur == kind {
			return true
		}
	}
	return false
}

func (p *parser) parseUnary() (ast.Expr, error) {
	if p.cur().Kind == token.Minus || p.cur().Kind == token.Bang {
		op := p.cur()
		p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Pos: op.Pos, Op: op.Kind, X: x}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (ast.Expr, error) {
	expr, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.cur().Kind == token.LParen {
		lparen := p.cur().Pos
		p.advance()
		var args []ast.Expr
		if p.cur().Kind != token.RParen {
			for {
				arg, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				args = append(args, arg)
				if p.cur().Kind != token.Comma {
					break
				}
				p.advance()
			}
		}
		if err := p.expect(token.RParen); err != nil {
			return nil, err
		}
		expr = &ast.CallExpr{
			Pos:    expr.Position(),
			Fn:     expr,
			LParen: lparen,
			Args:   args,
		}
	}
	return expr, nil
}

func (p *parser) parsePrimary() (ast.Expr, error) {
	switch p.cur().Kind {
	case token.Int:
		tok := p.cur()
		p.advance()
		value, err := strconv.ParseInt(tok.Text, 10, 64)
		if err != nil {
			return nil, diag.Errorf(tok.Pos, "integer literal out of range: %s", tok.Text)
		}
		return &ast.IntLit{Pos: tok.Pos, Value: value}, nil
	case token.True:
		pos := p.cur().Pos
		p.advance()
		return &ast.BoolLit{Pos: pos, Value: true}, nil
	case token.False:
		pos := p.cur().Pos
		p.advance()
		return &ast.BoolLit{Pos: pos, Value: false}, nil
	case token.Ident:
		tok := p.cur()
		p.advance()
		return &ast.Ident{Pos: tok.Pos, Name: tok.Text}, nil
	case token.LParen:
		p.advance()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(token.RParen); err != nil {
			return nil, err
		}
		return expr, nil
	case token.If:
		return p.parseIf()
	case token.LBrace:
		return p.parseBlock()
	case token.Fn:
		return p.parseFuncLit()
	default:
		return nil, diag.Errorf(p.cur().Pos, "expected expression, found %s", describe(p.cur()))
	}
}

func (p *parser) parseIf() (*ast.IfExpr, error) {
	ifTok := p.cur()
	if err := p.expect(token.If); err != nil {
		return nil, err
	}
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	if err := p.expect(token.Else); err != nil {
		return nil, err
	}
	var elseExpr ast.Expr
	switch p.cur().Kind {
	case token.If:
		elseExpr, err = p.parseIf()
	case token.LBrace:
		elseExpr, err = p.parseBlock()
	default:
		return nil, diag.Errorf(p.cur().Pos, "expected %s, found %s", token.LBrace, describe(p.cur()))
	}
	if err != nil {
		return nil, err
	}
	return &ast.IfExpr{Pos: ifTok.Pos, Cond: cond, Then: then, Else: elseExpr}, nil
}
