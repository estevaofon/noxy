package compiler

import (
	"strings"
	"testing"
)

// Um erro de compilação dentro de QUALQUER sub-expressão chega ao usuário
// com a mensagem original — o compilador nunca engole, troca por outra ou
// entra em panic. O veneno `!0` é um erro estático garantido ("operand of '!'
// must be bool, got int"); cada linha o coloca em uma posição sintática
// distinta e confere que é essa mensagem que sobe. Cobre os ramos
// `if err != nil { return err }` de compiler.go que nenhum teste exercitava.
func TestCompileErrorInsideAnySubexpressionPropagatesUnchanged(t *testing.T) {
	const poison = "operand of '!' must be bool, got int"
	prelude := "struct P\n    x: int\nend\nfunc f(b: bool) -> bool\n    return b\nend\nlet arr: int[] = [1]\nlet m: map[string, int] = {\"a\": 1}\nlet p: P = P(1)\nlet x: int = 1\nlet r: ref int = ref x\nlet c: chan int = make_chan(1)\n"
	positions := []struct{ name, source string }{
		{"let value", "let v: bool = !0\n"},
		{"assignment value", "let v: bool = true\nv = !0\n"},
		{"deref assignment target", "*(!0) = 1\n"},
		{"deref assignment value", "*r = !0\n"},
		{"index assignment index", "arr[!0] = 1\n"},
		{"index assignment value", "arr[0] = !0\n"},
		{"member assignment value", "p.x = !0\n"},
		{"array literal element", "let v: bool[] = [!0]\n"},
		{"map literal key", "let v: map[bool, int] = {!0: 1}\n"},
		{"map literal value", "let v: map[string, bool] = {\"a\": !0}\n"},
		{"index expression base", "let v: int = (!0)[0]\n"},
		{"index expression index", "let v: int = arr[!0]\n"},
		{"and left", "let v: bool = !0 && true\n"},
		{"and right", "let v: bool = true && !0\n"},
		{"or left", "let v: bool = !0 || true\n"},
		{"or right", "let v: bool = true || !0\n"},
		{"infix left", "let v: int = !0 + 1\n"},
		{"infix right", "let v: int = 1 + !0\n"},
		{"fused compare left", "if !0 < 1 then\n    print(1)\nend\n"},
		{"fused compare right", "if 1 < !0 then\n    print(1)\nend\n"},
		{"prefix operand", "let v: int = -(!0)\n"},
		{"explicit deref operand", "let v: int = *(!0)\n"},
		{"zeros size", "let v: int[] = zeros(!0)\n"},
		{"if condition", "if !0 then\n    print(1)\nend\n"},
		{"if consequence", "if true then\n    let v: bool = !0\nend\n"},
		{"if alternative", "if true then\n    print(1)\nelse\n    let v: bool = !0\nend\n"},
		{"while condition", "while !0 do\n    print(1)\nend\n"},
		{"while body", "while false do\n    let v: bool = !0\nend\n"},
		{"for collection", "for e in !0 do\n    print(e)\nend\n"},
		{"for body", "for e in arr do\n    let v: bool = !0\nend\n"},
		{"return value", "func g() -> bool\n    return !0\nend\n"},
		{"call callee", "let v: bool = (!0)(1)\n"},
		{"call argument", "let v: bool = f(!0)\n"},
		{"builtin argument", "print(!0)\n"},
		{"append argument", "append(ref arr, !0)\n"},
		{"chan_send channel", "chan_send(!0, 1)\n"},
		{"chan_send value", "chan_send(c, !0)\n"},
		{"chan_recv argument", "let v: int = chan_recv(!0)\n"},
		{"addr argument", "let v: string = addr(!0)\n"},
		{"ref index", "let v: ref int = ref arr[!0]\n"},
		{"member access base", "let v: int = (!0).x\n"},
		{"defer argument", "func g() -> void\n    defer f(!0)\nend\n"},
		{"f-string expression", "let v: string = f\"{!0}\"\n"},
		{"function literal body", "let g: func() -> bool = func() -> bool\n    return !0\nend\n"},
		{"when case channel", "when\n    case chan_recv(!0) then\n        print(1)\n    default\n        print(2)\nend\n"},
		{"when case body", "when\n    case chan_recv(c) then\n        let v: bool = !0\n    default\n        print(2)\nend\n"},
		{"when send case value", "when\n    case chan_send(c, !0) then\n        print(1)\n    default\n        print(2)\nend\n"},
		{"struct constructor argument", "let q: P = P(!0)\n"},
	}
	for _, tc := range positions {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := New().Compile(parse(prelude + tc.source))
			if err == nil {
				t.Fatalf("source %q deveria falhar na compilação", tc.source)
			}
			if !strings.Contains(err.Error(), poison) {
				t.Fatalf("source %q: erro %q não carrega a mensagem original %q", tc.source, err.Error(), poison)
			}
		})
	}
}
