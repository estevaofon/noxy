package compiler

import "noxy-vm/internal/ast"

// Issue #133: o tipo de um struct e a sua DECLARACAO, nao a grafia. Este
// arquivo concentra as duas operacoes de identidade:
//
//   - structDeclarationOf: a declaracao que um PrimitiveType designa —
//     prim.Decl quando preenchido; senao a resolucao por nome de sempre
//     (structDeclaration), que fica como rede de seguranca para nos que
//     nenhum ponto de resolucao tocou (a revisao adversarial procura esses);
//   - bindStructDecls: o UNICO ponto que preenche Decl. Roda dentro de
//     resolveAnnotation, in place e sem alocar — o fast path de custo zero
//     das anotacoes sem genericos (needsAnnotationResolution) fica intacto.
//
// Instancia generica (`main::Caixa<int>`, isGenericInstanceName) nunca recebe
// Decl: o nome achatado nao e identidade entre unidades de compilacao (spec
// §1.6) e programViewType a trata antes de olhar Decl.
func (c *Compiler) structDeclarationOf(prim *ast.PrimitiveType) *ast.StructStatement {
	if prim == nil {
		return nil
	}
	if prim.Decl != nil {
		return prim.Decl
	}
	if isBuiltinTypeName(prim.Name) {
		return nil
	}
	return c.structDeclaration(prim.Name)
}

// bindStructDecls preenche Decl em todo PrimitiveType de struct dentro de t
// que ainda nao o tenha, resolvendo o nome no escopo ATUAL do compilador
// (c.structs para nome simples, namespaceImports para `ns.T`). Nome que nao
// resolve fica com Decl nil e e reportado por checkDeclaredType. Idempotente:
// resolveStructFieldAnnotations roda mais de uma vez de proposito.
func (c *Compiler) bindStructDecls(t ast.NoxyType) {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		if typed.Decl != nil || isBuiltinTypeName(typed.Name) || isGenericInstanceName(typed.Name) {
			return
		}
		typed.Decl = c.structDeclaration(typed.Name)
	case *ast.ArrayType:
		c.bindStructDecls(typed.ElementType)
	case *ast.MapType:
		c.bindStructDecls(typed.KeyType)
		c.bindStructDecls(typed.ValueType)
	case *ast.RefType:
		c.bindStructDecls(typed.ElementType)
	case *ast.NullableType:
		c.bindStructDecls(typed.ElementType)
	case *ast.ChanType:
		c.bindStructDecls(typed.ElementType)
	case *ast.FunctionType:
		for _, param := range typed.Params {
			c.bindStructDecls(param)
		}
		c.bindStructDecls(typed.Return)
	case *ast.GenericType:
		for _, arg := range typed.Args {
			c.bindStructDecls(arg)
		}
	}
}
