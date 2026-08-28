package compiler

import (
	"strings"

	"noxy-vm/internal/ast"
)

// memberType devolve o tipo estatico do campo member de um valor do tipo
// owner — ou nil quando owner nao e um struct conhecido ou nao tem o campo
// (acesso dinamico, como sempre foi).
//
// E o UNICO ponto de resolucao de campo por tipo do dono: leitura (`a.f`),
// escrita (`a.f = v`), base de lvalue (cow_lowering) e `ref a.f` passam por
// aqui, para que um valor tipado `io.File` e um tipado `File` (select)
// resolvam igual — o dono resolve pela DECLARACAO que designa
// (structDeclaration, #56 item 8), nao por nome simples em c.structs.
//
// O tipo devolvido esta sempre escrito na VISAO DO PROGRAMA. Um struct do
// proprio programa devolve o tipo do campo como declarado. Um struct de
// MODULO tem os campos escritos no vocabulario do modulo (`rows: Row[]` em
// sqlite.nx): devolve-lo cru vazaria nomes que o programa nao declarou — foi
// isso que quebrou `let row: sqlite.Row = res.rows[i]` ("expected sqlite.Row,
// got Row") na tentativa ingenua da 0.12.0. programViewType traduz esse tipo
// nome a nome (issue #58 item 1).
func (c *Compiler) memberType(owner ast.NoxyType, member string) ast.NoxyType {
	primitive, ok := unwrapRefType(owner).(*ast.PrimitiveType)
	if !ok {
		return nil
	}
	definition := c.structDeclaration(primitive.Name)
	if definition == nil {
		return nil
	}
	var fieldType ast.NoxyType
	for _, field := range definition.FieldsList {
		if field.Name == member {
			fieldType = field.Type
			break
		}
	}
	if fieldType == nil {
		return nil
	}
	origin := c.structOrigin(definition)
	if origin == "" {
		return fieldType
	}
	translated, ok := c.programViewType(fieldType, origin)
	if !ok {
		return nil
	}
	return translated
}

// programViewType traduz um tipo escrito dentro do modulo origin para a visao
// do programa: cada nome de struct e reescrito para o nome pelo qual o
// PROGRAMA consegue designar aquela declaracao (programStructName). Devolve
// ok=false quando alguma parte do tipo nao e nomeavel pelo programa — o
// chamador trata o tipo INTEIRO como dinamico, nunca um `map[string, ???]`
// meio-tipado. Nunca muta t (que pertence ao AST memoizado do modulo):
// devolve nos novos so quando algo muda.
func (c *Compiler) programViewType(t ast.NoxyType, origin string) (ast.NoxyType, bool) {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		if isBuiltinTypeName(typed.Name) {
			// Primitivos sao universais.
			return typed, true
		}
		if isGenericInstanceName(typed.Name) {
			// Instancia de template resolvida DENTRO do modulo (`c: Caixa<int>`
			// em g.nx vira `main::Caixa<int>`, porque o validador de modulo
			// compila com moduleName "main"): o qualificador NAO e identidade
			// global — o importador nomeia a mesma instancia `g::Caixa<int>`, e
			// um template LOCAL homonimo tambem produz `main::Caixa<int>`.
			// Devolver o nome cru rejeitava `let k: Caixa<int> = h.c` (programa
			// valido) ou, pior, tipava o campo pelo template local. O nome
			// estruturado (template + args) nao sobrevive no nome achatado;
			// sem como reconstrui-lo na visao do programa, o campo e dinamico.
			return nil, false
		}
		definition := c.lookupStructFrom(origin, typed.Name)
		if definition == nil {
			return nil, false
		}
		name := c.programStructName(definition)
		if name == "" {
			return nil, false
		}
		if name == typed.Name {
			return typed, true
		}
		return &ast.PrimitiveType{Name: name}, true
	case *ast.ArrayType:
		element, ok := c.programViewType(typed.ElementType, origin)
		if !ok {
			return nil, false
		}
		return &ast.ArrayType{ElementType: element, Size: typed.Size}, true
	case *ast.MapType:
		key, ok := c.programViewType(typed.KeyType, origin)
		if !ok {
			return nil, false
		}
		mapValue, ok := c.programViewType(typed.ValueType, origin)
		if !ok {
			return nil, false
		}
		return &ast.MapType{KeyType: key, ValueType: mapValue}, true
	case *ast.RefType:
		element, ok := c.programViewType(typed.ElementType, origin)
		if !ok {
			return nil, false
		}
		return &ast.RefType{ElementType: element}, true
	case *ast.NullableType:
		element, ok := c.programViewType(typed.ElementType, origin)
		if !ok {
			return nil, false
		}
		return nullable(element), true
	case *ast.ChanType:
		element, ok := c.programViewType(typed.ElementType, origin)
		if !ok {
			return nil, false
		}
		return &ast.ChanType{ElementType: element}, true
	case *ast.FunctionType:
		params := make([]ast.NoxyType, len(typed.Params))
		for i, param := range typed.Params {
			translated, ok := c.programViewType(param, origin)
			if !ok {
				return nil, false
			}
			params[i] = translated
		}
		result, ok := c.programViewType(normalizeReturnType(typed.Return), origin)
		if !ok {
			return nil, false
		}
		return &ast.FunctionType{Params: params, Return: result}, true
	default:
		// GenericType/TypeParamType nao sobrevivem a resolucao de anotacoes de
		// um modulo validado; nil tambem cai aqui. Sem como nomear: dinamico.
		return nil, false
	}
}

// programStructName devolve o nome pelo qual o programa (este compilador)
// designa a declaracao definition, ou "" quando nao ha como nomea-la:
//
//  1. o nome simples, se o programa importou ESSA declaracao por `select`
//     (`use db select Row` — c.structs["Row"] e o mesmo ponteiro). E o nome
//     que o programa escreveu, entao e o que aparece nas mensagens;
//  2. `alias.Nome`, para o PRIMEIRO `use m [as alias]` em ordem de declaracao
//     cujo modulo exporta a declaracao (diretamente ou reexportada por
//     `select *`);
//  3. "" — nem namespace nem select: o programa nao consegue escrever esse
//     tipo, e o chamador o trata como dinamico.
//
// Identidade sempre por PONTEIRO de declaracao: um `struct Row` local
// homonimo nao captura o `Row` do modulo.
func (c *Compiler) programStructName(definition *ast.StructStatement) string {
	if selected, ok := c.structs[definition.Name]; ok && selected == definition {
		return definition.Name
	}
	for _, alias := range c.namespaceOrder {
		module, isNamespace := c.namespaceImports[alias]
		if !isNamespace {
			continue
		}
		exported, loadable := c.discoverModuleStructs(module)
		if !loadable {
			continue
		}
		if exported[definition.Name] == definition {
			return alias + "." + definition.Name
		}
	}
	return ""
}

// isBuiltinTypeName reconhece os nomes de tipo que nao designam struct: os
// primitivos do parser (int, float, string, bool, bytes, any, void, func) e
// o `null` que a inferencia produz.
func isBuiltinTypeName(name string) bool {
	switch name {
	case "any", "null", "bool", "int", "float", "string", "bytes", "void", "func":
		return true
	}
	return false
}

// isGenericInstanceName reconhece o nome qualificado de uma instancia
// monomorfizada (`main::Caixa<int>`, ver instanceName).
func isGenericInstanceName(name string) bool {
	return strings.Contains(name, "::")
}
