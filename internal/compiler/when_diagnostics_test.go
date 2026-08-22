package compiler

import (
	"strings"
	"testing"
)

// `when` (CONCURRENCY.md / spec §7): cada case é `chan_recv(ch)`,
// `x = chan_recv(ch)` ou `chan_send(ch, v)`. Qualquer outra forma é erro de
// compilação com mensagem própria — nenhuma delas tinha teste.
func TestWhenCaseDiagnostics(t *testing.T) {
	prelude := "let c: chan int = make_chan(1)\n"
	cases := []struct{ name, source, want string }{
		{
			name:   "case with a non-call condition",
			source: "when\n    case 1 then\n        print(1)\nend\n",
			want:   "invalid case condition: expected chan_send(...) or chan_recv(...)",
		},
		{
			name:   "case calling something else",
			source: "when\n    case length(c) then\n        print(1)\nend\n",
			want:   "invalid case call: expected chan_send or chan_recv, got length",
		},
		{
			name:   "chan_recv with two arguments",
			source: "when\n    case chan_recv(c, 2) then\n        print(1)\nend\n",
			want:   "chan_recv expects 1 argument",
		},
		{
			name:   "chan_send with one argument",
			source: "when\n    case chan_send(c) then\n        print(1)\nend\n",
			want:   "chan_send expects 2 arguments",
		},
		{
			name:   "assigning the result of chan_send",
			source: "let v: int = 0\nwhen\n    case v = chan_send(c, 1) then\n        print(1)\nend\n",
			want:   "cannot assign result of chan_send",
		},
		{
			name:   "chan_send outside when with one argument",
			source: "chan_send(c)\n",
			want:   "chan_send expects 2 arguments",
		},
		{
			name:   "chan_recv outside when with two arguments",
			source: "let v: int = chan_recv(c, 1)\n",
			want:   "chan_recv expects 1 argument",
		},
		{
			name:   "addr with two arguments",
			source: "let x: int = 1\nlet s: string = addr(ref x, 1)\n",
			want:   "addr expects 1 argument",
		},
		{
			name:   "append to a non-array",
			source: "let n: int = 1\nappend(n, 2)\n",
			want:   "append expects an array, got int",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := New().Compile(parse(prelude + tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("source %q: error=%v, want %q", tc.source, err, tc.want)
			}
		})
	}
	// A forma correta compila: recv com e sem binding, send e default.
	valid := prelude + "let v: int = 0\nwhen\n    case v = chan_recv(c) then\n        print(v)\n    case chan_recv(c) then\n        print(1)\n    case chan_send(c, 1) then\n        print(2)\n    default\n        print(3)\nend\n"
	if _, _, err := New().Compile(parse(valid)); err != nil {
		t.Fatalf("when válido deveria compilar: %v", err)
	}
}
