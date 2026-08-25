package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// R12 (issue #83, spec 2026-08-25-issue-83-exclusive-access-design §2.3):
//
//	Um parametro `ref T` e um EMPRESTIMO. O callee pode ler, escrever atraves
//	dele e repassa-lo adiante — o que NAO pode e GUARDA-LO: nada de armazenar
//	em campo, elemento, entrada de map ou global, nada de construtor, `return`
//	ou captura por closure. Um parametro que sobrevive a chamada se declara
//	`own ref T`, e um `own` so aceita referencia de CELULA (R10), nunca um
//	emprestimo.
//
// As duas checagens sao LOCAIS e de um nivel so — assinatura contra assinatura,
// corpo contra a propria assinatura. O compilador nunca inspeciona o corpo de um
// callee para decidir se uma chamada e legal; a §2.2 da spec registra por que
// inferir escape com pre-passe de ponto fixo foi descartado (a legalidade de uma
// chamada passaria a depender de um corpo invisivel ao autor).
//
// ETAPA 1 (v0.20.0): as duas emitem AVISO, nao erro — o mesmo rollout de R11
// (spec §9.2). Nada quebra nesta release; o numero do corpus e que autoriza a
// promocao a erro.

// recordOwnedParams publica os flags `own` da assinatura de uma funcao nomeada.
// A checagem do lado do CHAMADOR precisa deles, e `ast.FunctionType` — o que
// vive em c.globals — nao os carrega de proposito (§2.3: `own` nao e tipo, logo
// nao existe em tipo de funcao, e funcao de ordem superior nunca guarda).
func (c *Compiler) recordOwnedParams(name string, params []*ast.Parameter) {
	if c.ownedParams == nil {
		c.ownedParams = make(map[string][]bool)
	}
	flags := make([]bool, len(params))
	owns := false
	for i, p := range params {
		if p == nil {
			continue
		}
		flags[i] = p.Owned
		owns = owns || p.Owned
	}
	if !owns {
		// Assinatura sem nenhum `own`: nada a checar no call site. Nao guardar
		// mantem o registro proporcional ao uso do recurso, e o lookup ausente
		// e indistinguivel de "todos os parametros sao emprestimo".
		delete(c.ownedParams, name)
		return
	}
	c.ownedParams[name] = flags
}

// isOwnedParam diz se o parametro i de `name` foi declarado `own`.
func (c *Compiler) isOwnedParam(name string, i int) bool {
	flags, ok := c.ownedParams[name]
	if !ok || i < 0 || i >= len(flags) {
		return false
	}
	return flags[i]
}

// checkOwnArgument aplica R12 do lado do CHAMADOR: um parametro `own ref T` so
// aceita referencia de celula. `ref a[i]` / `ref p.x` e emprestimo (R11) e nao
// pode ser guardado por ninguem.
//
// Uma consulta a assinatura, sem olhar corpo nenhum.
func (c *Compiler) checkOwnArgument(callee string, index int, arg ast.Expression) {
	if callee == "" || !c.isOwnedParam(callee, index) {
		return
	}
	prefix, ok := arg.(*ast.PrefixExpression)
	if !ok || prefix.Operator != "ref" || !isContainerBorrow(prefix.Right) {
		return
	}
	display := exprDisplay(prefix.Right)
	c.warn(fmt.Sprintf(
		"argument %d to '%s' is declared 'own ref': it is kept beyond the call, so it cannot be a borrow — 'ref %s' points into a container"+
			"\n  hint: bind '%s' to a variable first and pass 'ref <variable>'",
		index+1, callee, display, display))
}

// borrowedParamNames devolve os parametros `ref T` SEM `own` — os emprestimos
// que o corpo nao pode guardar.
func borrowedParamNames(params []*ast.Parameter) map[string]bool {
	var names map[string]bool
	for _, p := range params {
		if p == nil || p.Owned {
			continue
		}
		if _, isRef := p.Type.(*ast.RefType); !isRef {
			continue
		}
		if names == nil {
			names = make(map[string]bool, len(params))
		}
		names[p.Name] = true
	}
	return names
}

// checkBorrowParamKept aplica R12 do lado do CALLEE: varre o corpo procurando um
// parametro emprestimo em POSICAO DE GUARDA. Varredura de um corpo, sem
// propagacao — o diagnostico sai na linha ofensora.
func (c *Compiler) checkBorrowParamKept(params []*ast.Parameter, body *ast.BlockStatement) {
	borrowed := borrowedParamNames(params)
	if len(borrowed) == 0 || body == nil {
		return
	}
	line := c.currentLine
	defer c.setLine(line)
	c.scanBlockForKeptBorrow(body, borrowed)
}

func (c *Compiler) scanBlockForKeptBorrow(block *ast.BlockStatement, borrowed map[string]bool) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		c.scanStmtForKeptBorrow(stmt, borrowed)
	}
}

func (c *Compiler) scanStmtForKeptBorrow(stmt ast.Statement, borrowed map[string]bool) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		c.setLine(s.Token.Line)
		if c.isStoringTarget(s.Target) {
			c.reportKeptBorrow(s.Value, borrowed, fmt.Sprintf("stored in '%s'", exprDisplay(s.Target)))
		}
		// Uma closure na RHS captura independentemente do alvo.
		c.scanExprForCapture(s.Value, borrowed)
	case *ast.LetStmt:
		c.setLine(s.Token.Line)
		// `let x: ref int = r` liga a um LOCAL, que morre com a chamada — nao
		// e guarda. Mas uma closure atribuida a um local sobrevive por outro
		// caminho (retorno, store), entao a captura ainda conta.
		c.scanExprForCapture(s.Value, borrowed)
	case *ast.ReturnStmt:
		c.setLine(s.Token.Line)
		c.reportKeptBorrow(s.ReturnValue, borrowed, "returned")
		c.scanExprForCapture(s.ReturnValue, borrowed)
	case *ast.ExpressionStmt:
		c.setLine(s.Token.Line)
		c.scanExprForCapture(s.Expression, borrowed)
		if call, ok := s.Expression.(*ast.CallExpression); ok {
			c.reportKeptBorrowInCall(call, borrowed)
		}
	case *ast.IfStatement:
		c.scanBlockForKeptBorrow(s.Consequence, borrowed)
		c.scanBlockForKeptBorrow(s.Alternative, borrowed)
	case *ast.WhileStatement:
		c.scanBlockForKeptBorrow(s.Body, borrowed)
	case *ast.ForStatement:
		c.scanBlockForKeptBorrow(s.Body, borrowed)
	case *ast.WhenStatement:
		for _, clause := range s.Cases {
			if clause != nil {
				c.scanBlockForKeptBorrow(clause.Body, borrowed)
			}
		}
	case *ast.BlockStatement:
		c.scanBlockForKeptBorrow(s, borrowed)
	case *ast.DeferStmt:
		c.setLine(s.Token.Line)
		if s.Call != nil {
			c.reportKeptBorrowInCall(s.Call, borrowed)
		}
	}
}

// isStoringTarget diz se o alvo de uma atribuicao SOBREVIVE a chamada: campo,
// elemento/entrada de map, ou global. Um local nao — ele morre junto com o
// quadro, e ligar um emprestimo a um local nao o faz escapar.
func (c *Compiler) isStoringTarget(target ast.Expression) bool {
	switch t := target.(type) {
	case *ast.MemberAccessExpression, *ast.IndexExpression:
		return true
	case *ast.Identifier:
		_, isGlobal := c.globals[t.Value]
		return isGlobal
	}
	return false
}

// reportKeptBorrow avisa se `expr` GUARDA um emprestimo. A distincao e
// estrutural, nao textual: `g = *r` copia o valor (legal), `g = f(r)` repassa
// (legal, e o callee responde pelo proprio corpo), `g = r` guarda.
func (c *Compiler) reportKeptBorrow(expr ast.Expression, borrowed map[string]bool, position string) {
	name, found := keptBorrowName(c, expr, borrowed)
	if !found {
		return
	}
	c.warn(fmt.Sprintf(
		"parameter '%s' is a borrow and cannot be kept: it is %s"+
			"\n  hint: declare it 'own ref' if it must outlive the call, or store a copy with '*%s'",
		name, position, name))
}

// reportKeptBorrowInCall cobre as chamadas que ARMAZENAM o argumento em vez de
// so usa-lo: construtor de struct, `append`, `chan_send`. Repassar um emprestimo
// para uma funcao comum e legal — o callee responde pelo proprio corpo.
func (c *Compiler) reportKeptBorrowInCall(call *ast.CallExpression, borrowed map[string]bool) {
	if !c.callKeepsArguments(call) {
		return
	}
	for _, arg := range call.Arguments {
		if name, found := directBorrowName(arg, borrowed); found {
			c.warn(fmt.Sprintf(
				"parameter '%s' is a borrow and cannot be kept: it is stored by '%s'"+
					"\n  hint: declare it 'own ref' if it must outlive the call, or store a copy with '*%s'",
				name, callableName(call.Function), name))
		}
	}
}

// callKeepsArguments: as chamadas cujo argumento sobrevive a chamada.
func (c *Compiler) callKeepsArguments(call *ast.CallExpression) bool {
	ident, ok := call.Function.(*ast.Identifier)
	if !ok {
		return false
	}
	switch ident.Value {
	case "append", "chan_send":
		return true
	}
	_, isConstructor := c.structs[ident.Value]
	return isConstructor
}

// keptBorrowName percorre uma expressao procurando um emprestimo GUARDADO.
// Estrutural: desvia de `*r` (copia o valor) e de argumento de chamada comum
// (repasse), entra em literal de array/map (o elemento sobrevive ao literal) e
// em construtor / append / chan_send.
func keptBorrowName(c *Compiler, expr ast.Expression, borrowed map[string]bool) (string, bool) {
	switch e := expr.(type) {
	case nil:
		return "", false
	case *ast.Identifier:
		if borrowed[e.Value] {
			return e.Value, true
		}
	case *ast.PrefixExpression:
		if e.Operator == "*" {
			// Deref: o que sai e o VALOR apontado, nao o emprestimo.
			return "", false
		}
		return keptBorrowName(c, e.Right, borrowed)
	case *ast.ArrayLiteral:
		for _, el := range e.Elements {
			if name, found := keptBorrowName(c, el, borrowed); found {
				return name, true
			}
		}
	case *ast.MapLiteral:
		// Values, nao Pairs: um map nao tem ordem, e o diagnostico precisa ser
		// deterministico entre execucoes.
		for _, v := range e.Values {
			if name, found := keptBorrowName(c, v, borrowed); found {
				return name, true
			}
		}
	case *ast.CallExpression:
		// Repassar e legal; so construtor / append / chan_send guardam.
		if !c.callKeepsArguments(e) {
			return "", false
		}
		for _, arg := range e.Arguments {
			if name, found := directBorrowName(arg, borrowed); found {
				return name, true
			}
		}
	case *ast.InfixExpression:
		if name, found := keptBorrowName(c, e.Left, borrowed); found {
			return name, true
		}
		return keptBorrowName(c, e.Right, borrowed)
	}
	return "", false
}

// directBorrowName reconhece o emprestimo passado NU — `f(r)`, nao `f(*r)`.
func directBorrowName(expr ast.Expression, borrowed map[string]bool) (string, bool) {
	ident, ok := expr.(*ast.Identifier)
	if !ok || !borrowed[ident.Value] {
		return "", false
	}
	return ident.Value, true
}

// scanExprForCapture cobre a ultima posicao de guarda da R12: a captura por
// literal de closure. Uma closure sobrevive ao quadro que a criou, entao o
// emprestimo capturado sobrevive junto — e a captura nao tem sintaxe propria
// onde ancorar a checagem, so a mencao do nome dentro do corpo.
func (c *Compiler) scanExprForCapture(expr ast.Expression, borrowed map[string]bool) {
	switch e := expr.(type) {
	case nil:
		return
	case *ast.FunctionLiteral:
		// Um parametro da closure com o mesmo nome SOMBREIA o emprestimo: a
		// mencao la dentro nao e captura.
		inner := borrowed
		for _, p := range e.Parameters {
			if p != nil && inner[p.Name] {
				inner = withoutName(inner, p.Name)
			}
		}
		if name, found := mentionedName(e.Body, inner); found {
			c.setLine(e.Token.Line)
			c.warn(fmt.Sprintf(
				"parameter '%s' is a borrow and cannot be kept: it is captured by a closure"+
					"\n  hint: declare it 'own ref' if it must outlive the call, or capture a copy with '*%s'",
				name, name))
		}
	case *ast.CallExpression:
		for _, arg := range e.Arguments {
			c.scanExprForCapture(arg, borrowed)
		}
	case *ast.InfixExpression:
		c.scanExprForCapture(e.Left, borrowed)
		c.scanExprForCapture(e.Right, borrowed)
	case *ast.PrefixExpression:
		c.scanExprForCapture(e.Right, borrowed)
	case *ast.ArrayLiteral:
		for _, el := range e.Elements {
			c.scanExprForCapture(el, borrowed)
		}
	case *ast.MapLiteral:
		for _, v := range e.Values {
			c.scanExprForCapture(v, borrowed)
		}
	}
}

func withoutName(names map[string]bool, drop string) map[string]bool {
	out := make(map[string]bool, len(names))
	for name := range names {
		if name != drop {
			out[name] = true
		}
	}
	return out
}

// mentionedName procura QUALQUER mencao de um dos nomes dentro de um bloco. Ao
// contrario das outras checagens, aqui a mencao basta: `*r` dentro de uma
// closure ainda captura `r`, porque o deref acontece quando a closure roda —
// possivelmente depois de a chamada ter terminado.
func mentionedName(block *ast.BlockStatement, names map[string]bool) (string, bool) {
	if block == nil || len(names) == 0 {
		return "", false
	}
	for _, stmt := range block.Statements {
		if name, found := mentionedInStmt(stmt, names); found {
			return name, true
		}
	}
	return "", false
}

func mentionedInStmt(stmt ast.Statement, names map[string]bool) (string, bool) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if name, found := mentionedInExpr(s.Target, names); found {
			return name, true
		}
		return mentionedInExpr(s.Value, names)
	case *ast.LetStmt:
		return mentionedInExpr(s.Value, names)
	case *ast.ReturnStmt:
		return mentionedInExpr(s.ReturnValue, names)
	case *ast.ExpressionStmt:
		return mentionedInExpr(s.Expression, names)
	case *ast.DeferStmt:
		if s.Call == nil {
			return "", false
		}
		return mentionedInExpr(s.Call, names)
	case *ast.IfStatement:
		if name, found := mentionedInExpr(s.Condition, names); found {
			return name, true
		}
		if name, found := mentionedName(s.Consequence, names); found {
			return name, true
		}
		return mentionedName(s.Alternative, names)
	case *ast.WhileStatement:
		if name, found := mentionedInExpr(s.Condition, names); found {
			return name, true
		}
		return mentionedName(s.Body, names)
	case *ast.ForStatement:
		if name, found := mentionedInExpr(s.Collection, names); found {
			return name, true
		}
		return mentionedName(s.Body, names)
	case *ast.WhenStatement:
		for _, clause := range s.Cases {
			if clause == nil {
				continue
			}
			if clause.Condition != nil {
				if name, found := mentionedInStmt(clause.Condition, names); found {
					return name, true
				}
			}
			if name, found := mentionedName(clause.Body, names); found {
				return name, true
			}
		}
	case *ast.BlockStatement:
		return mentionedName(s, names)
	}
	return "", false
}

func mentionedInExpr(expr ast.Expression, names map[string]bool) (string, bool) {
	switch e := expr.(type) {
	case nil:
		return "", false
	case *ast.Identifier:
		if names[e.Value] {
			return e.Value, true
		}
	case *ast.PrefixExpression:
		return mentionedInExpr(e.Right, names)
	case *ast.InfixExpression:
		if name, found := mentionedInExpr(e.Left, names); found {
			return name, true
		}
		return mentionedInExpr(e.Right, names)
	case *ast.MemberAccessExpression:
		return mentionedInExpr(e.Left, names)
	case *ast.IndexExpression:
		if name, found := mentionedInExpr(e.Left, names); found {
			return name, true
		}
		return mentionedInExpr(e.Index, names)
	case *ast.CallExpression:
		if name, found := mentionedInExpr(e.Function, names); found {
			return name, true
		}
		for _, arg := range e.Arguments {
			if name, found := mentionedInExpr(arg, names); found {
				return name, true
			}
		}
	case *ast.ArrayLiteral:
		for _, el := range e.Elements {
			if name, found := mentionedInExpr(el, names); found {
				return name, true
			}
		}
	case *ast.MapLiteral:
		for _, v := range e.Values {
			if name, found := mentionedInExpr(v, names); found {
				return name, true
			}
		}
	case *ast.FunctionLiteral:
		// Closure aninhada: um parametro homonimo sombreia.
		inner := names
		for _, p := range e.Parameters {
			if p != nil && inner[p.Name] {
				inner = withoutName(inner, p.Name)
			}
		}
		return mentionedName(e.Body, inner)
	}
	return "", false
}
