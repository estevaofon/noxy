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
	case *ast.FunctionType:
		// Callable signatures carry their own immutable runtime schema. Channels
		// named by that signature are not values embedded in the callable object.
		return false
	case *ast.PrimitiveType:
		definition := c.lookupStructFrom(origin, typed.Name)
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
		definition := c.lookupStructFrom(origin, typed.Name)
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
			_, info.ParamIsRef[i] = param.(*ast.RefType)
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

// unresolvedQualifiedFieldError explica, como erro de COMPILACAO, por que o
// ConstructorType de decl ficou incompleto QUANDO a causa e um campo tipado
// com nome qualificado (`ns.T`) que nao resolve — forma nova, sem legado a
// preservar, e sem programa valido possivel (nenhuma chamada ao construtor
// funcionaria). Um nome SIMPLES desconhecido num campo continua tolerado
// aqui como sempre foi (o construtor falha em runtime); nesse caso devolve
// nil.
func (c *Compiler) unresolvedQualifiedFieldError(decl *ast.StructStatement) error {
	for _, field := range decl.FieldsList {
		name, found := firstUnresolvedQualifiedName(field.Type, func(candidate string) bool {
			return c.lookupStructFrom("", candidate) != nil
		})
		if !found {
			continue
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
		return fmt.Errorf("[line %d] struct '%s' field '%s': cannot resolve type '%s': %s\n  hint: %s",
			decl.Token.Line, decl.Name, field.Name, name, reason, hint)
	}
	return nil
}

// firstUnresolvedQualifiedName procura em t (inclusive dentro de array, map,
// ref, chan, assinatura e argumentos genericos) o primeiro nome de tipo com a
// forma `ns.T` que resolves rejeita.
func firstUnresolvedQualifiedName(t ast.NoxyType, resolves func(string) bool) (string, bool) {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		if isQualifiedTypeName(typed.Name) && !resolves(typed.Name) {
			return typed.Name, true
		}
	case *ast.ArrayType:
		return firstUnresolvedQualifiedName(typed.ElementType, resolves)
	case *ast.MapType:
		if name, found := firstUnresolvedQualifiedName(typed.KeyType, resolves); found {
			return name, true
		}
		return firstUnresolvedQualifiedName(typed.ValueType, resolves)
	case *ast.RefType:
		return firstUnresolvedQualifiedName(typed.ElementType, resolves)
	case *ast.ChanType:
		return firstUnresolvedQualifiedName(typed.ElementType, resolves)
	case *ast.FunctionType:
		for _, param := range typed.Params {
			if name, found := firstUnresolvedQualifiedName(param, resolves); found {
				return name, true
			}
		}
		if typed.Return != nil {
			return firstUnresolvedQualifiedName(typed.Return, resolves)
		}
	case *ast.GenericType:
		for _, arg := range typed.Args {
			if name, found := firstUnresolvedQualifiedName(arg, resolves); found {
				return name, true
			}
		}
	}
	return "", false
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
