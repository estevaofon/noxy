package vm

import (
	"strings"
	"testing"
)

// Issue #126, revisao adversarial da tipagem por namespace: um argumento
// `ref x` cujo ALVO tem tipo desconhecido para o programa (o campo e de um
// struct que o programa nao consegue nomear, ou de uma instancia generica)
// nao prova o modo/alvo em compilacao. Antes da correcao, `modesProven`
// continuava true (a assinatura da funcao era exata), o call site emitia
// OP_CALL_STATIC e pulava validateParameterModes/validateRefTargets — a
// escrita entrava no campo errado e o programa seguia com lixo.
//
// Issue #133: um struct que o programa nao consegue nomear saiu da classe
// dinamica — programViewType sempre traduz um struct (com Decl), entao o
// campo `h.b` agora e `base.B` tipado e o mismatch e pego em COMPILACAO, nao
// em runtime. So a instancia generica (Caixa<int>, sem grafia possivel na
// visao do programa) continua sem tipo e cai para a checagem em runtime.

const refTargetBaseModule = `struct B
    n: int
end
`

const refTargetMidModule = `use base
struct Holder
    b: base.B
    tag: string
end
func mk() -> Holder
    return Holder(base.B(7), "hi")
end
func setstr(s: ref string) -> void
    *s = "PWNED"
end
func read(h: Holder) -> int
    return h.b.n
end
func setb(b: ref base.B) -> void
    *b = base.B(99)
end
`

func TestNamespaceRefArgumentWithUnnameableTargetIsCheckedAtCompileTime(t *testing.T) {
	// `h.b` e um `base.B`: com a #133, o campo e tipado (mesmo sem grafia no
	// programa) e o mismatch de `ref` e recusado em COMPILACAO.
	root := writeModuleFiles(t, map[string]string{
		"base.nx": refTargetBaseModule,
		"mid.nx":  refTargetMidModule,
	})
	_, err := runModuleProgram(t, root, `use mid
let h: mid.Holder = mid.mk()
mid.setstr(ref h.b)
`)
	if err == nil || !strings.Contains(err.Error(), "argument 1 to 'mid.setstr': expected ref string, got ref base.B") {
		t.Fatalf("error=%v, want compile-time ref target mismatch", err)
	}
}

func TestSelectRefArgumentWithUnnameableTargetIsCheckedAtCompileTime(t *testing.T) {
	// Mesmo programa pela forma `select`: este buraco JA EXISTIA antes da
	// tipagem por namespace (select sempre deu assinatura exata), e a mesma
	// correcao no ramo `ref` de compileCallExpression o fecha — agora tambem
	// em compilacao.
	root := writeModuleFiles(t, map[string]string{
		"base.nx": refTargetBaseModule,
		"mid.nx":  refTargetMidModule,
	})
	_, err := runModuleProgram(t, root, `use mid select setstr, mk, Holder
let h: Holder = mk()
setstr(ref h.b)
`)
	if err == nil || !strings.Contains(err.Error(), "argument 1 to 'setstr': expected ref string, got ref base.B") {
		t.Fatalf("error=%v, want compile-time ref target mismatch", err)
	}
}

func TestNamespaceRefArgumentIntoGenericInstanceFieldIsCheckedAtRuntime(t *testing.T) {
	// Sabor generico: o campo `c` e uma instancia de `Caixa<int>` do modulo,
	// tipo que o programa nao nomeia — `ref w.c` tem alvo desconhecido e a
	// escrita de int num struct precisa ser recusada em runtime.
	root := writeModuleFiles(t, map[string]string{
		"g.nx": `struct Caixa<T>
    v: T
end
struct Wrap
    c: Caixa<int>
    tag: string
end
func mk() -> Wrap
    let cx: Caixa<int> = Caixa(1)
    return Wrap(cx, "hi")
end
func bump(n: ref int) -> void
    *n = *n + 1
end
`,
	})
	_, err := runModuleProgram(t, root, `use g
let w: g.Wrap = g.mk()
g.bump(ref w.c)
`)
	if err == nil {
		t.Fatalf("expected runtime error, got none")
	}
	if !strings.Contains(err.Error(), "function 'bump' argument 1: expected ref int, got ref Caixa<int>") {
		t.Fatalf("expected error mentioning ref int mismatch, got %v", err)
	}
}

func TestNamespaceRefArgumentWithNameableTargetStillMutates(t *testing.T) {
	// Controle positivo: `h.tag` e `string`, um tipo que o programa nomeia
	// — o alvo e conhecido, o modo continua provado e a mutacao acontece.
	root := writeModuleFiles(t, map[string]string{
		"base.nx": refTargetBaseModule,
		"mid.nx":  refTargetMidModule,
	})
	reported, err := runModuleProgram(t, root, `use mid
let h: mid.Holder = mid.mk()
mid.setstr(ref h.tag)
test_report(h.tag)
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := reported.String(); got != "PWNED" {
		t.Fatalf("expected mutated tag \"PWNED\", got %q", got)
	}
}

func TestNamespaceRefArgumentWithUnnameableButCorrectTargetStillMutates(t *testing.T) {
	// Controle negativo (sem sobre-rejeicao): o alvo `h.b` continua sem tipo
	// estatico para o programa, mas AGORA o parametro tambem e `ref base.B`
	// — cair para OP_CALL nao pode virar erro: validateRefTargets aprova o
	// alvo e a mutacao acontece.
	root := writeModuleFiles(t, map[string]string{
		"base.nx": refTargetBaseModule,
		"mid.nx":  refTargetMidModule,
	})
	reported, err := runModuleProgram(t, root, `use mid
let h: mid.Holder = mid.mk()
mid.setb(ref h.b)
test_report(mid.read(h))
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := reported.String(); got != "99" {
		t.Fatalf("expected mutated field 99, got %q", got)
	}
}

// Issue #126, mesma revisao adversarial pelo outro lado da chamada: um
// argumento POR VALOR cujo tipo estatico o programa nao consegue nomear
// (aqui o retorno `ref base.B` de `mid.getref()`, que programView zera
// inteiro por ser inominavel apos `use mid`) tambem nao prova o MODO. Antes
// da correcao, `argType == nil` passava por areStrictTypesCompatible sem
// reclamar, `modesProven` continuava true, o call site emitia OP_CALL_STATIC
// e validateParameterModes nunca rodava: a funcao recebia uma referencia
// onde esperava um int e seguia em silencio.

const byValueUnknownMidModule = `use base
let origin: base.B = base.B(1)
func getref() -> ref base.B
    return ref origin
end
func takeint(n: int) -> int
    return n + 1
end
func takestring(s: string) -> string
    return s
end
`

func TestNamespaceByValueArgumentWithUnnameableTypeIsCheckedAtCompileTime(t *testing.T) {
	// Issue #133: `mid.getref()` devolve `ref base.B`, tipo agora nomeavel
	// (base.B com Decl) mesmo sem grafia no programa — o mismatch de modo
	// (por valor esperado, ref recebido) e pego em compilacao.
	root := writeModuleFiles(t, map[string]string{
		"base.nx": refTargetBaseModule,
		"mid.nx":  byValueUnknownMidModule,
	})
	_, err := runModuleProgram(t, root, `use mid
test_report(mid.takeint(mid.getref()))
`)
	if err == nil || !strings.Contains(err.Error(), "argument 1 to 'mid.takeint': expected int, got ref base.B") {
		t.Fatalf("error=%v, want compile-time mode mismatch", err)
	}
}

func TestNamespaceByValueStringArgumentWithUnnameableTypeIsCheckedAtCompileTime(t *testing.T) {
	// Mesmo buraco com parametro `string`: a mensagem de compilacao nomeia o
	// parametro esperado, entao o sabor confirma que a validacao roda para
	// qualquer tipo, nao so int.
	root := writeModuleFiles(t, map[string]string{
		"base.nx": refTargetBaseModule,
		"mid.nx":  byValueUnknownMidModule,
	})
	_, err := runModuleProgram(t, root, `use mid
test_report(mid.takestring(mid.getref()))
`)
	if err == nil || !strings.Contains(err.Error(), "argument 1 to 'mid.takestring': expected string, got ref base.B") {
		t.Fatalf("error=%v, want compile-time mode mismatch", err)
	}
}
