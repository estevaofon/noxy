// Loop apertado: despacho e aritmetica inteira, sem alocacao.
package main

import "fmt"

func main() {
	i, s := 0, 0
	for i < 3000000 {
		s = (s + i*3) % 1000003
		i = i + 1
	}
	fmt.Printf("CHECKSUM:%d\n", s)
}
