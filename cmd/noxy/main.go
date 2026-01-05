package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/token"
	"noxy-vm/internal/vm"
	"os"
	"path/filepath"
	"strings"
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
		startREPL(*showDisassembly)
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

func startREPL(showDisasm bool) {
	fmt.Println("Noxy REPL v0.1")
	fmt.Println("Type 'exit' to quit.")

	// Shared VM for persistence
	machine := vm.NewWithConfig(vm.VMConfig{RootPath: "."})
	scanner := bufio.NewScanner(os.Stdin)

	// Compiler needs to persist scope?
	// Currently compiler.New() creates fresh scope.
	// But VM globals are persistent.
	// If we want `let x = 10` to work, the compiler needs to know 'x' is defined if we were in same scope.
	// However, Noxy 'let' in top-level creates a GLOBAL.
	// The compiler treats top-level vars as globals.
	// When we compile a new line "print(x)", the compiler checks 'x'.
	// If 'x' is not in locals (scopeDepth=0 always for top level), it assumes Global.
	// The VM will check globals at runtime.
	// So, we do NOT need to persist Compiler state, only VM state (Globals).
	// EXCEPT if we wanted to support multi-line structures incrementally, but for now line-by-line.

	for {
		fmt.Print(">>> ")
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()

		if strings.TrimSpace(line) == "exit" {
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}

		// 1. Parse
		l := lexer.New(line)
		p := parser.New(l)
		program := p.ParseProgram()

		if len(p.Errors()) > 0 {
			for _, msg := range p.Errors() {
				fmt.Printf("%s\n", msg)
			}
			continue
		}

		// 2. REPL Magic: If statement is a single ExpressionStmt, print it.
		// "1 + 1" -> "print(1 + 1)"
		if len(program.Statements) == 1 {
			if exprStmt, ok := program.Statements[0].(*ast.ExpressionStmt); ok {
				// Wrap in print call
				// print(expr)
				callExpr := &ast.CallExpression{
					Token: token.Token{Type: token.IDENTIFIER, Literal: "print"},
					Function: &ast.Identifier{
						Token: token.Token{Type: token.IDENTIFIER, Literal: "print"},
						Value: "print",
					},
					Arguments: []ast.Expression{exprStmt.Expression},
				}
				// Replace statement
				program.Statements[0] = &ast.ExpressionStmt{
					Token:      exprStmt.Token,
					Expression: callExpr,
				}
			}
		}

		// 3. Compile
		c := compiler.New()
		chunk, err := c.Compile(program)
		if err != nil {
			fmt.Printf("Compiler error: %s\n", err)
			continue
		}

		// 4. Disassembly (optional)
		if showDisasm {
			chunk.DisassembleAll("REPL")
		}

		// 5. Interpret (using shared VM)
		// VM.Interpret resets stack but keeps globals (which we want).
		if err := machine.Interpret(chunk); err != nil {
			fmt.Printf("Runtime error: %s\n", err)
		}
	}
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
		for _, msg := range p.Errors() {
			fmt.Printf("%s\n", msg)
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
