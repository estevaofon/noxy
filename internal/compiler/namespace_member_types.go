package compiler

import "noxy-vm/internal/ast"

// namespaceMemberType devolve o tipo estatico de `alias.member` quando alias
// e um `use m [as alias]` (forma de namespace) nao sombreado por local ou
// upvalue — ou nil (dinamico) em qualquer outro caso.
//
// Issue #126 item 2: ate aqui o membro de namespace nao carregava tipo
// (importNamespace registra o modulo como objeto opaco), entao `m.f(...)`
// nao conferia aridade, argumentos nem retorno, e `let v = m.f()` nao
// inferia — enquanto `use m select f` conhecia a assinatura. O tipo vem da
// MESMA fonte do select (importedBindingType: funcao, construtor de struct
// ou `let` de topo, ja com anotacoes resolvidas) e e traduzido para a visao
// do programa pela regra da #58 item 1 (programViewType): `V` escrito
// dentro de vec.nx vira `vec.V` (primeiro alias declarado) ou `V` (se ha
// `select V`); qualquer parte que o programa nao consegue nomear torna o
// tipo INTEIRO dinamico, nunca meio-tipado. Template generico nao tem tipo
// de valor (importedBindingType devolve ok=false) e continua recusado em
// compileCallExpression com o hint de `select`.
//
// O acesso ao membro continua emitindo OP_GET_PROPERTY no objeto modulo; o
// que muda alem do tipo e o SITE DE CHAMADA: com assinatura inteiramente
// nomeavel a chamada passa pelo caminho isExact e emite OP_CALL_STATIC
// (modos provados), igual ao que `select` ja fazia. Excecao: um argumento
// `ref x` cujo tipo do alvo e desconhecido derruba modesProven e volta para
// OP_CALL, para validateParameterModes/validateRefTargets conferirem o alvo
// em runtime (compileCallExpression, ramo do parametro `ref`).
//
// Assimetria conhecida com `select`: um nome que chega a m so por
// REEXPORTACAO (`use x select *` dentro de m.nx) continua dinamico pelo
// namespace — moduleTopLevelBindings enxerga apenas as declaracoes do
// proprio m —, enquanto `use m select g` o resolve. Conservador (dinamico,
// nunca tipo errado) e documentado como follow-up.
func (c *Compiler) namespaceMemberType(access *ast.MemberAccessExpression) ast.NoxyType {
	base, ok := access.Left.(*ast.Identifier)
	// A guarda de sombreamento e a mesma de compileCallExpression (o hook do
	// template generico por namespace): um local ou upvalue com o nome do
	// alias vence. Hoje ela e defensiva — o chamador so consulta este caminho
	// quando a base compilou com tipo nil, e todo local/upvalue que sombreia
	// um alias carrega tipo estatico —, mas e a precondicao correta e evita
	// que um local de tipo desconhecido passe a ser lido como modulo.
	if !ok || c.isShadowedByLocal(base.Value) {
		return nil
	}
	module, isNamespace := c.namespaceImports[base.Value]
	if !isNamespace {
		return nil
	}
	declared, ok := c.importedBindingType(module, access.Member)
	if !ok || declared == nil {
		return nil
	}
	translated, ok := c.programViewType(declared, module)
	if !ok {
		return nil
	}
	return translated
}
