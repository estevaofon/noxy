package compiler

// Target-typing (spec §3): toda função que existe em runtime é valor de
// primeira classe; o template nunca é. A instanciação acontece no ponto em
// que a genérica vira valor, guiada pelo tipo alvo — este arquivo concentra
// as cinco posições enumeradas no §4 (`let` anotado, `return`, elemento de
// array literal, atribuição a campo, argumento de chamada) e o catálogo de
// erro do §9 para quando não há alvo concreto.

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// instantiateForTarget e o hook central do §3: quando name nomeia um template
// de FUNÇÃO visível (não sombreado por local/upvalue) e target e um
// *ast.FunctionType concreto (sem TypeParamType, após qualquer substituição
// já feita pelo chamador), unifica a assinatura do template contra target,
// garante a instância (ensureFunctionInstance) e devolve o nome qualificado
// para o chamador reescrever o identificador no AST.
//
// O segundo retorno (achouTemplate) distingue "não é o caso" de "é o caso e
// deu erro": false quando name não nomeia um template de função (ou está
// sombreado) — o chamador segue o caminho normal sem tocar em nada. true com
// err != nil é o erro do §9 (sem alvo concreto, ou falha de unificação);
// true com err == nil é o nome qualificado pronto para a reescrita.
//
// Só atua no pass 1 (§4): no pass 2 o AST já foi inteiramente reescrito pelo
// pass 1 (que compila o programa inteiro), então nenhum nome de template
// deveria alcançar esta função ali — o guard devolve "não encontrado" em vez
// de arriscar reinstanciar às cegas fora da janela em que a fila de
// instâncias (c.instances) está de fato ligada ao Program sendo montado.
func (c *Compiler) instantiateForTarget(name string, target ast.NoxyType, line int) (string, bool, error) {
	if !c.pass1 {
		return "", false, nil
	}
	tpl, isTemplate := c.registryOrInit().Funcs[name]
	if !isTemplate || c.isShadowedByLocal(name) {
		return "", false, nil
	}

	targetFn, ok := target.(*ast.FunctionType)
	if !ok || containsTypeParam(targetFn) {
		return "", true, noConcreteTargetError(line, name)
	}

	bindings := make(map[string]ast.NoxyType, len(tpl.Decl.TypeParams))
	templateSig := newFunctionType(tpl.Decl.Parameters, tpl.Decl.ReturnType)
	if err := unify(templateSig, targetFn, bindings); err != nil {
		return "", true, fmt.Errorf("[line %d] %v", line, err)
	}

	instName, _, err := c.ensureFunctionInstance(tpl, bindings, line)
	if err != nil {
		return "", true, err
	}
	return instName, true, nil
}

// rewriteIfGenericValue e o ponto de entrada comum das quatro posições
// "simples" do §3 (`let`, `return`, elemento de array, campo): quando expr é
// um *ast.Identifier nomeando um template de função, instancia contra target
// e reescreve expr.Value in-place para o nome qualificado; devolve o erro do
// §9 quando não há alvo concreto. Quando expr não é esse caso (não é
// Identifier, ou o identificador não nomeia um template), é um no-op — o
// chamador segue o caminho normal de compilação.
func (c *Compiler) rewriteIfGenericValue(expr ast.Expression, target ast.NoxyType) error {
	ident, ok := expr.(*ast.Identifier)
	if !ok {
		return nil
	}
	qualified, isTemplate, err := c.instantiateForTarget(ident.Value, target, ident.Token.Line)
	if !isTemplate {
		return nil
	}
	if err != nil {
		return err
	}
	ident.Value = qualified
	return nil
}

// isShadowedByLocal reporta se name resolve para um local ou upvalue no
// escopo atual — um binding mais interno vence sobre um template homônimo,
// mesma regra do sombreamento em call sites (compileCallExpression).
func (c *Compiler) isShadowedByLocal(name string) bool {
	if slot, _ := c.resolveLocal(name); slot != -1 {
		return true
	}
	if slot, _ := c.resolveUpvalue(name); slot != -1 {
		return true
	}
	return false
}

// setArrayElementHint arma o alvo de elemento de array do §3 (posição 3): o
// tipo de elemento da anotação do `let` quando o valor é, de fato, um array
// literal direto. Mesma disciplina de setGenericReturnHint — só arma quando o
// valor É o caso que o hint serve, para nenhuma outra expressão o consumir
// por engano.
func (c *Compiler) setArrayElementHint(target ast.NoxyType, valueExpr ast.Expression) {
	c.arrayElementHint = nil
	if !c.pass1 || target == nil {
		return
	}
	arrayTarget, ok := target.(*ast.ArrayType)
	if !ok {
		return
	}
	if _, ok := valueExpr.(*ast.ArrayLiteral); !ok {
		return
	}
	c.arrayElementHint = arrayTarget.ElementType
}

// bareFunctionTemplateArgument reporta se expr é um *ast.Identifier nomeando
// um template de FUNÇÃO visível (não sombreado) — a forma que não pode
// compilar pelo caminho normal (o template não existe em runtime, e
// c.Compile de um Identifier assim leria um global nunca definido). Usado
// pelo argumento de chamada do §3 (posição 5) para separar, num call site
// genérico, os argumentos que precisam de unificação bidirecional dos
// argumentos comuns que ancoram bindings pelo caminho de sempre.
func (c *Compiler) bareFunctionTemplateArgument(expr ast.Expression) (*ast.Identifier, *FuncTemplate, bool) {
	ident, ok := expr.(*ast.Identifier)
	if !ok {
		return nil, nil, false
	}
	tpl, isTemplate := c.registryOrInit().Funcs[ident.Value]
	if !isTemplate || c.isShadowedByLocal(ident.Value) {
		return nil, nil, false
	}
	return ident, tpl, true
}

// noConcreteTargetError e a mensagem verbatim do §9 para um identificador de
// template de função em posição de valor sem alvo concreto (`func` nu, `any`,
// ou corrente sem âncora).
func noConcreteTargetError(line int, name string) error {
	return fmt.Errorf(
		"[line %d] função genérica '%s' precisa de tipo concreto — anote a assinatura completa ou chame diretamente",
		line, name,
	)
}

// noConcreteStructTargetError e o par de noConcreteTargetError para STRUCT
// genérico: identificador nomeando um template de struct em posição de valor
// sem nada de onde tirar a tupla de argumentos de tipo (nem chamada de
// construtor — que passaria pelo hook de compileGenericConstructorSite —, nem
// anotação de `let` que instancie via resolveAnnotation).
func noConcreteStructTargetError(line int, name string) error {
	return fmt.Errorf(
		"[line %d] struct genérico '%s' precisa de tipo concreto — anote os argumentos de tipo ou construa diretamente",
		line, name,
	)
}

// rejectBareGenericTemplateIdentifier e o fallback do §9 para as posições de
// valor que NENHUM hook do §3/§4 intercepta antes de alcançar o case
// genérico de Identifier em compiler.go (Compile) — valor de map literal,
// expression statement solto. As cinco posições hookadas (§3: `let`
// anotado, `return`, elemento de array, campo de struct, argumento de
// chamada) e o call site direto (§4, callee de CallExpression, para função
// OU construtor de struct) já reescrevem identifier.Value para o nome
// qualificado da instância ANTES de qualquer Compile(identifier) acontecer —
// nesses casos identifier.Value não é mais chave de nenhum dos dois mapas do
// registry e esta função é um no-op.
//
// Quando o identificador NOMEIA um template e chegou aqui intocado, a
// causa é sempre a mesma: nenhum hook cobre esta posição, e compilar
// normalmente leria um global que nunca foi definido (template nunca emite
// bytecode) — mensagem confusa a jusante em vez do erro claro do catálogo.
// Chamador (compiler.go, case *ast.Identifier) só invoca isto depois que
// resolveLocal/resolveUpvalue já falharam, então a mesma regra de
// sombreamento das outras famílias de hook vale aqui de graça.
//
// c.generics pode ser nil (programa sem declaração genérica nenhuma, §5): o
// guard evita alocar o registry lazily (registryOrInit) só para descobrir
// que está vazio — custo zero por identificador em programa comum.
func (c *Compiler) rejectBareGenericTemplateIdentifier(identifier *ast.Identifier) error {
	if c.generics == nil {
		return nil
	}
	if _, isFuncTemplate := c.generics.Funcs[identifier.Value]; isFuncTemplate {
		return noConcreteTargetError(identifier.Token.Line, identifier.Value)
	}
	if _, isStructTemplate := c.generics.Structs[identifier.Value]; isStructTemplate {
		return noConcreteStructTargetError(identifier.Token.Line, identifier.Value)
	}
	return nil
}

// unifyBidirectional e a versão "dois lados" de unify, exigida pela
// unificação bidirecional do §3 (posição 5, argumento de chamada): expected
// pode conter TypeParamType do template do CALLER (bindings em
// callerBindings — ex.: B em aplica<A,B>) e actual pode conter TypeParamType
// do template do PRÓPRIO ARGUMENTO (bindings em argBindings — ex.: T em
// identity<T>). Nenhuma das duas assinaturas está totalmente concreta ainda:
// `func(int) -> B` (A já resolvido para int pelos argumentos não-genéricos,
// mas B ainda livre) contra `func(T) -> T`.
//
// Em cada posição da recursão, um TypeParamType já resolvido no SEU PRÓPRIO
// lado é primeiro substituído pelo tipo concreto conhecido — é isso que
// permite `T=int`, descoberto na posição de parâmetro, aparecer concreto
// quando a recursão chega na posição de retorno e precisa bindar B=int.
//
// Regras por par de nós, depois da resolução:
//   - os dois TypeParamType ainda não resolvidos: nada a fazer agora — se
//     nenhuma outra posição os resolver, a checagem de aridade final em
//     ensureFunctionInstance (chamada pelo caller) denuncia com "não foi
//     possível inferir".
//   - só expected é TypeParamType (não resolvido): binda em callerBindings.
//   - só actual é TypeParamType (não resolvido): binda em argBindings, papéis
//     trocados.
//   - os dois concretos: mesma recursão estrutural e mesmas folgas de
//     compatibilidade de unify (any/null não bindam, `func` nu aceita
//     qualquer chamável).
func unifyBidirectional(expected, actual ast.NoxyType, callerBindings, argBindings map[string]ast.NoxyType) error {
	if expected == nil || actual == nil {
		return nil
	}

	if tp, ok := expected.(*ast.TypeParamType); ok {
		if bound, ok := callerBindings[tp.Name]; ok {
			expected = bound
		}
	}
	if tp, ok := actual.(*ast.TypeParamType); ok {
		if bound, ok := argBindings[tp.Name]; ok {
			actual = bound
		}
	}

	expTP, expIsTP := expected.(*ast.TypeParamType)
	actTP, actIsTP := actual.(*ast.TypeParamType)

	switch {
	case expIsTP && actIsTP:
		// Nenhum lado resolvido ainda por esta posição; outra posição da
		// mesma assinatura (ou o hint do `let`) pode resolver depois.
		return nil
	case expIsTP:
		return bindTypeParam(callerBindings, expTP.Name, actual)
	case actIsTP:
		return bindTypeParam(argBindings, actTP.Name, expected)
	}

	// Ambos concretos: mesmas folgas de compatibilidade de unify (comentário
	// em generics_unify.go explica cada uma).
	if isAny(expected) || isAny(actual) || isNullType(actual) {
		return nil
	}
	if isBareFunctionType(expected) {
		if isCallableType(actual) {
			return nil
		}
		return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
	}

	switch exp := expected.(type) {
	case *ast.PrimitiveType:
		act, ok := actual.(*ast.PrimitiveType)
		if !ok || act.Name != exp.Name {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return nil

	case *ast.ArrayType:
		act, ok := actual.(*ast.ArrayType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return unifyBidirectional(exp.ElementType, act.ElementType, callerBindings, argBindings)

	case *ast.MapType:
		act, ok := actual.(*ast.MapType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		if err := unifyBidirectional(exp.KeyType, act.KeyType, callerBindings, argBindings); err != nil {
			return err
		}
		return unifyBidirectional(exp.ValueType, act.ValueType, callerBindings, argBindings)

	case *ast.RefType:
		act, ok := actual.(*ast.RefType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return unifyBidirectional(exp.ElementType, act.ElementType, callerBindings, argBindings)

	case *ast.ChanType:
		act, ok := actual.(*ast.ChanType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return unifyBidirectional(exp.ElementType, act.ElementType, callerBindings, argBindings)

	case *ast.FunctionType:
		act, ok := actual.(*ast.FunctionType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		if len(exp.Params) != len(act.Params) {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		for i, p := range exp.Params {
			if err := unifyBidirectional(p, act.Params[i], callerBindings, argBindings); err != nil {
				return err
			}
		}
		return unifyBidirectional(exp.Return, act.Return, callerBindings, argBindings)

	case *ast.GenericType:
		act, ok := actual.(*ast.GenericType)
		if !ok || exp.Name != act.Name || len(exp.Args) != len(act.Args) {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		for i, a := range exp.Args {
			if err := unifyBidirectional(a, act.Args[i], callerBindings, argBindings); err != nil {
				return err
			}
		}
		return nil

	default:
		return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
	}
}

// bindTypeParam aplica a um bindings map as mesmas regras do case
// TypeParamType de unify: ref é proibido como alvo de binding, any/null não
// contribuem binding, e um binding já existente que diverge é conflito.
func bindTypeParam(bindings map[string]ast.NoxyType, name string, value ast.NoxyType) error {
	if _, isRef := value.(*ast.RefType); isRef {
		return fmt.Errorf("%s não pode ser um tipo ref (tentativa de bindar %s)", name, value.String())
	}
	if isAny(value) || isNullType(value) {
		return nil
	}
	if existing, ok := bindings[name]; ok {
		if existing.String() != value.String() {
			return fmt.Errorf("%s inferido como %s e %s", name, existing.String(), value.String())
		}
		return nil
	}
	bindings[name] = ast.CloneType(value)
	return nil
}
