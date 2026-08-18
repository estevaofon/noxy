package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// unify casa estruturalmente expected (um tipo de template, possivelmente
// contendo TypeParamType) contra actual (um tipo concreto observado num site
// de chamada), acumulando bindings de parâmetro de tipo em bindings.
//
// Regras (§7 da spec):
//   - TypeParamType em posição expected binda actual (uma cópia via
//     ast.CloneType, para não compartilhar nós entre o template e a
//     instância inferida).
//   - actual == ref é proibido como alvo de binding: erro "não pode ser um
//     tipo ref".
//   - actual any/null não contribui binding (não é erro; unify retorna nil
//     sem tocar em bindings).
//   - Se T já tem binding e o novo valor diverge (comparação por String()),
//     erro de conflito contendo "inferido como".
//   - expected concreto (sem TypeParamType) delega para comparação
//     estrutural por construtor; mismatch de construtor é erro contendo
//     "esperava".
//   - GenericType unifica Args ponto a ponto (mesmo Name e aridade).
//
// nil: expected ou actual nil é tratado conservadoramente — sem binding, sem
// erro. Ambos os lados nil não deveriam ocorrer em uso real (a spec exige
// tipos resolvidos antes de chamar unify); optamos por não travar aqui e
// deixar validações posteriores (Tasks 8/10/11) pegarem tipos ausentes com
// mensagens mais específicas ao contexto delas.
func unify(expected, actual ast.NoxyType, bindings map[string]ast.NoxyType) error {
	if expected == nil || actual == nil {
		return nil
	}

	if tp, ok := expected.(*ast.TypeParamType); ok {
		if _, isRef := actual.(*ast.RefType); isRef {
			return fmt.Errorf("%s não pode ser um tipo ref (tentativa de bindar %s)", tp.Name, actual.String())
		}
		if isAnyOrNull(actual) {
			return nil
		}
		if existing, ok := bindings[tp.Name]; ok {
			if existing.String() != actual.String() {
				return fmt.Errorf("%s inferido como %s e %s", tp.Name, existing.String(), actual.String())
			}
			return nil
		}
		bindings[tp.Name] = ast.CloneType(actual)
		return nil
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
		return unify(exp.ElementType, act.ElementType, bindings)

	case *ast.MapType:
		act, ok := actual.(*ast.MapType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		if err := unify(exp.KeyType, act.KeyType, bindings); err != nil {
			return err
		}
		return unify(exp.ValueType, act.ValueType, bindings)

	case *ast.RefType:
		act, ok := actual.(*ast.RefType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return unify(exp.ElementType, act.ElementType, bindings)

	case *ast.ChanType:
		act, ok := actual.(*ast.ChanType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		return unify(exp.ElementType, act.ElementType, bindings)

	case *ast.FunctionType:
		act, ok := actual.(*ast.FunctionType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		if len(exp.Params) != len(act.Params) {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		for i, p := range exp.Params {
			if err := unify(p, act.Params[i], bindings); err != nil {
				return err
			}
		}
		return unify(exp.Return, act.Return, bindings)

	case *ast.GenericType:
		act, ok := actual.(*ast.GenericType)
		if !ok {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		if exp.Name != act.Name || len(exp.Args) != len(act.Args) {
			return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
		}
		for i, a := range exp.Args {
			if err := unify(a, act.Args[i], bindings); err != nil {
				return err
			}
		}
		return nil

	default:
		return fmt.Errorf("esperava %s, encontrado %s", expected.String(), actual.String())
	}
}

// isAnyOrNull reporta se t é o primitivo "any" ou "null" — os únicos tipos
// concretos que, em posição actual, não contribuem binding para um
// TypeParamType (spec §7).
func isAnyOrNull(t ast.NoxyType) bool {
	pt, ok := t.(*ast.PrimitiveType)
	if !ok {
		return false
	}
	return pt.Name == "any" || pt.Name == "null"
}

// containsTypeParam reporta se t contém, em qualquer profundidade, um
// TypeParamType — ou seja, se t ainda é um tipo de template não instanciado.
// Exportado dentro do pacote (nome exato containsTypeParam) porque Tasks
// 8/10/11 consomem esta função para decidir quando delegar para unify em vez
// de comparação estrutural direta.
func containsTypeParam(t ast.NoxyType) bool {
	if t == nil {
		return false
	}
	switch n := t.(type) {
	case *ast.TypeParamType:
		return true
	case *ast.PrimitiveType:
		return false
	case *ast.ArrayType:
		return containsTypeParam(n.ElementType)
	case *ast.MapType:
		return containsTypeParam(n.KeyType) || containsTypeParam(n.ValueType)
	case *ast.RefType:
		return containsTypeParam(n.ElementType)
	case *ast.ChanType:
		return containsTypeParam(n.ElementType)
	case *ast.FunctionType:
		for _, p := range n.Params {
			if containsTypeParam(p) {
				return true
			}
		}
		return containsTypeParam(n.Return)
	case *ast.GenericType:
		for _, a := range n.Args {
			if containsTypeParam(a) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
