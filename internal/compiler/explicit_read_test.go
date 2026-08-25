package compiler

// R2 (spec 2026-08-24-explicit-ref §3): um `ref T` nunca e lido
// implicitamente. Cada teste fixa o erro E o hint em uma posicao.
//
// requireCompileError e requireCompiles ja existem no pacote (definidos em
// let_inference_test.go e let_redeclaration_test.go, respectivamente) e sao
// reutilizados aqui.

import (
	"testing"
)

func TestLetAnnotatedFromRefIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let m: int = r`, "type mismatch in 'm' declaration: expected int, got ref int", "hint: use '*r' to read the referenced value")
}

func TestLetAnnotatedFromDerefCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let r: ref int = ref x
let m: int = *r`)
}

func TestLetInferredFromRefKeepsRefType(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let v = r
let n: int = v`, "expected int, got ref int", "hint: use '*v'")
}

func TestDerefAssignmentFromRefRHSIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
let s: ref int = ref z
*r = s`, "type mismatch in assignment: expected int, got ref int", "hint: use '*s' to read the referenced value")
}

func TestDerefAssignmentFromRefPrefixIsRebindOrValueHint(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
*r = ref z`, "cannot assign ref int to int through '*r'", "hint: use 'r = ref z' to rebind the reference, or '*r = z' to write the value")
}

func TestDerefAssignmentFromDerefCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
let s: ref int = ref z
*r = *s`)
}
