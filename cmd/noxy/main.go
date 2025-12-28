package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/vm"
	"os"
	"path/filepath"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic:", r)
		}
	}()

	// Parse flags
	showDisassembly := flag.Bool("disassembly", false, "Show bytecode disassembly")
	flag.Parse()

	// Remaining args are positional
	args := flag.Args()

	if len(args) < 1 {
		fmt.Printf("Usage: noxy [options] <script.nx>\n")
		// Fallback to inline verification if no args
		// verify() // Removing automatic verify on empty args to keep it clean, or keep it explicitly?
		// Let's keep it consistent.
		return
	}

	filename := args[0]
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file: %s\n", err)
		return
	}

	runWithConfig(string(content), getDir(filename), *showDisassembly)
}

func getDir(path string) string {
	return filepath.Dir(path)
}

func verify() {
	input := `
	func main()
		struct Point
			x: int
			y: int
		end

		print(111)
		let p1: Point = Point(1, 2)
		print(222)
		print(p1)
		
		print(333)
		let points: Point[] = [p1, Point(3, 4)]
		print(444)
		
		print(555)
		print(points)
		print(666)
		print(points[0])
	end
	main()
	`
	fmt.Printf("Verifying with input:\n%s\n", input)
	runWithConfig(input, ".", true)
}

func runWithConfig(input string, rootPath string, showDisasm bool) {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		fmt.Printf("Parser errors:\n")
		for _, msg := range p.Errors() {
			fmt.Printf("\t%s\n", msg)
		}
		os.Exit(1)
	}

	c := compiler.New()
	chunk, err := c.Compile(program)
	if err != nil {
		fmt.Printf("Compiler error: %s\n", err)
		os.Exit(1)
	}

	if showDisasm {
		fmt.Printf("Disassembly:\n")
		chunk.DisassembleAll("main")
		fmt.Printf("\nExecution:\n")
	}

	machine := vm.NewWithConfig(vm.VMConfig{RootPath: rootPath})
	if err := machine.Interpret(chunk); err != nil {
		fmt.Printf("Runtime error: %s\n", err)
		os.Exit(1)
	}
}
