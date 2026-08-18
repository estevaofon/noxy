// Recursao pura: custo de chamada de funcao + aritmetica de inteiros.
//
// Um .go por diretorio: varios "package main" com func main() no mesmo
// diretorio quebram `go build ./...` no modulo do repo.
package main

import "fmt"

func fib(n int) int {
	if n <= 1 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	fmt.Printf("CHECKSUM:%d\n", fib(30))
}
