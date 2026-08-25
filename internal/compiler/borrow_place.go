package compiler

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// O empréstimo como LUGAR (issue #83).
//
// `ref a[i]` / `ref p.x` não denota um objeto, denota um LUGAR dentro de um
// composto que o copy-on-write pode bifurcar. A implementação anterior
// resolvia a base na CRIAÇÃO — `compileLValueBase` emitia a família *_MUT, que
// uniciza o caminho — e congelava o objeto resultante dentro do `ObjRef`. Isso
// deixa a referência correta no instante em que nasce e errada a partir do
// primeiro evento que mexa no caminho:
//
//	let copia = arr   depois do ref  -> a escrita vaza para 'copia'
//	copia = h; h.xs[1] = 7           -> o CoW bifurca; a escrita cai num órfão
//
// Agora a base é compilada como REFERÊNCIA ao lugar do pai, recursivamente até
// um ref de célula (`OP_REF_LOCAL`/`OP_REF_UPVALUE`/`OP_REF_GLOBAL`), que o CoW
// não move — ele troca o CONTEÚDO de um slot, nunca o slot. A caminhada, a
// unicização e a gravação do clone de volta acontecem no momento da ESCRITA,
// em `borrowContainer` (internal/vm/references.go).
//
// É a mesma caminhada que a família *_MUT faz para `a[i].x = v`; o que muda é o
// INSTANTE em que ela roda. Por isso o custo não é novo: uma escrita através de
// empréstimo passa a custar o que a atribuição direta equivalente já custava, e
// a unicização ansiosa da criação deixa de acontecer.

// compileBorrowBase empilha uma REFERÊNCIA ao lugar de `expr` e devolve o tipo
// estático do CONTEÚDO daquele lugar (já com ref desembrulhado, porque o
// runtime auto-dereferencia um lugar que guarda referência).
func (c *Compiler) compileBorrowBase(expr ast.Expression) (ast.NoxyType, error) {
	switch n := expr.(type) {
	case *ast.Identifier:
		// Nome que JÁ guarda uma referência (`xs: ref int[]`): o lugar é o
		// referente, e o valor do slot já é o ref que aponta para lá. Compilar
		// como valor empilha exatamente esse ref.
		if declared, known := c.identifierDeclaredType(n.Value); known {
			if refType, isRef := declared.(*ast.RefType); isRef {
				if _, _, err := c.Compile(n); err != nil {
					return nil, err
				}
				return refType.ElementType, nil
			}
		}
		// Nome comum: ref de célula. É a raiz da cadeia e o ponto em que a
		// recursão para.
		return c.compileReferenceArgumentValue(n)

	case *ast.MemberAccessExpression:
		owner, err := c.compileBorrowBase(n.Left)
		if err != nil {
			return nil, err
		}
		name := c.makeConstant(value.NewString(n.Member))
		c.emitOpWithConstantIndex(chunk.OP_REF_PROPERTY, name)
		return unwrapRefType(c.memberType(owner, n.Member)), nil

	case *ast.IndexExpression:
		container, err := c.compileBorrowBase(n.Left)
		if err != nil {
			return nil, err
		}
		if _, indexType, err := c.Compile(n.Index); err != nil {
			return nil, err
		} else if err := c.rejectRefRead(indexType, n.Index, "index"); err != nil {
			return nil, err
		}
		c.emitByte(byte(chunk.OP_REF_INDEX))
		return unwrapRefType(indexElementType(container)), nil
	}

	// Forma que o caminho de lugar não expressa (base `any` vinda de chamada,
	// literal): cadeia antiga, que uniciza na criação. Continua com o furo do
	// #83, mas é o comportamento que já existia — nenhuma regressão, e nenhuma
	// dessas formas é um lvalue nomeável de onde o vazamento parta.
	t, _, err := c.compileLValueBase(expr)
	return t, err
}

// identifierDeclaredType devolve o tipo declarado de um nome, na ordem de
// resolução do compilador (local, upvalue, global).
func (c *Compiler) identifierDeclaredType(name string) (ast.NoxyType, bool) {
	if slot, declared := c.resolveLocal(name); slot != -1 {
		return declared, declared != nil
	}
	if upvalue, declared := c.resolveUpvalue(name); upvalue != -1 {
		return declared, declared != nil
	}
	declared, ok := c.resolveGlobalType(name)
	return declared, ok && declared != nil
}
