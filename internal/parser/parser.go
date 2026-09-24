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
	var (
		typeDecls []*ast.TypeDecl
		funcs     []*ast.FuncDecl
	)
	for p.cur().Kind != token.EOF {
		switch p.cur().Kind {
		case token.Type:
			decl, err := p.parseTypeDecl()
			if err != nil {
				return nil, err
			}
			typeDecls = append(typeDecls, decl)
		case token.Fn:
			fn, err := p.parseFuncDecl()
			if err != nil {
				return nil, err
			}
			funcs = append(funcs, fn)
		default:
			return nil, diag.Errorf(p.cur().Pos, "expected %s or %s, found %s", token.Fn, token.Type, describe(p.cur()))
		}
	}
	return &ast.File{Types: typeDecls, Funcs: funcs}, nil
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
	case token.Match:
		return p.parseMatch()
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

func isUpperName(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

func (p *parser) parseTypeDecl() (*ast.TypeDecl, error) {
	typeTok := p.cur()
	if err := p.expect(token.Type); err != nil {
		return nil, err
	}
	if p.cur().Kind != token.Ident {
		return nil, diag.Errorf(p.cur().Pos, "expected %s, found %s", token.Ident, describe(p.cur()))
	}
	nameTok := p.cur()
	p.advance()
	if !isUpperName(nameTok.Text) {
		return nil, diag.Errorf(nameTok.Pos, "type name '%s' must start with an uppercase letter", nameTok.Text)
	}
	if err := p.expect(token.Assign); err != nil {
		return nil, err
	}
	if p.cur().Kind == token.Bar {
		p.advance()
	}
	ctor, err := p.parseCtorDecl()
	if err != nil {
		return nil, err
	}
	ctors := []*ast.CtorDecl{ctor}
	for p.cur().Kind == token.Bar {
		p.advance()
		ctor, err = p.parseCtorDecl()
		if err != nil {
			return nil, err
		}
		ctors = append(ctors, ctor)
	}
	return &ast.TypeDecl{
		Pos:     typeTok.Pos,
		Name:    nameTok.Text,
		NamePos: nameTok.Pos,
		Ctors:   ctors,
	}, nil
}

func (p *parser) parseCtorDecl() (*ast.CtorDecl, error) {
	if p.cur().Kind != token.Ident {
		return nil, diag.Errorf(p.cur().Pos, "expected %s, found %s", token.Ident, describe(p.cur()))
	}
	nameTok := p.cur()
	p.advance()
	if !isUpperName(nameTok.Text) {
		return nil, diag.Errorf(nameTok.Pos, "constructor name '%s' must start with an uppercase letter", nameTok.Text)
	}
	var fields []ast.TypeExpr
	if p.cur().Kind == token.LParen {
		p.advance()
		field, err := p.parseType()
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
		for p.cur().Kind == token.Comma {
			p.advance()
			field, err = p.parseType()
			if err != nil {
				return nil, err
			}
			fields = append(fields, field)
		}
		if err := p.expect(token.RParen); err != nil {
			return nil, err
		}
	}
	return &ast.CtorDecl{Pos: nameTok.Pos, Name: nameTok.Text, Fields: fields}, nil
}

func (p *parser) parseMatch() (*ast.MatchExpr, error) {
	matchTok := p.cur()
	if err := p.expect(token.Match); err != nil {
		return nil, err
	}
	scrutinee, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(token.LBrace); err != nil {
		return nil, err
	}
	var arms []*ast.MatchArm
	for {
		arm, err := p.parseArm()
		if err != nil {
			return nil, err
		}
		arms = append(arms, arm)
		if p.cur().Kind == token.Comma {
			p.advance()
			if p.cur().Kind == token.RBrace {
				break
			}
			continue
		}
		if p.cur().Kind == token.RBrace {
			break
		}
		return nil, diag.Errorf(p.cur().Pos, "expected %s or %s, found %s", token.Comma, token.RBrace, describe(p.cur()))
	}
	if err := p.expect(token.RBrace); err != nil {
		return nil, err
	}
	return &ast.MatchExpr{Pos: matchTok.Pos, Scrutinee: scrutinee, Arms: arms}, nil
}

func (p *parser) parseArm() (*ast.MatchArm, error) {
	pattern, err := p.parsePattern()
	if err != nil {
		return nil, err
	}
	if err := p.expect(token.FatArrow); err != nil {
		return nil, err
	}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ast.MatchArm{Pattern: pattern, Body: body}, nil
}

func (p *parser) parsePattern() (ast.Pattern, error) {
	switch p.cur().Kind {
	case token.Underscore:
		pos := p.cur().Pos
		p.advance()
		return &ast.WildcardPat{Pos: pos}, nil
	case token.Int:
		tok := p.cur()
		p.advance()
		value, err := strconv.ParseInt(tok.Text, 10, 64)
		if err != nil {
			return nil, diag.Errorf(tok.Pos, "integer literal out of range: %s", tok.Text)
		}
		return &ast.IntPat{Pos: tok.Pos, Value: value}, nil
	case token.True:
		pos := p.cur().Pos
		p.advance()
		return &ast.BoolPat{Pos: pos, Value: true}, nil
	case token.False:
		pos := p.cur().Pos
		p.advance()
		return &ast.BoolPat{Pos: pos, Value: false}, nil
	case token.Ident:
		nameTok := p.cur()
		p.advance()
		if !isUpperName(nameTok.Text) {
			return &ast.VarPat{Pos: nameTok.Pos, Name: nameTok.Text}, nil
		}
		var args []ast.Pattern
		if p.cur().Kind == token.LParen {
			p.advance()
			arg, err := p.parseSubPattern()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			for p.cur().Kind == token.Comma {
				p.advance()
				arg, err = p.parseSubPattern()
				if err != nil {
					return nil, err
				}
				args = append(args, arg)
			}
			if err := p.expect(token.RParen); err != nil {
				return nil, err
			}
		}
		return &ast.CtorPat{Pos: nameTok.Pos, Name: nameTok.Text, Args: args}, nil
	default:
		return nil, diag.Errorf(p.cur().Pos, "expected pattern, found %s", describe(p.cur()))
	}
}

func (p *parser) parseSubPattern() (ast.Pattern, error) {
	switch p.cur().Kind {
	case token.Underscore:
		pos := p.cur().Pos
		p.advance()
		return &ast.WildcardPat{Pos: pos}, nil
	case token.Ident:
		if isUpperName(p.cur().Text) {
			return nil, diag.Errorf(p.cur().Pos, "nested patterns are not supported in v0.2")
		}
		nameTok := p.cur()
		p.advance()
		return &ast.VarPat{Pos: nameTok.Pos, Name: nameTok.Text}, nil
	case token.Int, token.True, token.False:
		return nil, diag.Errorf(p.cur().Pos, "nested patterns are not supported in v0.2")
	default:
		return nil, diag.Errorf(p.cur().Pos, "expected pattern, found %s", describe(p.cur()))
	}
}
