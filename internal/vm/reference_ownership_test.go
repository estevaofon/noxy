package vm

import (
	"testing"
)

func TestImportedFunctionUsesCallerGlobalReferenceOwner(t *testing.T) {
	got := runTypedFunctionProgram(t, `
use rand
let rng: rand.RandomGenerator = rand.RandomGenerator(1, 2, 3, 100)
rand.rng.state = 10
rand.new_random(ref rng)
test_report(rng.state * 100 + rand.rng.state)`)
	testExpectedObject(t, 510, got)
}
