package main

import (
	"fmt"
	"io/ioutil"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/vm"
	"os"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic:", r)
		}
	}()

	if len(os.Args) < 2 {
		fmt.Printf("Usage: noxy <script.nx>\n")
		// Fallback to inline verification if no args
		verify()
		return
	}

	filename := os.Args[1]
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file: %s\n", err)
		return
	}

	run(string(content))
}

func verify() {
	input := `
	func fib(n: int) -> int
		if n < 2 then
			return n
		end
		return fib(n - 1) + fib(n - 2)
	end
	let result: int = fib(10)
	print(result)
	`
	fmt.Printf("Verifying with input:\n%s\n", input)
	run(input)
}

func run(input string) {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		fmt.Printf("Parser errors:\n")
		for _, msg := range p.Errors() {
			fmt.Printf("\t%s\n", msg)
		}
		return
	}

	c := compiler.New()
	chunk, err := c.Compile(program)
	if err != nil {
		fmt.Printf("Compiler error: %s\n", err)
		return
	}

	fmt.Printf("Disassembly:\n")
	chunk.Disassemble("main")

	fmt.Printf("\nExecution:\n")
	machine := vm.New()
	if err := machine.Interpret(chunk); err != nil {
		fmt.Printf("Runtime error: %s\n", err)
	}
}
