package compiler

import (
	"fmt"

	"github.com/estevaofon/noxy/internal/ast"
)

// Achado 4 do Noxy-Editor: `use strings select *` liga strings.contains ao
// nome `contains` e o builtin contains(arr, v) some — `contains(KEYWORDS,
// word)` vira "argument 1 to 'contains': expected string, got string[]",
// um erro verdadeiro que nao diz de onde veio esse `contains`. O hint
// abaixo e acrescentado aos erros de aridade e de tipo de argumento de uma
// chamada cujo callee e um nome (1) ligado por um `use m select *` deste
// compilador e (2) que e um builtin central (pureBuiltins) ou um nativo que
// o runtime registrou (knownGlobals, semeado pela CLI/VM). E um hint no erro, nao um aviso no `use`: quem importa
// strings inteiro e nunca chama contains com array nao tem nada a corrigir.
//
// So `strings.contains` e `http_client.delete` colidem com um nativo global
// na stdlib de hoje (builtins_registry_test.go x exports dos .nx), mas a
// regra e generica: qualquer pacote pode exportar um nome de builtin.

// noteWildcardImport registra que name entrou no escopo por `use module
// select *`. Copiado por compilador como namespaceImports: um `use` dentro
// de um corpo de funcao so afeta aquele corpo.
func (c *Compiler) noteWildcardImport(module, name string) {
	if c.wildcardImports == nil {
		c.wildcardImports = make(map[string]string)
	}
	c.wildcardImports[name] = module
}

// shadowedBuiltinHint devolve o hint (com a quebra de linha inicial) quando
// callee e um builtin sombreado por wildcard, ou "" caso contrario.
func (c *Compiler) shadowedBuiltinHint(callee ast.Expression) string {
	ident, ok := callee.(*ast.Identifier)
	if !ok {
		return ""
	}
	name := ident.Value
	module, imported := c.wildcardImports[name]
	if !imported || c.isShadowedByLocal(name) {
		return ""
	}
	// Builtin central que o compilador conhece por si (pureBuiltins) ou
	// nativo que o embutidor semeou (knownGlobals; nil fora da CLI/VM).
	_, core := pureBuiltins[name]
	_, native := c.knownGlobals[name]
	if !core && !native {
		return ""
	}
	return fmt.Sprintf(
		"\n  hint: '%s' here is %s.%s (imported by 'use %s select *'), which shadows the builtin '%s'; import %s by name ('use %s select ...') to keep the builtin",
		name, module, name, module, name, module, module,
	)
}
