package compiler

import "noxy-vm/internal/ast"

// FuncTemplate guarda a declaracao original (nao compilada) de uma funcao
// generica, junto com o modulo onde foi declarada. Monomorfizacao (Tasks
// futuras) usa Decl para instanciar uma copia concreta por combinacao de
// tipos observada em sites de chamada.
type FuncTemplate struct {
	Decl   *ast.FunctionStatement
	Module string
}

// StructTemplate e o analogo de FuncTemplate para structs genericas.
type StructTemplate struct {
	Decl   *ast.StructStatement
	Module string
}

// GenericRegistry acumula os templates genericos vistos durante a compilacao
// de um programa (e, futuramente, de seus modulos). Declaracoes com
// TypeParams nao-vazio nao emitem bytecode: elas apenas populam este
// registro, para serem monomorfizadas sob demanda nos sites de uso.
type GenericRegistry struct {
	Funcs   map[string]*FuncTemplate
	Structs map[string]*StructTemplate
}

// NewGenericRegistry cria um GenericRegistry vazio, pronto para uso.
func NewGenericRegistry() *GenericRegistry {
	return &GenericRegistry{
		Funcs:   make(map[string]*FuncTemplate),
		Structs: make(map[string]*StructTemplate),
	}
}

// SetGenericState injeta um GenericRegistry existente no compilador. Usado
// pelo REPL (que cria um *Compiler novo a cada linha, mas precisa manter os
// templates genericos vistos em linhas anteriores) e por testes que
// precisam inspecionar/preparar o registro antes de compilar.
func (c *Compiler) SetGenericState(reg *GenericRegistry) {
	c.generics = reg
}

// registryOrInit retorna o GenericRegistry do compilador, inicializando-o
// lazily na primeira chamada. Todo acesso a c.generics dentro do pacote deve
// passar por aqui para nao lidar com nil.
func (c *Compiler) registryOrInit() *GenericRegistry {
	if c.generics == nil {
		c.generics = NewGenericRegistry()
	}
	return c.generics
}
