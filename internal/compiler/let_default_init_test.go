package compiler

import (
	"testing"
)

// Issue #61 item 1: `let nome: tipo` sem `= valor` usa o default do tipo —
// 0, 0.0, false, "", b"", [], {} e null para struct/ref/any. `chan` e os
// tipos funcao NAO tem default: `let c: chan int = null` ja era rejeitado
// pelo checador (acceptsNull so admite any/null/ref/struct) e o `let` sem
// valor era a unica brecha que fabricava um null num tipo nao-nulavel — em
// chan o marcador de runtime estourava com "runtime value metadata conflicts
// with static context"; em func passava em silencio. Agora e erro de
// compilacao que nomeia o tipo sem default (inclusive o elemento de um array
// dimensionado, que emitDefaultInit preenche com o default do elemento).

func TestLetWithoutInitializerRejectsTypesWithoutDefault(t *testing.T) {
	cases := []struct{ name, src, variable, culprit, hint string }{
		{"chan", "let c: chan int\n", "c", "chan int", "let c: chan int = ..."},
		{"bare func", "let f: func\n", "f", "func", "let f: func = ..."},
		{"exact func", "let g: func(int) -> int\n", "g", "func(int) -> int", "let g: func(int) -> int = ..."},
		{"sized array of chan", "let cs: (chan int)[2]\n", "cs", "chan int", "let cs: (chan int)[] = ..."},
		{"sized array of func", "let fs: (func(int) -> int)[2]\n", "fs", "func(int) -> int", "let fs: (func(int) -> int)[] = ..."},
		// `chan int[]` e um CANAL de int[] (o prefixo captura o `[]`), nao
		// um array de canais — sem default como qualquer chan.
		{"chan of arrays", "let c: chan int[]\n", "c", "chan int[]", "let c: chan int[] = ..."},
		{"inside a function body", "func f()\n    let c: chan int\nend\n", "c", "chan int", "let c: chan int = ..."},
		// Spec §2.4 fase 2 (issue #105): struct e ref nus nunca sao null.
		{"struct", "struct P\n    x: int\nend\nlet p: P\n", "p", "P", "let p: P = ...' or declare it as 'P?"},
		{"ref", "let r: ref int\n", "r", "ref int", "let r: ref int = ...' or declare it as 'ref int?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileFunctionSource(t, tc.src)
			requireErrorMentions(t, err,
				"variable '"+tc.variable+"' needs an initializer",
				tc.culprit+" has no default value",
				"hint: write '"+tc.hint+"'")
			requireNotMentions(t, err, "type mismatch")
		})
	}
	_, err := compileFunctionSource(t, "let x: int = 1\nlet c: chan int\n")
	requireErrorMentions(t, err, "[line 2]")
}

func TestLetWithoutInitializerUsesTypeDefault(t *testing.T) {
	cases := []struct{ name, src string }{
		{"nullable struct is null", "struct P\n    x: int\nend\nlet p: P?\n"},
		{"nullable ref is null", "let r: ref int?\n"},
		{"any is null", "let a: any\n"},
		{"scalars", "let n: int\nlet f: float\nlet b: bool\nlet s: string\nlet by: bytes\n"},
		{"containers are empty", "let xs: int[]\nlet m: map[string, int]\n"},
		{"dynamic array of chan is empty", "let cs: (chan int)[]\n"},
		{"dynamic array of ref is empty", "let rs: (ref int)[]\n"},
		{"dynamic array of func is empty", "let fs: (func(int) -> int)[]\n"},
		{"sized array of a defaultable type", "let ns: int[3]\nlet ps: string[2]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := compileFunctionSource(t, tc.src); err != nil {
				t.Fatalf("%s deveria compilar: %v", tc.src, err)
			}
		})
	}
}
