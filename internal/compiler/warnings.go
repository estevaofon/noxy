package compiler

import "fmt"

// Warning e um diagnostico NAO fatal do compilador. O compilador nunca
// escreve em stdout/stderr (AGENTS.md E.6: stdout e do programa, stderr e do
// diagnostico): acumula os avisos e quem chamou Compile decide o destino — a
// CLI e o REPL imprimem em diagOut, o loader de modulos da VM em os.Stderr.
// Issue #61 item 3.
type Warning struct {
	Message string
	File    string
	Line    int
}

// String e o texto que a CLI mostra: duas linhas, no formato dos erros.
func (w Warning) String() string {
	return fmt.Sprintf("warning: %s\n  --> %s:%d", w.Message, w.File, w.Line)
}

// warn registra um aviso na lista compartilhada por toda a arvore de
// compiladores (raiz + NewChild dos corpos de funcao): e o compilador RAIZ
// que a CLI consulta. Aviso repetido (mesmo arquivo/linha/mensagem — um
// template generico instanciado N vezes) entra uma vez.
func (c *Compiler) warn(message string) {
	if c.warnings == nil {
		c.warnings = &[]Warning{}
	}
	w := Warning{Message: message, File: c.FileName, Line: c.currentLine}
	for _, existing := range *c.warnings {
		if existing == w {
			return
		}
	}
	*c.warnings = append(*c.warnings, w)
}

// Warnings devolve os avisos acumulados por este compilador e seus filhos,
// na ordem de emissao (nil quando nao houve nenhum).
func (c *Compiler) Warnings() []Warning {
	if c.warnings == nil || len(*c.warnings) == 0 {
		return nil
	}
	return append([]Warning(nil), (*c.warnings)...)
}
