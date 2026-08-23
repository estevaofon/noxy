package compiler

import "noxy-vm/internal/ast"

// Tipo de retorno estatico dos builtins CENTRAIS (os que existem sem `use`:
// registrados pela VM em defineNatives, sem wrapper tipado na stdlib). Ate a
// issue #41 eles compilavam como global desconhecido — tipo nil, que o
// checador aceita em qualquer posicao e so o runtime confere. A inferencia
// de `let` (`let n = length(xs)`) precisa do tipo estatico, e a checagem
// ganha junto em toda posicao: `let s: string = length(xs)` passa a ser erro
// de compilacao, nao de runtime.
//
// So entram aqui builtins cujo tipo de retorno NAO depende dos argumentos
// (ou depende de forma trivial, como keys/slice, tratados em
// builtinReturnType). Os que devolvem `any` (json_parse, task_await...) ou
// estruturas da stdlib ficam de fora: continuam nil, e um `let` sem
// anotacao sobre eles pede a anotacao.
//
// Um `func length(...)` declarado pelo programa sombreia o builtin (spec
// §10, regra do `range`): o chamador so consulta esta tabela quando o nome
// NAO resolve para local, upvalue ou global declarado.
var coreBuiltinReturnTypes = map[string]string{
	"length":     "int",
	"to_str":     "string",
	"to_int":     "int",
	"to_float":   "float",
	"to_bytes":   "bytes",
	"type":       "string",
	"input":      "string",
	"fmt":        "string",
	"hex":        "string",
	"hex_encode": "string",
	"hex_decode": "bytes",
	"ord":        "int",
	"contains":   "bool",
	"has_key":    "bool",
	"json_dumps": "string",
}

// builtinReturnType devolve o tipo estatico de `name(args...)` para um
// builtin central, ou nil quando o nome nao e um builtin tipado aqui (ou o
// tipo depende de um argumento cujo tipo estatico nao se conhece).
func builtinReturnType(name string, argTypes []ast.NoxyType) ast.NoxyType {
	if primitive, ok := coreBuiltinReturnTypes[name]; ok {
		return &ast.PrimitiveType{Name: primitive}
	}
	switch name {
	case "keys":
		// keys(map[K, V]) -> K[]
		if len(argTypes) == 1 {
			if mapType, ok := argTypes[0].(*ast.MapType); ok && mapType.KeyType != nil {
				return &ast.ArrayType{ElementType: mapType.KeyType}
			}
		}
	case "slice":
		// slice(T[], ...) -> T[]; slice(string, ...) -> string
		if len(argTypes) >= 1 {
			switch typed := argTypes[0].(type) {
			case *ast.ArrayType:
				if typed.ElementType != nil {
					return &ast.ArrayType{ElementType: typed.ElementType}
				}
			case *ast.PrimitiveType:
				if typed.Name == "string" {
					return typed
				}
			}
		}
	}
	return nil
}
