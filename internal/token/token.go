package token

import "fmt"

type TokenType string

const (
	// Literais
	INT     TokenType = "INT"
	FLOAT   TokenType = "FLOAT"
	STRING  TokenType = "STRING"
	BYTES   TokenType = "BYTES"
	FSTRING TokenType = "FSTRING"

	// Identificador
	IDENTIFIER TokenType = "IDENTIFIER"

	// Palavras-chave - Declarações
	LET    TokenType = "LET"
	GLOBAL TokenType = "GLOBAL"
	FUNC   TokenType = "FUNC"
	STRUCT TokenType = "STRUCT"

	// Palavras-chave - Controle de fluxo
	IF      TokenType = "IF"
	THEN    TokenType = "THEN"
	ELSE    TokenType = "ELSE"
	ELIF    TokenType = "ELIF"
	END     TokenType = "END"
	WHILE   TokenType = "WHILE"
	DO      TokenType = "DO"
	RETURN  TokenType = "RETURN"
	BREAK   TokenType = "BREAK"
	FOR     TokenType = "FOR"
	IN      TokenType = "IN"
	WHEN    TokenType = "WHEN"
	CASE    TokenType = "CASE"
	DEFAULT TokenType = "DEFAULT"

	// Palavras-chave - Tipos
	TYPE_INT    TokenType = "TYPE_INT"
	TYPE_FLOAT  TokenType = "TYPE_FLOAT"
	TYPE_STRING TokenType = "TYPE_STRING"
	TYPE_STR    TokenType = "TYPE_STR"
	TYPE_BOOL   TokenType = "TYPE_BOOL"
	TYPE_BYTES  TokenType = "TYPE_BYTES"
	TYPE_VOID   TokenType = "TYPE_VOID"
	TYPE_ANY    TokenType = "TYPE_ANY" // Add ANY
	REF         TokenType = "REF"
	MAP         TokenType = "MAP"
	CHAN        TokenType = "CHAN"

	// Palavras-chave - Literais
	TRUE  TokenType = "TRUE"
	FALSE TokenType = "FALSE"
	NULL  TokenType = "NULL"

	// Palavras-chave - Módulos
	USE    TokenType = "USE"
	SELECT TokenType = "SELECT"
	AS     TokenType = "AS"

	// Palavras-chave - Especiais
	ZEROS TokenType = "ZEROS"

	// Operadores aritméticos
	PLUS    TokenType = "PLUS"    // +
	MINUS   TokenType = "MINUS"   // -
	STAR    TokenType = "STAR"    // *
	SLASH   TokenType = "SLASH"   // /
	PERCENT TokenType = "PERCENT" // %

	// Operadores de comparação
	GT  TokenType = "GT"  // >
	LT  TokenType = "LT"  // <
	GTE TokenType = "GTE" // >=
	LTE TokenType = "LTE" // <=
	EQ  TokenType = "EQ"  // ==
	NEQ TokenType = "NEQ" // !=

	// Operadores lógicos
	AND TokenType = "AND" // &&
	OR  TokenType = "OR"  // ||
	NOT TokenType = "NOT" // !

	// Operadores Bitwise
	BIT_AND     TokenType = "BIT_AND"     // &
	BIT_OR      TokenType = "BIT_OR"      // |
	BIT_XOR     TokenType = "BIT_XOR"     // ^
	BIT_NOT     TokenType = "BIT_NOT"     // ~
	SHIFT_LEFT  TokenType = "SHIFT_LEFT"  // <<
	SHIFT_RIGHT TokenType = "SHIFT_RIGHT" // >>

	// Atribuição
	ASSIGN TokenType = "ASSIGN" // =

	// Retorno de função
	ARROW TokenType = "ARROW" // ->

	// Delimitadores
	LPAREN   TokenType = "LPAREN"   // (
	RPAREN   TokenType = "RPAREN"   // )
	LBRACKET TokenType = "LBRACKET" // [
	RBRACKET TokenType = "RBRACKET" // ]
	LBRACE   TokenType = "LBRACE"   // {
	RBRACE   TokenType = "RBRACE"   // }
	COMMA    TokenType = "COMMA"    // ,
	COLON    TokenType = "COLON"    // :
	DOT      TokenType = "DOT"      // .

	// Especiais
	NEWLINE TokenType = "NEWLINE"
	EOF     TokenType = "EOF"
	ILLEGAL TokenType = "ILLEGAL"
)

var keywords = map[string]TokenType{
	"let":     LET,
	"global":  GLOBAL,
	"func":    FUNC,
	"struct":  STRUCT,
	"if":      IF,
	"then":    THEN,
	"else":    ELSE,
	"end":     END,
	"while":   WHILE,
	"do":      DO,
	"return":  RETURN,
	"break":   BREAK,
	"int":     TYPE_INT,
	"float":   TYPE_FLOAT,
	"string":  TYPE_STRING,
	"str":     TYPE_STR,
	"bool":    TYPE_BOOL,
	"bytes":   TYPE_BYTES,
	"void":    TYPE_VOID,
	"any":     TYPE_ANY, // Add any keyword
	"ref":     REF,
	"map":     MAP,
	"chan":    CHAN,
	"true":    TRUE,
	"false":   FALSE,
	"null":    NULL,
	"use":     USE,
	"select":  SELECT,
	"as":      AS,
	"zeros":   ZEROS,
	"elif":    ELIF,
	"for":     FOR,
	"in":      IN,
	"when":    WHEN,
	"case":    CASE,
	"default": DEFAULT,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENTIFIER
}

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
}

func (t Token) String() string {
	return fmt.Sprintf("Token(%s, %q, Line: %d, Col: %d)", t.Type, t.Literal, t.Line, t.Column)
}
