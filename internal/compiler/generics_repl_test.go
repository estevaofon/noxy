package compiler

import (
	"testing"

	"noxy-vm/internal/ast"
)

// Task 14 (spec §5): o REPL cria um *Compiler NOVO a cada linha (correto —
// cada linha e sua propria compilacao de Program, e o instanceQueue e por
// Program), mas precisa persistir TRES estados entre linhas para o codigo
// nao "esquecer" o que uma linha anterior declarou: globals (ja persistia),
// c.structs (structs comuns E instancias de struct generico — bug de carona
// pre-existente: sem isto, o REGISTRO da instancia feito por
// registerStructInstance em ensureStructInstance some junto com o mapa
// descartavel da linha) e o GenericRegistry (templates genericos, via
// SetGenericState — sem isto, o template declarado numa linha nunca chega
// a linha seguinte, que ve apenas um registry novo e vazio).
//
// Este teste exercita o CONTRATO do compilador que cmd/noxy/main.go usa —
// nao o main.go em si (cmd/noxy nao tem testes hoje e a mudanca la e um
// espelho direto: hoisting dos tres mapas/registry para fora do loop, com
// SetGenericState chamado por linha).
//
// Sequencia: linha 1 declara o template `Caixa<T>`; linha 2 instancia
// `Caixa<int>` num `let`; linha 3 acessa o campo `valor` da instancia. Cada
// "linha" e um Compiler NOVO (mimetizando main.go), mas globals/structs/reg
// sao os MESMOS mapas/ponteiro atravessando as tres iteracoes.
func TestREPLPersistsGenericsAcrossLines(t *testing.T) {
	globals := make(map[string]ast.NoxyType)
	structs := make(map[string]*ast.StructStatement)
	reg := NewGenericRegistry()
	lines := []string{
		"struct Caixa<T>\n    valor: T\nend",
		"let c: Caixa<int> = Caixa(7)",
		"c.valor",
	}
	for i, line := range lines {
		c := NewWithState(globals, structs, "REPL")
		c.SetGenericState(reg)
		if _, _, err := c.Compile(parse(line)); err != nil {
			t.Fatalf("linha %d (%q): %v", i+1, line, err)
		}
		globals = c.GetGlobals()
	}
}
