package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/console"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/pkgmanager"
	"noxy-vm/internal/token"
	"noxy-vm/internal/version"
	"noxy-vm/internal/vm"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
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
	showVersion := flag.Bool("version", false, "Show version information")
	showHelp := flag.Bool("help", false, "Show help message")

	// Custom Usage to show double dashes
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: noxy [options] [file]\n\nOptions:\n")
		flag.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(os.Stderr, "  --%s\n\t%s\n", f.Name, f.Usage)
		})
	}

	getPkg := flag.String("get", "", "Download and install a package (e.g. github.com/user/repo@version)")
	cpuProfile := flag.String("cpuprofile", "", "Write CPU profile to file")
	memProfile := flag.String("memprofile", "", "Write memory profile to file")
	flag.Parse()

	if *showHelp {
		flag.Usage()
		return
	}

	if *showVersion {
		fmt.Printf("Noxy %s\n", version.Version)
		return
	}

	if *getPkg != "" {
		if err := pkgmanager.Get(*getPkg); err != nil {
			fmt.Printf("Error getting package: %s\n", err)
			os.Exit(1)
		}
		return
	}

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

	exitCode := runFile(filename, string(content), getDir(filename), *showDisassembly, *cpuProfile, *memProfile)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// runFile executa o arquivo cercado pelos profiles de CPU/memoria pedidos
// pelas flags. runWithConfig devolve um codigo de saida em vez de chamar
// os.Exit diretamente, entao os defers deste closure (StopCPUProfile,
// f.Close) sempre rodam antes do processo terminar — inclusive quando o
// programa falha no parser, no compilador ou em runtime. Sem isto, um
// programa que termina em erro (justamente o caso mais interessante de
// perfilar) deixava o --cpuprofile truncado e pulava o --memprofile por
// completo, porque os.Exit não executa defers pendentes.
func runFile(filename, input, rootPath string, showDisasm bool, cpuProfilePath, memProfilePath string) int {
	if cpuProfilePath != "" {
		f, err := os.Create(cpuProfilePath)
		if err != nil {
			fmt.Printf("Error creating CPU profile: %s\n", err)
			return 1
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Printf("Error starting CPU profile: %s\n", err)
			return 1
		}
		defer pprof.StopCPUProfile()
	}

	exitCode := runWithConfig(filename, input, rootPath, showDisasm)

	if memProfilePath != "" {
		f, err := os.Create(memProfilePath)
		if err != nil {
			fmt.Printf("Error creating memory profile: %s\n", err)
			return 1
		}
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			fmt.Printf("Error writing memory profile: %s\n", err)
		}
		f.Close()
	}

	return exitCode
}

func getDir(path string) string {
	return filepath.Dir(path)
}

func startREPL(showDisasm bool) {
	fmt.Printf("Noxy REPL %s\n", version.Version)
	fmt.Println("Type 'exit' to quit.")

	// Shared VM for persistence
	machine := vm.NewWithConfig(vm.VMConfig{RootPath: "."})
	scanner := bufio.NewScanner(os.Stdin)

	// Persist globals across REPL lines
	replGlobals := make(map[string]ast.NoxyType)

	var inputBuffer string

	for {
		// A crashed raw-mode program (e.g. a terminal game) leaves the shared
		// console without line input or echo, which would freeze Scan below.
		console.EnsureLineInput()

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
		c := compiler.NewWithState(replGlobals, make(map[string]*ast.StructStatement), "REPL")
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
	runWithConfig("verify.nx", input, ".", true)
}

// runWithConfig devolve o codigo de saida (0 sucesso, 1 erro) em vez de
// chamar os.Exit diretamente — quem chama (runFile) precisa da chance de
// rodar seus proprios defers (parar/gravar profile) antes do processo
// terminar. Mensagens e codigos de saida observaveis pela CLI continuam
// identicos.
func runWithConfig(filename string, input string, rootPath string, showDisasm bool) int {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		for _, msg := range p.Errors() {
			fmt.Printf("%s\n", msg)
		}
		return 1
	}

	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filename, rootPath)
	chunk, _, err := c.Compile(program)
	if err != nil {
		fmt.Printf("Compiler error: %s\n", err)
		return 1
	}

	if showDisasm {
		fmt.Printf("Disassembly:\n")
		chunk.DisassembleAll("main")
		fmt.Printf("\nExecution:\n")
	}

	machine := vm.NewWithConfig(vm.VMConfig{RootPath: rootPath})
	if err := machine.Interpret(chunk); err != nil {
		fmt.Printf("Runtime error: %s\n", err)
		return 1
	}
	return 0
}
