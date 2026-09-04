package compiler

import (
	"fmt"
	"strings"
	"unicode"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

func (c *Compiler) runtimeTypeInfo(t ast.NoxyType) *value.RuntimeTypeInfo {
	info, ok := c.runtimeTypeInfoWithStructs(t, make(map[*ast.StructStatement]*value.RuntimeTypeInfo), "")
	if !ok {
		return nil
	}
	return info
}

func (c *Compiler) emitRuntimeValueType(t ast.NoxyType) error {
	if !c.requiresRuntimeValueType(t, make(map[*ast.StructStatement]bool), "") {
		return nil
	}
	return c.emitMarkRuntimeValueType(t)
}

// emitDynamicBoundaryGuard e a checagem de runtime da fronteira dinamica
// (spec §4.2, #118/#120): quando o tipo estatico do valor e `any` e o slot
// tem tipo concreto, o valor no topo da pilha e conferido contra o tipo do
// slot — `expected int, got string`. Vale para let anotado, atribuicao,
// argumento de assinatura exata e return. Tipos que ja carregam metadado
// (array, map, chan) foram marcados — e validados — por emitRuntimeValueType;
// o resto (primitivos, structs) so passa por aqui. Codigo tipado nao paga
// nada.
//
// Tipo estatico DESCONHECIDO (nil: nativo sem assinatura, plugin; depois
// da #126 um membro de namespace `m.f()` so cai aqui quando o programa nao
// consegue NOMEAR o tipo do modulo) fica de fora de proposito (revisao do #119,
// decisao B): 134 natives da stdlib nao declaram contrato de retorno, e a
// guarda em cada `return native(...)` custava +35-55 % por chamada de
// wrapper, alem de transformar nulls silenciosos de crypto/net em aborts
// dentro da stdlib. Contrato de retorno dos natives e issue propria.
func (c *Compiler) emitDynamicBoundaryGuard(expected, actual ast.NoxyType) error {
	if expected == nil || isAny(expected) {
		return nil
	}
	if !isAny(actual) {
		return nil
	}
	if c.requiresRuntimeValueType(expected, make(map[*ast.StructStatement]bool), "") {
		return nil
	}
	return c.emitMarkRuntimeValueType(expected)
}

// emitSlotGuards e o UNICO ponto de emissao das checagens de um slot tipado
// que recebe um valor (let, atribuicao, argumento, return, elemento de
// append, chave de delete...): metadado de composto (emitRuntimeValueType) e
// guarda da fronteira dinamica (emitDynamicBoundaryGuard). Todo site de slot
// passa por aqui — a revisao do #119 achou tres sites que emitiam so o
// primeiro.
func (c *Compiler) emitSlotGuards(expected, actual ast.NoxyType) error {
	if err := c.emitRuntimeValueType(expected); err != nil {
		return err
	}
	return c.emitDynamicBoundaryGuard(expected, actual)
}

// emitMarkRuntimeValueType emite OP_MARK_RUNTIME_VALUE_TYPE para o tipo t
// sobre o valor no topo da pilha; no-op para tipos sem descricao de runtime.
func (c *Compiler) emitMarkRuntimeValueType(t ast.NoxyType) error {
	runtimeType := c.runtimeTypeInfo(t)
	if runtimeType == nil {
		return nil
	}
	typeConstant := c.makeConstant(value.NewRuntimeTypeInfo(runtimeType))
	if typeConstant > 65535 {
		return fmt.Errorf("[line %d] too many constants for runtime value metadata", c.currentLine)
	}
	c.emitByte(byte(chunk.OP_MARK_RUNTIME_VALUE_TYPE))
	c.emitByte(byte((typeConstant >> 8) & 0xff))
	c.emitByte(byte(typeConstant & 0xff))
	return nil
}

// requiresRuntimeValueType responde se um valor do tipo t carrega metadado de
// runtime (canal, array, map — direta ou transitivamente por campos de
// struct). origin e a unidade de compilacao em que t foi escrito ("" = o
// programa; o nome de um modulo para os campos de um struct importado), para
// que `io.File` no programa e `Inner` dentro de mod_a resolvam no escopo
// certo — ver lookupStructFrom.
func (c *Compiler) requiresRuntimeValueType(t ast.NoxyType, visiting map[*ast.StructStatement]bool, origin string) bool {
	switch typed := t.(type) {
	case *ast.ChanType:
		return true
	case *ast.ArrayType, *ast.MapType:
		return true
	case *ast.RefType:
		return c.requiresRuntimeValueType(typed.ElementType, visiting, origin)
	case *ast.NullableType:
		return c.requiresRuntimeValueType(typed.ElementType, visiting, origin)
	case *ast.FunctionType:
		// Callable signatures carry their own immutable runtime schema. Channels
		// named by that signature are not values embedded in the callable object.
		return false
	case *ast.PrimitiveType:
		definition := typed.Decl
		if definition == nil {
			definition = c.lookupStructFrom(origin, typed.Name)
		}
		if definition == nil || visiting[definition] {
			return false
		}
		visiting[definition] = true
		defer delete(visiting, definition)
		fieldOrigin := c.structOrigin(definition)
		for _, field := range definition.FieldsList {
			if c.requiresRuntimeValueType(field.Type, visiting, fieldOrigin) {
				return true
			}
		}
	}
	return false
}

// runtimeTypeInfoWithStructs returns ok only for a complete runtime schema.
// A composite carrying an unknown child would otherwise turn that child into
// an accidental dynamic wildcard at typed native boundaries.
//
// Um nome de struct resolve pela DECLARACAO que designa (lookupStructFrom):
// `File` importado por select e `io.File` qualificado sao o mesmo ponteiro,
// e os campos de um struct importado resolvem no escopo do MODULO que o
// declarou (structOrigin) — `Outer{i: Inner}` de mod_a fecha mesmo que o
// programa nunca tenha importado `Inner`. O memo structs e por ponteiro de
// declaracao, para que dois structs homonimos de modulos diferentes nao
// compartilhem layout.
func (c *Compiler) runtimeTypeInfoWithStructs(t ast.NoxyType, structs map[*ast.StructStatement]*value.RuntimeTypeInfo, origin string) (*value.RuntimeTypeInfo, bool) {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		switch typed.Name {
		case "any":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_ANY}, true
		case "null":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_NULL}, true
		case "bool":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_BOOL}, true
		case "int":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_INT}, true
		case "float":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_FLOAT}, true
		case "string":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}, true
		case "bytes":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_BYTES}, true
		case "void":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_VOID}, true
		case "func":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, CallableBare: true}, true
		}
		definition := typed.Decl
		if definition == nil {
			definition = c.lookupStructFrom(origin, typed.Name)
		}
		if definition == nil {
			return nil, false
		}
		if existing, ok := structs[definition]; ok {
			return existing, true
		}
		info := &value.RuntimeTypeInfo{
			Kind:   value.TYPE_STRUCT,
			Name:   definition.Name,
			Fields: make(map[string]*value.RuntimeTypeInfo, len(definition.FieldsList)),
		}
		structs[definition] = info
		fieldOrigin := c.structOrigin(definition)
		for _, field := range definition.FieldsList {
			fieldInfo, complete := c.runtimeTypeInfoWithStructs(field.Type, structs, fieldOrigin)
			if !complete {
				delete(structs, definition)
				return nil, false
			}
			info.Fields[field.Name] = fieldInfo
		}
		return info, true
	case *ast.ArrayType:
		element, ok := c.runtimeTypeInfoWithStructs(typed.ElementType, structs, origin)
		if !ok {
			return nil, false
		}
		return &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: element, Size: typed.Size}, true
	case *ast.MapType:
		key, ok := c.runtimeTypeInfoWithStructs(typed.KeyType, structs, origin)
		if !ok {
			return nil, false
		}
		mapValue, ok := c.runtimeTypeInfoWithStructs(typed.ValueType, structs, origin)
		if !ok {
			return nil, false
		}
		return &value.RuntimeTypeInfo{Kind: value.TYPE_MAP, Key: key, Value: mapValue}, true
	case *ast.RefType:
		element, ok := c.runtimeTypeInfoWithStructs(typed.ElementType, structs, origin)
		if !ok {
			return nil, false
		}
		return &value.RuntimeTypeInfo{Kind: value.TYPE_REF, Element: element}, true
	case *ast.NullableType:
		// Copia rasa com a flag: o schema do elemento pode ser memoizado
		// (structs) e nao pode ser mutado no lugar.
		element, ok := c.runtimeTypeInfoWithStructs(typed.ElementType, structs, origin)
		if !ok {
			return nil, false
		}
		info := *element
		info.Nullable = true
		return &info, true
	case *ast.ChanType:
		element, ok := c.runtimeTypeInfoWithStructs(typed.ElementType, structs, origin)
		if !ok {
			return nil, false
		}
		return &value.RuntimeTypeInfo{Kind: value.TYPE_CHANNEL, Element: element}, true
	case *ast.FunctionType:
		info := &value.RuntimeTypeInfo{
			Kind:       value.TYPE_CALLABLE,
			Params:     make([]*value.RuntimeTypeInfo, len(typed.Params)),
			ParamIsRef: make([]bool, len(typed.Params)),
		}
		for i, param := range typed.Params {
			paramInfo, ok := c.runtimeTypeInfoWithStructs(param, structs, origin)
			if !ok {
				return nil, false
			}
			info.Params[i] = paramInfo
			_, info.ParamIsRef[i] = asRefType(param)
		}
		result := normalizeReturnType(typed.Return)
		returnInfo, ok := c.runtimeTypeInfoWithStructs(result, structs, origin)
		if !ok {
			return nil, false
		}
		info.Return = returnInfo
		return info, true
	default:
		return nil, false
	}
}

// unknownFieldTypeError e checkDeclaredType aplicado a cada campo de decl:
// todo nome de tipo num campo tem de designar um struct conhecido — erro de
// COMPILACAO com hint, nunca o "struct constructor has incomplete runtime
// type metadata" incondicional de runtime (issue #58 item 2; a forma
// qualificada `ns.T` ja era erro desde a 0.12.0).
func (c *Compiler) unknownFieldTypeError(decl *ast.StructStatement) error {
	// Instancia de template IMPORTADO (`caixas::Caixa<int>`): o campo que nao
	// resolve foi escrito dentro do modulo do template, entao o hint pode
	// dizer de onde importa-lo (`use caixas select Meta`) em vez do `m`
	// generico. Instancia de template local (`main::...`) e struct comum: "".
	importFrom := ""
	if module, _, isInstance := strings.Cut(decl.Name, "::"); isInstance && module != c.moduleName {
		importFrom = module
	}
	for _, field := range decl.FieldsList {
		position := fmt.Sprintf("struct '%s' field '%s'", displayStructName(decl.Name), field.Name)
		if err := c.checkDeclaredTypeFrom(field.Type, decl.Token.Line, position, importFrom); err != nil {
			return err
		}
	}
	return nil
}

// checkSignatureTypes e checkDeclaredType aplicado aos parametros e ao
// retorno de uma assinatura (func declarada ou literal; name vazio = literal
// anonimo, sem prefixo "function 'f'").
func (c *Compiler) checkSignatureTypes(name string, params []*ast.Parameter, returnType ast.NoxyType, line int) error {
	prefix := ""
	if name != "" {
		prefix = fmt.Sprintf("function '%s' ", name)
	}
	// Parametro duplicado (issue #47 parte 1): sem este check o segundo `x`
	// sombreia o primeiro em resolveLocal e o primeiro argumento fica
	// inalcancavel para sempre, em silencio.
	display := name
	if display == "" {
		display = "<anonymous>"
	}
	seen := make(map[string]struct{}, len(params))
	for _, param := range params {
		if _, dup := seen[param.Name]; dup {
			return fmt.Errorf("[line %d] duplicate parameter '%s' in function '%s'", line, param.Name, display)
		}
		seen[param.Name] = struct{}{}
	}
	for _, param := range params {
		if err := c.checkDeclaredType(param.Type, line, fmt.Sprintf("%sparameter '%s'", prefix, param.Name)); err != nil {
			return err
		}
	}
	if returnType != nil {
		if err := c.checkDeclaredType(returnType, line, prefix+"return type"); err != nil {
			return err
		}
	}
	return nil
}

// checkDeclaredType exige que todo nome de struct dentro de t (inclusive em
// array, map, ref, chan e assinatura) designe uma declaracao conhecida neste
// compilador — nome simples via c.structs, `ns.T` via o namespace importado,
// instancia generica ja registrada (structDeclaration). position e o lugar
// da anotacao na mensagem ("struct 'A' field 'b'", "function 'f' parameter
// 'x'", "function 'f' return type", "variable 'x'").
//
// Nome simples desconhecido: `unknown type 'T'` + hint de declarar/importar.
// Nome qualificado: mantem o diagnostico da 0.12.0 (`cannot resolve type
// 'io.Nope': module 'io' has no struct 'Nope'` / `'foo' is not an imported
// module`), que diz exatamente qual metade falhou.
func (c *Compiler) checkDeclaredType(t ast.NoxyType, line int, position string) error {
	return c.checkDeclaredTypeFrom(t, line, position, "")
}

// checkDeclaredTypeFrom e checkDeclaredType com o modulo sugerido no hint de
// importacao (`use <importFrom> select T`) quando o chamador sabe de onde o
// nome veio; "" usa o `m` generico.
func (c *Compiler) checkDeclaredTypeFrom(t ast.NoxyType, line int, position, importFrom string) error {
	name, found := firstUnknownTypeName(t, func(candidate *ast.PrimitiveType) bool {
		return c.structDeclarationOf(candidate) != nil
	})
	if !found {
		return nil
	}
	if !isQualifiedTypeName(name) {
		module := importFrom
		if module == "" {
			module = "m"
		}
		return fmt.Errorf("[line %d] %s: unknown type '%s'\n  hint: declare 'struct %s' or import it with 'use %s select %s'",
			line, position, name, name, module, name)
	}
	ns, base, _ := strings.Cut(name, ".")
	var reason, hint string
	module, isNamespace := c.namespaceImports[ns]
	switch {
	case !isNamespace:
		reason = fmt.Sprintf("'%s' is not an imported module", ns)
		hint = fmt.Sprintf("add 'use %s' at the top of the file", ns)
	default:
		if _, loadable := c.discoverModuleStructs(module); !loadable {
			reason = fmt.Sprintf("module '%s' could not be loaded", module)
			hint = "check the module path"
		} else {
			reason = fmt.Sprintf("module '%s' has no struct '%s'", module, base)
			hint = fmt.Sprintf("check the struct name against the structs declared in '%s'", module)
		}
	}
	return fmt.Errorf("[line %d] %s: cannot resolve type '%s': %s\n  hint: %s",
		line, position, name, reason, hint)
}

// firstUnknownTypeName procura em t (inclusive dentro de array, map, ref,
// chan, assinatura e argumentos genericos) o primeiro nome de tipo que nao e
// primitivo e que resolves rejeita. TypeParamType nao conta: fora de template
// resolveAnnotation ja o recusou; dentro de instancia ja foi substituido.
func firstUnknownTypeName(t ast.NoxyType, resolves func(*ast.PrimitiveType) bool) (string, bool) {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		if !isBuiltinTypeName(typed.Name) && !resolves(typed) {
			return typed.Name, true
		}
	case *ast.ArrayType:
		return firstUnknownTypeName(typed.ElementType, resolves)
	case *ast.MapType:
		if name, found := firstUnknownTypeName(typed.KeyType, resolves); found {
			return name, true
		}
		return firstUnknownTypeName(typed.ValueType, resolves)
	case *ast.RefType:
		return firstUnknownTypeName(typed.ElementType, resolves)
	case *ast.NullableType:
		return firstUnknownTypeName(typed.ElementType, resolves)
	case *ast.ChanType:
		return firstUnknownTypeName(typed.ElementType, resolves)
	case *ast.FunctionType:
		for _, param := range typed.Params {
			if name, found := firstUnknownTypeName(param, resolves); found {
				return name, true
			}
		}
		if typed.Return != nil {
			return firstUnknownTypeName(typed.Return, resolves)
		}
	case *ast.GenericType:
		for _, arg := range typed.Args {
			if name, found := firstUnknownTypeName(arg, resolves); found {
				return name, true
			}
		}
	}
	return "", false
}

// displayStructName e o nome de um struct como o usuario o escreveu: uma
// instancia generica (`main::Caixa<int>`) aparece sem o qualificador interno
// de modulo (`Caixa<int>`), como em displayInstanceName.
func displayStructName(name string) string {
	if _, display, found := strings.Cut(name, "::"); found {
		return display
	}
	return name
}

// isQualifiedTypeName reconhece `ns.T` — exatamente um ponto separando dois
// identificadores. Nomes de instancia generica (`Caixa<int>`) e nomes
// simples nao contam.
func isQualifiedTypeName(name string) bool {
	ns, base, found := strings.Cut(name, ".")
	return found && isIdentifierName(ns) && isIdentifierName(base)
}

func isIdentifierName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', unicode.IsLetter(r):
		case unicode.IsDigit(r) && i > 0:
		default:
			return false
		}
	}
	return true
}
