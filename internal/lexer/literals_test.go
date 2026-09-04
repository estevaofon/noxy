package lexer

import (
	"testing"

	"noxy-vm/internal/token"
)

// Literais inteiros binários (spec §2.1: `0b…` ao lado de `0x…`) — o único
// prefixo numérico que nenhum teste Go exercitava.
func TestBinaryIntegerLiterals(t *testing.T) {
	cases := []struct{ source, want string }{
		{"0b1010", "0b1010"},
		{"0B11", "0B11"},
		{"0b0", "0b0"},
	}
	for _, tc := range cases {
		requireLiteral(t, tc.source, token.INT, tc.want)
	}
	// O literal termina no primeiro dígito que não é 0/1: `0b102` lexa como
	// `0b10` seguido de `2` — mesmo recorte de `0xFG` (0xF, G).
	lex := New("0b102")
	first := lex.NextToken()
	second := lex.NextToken()
	if first.Type != token.INT || first.Literal != "0b10" || second.Type != token.INT || second.Literal != "2" {
		t.Fatalf("0b102 lexou como %s(%q) %s(%q), want INT(0b10) INT(2)", first.Type, first.Literal, second.Type, second.Literal)
	}
}

// Escapes de aspa: `\'` em string de aspas simples e `\"` em aspas duplas
// produzem o caractere literal; `\\` produz uma barra.
func TestQuoteAndBackslashEscapes(t *testing.T) {
	requireLiteral(t, `'it\'s'`, token.STRING, "it's")
	requireLiteral(t, `"say \"hi\""`, token.STRING, `say "hi"`)
	requireLiteral(t, `"a\\b"`, token.STRING, `a\b`)
	requireLiteral(t, `'a\\b'`, token.STRING, `a\b`)
}

// Literal cortado logo depois de uma barra (`"abc\` no fim do arquivo) e
// string de aspas simples sem fechamento são ILLEGAL com motivo legível, não
// um token STRING com lixo.
func TestUnterminatedLiteralAfterBackslashAndSingleQuotes(t *testing.T) {
	requireIllegal(t, `"abc\`, "unterminated")
	requireIllegal(t, `'abc`, "unterminated")
	requireIllegal(t, `'abc\`, "unterminated")
}

// Um caractere fora do alfabeto da linguagem vira um token ILLEGAL com o
// próprio caractere como literal, e o lexer segue em frente (não trava).
func TestUnknownCharacterIsIllegalToken(t *testing.T) {
	lex := New("let x: int = 1 @ 2\n")
	var illegal []string
	for {
		tok := lex.NextToken()
		if tok.Type == token.EOF {
			break
		}
		if tok.Type == token.ILLEGAL {
			illegal = append(illegal, tok.Literal)
		}
	}
	if len(illegal) != 1 || illegal[0] != "@" {
		t.Fatalf("tokens ILLEGAL = %q, want [\"@\"]", illegal)
	}
}

// Issue #126 item 3 (PEP 701): dentro de `{...}` de uma f-string, uma aspa
// igual a do delimitador abre um literal aninhado em vez de fechar a
// f-string. O lexer conta a profundidade de chaves; o parser continua
// recebendo o literal inteiro e re-lexando cada `{...}`.
func TestFStringQuotesInsideBraces(t *testing.T) {
	cases := []struct {
		name, source, want string
	}{
		{"double inside double", `f"n = {fmt("%03d", n)}"`, `n = {fmt("%03d", n)}`},
		{"single inside single", `f'{fmt('%d', n)}'`, `{fmt('%d', n)}`},
		{"map literal with space", `f"{ {"a": 1}["a"] }"`, `{ {"a": 1}["a"] }`},
		{"escaped quote inside nested string stays verbatim", `f"{s + "a\"b"}"`, `{s + "a\"b"}`},
		{"literal braces still escape at depth zero", `f"{{x}} = {x}"`, `{{x}} = {x}`},
		{"nested braces close in order", `f"{{{x}}}"`, `{{{x}}}`},
		{"brace char inside nested string does not count", `f"{"}"}!"`, `{"}"}!`},
		{"text after the expression", `f"{a}: {b}"`, `{a}: {b}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireLiteral(t, tc.source, token.FSTRING, tc.want)
		})
	}
}

func TestFStringUnclosedBraceIsReportedByTheLexer(t *testing.T) {
	requireIllegal(t, "f\"{x\"\n", "unclosed brace in f-string")
	requireIllegal(t, "f\"{x\"\n", "hint:")
	requireIllegal(t, `f"{x`, "unclosed brace in f-string")
	requireIllegal(t, `f"{"abc}"`, "unterminated")
}
