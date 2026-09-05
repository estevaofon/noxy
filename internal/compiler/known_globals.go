package compiler

import (
	"fmt"

	"github.com/estevaofon/noxy/internal/ast"
)

// Issue #47 parte 3: um global inexistente e erro de COMPILACAO, nao um
// `undefined global variable` que so explode quando (e se) a linha rodar.
//
// O compilador nao enxerga os nativos da VM (print, length, io_*, ...), as
// extensoes WASM nem os plugins — sao registrados em runtime no ambiente
// raiz. Entao o check e ativado pelo EMBUTIDOR, que semeia os nomes que
// garante existir: a CLI e o carregador de modulos da VM passam
// vm.GlobalNames() (+ PluginNativeNames). compiler.New() puro continua
// permissivo (knownGlobals nil), preservando o contrato de quem embute o
// compilador sem uma VM ao lado.

// SetKnownGlobals arma o check de global inexistente com o conjunto de
// nomes que o runtime garante existir.
func (c *Compiler) SetKnownGlobals(names []string) {
	c.knownGlobals = make(map[string]struct{}, len(names))
	for _, name := range names {
		c.knownGlobals[name] = struct{}{}
	}
}

// globalIsKnown diz se `name` resolve em compilacao: declarado neste
// Program (globals sequenciais ou programBindings, que carrega as
// declaracoes de topo para referencias adiantadas em corpos de funcao),
// importado como namespace, ou semeado pelo embutidor. nil = check
// desligado.
func (c *Compiler) globalIsKnown(name string) bool {
	if c.knownGlobals == nil {
		return true
	}
	if _, ok := c.globals[name]; ok {
		return true
	}
	if _, ok := c.programBindings[name]; ok {
		return true
	}
	if _, ok := c.namespaceImports[name]; ok {
		return true
	}
	if _, ok := c.knownGlobals[name]; ok {
		return true
	}
	return false
}

func (c *Compiler) undefinedGlobalError(name string) error {
	return fmt.Errorf("[line %d] undefined global '%s'\n  hint: declare it with 'let %s = ...' or check the spelling", c.currentLine, name, name)
}

func (c *Compiler) undeclaredAssignmentError(name string) error {
	return fmt.Errorf("[line %d] cannot assign to undeclared name '%s'\n  hint: declare it with 'let %s = ...'", c.currentLine, name, name)
}

// PluginNativeNames devolve os nativos que sys_load_plugin("<nome>", ...)
// registra em runtime ("<nome>_request"), lendo o literal no AST — o unico
// caso em que um global nasce depois da compilacao do proprio arquivo que
// o referencia (o wrapper do plugin chama o nativo nas funcoes seguintes).
func PluginNativeNames(program *ast.Program) []string {
	var names []string
	ast.Inspect(program, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpression)
		if !ok {
			return true
		}
		ident, ok := call.Function.(*ast.Identifier)
		if !ok || ident.Value != "sys_load_plugin" || len(call.Arguments) == 0 {
			return true
		}
		if lit, ok := call.Arguments[0].(*ast.StringLiteral); ok {
			names = append(names, lit.Value+"_request")
		}
		return true
	})
	return names
}
