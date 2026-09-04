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
	// Lugar que JÁ guarda uma referência — `xs: ref int[]`, ou o campo `next`
	// de um nó de lista (`next: ref Node`). O lugar de verdade é o REFERENTE, e
	// ler a expressão como valor já produz o ref que aponta para lá: uma
	// referência de célula, que é exatamente a raiz que a cadeia procura.
	//
	// Precisa vir ANTES da recursão. Emitir `OP_REF_PROPERTY` sobre um campo
	// declarado `ref T` dispara a guarda de R1 do runtime ("slot 'next' already
	// holds a reference"), que existe para o passo FINAL de um empréstimo — não
	// se toma referência de referência — mas que não sabe distinguir o passo
	// final de um passo INTERMEDIÁRIO do caminho, onde atravessar um campo ref
	// é legal e é como toda estrutura ligada do Noxy é percorrida.
	if declared, known := c.lvalueStaticType(expr); known {
		if refType, isRef := asRefType(declared); isRef {
			if _, _, err := c.Compile(expr); err != nil {
				return nil, err
			}
			return refType.ElementType, nil
		}
	}

	switch n := expr.(type) {
	case *ast.Identifier:
		if _, isNamespace := c.pureNamespaceAlias(n.Value); isNamespace {
			// Objeto do namespace: visao do modulo, nunca unicizado — issue
			// #133. OP_REF_GLOBAL faria a escrita unicizar a celula global, e
			// copyValue clona um ObjMap para um store NOVO: com mais de um
			// dono do mapa (dois `use` do mesmo modulo, ou `let m: any = st`)
			// o empréstimo cairia num orfao destacado do modulo. Empilhamos o
			// MAPA em si, por leitura simples; OP_REF_PROPERTY sobre uma base
			// nao-ref congela o container (Base vazia, borrowContainer devolve
			// ref.Container), o que aqui e exatamente certo: o CoW nunca move
			// esse mapa, porque nunca o unicizamos.
			nameConstant := c.makeConstant(value.NewString(n.Value))
			c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, nameConstant)
			return nil, nil
		}
		// Nome comum: ref de célula. É a raiz da cadeia e o ponto em que a
		// recursão para.
		return c.compileReferenceArgumentValue(n)

	case *ast.MemberAccessExpression:
		owner, err := c.compileBorrowBase(n.Left)
		if err != nil {
			return nil, err
		}
		// Spec §2.4: base `T?` ou `ref (T?)` — aplica o narrowing e recusa
		// a leitura sem teste, em vez de devolver `ref any` em silencio.
		owner, err = c.narrowBorrowOwner(n.Left, owner)
		if err != nil {
			return nil, err
		}
		name := c.makeConstant(value.NewString(n.Member))
		c.emitOpWithConstantIndex(chunk.OP_REF_PROPERTY, name)
		fieldType := c.memberType(owner, n.Member)
		if fieldType == nil && owner == nil {
			fieldType = c.namespaceMemberType(n) // issue #133: `ref m.x`
		}
		return unwrapRefType(fieldType), nil

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

// lvalueStaticType devolve o tipo estático de uma cadeia de l-value SEM emitir
// nada — a mesma recursão de compileBorrowBase, só que sobre tipos. Serve para
// decidir, antes de emitir, se um nível do caminho guarda uma referência.
func (c *Compiler) lvalueStaticType(expr ast.Expression) (ast.NoxyType, bool) {
	switch n := expr.(type) {
	case *ast.Identifier:
		return c.identifierDeclaredType(n.Value)
	case *ast.MemberAccessExpression:
		owner, ok := c.lvalueStaticType(n.Left)
		if !ok {
			// Issue #133: `m.x` com m alias de namespace (sem tipo proprio).
			t := c.namespaceMemberType(n)
			return t, t != nil
		}
		t := c.memberType(unwrapRefType(owner), n.Member)
		return t, t != nil
	case *ast.IndexExpression:
		container, ok := c.lvalueStaticType(n.Left)
		if !ok {
			return nil, false
		}
		t := indexElementType(unwrapRefType(container))
		return t, t != nil
	}
	return nil, false
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
