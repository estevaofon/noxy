package compiler

import (
	"fmt"
	"strings"

	"noxy-vm/internal/ast"
)

// Narrowing de nulidade (spec §2.4, issue #105 item 1) — modelo Kotlin/Dart.
//
// O compilador guarda um conjunto de CHAVES ESTAVEIS provadas nao-nulas
// (stableKey: `x`, `*r`, `a.b.c`). Um fato nasce de `e != null` (ramo then),
// `e == null` (ramo else, e depois do `if` quando o then termina), `&&`/`||`
// (operando direito ve o fato do esquerdo), `while e != null do` (corpo) e
// `if r.ok then` (r.value, Parte B). Ao ler um identificador/membro/deref
// com fato, o tipo `T?` estreita para `T`.
//
// Fatos morrem quando o valor pode mudar sem que o compilador veja: uma
// atribuicao ao prefixo da chave (dropKey), uma chamada (dropAfterCall —
// raizes compartilhadas: ref, global, upvalue, capturada, ou local cujo
// endereco ja foi tomado), e a entrada num laco cujo corpo atribui a raiz
// (dropForLoop). Chaves compostas cuja raiz e um local VALOR nao
// compartilhado sobrevivem a chamadas: ninguem fora do frame alcanca esse
// valor (semantica de copia, §2.2).

func isNullLiteral(e ast.Expression) bool {
	_, ok := e.(*ast.NullLiteral)
	return ok
}

// conditionFacts devolve as chaves provadas nao-nulas quando cond e
// verdadeira (then) e quando e falsa (els).
func (c *Compiler) conditionFacts(cond ast.Expression) (then, els []string) {
	switch e := cond.(type) {
	case *ast.InfixExpression:
		switch e.Operator {
		case "!=", "==":
			var subject ast.Expression
			if isNullLiteral(e.Right) {
				subject = e.Left
			} else if isNullLiteral(e.Left) {
				subject = e.Right
			}
			if subject == nil {
				return nil, nil
			}
			key, ok := stableKey(subject)
			if !ok {
				return nil, nil
			}
			if e.Operator == "!=" {
				return []string{key}, nil
			}
			return nil, []string{key}
		case "&&":
			lt, _ := c.conditionFacts(e.Left)
			rt, _ := c.conditionFacts(e.Right)
			return append(lt, rt...), nil
		case "||":
			_, le := c.conditionFacts(e.Left)
			_, re := c.conditionFacts(e.Right)
			return nil, append(le, re...)
		}
	case *ast.PrefixExpression:
		if e.Operator == "!" {
			t, f := c.conditionFacts(e.Right)
			return f, t
		}
	case *ast.MemberAccessExpression:
		if key := c.resultValueKey(e); key != "" {
			return []string{key}, nil
		}
	}
	return nil, nil
}

// narrowType aplica o fato da chave ao tipo declarado: `T?` vira `T`.
func (c *Compiler) narrowType(key string, declared ast.NoxyType) ast.NoxyType {
	if declared == nil || c.narrowed == nil {
		return declared
	}
	if _, ok := c.narrowed[key]; !ok {
		return declared
	}
	if elem, ok := nonNull(declared); ok {
		return elem
	}
	return declared
}

// pushFacts adiciona fatos e devolve o restore que repoe o conjunto anterior.
// O registro de fatos perdidos (narrowLost) acompanha: um fato perdido dentro
// do ramo nao vale fora dele, e testar de novo apaga a perda.
func (c *Compiler) pushFacts(keys []string) func() {
	saved := c.narrowed
	next := make(map[string]struct{}, len(saved)+len(keys))
	for k := range saved {
		next[k] = struct{}{}
	}
	savedLost := c.narrowLost
	nextLost := make(map[string]string, len(savedLost))
	for k, why := range savedLost {
		nextLost[k] = why
	}
	for _, k := range keys {
		next[k] = struct{}{}
		delete(nextLost, k)
	}
	c.narrowed = next
	c.narrowLost = nextLost
	return func() {
		c.narrowed = saved
		c.narrowLost = savedLost
	}
}

// keepFacts adiciona fatos sem restore (depois de um ramo que termina).
func (c *Compiler) keepFacts(keys []string) {
	if len(keys) == 0 {
		return
	}
	c.pushFacts(keys)
}

func keyDependsOn(key, root string) bool {
	if key == root {
		return true
	}
	bare := strings.TrimLeft(key, "*")
	return bare == root || strings.HasPrefix(bare, root+".")
}

// dropKey invalida a chave e tudo que depende dela (`x` derruba `x.a`, `*x`).
// Uma atribuicao supera o motivo "perdido por chamada": o registro sai junto.
func (c *Compiler) dropKey(key string) {
	for k := range c.narrowed {
		if keyDependsOn(k, key) {
			delete(c.narrowed, k)
		}
	}
	for k := range c.narrowLost {
		if keyDependsOn(k, key) {
			delete(c.narrowLost, k)
		}
	}
}

// dropCompound invalida toda chave composta (com `.` ou `*`).
func (c *Compiler) dropCompound() {
	for k := range c.narrowed {
		if strings.ContainsAny(k, ".*") {
			delete(c.narrowed, k)
		}
	}
	for k := range c.narrowLost {
		if strings.ContainsAny(k, ".*") {
			delete(c.narrowLost, k)
		}
	}
}

func rootOf(key string) string {
	key = strings.TrimLeft(key, "*")
	if i := strings.IndexByte(key, '.'); i >= 0 {
		return key[:i]
	}
	return key
}

// rootIsShared: a raiz pode mudar por fora deste frame — slot `ref`,
// capturada por closure, endereco tomado com `ref`, upvalue ou global.
func (c *Compiler) rootIsShared(root string) bool {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].Name != root {
			continue
		}
		l := c.locals[i]
		if _, isRef := asRefType(l.Type); isRef {
			return true
		}
		return l.IsCaptured || l.RefTaken
	}
	return true
}

// dropAfterCall invalida o que uma chamada pode ter mudado e registra o
// motivo, para o diagnostico de leitura sem fato (mayBeNullError).
func (c *Compiler) dropAfterCall() {
	for k := range c.narrowed {
		if strings.HasPrefix(k, "*") || c.rootIsShared(rootOf(k)) {
			delete(c.narrowed, k)
			if c.narrowLost == nil {
				c.narrowLost = make(map[string]string)
			}
			c.narrowLost[k] = c.sharedRootReason(rootOf(k))
		}
	}
}

// sharedRootReason descreve por que a raiz e alcancavel durante uma chamada —
// a mesma classificacao de rootIsShared, em palavras.
func (c *Compiler) sharedRootReason(root string) string {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].Name != root {
			continue
		}
		l := c.locals[i]
		if _, isRef := asRefType(l.Type); isRef {
			return fmt.Sprintf("'%s' is a ref", root)
		}
		if l.IsCaptured {
			return fmt.Sprintf("'%s' is captured by a closure", root)
		}
		return fmt.Sprintf("'%s' had its address taken with 'ref'", root)
	}
	if slot, _ := c.resolveUpvalue(root); slot != -1 {
		return fmt.Sprintf("'%s' is an upvalue", root)
	}
	return fmt.Sprintf("'%s' is a global", root)
}

// pureBuiltins sao os builtins centrais (builtin_return_types.go, mais os
// sem tipo de retorno util) que nunca executam codigo Noxy: uma chamada a
// eles nao pode reatribuir raiz nenhuma, entao nao encerra narrowing (#118).
// call_result, spawn_task, task_await e chamadas via `func` bare ficam fora:
// reentram em codigo do programa.
var pureBuiltins = func() map[string]struct{} {
	set := map[string]struct{}{"print": {}, "range": {}, "keys": {}, "slice": {}}
	for name := range coreBuiltinReturnTypes {
		set[name] = struct{}{}
	}
	return set
}()

// isPureBuiltinCall: a chamada resolve para um builtin puro — nome na tabela
// e nao sombreado por local, upvalue ou global declarado pelo programa (a
// mesma regra de sombreamento de builtinReturnType, compileCallExpression).
func (c *Compiler) isPureBuiltinCall(call *ast.CallExpression) bool {
	ident, ok := call.Function.(*ast.Identifier)
	if !ok {
		return false
	}
	if _, pure := pureBuiltins[ident.Value]; !pure {
		return false
	}
	if c.isShadowedByLocal(ident.Value) {
		return false
	}
	_, declared := c.globals[ident.Value]
	return !declared
}

// noteAssignment invalida os fatos afetados por uma atribuicao a target.
func (c *Compiler) noteAssignment(target ast.Expression) {
	switch t := target.(type) {
	case *ast.IndexExpression:
		if key, ok := stableKey(t.Left); ok {
			c.dropKey(key)
			if c.rootIsShared(rootOf(key)) {
				c.dropCompound()
			}
			return
		}
		c.dropCompound()
		return
	}
	key, ok := stableKey(target)
	if !ok {
		c.dropCompound()
		return
	}
	c.dropKey(key)
	if strings.HasPrefix(key, "*") || (strings.Contains(key, ".") && c.rootIsShared(rootOf(key))) {
		// Escrita atraves de referencia ou em caminho compartilhado: pode
		// ter alcancado qualquer outro caminho que apelide o mesmo slot.
		c.dropCompound()
	}
}

// markRefTaken registra que o endereco do local raiz de expr foi tomado.
func (c *Compiler) markRefTaken(expr ast.Expression) {
	key, ok := stableKey(expr)
	if !ok {
		return
	}
	root := rootOf(key)
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].Name == root {
			c.locals[i].RefTaken = true
			return
		}
	}
}

// loopEffects colhe as raizes atribuidas no corpo de um laco (alvo de `=`,
// operando de `ref`, variavel do for) e se ha alguma chamada que encerra
// narrowing (builtin puro nao conta — isPureBuiltinCall).
func (c *Compiler) loopEffects(body *ast.BlockStatement) (roots []string, hasCall bool) {
	if body == nil {
		return nil, false
	}
	seen := map[string]struct{}{}
	add := func(expr ast.Expression) {
		if key, ok := stableKey(expr); ok {
			seen[rootOf(key)] = struct{}{}
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			if idx, ok := n.Target.(*ast.IndexExpression); ok {
				add(idx.Left)
			} else {
				add(n.Target)
			}
		case *ast.PrefixExpression:
			if n.Operator == "ref" {
				add(n.Right)
			}
		case *ast.ForStatement:
			seen[n.Identifier] = struct{}{}
		case *ast.CallExpression:
			if !c.isPureBuiltinCall(n) {
				hasCall = true
			}
		}
		return true
	})
	for root := range seen {
		roots = append(roots, root)
	}
	return roots, hasCall
}

// dropForLoop invalida, ANTES de compilar um laco, os fatos que o corpo
// pode derrubar em qualquer iteracao.
func (c *Compiler) dropForLoop(body *ast.BlockStatement) {
	roots, hasCall := c.loopEffects(body)
	for _, r := range roots {
		c.dropKey(r)
	}
	if hasCall {
		c.dropAfterCall()
	}
}

// blockTerminates: o bloco nunca chega ao fim por cima — return, break,
// continue, exit/sys_exit, ou if/when com todos os ramos terminando.
func blockTerminates(block *ast.BlockStatement) bool {
	if block == nil || len(block.Statements) == 0 {
		return false
	}
	return statementTerminates(block.Statements[len(block.Statements)-1])
}

func statementTerminates(statement ast.Statement) bool {
	switch s := statement.(type) {
	case *ast.ReturnStmt, *ast.BreakStmt, *ast.ContinueStmt:
		return true
	case *ast.BlockStatement:
		return blockTerminates(s)
	case *ast.ExpressionStmt:
		call, ok := s.Expression.(*ast.CallExpression)
		if !ok {
			return false
		}
		ident, ok := call.Function.(*ast.Identifier)
		return ok && (ident.Value == "exit" || ident.Value == "sys_exit")
	case *ast.IfStatement:
		return s.Alternative != nil && blockTerminates(s.Consequence) && blockTerminates(s.Alternative)
	case *ast.WhenStatement:
		if len(s.Cases) == 0 {
			return false
		}
		hasDefault := false
		for _, clause := range s.Cases {
			hasDefault = hasDefault || clause.IsDefault
			if !blockTerminates(clause.Body) {
				return false
			}
		}
		return hasDefault
	}
	return false
}

// narrowBorrowOwner aplica o narrowing ao dono de um emprestimo `ref base.f`
// cujo tipo ja veio atravessado (compileBorrowBase devolve o elemento de um
// `ref`): tenta o fato da chave e, se a base e `ref (T?)`, o fato de `*chave`.
// Sem fato, o erro nomeia `*base` quando a nulidade e do slot apontado.
func (c *Compiler) narrowBorrowOwner(base ast.Expression, owner ast.NoxyType) (ast.NoxyType, error) {
	key, stable := stableKey(base)
	if stable {
		owner = c.narrowType(key, owner)
		if isNullable(owner) {
			owner = c.narrowType("*"+key, owner)
		}
	}
	if !isNullable(owner) {
		return owner, nil
	}
	baseType, err := c.typeOfDiscardedExpression(base)
	if err == nil {
		if _, viaRef := asRefType(baseType); viaRef && !isNullable(baseType) {
			return nil, c.mayBeNullError(&ast.PrefixExpression{Operator: "*", Right: base}, owner)
		}
	}
	return nil, c.mayBeNullError(base, owner)
}

// mayBeNullError e o diagnostico de leitura atraves de um `T?` sem teste.
// Se o fato existiu e uma chamada o derrubou (narrowLost), diz isso: sugerir
// `if x != null` com o `if` a duas linhas de distancia e enganoso (#118).
func (c *Compiler) mayBeNullError(expr ast.Expression, t ast.NoxyType) error {
	if key, ok := stableKey(expr); ok {
		if why, lost := c.narrowLost[key]; lost {
			hint := fmt.Sprintf("test it again after the call, or bind it first ('let v = %s' before the 'if') and use 'v'", key)
			if strings.HasSuffix(why, "is a global") {
				hint = fmt.Sprintf("test it again after the call, bind it first ('let v = %s' before the 'if') and use 'v', or move the code into a function", key)
			}
			return fmt.Errorf("[line %d] '%s' may be null: it was tested, but %s and a call came between the test and this use\n  hint: %s", c.currentLine, key, why, hint)
		}
		return fmt.Errorf("[line %d] '%s' may be null; test it first\n  hint: use 'if %s != null then ... end'", c.currentLine, key, key)
	}
	return fmt.Errorf("[line %d] value of type %s may be null; test it first\n  hint: bind it with 'let' and test for null", c.currentLine, t.String())
}

// resultValueKey: `if r.ok then` estreita `r.value` para T quando r e
// errors::Result<T> (o compilador conhece Result nominalmente — spec §7).
// O tipo da base sai de uma compilacao descartavel (typeOfDiscardedExpression).
func (c *Compiler) resultValueKey(e *ast.MemberAccessExpression) string {
	if e.Member != "ok" {
		return ""
	}
	key, ok := stableKey(e.Left)
	if !ok {
		return ""
	}
	baseType, err := c.typeOfDiscardedExpression(e.Left)
	if err != nil || baseType == nil {
		return ""
	}
	if _, isResult := c.resultTypeArgs(baseType); !isResult {
		return ""
	}
	return key + ".value"
}
