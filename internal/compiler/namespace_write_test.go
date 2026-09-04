package compiler

import "testing"

// Issue #133 item 1: escrita pelo namespace e legal, tipada com o tipo
// declarado pelo membro, e cai no store vivo do modulo. A regra
// "module variables are read-only outside the module" (0.11.0) sai.

const stateModule = `struct P
    x: int
end
let origin: P = P(0)
let count: int = 0
let xs: int[] = [1, 2]
let name = "a"
let link: ref int = ref count
func read_count() -> int
    return count
end
`

func stateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeModuleFile(t, root, "st.nx", stateModule)
	return root
}

func TestNamespaceWriteCompilesWithDeclaredType(t *testing.T) {
	requireNoError(t, compileSourceAtRoot(t, stateRoot(t), "use st\nst.count = 9\nst.name = \"b\"\nst.origin = st.P(3)\nst.xs = [4]\n"))
}

func TestNamespaceWriteRejectsTypeMismatch(t *testing.T) {
	err := compileSourceAtRoot(t, stateRoot(t), "use st\nst.count = \"a\"\n")
	requireErrorMentions(t, err, "type mismatch in assignment to 'st.count': expected int, got string")
	err = compileSourceAtRoot(t, stateRoot(t), "use st\nst.origin = 1\n")
	requireErrorMentions(t, err, "expected st.P, got int")
}

func TestNamespaceWriteRejectsMissingMemberFunctionAndStruct(t *testing.T) {
	err := compileSourceAtRoot(t, stateRoot(t), "use st\nst.nope = 1\n")
	requireErrorMentions(t, err, "'st' has no member 'nope'")
	err = compileSourceAtRoot(t, stateRoot(t), "use st\nst.read_count = 1\n")
	requireErrorMentions(t, err, "cannot assign to 'st.read_count': it is a function", "only module variables ('let') can be assigned")
	err = compileSourceAtRoot(t, stateRoot(t), "use st\nst.P = 1\n")
	requireErrorMentions(t, err, "cannot assign to 'st.P': it is a struct")
	requireErrorLacks(t, err, "read-only outside the module")
}

func TestNamespaceWriteToRefMemberOnlyRebinds(t *testing.T) {
	requireNoError(t, compileSourceAtRoot(t, stateRoot(t), "use st\nlet other: int = 1\nst.link = ref other\n"))
	err := compileSourceAtRoot(t, stateRoot(t), "use st\nst.link = 5\n")
	requireErrorMentions(t, err, "st.link")
	requireErrorLacks(t, err, "has no member")
}

func TestNamespaceNestedWritesAreTyped(t *testing.T) {
	root := stateRoot(t)
	requireNoError(t, compileSourceAtRoot(t, root, "use st\nst.origin.x = 5\nst.xs[0] = 7\n"))
	err := compileSourceAtRoot(t, root, "use st\nst.origin.x = \"a\"\n")
	requireErrorMentions(t, err, "type mismatch in field assignment: expected int, got string")
	err = compileSourceAtRoot(t, root, "use st\nst.xs[0] = \"a\"\n")
	requireErrorMentions(t, err, "type mismatch in array assignment: expected int, got string")
}

func TestNamespaceRefAndBuiltinsAreTyped(t *testing.T) {
	root := stateRoot(t)
	requireNoError(t, compileSourceAtRoot(t, root, "use st\nlet r = ref st.count\n*r = 3\nappend(ref st.xs, 3)\nlet last: int = pop(ref st.xs)\n"))
	err := compileSourceAtRoot(t, root, "use st\nlet r: ref string = ref st.count\n")
	requireErrorMentions(t, err, "expected ref string, got ref int")
	err = compileSourceAtRoot(t, root, "use st\nappend(ref st.xs, \"a\")\n")
	requireErrorMentions(t, err, "expected int, got string")
}

func TestNamespaceWriteWithShadowedAliasIsAFieldWrite(t *testing.T) {
	// `st` local sombreia o alias: a atribuicao e num campo do struct local.
	err := compileSourceAtRoot(t, stateRoot(t), `use st
struct Box
    count: string
end
func f() -> int
    let st: Box = Box("x")
    st.count = 1
    return 0
end
`)
	requireErrorMentions(t, err, "type mismatch in field assignment: expected string, got int")
}

func TestNamespaceWriteToUnloadableModuleIsDynamic(t *testing.T) {
	// Modulo inexistente nao e erro de compilacao (cf.
	// TestNamespaceMemberOfUnloadableModuleStaysDynamic); a escrita fica
	// dinamica, sem erro novo.
	requireNoError(t, compileSourceAtRoot(t, stateRoot(t), "use nope\nnope.x = 1\n"))
}
