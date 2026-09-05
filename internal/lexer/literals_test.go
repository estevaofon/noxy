package lexer

import (
	"testing"

	"github.com/estevaofon/noxy/internal/token"
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
	requireLexerError(t, `"abc\`, "unterminated")
	requireLexerError(t, `'abc`, "unterminated")
	requireLexerError(t, `'abc\`, "unterminated")
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
	requireLexerError(t, "f\"{x\"\n", "unclosed brace in f-string")
	requireLexerError(t, "f\"{x\"\n", "hint:")
	requireLexerError(t, `f"{x`, "unclosed brace in f-string")
	// The nested literal "abc}" closes fine (its own quote matches at the
	// end); what never closes is the `{` opened before it, so EOF arrives
	// with depth 1 and the diagnosis is "unclosed brace", same as CPython
	// 3.12 ("f-string: expecting '}'") for the equivalent input.
	requireLexerError(t, `f"{"abc}"`, "unclosed brace in f-string")
	// Here the nested literal itself never closes either (no matching `"`
	// before EOF) — still reported as "unclosed brace in f-string": the `{`
	// is also open, and that's the actionable fix regardless of which quote
	// is missing.
	requireLexerError(t, `f"{"abc`, "unclosed brace in f-string")
}

// The parser duplicates this text nowhere anymore (it references
// lexer.UnclosedBraceReason directly, issue #126); pin the exact wording
// here so a future edit can't silently drift the hint.
func TestUnclosedBraceReasonIsThePinnedSpecText(t *testing.T) {
	want := "unclosed brace in f-string\n  hint: every '{' that starts an expression needs a matching '}'; write '{{' for a literal brace"
	if UnclosedBraceReason != want {
		t.Fatalf("UnclosedBraceReason = %q, want %q", UnclosedBraceReason, want)
	}
}

// Issue #134: a razao escrita pelo lexer ("unterminated string",
// UnclosedBraceReason) e o caractere desconhecido copiado verbatim sao tokens
// de TIPOS distintos — LEXER_ERROR e ILLEGAL. Antes a distincao era
// lexer.IsReason, uma contagem de runas do literal (issue #126), que nada
// impunha: uma razao de uma palavra ou um ILLEGAL com mais de uma runa
// mudava o diagnostico do parser sem quebrar teste algum.
func TestLexerErrorAndIllegalAreDistinctTypes(t *testing.T) {
	if got := firstToken(t, `"abc`); got.Type != token.LEXER_ERROR || got.Literal != "unterminated string" {
		t.Fatalf("token = %#v, want LEXER_ERROR \"unterminated string\"", got)
	}
	if got := firstToken(t, `f"{x`); got.Type != token.LEXER_ERROR || got.Literal != UnclosedBraceReason {
		t.Fatalf("token = %#v, want LEXER_ERROR with UnclosedBraceReason", got)
	}
	if got := firstToken(t, "@"); got.Type != token.ILLEGAL || got.Literal != "@" {
		t.Fatalf("token = %#v, want ILLEGAL \"@\"", got)
	}
}
