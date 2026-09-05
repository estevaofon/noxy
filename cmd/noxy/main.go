package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/console"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/lineedit"
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

// diagOut recebe TODO diagnostico da CLI (parser, compilador, runtime,
// hints, leitura de arquivo, profiles); a saida do programa continua em
// stdout. Variavel para os testes redirecionarem.
var diagOut io.Writer = os.Stderr

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(diagOut, "Recovered from panic:", r)
			diagOut.Write(debug.Stack())
			// Um panic que chegou ate aqui e falha, nunca sucesso: sem este
			// exit, scripts/CI viam codigo 0 de um programa que explodiu no
			// meio. Este defer e o primeiro registrado em main, logo roda por
			// ultimo — os demais defers ja executaram quando o Exit dispara.
			os.Exit(1)
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

	getPkg := flag.String("get", "", "Add or upgrade a package (e.g. github.com/user/repo@v1.0.0) and sync noxy_libs")
	doSync := flag.Bool("sync", false, "Install every dependency of noxy.mod into noxy_libs, verified against noxy.sum")
	locked := flag.Bool("locked", false, "With --sync: fail instead of changing noxy.sum or noxy.mod (CI)")
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
			fmt.Fprintf(diagOut, "Error getting package: %s\n", err)
			os.Exit(1)
		}
		return
	}

	if *locked && !*doSync {
		fmt.Fprintln(diagOut, "Error: --locked requires --sync")
		os.Exit(2)
	}
	if *doSync {
		cwd, _ := os.Getwd()
		root, ok := pkgmanager.FindRoot(cwd)
		if !ok {
			fmt.Fprintf(diagOut, "Error: no noxy.mod in %s or any parent\n", cwd)
			os.Exit(1)
		}
		if err := pkgmanager.Sync(root, pkgmanager.SyncOptions{Locked: *locked, Out: os.Stdout}); err != nil {
			fmt.Fprintf(diagOut, "Error syncing packages: %s\n", err)
			os.Exit(1)
		}
		return
	}

	// Remaining args are positional
	args := flag.Args()

	if len(args) < 1 {
		if code := startREPL(*showDisassembly); code != 0 {
			os.Exit(code)
		}
		return
	}

	filename := args[0]
	content, ok := loadScript(filename)
	if !ok {
		os.Exit(1)
	}

	exitCode := runFile(filename, content, getDir(filename), *showDisassembly, *cpuProfile, *memProfile)
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// loadScript le o programa; em falha escreve o diagnostico em diagOut e
// devolve ok=false — main sai com 1 (antes devolvia 0 e so imprimia).
func loadScript(filename string) (string, bool) {
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(diagOut, "Error reading file: %s\n", err)
		return "", false
	}
	return string(content), true
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
			fmt.Fprintf(diagOut, "Error creating CPU profile: %s\n", err)
			return 1
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(diagOut, "Error starting CPU profile: %s\n", err)
			return 1
		}
		defer pprof.StopCPUProfile()
	}

	exitCode := runWithConfig(filename, input, rootPath, showDisasm)

	if memProfilePath != "" {
		f, err := os.Create(memProfilePath)
		if err != nil {
			fmt.Fprintf(diagOut, "Error creating memory profile: %s\n", err)
			return 1
		}
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			fmt.Fprintf(diagOut, "Error writing memory profile: %s\n", err)
		}
		f.Close()
	}

	return exitCode
}

func getDir(path string) string {
	return filepath.Dir(path)
}

// lineSource e de onde o REPL le cada linha: o editor de linha (tty POSIX,
// com setas e historico) ou o bufio.Scanner (pipe, arquivo, Windows — onde o
// proprio console edita a linha). ReadLine mostra o prompt e devolve a linha
// sem terminador; io.EOF encerra o REPL normalmente e lineedit.ErrInterrupt
// (Ctrl-C) o encerra como um SIGINT encerraria — no Windows (console cooked)
// e no Unix antes do editor, Ctrl-C no prompt matava o processo, e o editor
// preserva esse contrato.
type lineSource interface {
	ReadLine(prompt string) (string, error)
}

// scannerSource le linhas convencionais de stdin.
type scannerSource struct {
	scanner *bufio.Scanner
}

func (s *scannerSource) ReadLine(prompt string) (string, error) {
	// A crashed raw-mode program (e.g. a terminal game) leaves the shared
	// console without line input or echo, which would freeze Scan below.
	console.EnsureLineInput()
	// os.Stdout nao tem buffer em Go: o prompt sai direto no write, sem Sync
	// (FlushFileBuffers num pipe do Windows bloqueia ate alguem ler).
	fmt.Print(prompt)
	if !s.scanner.Scan() {
		return "", io.EOF
	}
	return s.scanner.Text(), nil
}

// startREPL roda o REPL e devolve o codigo de saida do processo: 0 em `exit`
// ou EOF (Ctrl-D), 130 (128+SIGINT) quando Ctrl-C encerrou — o mesmo que o
// shell veria se o processo tivesse morrido pelo sinal.
func startREPL(showDisasm bool) int {
	fmt.Printf("Noxy REPL %s\n", version.Version)
	fmt.Println("Type 'exit' to quit.")

	// Prompt roxo como no REPL do Python 3.13 (bold magenta). So colore quando
	// stdout e um terminal capaz de ANSI — em pipe/arquivo os bytes de escape
	// sujariam a saida — e a convencao NO_COLOR desliga a cor.
	prompt, contPrompt := ">>> ", "... "
	if os.Getenv("NO_COLOR") == "" && console.EnableANSIStdout() {
		prompt = "\x1b[1;35m>>> \x1b[0m"
		contPrompt = "\x1b[1;35m... \x1b[0m"
	}

	// Num tty POSIX o editor de linha cuida de setas, Home/End e historico
	// (o modo canonico do tty nao sabe: seta virava "^[[A" na tela). Em pipe
	// e no Windows (console cooked ja edita) fica o Scanner de sempre.
	var src lineSource
	if editor := lineedit.Stdin(); editor != nil {
		src = editor
	} else {
		src = &scannerSource{scanner: bufio.NewScanner(os.Stdin)}
	}
	return replExitCode(runREPL(src, prompt, contPrompt, showDisasm))
}

// replExitCode traduz o fim do loop em codigo de saida: Ctrl-C vira 130,
// como um processo morto por SIGINT; o resto e saida normal.
func replExitCode(err error) int {
	if errors.Is(err, lineedit.ErrInterrupt) {
		return 130
	}
	return 0
}

// runREPL e o loop ler-compilar-executar sobre a fonte de linhas dada.
// Devolve lineedit.ErrInterrupt se Ctrl-C o encerrou e nil em `exit`/EOF.
func runREPL(src lineSource, prompt, contPrompt string, showDisasm bool) error {
	// Shared VM for persistence
	machine := vm.NewWithConfig(vm.VMConfig{RootPath: "."})
	// Extensoes por processo precisam de EOF/kill na saida (spec §4.5); o
	// defer cobre sucesso, erro de runtime e o desenrolar de um panic.
	defer machine.CloseExtensions()

	// Persist globals, struct definitions (comuns e instancias monomorfizadas
	// de generico) e o registry de templates genericos entre linhas do REPL
	// (spec §5) — os tres compoem o estado de sessao. replStructs conserta um
	// bug pre-existente: antes desta task, cada linha recebia um mapa de
	// structs NOVO, entao um `struct Point ... end` numa linha desaparecia na
	// linha seguinte. replGenerics faz o REPL enxergar na proxima linha um
	// template genérico (`struct Caixa<T>`/`func id<T>`) declarado numa linha
	// anterior — sem ele, SetGenericState nunca era chamado e cada linha via
	// um GenericRegistry vazio.
	replGlobals := make(map[string]ast.NoxyType)
	replStructs := make(map[string]*ast.StructStatement)
	replGenerics := compiler.NewGenericRegistry()
	// replBindings: nomes globais (let, func, struct, import) de linhas
	// anteriores — a sessao segue a mesma regra de redeclaracao de um
	// arquivo (spec §3), linha a linha; so func sobre func e permitido.
	replBindings := make(map[string]compiler.GlobalDecl)
	// replModules: aliases de `use m` e cache de descoberta de modulos das
	// linhas anteriores — `use io` numa linha e `let f: io.File` na seguinte
	// (e a origem dos structs importados, para tipar `f.path`) so funcionam
	// se o estado de modulos acompanhar globals/structs (0.13.0, #58).
	var replModules *compiler.ModuleState

	var inputBuffer string

	for {
		currentPrompt := prompt
		if inputBuffer != "" {
			currentPrompt = contPrompt
		}
		line, err := src.ReadLine(currentPrompt)
		if errors.Is(err, lineedit.ErrInterrupt) {
			// Ctrl-C encerra o REPL (o editor ja restaurou o tty e escreveu
			// "^C"); o chamador sai com 130.
			return err
		}
		if err != nil {
			break
		}

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
				fmt.Fprintf(diagOut, "%s\n", msg)
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
		c := compiler.NewWithState(replGlobals, replStructs, "REPL")
		c.SetGenericState(replGenerics)
		c.SetSessionBindings(replBindings)
		c.SetKnownGlobals(append(machine.GlobalNames(), compiler.PluginNativeNames(program)...))
		c.SetModuleState(replModules)
		chunk, _, err := c.Compile(program)
		// Avisos do compilador sao diagnostico: diagOut, nunca stdout
		// (issue #61 item 3). Saem mesmo quando a compilacao falha depois.
		for _, warning := range c.Warnings() {
			fmt.Fprintln(diagOut, warning)
		}
		if err != nil {
			fmt.Fprintf(diagOut, "Compiler error: %s\n", err)
			inputBuffer = "" // Reset
			continue
		}

		// Update globals
		replGlobals = c.GetGlobals()
		replModules = c.ModuleState()
		// Sessao lembra os nomes desta linha — SO apos compilar com sucesso,
		// para uma linha rejeitada nao queimar o nome.
		for name, decl := range c.ProgramBindings() {
			replBindings[name] = decl
		}

		// 4. Disassembly (optional)
		if showDisasm {
			chunk.DisassembleAll("REPL")
		}

		// 5. Interpret (using shared VM)
		// VM.Interpret resets stack but keeps globals (which we want).
		if err := machine.Interpret(chunk); err != nil {
			fmt.Fprintf(diagOut, "Runtime error: %s\n", err)
			var advised *vm.AdvisedError
			if errors.As(err, &advised) {
				fmt.Fprintf(diagOut, "hint: %s\n", advised.Advice)
			}
		}

		inputBuffer = "" // Reset buffer after execution
	}
	return nil
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
			fmt.Fprintf(diagOut, "%s\n", msg)
		}
		return 1
	}

	// A VM nasce antes do compilador: seus nativos sao os globais que o
	// check de global inexistente (issue #47 parte 3) garante existir.
	machine := vm.NewWithConfig(vm.VMConfig{RootPath: rootPath})
	// Extensoes por processo precisam de EOF/kill na saida (spec §4.5); o
	// defer cobre sucesso, erro de runtime e o desenrolar de um panic.
	defer machine.CloseExtensions()
	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filename, rootPath)
	c.SetKnownGlobals(append(machine.GlobalNames(), compiler.PluginNativeNames(program)...))
	chunk, _, err := c.Compile(program)
	// Avisos do compilador sao diagnostico: diagOut, nunca stdout (issue
	// #61 item 3). Saem mesmo quando a compilacao falha depois.
	for _, warning := range c.Warnings() {
		fmt.Fprintln(diagOut, warning)
	}
	if err != nil {
		fmt.Fprintf(diagOut, "Compiler error: %s\n", err)
		return 1
	}

	if showDisasm {
		fmt.Printf("Disassembly:\n")
		chunk.DisassembleAll("main")
		fmt.Printf("\nExecution:\n")
	}

	if err := machine.Interpret(chunk); err != nil {
		fmt.Fprintf(diagOut, "Runtime error: %s\n", err)
		var advised *vm.AdvisedError
		if errors.As(err, &advised) {
			fmt.Fprintf(diagOut, "hint: %s\n", advised.Advice)
		}
		return 1
	}
	return 0
}
