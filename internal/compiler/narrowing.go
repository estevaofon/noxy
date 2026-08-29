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
			lt = c.dropFactsCrossingCall(lt, e.Right, "&&", "!=")
			return append(lt, rt...), nil
		case "||":
			_, le := c.conditionFacts(e.Left)
			_, re := c.conditionFacts(e.Right)
			le = c.dropFactsCrossingCall(le, e.Right, "||", "==")
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
	return c.pushFactsWith(keys, c.takePendingLost())
}

// takePendingLost entrega (e zera) as perdas registradas por conditionFacts
// desde a ultima consulta. O `if` as toma uma vez e as da aos DOIS ramos.
func (c *Compiler) takePendingLost() map[string]lostFact {
	pending := c.narrowLostPending
	c.narrowLostPending = nil
	return pending
}

// pushFactsWith e pushFacts com as perdas pendentes explicitas.
func (c *Compiler) pushFactsWith(keys []string, pending map[string]lostFact) func() {
	saved := c.narrowed
	next := make(map[string]struct{}, len(saved)+len(keys))
	for k := range saved {
		next[k] = struct{}{}
	}
	savedLost := c.narrowLost
	nextLost := make(map[string]lostFact, len(savedLost)+len(pending))
	for k, why := range savedLost {
		nextLost[k] = why
	}
	for k, lost := range pending {
		nextLost[k] = lost
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
	c.keepFactsWith(keys, c.takePendingLost())
}

func (c *Compiler) keepFactsWith(keys []string, pending map[string]lostFact) {
	if len(keys) == 0 && len(pending) == 0 {
		return
	}
	c.pushFactsWith(keys, pending)
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

// lostFact: por que um fato foi derrubado por chamada — a raiz (why), o que
// aconteceu (event) e a primeira saida do hint (again) — so para o
// diagnostico de mayBeNullError.
type lostFact struct {
	why   string
	event string
	again string
}

// firstImpureCall devolve a primeira chamada de expr que encerra narrowing
// (nao e builtin puro), ou nil.
func (c *Compiler) firstImpureCall(expr ast.Expression) *ast.CallExpression {
	var found *ast.CallExpression
	ast.Inspect(expr, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		if call, ok := node.(*ast.CallExpression); ok && !c.isPureBuiltinCall(call) {
			found = call
			return false
		}
		return true
	})
	return found
}

// dropFactsCrossingCall (#120 item 3): em `a && b` / `a || b`, uma chamada
// nao-pura em b roda DEPOIS do teste de a — os fatos de a sobre raiz
// compartilhada nao podem sair para o ramo (dentro da expressao o
// dropAfterCall ja os derruba; o furo era so nos fatos exportados). Registra
// a perda para o diagnostico apontar a chamada na condicao.
func (c *Compiler) dropFactsCrossingCall(keys []string, right ast.Expression, op, cmp string) []string {
	if len(keys) == 0 {
		return keys
	}
	call := c.firstImpureCall(right)
	if call == nil {
		return keys
	}
	kept := keys[:0:0]
	for _, k := range keys {
		shared, why := c.rootSharing(rootOf(k))
		if !strings.HasPrefix(k, "*") && !shared {
			kept = append(kept, k)
			continue
		}
		if why == "" {
			why = fmt.Sprintf("'%s' is read through a reference", k)
		}
		// Pendente: conditionFacts roda ANTES do pushFacts do ramo; o registro
		// entra no ramo (e so nele) quando pushFacts o consome.
		if c.narrowLostPending == nil {
			c.narrowLostPending = make(map[string]lostFact)
		}
		c.narrowLostPending[k] = lostFact{
			why:   why,
			event: "a call in the condition ran after the test",
			again: fmt.Sprintf("put the call before the test ('%s(...) %s %s %s null')", calleeLabel(call), op, k, cmp),
		}
	}
	return kept
}

// calleeLabel e o nome do callee para um hint — nunca call.String(), que e
// um renderer de debug (perde aspas e parenteses).
func calleeLabel(call *ast.CallExpression) string {
	switch fn := call.Function.(type) {
	case *ast.Identifier:
		return fn.Value
	case *ast.MemberAccessExpression:
		if base, ok := fn.Left.(*ast.Identifier); ok {
			return base.Value + "." + fn.Member
		}
	}
	return "f"
}

// rootSharing: a raiz pode mudar por fora deste frame — slot `ref`,
// capturada por closure, endereco tomado com `ref`, upvalue ou global — e o
// motivo em palavras, para o diagnostico do fato perdido.
func (c *Compiler) rootSharing(root string) (shared bool, why string) {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].Name != root {
			continue
		}
		l := c.locals[i]
		if _, isRef := asRefType(l.Type); isRef {
			return true, fmt.Sprintf("'%s' is a ref", root)
		}
		if l.IsCaptured {
			return true, fmt.Sprintf("'%s' is captured by a closure", root)
		}
		if l.RefTaken {
			return true, fmt.Sprintf("'%s' had its address taken with 'ref'", root)
		}
		return false, ""
	}
	if c.enclosingHasLocal(root) {
		return true, fmt.Sprintf("'%s' is an upvalue", root)
	}
	return true, fmt.Sprintf("'%s' is a global", root)
}

// enclosingHasLocal: o nome resolve para um local de alguma funcao envolvente
// — SEM capturar. resolveUpvalue marca IsCaptured e cria o upvalue; uma
// classificacao para diagnostico nao pode mudar o programa (a revisao do #119
// achou uma chave morta `p` capturando o `p` homonimo do pai).
func (c *Compiler) enclosingHasLocal(name string) bool {
	for e := c.enclosing; e != nil; e = e.enclosing {
		if slot, _ := e.resolveLocal(name); slot != -1 {
			return true
		}
	}
	return false
}

func (c *Compiler) rootIsShared(root string) bool {
	shared, _ := c.rootSharing(root)
	return shared
}

// dropAfterCall invalida o que uma chamada pode ter mudado e registra o
// motivo, para o diagnostico de leitura sem fato (mayBeNullError).
func (c *Compiler) dropAfterCall() {
	c.dropSharedAfterCall("a call came between the test and this use", "test it again after the call")
}

// dropSharedAfterCall e o corpo de dropAfterCall; event/again descrevem a
// queda no diagnostico — a entrada de um laco (dropForLoop) tem a sua: a
// chamada pode vir DEPOIS do uso no texto e rodar antes dele na iteracao
// seguinte.
func (c *Compiler) dropSharedAfterCall(event, again string) {
	for k := range c.narrowed {
		shared, why := c.rootSharing(rootOf(k))
		if !strings.HasPrefix(k, "*") && !shared {
			continue
		}
		delete(c.narrowed, k)
		if why == "" {
			why = fmt.Sprintf("'%s' is read through a reference", k)
		}
		if c.narrowLost == nil {
			c.narrowLost = make(map[string]lostFact)
		}
		c.narrowLost[k] = lostFact{why: why, event: event, again: again}
	}
}

// pureBuiltins sao os builtins centrais (builtin_return_types.go, mais os
// sem tipo de retorno util — print/eprint/iprint/eiprint, append/pop/delete,
// range/keys/slice) que nunca executam codigo Noxy: uma chamada a eles nao
// pode reatribuir raiz nenhuma, entao nao encerra narrowing (#118).
// call_result, spawn_task, task_await e chamadas via `func` bare ficam fora:
// reentram em codigo do programa. json_loads tambem: escreve atraves do ref
// e pode por null na raiz.
var pureBuiltins = func() map[string]struct{} {
	set := map[string]struct{}{
		"print": {}, "eprint": {}, "iprint": {}, "eiprint": {},
		"append": {}, "pop": {}, "delete": {},
		"range": {}, "keys": {}, "slice": {},
	}
	for name := range coreBuiltinReturnTypes {
		set[name] = struct{}{}
	}
	return set
}()

// isPureBuiltinCall: a chamada resolve para um builtin puro — nome na tabela
// e nao sombreado por local, upvalue ou global declarado pelo programa (a
// mesma regra de sombreamento de builtinReturnType, compileCallExpression) —
// ou para um construtor de struct (declarado ou template generico), que
// tambem nao roda codigo do programa.
func (c *Compiler) isPureBuiltinCall(call *ast.CallExpression) bool {
	ident, ok := call.Function.(*ast.Identifier)
	if !ok {
		return false
	}
	if c.isShadowedByLocal(ident.Value) {
		return false
	}
	if _, pure := pureBuiltins[ident.Value]; pure {
		_, declared := c.globals[ident.Value]
		return !declared
	}
	if c.structDeclaration(ident.Value) != nil {
		return true
	}
	_, isStructTemplate := c.registryOrInit().Structs[ident.Value]
	return isStructTemplate
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
	// A pureza e decidida ANTES de compilar o corpo: um `let print = zera`
	// (ou `func`) declarado dentro do laco sombreia o builtin na iteracao — o
	// nome declarado no corpo torna a chamada homonima impura (revisao #119).
	declared := map[string]struct{}{}
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.LetStmt:
			if n.Name != nil {
				declared[n.Name.Value] = struct{}{}
			}
		case *ast.FunctionStatement:
			declared[n.Name] = struct{}{}
		}
		return true
	})
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
			shadowedInBody := false
			if ident, ok := n.Function.(*ast.Identifier); ok {
				_, shadowedInBody = declared[ident.Value]
			}
			if shadowedInBody || !c.isPureBuiltinCall(n) {
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
		c.dropSharedAfterCall("the loop body calls a function that can run before this use", "test it again inside the loop")
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
		if lost, ok := c.narrowLost[key]; ok {
			hint := fmt.Sprintf("%s, or bind it first ('let v = %s' before the 'if') and use 'v'", lost.again, key)
			if strings.HasSuffix(lost.why, "is a global") {
				hint = fmt.Sprintf("%s, bind it first ('let v = %s' before the 'if') and use 'v', or move the code into a function", lost.again, key)
			}
			return fmt.Errorf("[line %d] '%s' may be null: it was tested, but %s and %s\n  hint: %s", c.currentLine, key, lost.why, lost.event, hint)
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
