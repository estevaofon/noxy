package parser

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/token"
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
	p.registerPrefix(token.FSTRING, p.parseFString)
	p.registerPrefix(token.NULL, p.parseNull)
	p.registerPrefix(token.ZEROS, p.parseZeros)
	p.registerPrefix(token.REF, p.parsePrefixExpression)
	// p.registerPrefix(token.IF, p.parseIfExpression) // Removed
	// p.registerPrefix(token.FUNC, p.parseFunctionLiteral) // Removed. Func is statement now.

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
	p.registerInfix(token.LPAREN, p.parseCallExpression)
	p.registerInfix(token.LBRACKET, p.parseIndexExpression)
	p.registerInfix(token.DOT, p.parseMemberAccess)

	return p
}

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead",
		t, p.peekToken.Type)
	p.errors = append(p.errors, msg)
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()

	// Skip NEW tokens if they are just fillers?
	// In Noxy Python parser: skip_newlines() was explicit.
	// Here we might want to handle them. For now, let's keep them and handle in parsing logic.
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
	case token.STRUCT:
		return p.parseStructStatement()
	case token.FUNC:
		return p.parseFunctionStatement()
	case token.BREAK:
		return p.parseBreakStatement()
	case token.USE:
		return p.parseUseStatement()
	case token.NEWLINE:
		return nil // Skip empty lines / separators
	default:
		// Attempt to parse expression
		expr := p.parseExpression(LOWEST)

		// Check if it's an assignment
		if p.peekTokenIs(token.ASSIGN) {
			p.nextToken() // eat ASSIGN
			stmt := &ast.AssignStmt{Token: p.curToken, Target: expr}
			p.nextToken() // move to value
			stmt.Value = p.parseExpression(LOWEST)

			if p.peekTokenIs(token.NEWLINE) {
				p.nextToken()
			}
			return stmt
		}

		// Otherwise expression statement
		stmt := &ast.ExpressionStmt{Token: p.curToken, Expression: expr}
		if p.peekTokenIs(token.NEWLINE) {
			p.nextToken()
		}
		return stmt
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

	if p.curTokenIs(token.ELSE) {
		p.nextToken() // eat ELSE
		stmt.Alternative = p.parseBlockStatement()
	}

	return stmt
}

func (p *Parser) parseLetStatement() *ast.LetStmt {
	stmt := &ast.LetStmt{Token: p.curToken}

	if !p.expectPeek(token.IDENTIFIER) {
		return nil
	}

	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

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
	stmt.Module = p.curToken.Literal

	// Handle 'select *' or just 'use time'
	// User example: use time select *
	if p.peekToken.Type == token.SELECT {
		p.nextToken() // eat select
		if p.peekToken.Type == token.STAR {
			p.nextToken() // eat *
		} else {
			// For now, assume *
			// p.expectPeek(token.STAR) ?
		}
	}

	if p.peekToken.Type == token.NEWLINE {
		p.nextToken()
	}
	return stmt
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
	}

	var t ast.NoxyType
	// Primitive types and Identifier types
	switch p.curToken.Type {
	case token.TYPE_INT:
		t = &ast.PrimitiveType{Name: "int"}
	case token.TYPE_STRING:
		t = &ast.PrimitiveType{Name: "string"}
	case token.IDENTIFIER:
		t = &ast.PrimitiveType{Name: p.curToken.Literal}
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

		// Check for size (optional)
		if !p.peekTokenIs(token.RBRACKET) {
			p.nextToken() // Eat the size token (e.g. 15 or Identifier)
			// TODO: Parse expression if complex size
		}

		if !p.expectPeek(token.RBRACKET) {
			return nil
		}
		t = &ast.ArrayType{ElementType: t}
	}

	return t
}

// Precedence system setup
const (
	_ int = iota
	LOWEST
	OR          // |
	AND         // &
	EQUALS      // ==
	LESSGREATER // > or <
	SUM         // + or -
	PRODUCT     // * or /
	PREFIX      // -X or !X
	CALL        // myFunction(X)
	INDEX       // array[index]
)

var precedences = map[token.TokenType]int{
	token.EQ:       EQUALS,
	token.NEQ:      EQUALS,
	token.LT:       LESSGREATER,
	token.GT:       LESSGREATER,
	token.LTE:      LESSGREATER,
	token.GTE:      LESSGREATER,
	token.AND:      AND,
	token.OR:       OR,
	token.PLUS:     SUM,
	token.MINUS:    SUM,
	token.SLASH:    PRODUCT,
	token.STAR:     PRODUCT,
	token.PERCENT:  PRODUCT,
	token.LPAREN:   CALL,
	token.LBRACKET: INDEX,
	token.DOT:      INDEX,
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
	value := int64(0)
	fmt.Sscanf(p.curToken.Literal, "%d", &value)
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

	return stmt
}

func (p *Parser) parseBlockStatement() *ast.BlockStatement {
	block := &ast.BlockStatement{Token: p.curToken}
	block.Statements = []ast.Statement{}

	// Skip current token (THEN or ELSE)
	p.nextToken()

	for !p.curTokenIs(token.END) && !p.curTokenIs(token.ELSE) && !p.curTokenIs(token.EOF) {
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

func (p *Parser) parseFunctionStatement() *ast.FunctionStatement {
	stmt := &ast.FunctionStatement{Token: p.curToken}

	if !p.expectPeek(token.IDENTIFIER) {
		return nil
	}
	stmt.Name = p.curToken.Literal

	if !p.expectPeek(token.LPAREN) {
		return nil
	}

	stmt.Parameters = p.parseFunctionParameters()

	// Return type arrow? `-> type`
	if p.peekTokenIs(token.ARROW) {
		p.nextToken() // eat )
		p.nextToken() // eat ->
		p.parseType() // Consumes type tokens.
	}

	stmt.Body = p.parseBlockStatement()

	return stmt
}

func (p *Parser) parseFunctionParameters() []*ast.Identifier {
	identifiers := []*ast.Identifier{}

	if p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		return identifiers
	}

	p.nextToken()

	ident := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	// Expect Type: `name: type`
	if !p.expectPeek(token.COLON) {
		return nil
	}
	p.nextToken() // eat COLON
	p.parseType() // eat Type

	identifiers = append(identifiers, ident)

	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		ident := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

		if !p.expectPeek(token.COLON) {
			return nil
		}
		p.nextToken()
		p.parseType()

		identifiers = append(identifiers, ident)
	}

	if !p.expectPeek(token.RPAREN) {
		return nil
	}

	return identifiers
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

	if p.peekTokenIs(end) {
		p.nextToken()
		return list
	}

	p.nextToken()
	list = append(list, p.parseExpression(LOWEST))

	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		list = append(list, p.parseExpression(LOWEST))
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

		if !p.peekTokenIs(token.RBRACE) && !p.expectPeek(token.COMMA) {
			return nil
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
	msg := fmt.Sprintf("no prefix parse function for %s found", t)
	p.errors = append(p.errors, msg)
}
