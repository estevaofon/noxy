package compiler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

// TestBorrowCorpusScan e o GATE da etapa de warning do issue #83 (spec
// 2026-08-25-issue-83-borrow-scope §9.2): mede quantos sites do corpus a regra
// R11/R12 atinge, sem executar os exemplos. Nao e um teste de regressao — nao
// afirma nada, so conta e lista — por isso fica pulado por padrao.
//
//	NOXY_SCAN=1 go test ./internal/compiler -run TestBorrowCorpusScan -v
//
// E o numero que ele imprime, nao uma estimativa por grep, que autoriza a
// promocao dos avisos a erro na v0.21.0.
func TestBorrowCorpusScan(t *testing.T) {
	if os.Getenv("NOXY_SCAN") == "" {
		t.Skip("gate manual: NOXY_SCAN=1")
	}
	roots := []string{"../../noxy_examples", "../stdlib", "../../tests"}
	var files []string
	for _, r := range roots {
		filepath.Walk(r, func(p string, i os.FileInfo, e error) error {
			if e == nil && !i.IsDir() && strings.HasSuffix(p, ".nx") {
				files = append(files, p)
			}
			return nil
		})
	}
	parseErr, warned, total := 0, 0, 0
	for _, f := range files {
		total++
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		func() {
			defer func() { recover() }()
			p := parser.New(lexer.New(string(src)))
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				parseErr++
				t.Logf("PARSE-ERR %s", f)
				return
			}
			c := compiler.NewWithStateAndRoot(map[string]ast.NoxyType{}, map[string]*ast.StructStatement{}, f, filepath.Dir(f))
			c.Compile(prog)
			hit := false
			for _, w := range c.Warnings() {
				if strings.Contains(w.Message, "reference into a container") || strings.Contains(w.Message, "no ref-parameter contract") {
					if !hit {
						warned++
						hit = true
					}
					t.Logf("%s:%d: %s", f, w.Line, strings.SplitN(w.Message, "\n", 2)[0])
				}
			}
		}()
	}
	t.Logf("TOTAIS: %d arquivos | %d parse-erro | %d com aviso R11", total, parseErr, warned)
}
