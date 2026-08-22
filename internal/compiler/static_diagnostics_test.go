package compiler

import (
	"strings"
	"testing"
)

// Diagnósticos estáticos que o perfil de cobertura mostrou sem nenhum teste:
// cada linha é um erro de compilação que um programa real pode provocar, com
// o texto exato que o usuário vê (spec §7, §8, §11, CONCURRENCY.md). Fixar o
// texto evita que uma refatoração silenciosamente troque a mensagem ou, pior,
// deixe o programa passar para o runtime.
func TestStaticDiagnosticsAreReportedWithTheirExactMessage(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{
			name:   "&& with non-bool operand",
			source: "if 1 && true then print(1) end\n",
			want:   "logical operators require boolean operands, got int and bool",
		},
		{
			name:   "deref assignment value type",
			source: "let x: int = 1\nlet r: ref int = ref x\n*r = \"s\"\n",
			want:   "type mismatch in assignment: expected int, got string",
		},
		{
			name:   "deref assignment to non-ref",
			source: "let x: int = 1\n*x = 2\n",
			want:   "cannot dereference non-reference type int in assignment",
		},
		{
			name:   "array index assignment with string index",
			source: "let arr: int[] = [1]\narr[\"a\"] = 1\n",
			want:   "array index must be int, got string",
		},
		{
			name:   "map index assignment with wrong key type",
			source: "let m: map[string, int] = {\"a\": 1}\nm[1] = 2\n",
			want:   "type mismatch in map key: expected string, got int",
		},
		{
			name:   "index assignment on string",
			source: "let s: string = \"x\"\ns[0] = \"y\"\n",
			want:   "index assignment on non-array/map type: string",
		},
		{
			name:   "map literal with mixed key types",
			source: "let m: map[string, int] = {\"a\": 1, 2: 3}\n",
			want:   "mixed key types in map",
		},
		{
			name:   "chan_send first argument not a channel",
			source: "chan_send(1, 2)\n",
			want:   "first argument to chan_send must be a channel, got int",
		},
		{
			name:   "chan_send value type mismatch",
			source: "let c: chan int = make_chan(1)\nchan_send(c, \"s\")\n",
			want:   "cannot send string to chan int",
		},
		{
			name:   "chan_recv argument not a channel",
			source: "chan_recv(1)\n",
			want:   "argument to chan_recv must be a channel, got int",
		},
		{
			name:   "generic struct declared inside a function",
			source: "func f()\n    struct Caixa<T>\n        v: T\n    end\nend\n",
			want:   "top level",
		},
		{
			name:   "mixed arithmetic is float",
			source: "let n: int = 1 + 2.5\n",
			want:   "got float",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := New().Compile(parse(tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("source %q: error=%v, want it to contain %q", tc.source, err, tc.want)
			}
		})
	}
}

// O contraponto dos erros acima: as formas corretas compilam.
func TestStaticDiagnosticsCounterpartsCompile(t *testing.T) {
	for _, source := range []string{
		"let a: bool = true\nif a && true then print(1) end\n",
		"let x: int = 1\nlet r: ref int = ref x\n*r = 2\n",
		"let arr: int[] = [1]\narr[0] = 1\n",
		"let m: map[string, int] = {\"a\": 1}\nm[\"b\"] = 2\n",
		"let c: chan int = make_chan(1)\nchan_send(c, 1)\nlet v: int = chan_recv(c)\n",
		"struct Caixa<T>\n    v: T\nend\nlet c: Caixa<int> = Caixa(1)\n",
		"let f: float = 1 + 2.5\n",
	} {
		if _, _, err := New().Compile(parse(source)); err != nil {
			t.Fatalf("source %q deveria compilar: %v", source, err)
		}
	}
}
