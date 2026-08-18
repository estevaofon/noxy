package vm

// E2E de genericos por monomorfizacao (spec §4/§5): o programa inteiro passa
// pelo two-pass do compilador e roda na VM sem qualquer mudanca de runtime.

import "testing"

func TestGenericFunctionEndToEnd(t *testing.T) {
	got := captureVMSource(t, `
func first<T>(arr: T[]) -> T
    return arr[0]
end
let nums: int[] = [7, 8]
test_report(first(nums))
`)
	expectInt(t, got, 7, "first<int> deve devolver 7")
}

func TestGenericTwoInstantiations(t *testing.T) {
	got := captureVMSource(t, `
func size<T>(arr: T[]) -> int
    return length(arr)
end
let nums: int[] = [1, 2, 3]
let names: string[] = ["a"]
test_report(size(nums) + size(names))
`)
	expectInt(t, got, 4, "size<int> + size<string>")
}

func TestGenericRecursion(t *testing.T) {
	got := captureVMSource(t, `
func soma_rec<T>(arr: T[], i: int) -> int
    if i >= length(arr) then
        return 0
    end
    return 1 + soma_rec(arr, i + 1)
end
let xs: string[] = ["a", "b", "c"]
test_report(soma_rec(xs, 0))
`)
	expectInt(t, got, 3, "recursao generica")
}

func TestGenericCallsGeneric(t *testing.T) {
	got := captureVMSource(t, `
func first<T>(arr: T[]) -> T
    return arr[0]
end
func head_twice<T>(a: T[], b: T[]) -> T
    let x: T = first(a)
    return first(b)
end
let xs: int[] = [1]
let ys: int[] = [2]
test_report(head_twice(xs, ys))
`)
	expectInt(t, got, 2, "cascata generico->generico")
}
