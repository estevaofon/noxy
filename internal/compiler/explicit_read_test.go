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

func TestInfixOperandRefIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let y: int = r + 1`, "operand of '+' cannot be ref int: a ref is never read implicitly", "hint: use '*r' to read the referenced value")
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let y: int = 1 + r`, "operand of '+' cannot be ref int", "hint: use '*r'")
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let b: bool = r < 5`, "operand of '<' cannot be ref int", "hint: use '*r'")
}

func TestUnaryOperandRefIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let y: int = -r`, "operand of '-' cannot be ref int", "hint: use '*r'")
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
let g: bool = !rb`, "operand of '!' cannot be ref bool", "hint: use '*rb'")
}

func TestLogicalOperandRefIsError(t *testing.T) {
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
let g: bool = rb && true`, "operand of '&&' cannot be ref bool", "hint: use '*rb'")
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
let g: bool = false || rb`, "operand of '||' cannot be ref bool", "hint: use '*rb'")
}

func TestConditionRefIsError(t *testing.T) {
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
if rb then
    print(1)
end`, "condition cannot be ref bool: a ref is never read implicitly", "hint: use '*rb'")
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
while rb do
    f = false
end`, "condition cannot be ref bool", "hint: use '*rb'")
}

func TestEqualityBetweenRefsStillCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let r: ref int = ref x
let r2: ref int = ref x
let same: bool = r == r2
let isnull: bool = r == null
let val: bool = *r == 10`)
}

func TestExplicitReadsInOperatorsCompile(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let f: bool = true
let r: ref int = ref x
let rb: ref bool = ref f
let a: int = *r + 1
let b: int = -*r
let c: bool = !*rb && *rb
if *rb then
    x = 1
end
while *rb do
    f = false
end`)
}
