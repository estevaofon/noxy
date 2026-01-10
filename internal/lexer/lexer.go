package lexer

import (
	"noxy-vm/internal/token"
)

type Lexer struct {
	input        string
	position     int  // current position in input (points to current char)
	readPosition int  // current reading position in input (after current char)
	ch           byte // current char under examination
	line         int
	column       int
}

func New(input string) *Lexer {
	l := &Lexer{input: input, line: 1, column: 0}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition += 1
	l.column++
}

func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

func (l *Lexer) NextToken() token.Token {
	var tok token.Token

	// Skip whitespace
	l.skipWhitespace()

	// Capture start position of the token
	startLine := l.line
	startColumn := l.column

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.EQ, Literal: string(ch) + string(l.ch)}
		} else {
			tok = newToken(token.ASSIGN, l.ch)
		}
	case '+':
		tok = newToken(token.PLUS, l.ch)
	case '-':
		if l.peekChar() == '>' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.ARROW, Literal: string(ch) + string(l.ch)}
		} else {
			tok = newToken(token.MINUS, l.ch)
		}
	case '*':
		tok = newToken(token.STAR, l.ch)
	case '/':
		if l.peekChar() == '/' {
			l.skipComment()
			return l.NextToken()
		} else {
			tok = newToken(token.SLASH, l.ch)
		}
	case '%':
		tok = newToken(token.PERCENT, l.ch)
	case '<':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LTE, Literal: string(ch) + string(l.ch)}
		} else if l.peekChar() == '<' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.SHIFT_LEFT, Literal: string(ch) + string(l.ch)}
		} else {
			tok = newToken(token.LT, l.ch)
		}
	case '>':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.GTE, Literal: string(ch) + string(l.ch)}
		} else if l.peekChar() == '>' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.SHIFT_RIGHT, Literal: string(ch) + string(l.ch)}
		} else {
			tok = newToken(token.GT, l.ch)
		}
	case '!':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.NEQ, Literal: string(ch) + string(l.ch)}
		} else {
			tok = newToken(token.NOT, l.ch)
		}
	case '&':
		if l.peekChar() == '&' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.AND, Literal: string(ch) + string(l.ch)}
		} else {
			tok = newToken(token.BIT_AND, l.ch)
		}
	case '|':
		if l.peekChar() == '|' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.OR, Literal: string(ch) + string(l.ch)}
		} else {
			tok = newToken(token.BIT_OR, l.ch)
		}
	case '^':
		tok = newToken(token.BIT_XOR, l.ch)
	case '~':
		tok = newToken(token.BIT_NOT, l.ch)
	case '(':
		tok = newToken(token.LPAREN, l.ch)
	case ')':
		tok = newToken(token.RPAREN, l.ch)
	case '{':
		tok = newToken(token.LBRACE, l.ch)
	case '}':
		tok = newToken(token.RBRACE, l.ch)
	case '[':
		tok = newToken(token.LBRACKET, l.ch)
	case ']':
		tok = newToken(token.RBRACKET, l.ch)
	case ',':
		tok = newToken(token.COMMA, l.ch)
	case ':':
		tok = newToken(token.COLON, l.ch)
	case '.':
		tok = newToken(token.DOT, l.ch)
	case '\n':
		tok = newToken(token.NEWLINE, l.ch)
		// For NEWLINE token, we want the line/col of the newline char itself
		tok.Line = startLine
		tok.Column = startColumn
		l.line++
		l.column = 0 // Reset for next line
		l.readChar()
		return tok
	case '"':
		lit, ok := l.readString('"')
		if !ok {
			tok.Type = token.ILLEGAL
			tok.Literal = "unterminated string"
		} else {
			tok.Type = token.STRING
			tok.Literal = lit
		}
	case '\'':
		lit, ok := l.readString('\'')
		if !ok {
			tok.Type = token.ILLEGAL
			tok.Literal = "unterminated string"
		} else {
			tok.Type = token.STRING
			tok.Literal = lit
		}
	case 'b': // Potential bytes literal
		if l.peekChar() == '"' || l.peekChar() == '\'' {
			quote := l.peekChar()
			l.readChar() // eat 'b'
			lit, ok := l.readBytes(quote)
			if !ok {
				tok.Type = token.ILLEGAL
				tok.Literal = "unterminated bytes literal"
			} else {
				tok.Type = token.BYTES
				tok.Literal = lit
			}
		} else {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			// Early return for identifier needed because readIdentifier advances
			tok.Line = startLine
			tok.Column = startColumn
			return tok
		}
	case 'f': // Potential f-string
		if l.peekChar() == '"' || l.peekChar() == '\'' {
			quote := l.peekChar()
			l.readChar() // eat 'f'
			lit, ok := l.readFString(quote)
			if !ok {
				tok.Type = token.ILLEGAL
				tok.Literal = "unterminated f-string"
			} else {
				tok.Type = token.FSTRING
				tok.Literal = lit
			}
		} else {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			tok.Line = startLine
			tok.Column = startColumn
			return tok
		}
	case 0:
		tok.Literal = ""
		tok.Type = token.EOF
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			tok.Line = startLine
			tok.Column = startColumn
			return tok
		} else if isDigit(l.ch) {
			tok.Type, tok.Literal = l.readNumber()
			tok.Line = startLine
			tok.Column = startColumn
			return tok
		} else {
			tok = newToken(token.ILLEGAL, l.ch)
		}
	}

	// Apply position info to all other tokens
	tok.Line = startLine
	tok.Column = startColumn

	l.readChar()
	return tok
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
		l.readChar()
	}
}

func (l *Lexer) skipComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
	// Note: We stop at '\n' which will be consumed by NextToken and return a NEWLINE token
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) || l.ch == '_' {
		l.readChar()
	}
	return l.input[position:l.position]
}

func (l *Lexer) readNumber() (token.TokenType, string) {
	position := l.position

	if l.ch == '0' && (l.peekChar() == 'x' || l.peekChar() == 'X') {
		l.readChar() // 0
		l.readChar() // x
		for isHexDigit(l.ch) {
			l.readChar()
		}
		return token.INT, l.input[position:l.position]
	}

	isFloat := false
	for isDigit(l.ch) {
		l.readChar()
	}
	if l.ch == '.' {
		isFloat = true
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	if isFloat {
		return token.FLOAT, l.input[position:l.position]
	}
	return token.INT, l.input[position:l.position]
}

func isHexDigit(ch byte) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func (l *Lexer) readString(quote byte) (string, bool) {
	// l.ch is currently quote
	l.readChar() // Skip opening quote

	var out []byte

	for {
		if l.ch == 0 {
			return string(out), false
		}
		if l.ch == quote {
			break
		}
		if l.ch == '\\' {
			l.readChar() // Skip backslash
			switch l.ch {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case '"':
				out = append(out, '"')
			case '\'':
				out = append(out, '\'')
			case '\\':
				out = append(out, '\\')
			default:
				// Unknown escape, keep literal backslash and char?
				// Or just the char?
				// Usually languages keep the backslash if invalid escape, or error.
				// Let's keep it simple: just the char (like Go? No Go errors).
				// Python: '\z' -> '\z'.
				// Let's replicate python behavior roughly: if not special, keep backslash + char.
				out = append(out, '\\')
				out = append(out, l.ch)
			}
		} else {
			out = append(out, l.ch)
		}
		l.readChar()
	}

	return string(out), true
}

func (l *Lexer) readBytes(quote byte) (string, bool) {
	l.readChar()

	var out []byte

	for {
		if l.ch == 0 {
			return string(out), false
		}
		if l.ch == quote {
			break
		}
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case '"':
				out = append(out, '"')
			case '\'':
				out = append(out, '\'')
			case '\\':
				out = append(out, '\\')
			default:
				out = append(out, '\\')
				out = append(out, l.ch)
			}
		} else {
			out = append(out, l.ch)
		}
		l.readChar()
	}
	return string(out), true // The parser converts this string to Bytes Value
}

func (l *Lexer) readFString(quote byte) (string, bool) {
	l.readChar() // Skip opening quote

	var out []byte

	for {
		if l.ch == 0 {
			return string(out), false
		}
		if l.ch == quote {
			break
		}
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case '"':
				out = append(out, '"')
			case '\'':
				out = append(out, '\'')
			case '\\':
				out = append(out, '\\')
			case '{': // Escaped interpolation start?
				out = append(out, '{')
			default:
				out = append(out, '\\')
				out = append(out, l.ch)
			}
		} else {
			out = append(out, l.ch)
		}
		l.readChar()
	}
	return string(out), true
}

func newToken(tokenType token.TokenType, ch byte) token.Token {
	return token.Token{Type: tokenType, Literal: string(ch)}
}

func isLetter(ch byte) bool {
	return 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z' || ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}
