package compiler

import "testing"

// Achado 6 do Noxy-Editor: `chan_recv(m.inbox)` num corpo de funcao, com
// `use src.server as server` apontando para um modulo que NAO carrega,
// derrubava o compilador com nil pointer dereference — o membro de um
// namespace que nao resolve tem tipo nil (dinamico), e chan_recv chamava
// String() nele sem checar. chan_send ja tratava o nil; chan_recv passa a
// aceitar o tipo desconhecido como dinamico (resultado `any`), e o runtime
// e quem reporta o import que falhou.
func TestChanRecvOnUnknownTypedArgumentDoesNotPanic(t *testing.T) {
	src := "use src.server as server\n" +
		"func owner() -> void\n" +
		"    let r: any = chan_recv(server.inbox)\n" +
		"    print(r)\n" +
		"end\n"
	if _, err := compileNamedSource(t, "protocol.nx", src); err != nil {
		t.Fatalf("compile error: %v", err)
	}
}
