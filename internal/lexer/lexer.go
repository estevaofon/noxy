package lexer

import (
	"unicode"
	"unicode/utf8"

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
	// Coluna em CARACTERES, nao em bytes (issue #134): um byte de
	// continuacao UTF-8 (10xxxxxx) pertence ao caractere que o byte
	// anterior comecou e nao abre coluna nova. Linhas ASCII nao mudam.
	if l.ch&0xC0 != 0x80 {
		l.column++
	}
}

func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

// currentRune decodifica o caractere UTF-8 que comeca em l.position. Para
// ASCII (o caso comum) devolve rune(l.ch) e size 1 sem decodificar. Um byte
// invalido decodifica como utf8.RuneError com size 1.
func (l *Lexer) currentRune() (r rune, size int) {
	if l.ch < utf8.RuneSelf {
		return rune(l.ch), 1
	}
	return utf8.DecodeRuneInString(l.input[l.position:])
}

// advanceRune consome o caractere inteiro devolvido por currentRune.
func (l *Lexer) advanceRune(size int) {
	for i := 0; i < size; i++ {
		l.readChar()
	}
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
	case '?':
		tok = newToken(token.QUESTION, l.ch)
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
		lit, reason := l.readQuoted('"', literalString)
		if reason != "" {
			tok.Type = token.LEXER_ERROR
			tok.Literal = reason
		} else {
			tok.Type = token.STRING
			tok.Literal = lit
		}
	case '\'':
		lit, reason := l.readQuoted('\'', literalString)
		if reason != "" {
			tok.Type = token.LEXER_ERROR
			tok.Literal = reason
		} else {
			tok.Type = token.STRING
			tok.Literal = lit
		}
	case 'b': // Potential bytes literal
		if l.peekChar() == '"' || l.peekChar() == '\'' {
			quote := l.peekChar()
			l.readChar() // eat 'b'
			lit, reason := l.readQuoted(quote, literalBytes)
			if reason != "" {
				tok.Type = token.LEXER_ERROR
				tok.Literal = reason
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
			lit, reason := l.readQuoted(quote, literalFString)
			if reason != "" {
				tok.Type = token.LEXER_ERROR
				tok.Literal = reason
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
		r, size := l.currentRune()
		if isIdentStart(r) {
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
			// Caractere fora do alfabeto: UM token ILLEGAL por CARACTERE,
			// com a runa inteira como literal (issue #134). Retorna cedo,
			// como os ramos acima: a cauda comum de NextToken faz um
			// readChar, que avancaria so um byte de um caractere multibyte.
			tok = token.Token{Type: token.ILLEGAL, Literal: string(r), Line: startLine, Column: startColumn}
			l.advanceRune(size)
			return tok
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
	for {
		r, size := l.currentRune()
		if !isIdentPart(r) {
			break
		}
		l.advanceRune(size)
	}
	return l.input[position:l.position]
}

func (l *Lexer) readNumber() (token.TokenType, string) {
	position := l.position

	if l.ch == '0' {
		if l.peekChar() == 'x' || l.peekChar() == 'X' {
			l.readChar() // 0
			l.readChar() // x
			for isHexDigit(l.ch) {
				l.readChar()
			}
			return token.INT, l.input[position:l.position]
		}
		if l.peekChar() == 'b' || l.peekChar() == 'B' {
			l.readChar() // 0
			l.readChar() // b
			for isBinaryDigit(l.ch) {
				l.readChar()
			}
			return token.INT, l.input[position:l.position]
		}
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
	// Expoente: [eE][+-]?digitos — so quando de fato ha digito depois, para
	// que `1e` seguido de outra coisa continue lexando INT + identificador.
	if (l.ch == 'e' || l.ch == 'E') && l.exponentAhead() {
		isFloat = true
		l.readChar() // e
		if l.ch == '+' || l.ch == '-' {
			l.readChar()
		}
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	if isFloat {
		return token.FLOAT, l.input[position:l.position]
	}
	return token.INT, l.input[position:l.position]
}

func isBinaryDigit(ch byte) bool {
	return ch == '0' || ch == '1'
}

func isHexDigit(ch byte) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

// exponentAhead responde se, com l.ch em 'e'/'E', os proximos caracteres
// formam um expoente valido: digito, ou sinal seguido de digito.
func (l *Lexer) exponentAhead() bool {
	next := l.peekChar()
	if isDigit(next) {
		return true
	}
	if next != '+' && next != '-' {
		return false
	}
	if l.readPosition+1 >= len(l.input) {
		return false
	}
	return isDigit(l.input[l.readPosition+1])
}

// literalKind selects which escape rules apply to a quoted literal.
type literalKind int

const (
	literalString literalKind = iota
	literalBytes
	literalFString
)

func (k literalKind) name() string {
	switch k {
	case literalBytes:
		return "bytes literal"
	case literalFString:
		return "f-string"
	default:
		return "string"
	}
}

func hexValue(ch byte) (int, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0'), true
	case ch >= 'a' && ch <= 'f':
		return int(ch-'a') + 10, true
	case ch >= 'A' && ch <= 'F':
		return int(ch-'A') + 10, true
	}
	return 0, false
}

// appendRune encodes a validated codepoint as UTF-8. Bytes literals take the
// same encoding: b"caf\u{e9}" is the UTF-8 spelling of café.
func appendRune(out []byte, code int) []byte {
	return utf8.AppendRune(out, rune(code))
}

// readEscape consumes one escape sequence. l.ch is the character after the
// backslash on entry and the last consumed character on return. An empty
// reason means success.
//
// Escapes fall into three groups. The historical set (\n \r \t \" \' \\, plus
// \{ in f-strings) is unchanged. \x writes one raw byte and is confined to
// bytes literals: allowing it in a string would let a source file build a
// string holding invalid UTF-8, which is the invariant to_str exists to
// enforce. \u writes a validated codepoint and is accepted everywhere.
//
// An unrecognised escape keeps its current permissive behaviour of preserving
// the backslash, so a Windows path such as "C:\path" still lexes. That
// permissiveness is why \u commits to the strict form only once it sees a
// brace or a hex digit: "C:\users" has no valid escape after the backslash and
// stays literal, while "\u01" is unambiguously a malformed escape.
func (l *Lexer) readEscape(out []byte, kind literalKind) ([]byte, string) {
	switch l.ch {
	case 'n':
		return append(out, '\n'), ""
	case 'r':
		return append(out, '\r'), ""
	case 't':
		return append(out, '\t'), ""
	case '"':
		return append(out, '"'), ""
	case '\'':
		return append(out, '\''), ""
	case '\\':
		return append(out, '\\'), ""
	case '{':
		if kind == literalFString {
			return append(out, '{'), ""
		}
	case 'x':
		return l.readHexEscape(out, kind)
	case 'u':
		if l.peekChar() == '{' {
			return l.readBracedUnicodeEscape(out)
		}
		if _, isHex := hexValue(l.peekChar()); isHex {
			return l.readFourDigitUnicodeEscape(out)
		}
	}
	return append(append(out, '\\'), l.ch), ""
}

// readHexEscape consumes \xNN, which writes the raw byte NN.
func (l *Lexer) readHexEscape(out []byte, kind literalKind) ([]byte, string) {
	if kind != literalBytes {
		return out, `\x escape is only valid in a bytes literal; use \u{...} to write a character`
	}
	code := 0
	for i := 0; i < 2; i++ {
		digit, isHex := hexValue(l.peekChar())
		if !isHex {
			return out, `\x escape needs 2 hex digits`
		}
		l.readChar()
		code = code*16 + digit
	}
	return append(out, byte(code)), ""
}

// readBracedUnicodeEscape consumes \u{...} with 1 to 6 hex digits.
func (l *Lexer) readBracedUnicodeEscape(out []byte) ([]byte, string) {
	l.readChar() // consume '{'
	code := 0
	digits := 0
	for {
		next := l.peekChar()
		if next == '}' {
			l.readChar()
			break
		}
		if next == 0 || next == '"' || next == '\'' || next == '\n' {
			return out, "unterminated unicode escape"
		}
		digit, isHex := hexValue(next)
		if !isHex || digits == 6 {
			return out, `\u{...} escape needs 1 to 6 hex digits`
		}
		l.readChar()
		code = code*16 + digit
		digits++
	}
	if digits == 0 {
		return out, `\u{...} escape needs 1 to 6 hex digits`
	}
	return appendValidatedRune(out, code)
}

// readFourDigitUnicodeEscape consumes \uNNNN, the form three shipped examples
// already use for ANSI sequences.
func (l *Lexer) readFourDigitUnicodeEscape(out []byte) ([]byte, string) {
	code := 0
	for i := 0; i < 4; i++ {
		digit, isHex := hexValue(l.peekChar())
		if !isHex {
			return out, `\uNNNN escape needs 4 hex digits`
		}
		l.readChar()
		code = code*16 + digit
	}
	return appendValidatedRune(out, code)
}

func appendValidatedRune(out []byte, code int) ([]byte, string) {
	if code >= 0xD800 && code <= 0xDFFF {
		return out, "unicode escape is a surrogate codepoint, which is not a character"
	}
	if code > 0x10FFFF {
		return out, "unicode escape is out of range for a codepoint"
	}
	return appendRune(out, code), ""
}

// readQuoted scans the body of a quoted literal. An empty reason means
// success; otherwise it describes why the literal is illegal.
//
// F-strings are brace-aware (issue #126, the PEP 701 rule): inside a `{...}`
// expression a `"` or a `'` — either quote character, not just the one
// matching the f-string's own delimiter (needed for e.g. f"{a['k']}") —
// opens a nested string literal (copied verbatim, escapes included — the
// parser re-lexes the expression) instead of closing the f-string. At depth
// 0 `{{` and `}}` are literal braces and do not change the depth. A `{`
// still open at a raw newline or at EOF is reported here, next to its
// cause.
func (l *Lexer) readQuoted(quote byte, kind literalKind) (string, string) {
	l.readChar() // Skip opening quote

	var out []byte
	depth := 0 // f-string only: `{` of expressions still open

	for {
		if l.ch == 0 {
			if depth > 0 {
				return string(out), UnclosedBraceReason
			}
			return string(out), "unterminated " + kind.name()
		}
		if l.ch == quote && depth == 0 {
			break
		}
		switch {
		case kind == literalFString && depth > 0 && l.ch == '\n':
			return string(out), UnclosedBraceReason
		case kind == literalFString && depth == 0 && (l.ch == '{' || l.ch == '}') && l.peekChar() == l.ch:
			out = append(out, l.ch, l.ch)
			l.readChar()
		case kind == literalFString && l.ch == '{':
			depth++
			out = append(out, '{')
		case kind == literalFString && depth > 0 && l.ch == '}':
			depth--
			out = append(out, '}')
		case kind == literalFString && depth > 0 && (l.ch == '"' || l.ch == '\''):
			var reason string
			out, reason = l.readNestedQuoted(out, l.ch)
			if reason != "" {
				return string(out), reason
			}
		case l.ch == '\\':
			l.readChar() // Skip backslash
			if l.ch == 0 {
				return string(out), "unterminated " + kind.name()
			}
			var reason string
			out, reason = l.readEscape(out, kind)
			if reason != "" {
				return string(out), reason
			}
		default:
			out = append(out, l.ch)
		}
		l.readChar()
	}
	return string(out), ""
}

const UnclosedBraceReason = "unclosed brace in f-string\n  hint: every '{' that starts an expression needs a matching '}'; write '{{' for a literal brace"

// readNestedQuoted copies a string literal that appears INSIDE an f-string
// expression, verbatim (opening quote, body with escapes untouched, closing
// quote), leaving l.ch on the closing quote. The parser re-lexes the
// expression, so the escapes are interpreted there.
//
// A raw newline here means the nested literal — and therefore the `{`
// expression around it — cannot close on this line, same as an unescaped
// newline anywhere else at depth > 0: reported as UnclosedBraceReason rather
// than a plain "unterminated string", since the actionable fix is the same
// missing `}`.
func (l *Lexer) readNestedQuoted(out []byte, quote byte) ([]byte, string) {
	out = append(out, quote)
	l.readChar()
	for {
		if l.ch == 0 {
			return out, UnclosedBraceReason
		}
		if l.ch == '\n' {
			return out, UnclosedBraceReason
		}
		if l.ch == quote {
			return append(out, quote), ""
		}
		if l.ch == '\\' {
			out = append(out, '\\')
			l.readChar()
			if l.ch == 0 {
				return out, UnclosedBraceReason
			}
		}
		out = append(out, l.ch)
		l.readChar()
	}
}

func newToken(tokenType token.TokenType, ch byte) token.Token {
	return token.Token{Type: tokenType, Literal: string(ch)}
}

// isIdentStart / isIdentPart definem identificador como Go (spec §1.3):
// comeca com letra ou '_', continua com letra, digito ou '_', com letra e
// digito por unicode.IsLetter/IsDigit. Numeros NAO passam por aqui —
// readNumber continua a usar o isDigit ASCII abaixo.
func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}
