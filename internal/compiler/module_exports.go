package compiler

import (
	"fmt"
	"maps"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/stdlib"
	"os"
	"path/filepath"
	"strings"
)

// moduleDiscoveryState e o estado compartilhado da descoberta de modulos
// dentro de UMA arvore de compiladores:
//
//   - active e o guard de ciclo (um modulo em carga nao pode ser recarregado
//     por dentro da propria carga);
//   - loaded e a MEMOIZACAO de loadModuleDeclarations. Sem ela, cada `use`
//     paga um parse + um Compile completo de validacao do modulo (e,
//     recursivamente, dos modulos que ele importa) UMA VEZ POR CHAMADOR:
//     predeclareImportedTemplates (discoverModuleExports +
//     moduleTopLevelBindings), predeclareImport (idem) e o case *ast.UseStmt
//     de compiler.go — tudo isso dobrado pelo two-pass dos genericos. Medido
//     em `use http select *`: 1,4s -> 12,3s. Com o memo o custo volta a UMA
//     carga por modulo por compilacao.
type moduleDiscoveryState struct {
	active map[string]bool
	loaded map[string]loadedModule
	// origins: declaracao de struct -> modulo que a declarou. Chave por
	// PONTEIRO (estavel, porque o Program e memoizado em loaded); um struct
	// do programa nunca entra aqui — structOrigin devolve "" para ele.
	origins map[*ast.StructStatement]string
	// scopes e o memo de moduleStructScope por modulo.
	scopes map[string]*moduleStructScope
	// exported e o memo de discoverModuleStructs por modulo (so sucessos):
	// structDeclaration e programStructName consultam a lista de structs
	// exportados a cada nome qualificado / acesso a membro de valor de
	// modulo, e a lista e uma funcao pura do Program memoizado em loaded.
	exported map[string]map[string]*ast.StructStatement
}

// loadedModule e o resultado memoizado de loadModuleDeclarations: o Program
// ja parseado E VALIDADO (parseModuleDeclarations roda validator.Compile
// antes de devolver) ou, para modulo de diretorio, a lista de submodulos.
//
// Compartilhar o MESMO *ast.Program entre chamadores e seguro porque a
// mutacao in-place que o pipeline faz nele (resolveSignatureAnnotations /
// resolveStructFieldAnnotations, e o merge das instancias monomorfizadas de
// runGenericsPass1) acontece INTEIRA dentro de parseModuleDeclarations, antes
// do primeiro retorno — o codigo anterior a este memo ja devolvia o AST
// pos-mutacao a todo chamador, so que reconstruido do zero a cada vez. Os
// consumidores (discoverModuleExports, discoverModuleStructs,
// moduleTopLevelBindings, importBindingFrom) apenas LEEM as declaracoes; o
// unico que guarda um ponteiro para dentro do modulo e importBindingFrom, ao
// registrar um template no GenericRegistry, e a monomorfizacao sempre trabalha
// sobre um CLONE (substituteFunction/substituteStruct) — nunca sobre tpl.Decl.
type loadedModule struct {
	program        *ast.Program
	directoryNames []string
}

func newModuleDiscoveryState() *moduleDiscoveryState {
	return &moduleDiscoveryState{
		active:   make(map[string]bool),
		loaded:   make(map[string]loadedModule),
		origins:  make(map[*ast.StructStatement]string),
		scopes:   make(map[string]*moduleStructScope),
		exported: make(map[string]map[string]*ast.StructStatement),
	}
}

// discoveryState devolve — criando na primeira chamada — o estado de
// descoberta deste compilador. Criar UMA VEZ e guardar em c.moduleDiscovery e
// o que faz o memo de loadModuleDeclarations valer entre os varios chamadores
// de um mesmo `use` (antes, cada um fabricava um estado descartavel e o memo
// morreria junto com ele). NewChild e newPass1Compiler propagam o ponteiro,
// entao a arvore inteira de uma compilacao compartilha um unico cache — o
// filho de NewChild (corpo de funcao, onde um `use` aninhado tambem compila) e
// o compilador descartavel do pass 1 dos genericos inclusos.
func (c *Compiler) discoveryState() *moduleDiscoveryState {
	if c.moduleDiscovery == nil {
		c.moduleDiscovery = newModuleDiscoveryState()
	}
	return c.moduleDiscovery
}

func (c *Compiler) discoverModuleExports(module string) (map[string]struct{}, bool) {
	return c.discoverModuleExportsWithState(module, c.discoveryState())
}

func (c *Compiler) discoverModuleExportsWithState(module string, state *moduleDiscoveryState) (map[string]struct{}, bool) {
	exports := make(map[string]struct{})
	program, directoryExports, ok := c.loadModuleDeclarations(module, state)
	if !ok {
		return exports, false
	}
	for _, name := range directoryExports {
		exports[name] = struct{}{}
	}
	if program == nil {
		return exports, true
	}

	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case *ast.LetStmt:
			exports[declaration.Name.Value] = struct{}{}
		case *ast.FunctionStatement:
			exports[declaration.Name] = struct{}{}
		case *ast.StructStatement:
			exports[declaration.Name] = struct{}{}
		case *ast.UseStmt:
			switch {
			case declaration.SelectAll:
				imported, loadable := c.discoverModuleExportsWithState(declaration.Module, state)
				if !loadable {
					return make(map[string]struct{}), false
				}
				for name := range imported {
					exports[name] = struct{}{}
				}
			case len(declaration.Selectors) > 0:
				for _, name := range declaration.Selectors {
					exports[name] = struct{}{}
				}
			default:
				name := declaration.Alias
				if name == "" {
					parts := strings.Split(declaration.Module, ".")
					name = parts[len(parts)-1]
				}
				exports[name] = struct{}{}
			}
		}
	}
	return exports, true
}

// discoverModuleStructs finds the struct definitions a module makes available
// by name, so the importing compiler can resolve a field typed as an
// imported struct (e.g. `listener: Socket` after `use net select *`).
//
// `use pkg select *` only ever bound imported names as VALUES
// (c.globals[name] = nil, erasing the static type at the call site — see
// predeclareImport). It never taught c.structs, the separate registry
// runtimeTypeInfoWithStructs walks to resolve a struct FIELD's own field
// layout, about structs defined in another compilation unit. A local struct
// embedding an imported struct type therefore built an incomplete
// ConstructorType (see runtimeTypeInfoWithStructs and runtimeTypeComplete),
// which made every call to that struct's constructor raise "struct
// constructor has incomplete runtime type metadata" -- unconditionally, since
// the incompleteness is baked in at compile time and never resolves itself at
// runtime. This is exactly HttpServer's shape (`listener: Socket`), so
// new_server() was unusable before this fix.
func (c *Compiler) discoverModuleStructs(module string) (map[string]*ast.StructStatement, bool) {
	// Reuse c.moduleDiscovery exactly like discoverModuleExports does. A fresh
	// state here would not know a module already being validated (e.g. a
	// function-body-only `use self select *` self-cycle) is in progress, so
	// the cycle guard in loadModuleDeclarations would never trip: each
	// validator compile spawned to check a nested use statement would start
	// its own independent, equally cycle-blind discovery, recursing without
	// bound.
	return c.discoverModuleStructsWithState(module, c.discoveryState())
}

func (c *Compiler) discoverModuleStructsWithState(module string, state *moduleDiscoveryState) (map[string]*ast.StructStatement, bool) {
	if cached, hit := state.exported[module]; hit {
		return cached, true
	}
	structs, ok := c.buildModuleStructExports(module, state)
	if ok {
		// So SUCESSOS entram no memo (mesma politica de loadModuleDeclarations:
		// uma falha pode ser contextual, ex.: guard de ciclo). O map devolvido
		// e compartilhado entre chamadores e NAO pode ser mutado por eles —
		// importModuleStructs copia para c.structs, nunca o contrario.
		state.exported[module] = structs
	}
	return structs, ok
}

func (c *Compiler) buildModuleStructExports(module string, state *moduleDiscoveryState) (map[string]*ast.StructStatement, bool) {
	structs := make(map[string]*ast.StructStatement)
	program, _, ok := c.loadModuleDeclarations(module, state)
	if !ok {
		return structs, false
	}
	if program == nil {
		// Directory module: no structs of its own to contribute directly.
		return structs, true
	}

	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case *ast.StructStatement:
			// Um TEMPLATE (struct Result<T>) nao e struct concreto: vive no
			// GenericRegistry via predeclareImportedTemplates, nunca em
			// c.structs — senao `use errors select *` faria o nome bare
			// `Result` parecer um struct comum no importador.
			if len(declaration.TypeParams) > 0 {
				continue
			}
			structs[declaration.Name] = declaration
			state.origins[declaration] = module
		case *ast.UseStmt:
			switch {
			case declaration.SelectAll:
				imported, loadable := c.discoverModuleStructsWithState(declaration.Module, state)
				if !loadable {
					return make(map[string]*ast.StructStatement), false
				}
				maps.Copy(structs, imported)
			case len(declaration.Selectors) > 0:
				// Espelho de discoverModuleExports: um nome importado por
				// `select` e reexportado como VALOR pelo modulo (entra no
				// ExportMap), entao a DECLARACAO do struct tem de acompanhar
				// — senao `use a select *` ligaria o construtor T mas `let t:
				// T` acusaria `unknown type 'T'` (issue #58 item 2).
				imported, loadable := c.discoverModuleStructsWithState(declaration.Module, state)
				if !loadable {
					// Dependencia que nao carrega (ou alcancada por dentro da
					// propria carga — guard de ciclo): segue sem esses nomes,
					// como buildModuleStructScope. O resultado deste modulo
					// ainda e memoizado como sucesso; num ciclo, a parte que
					// faltou e a do proprio modulo em carga, cuja lista sai
					// completa pela chamada de fora.
					continue
				}
				for _, name := range declaration.Selectors {
					if definition, exported := imported[name]; exported {
						structs[name] = definition
					}
				}
			}
		}
	}
	return structs, true
}

// moduleStructScope e o que um struct declarado DENTRO de um modulo enxerga
// ao nomear o tipo de um campo: os structs visiveis por nome simples (os
// proprios, os de `use d select *` e os de `use d select A, B`) e os
// namespaces (`use d [as x]`) pelos quais um campo `x.T` resolve.
//
// Difere de discoverModuleStructs, que e a lista do que o modulo EXPORTA
// (proprios + `select *` transitivo): um `use net select Socket` dentro do
// modulo nao reexporta Socket, mas um campo `listener: Socket` do modulo tem
// de resolver.
type moduleStructScope struct {
	structs    map[string]*ast.StructStatement
	namespaces map[string]string
}

func (c *Compiler) moduleStructScope(module string) (*moduleStructScope, bool) {
	state := c.discoveryState()
	if scope, hit := state.scopes[module]; hit {
		return scope, true
	}
	scope, ok := c.buildModuleStructScope(module, state)
	if ok {
		// So SUCESSOS entram no memo — mesma politica de loadModuleDeclarations
		// (uma falha pode ser contextual, ex.: guard de ciclo).
		state.scopes[module] = scope
	}
	return scope, ok
}

func (c *Compiler) buildModuleStructScope(module string, state *moduleDiscoveryState) (*moduleStructScope, bool) {
	program, _, ok := c.loadModuleDeclarations(module, state)
	if !ok {
		return nil, false
	}
	scope := &moduleStructScope{
		structs:    make(map[string]*ast.StructStatement),
		namespaces: make(map[string]string),
	}
	if program == nil {
		return scope, true
	}
	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case *ast.StructStatement:
			if len(declaration.TypeParams) > 0 {
				continue
			}
			scope.structs[declaration.Name] = declaration
			state.origins[declaration] = module
		case *ast.UseStmt:
			switch {
			case declaration.SelectAll:
				imported, loadable := c.discoverModuleStructsWithState(declaration.Module, state)
				if loadable {
					maps.Copy(scope.structs, imported)
				}
			case len(declaration.Selectors) > 0:
				imported, loadable := c.discoverModuleStructsWithState(declaration.Module, state)
				if !loadable {
					continue
				}
				for _, name := range declaration.Selectors {
					if definition, exported := imported[name]; exported {
						scope.structs[name] = definition
					}
				}
			default:
				name := declaration.Alias
				if name == "" {
					parts := strings.Split(declaration.Module, ".")
					name = parts[len(parts)-1]
				}
				scope.namespaces[name] = declaration.Module
			}
		}
	}
	return scope, true
}

// structOrigin devolve o modulo que declarou decl, ou "" para um struct do
// proprio programa (inclusive os declarados dentro de funcoes).
func (c *Compiler) structOrigin(decl *ast.StructStatement) string {
	if decl == nil || c.moduleDiscovery == nil {
		return ""
	}
	return c.moduleDiscovery.origins[decl]
}

// lookupStructFrom resolve um nome de tipo como ele seria lido DENTRO de
// origin: "" e o programa (c.structs por nome simples, namespaceImports para
// `ns.T` — structDeclaration); um nome de modulo usa o escopo desse modulo
// (moduleStructScope). nil quando nao designa struct conhecido.
func (c *Compiler) lookupStructFrom(origin, name string) *ast.StructStatement {
	if origin == "" {
		return c.structDeclaration(name)
	}
	scope, ok := c.moduleStructScope(origin)
	if !ok {
		return nil
	}
	if decl, found := scope.structs[name]; found {
		return decl
	}
	ns, base, found := strings.Cut(name, ".")
	if !found {
		return nil
	}
	dependency, isNamespace := scope.namespaces[ns]
	if !isNamespace {
		return nil
	}
	discovered, loadable := c.discoverModuleStructs(dependency)
	if !loadable {
		return nil
	}
	return discovered[base]
}

// importModuleStructs registers struct field layouts for the names a use
// statement brings into scope, so a locally defined struct can embed one of
// them as a field. Names is nil for `select *`, meaning every struct the
// module exports (directly or via its own nested `select *` imports).
func (c *Compiler) importModuleStructs(module string, names []string) {
	discovered, loadable := c.discoverModuleStructs(module)
	if !loadable {
		return
	}
	if names == nil {
		maps.Copy(c.structs, discovered)
		return
	}
	for _, name := range names {
		if definition, ok := discovered[name]; ok {
			c.structs[name] = definition
		}
	}
}

// rejectNestedTemplateImport recusa, com a mensagem acionavel do §9, um `use`
// ANINHADO (dentro de corpo de funcao ou de lambda — c.enclosing != nil) que
// traga um template generico para o escopo.
//
// Por que e um erro e nao uma feature: a decisao "este programa precisa do
// two-pass?" (hasGenerics em Compile(*ast.Program)) e tomada UMA VEZ, na
// entrada, olhando so os `use` do TOPO (predeclareImportedTemplates varre
// statements de topo). Um `use` aninhado registra o template no meio da
// compilacao, DEPOIS que essa decisao ja passou: no pass 1 nada acontece
// (aquele programa nem entrou no two-pass) e no pass 2 a chamada bate no
// guard defensivo de compileGenericCallSite, que acusa "bug do compilador de
// genéricos" com a linha errada — mensagem inutil para quem escreveu o
// programa. Suportar de verdade exigiria redecidir o two-pass a cada `use`
// aninhado; ate la, o erro claro (com a saida obvia: mover o `use` para o
// topo) vale mais que o falso relato de bug.
//
// A forma de NAMESPACE (`use m [as alias]`) nunca importa nome nenhum (§8) e
// portanto nunca traz template: passa direto. Modulo que nao carrega tambem
// passa direto (bindings vazio) — a tolerancia que
// TestFunctionBodyOnlyWildcardDoesNotAffectModuleLoadability exige.
func (c *Compiler) rejectNestedTemplateImport(declaration *ast.UseStmt) error {
	if c.enclosing == nil {
		return nil
	}
	var names []string
	switch {
	case declaration.SelectAll:
		exports, _ := c.discoverModuleExports(declaration.Module)
		names = make([]string, 0, len(exports))
		for name := range exports {
			names = append(names, name)
		}
	case len(declaration.Selectors) > 0:
		names = declaration.Selectors
	default:
		return nil
	}
	bindings, _ := c.moduleTopLevelBindings(declaration.Module)
	for _, name := range names {
		if isModuleTemplateDeclaration(bindings, name) {
			return nestedTemplateImportError(declaration.Token.Line)
		}
	}
	return nil
}

// nestedTemplateImportError e a mensagem verbatim do §9 para o caso acima.
func nestedTemplateImportError(line int) error {
	return fmt.Errorf(
		"[line %d] template genérico importado dentro de corpo de função não é suportado — mova o 'use' para o top level",
		line,
	)
}

func (c *Compiler) predeclareImport(declaration *ast.UseStmt) error {
	// Os structs importados por select/select* entram em c.structs ja no
	// predeclare (o case *ast.UseStmt repete, idempotente): como funcoes e
	// `let` de topo, um `use` de topo vale para o arquivo inteiro, entao uma
	// assinatura ou campo declarado ANTES da linha do `use` ja enxerga o
	// struct — sem isso checkDeclaredType (issue #58 item 2) acusaria
	// `unknown type` por ordem de declaracao.
	switch {
	case declaration.SelectAll:
		exports, _ := c.discoverModuleExports(declaration.Module)
		bindings, _ := c.moduleTopLevelBindings(declaration.Module)
		for name := range exports {
			if err := c.importBindingFrom(declaration.Module, bindings, name); err != nil {
				return err
			}
		}
		c.importModuleStructs(declaration.Module, nil)
	case len(declaration.Selectors) > 0:
		bindings, _ := c.moduleTopLevelBindings(declaration.Module)
		for _, name := range declaration.Selectors {
			if err := c.importBindingFrom(declaration.Module, bindings, name); err != nil {
				return err
			}
		}
		c.importModuleStructs(declaration.Module, declaration.Selectors)
	default:
		name := declaration.Alias
		if name == "" {
			parts := strings.Split(declaration.Module, ".")
			name = parts[len(parts)-1]
		}
		c.importNamespace(declaration.Module, name)
	}
	return nil
}

// predeclareImportedTemplates registra SO os templates genericos que os
// `use` statements do TOPO do programa importam (select*/select — a forma
// de namespace nunca importa template nenhum, §8). Precisa rodar ANTES da
// decisao hasGenerics()/runGenericsPass1 em Compile(*ast.Program): aquela
// decisao so olha se o registry ja tem alguma entrada, e o predeclare
// "de verdade" dos imports (predeclareGlobalBindings -> predeclareImport,
// que tambem cataloga os globals tipados para programBindings) so roda
// DEPOIS dessa decisao. Sem este passo adiantado, um programa que so usa
// genericos IMPORTADOS (nenhum declarado localmente) nunca aciona o
// two-pass — e a interceptacao de call site em compileCallExpression, que
// olha o registry sem checar c.pass1, encontraria o template e chamaria
// compileGenericCallSite fora do pass 1, batendo no erro defensivo "chegou
// ao pass 2 sem monomorfização" (exatamente o bug pego pelo TDD deste
// arquivo). Idempotente com a passada completa que roda depois — registrar
// o mesmo template duas vezes so troca o ponteiro do AST (reparse) por um
// estruturalmente identico, sem efeito observavel.
func (c *Compiler) predeclareImportedTemplates(statements []ast.Statement) error {
	for _, statement := range statements {
		declaration, ok := statement.(*ast.UseStmt)
		if !ok {
			continue
		}
		switch {
		case declaration.SelectAll:
			exports, _ := c.discoverModuleExports(declaration.Module)
			bindings, _ := c.moduleTopLevelBindings(declaration.Module)
			for name := range exports {
				if !isModuleTemplateDeclaration(bindings, name) {
					continue
				}
				if err := c.importBindingFrom(declaration.Module, bindings, name); err != nil {
					return err
				}
			}
		case len(declaration.Selectors) > 0:
			bindings, _ := c.moduleTopLevelBindings(declaration.Module)
			for _, name := range declaration.Selectors {
				if !isModuleTemplateDeclaration(bindings, name) {
					continue
				}
				if err := c.importBindingFrom(declaration.Module, bindings, name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// moduleTopLevelBindings analisa (e valida, via loadModuleDeclarations, que
// ja roda um Compile completo do modulo num validator descartavel — ver
// parseModuleDeclarations) `module` UMA VEZ e indexa suas declaracoes de
// topo (func, struct, let) por nome. Existe para que um `use m select *`
// com N exports nao repita o parse+validate completo do modulo N vezes —
// cada chamador (predeclareImport, o case *ast.UseStmt de compiler.go)
// busca aqui uma unica vez por `use` e consulta o mapa por nome depois.
//
// As anotacoes de tipo dentro das declaracoes devolvidas ja saem
// RESOLVIDAS (nomes de instancia qualificados no lugar de GenericType cru):
// loadModuleDeclarations roda validator.Compile(program) antes de devolver,
// e resolveSignatureAnnotations/resolveStructFieldAnnotations mutam o AST
// in-place durante esse Compile — o mesmo mecanismo que faz o proprio
// modulo compilar corretamente sozinho.
func (c *Compiler) moduleTopLevelBindings(module string) (map[string]ast.Statement, bool) {
	program, _, ok := c.loadModuleDeclarations(module, c.discoveryState())
	if !ok {
		return nil, false
	}
	declarations := make(map[string]ast.Statement)
	if program == nil {
		// Modulo de diretorio: sem declaracoes de topo proprias.
		return declarations, true
	}
	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case *ast.FunctionStatement:
			declarations[declaration.Name] = declaration
		case *ast.StructStatement:
			declarations[declaration.Name] = declaration
		case *ast.LetStmt:
			declarations[declaration.Name.Value] = declaration
		}
	}
	return declarations, true
}

// importBindingFrom registra UM nome importado de module no compilador
// atual (§8, predeclare tipado + import de templates, R8):
//
//   - template generico (TypeParams>0): entra no GenericRegistry com
//     Module = moduleQualifier(module) — NAO em c.globals, que e so para
//     valores em runtime. E o registro que faz `use colecoes select
//     processa` disparar compileGenericCallSite/compileGenericConstructorSite
//     para "processa" como se fosse um template local, so que instanciando
//     com Module="colecoes" (identidade de instancia distinta de um
//     template homonimo de outro modulo — R8).
//   - func/let/struct comum: entra em c.globals com o TIPO DECLARADO (nao
//     mais apagado para nil) — e a mudanca central do §8 que destrava
//     inferencia sobre dado importado (TestImportedDataInference).
//   - nome que declarations nao resolve (submodulo de diretorio, export
//     vindo de um `use` aninhado sem entrada propria aqui): mantem o
//     comportamento anterior a Task 12 (tipo apagado) em vez de quebrar o
//     import — degrada, nao regride.
func (c *Compiler) importBindingFrom(module string, declarations map[string]ast.Statement, name string) error {
	switch declaration := declarations[name].(type) {
	case *ast.FunctionStatement:
		if len(declaration.TypeParams) > 0 {
			return c.registerFuncTemplate(name, &FuncTemplate{Decl: declaration, Module: moduleQualifier(module)}, declaration.Token.Line)
		}
		c.globals[name] = newFunctionType(declaration.Parameters, declaration.ReturnType)
	case *ast.StructStatement:
		if len(declaration.TypeParams) > 0 {
			return c.registerStructTemplate(name, &StructTemplate{Decl: declaration, Module: moduleQualifier(module)}, declaration.Token.Line)
		}
		params := make([]ast.NoxyType, len(declaration.FieldsList))
		for index, field := range declaration.FieldsList {
			params[index] = field.Type
		}
		c.globals[name] = newStructFunctionType(declaration.Name, params)
	case *ast.LetStmt:
		c.globals[name] = declaration.Type
	default:
		c.globals[name] = nil
	}
	return nil
}

// importedBindingType devolve o tipo declarado, no modulo definidor, de um
// binding NAO-generico exportado por module — a metade "modulo definidor"
// da checagem de identidade do §8 (validateImportedTemplateScope, em
// generics.go): o mesmo dado que importBindingFrom usa para popular
// c.globals no IMPORTADOR, olhado da perspectiva de quem define. ok=false
// para um nome que e um template generico (sem tipo de valor — ver
// validateImportedTemplateScope, que trata esse caso separadamente
// consultando o registry) ou que module simplesmente nao declara.
func (c *Compiler) importedBindingType(module, name string) (ast.NoxyType, bool) {
	bindings, ok := c.moduleTopLevelBindings(module)
	if !ok {
		return nil, false
	}
	switch declaration := bindings[name].(type) {
	case *ast.FunctionStatement:
		if len(declaration.TypeParams) > 0 {
			return nil, false
		}
		return newFunctionType(declaration.Parameters, declaration.ReturnType), true
	case *ast.StructStatement:
		if len(declaration.TypeParams) > 0 {
			return nil, false
		}
		params := make([]ast.NoxyType, len(declaration.FieldsList))
		for index, field := range declaration.FieldsList {
			params[index] = field.Type
		}
		return newStructFunctionType(declaration.Name, params), true
	case *ast.LetStmt:
		return declaration.Type, true
	default:
		return nil, false
	}
}

// importNamespace registra `use m [as alias]` (forma de namespace, sem
// select): o modulo entra em c.globals como objeto opaco (sem tipo
// estatico — nao ha "tipo de modulo" na linguagem, e por isso o nome
// continua apagado para nil aqui, ao contrario de importBindingFrom) e o
// par alias->modulo fica em c.namespaceImports, para que compileCallExpression
// recuse `m.processa(...)` em tempo de compilacao (§8/§9: "não é acessível
// via namespace") em vez de falhar em runtime com uma mensagem generica de
// propriedade nao encontrada.
func (c *Compiler) importNamespace(module, bindName string) {
	c.globals[bindName] = nil
	if c.namespaceImports == nil {
		c.namespaceImports = make(map[string]string)
	}
	if _, seen := c.namespaceImports[bindName]; !seen {
		// Ordem de declaracao dos aliases (programStructName escolhe o
		// primeiro); um alias redeclarado mantem a posicao original.
		c.namespaceOrder = append(c.namespaceOrder, bindName)
	}
	c.namespaceImports[bindName] = module
}

// moduleExportsGenericTemplateName responde se module declara, no seu top
// level, um template generico (func ou struct) chamado name — a consulta
// que o erro de namespace do §9 faz antes de recusar `m.name(...)`.
func (c *Compiler) moduleExportsGenericTemplateName(module, name string) bool {
	bindings, ok := c.moduleTopLevelBindings(module)
	if !ok {
		return false
	}
	return isModuleTemplateDeclaration(bindings, name)
}

// isModuleTemplateDeclaration responde se declarations[name] e um template
// generico (func ou struct com TypeParams>0) — usado tanto pelo erro de
// namespace quanto pelo case *ast.UseStmt de compiler.go (para pular o
// GET_PROPERTY de runtime num `select` que nomeia um template, que nunca
// tem chave no ExportMap do modulo).
func isModuleTemplateDeclaration(declarations map[string]ast.Statement, name string) bool {
	switch declaration := declarations[name].(type) {
	case *ast.FunctionStatement:
		return len(declaration.TypeParams) > 0
	case *ast.StructStatement:
		return len(declaration.TypeParams) > 0
	default:
		return false
	}
}

// loadModuleDeclarations resolve, parseia e VALIDA um modulo, memoizando o
// resultado em state.loaded (ver moduleDiscoveryState).
//
// So SUCESSOS entram no cache. Uma falha pode ser CONTEXTUAL: o guard de
// ciclo (state.active) faz um modulo que carrega perfeitamente sozinho
// devolver false quando alcancado por dentro da propria carga, e memoizar
// esse false condenaria o modulo pelo resto da compilacao. O inverso nao
// existe — estar dentro de uma recursao so pode fazer FALHAR, nunca
// suceder —, entao um sucesso e sempre valido para qualquer contexto.
func (c *Compiler) loadModuleDeclarations(module string, state *moduleDiscoveryState) (*ast.Program, []string, bool) {
	if cached, hit := state.loaded[module]; hit {
		return cached.program, cached.directoryNames, true
	}
	if state.active[module] {
		return nil, nil, false
	}
	state.active[module] = true
	defer delete(state.active, module)

	program, directoryNames, ok := c.resolveModuleDeclarations(module, state)
	if ok {
		if state.loaded == nil {
			state.loaded = make(map[string]loadedModule)
		}
		state.loaded[module] = loadedModule{program: program, directoryNames: directoryNames}
	}
	return program, directoryNames, ok
}

// resolveModuleDeclarations e o corpo nao-memoizado de loadModuleDeclarations:
// procura o arquivo/diretorio/embed do modulo e delega o parse+validacao.
func (c *Compiler) resolveModuleDeclarations(module string, state *moduleDiscoveryState) (*ast.Program, []string, bool) {
	pathName := strings.ReplaceAll(module, ".", string(filepath.Separator))
	for _, candidate := range c.moduleFileCandidates(pathName) {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			base := filepath.Base(candidate)
			for _, entry := range []string{base + ".nx", "main.nx"} {
				entryPath := filepath.Join(candidate, entry)
				if entryInfo, entryErr := os.Stat(entryPath); entryErr == nil && !entryInfo.IsDir() {
					return c.parseModuleDeclarationsFile(entryPath, state)
				}
			}
			entries, readErr := os.ReadDir(candidate)
			if readErr != nil {
				return nil, nil, false
			}
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				if entry.IsDir() {
					_, _, loadable := c.loadModuleDeclarations(module+"."+entry.Name(), state)
					if loadable {
						names = append(names, entry.Name())
					}
				} else if strings.HasSuffix(entry.Name(), ".nx") {
					name := strings.TrimSuffix(entry.Name(), ".nx")
					_, _, loadable := c.loadModuleDeclarations(module+"."+name, state)
					if !loadable {
						return nil, nil, false
					}
					names = append(names, name)
				}
			}
			return nil, names, true
		}
		return c.parseModuleDeclarationsFile(candidate, state)
	}

	embedPath := strings.ReplaceAll(module, ".", "/") + ".nx"
	content, err := stdlib.FS.ReadFile(embedPath)
	if err != nil {
		return nil, nil, false
	}
	return c.parseModuleDeclarations(content, module, state)
}

func (c *Compiler) moduleFileCandidates(pathName string) []string {
	root := c.moduleRoot
	if root == "" {
		root = "."
	}

	var searchRoots []string
	if noxyPath := os.Getenv("NOXY_PATH"); noxyPath != "" {
		searchRoots = append(searchRoots, filepath.SplitList(noxyPath)...)
	}

	var candidates []string
	addSuffix := func(suffix string) {
		for _, searchRoot := range searchRoots {
			candidates = append(candidates,
				filepath.Join(searchRoot, suffix, suffix+".nx"),
				filepath.Join(searchRoot, suffix),
				filepath.Join(searchRoot, suffix+".nx"),
			)
		}
		candidates = append(candidates,
			filepath.Join(root, "noxy_libs", suffix, suffix+".nx"),
			filepath.Join(root, "noxy_libs", suffix),
			filepath.Join(root, "stdlib", suffix),
			filepath.Join(root, suffix),
			filepath.Join("noxy_libs", suffix, suffix+".nx"),
			filepath.Join("noxy_libs", suffix),
			filepath.Join("stdlib", suffix),
			suffix,
		)
	}
	addSuffix(pathName + ".nx")
	addSuffix(pathName)
	return candidates
}

func (c *Compiler) parseModuleDeclarationsFile(path string, state *moduleDiscoveryState) (*ast.Program, []string, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	return c.parseModuleDeclarations(content, path, state)
}

func (c *Compiler) parseModuleDeclarations(content []byte, fileName string, state *moduleDiscoveryState) (*ast.Program, []string, bool) {
	p := parser.New(lexer.New(string(content)))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, nil, false
	}
	for _, statement := range program.Statements {
		declaration, ok := statement.(*ast.UseStmt)
		if !ok {
			continue
		}
		if declaration.SelectAll {
			if _, loadable := c.discoverModuleExportsWithState(declaration.Module, state); !loadable {
				return nil, nil, false
			}
			continue
		}
		if len(declaration.Selectors) > 0 {
			exports, loadable := c.discoverModuleExportsWithState(declaration.Module, state)
			if !loadable {
				return nil, nil, false
			}
			for _, selector := range declaration.Selectors {
				if _, exists := exports[selector]; !exists {
					return nil, nil, false
				}
			}
			continue
		}
		if _, _, loadable := c.loadModuleDeclarations(declaration.Module, state); !loadable {
			return nil, nil, false
		}
	}
	validator := NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), fileName, c.moduleRoot)
	validator.moduleDiscovery = state
	if _, _, err := validator.Compile(program); err != nil {
		return nil, nil, false
	}
	return program, nil, true
}
