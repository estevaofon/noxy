package parser

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/token"
	"strconv"
)

type Parser struct {
	l *lexer.Lexer

	curToken  token.Token
	peekToken token.Token

	prefixParseFns map[token.TokenType]func() ast.Expression
	infixParseFns  map[token.TokenType]func(ast.Expression) ast.Expression

	errors []string
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:      l,
		errors: []string{},
	}

	// Read two tokens, so curToken and peekToken are both set
	p.nextToken()
	p.nextToken()

	p.prefixParseFns = make(map[token.TokenType]func() ast.Expression)
	p.registerPrefix(token.IDENTIFIER, p.parseIdentifier)
	p.registerPrefix(token.INT, p.parseIntegerLiteral)
	p.registerPrefix(token.FLOAT, p.parseFloatLiteral)
	p.registerPrefix(token.NOT, p.parsePrefixExpression)
	p.registerPrefix(token.MINUS, p.parsePrefixExpression)
	p.registerPrefix(token.TRUE, p.parseBoolean)
	p.registerPrefix(token.FALSE, p.parseBoolean)
	p.registerPrefix(token.LPAREN, p.parseGroupedExpression)
	p.registerPrefix(token.LBRACKET, p.parseArrayLiteral)
	p.registerPrefix(token.STRING, p.parseStringLiteral)
	p.registerPrefix(token.BYTES, p.parseBytesLiteral)
	p.registerPrefix(token.FSTRING, p.parseFString)
	p.registerPrefix(token.NULL, p.parseNull)
	p.registerPrefix(token.ZEROS, p.parseZeros)
	p.registerPrefix(token.REF, p.parsePrefixExpression)
	p.registerPrefix(token.BIT_NOT, p.parsePrefixExpression)
	// p.registerPrefix(token.IF, p.parseIfExpression) // Removed
	p.registerPrefix(token.FUNC, p.parseFunctionLiteral)

	p.registerPrefix(token.PERCENT, p.parseGroupedExpression) // Grouped expression logic for PERCENT? No.
	// Oh wait, I registered PERCENT for infix above in previous steps.
	// LBRACE is for Map Literal.
	p.registerPrefix(token.LBRACE, p.parseMapLiteral)

	p.infixParseFns = make(map[token.TokenType]func(ast.Expression) ast.Expression)
	p.registerInfix(token.PLUS, p.parseInfixExpression)
	p.registerInfix(token.MINUS, p.parseInfixExpression)
	p.registerInfix(token.SLASH, p.parseInfixExpression)
	p.registerInfix(token.STAR, p.parseInfixExpression)
	p.registerInfix(token.PERCENT, p.parseInfixExpression) // Add this
	p.registerInfix(token.EQ, p.parseInfixExpression)
	p.registerInfix(token.NEQ, p.parseInfixExpression)
	p.registerInfix(token.LT, p.parseInfixExpression)
	p.registerInfix(token.GT, p.parseInfixExpression)
	p.registerInfix(token.LTE, p.parseInfixExpression)
	p.registerInfix(token.GTE, p.parseInfixExpression)
	p.registerInfix(token.AND, p.parseInfixExpression)
	p.registerInfix(token.OR, p.parseInfixExpression)
	p.registerInfix(token.BIT_AND, p.parseInfixExpression)
	p.registerInfix(token.BIT_OR, p.parseInfixExpression)
	p.registerInfix(token.BIT_XOR, p.parseInfixExpression)
	p.registerInfix(token.SHIFT_LEFT, p.parseInfixExpression)
	p.registerInfix(token.SHIFT_RIGHT, p.parseInfixExpression)
	p.registerInfix(token.LPAREN, p.parseCallExpression)
	p.registerInfix(token.LBRACKET, p.parseIndexExpression)
	p.registerInfix(token.DOT, p.parseMemberAccess)

	return p
}

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("  File \"%s\", line %d\n    %s\nSyntaxError: expected %s, found %s",
		"file", p.peekToken.Line, "code line here...", t.Display(), p.peekToken.Type.Display())
	// Wait, I can't easily get the filename and code line here without passing it down or storing simpler "Line:Col: msg" and letting main format it?
	// The user requested: SyntaxError: invalid syntax "def"
	// My previous format was: [Line:Col] msg.
	// Providing a full Python-style traceback (File "...", line X \n code \n Error) requires more context in the error or main.
	// For now, I will stick to the requested single line format or simple multiline if I can't get file/code easily.
	// "[Line:Col] SyntaxError: ..." is good enough for now.
	// Actually, let's just do:
	// [Line:Col] SyntaxError: expected ')' found ':'
	msg = fmt.Sprintf("[%d:%d] SyntaxError: expected %s, found %s",
		p.peekToken.Line, p.peekToken.Column, t.Display(), p.peekToken.Type.Display())
	p.errors = append(p.errors, msg)
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()

	// Skip NEW tokens if they are just fillers?
	// In Noxy Python parser: skip_newlines() was explicit.
	// Here we might want to handle them. For now, let's keep them and handle in parsing logic.
}

func (p *Parser) skipUntilEnd() {
	for !p.curTokenIs(token.END) && !p.curTokenIs(token.EOF) {
		p.nextToken()
	}
}

func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{}
	program.Statements = []ast.Statement{}

	for p.curToken.Type != token.EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.nextToken()
	}

	return program
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.LET:
		return p.parseLetStatement()
	case token.RETURN:
		return p.parseReturnStatement()
	case token.IF:
		return p.parseIfStatement()
	case token.WHILE:
		return p.parseWhileStatement()
	case token.FOR:
		return p.parseForStatement()
	case token.STRUCT:
		return p.parseStructStatement()
	case token.FUNC:
		return p.parseFunctionStatement()
	case token.BREAK:
		return p.parseBreakStatement()
	case token.USE:
		return p.parseUseStatement()
	case token.WHEN:
		return p.parseWhenStatement()
	case token.NEWLINE:
		return nil // Skip empty lines / separators
	default:
		// Attempt to parse expression
		expr := p.parseExpression(LOWEST)

		// Check for missing 'let' (Identifier followed by Colon)
		if ident, ok := expr.(*ast.Identifier); ok && p.peekTokenIs(token.COLON) {
			msg := fmt.Sprintf("[%d:%d] SyntaxError: missing 'let' keyword for variable declaration\n  hint: use 'let %s%s ...'",
				ident.Token.Line, ident.Token.Column,
				ident.Value, p.peekToken.Literal)
			p.errors = append(p.errors, msg)

			// Skip until newline to prevent cascading errors
			for !p.curTokenIs(token.NEWLINE) && !p.curTokenIs(token.EOF) {
				p.nextToken()
			}
			return nil
		}

		// Check if it's an assignment
		// Handle `case msg = recv` (ExpressionStmt vs AssignStmt logic correction in parseWhenStatement used parseExpression)
		// But here in parseStatement generic:
		if p.peekTokenIs(token.ASSIGN) {
			p.nextToken() // eat ASSIGN -> curToken is ASSIGN
			// stmt target is `expr`.
			// But `expr` might be complicated tree? AssignStmt allows generic expression target?
			// Typically only Identifier or Index/MemberAccess.
			// AssignStmt struct has Target Expression. Valid.
			// Logic is correct.
			tokenAssign := p.curToken
			p.nextToken() // move to value
			stmt := &ast.AssignStmt{Token: tokenAssign, Target: expr}
			stmt.Value = p.parseExpression(LOWEST)

			if p.peekTokenIs(token.NEWLINE) {
				p.nextToken()
			}
			return stmt
		}

		// Otherwise expression statement
		if expr != nil {
			// Allow all expressions as statements (Python-like)
			stmt := &ast.ExpressionStmt{Token: p.curToken, Expression: expr}
			if p.peekTokenIs(token.NEWLINE) {
				p.nextToken()
			}
			return stmt
		}

		return nil
	}
}

func (p *Parser) parseAssignStatement() *ast.AssignStmt {
	stmt := &ast.AssignStmt{Token: p.peekToken} // The '=' token

	// Parse Target (Identifier)
	stmt.Target = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	p.nextToken() // Eat Identifier
	p.nextToken() // Eat '='

	stmt.Value = p.parseExpression(LOWEST)

	// Optional newline
	if p.peekToken.Type == token.NEWLINE {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseIfStatement() *ast.IfStatement {
	stmt := &ast.IfStatement{Token: p.curToken}

	p.nextToken() // Eat IF

	// Condition starts AFTER 'if'.
	stmt.Condition = p.parseExpression(LOWEST)

	if !p.expectPeek(token.THEN) {
		return nil
	}

	stmt.Consequence = p.parseBlockStatement()

	// Verify valid block termination for Consequence
	if !p.curTokenIs(token.END) && !p.curTokenIs(token.ELSE) && !p.curTokenIs(token.ELIF) {
		got := p.curToken.Literal
		if p.curTokenIs(token.EOF) {
			got = "EOF"
		}
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: expected 'end', 'else' or 'elif', found %s",
			p.curToken.Line, p.curToken.Column, got))
		return nil
	}

	if p.curTokenIs(token.ELIF) {
		// Treat 'elif' as 'else { if ... }'
		// We create a wrapping block for the "else" part
		wrapperBlock := &ast.BlockStatement{
			Token:      p.curToken, // The ELIF token
			Statements: []ast.Statement{},
		}

		// Recursively parse the 'if' part (since elif is essentially an if)
		// parseIfStatement expects current token to be IF (or ELIF now) and consumes it
		nestedIf := p.parseIfStatement()
		if nestedIf == nil {
			return nil
		}

		wrapperBlock.Statements = append(wrapperBlock.Statements, nestedIf)
		stmt.Alternative = wrapperBlock
	} else if p.curTokenIs(token.ELSE) {
		stmt.Alternative = p.parseBlockStatement()

		// Verify valid block termination for Alternative
		if !p.curTokenIs(token.END) {
			got := p.curToken.Literal
			if p.curTokenIs(token.EOF) {
				got = "EOF"
			}
			p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: expected 'end', found %s",
				p.curToken.Line, p.curToken.Column, got))
			return nil
		}
	}

	return stmt
}

func (p *Parser) parseLetStatement() *ast.LetStmt {
	stmt := &ast.LetStmt{Token: p.curToken}

	if !p.expectPeek(token.IDENTIFIER) {
		return nil
	}

	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	// Specific check for missing type: let x = ...
	if p.peekTokenIs(token.ASSIGN) {
		msg := fmt.Sprintf("[%d:%d] SyntaxError: missing type annotation for '%s'\n  hint: use 'let %s: <type> = ...'",
			p.peekToken.Line, p.peekToken.Column,
			stmt.Name.Value, stmt.Name.Value)
		p.errors = append(p.errors, msg)

		// Advance to avoid subsequent errors
		for !p.curTokenIs(token.NEWLINE) && !p.curTokenIs(token.EOF) {
			p.nextToken()
		}
		return nil
	}

	if !p.expectPeek(token.COLON) {
		return nil
	}

	p.nextToken() // Eat COLON

	stmt.Type = p.parseType()

	if p.peekToken.Type == token.ASSIGN {
		p.nextToken() // Eat ASSIGN
		p.nextToken() // Start expression
		stmt.Value = p.parseExpression(LOWEST)
	}

	return stmt
}

func (p *Parser) parseReturnStatement() *ast.ReturnStmt {
	stmt := &ast.ReturnStmt{Token: p.curToken}

	p.nextToken()

	// Handle return void
	if p.curToken.Type == token.NEWLINE || p.curToken.Type == token.EOF || p.curToken.Type == token.END {
		return stmt
	}

	stmt.ReturnValue = p.parseExpression(LOWEST)

	// Optional newline
	if p.peekToken.Type == token.NEWLINE {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseBreakStatement() *ast.BreakStmt {
	stmt := &ast.BreakStmt{Token: p.curToken}
	p.nextToken() // eat 'break'
	// Optional newline
	if p.peekToken.Type == token.NEWLINE {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseUseStatement() *ast.UseStmt {
	stmt := &ast.UseStmt{Token: p.curToken}

	if !p.expectPeek(token.IDENTIFIER) {
		return nil
	}
	// Parse dot-separated module path: pkg.sub.mod
	stmt.Module = p.curToken.Literal

	for p.peekTokenIs(token.DOT) {
		p.nextToken() // eat .
		if !p.expectPeek(token.IDENTIFIER) {
			return nil
		}
		stmt.Module += "." + p.curToken.Literal
	}

	// Check for 'as' Alias
	if p.peekTokenIs(token.AS) {
		p.nextToken() // eat as
		if !p.expectPeek(token.IDENTIFIER) {
			return nil
		}
		stmt.Alias = p.curToken.Literal
	}

	// Check for 'select'
	if p.peekTokenIs(token.SELECT) {
		p.nextToken() // eat select

		if p.peekTokenIs(token.STAR) {
			p.nextToken() // eat *
			stmt.SelectAll = true
		} else {
			// Parse list of identifiers
			start := true
			for start || p.peekTokenIs(token.COMMA) {
				if !start {
					p.nextToken() // eat comma
				}
				start = false

				if !p.expectPeek(token.IDENTIFIER) {
					return nil
				}
				stmt.Selectors = append(stmt.Selectors, p.curToken.Literal)
			}
		}
	}

	if p.peekTokenIs(token.NEWLINE) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseWhenStatement() *ast.WhenStatement {
	stmt := &ast.WhenStatement{Token: p.curToken, Cases: []*ast.CaseClause{}}
	p.nextToken() // eat 'when'

	if p.curTokenIs(token.NEWLINE) {
		p.nextToken()
	}

	for !p.curTokenIs(token.END) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.CASE) {
			cc := &ast.CaseClause{Token: p.curToken, IsDefault: false}
			p.nextToken() // eat 'case'

			// Condition statement (ExprStmt or AssignStmt)
			// Problem: parseStatement eats newline?
			// case recv(c) then ...
			// If we call parseStatement, it might look for newline terminator.
			// But 'then' is the terminator here.
			// So we can parseExpression? Or Assignment?
			// Assignments starts with Identifier?
			// parseStatement handles choice.
			// However custom case parsing:
			// parseExpression(LOWEST) -> if '=' peek -> Assign. else ExprStmt.

			expr := p.parseExpression(LOWEST)
			if p.peekTokenIs(token.ASSIGN) {
				// Assigment: case msg = ...
				p.nextToken() // eat identifier (expr must be identifier)
				ident, ok := expr.(*ast.Identifier)
				if !ok {
					p.errors = append(p.errors, "case assignment target must be identifier")
					return nil
				}
				assignStmt := &ast.AssignStmt{Token: p.curToken, Target: ident}
				p.nextToken() // eat '='
				assignStmt.Value = p.parseExpression(LOWEST)
				cc.Condition = assignStmt
			} else {
				// Expression statement: case send(...) or case recv(...)
				// We don't have the start token of expr easily here without casting or capturing before.
				// However, using p.curToken (which is probably THEN) is okay for now as it's just for error reporting location roughly.
				// Or use token.Token{Type: token.ILLEGAL, Literal: expr.TokenLiteral()}? No.
				// Let's blindly use p.curToken for now, or capture it before parsing expr if possible.
				// Refactoring to capture start token:
				// But wait, I can't restart parsing.
				// Let's use p.curToken. It's close enough.
				cc.Condition = &ast.ExpressionStmt{Token: p.curToken, Expression: expr}
			}

			if !p.expectPeek(token.THEN) {
				return nil
			}

			cc.Body = p.parseCaseBody()
			stmt.Cases = append(stmt.Cases, cc)
		} else if p.curTokenIs(token.DEFAULT) {
			cc := &ast.CaseClause{Token: p.curToken, IsDefault: true}
			p.nextToken() // eat 'default'

			cc.Body = p.parseCaseBody()
			stmt.Cases = append(stmt.Cases, cc)
		} else if p.curTokenIs(token.NEWLINE) {
			p.nextToken()
		} else {
			// Error unexpected token
			p.errors = append(p.errors, fmt.Sprintf("unexpected token in when block: %s", p.curToken.Literal))
			p.nextToken()
		}
	}

	if p.curTokenIs(token.END) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseCaseBlock() *ast.BlockStatement {
	block := &ast.BlockStatement{Token: p.curToken}
	block.Statements = []ast.Statement{}

	if p.curTokenIs(token.NEWLINE) {
		p.nextToken()
	}

	for !p.curTokenIs(token.CASE) && !p.curTokenIs(token.DEFAULT) && !p.curTokenIs(token.END) && !p.curTokenIs(token.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
		if p.curTokenIs(token.NEWLINE) {
			p.nextToken() // skip separators
		}
	}
	return block
}

func (p *Parser) parseExpressionStatement() *ast.ExpressionStmt {
	stmt := &ast.ExpressionStmt{Token: p.curToken}

	stmt.Expression = p.parseExpression(LOWEST)

	// Optional newline
	if p.peekToken.Type == token.NEWLINE {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseExpression(precedence int) ast.Expression {
	// Simple identifier or integer literal for now
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.noPrefixParseFnError(p.curToken.Type)
		return nil
	}
	leftExp := prefix()

	for !p.peekTokenIs(token.NEWLINE) && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}
		p.nextToken()
		leftExp = infix(leftExp)
	}

	return leftExp
}

// Helpers
func (p *Parser) curTokenIs(t token.TokenType) bool {
	return p.curToken.Type == t
}

func (p *Parser) peekTokenIs(t token.TokenType) bool {
	return p.peekToken.Type == t
}

func (p *Parser) expectPeek(t token.TokenType) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}

// Type parsing
func (p *Parser) parseType() ast.NoxyType {
	// Optional REF
	if p.curToken.Type == token.REF {
		p.nextToken()
		// Wrap recursive call
		elementType := p.parseType()
		if elementType == nil {
			return nil
		}
		return &ast.RefType{ElementType: elementType}
	}

	var t ast.NoxyType
	// Primitive types and Identifier types
	switch p.curToken.Type {
	case token.TYPE_INT:
		t = &ast.PrimitiveType{Name: "int"}
	case token.TYPE_FLOAT:
		t = &ast.PrimitiveType{Name: "float"}
	case token.TYPE_STRING:
		t = &ast.PrimitiveType{Name: "string"}
	case token.TYPE_BOOL:
		t = &ast.PrimitiveType{Name: "bool"}
	case token.TYPE_BYTES:
		t = &ast.PrimitiveType{Name: "bytes"}
	case token.FUNC:
		t = &ast.PrimitiveType{Name: "func"} // Generic function type
	case token.BYTES: // This is Literal 'b"..."'.
		// Shouldn't be here in parseType.
		// But wait, declaration says ": bytes".
		// Is "bytes" a keyword? Check token.go.
		t = &ast.PrimitiveType{Name: "bytes"}
	case token.IDENTIFIER:
		name := p.curToken.Literal
		// Support dot notation for types (e.g. io.File)
		for p.peekTokenIs(token.DOT) {
			p.nextToken() // eat .
			if !p.expectPeek(token.IDENTIFIER) {
				return nil
			}
			name += "." + p.curToken.Literal
		}
		t = &ast.PrimitiveType{Name: name}
	case token.MAP:
		// map[key, val]
		if !p.expectPeek(token.LBRACKET) {
			return nil
		}
		// curToken is LBRACKET. Move to KeyType.
		// p.nextToken() // This was redundant. expectPeek already advanced curToken to LBRACKET, and peekToken to KeyType.

		// Now, p.curToken is LBRACKET, p.peekToken is the start of the KeyType.
		// We need to advance curToken to the KeyType before parsing it.
		p.nextToken() // Advance curToken to the KeyType

		keyType := p.parseType()

		// Expect COMMA
		// parseType leaves curToken at the last token of the type (e.g. TypeName or RBRACKET of array)
		// So peek should be COMMA.

		if !p.expectPeek(token.COMMA) {
			return nil
		}
		p.nextToken() // move to start of ValueType

		valueType := p.parseType()

		if !p.expectPeek(token.RBRACKET) {
			return nil
		}

		t = &ast.MapType{KeyType: keyType, ValueType: valueType}
		return t // Map type doesn't support array suffix immediately? 'map[]' -> array of maps?
		// If so, we should fall through to array check.

	default:
		// Fallback or error?
		t = &ast.PrimitiveType{Name: "int"} // Default
	}

	// Check for array brackets [] or [size]
	if p.peekTokenIs(token.LBRACKET) {
		p.nextToken() // eat [

		size := 0
		// Check for size (optional)
		if !p.peekTokenIs(token.RBRACKET) {
			p.nextToken()                     // Eat the size token
			if p.curToken.Type == token.INT { // Verify token type name
				fmt.Sscanf(p.curToken.Literal, "%d", &size)
			}
		}

		if !p.expectPeek(token.RBRACKET) {
			return nil
		}
		t = &ast.ArrayType{ElementType: t, Size: size}
	}

	return t
}

// Precedence system setup
const (
	_ int = iota
	LOWEST
	OR          // ||
	AND         // &&
	BIT_OR      // |
	BIT_XOR     // ^
	BIT_AND     // &
	EQUALS      // ==
	LESSGREATER // > or <
	SHIFT       // << or >>
	SUM         // + or -
	PRODUCT     // * or /
	PREFIX      // -X or !X or ~X
	CALL        // myFunction(X)
	INDEX       // array[index]
)

var precedences = map[token.TokenType]int{
	token.EQ:          EQUALS,
	token.NEQ:         EQUALS,
	token.LT:          LESSGREATER,
	token.GT:          LESSGREATER,
	token.LTE:         LESSGREATER,
	token.GTE:         LESSGREATER,
	token.AND:         AND,
	token.OR:          OR,
	token.BIT_AND:     BIT_AND,
	token.BIT_OR:      BIT_OR,
	token.BIT_XOR:     BIT_XOR,
	token.SHIFT_LEFT:  SHIFT,
	token.SHIFT_RIGHT: SHIFT,
	token.PLUS:        SUM,
	token.MINUS:       SUM,
	token.SLASH:       PRODUCT,
	token.STAR:        PRODUCT,
	token.PERCENT:     PRODUCT,
	token.LPAREN:      CALL,
	token.LBRACKET:    INDEX,
	token.DOT:         INDEX,
}

func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) curPrecedence() int {
	if p, ok := precedences[p.curToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) registerPrefix(tokenType token.TokenType, fn func() ast.Expression) {
	p.prefixParseFns[tokenType] = fn
}

func (p *Parser) registerInfix(tokenType token.TokenType, fn func(ast.Expression) ast.Expression) {
	p.infixParseFns[tokenType] = fn
}

func (p *Parser) parseIdentifier() ast.Expression {
	return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseIntegerLiteral() ast.Expression {
	lit := &ast.IntegerLiteral{Token: p.curToken}

	value, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		// handle error? fallback?
		// for now default 0 or log?
		// Sscanf previously just set 0 if failed?
		// Let's keep 0 but maybe log if needed.
		// Actually ParseInt with base 0 handles 0x.
		fmt.Printf("Error parsing int: %s\n", err) // Optional debug
	}
	lit.Value = value
	return lit
}

func (p *Parser) parseFloatLiteral() ast.Expression {
	lit := &ast.FloatLiteral{Token: p.curToken}
	value := float64(0)
	fmt.Sscanf(p.curToken.Literal, "%f", &value)
	lit.Value = value
	return lit
}

func (p *Parser) parseStringLiteral() ast.Expression {
	return &ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseBytesLiteral() ast.Expression {
	return &ast.BytesLiteral{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseNull() ast.Expression {
	return &ast.NullLiteral{Token: p.curToken}
}

func (p *Parser) parseZeros() ast.Expression {
	// zeros(SIZE)
	lit := &ast.ZerosLiteral{Token: p.curToken}

	if !p.expectPeek(token.LPAREN) {
		return nil
	}

	p.nextToken()
	lit.Size = p.parseExpression(LOWEST)

	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	return lit
}

func (p *Parser) parseBoolean() ast.Expression {
	return &ast.Boolean{Token: p.curToken, Value: p.curTokenIs(token.TRUE)}
}

func (p *Parser) parseFString() ast.Expression {
	// Simple F-String parser
	// Breaks literal into parts and concatenates matches
	literal := p.curToken.Literal
	var exprs []ast.Expression

	lastIdx := 0
	for i := 0; i < len(literal); i++ {
		if literal[i] == '{' {
			// Add previous string part
			if i > lastIdx {
				exprs = append(exprs, &ast.StringLiteral{
					Token: token.Token{Type: token.STRING, Literal: literal[lastIdx:i]},
					Value: literal[lastIdx:i],
				})
			}

			// Find closing brace
			// Note: This logic is simple and doesn't handle nested braces like { {a:1} }
			// For a full implementation we need a proper brace counter or sub-lexer
			// But since we are extracting the string to pass to a new parser,
			// we can just find the matching }

			braceCount := 1
			j := i + 1
			for ; j < len(literal); j++ {
				if literal[j] == '{' {
					braceCount++
				} else if literal[j] == '}' {
					braceCount--
					if braceCount == 0 {
						break
					}
				}
			}

			if j >= len(literal) {
				// Error: unclosed brace
				p.errors = append(p.errors, fmt.Sprintf("unclosed brace in f-string"))
				return nil
			}

			exprContent := literal[i+1 : j]

			// Parse expression
			l := lexer.New(exprContent)
			par := New(l) // Recursive parser
			// Note: We need to register same prefixes/infixes? New() does that.
			// But typically recursive parser creation is heavy.
			// Ideally we use p itself? No, p depends on p.l.
			// New parser is safer.

			innerExpr := par.parseExpression(LOWEST)
			// Check errors
			if len(par.Errors()) > 0 {
				for _, msg := range par.Errors() {
					p.errors = append(p.errors, fmt.Sprintf("f-string expr error: %s", msg))
				}
				return nil
			}

			// Wrap in to_str() call: to_str(expr)
			callExpr := &ast.CallExpression{
				Token: token.Token{Type: token.IDENTIFIER, Literal: "("}, // Dummy token?
				Function: &ast.Identifier{
					Token: token.Token{Type: token.IDENTIFIER, Literal: "to_str"},
					Value: "to_str",
				},
				Arguments: []ast.Expression{innerExpr},
			}

			exprs = append(exprs, callExpr)

			lastIdx = j + 1
			i = j // Advance outer loop
		}
	}

	// Add remaining string
	if lastIdx < len(literal) {
		exprs = append(exprs, &ast.StringLiteral{
			Token: token.Token{Type: token.STRING, Literal: literal[lastIdx:]},
			Value: literal[lastIdx:],
		})
	}

	if len(exprs) == 0 {
		return &ast.StringLiteral{Token: p.curToken, Value: ""}
	}

	// Combine with +
	combined := exprs[0]
	for i := 1; i < len(exprs); i++ {
		combined = &ast.InfixExpression{
			Token:    token.Token{Type: token.PLUS, Literal: "+"},
			Left:     combined,
			Operator: "+",
			Right:    exprs[i],
		}
	}

	return combined
}

func (p *Parser) parsePrefixExpression() ast.Expression {
	expression := &ast.PrefixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
	}
	p.nextToken()
	expression.Right = p.parseExpression(PREFIX)
	return expression
}

func (p *Parser) parseInfixExpression(left ast.Expression) ast.Expression {
	expression := &ast.InfixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
		Left:     left,
	}

	precedence := p.curPrecedence()
	p.nextToken()
	expression.Right = p.parseExpression(precedence)

	return expression
}

func (p *Parser) parseGroupedExpression() ast.Expression {
	p.nextToken()
	exp := p.parseExpression(LOWEST)
	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	return exp
}

func (p *Parser) parseWhileStatement() *ast.WhileStatement {
	stmt := &ast.WhileStatement{Token: p.curToken}
	p.nextToken() // Eat WHILE

	stmt.Condition = p.parseExpression(LOWEST)

	if !p.expectPeek(token.DO) {
		// Or if Noxy is `while cond {` or `while cond do`?
		// Assuming DO as per Lexer tokens
		return nil
	}

	stmt.Body = p.parseBlockStatement()

	if !p.curTokenIs(token.END) {
		got := p.curToken.Literal
		if p.curTokenIs(token.EOF) {
			got = "EOF"
		}
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: expected 'end', found %s",
			p.curToken.Line, p.curToken.Column, got))
		return nil
	}

	return stmt
}

func (p *Parser) parseBlockStatement() *ast.BlockStatement {
	block := &ast.BlockStatement{Token: p.curToken}
	block.Statements = []ast.Statement{}

	// Skip current token (THEN or ELSE)
	p.nextToken()

	for !p.curTokenIs(token.END) && !p.curTokenIs(token.ELSE) && !p.curTokenIs(token.ELIF) && !p.curTokenIs(token.EOF) {
		// Removed check for FUNC/STRUCT to allow nested definitions (closures)

		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}

	// If we stopped at ELSE, we leave it there for `parseIfExpression` to see?
	// `parseIfExpression` checks `peekTokenIs(ELSE)`.
	// If we are AT `ELSE`, `parseBlockStatement` loop terminated.
	// So `curToken` IS `ELSE`.
	// If `parseIfExpression` does `p.peekTokenIs(ELSE)`, it checks NEXT token.
	// Discrepancy.

	// Let's fix `parseIfExpression`.

	return block
}

func (p *Parser) parseCaseBody() *ast.BlockStatement {
	block := &ast.BlockStatement{Token: p.curToken}
	block.Statements = []ast.Statement{}

	// Optional newline handling if previous token was THEN/DEFAULT
	if p.curTokenIs(token.THEN) {
		p.nextToken()
	}
	if p.curTokenIs(token.NEWLINE) {
		p.nextToken()
	}

	for !p.curTokenIs(token.END) && !p.curTokenIs(token.CASE) && !p.curTokenIs(token.DEFAULT) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.FUNC) || p.curTokenIs(token.STRUCT) {
			p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: unexpected %q, expected 'end', 'case' or 'default'",
				p.curToken.Line, p.curToken.Column, p.curToken.Literal))
			break
		}

		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}

	return block
}

func (p *Parser) parseFunctionStatement() *ast.FunctionStatement {
	stmt := &ast.FunctionStatement{Token: p.curToken}

	if !p.expectPeek(token.IDENTIFIER) {
		return nil
	}
	stmt.Name = p.curToken.Literal

	if !p.expectPeek(token.LPAREN) {
		return nil
	}

	errCountBefore := len(p.errors)
	stmt.Parameters = p.parseFunctionParameters()

	// If there was an error in parameters (stmt.Parameters is nil AND/OR errors increased), skip until end
	// Note: parseFunctionParameters returns nil on error.
	if stmt.Parameters == nil && len(p.errors) > errCountBefore {
		p.skipUntilEnd()
		return nil
	}

	// Return type arrow? `-> type`
	if p.peekTokenIs(token.ARROW) {
		p.nextToken() // eat )
		p.nextToken() // eat ->
		p.parseType() // Consumes type tokens.
	}

	stmt.Body = p.parseBlockStatement()

	if !p.curTokenIs(token.END) {
		got := p.curToken.Literal
		if p.curTokenIs(token.EOF) {
			got = "EOF"
		}
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: expected 'end', found %s",
			p.curToken.Line, p.curToken.Column, got))
		return nil
	}

	return stmt
}

func (p *Parser) parseFunctionLiteral() ast.Expression {
	lit := &ast.FunctionLiteral{Token: p.curToken}

	// Optional Name (e.g. func myName(...) ...)
	if p.peekTokenIs(token.IDENTIFIER) {
		p.nextToken()
		lit.Name = p.curToken.Literal
	}

	if !p.expectPeek(token.LPAREN) {
		return nil
	}

	errCountBefore := len(p.errors)
	lit.Parameters = p.parseFunctionParameters()

	if lit.Parameters == nil && len(p.errors) > errCountBefore {
		p.skipUntilEnd()
		return nil
	}

	// Return type
	if p.peekTokenIs(token.ARROW) {
		p.nextToken() // eat )
		p.nextToken() // eat ->
		p.parseType()
	}

	lit.Body = p.parseBlockStatement()

	if !p.curTokenIs(token.END) {
		got := p.curToken.Literal
		if p.curTokenIs(token.EOF) {
			got = "EOF"
		}
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: expected 'end', found %s",
			p.curToken.Line, p.curToken.Column, got))
		return nil
	}

	return lit
}

func (p *Parser) parseFunctionParameters() []*ast.Parameter {
	parameters := []*ast.Parameter{}

	if p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		return parameters
	}

	p.nextToken() // Eat first identifier

	// ident := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	paramName := p.curToken.Literal
	// paramToken := p.curToken

	// Expect Type: `name: type`
	// Check for missing type
	if p.peekTokenIs(token.COMMA) || p.peekTokenIs(token.RPAREN) {
		msg := fmt.Sprintf("[%d:%d] SyntaxError: missing type annotation for parameter '%s'\n  hint: use '%s: <type>'",
			p.peekToken.Line, p.peekToken.Column,
			paramName, paramName)
		p.errors = append(p.errors, msg)
		// Don't return nil instantly, maybe try to recover?
		// For now returning nil stops parsing effectively
		return nil
	}

	if !p.expectPeek(token.COLON) {
		return nil
	}
	p.nextToken()          // eat COLON
	pType := p.parseType() // eat Type

	parameters = append(parameters, &ast.Parameter{Name: paramName, Type: pType})

	for p.peekTokenIs(token.COMMA) {
		p.nextToken() // eat COMMA
		p.nextToken() // eat next IDENTIFIER
		// ident := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
		paramName = p.curToken.Literal

		if p.peekTokenIs(token.COMMA) || p.peekTokenIs(token.RPAREN) {
			msg := fmt.Sprintf("[%d:%d] SyntaxError: missing type annotation for parameter '%s'\n  hint: use '%s: <type>'",
				p.peekToken.Line, p.peekToken.Column,
				paramName, paramName)
			p.errors = append(p.errors, msg)
			return nil
		}

		if !p.expectPeek(token.COLON) {
			return nil
		}
		p.nextToken()
		pType = p.parseType()

		parameters = append(parameters, &ast.Parameter{Name: paramName, Type: pType})
	}

	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	return parameters
}

func (p *Parser) parseCallExpression(function ast.Expression) ast.Expression {
	exp := &ast.CallExpression{Token: p.curToken, Function: function}
	exp.Arguments = p.parseCallArguments()
	return exp
}

func (p *Parser) parseCallArguments() []ast.Expression {
	args := []ast.Expression{}

	if p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		return args
	}

	p.nextToken()
	args = append(args, p.parseExpression(LOWEST))

	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		args = append(args, p.parseExpression(LOWEST))
	}

	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	return args
}

func (p *Parser) parseArrayLiteral() ast.Expression {
	array := &ast.ArrayLiteral{Token: p.curToken}
	array.Elements = p.parseExpressionList(token.RBRACKET)
	return array
}

func (p *Parser) parseExpressionList(end token.TokenType) []ast.Expression {
	list := []ast.Expression{}

	// Skip initial newlines
	for p.peekTokenIs(token.NEWLINE) {
		p.nextToken()
	}

	if p.peekTokenIs(end) {
		p.nextToken()
		return list
	}

	p.nextToken()
	list = append(list, p.parseExpression(LOWEST))

	for p.peekTokenIs(token.COMMA) || p.peekTokenIs(token.NEWLINE) {
		// If newline, just skip
		if p.peekTokenIs(token.NEWLINE) {
			p.nextToken()
			continue
		}

		p.nextToken() // eat COMMA

		// Skip newlines after comma
		for p.peekTokenIs(token.NEWLINE) {
			p.nextToken()
		}

		// Check for trailing comma + end
		if p.peekTokenIs(end) {
			break
		}

		p.nextToken()
		list = append(list, p.parseExpression(LOWEST))
	}

	// Skip potential trailing newlines before closing bracket
	for p.peekTokenIs(token.NEWLINE) {
		p.nextToken()
	}

	if !p.expectPeek(end) {
		return nil
	}

	return list
}

func (p *Parser) parseIndexExpression(left ast.Expression) ast.Expression {
	exp := &ast.IndexExpression{Token: p.curToken, Left: left}

	p.nextToken()
	exp.Index = p.parseExpression(LOWEST)

	if !p.expectPeek(token.RBRACKET) {
		return nil
	}

	return exp
}

func (p *Parser) parseMapLiteral() ast.Expression {
	hash := &ast.MapLiteral{Token: p.curToken}
	hash.Keys = []ast.Expression{}
	hash.Values = []ast.Expression{}

	// Skip initial newlines
	for p.peekTokenIs(token.NEWLINE) {
		p.nextToken()
	}

	for !p.peekTokenIs(token.RBRACE) {
		p.nextToken()
		// Parse Key
		key := p.parseExpression(LOWEST)

		if !p.expectPeek(token.COLON) {
			return nil
		}

		p.nextToken()
		// Parse Value
		value := p.parseExpression(LOWEST)

		hash.Keys = append(hash.Keys, key)
		hash.Values = append(hash.Values, value)

		// Check for newline/comma
		if p.peekTokenIs(token.NEWLINE) {
			p.nextToken()
			// Skip extra newlines
			for p.peekTokenIs(token.NEWLINE) {
				p.nextToken()
			}
			// Optional comma
			if p.peekTokenIs(token.COMMA) {
				p.nextToken()
				// Skip newlines after comma
				for p.peekTokenIs(token.NEWLINE) {
					p.nextToken()
				}
			}
		} else if p.peekTokenIs(token.COMMA) {
			p.nextToken()
			// Skip newlines after comma
			for p.peekTokenIs(token.NEWLINE) {
				p.nextToken()
			}
		} else if !p.peekTokenIs(token.RBRACE) {
			// If not RBRACE and no comma/newline, error
			if !p.expectPeek(token.COMMA) {
				return nil
			}
		}
	}

	if !p.expectPeek(token.RBRACE) {
		return nil
	}

	return hash
}

func (p *Parser) parseStructStatement() *ast.StructStatement {
	stmt := &ast.StructStatement{Token: p.curToken}

	if !p.expectPeek(token.IDENTIFIER) {
		return nil
	}
	stmt.Name = p.curToken.Literal

	stmt.FieldsList = []*ast.StructField{}
	// Fields are inside until END
	// struct Point
	//    x: int
	//    y: int
	// end
	// OR comma separated?
	// Spec `struct Point x: int, y: int end` ?
	// Or block-like?
	// Noxy Python parser used `block` for struct fields?
	// Let's assume standard block-like or comma list.
	// Python parser: `parse_struct_def`.
	// Let's check spec or assume block-like structure as most Noxy constructs use `end`.

	// "struct" Name
	// Fields...
	// "end"

	p.nextToken() // move past Name.

	for !p.curTokenIs(token.END) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.NEWLINE) || p.curTokenIs(token.COMMA) {
			p.nextToken()
			continue
		}

		if p.curToken.Type != token.IDENTIFIER {
			// Error or break?
			// If not identifier, maybe illegal.
			p.nextToken()
			continue
		}

		field := &ast.StructField{Name: p.curToken.Literal}

		if !p.expectPeek(token.COLON) {
			return nil
		}
		p.nextToken() // eat COLON
		field.Type = p.parseType()

		stmt.FieldsList = append(stmt.FieldsList, field)
		p.nextToken() // eat Type (parseType ends at type token)
	}

	if !p.curTokenIs(token.END) {
		got := p.curToken.Literal
		if p.curTokenIs(token.EOF) {
			got = "EOF"
		}
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: expected 'end', found %s",
			p.curToken.Line, p.curToken.Column, got))
		return nil
	}

	return stmt
}

func (p *Parser) parseMemberAccess(left ast.Expression) ast.Expression {
	exp := &ast.MemberAccessExpression{Token: p.curToken, Left: left}

	if !p.expectPeek(token.IDENTIFIER) {
		return nil
	}
	exp.Member = p.curToken.Literal

	return exp
}

func (p *Parser) noPrefixParseFnError(t token.TokenType) {
	msg := fmt.Sprintf("[%d:%d] SyntaxError: invalid syntax %q", p.curToken.Line, p.curToken.Column, p.curToken.Literal)
	// If literal is empty (e.g. EOF), might be weird.
	if p.curToken.Type == token.EOF {
		msg = fmt.Sprintf("[%d:%d] SyntaxError: unexpected EOF", p.curToken.Line, p.curToken.Column)
	}
	p.errors = append(p.errors, msg)
}

func (p *Parser) parseForStatement() *ast.ForStatement {
	stmt := &ast.ForStatement{Token: p.curToken}

	if !p.expectPeek(token.IDENTIFIER) {
		return nil
	}

	stmt.Identifier = p.curToken.Literal

	if !p.expectPeek(token.IN) {
		return nil
	}

	p.nextToken() // eat IN

	stmt.Collection = p.parseExpression(LOWEST)

	if !p.expectPeek(token.DO) {
		return nil
	}

	// Optional newline before block
	if p.peekTokenIs(token.NEWLINE) {
		p.nextToken()
	}

	stmt.Body = p.parseBlockStatement()

	if !p.curTokenIs(token.END) {
		got := p.curToken.Literal
		if p.curTokenIs(token.EOF) {
			got = "EOF"
		}
		p.errors = append(p.errors, fmt.Sprintf("[%d:%d] SyntaxError: expected 'end' after for loop, found %s",
			p.curToken.Line, p.curToken.Column, got))
		return nil
	}

	return stmt
}
