package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// Emprestimo escopado (issue #83, spec 2026-08-25-issue-83-borrow-scope):
// `ref` sobre campo / indice / entrada de map NAO denota uma celula — o
// referente mora dentro de um composto que o copy-on-write pode duplicar. A
// referencia so e solida enquanto o compilador garante que o composto continua
// unico, e isso vale exatamente pela duracao da chamada em que ela foi escrita.
//
// R10: `ref <nome>` promove o slot a celula; o CoW troca o CONTEUDO de um slot,
// nunca o slot, entao a celula sobrevive a qualquer copia. Primeira classe,
// inalterado.
//
// R11: `ref <expr>.campo` / `ref <expr>[i]` e um EMPRESTIMO. Vale so em posicao
// de argumento. Ligar a um nome, retornar, guardar, mandar por canal ou capturar
// em closure faz a referencia sobreviver a unicizacao feita na criacao dela
// (compileLValueBase -> familia _MUT), e a partir dai uma copia posterior do
// conteiner enxerga escritas pelo emprestimo — o vazamento do #83.
//
// A estrategia (unicizar a base na criacao) e a do JOT 21(2) 2022 §6.3
// (Racordon et al., "Implementation Strategies for Mutable Value Semantics") e
// ja estava implementada; o que faltava e a precondicao do §5.3 do mesmo paper:
// "the language disallows the pointer to escape in any way".
//
// ETAPA 1 (v0.20.0): esta checagem emite AVISO, nao erro. O numero real de
// impacto no corpus vem do aviso, e e ele que autoriza a promocao a erro na
// v0.21.0 (spec §9.2). Nada quebra nesta release.

// isContainerBorrow diz se `ref expr` produz um emprestimo — as duas formas que
// emitem OP_REF_PROPERTY / OP_REF_INDEX em compileReferenceArgumentValue. As
// demais (identificador local, upvalue, global) sao referencia de celula (R10) e
// nao passam por aqui.
func isContainerBorrow(expr ast.Expression) bool {
	switch expr.(type) {
	case *ast.MemberAccessExpression, *ast.IndexExpression:
		return true
	}
	return false
}

// borrowRoot devolve o nome no fundo da cadeia de l-value de um emprestimo
// (`ref a[i]` -> "a"; `ref p.x.y` -> "p"), que e a raiz cuja unicidade R12
// protege. O segundo retorno e false quando a raiz nao e um nome — base `any`,
// retorno de chamada, literal — caso em que a analise nao consegue enxergar o
// que precisa proteger.
func borrowRoot(expr ast.Expression) (string, bool) {
	for {
		switch e := expr.(type) {
		case *ast.Identifier:
			return e.Value, true
		case *ast.MemberAccessExpression:
			expr = e.Left
		case *ast.IndexExpression:
			expr = e.Left
		default:
			return "", false
		}
	}
}

// borrowEscapeHint sugere a saida para um emprestimo fora de posicao de
// argumento: usar o l-value direto, ou passar como argumento.
func borrowEscapeHint(expr ast.Expression) string {
	display := exprDisplay(expr)
	return fmt.Sprintf("\n  hint: use '%s' directly, or pass 'ref %s' as a call argument", display, display)
}

// checkBorrowEscape aplica R11 fora da posicao de argumento. Chamado do case
// *ast.PrefixExpression de Compile — o caminho por onde `ref x` passa em TODA
// posicao que nao seja argumento de parametro `ref T` (aquela vai por
// compileRefArgument -> compileReferenceArgument, sem tocar aqui).
//
// Etapa 1: aviso. A promocao a erro troca o corpo por um `return fmt.Errorf`
// com as mensagens por posicao da spec §4; a decisao de qual posicao e a que o
// aviso ainda nao distingue (let, return, store, canal, captura) exige levar a
// posicao sintatica ate aqui, e vem junto da promocao.
func (c *Compiler) checkBorrowEscape(expr ast.Expression) {
	if !isContainerBorrow(expr) {
		return
	}
	display := exprDisplay(expr)
	if c.borrowArgDepth > 0 {
		// Posicao de argumento, mas o callee nao tem parametro `ref T` — native
		// sem assinatura, valor `func` bare, `addr`, `print`, `chan_send`. O
		// emprestimo pode ou nao escapar la dentro; o compilador nao consegue
		// olhar. R12 check 3: recusa conservadora, mensagem propria.
		c.warn(fmt.Sprintf(
			"cannot check 'ref %s': the callee has no ref-parameter contract to inspect"+
				"\n  hint: pass a reference to a variable, or pass a copy",
			display))
		return
	}
	c.warn(fmt.Sprintf(
		"reference into a container escapes the call: 'ref %s' is only valid as a call argument%s",
		display, borrowEscapeHint(expr)))
}

// compileCallArgument compila uma expressao em POSICAO DE ARGUMENTO, marcando
// isso para checkBorrowEscape. Os builtins com tratamento dedicado em
// compileCallExpression (chan_send, chan_recv, addr) compilam seus argumentos
// fora do laco generico; sem este funil, um emprestimo ali seria diagnosticado
// como escape (R11) em vez de recusa conservadora (R12 check 3), que e o que
// realmente e — `addr(ref p.x)` nao escapa, o compilador e que nao tem contrato
// para inspecionar.
func (c *Compiler) compileCallArgument(arg ast.Expression) (interface{}, ast.NoxyType, error) {
	c.borrowArgDepth++
	defer func() { c.borrowArgDepth-- }()
	chunk, t, err := c.Compile(arg)
	return chunk, t, err
}
