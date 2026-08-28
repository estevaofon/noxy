package lexer

import (
	"testing"

	"noxy-vm/internal/token"
)

func TestQuestionMarkIsAToken(t *testing.T) {
	l := New("Node? ref int?")
	want := []struct {
		typ     token.TokenType
		literal string
	}{
		{token.IDENTIFIER, "Node"},
		{token.QUESTION, "?"},
		{token.REF, "ref"},
		{token.TYPE_INT, "int"},
		{token.QUESTION, "?"},
		{token.EOF, ""},
	}
	for i, w := range want {
		tok := l.NextToken()
		if tok.Type != w.typ || tok.Literal != w.literal {
			t.Fatalf("token %d: got %s %q, want %s %q", i, tok.Type, tok.Literal, w.typ, w.literal)
		}
	}
}
