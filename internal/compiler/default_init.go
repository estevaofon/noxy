package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// typeWithoutDefault responde qual (sub)tipo impede `let nome: tipo` sem
// inicializador (issue #61 item 1). chan e os tipos funcao NAO tem valor
// default: `= null` e rejeitado pelo checador (acceptsNull so admite
// any/null/ref/struct), e o null que emitDefaultInit fabricava para eles era
// a unica brecha — em chan o marcador de runtime estourava com "runtime value
// metadata conflicts with static context"; em func passava em silencio.
// Array dimensionado repete o default do ELEMENTO, entao herda a restricao;
// array dinamico e map comecam vazios e nao precisam de default de elemento.
// Devolve nil quando o tipo tem default.
func typeWithoutDefault(t ast.NoxyType) ast.NoxyType {
	switch typed := t.(type) {
	case *ast.ChanType, *ast.FunctionType:
		return t
	case *ast.PrimitiveType:
		if isBareFunctionType(typed) {
			return t
		}
	case *ast.ArrayType:
		if typed.Size > 0 {
			return typeWithoutDefault(typed.ElementType)
		}
	}
	return nil
}

// defaultInitError e o erro de `let nome: tipo` sem valor quando o tipo (ou
// o elemento do array dimensionado) nao tem default; nil quando tem.
func (c *Compiler) defaultInitError(name string, declared ast.NoxyType) error {
	culprit := typeWithoutDefault(declared)
	if culprit == nil {
		return nil
	}
	return fmt.Errorf("[line %d] variable '%s' needs an initializer: %s has no default value; hint: write 'let %s: %s = ...'",
		c.currentLine, name, culprit.String(), name, declared.String())
}
