package parser

// Issue #44 (3): literal inteiro fora da faixa de int64 e erro de parse, nao
// saturacao silenciosa (strconv.ParseInt devolve o valor saturado JUNTO com
// ErrRange, e o erro era descartado). O menos unario diretamente sobre um
// literal inteiro funde o sinal no proprio literal: a faixa de int64 e
// assimetrica e o minimo (-9223372036854775808) so e representavel assim —
// sem a fusao, a parte positiva estoura antes de o menos ser aplicado.

import (
	"math"
	"strings"
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
)

func TestIntLiteralOutOfRangeIsError(t *testing.T) {
	for _, source := range []string{
		"let x: int = 9223372036854775808",
		"let x: int = 99999999999999999999999999",
		"let x: int = 0xFFFFFFFFFFFFFFFF",
		"let x: int = -9223372036854775809",
	} {
		p := New(lexer.New(source))
		_ = p.ParseProgram()
		if len(p.Errors()) == 0 || !strings.Contains(strings.Join(p.Errors(), "\n"), "out of int64 range") {
			t.Fatalf("source=%q errors=%v, quer erro de faixa", source, p.Errors())
		}
	}
}

func TestMinInt64LiteralIsExact(t *testing.T) {
	p := New(lexer.New("let x: int = -9223372036854775808"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	let := program.Statements[0].(*ast.LetStmt)
	lit, ok := let.Value.(*ast.IntegerLiteral)
	if !ok {
		t.Fatalf("valor=%#v, quer IntegerLiteral com sinal fundido", let.Value)
	}
	if lit.Value != math.MinInt64 {
		t.Fatalf("valor=%d, quer %d", lit.Value, int64(math.MinInt64))
	}
	if lit.String() != "-9223372036854775808" {
		t.Fatalf("String()=%q deve refletir o literal com sinal", lit.String())
	}
}

func TestNegativeLiteralFoldKeepsMaxIntNegatable(t *testing.T) {
	p := New(lexer.New("let x: int = -9223372036854775807"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	let := program.Statements[0].(*ast.LetStmt)
	lit, ok := let.Value.(*ast.IntegerLiteral)
	if !ok || lit.Value != -math.MaxInt64 {
		t.Fatalf("valor=%#v, quer IntegerLiteral(-9223372036854775807)", let.Value)
	}
}

func TestUnaryMinusOnNonLiteralStaysPrefix(t *testing.T) {
	p := New(lexer.New("let x: int = -y"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	let := program.Statements[0].(*ast.LetStmt)
	if _, ok := let.Value.(*ast.PrefixExpression); !ok {
		t.Fatalf("valor=%#v, menos unario sobre identificador segue PrefixExpression", let.Value)
	}
}

func TestInfixMinusUnaffectedByFold(t *testing.T) {
	p := New(lexer.New("let x: int = 1 - 5"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	let := program.Statements[0].(*ast.LetStmt)
	infix, ok := let.Value.(*ast.InfixExpression)
	if !ok || infix.Operator != "-" {
		t.Fatalf("valor=%#v, quer InfixExpression de subtracao", let.Value)
	}
	right, ok := infix.Right.(*ast.IntegerLiteral)
	if !ok || right.Value != 5 {
		t.Fatalf("lado direito=%#v, quer IntegerLiteral(5)", infix.Right)
	}
}
