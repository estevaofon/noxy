package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Issue #133 item 1: `m.x = v` pelo namespace. Precedente: Python, Go
// (variavel exportada de pacote), Nim, Swift (`public var`) permitem escrever
// num global de outro modulo. A regra "module variables are read-only
// outside the module" (0.11.0, #56 §8b) era remendo para uma escrita que
// gravava num binding que ninguem lia; desde o #126 o membro tem tipo
// estatico e o objeto do namespace compartilha o bindingStore do modulo
// (GlobalEnvironment.ExportMap), entao a escrita cai na variavel viva —
// a leitura ja era "live" pela spec §11; a escrita passa a ser tambem.
// `select` continua snapshot.

// pureNamespaceAlias reporta se name e um alias de `use m [as name]` nao
// sombreado por local/upvalue nem por um global tipado homonimo — o global
// tem de ser o marcador que importNamespace deixou (presente, tipo nil).
func (c *Compiler) pureNamespaceAlias(name string) (string, bool) {
	globalType, isGlobal := c.globals[name]
	if !isGlobal || globalType != nil || c.isShadowedByLocal(name) {
		return "", false
	}
	module, isNamespace := c.namespaceImports[name]
	return module, isNamespace
}

// compileNamespaceMemberAssignment compila `alias.member = valor`:
//
//   - membro inexistente: `'m' has no member 'y'`; funcao/struct: `cannot
//     assign to 'm.f': it is a function`;
//   - `let` do modulo: o MESMO protocolo da atribuicao a global (compiler.go,
//     ramo Identifier): membro `ref T` so aceita rebind por ref/null;
//     membro comum exige areTypesCompatible; emitSlotGuards; e a escrita e
//     OP_SET_PROPERTY no objeto do namespace (ramo ObjMap do VM);
//   - tipo declarado que a visao do programa nao traduz (instancia de struct
//     generico do modulo): RECUSADA (issue #133, caso 2) — escrever sem
//     checagem quebrava o modulo por dentro;
//   - modulo nao carregavel (ou `let` sem tipo declarado): escrita dinamica,
//     sem checagem estatica e sem erro novo (como um global `any`).
//
// Pilha: OP_GET_GLOBAL alias (leitura simples — a escrita e no store
// compartilhado, nao numa copia), valor, OP_SET_PROPERTY ([base, val] ->
// [val]), OP_POP.
func (c *Compiler) compileNamespaceMemberAssignment(n *ast.AssignStmt, alias, module string, target *ast.MemberAccessExpression) (*chunk.Chunk, ast.NoxyType, error) {
	member := target.Member
	targetName := alias + "." + member
	var memberType ast.NoxyType
	if origin := c.declaringModule(module, member); origin != "" {
		bindings, _ := c.moduleTopLevelBindings(origin)
		switch declaration := bindings[member].(type) {
		case *ast.FunctionStatement:
			return nil, nil, fmt.Errorf("[line %d] cannot assign to '%s': it is a function\n  hint: only module variables ('let') can be assigned", c.currentLine, targetName)
		case *ast.StructStatement:
			return nil, nil, fmt.Errorf("[line %d] cannot assign to '%s': it is a struct\n  hint: only module variables ('let') can be assigned", c.currentLine, targetName)
		case *ast.LetStmt:
			memberType = c.namespaceMemberType(target)
			// Issue #133 (caso 2): o membro TEM tipo declarado, mas a visao do
			// programa nao consegue traduzi-lo (instancia de struct generico
			// do modulo, spec §1.6 — importedBindingType devolve o tipo do
			// `let` inalterado, entao aqui so programViewType pode ter
			// falhado). Ler assim continua dinamico e conservador; escrever
			// nao: a escrita dinamica gravava um valor de outro tipo no global
			// do modulo e o proprio modulo falhava depois, com o erro numa
			// linha DENTRO dele. Compilador fala primeiro.
			if memberType == nil && declaration.Type != nil {
				return nil, nil, fmt.Errorf("[line %d] cannot assign to '%s': its type cannot be translated here (it involves an instance of a generic struct of '%s')\n  hint: expose a function in '%s' that updates it", c.currentLine, targetName, origin, origin)
			}
		}
	} else if _, loadable := c.moduleTopLevelBindings(module); loadable {
		return nil, nil, fmt.Errorf("[line %d] '%s' has no member '%s'", c.currentLine, alias, member)
	}

	c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString(alias)))

	// §3 target-typing, posicao 4: o tipo declarado do membro e o alvo de um
	// template de funcao nu no valor.
	if err := c.rewriteIfGenericValue(n.Value, memberType); err != nil {
		return nil, nil, err
	}
	_, valType, err := c.Compile(n.Value)
	if err != nil {
		return nil, nil, err
	}

	if memberType != nil {
		if refType, isRef := asRefType(memberType); isRef {
			_, isRefVal := asRefType(valType)
			if !(isRefVal || valType == nil || isNullType(valType)) {
				if c.areTypesCompatible(refType.ElementType, valType) {
					return nil, nil, referenceAssignmentTypeError(c.currentLine, targetName, memberType, valType)
				}
				return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to '%s': expected %s, got %s", c.currentLine, targetName, memberType.String(), valType.String())
			}
		}
		if !c.areTypesCompatible(memberType, valType) {
			return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to '%s': expected %s, got %s%s%s", c.currentLine, targetName, memberType.String(), valType.String(), c.derefReadHint(memberType, valType, n.Value), c.nullMismatchHint(memberType, valType, n.Value))
		}
		if err := c.emitSlotGuards(memberType, valType); err != nil {
			return nil, nil, err
		}
	}

	c.emitOpWithConstantIndex(chunk.OP_SET_PROPERTY, c.makeConstant(value.NewString(member)))
	c.emitByte(byte(chunk.OP_POP))
	return c.currentChunk, nil, nil
}
