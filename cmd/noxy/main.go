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
	"runtime/debug"
	"strings"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic:", r)
			debug.PrintStack()
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

	// Persist globals across REPL lines
	replGlobals := make(map[string]ast.NoxyType)

	var inputBuffer string

	for {
		if inputBuffer == "" {
			fmt.Print(">>> ")
		} else {
			fmt.Print("... ")
		}
		os.Stdout.Sync()

		if !scanner.Scan() {
			break
		}
		line := scanner.Text()

		if strings.TrimSpace(line) == "exit" {
			break
		}

		// Handle empty lines in multiline mode
		if strings.TrimSpace(line) == "" && inputBuffer == "" {
			continue
		}

		// Append to buffer
		if inputBuffer == "" {
			inputBuffer = line
		} else {
			inputBuffer += "\n" + line
		}

		// 1. Parse
		l := lexer.New(inputBuffer)
		p := parser.New(l)
		program := p.ParseProgram()

		if len(p.Errors()) > 0 {
			// Check for incomplete input
			isIncomplete := false
			for _, msg := range p.Errors() {
				// We look for errors indicating we hit EOF unexpectedly
				// "found end of file" (from token.Display) or "found EOF" (literal fallback)
				if strings.Contains(msg, "found end of file") || strings.Contains(msg, "found EOF") {
					isIncomplete = true
					break
				}
			}

			if isIncomplete {
				// Continue reading
				continue
			}

			// Real Error
			for _, msg := range p.Errors() {
				fmt.Printf("%s\n", msg)
			}
			inputBuffer = "" // Reset
			continue
		}

		// If we are here, parse was successful.
		// BUT we might want to enforce that if we opened a block, we closed it?
		// Parser should error if block not closed.
		// Just creating an empty line to finish multiline input?
		// Python REPL:
		// >>> if True:
		// ...     print(1)
		// ...
		// (Run)
		//
		// Noxy syntax uses 'end'. So 'end' token should close the block.
		// If parser says OK, then the block IS closed.
		// So we can run immediately.

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
		c := compiler.NewWithState(replGlobals)
		chunk, _, err := c.Compile(program)
		if err != nil {
			fmt.Printf("Compiler error: %s\n", err)
			inputBuffer = "" // Reset
			continue
		}

		// Update globals
		replGlobals = c.GetGlobals()

		// 4. Disassembly (optional)
		if showDisasm {
			chunk.DisassembleAll("REPL")
		}

		// 5. Interpret (using shared VM)
		// VM.Interpret resets stack but keeps globals (which we want).
		if err := machine.Interpret(chunk); err != nil {
			fmt.Printf("Runtime error: %s\n", err)
		}

		inputBuffer = "" // Reset buffer after execution
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
	chunk, _, err := c.Compile(program)
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
