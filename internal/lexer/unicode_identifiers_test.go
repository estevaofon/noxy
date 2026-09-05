package lexer

import (
	"testing"

	"github.com/estevaofon/noxy/internal/token"
)

type wantToken struct {
	typ token.TokenType
	lit string
}

func requireTokens(t *testing.T, source string, want []wantToken) {
	t.Helper()
	lex := New(source)
	for i, w := range want {
		got := lex.NextToken()
		if got.Type != w.typ || got.Literal != w.lit {
			t.Fatalf("source %q: token %d = %s %q, want %s %q", source, i, got.Type, got.Literal, w.typ, w.lit)
		}
	}
	if got := lex.NextToken(); got.Type != token.EOF {
		t.Fatalf("source %q: trailing token %s %q, want EOF", source, got.Type, got.Literal)
	}
}

// Issue #134 (spec §1.3): identificador comeca com letra ou '_' e continua
// com letra, digito ou '_', letras e digitos por unicode.IsLetter/IsDigit —
// como Go. Antes isLetter era ASCII e cada letra acentuada virava um
// ILLEGAL por byte.
func TestUnicodeIdentifiers(t *testing.T) {
	requireTokens(t, "let café = área + _x1 + π", []wantToken{
		{token.LET, "let"}, {token.IDENTIFIER, "café"}, {token.ASSIGN, "="},
		{token.IDENTIFIER, "área"}, {token.PLUS, "+"}, {token.IDENTIFIER, "_x1"},
		{token.PLUS, "+"}, {token.IDENTIFIER, "π"},
	})
}

// Uma keyword continua a ser reconhecida pelo literal exato; um identificador
// que a estende com uma letra Unicode e um identificador comum.
func TestUnicodeIdentifierDoesNotCollideWithKeyword(t *testing.T) {
	requireTokens(t, "for forê in", []wantToken{
		{token.FOR, "for"}, {token.IDENTIFIER, "forê"}, {token.IN, "in"},
	})
}

// Numero continua ASCII: `1é` e INT "1" seguido de IDENTIFIER "é", o mesmo
// que `1ex` (TestNextToken) e o que o scanner de Go faz.
func TestDigitThenUnicodeLetterIsNumberThenIdentifier(t *testing.T) {
	requireTokens(t, "1é", []wantToken{{token.INT, "1"}, {token.IDENTIFIER, "é"}})
}

// Caractere fora do alfabeto vira UM ILLEGAL por caractere, nao um por byte:
// aspas curvas coladas de um editor sao dois tokens, nao seis (design §2.3).
func TestUnknownCharacterIsOneIllegalPerRune(t *testing.T) {
	requireTokens(t, "“abc”", []wantToken{
		{token.ILLEGAL, "“"}, {token.IDENTIFIER, "abc"}, {token.ILLEGAL, "”"},
	})
}

// Column conta caracteres, nao bytes (design §2.4): o '@' de `let s = "aé" @`
// esta na coluna 14; em bytes seria 15.
func TestColumnCountsRunes(t *testing.T) {
	lex := New("let s = \"aé\" @\ncafé x")
	var at, x token.Token
	for {
		tok := lex.NextToken()
		if tok.Type == token.EOF {
			break
		}
		if tok.Type == token.ILLEGAL {
			at = tok
		}
		if tok.Type == token.IDENTIFIER && tok.Literal == "x" {
			x = tok
		}
	}
	if at.Line != 1 || at.Column != 14 {
		t.Fatalf("'@' at [%d:%d], want [1:14]", at.Line, at.Column)
	}
	if x.Line != 2 || x.Column != 6 {
		t.Fatalf("'x' at [%d:%d], want [2:6]", x.Line, x.Column)
	}
}
