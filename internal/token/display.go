package token

var tokenDisplay = map[TokenType]string{
	INT:        "integer",
	FLOAT:      "float",
	STRING:     "string",
	BYTES:      "bytes literal",
	FSTRING:    "formatted string",
	IDENTIFIER: "identifier",

	LET:    "let",
	GLOBAL: "global",
	FUNC:   "func",
	STRUCT: "struct",
	IF:     "if",
	THEN:   "then",
	ELSE:   "else",
	END:    "end",
	WHILE:  "while",
	DO:     "do",
	RETURN: "return",
	BREAK:  "break",

	TYPE_INT:    "int",
	TYPE_FLOAT:  "float",
	TYPE_STRING: "string",
	TYPE_STR:    "str",
	TYPE_BOOL:   "bool",
	TYPE_BYTES:  "bytes",
	TYPE_VOID:   "void",
	REF:         "ref",
	MAP:         "map",

	TRUE:  "true",
	FALSE: "false",
	NULL:  "null",

	USE:    "use",
	SELECT: "select",
	AS:     "as",
	ZEROS:  "zeros",

	PLUS:    "'+'",
	MINUS:   "'-'",
	STAR:    "'*'",
	SLASH:   "'/'",
	PERCENT: "'%'",

	GT:  "'>'",
	LT:  "'<'",
	GTE: "'>='",
	LTE: "'<='",
	EQ:  "'=='",
	NEQ: "'!='",

	AND: "'&'",
	OR:  "'|'",
	NOT: "'!'",

	ASSIGN: "'='",
	ARROW:  "'->'",

	LPAREN:   "'('",
	RPAREN:   "')'",
	LBRACKET: "'['",
	RBRACKET: "']'",
	LBRACE:   "'{'",
	RBRACE:   "'}'",
	COMMA:    "','",
	COLON:    "':'",
	DOT:      "'.'",

	NEWLINE: "newline",
	EOF:     "end of file",
	ILLEGAL: "illegal token",
}

func (t TokenType) Display() string {
	if s, ok := tokenDisplay[t]; ok {
		return s
	}
	return string(t)
}
