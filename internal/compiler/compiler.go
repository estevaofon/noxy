package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
	"path/filepath"
	"strings"
)

type Local struct {
	Name  string
	Depth int
	// Line e a linha da declaracao — so e lida pelo erro de redeclaracao,
	// que aponta a primeira ocorrencia do nome no escopo.
	Line       int
	Type       ast.NoxyType
	IsCaptured bool
	IsParam    bool
	// Owns diz que o SLOT deste vinculo RETEM o composto que guarda — ou seja,
	// que existe um inc pareado com o release de fim de frame (OP_OWN_LOCAL no
	// `let`, na variavel de for-each e no binding de case do select, ou o
	// retain de parametro sem `ref` em callPreparedClosure). E a unica pergunta
	// que os funis de escrita precisam responder. Com todo local nomeado
	// nao-`ref` possuidor desde o nascimento (spec §4.2), Owns coincide com "o
	// tipo declarado nao e `ref T`" — os nao-possuidores restantes sao os slots
	// `ref` (emprestimo) e os slots ocultos da maquinaria ($collection/$map/
	// $sel_*, que emprestam de proposito e sao inalcancaveis pelos funis).
	//
	// O default e false — a direcao segura para o gemeo MUT (no maximo deixa um
	// dono a mais, custando uma copia; nunca solta o que nao reteve). Marque
	// true exatamente onde o inc e emitido.
	Owns bool
}

type Loop struct {
	EnclosingLocals int
	BreakJumps      []int
	// ContinueTarget >= 0: alvo para tras (while: inicio da condicao), emitido
	// como OP_LOOP direto; -1: alvo adiante (for: passo de incremento),
	// registrado em ContinueJumps e patchado quando o alvo e emitido.
	ContinueTarget int
	ContinueJumps  []int
}

type Upvalue struct {
	Index   uint8
	IsLocal bool
	Type    ast.NoxyType
}

type scopedStructBinding struct {
	Depth   int
	Name    string
	Prior   *ast.StructStatement
	Existed bool
}

type Compiler struct {
	enclosing           *Compiler
	currentChunk        *chunk.Chunk
	locals              []Local
	globals             map[string]ast.NoxyType
	upvalues            []Upvalue
	scopeDepth          int
	loops               []*Loop
	currentLine         int
	FileName            string
	moduleRoot          string
	funcReturnType      ast.NoxyType // Expected return type for current function context
	currentFunctionName string
	structs             map[string]*ast.StructStatement
	scopedStructs       []scopedStructBinding
	programBindings     map[string]ast.NoxyType
	moduleDiscovery     *moduleDiscoveryState
	generics            *GenericRegistry // lazy-init: use registryOrInit()
	moduleName          string           // default "main"; setter usado nas tasks de modulo
	instances           *instanceQueue   // lazy-init: use instancesOrInit()
	// pass1 marca o compilador descartavel da primeira passada dos genericos
	// (§5): interceptacao de call site generico so acontece com ele ligado, e
	// ele tambem e o guard de recursao do two-pass (um Compile(*ast.Program)
	// em pass 1 nao reentra na pass 1).
	pass1 bool
	// genericReturnHint carrega a anotacao do `let` envolvente para o proximo
	// call site generico (target-typing do §7). Armado em setGenericReturnHint,
	// consumido e limpo em compileGenericCallSite.
	// O mesmo campo serve ao construtor de struct generico (`let c: Caixa<int> =
	// Caixa([])`), consumido e limpo em compileGenericConstructorSite.
	genericReturnHint ast.NoxyType
	// arrayElementHint carrega o tipo de elemento da anotacao do `let`
	// envolvente para o proximo array literal (target-typing do §3, posicao 3):
	// `let fs: (func(int) -> int)[] = [dobro, identity]` precisa que cada
	// elemento identificador nu (`identity`) saiba o alvo `func(int) -> int`
	// antes de ser compilado. Armado em setArrayElementHint, consumido e limpo
	// no proprio case de ArrayLiteral.
	arrayElementHint ast.NoxyType
	// namespaceImports mapeia o nome local de um `use m` (forma de namespace,
	// sem select) para o modulo que ele nomeia — §8: "template genérico não é
	// acessível via namespace". E uma COPIA por compilador (como globals/
	// structs, nao um ponteiro compartilhado como generics/instances):
	// um `use` dentro de um corpo de funcao (permitido — ver
	// TestRuntimeFunctionBodyOnlyWildcardDoesNotInvalidateModule) so pode
	// afetar o escopo daquele corpo, nunca vazar para o compilador pai.
	namespaceImports map[string]string
	// sessionLets e a memoria de sessao do REPL (nil fora dele): nomes de
	// `let` global de linhas ANTERIORES. O predeclare so CHECA contra ele;
	// quem registra e o loop do REPL apos a linha compilar com sucesso —
	// linha rejeitada nao queima o nome. O scratch do pass 1
	// (newPass1Compiler) nao recebe o campo: fica nil, sem check nem
	// registro duplicado no two-pass.
	sessionLets map[string]int
	// programLets acumula os `let` top-level da compilacao corrente
	// (preenchido pelo predeclare) para o REPL ler via ProgramLets.
	programLets map[string]int
}

type callEmission struct {
	deferred         bool
	registrationLine int
}

var emitImmediateCall callEmission

func emitDeferredCall(line int) callEmission {
	return callEmission{deferred: true, registrationLine: line}
}

func New() *Compiler {
	return NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "")
}

func NewWithState(globals map[string]ast.NoxyType, structs map[string]*ast.StructStatement, fileName string) *Compiler {
	moduleRoot := "."
	if fileName != "" && fileName != "REPL" {
		moduleRoot = filepath.Dir(fileName)
	}
	return NewWithStateAndRoot(globals, structs, fileName, moduleRoot)
}

func NewWithStateAndRoot(globals map[string]ast.NoxyType, structs map[string]*ast.StructStatement, fileName, moduleRoot string) *Compiler {
	if moduleRoot == "" {
		moduleRoot = "."
	}
	c := &Compiler{
		enclosing:    nil,
		currentChunk: chunk.New(),
		locals:       []Local{},
		globals:      globals,
		structs:      structs,
		upvalues:     []Upvalue{},
		scopeDepth:   0,
		loops:        []*Loop{},
		currentLine:  1,
		FileName:     fileName,
		moduleRoot:   moduleRoot,
		moduleName:   "main",
	}
	c.currentChunk.FileName = fileName
	return c
}

func NewChild(parent *Compiler) *Compiler {
	childGlobals := make(map[string]ast.NoxyType, len(parent.globals))
	for name, bindingType := range parent.globals {
		childGlobals[name] = bindingType
	}
	childStructs := make(map[string]*ast.StructStatement, len(parent.structs))
	for name, definition := range parent.structs {
		childStructs[name] = definition
	}
	childNamespaceImports := make(map[string]string, len(parent.namespaceImports))
	for name, module := range parent.namespaceImports {
		childNamespaceImports[name] = module
	}
	c := &Compiler{
		enclosing:        parent,
		currentChunk:     chunk.New(),
		locals:           []Local{},
		globals:          childGlobals,
		structs:          childStructs,
		upvalues:         []Upvalue{},
		scopeDepth:       0,
		loops:            []*Loop{},
		currentLine:      parent.currentLine,
		FileName:         parent.FileName,
		moduleRoot:       parent.moduleRoot,
		programBindings:  parent.programBindings,
		moduleDiscovery:  parent.discoveryState(),
		generics:         parent.generics,
		moduleName:       parent.moduleName,
		instances:        parent.instances,
		pass1:            parent.pass1,
		namespaceImports: childNamespaceImports,
	}
	c.currentChunk.FileName = parent.FileName
	return c
}

func (c *Compiler) GetGlobals() map[string]ast.NoxyType {
	return c.globals
}

// SetSessionLets arma a checagem de redeclaracao entre linhas de uma sessao
// interativa (REPL). Fora do REPL ninguem chama e o campo fica nil.
func (c *Compiler) SetSessionLets(m map[string]int) {
	c.sessionLets = m
}

// ProgramLets devolve os `let` top-level vistos pela ultima compilacao —
// o REPL faz o merge para a sessao somente apos sucesso.
func (c *Compiler) ProgramLets() map[string]int {
	return c.programLets
}

func (c *Compiler) Compile(node ast.Node) (*chunk.Chunk, ast.NoxyType, error) {
	switch n := node.(type) {
	case *ast.Program:
		// Two-pass dos genericos (§5). Fica AQUI, na entrada de *ast.Program,
		// para que todo caminho que compila um Program — programa principal,
		// validator de modulo, runtime do `use`, REPL — ganhe o tratamento
		// sem duplicacao. Programa sem genericos nao paga nada alem da
		// varredura das declaracoes do topo.
		if err := c.predeclareGenericTemplates(n.Statements); err != nil {
			return nil, nil, err
		}
		// §8/R8: templates IMPORTADOS entram no registry aqui tambem, cedo —
		// ver o comentario de predeclareImportedTemplates para o porque
		// (hasGenerics(), logo abaixo, precisa ja enxergar um template que so
		// veio de `use m select f`, nao de uma declaracao local).
		if err := c.predeclareImportedTemplates(n.Statements); err != nil {
			return nil, nil, err
		}
		if !c.pass1 && c.hasGenerics() {
			if err := c.runGenericsPass1(n); err != nil {
				return nil, nil, err
			}
		}

		sequentialBindings := make(map[string]ast.NoxyType, len(c.globals))
		for name, bindingType := range c.globals {
			sequentialBindings[name] = bindingType
		}
		if err := c.predeclareStructs(n.Statements); err != nil {
			return nil, nil, err
		}
		if err := c.predeclareGlobalBindings(n.Statements); err != nil {
			return nil, nil, err
		}
		c.programBindings = make(map[string]ast.NoxyType, len(c.globals))
		for name, bindingType := range c.globals {
			c.programBindings[name] = bindingType
		}
		for name := range c.globals {
			delete(c.globals, name)
		}
		for name, bindingType := range sequentialBindings {
			c.globals[name] = bindingType
		}
		for _, stmt := range n.Statements {
			if _, _, err := c.Compile(stmt); err != nil {
				return nil, nil, err
			}
		}
		// Implicit return for script/module
		c.emitByte(byte(chunk.OP_NULL))
		c.emitByte(byte(chunk.OP_RETURN))
		return c.currentChunk, nil, nil

	case *ast.LetStmt:
		c.setLine(n.Token.Line)
		// Redeclaracao no mesmo escopo e erro (spec §3): `let` cria vinculo
		// novo, e um segundo `let` do mesmo nome no mesmo depth poderia ate
		// trocar o tipo por baixo da §2.0. Escopos internos (depth maior)
		// continuam livres para sombrear — a pilha de locals e ordenada por
		// depth, entao a varredura para na primeira mudanca de profundidade.
		// Globais tem o proprio check em predeclareGlobalBindings.
		if c.scopeDepth > 0 {
			for i := len(c.locals) - 1; i >= 0 && c.locals[i].Depth == c.scopeDepth; i-- {
				if c.locals[i].Name == n.Name.Value {
					return nil, nil, fmt.Errorf(
						"[line %d] variable '%s' redeclared in this scope (previous declaration at line %d); hint: to update the value, use '%s = ...' without 'let'",
						n.Token.Line, n.Name.Value, c.locals[i].Line, n.Name.Value)
				}
			}
		}
		// §4, terceira familia de hooks: a anotacao pode nomear instancias de
		// struct generico (`Caixa<int>`, `Caixa<int>[]`, `ref Node<int>`). A
		// resolucao vem ANTES de tudo — antes do hint, do emitDefaultInit e da
		// checagem de tipos — e reescreve n.Type in-place para o nome
		// qualificado, que e o que o pass 2 compila.
		resolvedAnnotation, err := c.resolveAnnotation(n.Type, n.Token.Line)
		if err != nil {
			return nil, nil, err
		}
		n.Type = resolvedAnnotation

		var valType ast.NoxyType
		// Compile initializer
		if n.Value != nil {
			// §3 target-typing, posicao 1: o valor do `let` pode ser um
			// identificador NU nomeando um template de funcao (`let f:
			// func(int) -> int = identity`) — a anotacao e o alvo concreto que
			// instancia a genérica antes de qualquer tentativa de compilar o
			// identificador normalmente (o que hoje leria um global nunca
			// definido).
			if err := c.rewriteIfGenericValue(n.Value, n.Type); err != nil {
				return nil, nil, err
			}
			// Target-typing do §7: a anotacao do `let` e o hint para o T que
			// so aparece no retorno do template. Armado imediatamente antes
			// (e limpo imediatamente depois) da compilacao do valor.
			c.setGenericReturnHint(n.Type, n.Value)
			// §3, posicao 3: elemento de array literal — o tipo de elemento
			// da anotacao e o alvo para cada identificador nu dentro de `[
			// ... ]`. Mesma disciplina de armar/limpar do hint acima.
			c.setArrayElementHint(n.Type, n.Value)
			_, t, err := c.Compile(n.Value)
			c.genericReturnHint = nil
			c.arrayElementHint = nil
			if err != nil {
				return nil, nil, err
			}
			valType = t
		} else {
			// Default value
			if err := c.emitDefaultInit(n.Type); err != nil {
				return nil, nil, err
			}
			valType = n.Type
		}

		// Type Check
		// Auto-Deref if Value is Reference and Target is NOT Reference
		if n.Type != nil {
			if refType, isRef := valType.(*ast.RefType); isRef {
				if _, targetIsRef := n.Type.(*ast.RefType); !targetIsRef {
					// We have Ref, want Value -> Deref
					c.emitByte(byte(chunk.OP_DEREF))
					valType = refType.ElementType
				}
			}

			if !c.areTypesCompatible(n.Type, valType) {
				return nil, nil, fmt.Errorf("[line %d] type mismatch in '%s' declaration: expected %s, got %s", c.currentLine, n.Name.Value, n.Type.String(), noxyTypeName(valType))
			}
			if err := c.emitRuntimeValueType(n.Type); err != nil {
				return nil, nil, err
			}
		}

		if c.scopeDepth > 0 {
			// Local variable
			// RC: o let e um vinculo duravel do frame (spec §4.2) — exceto
			// quando o tipo declarado e `ref T`. Um vinculo ref e EMPRESTIMO
			// (borrow), nunca dono: conta-lo daria um dono a mais ao objeto
			// emprestado e a mutacao atraves do emprestimo clonaria (escrita
			// perdida). Mesma regra dos parametros IsRef, que
			// callPreparedClosure ja pula. A condicao aqui e exatamente a que
			// resolveLocal enxerga depois (addLocal guarda este n.Type), entao
			// o rebind — OP_SET_LOCAL_BORROW — decide igual, e um mesmo slot
			// nunca mistura escrita contada com escrita emprestada.
			if _, isRefBinding := n.Type.(*ast.RefType); !isRefBinding {
				c.emitByte(byte(chunk.OP_OWN_LOCAL))
				c.addOwnedLocal(n.Name.Value, n.Type)
			} else {
				c.addLocal(n.Name.Value, n.Type)
			}
			// Do NOT pop. The value stays on stack and becomes the local variable.
		} else {
			// Global
			// Register global type
			c.globals[n.Name.Value] = n.Type

			nameConstant := c.makeConstant(value.NewString(n.Name.Value))
			// RC: mesma regra do let local — global de tipo `ref T` e
			// emprestimo, nao dono (ver OP_SET_LOCAL_BORROW). O tipo do global
			// fica registrado em c.globals, entao o rebind adiante decide
			// igual e o slot nunca mistura escrita contada com emprestada.
			if _, isRefBinding := n.Type.(*ast.RefType); isRefBinding {
				c.emitOpWithConstantIndex(chunk.OP_SET_GLOBAL_BORROW, nameConstant)
			} else {
				c.emitOpWithConstantIndex(chunk.OP_SET_GLOBAL, nameConstant)
			}
			c.emitByte(byte(chunk.OP_POP))
		}
		return c.currentChunk, nil, nil

	case *ast.ExpressionStmt:
		c.setLine(n.Token.Line)
		_, _, err := c.Compile(n.Expression)
		if err != nil {
			return nil, nil, err
		}
		c.emitByte(byte(chunk.OP_POP)) // Pop expression result (stmt)
		return c.currentChunk, nil, nil

	case *ast.IntegerLiteral:
		c.setLine(n.Token.Line)
		c.emitConstant(value.NewInt(n.Value))
		return c.currentChunk, &ast.PrimitiveType{Name: "int"}, nil

	case *ast.FloatLiteral:
		c.emitConstant(value.NewFloat(n.Value))
		return c.currentChunk, &ast.PrimitiveType{Name: "float"}, nil

	case *ast.Boolean:
		if n.Value {
			c.emitByte(byte(chunk.OP_TRUE))
		} else {
			c.emitByte(byte(chunk.OP_FALSE))
		}
		return c.currentChunk, &ast.PrimitiveType{Name: "bool"}, nil

	case *ast.StringLiteral:
		c.emitConstant(value.NewString(n.Value))
		return c.currentChunk, &ast.PrimitiveType{Name: "string"}, nil

	case *ast.BytesLiteral:
		c.emitConstant(value.NewBytes(n.Value))
		return c.currentChunk, &ast.PrimitiveType{Name: "bytes"}, nil

	case *ast.AssignStmt:
		c.setLine(n.Token.Line)
		if prefixExp, ok := n.Target.(*ast.PrefixExpression); ok {
			// Explicit Dereference Assignment: *ref = val
			// This signals an UPDATE (writing to the value pointed to).
			if prefixExp.Operator != "*" {
				return nil, nil, fmt.Errorf("[line %d] invalid assignment target", c.currentLine)
			}

			// 1. Compile Operator (The Reference)
			// e.g. *x = 10 -> compile x
			_, refType, err := c.Compile(prefixExp.Right)
			if err != nil {
				return nil, nil, err
			}

			// Must be a Reference type
			refT, isRef := refType.(*ast.RefType)
			if !isRef {
				return nil, nil, fmt.Errorf("[line %d] cannot dereference non-reference type %s in assignment", c.currentLine, refType.String())
			}

			// 2. Compile Value
			_, valType, err := c.Compile(n.Value)
			if err != nil {
				return nil, nil, err
			}

			// 3. Type Check: ElementType vs ValueType
			// Logic: *ref<T> = T
			// Check if value type matches the element type of the reference

			// Auto-deref RHS if it is a RefType but we need a Value.
			if valRef, valIsRef := valType.(*ast.RefType); valIsRef {
				// If target is value type, dereference the RHS reference.
				if _, targetIsRef := refT.ElementType.(*ast.RefType); !targetIsRef {
					c.emitByte(byte(chunk.OP_DEREF))
					valType = valRef.ElementType
				}
			}

			if !c.areTypesCompatible(refT.ElementType, valType) {
				return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment: expected %s, got %s", c.currentLine, refT.ElementType.String(), valType.String())
			}
			if err := c.emitRuntimeValueType(refT.ElementType); err != nil {
				return nil, nil, err
			}

			// 4. Emit Store
			// Stack: [Ref, Val]
			// OP_STORE_REF consumes both (Val -> *Ref).
			c.emitByte(byte(chunk.OP_STORE_REF))

			return c.currentChunk, nil, nil

		} else if ident, ok := n.Target.(*ast.Identifier); ok {
			// Fusao de incremento: emite OP_INC_LOCAL_INT e nao empilha nada
			// (atribuicao e statement; o POP do caminho generico tambem cai fora).
			if c.tryFuseLocalIntIncrement(ident, n.Value) {
				return c.currentChunk, nil, nil
			}
			// Identifier Assignment: x = val
			// 1. Compile Value (pushed to stack)
			_, valType, err := c.Compile(n.Value)
			if err != nil {
				return nil, nil, err
			}

			// 2. Check and Set Variable
			if arg, localType := c.resolveLocal(ident.Value); arg != -1 {
				// Local Logic
				_ = c.locals[arg] // Keep reference for potential future use

				if refType, isRef := localType.(*ast.RefType); isRef {
					// Assignment to a Reference Variable (local ref T)
					// This is a REBIND (ref = ref).
					// Update (*ref = val) is handled by PrefixExpression.

					isRefVal := false
					if valType != nil {
						_, isRefVal = valType.(*ast.RefType)
					}

					// REBIND: ref = ref OR ref = nil (dynamic/unknown)
					// Enable rebind if valType is nil (unknown, e.g. from imports) or explicitly ref
					if isRefVal || valType == nil || isNullType(valType) {
						if valType != nil && !c.areTypesCompatible(refType, valType) {
							return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to '%s': expected %s, got %s", c.currentLine, ident.Value, localType.String(), valType.String())
						}

						// Check if trying to rebind a ref parameter
						local := c.locals[arg]
						if local.IsParam {
							fmt.Printf("warning: rebinding ref parameter '%s' has no effect outside function\n", ident.Value)
							fmt.Printf("  --> %s:%d\n", c.FileName, c.currentLine)
						}
						if err := c.emitRuntimeValueType(localType); err != nil {
							return nil, nil, err
						}
						// RC: rebind de local `ref` e troca de EMPRESTIMO, nao
						// de posse — grava no slot sem retain/release (o dono
						// real e o campo/global/slot do chamador apontado).
						// Contar aqui daria um dono a mais ao objeto e faria a
						// mutacao atraves do emprestimo clonar.
						c.emitBytes(byte(chunk.OP_SET_LOCAL_BORROW), byte(arg))
						c.emitByte(byte(chunk.OP_POP))
						return c.currentChunk, nil, nil
					}

					if c.areTypesCompatible(refType.ElementType, valType) {
						return nil, nil, referenceAssignmentTypeError(c.currentLine, ident.Value, localType, valType)
					}
					return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to '%s': expected %s, got %s", c.currentLine, ident.Value, localType.String(), valType.String())

				} else {
					// Standard Value Assignment (int = int)
					if !c.areTypesCompatible(localType, valType) {
						return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to '%s': expected %s, got %s%s", c.currentLine, ident.Value, localType.String(), valType.String(), c.derefReadHint(localType, valType, n.Value))
					}
					if err := c.emitRuntimeValueType(localType); err != nil {
						return nil, nil, err
					}
					// RC: mesma pergunta do gemeo MUT — so slot POSSUIDOR troca
					// posse. Com for-each e select possuidores desde o
					// nascimento, os nao-possuidores nomeados sao exatamente os
					// slots `ref`; o flag Owns segue decidindo.
					if c.localOwns(arg) {
						c.emitBytes(byte(chunk.OP_SET_LOCAL), byte(arg))
					} else {
						c.emitBytes(byte(chunk.OP_SET_LOCAL_BORROW), byte(arg))
					}
					c.emitByte(byte(chunk.OP_POP))
				}
			} else if arg, upvalueType := c.resolveUpvalue(ident.Value); arg != -1 {
				// Upvalue Logic
				if refType, isRef := upvalueType.(*ast.RefType); isRef &&
					!(isReferenceType(valType) || valType == nil || isNullType(valType)) &&
					c.areTypesCompatible(refType.ElementType, valType) {
					return nil, nil, referenceAssignmentTypeError(c.currentLine, ident.Value, upvalueType, valType)
				}
				if !c.areTypesCompatible(upvalueType, valType) {
					return nil, nil, fmt.Errorf(
						"[line %d] type mismatch in assignment to '%s': expected %s, got %s%s",
						c.currentLine, ident.Value, noxyTypeName(upvalueType), noxyTypeName(valType), c.derefReadHint(upvalueType, valType, n.Value),
					)
				}
				if err := c.emitRuntimeValueType(upvalueType); err != nil {
					return nil, nil, err
				}
				c.emitBytes(byte(chunk.OP_SET_UPVALUE), byte(arg))
				c.emitByte(byte(chunk.OP_POP))
			} else {
				// Global Logic
				if globalType, exists := c.globals[ident.Value]; exists {
					// Check if global is a reference type
					if refType, isRef := globalType.(*ast.RefType); isRef {
						// Global Reference Assignment
						_, isRefVal := valType.(*ast.RefType)

						// Allow rebind if valType is Ref or nil (dynamic/unknown)
						if isRefVal || valType == nil || isNullType(valType) {
							if valType != nil && !c.areTypesCompatible(globalType, valType) {
								return nil, nil, fmt.Errorf("[line %d] type mismatch in rebind to global '%s': expected %s, got %s", c.currentLine, ident.Value, globalType.String(), valType.String())
							}

							nameConstant := c.makeConstant(value.NewString(ident.Value))
							if err := c.emitRuntimeValueType(globalType); err != nil {
								return nil, nil, err
							}
							// RC: rebind de global `ref` e troca de
							// emprestimo, nao de posse (ver
							// OP_SET_LOCAL_BORROW).
							c.emitOpWithConstantIndex(chunk.OP_SET_GLOBAL_BORROW, nameConstant)
							c.emitByte(byte(chunk.OP_POP))
						} else {
							// User tried `ref = val`. Explicitly FORBID update via name.
							if c.areTypesCompatible(refType.ElementType, valType) {
								return nil, nil, referenceAssignmentTypeError(c.currentLine, ident.Value, globalType, valType)
							}
							return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to global '%s': expected %s, got %s", c.currentLine, ident.Value, globalType.String(), valType.String())
						}
						return c.currentChunk, nil, nil
					}

					// Standard Global Assignment
					if !c.areTypesCompatible(globalType, valType) {
						return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to global '%s': expected %s, got %s%s", c.currentLine, ident.Value, globalType.String(), valType.String(), c.derefReadHint(globalType, valType, n.Value))
					}
					if err := c.emitRuntimeValueType(globalType); err != nil {
						return nil, nil, err
					}
				}
				nameConstant := c.makeConstant(value.NewString(ident.Value))
				c.emitOpWithConstantIndex(chunk.OP_SET_GLOBAL, nameConstant)
				c.emitByte(byte(chunk.OP_POP))
			}
		} else if indexExp, ok := n.Target.(*ast.IndexExpression); ok {
			// Array/Map Assignment: arr[i] = val
			// IndexExpression assignment is REBINDING the slot in the container.
			// If the container holds References, we are rebinding that slot.
			// If the container holds Values, we are updating that slot.

			// 1. Compile Array (Left) — CoW: cadeia MUT uniciza cada nível
			// do caminho (inclui OP_DEREF_MUT quando a base é ref)
			leftType, _, err := c.compileLValueBase(indexExp.Left)
			if err != nil {
				return nil, nil, err
			}

			// 2. Compile Index
			_, idxType, err := c.Compile(indexExp.Index)
			if err != nil {
				return nil, nil, err
			}

			if _, ok := idxType.(*ast.RefType); ok {
				c.emitByte(byte(chunk.OP_DEREF))
			}

			// 3. Compile Value
			_, valType, err := c.Compile(n.Value)
			if err != nil {
				return nil, nil, err
			}

			// Unwrap RefType
			if ref, ok := leftType.(*ast.RefType); ok {
				leftType = ref.ElementType
			}
			if ref, ok := idxType.(*ast.RefType); ok {
				idxType = ref.ElementType
			}

			// Type Check
			var assignedType ast.NoxyType
			if arrType, ok := leftType.(*ast.ArrayType); ok {
				assignedType = arrType.ElementType
				if idxType != nil && idxType.String() != "int" {
					return nil, nil, fmt.Errorf("[line %d] array index must be int, got %s", c.currentLine, idxType.String())
				}

				// STRICT CHECK:
				// If ElementType is Ref, Value MUST be Ref (Rebind).
				// Implict deref/update via assignment is NOT allowed if types don't match.
				// User must use `*arr[i] = val` for updates.

				if refType, isRef := arrType.ElementType.(*ast.RefType); isRef &&
					!(isReferenceType(valType) || valType == nil || isNullType(valType)) &&
					c.areTypesCompatible(refType.ElementType, valType) {
					return nil, nil, referenceSlotAssignmentTypeError(c.currentLine, assignmentTargetName(indexExp), "element", arrType.ElementType, valType)
				}
				if !c.areTypesCompatible(arrType.ElementType, valType) {
					return nil, nil, fmt.Errorf("[line %d] type mismatch in array assignment: expected %s, got %s%s", c.currentLine, arrType.ElementType.String(), valType.String(), c.derefReadHint(arrType.ElementType, valType, n.Value))
				}
			} else if mapType, ok := leftType.(*ast.MapType); ok {
				assignedType = mapType.ValueType
				if !c.areTypesCompatible(mapType.KeyType, idxType) {
					return nil, nil, fmt.Errorf("[line %d] type mismatch in map key: expected %s, got %s", c.currentLine, mapType.KeyType.String(), idxType.String())
				}
				if refType, isRef := mapType.ValueType.(*ast.RefType); isRef &&
					!(isReferenceType(valType) || valType == nil || isNullType(valType)) &&
					c.areTypesCompatible(refType.ElementType, valType) {
					return nil, nil, referenceSlotAssignmentTypeError(c.currentLine, assignmentTargetName(indexExp), "entry", mapType.ValueType, valType)
				}
				if !c.areTypesCompatible(mapType.ValueType, valType) {
					return nil, nil, fmt.Errorf("[line %d] type mismatch in map value: expected %s, got %s%s", c.currentLine, mapType.ValueType.String(), valType.String(), c.derefReadHint(mapType.ValueType, valType, n.Value))
				}
			} else {
				if leftType != nil && leftType.String() != "any" {
					return nil, nil, fmt.Errorf("[line %d] index assignment on non-array/map type: %s", c.currentLine, leftType.String())
				}
			}
			if err := c.emitRuntimeValueType(assignedType); err != nil {
				return nil, nil, err
			}

			c.emitByte(byte(chunk.OP_SET_INDEX))
			c.emitByte(byte(chunk.OP_POP))

		} else if memberExp, ok := n.Target.(*ast.MemberAccessExpression); ok {
			// Struct Field Assignment: obj.field = val
			// Only REBIND allowed for Ref Fields.
			// *obj.field = val is handled by PrefixExpression.

			// 1. Compile Object — CoW: cadeia MUT uniciza cada nível do
			// caminho (inclui OP_DEREF_MUT quando a base é ref)
			// compileLValueBase ja devolve o tipo DESEMBRULHADO (emite
			// OP_DEREF_MUT quando a base e ref), entao o campo resolve igual
			// com base `ref` ou valor e a checagem abaixo vale para as duas
			// (issue #50, Parte 1; spec 2026-08-20-ref-slot-invariant §3).
			leftType, _, err := c.compileLValueBase(memberExp.Left)
			if err != nil {
				return nil, nil, err
			}

			// RESOLVE FIELD TYPE (antes de compilar o valor: §3 target-typing,
			// posicao 4, precisa do tipo declarado do campo como alvo para
			// decidir se n.Value e um template de funcao nu):
			var fieldType ast.NoxyType
			if prim, ok := leftType.(*ast.PrimitiveType); ok {
				if structDef, exists := c.structs[prim.Name]; exists {
					for _, f := range structDef.FieldsList {
						if f.Name == memberExp.Member {
							fieldType = f.Type
							break
						}
					}
				}
			}

			// §3 target-typing, posicao 4: `campo.transform = identity` — o
			// tipo declarado do campo e o alvo concreto.
			if err := c.rewriteIfGenericValue(n.Value, fieldType); err != nil {
				return nil, nil, err
			}

			// 2. Compile Value
			_, valType, err := c.Compile(n.Value)
			if err != nil {
				return nil, nil, err
			}

			// TYPE-BASED ASSIGNMENT LOGIC:
			if fieldType != nil {
				// If Field is Ref, Value MUST be compatible Ref (Rebind).
				if refType, isRefField := fieldType.(*ast.RefType); isRefField {
					// Check Compatibility
					isRefVal := false
					if valType != nil {
						_, isRefVal = valType.(*ast.RefType)
					}

					// Assuming null is compatible
					if isRefVal || valType == nil || isNullType(valType) {
						if valType != nil && !c.areTypesCompatible(fieldType, valType) {
							return nil, nil, fmt.Errorf("[line %d] type mismatch in rebind: expected %s, got %s", c.currentLine, fieldType.String(), valType.String())
						}
					} else if c.areTypesCompatible(refType.ElementType, valType) {
						return nil, nil, referenceSlotAssignmentTypeError(c.currentLine, assignmentTargetName(memberExp), "field", fieldType, valType)
					} else {
						return nil, nil, fmt.Errorf("[line %d] type mismatch in rebind: expected %s, got %s", c.currentLine, fieldType.String(), valType.String())
					}
				} else {
					// Standard Field
					if !c.areTypesCompatible(fieldType, valType) {
						return nil, nil, fmt.Errorf("[line %d] type mismatch in field assignment: expected %s, got %s%s", c.currentLine, fieldType.String(), valType.String(), c.derefReadHint(fieldType, valType, n.Value))
					}
				}
				if err := c.emitRuntimeValueType(fieldType); err != nil {
					return nil, nil, err
				}
			}

			// Field Name
			nameConst := c.makeConstant(value.NewString(memberExp.Member))
			c.emitOpWithConstantIndex(chunk.OP_SET_PROPERTY, nameConst)
			c.emitByte(byte(chunk.OP_POP))

		} else {
			return nil, nil, fmt.Errorf("[line %d] assignment target not supported yet", c.currentLine)
		}
		return c.currentChunk, nil, nil

	case *ast.StructStatement:
		if len(n.TypeParams) > 0 {
			if c.scopeDepth > 0 || c.enclosing != nil {
				return nil, nil, fmt.Errorf("[line %d] declaração genérica só é permitida no top level", n.Token.Line)
			}
			if err := c.registerStructTemplate(n.Name, &StructTemplate{Decl: n, Module: c.moduleName}, n.Token.Line); err != nil {
				return nil, nil, err
			}
			return c.currentChunk, nil, nil
		}
		c.setLine(n.Token.Line)
		// §4, terceira familia de hooks: um campo pode anotar um struct generico
		// (`c: Caixa<int>`), inclusive num struct comum. Resolve in-place nos dois
		// espelhos antes de derivar o valor do struct e o tipo do construtor.
		if err := c.resolveStructFieldAnnotations(n, n.Token.Line); err != nil {
			return nil, nil, err
		}
		if c.scopeDepth > 0 {
			prior, existed := c.structs[n.Name]
			c.scopedStructs = append(c.scopedStructs, scopedStructBinding{
				Depth:   c.scopeDepth,
				Name:    n.Name,
				Prior:   prior,
				Existed: existed,
			})
			c.structs[n.Name] = n
		}

		fields := []string{}
		for _, f := range n.FieldsList {
			fields = append(fields, f.Name)
		}
		structObj := value.NewStruct(n.Name, fields)
		structDefinition := structObj.Obj.(*value.ObjStruct)
		structDefinition.JSONDynamicFields = make(map[string]bool)
		for _, field := range n.FieldsList {
			if primitive, ok := field.Type.(*ast.PrimitiveType); ok && primitive.Name == "any" {
				structDefinition.JSONDynamicFields[field.Name] = true
			}
			// RefFields: schema de runtime do slot ref (spec
			// 2026-08-20-ref-slot-invariant §6.1); nil quando o struct nao
			// tem campo ref.
			if _, isRef := field.Type.(*ast.RefType); isRef {
				if structDefinition.RefFields == nil {
					structDefinition.RefFields = make(map[string]bool)
				}
				structDefinition.RefFields[field.Name] = true
			}
		}
		c.emitConstant(structObj)

		// Create Constructor Signature
		paramTypes := []ast.NoxyType{}
		for _, f := range n.FieldsList {
			paramTypes = append(paramTypes, f.Type)
		}
		structType := newStructFunctionType(n.Name, paramTypes)
		structDefinition.ConstructorType = c.runtimeTypeInfo(structType)

		if c.scopeDepth > 0 {
			// Local scope: struct is a local variable
			c.addLocal(n.Name, structType)
			// Value stays on stack as local
		} else {
			// Global scope: struct is a global
			c.globals[n.Name] = structType
			// Register struct definition for field lookup
			c.structs[n.Name] = n

			nameConst := c.makeConstant(value.NewString(n.Name))
			c.emitOpWithConstantIndex(chunk.OP_SET_GLOBAL, nameConst)
			c.emitByte(byte(chunk.OP_POP))
		}
		return c.currentChunk, nil, nil

	case *ast.MemberAccessExpression:
		// Left . Member
		_, leftType, err := c.Compile(n.Left)
		if err != nil {
			return nil, nil, err
		}

		// Auto-dereference if left is a Ref
		if ref, ok := leftType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
			leftType = ref.ElementType
		}

		nameConst := c.makeConstant(value.NewString(n.Member))
		c.emitOpWithConstantIndex(chunk.OP_GET_PROPERTY, nameConst)

		// RESOLVE FIELD TYPE:
		// Look up struct definition if leftType is a named PrimitiveType
		if prim, ok := leftType.(*ast.PrimitiveType); ok {
			if structDef, exists := c.structs[prim.Name]; exists {
				// Find field type
				for _, f := range structDef.FieldsList {
					if f.Name == n.Member {
						return c.currentChunk, f.Type, nil
					}
				}
				// Field not found logic? (or let runtime handle if dynamic)
				// For strict structs, this should probably be an error, but let's return nil (dynamic) if not found.
			}
		}

		return c.currentChunk, nil, nil

	case *ast.ArrayLiteral:
		// §3 target-typing, posicao 3: consome o hint armado pelo `let`
		// envolvente (setArrayElementHint) ANTES de compilar qualquer
		// elemento — leitura unica, para nao vazar num array literal aninhado
		// dentro de um elemento deste.
		elementHint := c.arrayElementHint
		c.arrayElementHint = nil
		var elemType ast.NoxyType
		for i, el := range n.Elements {
			if elementHint != nil {
				if err := c.rewriteIfGenericValue(el, elementHint); err != nil {
					return nil, nil, err
				}
			}
			_, t, err := c.Compile(el)
			if err != nil {
				return nil, nil, err
			}
			if i == 0 {
				elemType = t
			} else {
				if !c.areTypesCompatible(elemType, t) {
					elemType = commonInferredType(elemType, t)
				}
			}
		}
		// Count
		count := len(n.Elements)
		if count > 65535 {
			return nil, nil, fmt.Errorf("[line %d] array literal too large", c.currentLine)
		}
		c.emitByte(byte(chunk.OP_ARRAY))
		c.emitByte(byte((count >> 8) & 0xff))
		c.emitByte(byte(count & 0xff))

		return c.currentChunk, &ast.ArrayType{ElementType: elemType, Size: count}, nil

	case *ast.MapLiteral:
		// Push keys and values: k1, v1, k2, v2, ...
		var keyType ast.NoxyType
		var valType ast.NoxyType

		for i, key := range n.Keys {
			_, kt, err := c.Compile(key)
			if err != nil {
				return nil, nil, err
			}
			_, vt, err := c.Compile(n.Values[i])
			if err != nil {
				return nil, nil, err
			}

			if i == 0 {
				keyType = kt
				valType = vt
			} else {
				if !c.areTypesCompatible(keyType, kt) {
					return nil, nil, fmt.Errorf("[line %d] mixed key types in map", c.currentLine)
				}
				if !c.areTypesCompatible(valType, vt) {
					valType = commonInferredType(valType, vt)
					// Once valType is "any", areTypesCompatible(any, T) returns true.
				}
			}
		}
		count := len(n.Keys)
		if count > 65535 {
			return nil, nil, fmt.Errorf("[line %d] map literal too large", c.currentLine)
		}
		c.emitByte(byte(chunk.OP_MAP))
		c.emitByte(byte((count >> 8) & 0xff))
		c.emitByte(byte(count & 0xff))

		return c.currentChunk, &ast.MapType{KeyType: keyType, ValueType: valType}, nil

	case *ast.IndexExpression:
		_, leftType, err := c.Compile(n.Left)
		if err != nil {
			return nil, nil, err
		}

		// Auto-dereference collection if Ref
		if _, ok := leftType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
			// leftType unwrapping happens below in the original code logic implicitly via re-check?
			// Actually the original code does unwrapping explicitly at line 448.
			// But we need to emit OP_DEREF *before* compilation of index?
			// No, standard order: Compile Left, Compile Index.
			// So we deref Left now.
		}

		_, idxType, err := c.Compile(n.Index)
		if err != nil {
			return nil, nil, err
		}

		// Auto-dereference index if Ref
		if _, ok := idxType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
		}

		// Unwrap RefType in index
		if ref, ok := idxType.(*ast.RefType); ok {
			idxType = ref.ElementType
		}

		// Index should be int (usually)
		if idxType != nil && idxType.String() != "int" {
			// Warn or Error? Error.
			// return nil, nil, fmt.Errorf("index must be int, got %s", idxType)
		}

		c.emitByte(byte(chunk.OP_GET_INDEX))

		// Result Type: Element type of array
		// Unwrap RefType (getting index from ref array)
		if ref, ok := leftType.(*ast.RefType); ok {
			leftType = ref.ElementType
		}
		if arrKey, ok := leftType.(*ast.ArrayType); ok {
			return c.currentChunk, arrKey.ElementType, nil
		}
		// Map logic needed here too? index on map?
		if mapKey, ok := leftType.(*ast.MapType); ok {
			return c.currentChunk, mapKey.ValueType, nil
		}

		return c.currentChunk, nil, nil

	case *ast.Identifier:
		// Check local
		if arg, t := c.resolveLocal(n.Value); arg != -1 {
			c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(arg))
			return c.currentChunk, t, nil
		} else if arg, upvalueType := c.resolveUpvalue(n.Value); arg != -1 {
			c.emitBytes(byte(chunk.OP_GET_UPVALUE), byte(arg))
			return c.currentChunk, upvalueType, nil
		} else {
			// Global
			// §9/§4: fallback para as posicoes de valor que nenhum hook de
			// genericos intercepta (map literal, expression statement solto)
			// — ver o comentario de rejectBareGenericTemplateIdentifier em
			// generics_target.go. Sombreamento por local/upvalue ja foi
			// descartado pelos dois ramos acima, entao a mesma regra vale
			// aqui sem checagem extra.
			if err := c.rejectBareGenericTemplateIdentifier(n); err != nil {
				return nil, nil, err
			}
			nameConstant := c.makeConstant(value.NewString(n.Value))
			c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, nameConstant)

			if t, ok := c.globals[n.Value]; ok {
				return c.currentChunk, t, nil
			}
			return c.currentChunk, nil, nil // Unknown global currently
		}

	case *ast.InfixExpression:
		// Short-circuit Logic
		if n.Operator == "&&" {
			_, leftType, err := c.Compile(n.Left)
			if err != nil {
				return nil, nil, err
			}
			endJump := c.emitJump(chunk.OP_JUMP_IF_FALSE)
			c.emitByte(byte(chunk.OP_POP))
			_, rightType, err := c.Compile(n.Right)
			if err != nil {
				return nil, nil, err
			}
			c.patchJump(endJump)
			if !c.areTypesCompatible(&ast.PrimitiveType{Name: "bool"}, leftType) || !c.areTypesCompatible(&ast.PrimitiveType{Name: "bool"}, rightType) {
				l := "nil"
				if leftType != nil {
					l = leftType.String()
				}
				r := "nil"
				if rightType != nil {
					r = rightType.String()
				}
				return nil, nil, fmt.Errorf("[line %d] logical operators require boolean operands, got %s and %s", c.currentLine, l, r)
			}

			return c.currentChunk, &ast.PrimitiveType{Name: "bool"}, nil
		}
		if n.Operator == "||" {
			_, _, err := c.Compile(n.Left)
			if err != nil {
				return nil, nil, err
			}
			endJump := c.emitJump(chunk.OP_JUMP_IF_TRUE)
			c.emitByte(byte(chunk.OP_POP))
			_, _, err = c.Compile(n.Right)
			if err != nil {
				return nil, nil, err
			}
			c.patchJump(endJump)

			return c.currentChunk, &ast.PrimitiveType{Name: "bool"}, nil
		}

		// Em `==`/`!=` um operando ref NUNCA e dereferenciado
		// implicitamente (spec §2.3, excecao 1): dois refs chegam inteiros
		// ate OP_EQUAL para comparar IDENTIDADE DE SLOT (§2.2.7), ref vs
		// null pergunta sobre o PROPRIO ref, e o caso misto estatico
		// ref vs valor e rejeitado logo abaixo (rejectMixedRefComparison)
		// com hint para o deref explicito. Todos os demais operadores
		// seguem dereferenciando neste ponto.
		identityComparison := n.Operator == "==" || n.Operator == "!="

		_, leftType, err := c.Compile(n.Left)
		if err != nil {
			return nil, nil, err
		}

		if _, ok := leftType.(*ast.RefType); ok && !identityComparison {
			// Always deref ref types before comparison (including null comparison)
			// This ensures 'ref Node == null' compares the pointed-to value, not the ref itself
			c.emitByte(byte(chunk.OP_DEREF))
			if ref, ok := leftType.(*ast.RefType); ok {
				leftType = ref.ElementType
			}
		}

		_, rightType, err := c.Compile(n.Right)
		if err != nil {
			return nil, nil, err
		}

		if _, ok := rightType.(*ast.RefType); ok && !identityComparison {
			// Always deref ref types before comparison (including null comparison)
			c.emitByte(byte(chunk.OP_DEREF))
			if ref, ok := rightType.(*ast.RefType); ok {
				rightType = ref.ElementType
			}
		}

		// Igualdade estrita de ref: no caso MISTO (um lado estaticamente
		// `ref T`, o outro um valor conhecido nao-ref) `==`/`!=` nao le o
		// valor implicitamente — o deref e por conta do programador, como
		// no `=`. null (pergunta sobre o proprio ref), `any` e tipos
		// desconhecidos (fronteira dinamica, que pode carregar um ref em
		// runtime) passam; dois refs comparam identidade em runtime.
		if identityComparison {
			if err := c.rejectMixedRefComparison(n, leftType, rightType); err != nil {
				return nil, nil, err
			}
		}

		// Structs nunca definem operador aritmetico (OP_ADD e companhia, em
		// runtime, so aceitam numeros — e OP_ADD tambem strings/bytes — nunca
		// ObjStruct: cai no "operands must be numbers..." generico do
		// executor). Sem esta checagem, 'a + b' com a,b struct compilava
		// silenciosamente e so estourava no runtime; dentro do corpo de uma
		// instancia generica monomorfizada isso escapava por completo da
		// cadeia de instanciacao do §9 (instantiationChainError so envolve
		// ERROS DE COMPILACAO). Pegar aqui cedo produz a mensagem exata do
		// catalogo, com a linha do proprio operador.
		if arithmeticOperators[n.Operator] {
			if structName, isStruct := c.structOperandName(leftType, rightType); isStruct {
				return nil, nil, fmt.Errorf("[line %d] operador '%s' não definido para %s", n.Token.Line, n.Operator, structName)
			}
		}

		// Check if both operands are INT for optimization
		isInt := false
		if leftType != nil && rightType != nil {
			if leftType.String() == "int" && rightType.String() == "int" {
				isInt = true
			}
		}

		// Irmao float de isInt: so dispara quando AMBOS os lados sao
		// estaticamente float. Mistos int/float ficam no caminho generico,
		// que ja faz a promocao numerica.
		isFloat := false
		if leftType != nil && rightType != nil {
			if leftType.String() == "float" && rightType.String() == "float" {
				isFloat = true
			}
		}

		switch n.Operator {
		case "+":
			if isInt {
				c.emitByte(byte(chunk.OP_ADD_INT))
			} else if isFloat {
				c.emitByte(byte(chunk.OP_ADD_FLOAT))
			} else {
				c.emitByte(byte(chunk.OP_ADD))
			}
		case "-":
			if isInt {
				c.emitByte(byte(chunk.OP_SUB_INT))
			} else if isFloat {
				c.emitByte(byte(chunk.OP_SUB_FLOAT))
			} else {
				c.emitByte(byte(chunk.OP_SUBTRACT))
			}
		case "*":
			if isInt {
				c.emitByte(byte(chunk.OP_MUL_INT))
			} else if isFloat {
				c.emitByte(byte(chunk.OP_MUL_FLOAT))
			} else {
				c.emitByte(byte(chunk.OP_MULTIPLY))
			}
		case "/":
			if isInt {
				c.emitByte(byte(chunk.OP_DIV_INT))
			} else if isFloat {
				c.emitByte(byte(chunk.OP_DIV_FLOAT))
			} else {
				c.emitByte(byte(chunk.OP_DIVIDE))
			}
		case ">":
			if isInt {
				c.emitByte(byte(chunk.OP_GREATER_INT))
			} else if isFloat {
				c.emitByte(byte(chunk.OP_GREATER_FLOAT))
			} else {
				c.emitByte(byte(chunk.OP_GREATER))
			}
		case "<":
			if isInt {
				c.emitByte(byte(chunk.OP_LESS_INT))
			} else if isFloat {
				c.emitByte(byte(chunk.OP_LESS_FLOAT))
			} else {
				c.emitByte(byte(chunk.OP_LESS))
			}
		case "==":
			if isInt {
				c.emitByte(byte(chunk.OP_EQUAL_INT))
			} else {
				c.emitByte(byte(chunk.OP_EQUAL))
			}
		case "!=": // != is NOT EQUAL
			// Optimized != is !(==)
			if isInt {
				c.emitByte(byte(chunk.OP_EQUAL_INT))
			} else {
				c.emitByte(byte(chunk.OP_EQUAL))
			}
			c.emitByte(byte(chunk.OP_NOT))
		case ">=": // >= is NOT LESS
			if isInt {
				c.emitByte(byte(chunk.OP_LESS_INT))
			} else {
				c.emitByte(byte(chunk.OP_LESS))
			}
			c.emitByte(byte(chunk.OP_NOT))
		case "<=": // <= is NOT GREATER
			if isInt {
				c.emitByte(byte(chunk.OP_GREATER_INT))
			} else {
				c.emitByte(byte(chunk.OP_GREATER))
			}
			c.emitByte(byte(chunk.OP_NOT))
		case "|":
			c.emitByte(byte(chunk.OP_BIT_OR))
		case "&":
			c.emitByte(byte(chunk.OP_BIT_AND))
		case "^":
			c.emitByte(byte(chunk.OP_BIT_XOR))
		case "<<":
			c.emitByte(byte(chunk.OP_SHIFT_LEFT))
		case ">>":
			c.emitByte(byte(chunk.OP_SHIFT_RIGHT))
		case "%":
			if isInt {
				c.emitByte(byte(chunk.OP_MOD_INT))
			} else {
				c.emitByte(byte(chunk.OP_MODULO))
			}
		default:
			return nil, nil, fmt.Errorf("unknown operator %s", n.Operator)
		}

		// Return type logic
		if n.Operator == "==" || n.Operator == "!=" || n.Operator == ">" || n.Operator == "<" || n.Operator == ">=" || n.Operator == "<=" {
			return c.currentChunk, &ast.PrimitiveType{Name: "bool"}, nil
		}

		// Arithmetic: if either is float, result is float
		isFloatObj := false
		if leftType != nil && leftType.String() == "float" {
			isFloatObj = true
		}
		if rightType != nil && rightType.String() == "float" {
			isFloatObj = true
		}

		if isFloatObj {
			return c.currentChunk, &ast.PrimitiveType{Name: "float"}, nil
		}

		// Match left type (int/int -> int)
		if c.areTypesCompatible(leftType, rightType) {
			return c.currentChunk, leftType, nil
		}
		// Fallback?
		return c.currentChunk, leftType, nil

	case *ast.PrefixExpression:
		// Handle 'ref' operator specially - don't compile Right first
		if n.Operator == "ref" {
			element, err := c.compileReferenceArgument(n.Right)
			if err != nil {
				return nil, nil, err
			}
			return c.currentChunk, &ast.RefType{ElementType: element}, nil
		}

		// For other operators (-, !, ~), compile Right first
		_, rightType, err := c.Compile(n.Right)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := rightType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
			if ref, ok := rightType.(*ast.RefType); ok {
				rightType = ref.ElementType
			}
		}
		if n.Operator == "-" {
			c.emitByte(byte(chunk.OP_NEGATE))
			return c.currentChunk, rightType, nil
		} else if n.Operator == "!" {
			c.emitByte(byte(chunk.OP_NOT))
			return c.currentChunk, &ast.PrimitiveType{Name: "bool"}, nil
		} else if n.Operator == "~" {
			c.emitByte(byte(chunk.OP_BIT_NOT))
			return c.currentChunk, rightType, nil
		}
		return c.currentChunk, rightType, nil

	case *ast.NullLiteral:
		c.emitByte(byte(chunk.OP_NULL))
		return c.currentChunk, &ast.PrimitiveType{Name: "null"}, nil

	case *ast.ZerosLiteral:
		_, _, err := c.Compile(n.Size)
		if err != nil {
			return nil, nil, err
		}
		c.emitByte(byte(chunk.OP_ZEROS))
		size := 0
		if literal, ok := n.Size.(*ast.IntegerLiteral); ok {
			size = int(literal.Value)
		}
		return c.currentChunk, &ast.ArrayType{
			ElementType: &ast.PrimitiveType{Name: "int"},
			Size:        size,
		}, nil

	case *ast.IfStatement:
		c.setLine(n.Token.Line)
		// Compile condition: fusao especulativa (comparacao int + salto) com
		// rollback para o caminho generico se um dos lados nao for int.
		fusedOp, fused, err := c.tryCompileFusedCondition(n.Condition)
		if err != nil {
			return nil, nil, err
		}
		var jumpToElse int
		if fused {
			jumpToElse = c.emitJump(fusedOp)
		} else {
			_, condType, err := c.Compile(n.Condition)
			if err != nil {
				return nil, nil, err
			}
			if _, ok := condType.(*ast.RefType); ok {
				c.emitByte(byte(chunk.OP_DEREF))
			}

			// Emit JumpIfFalse
			jumpToElse = c.emitJump(chunk.OP_JUMP_IF_FALSE)

			// Compile Then block (Consequence)
			c.emitByte(byte(chunk.OP_POP)) // Pop condition value (since we entered THEN)
		}

		_, _, err = c.Compile(n.Consequence)
		if err != nil {
			return nil, nil, err
		}

		// Emit Jump to End (skip Else)
		jumpToEnd := c.emitJump(chunk.OP_JUMP)

		// Patch Else jump
		c.patchJump(jumpToElse)

		if !fused {
			c.emitByte(byte(chunk.OP_POP)) // Pop condition value (if we jumped here, condition was false)
		}

		// Compile Else block (Alternative)
		if n.Alternative != nil {
			_, _, err = c.Compile(n.Alternative)
			if err != nil {
				return nil, nil, err
			}
		}

		// Patch End jump
		c.patchJump(jumpToEnd)
		return c.currentChunk, nil, nil

	case *ast.WhileStatement:
		c.setLine(n.Token.Line)
		loopStart := len(c.currentChunk.Code)

		// Push Loop
		loop := &Loop{EnclosingLocals: len(c.locals), BreakJumps: []int{}, ContinueTarget: loopStart}
		c.loops = append(c.loops, loop)

		// Compile condition: fusao especulativa (comparacao int + salto) com
		// rollback para o caminho generico se um dos lados nao for int.
		fusedOp, fused, err := c.tryCompileFusedCondition(n.Condition)
		if err != nil {
			return nil, nil, err
		}
		var jumpToExit int
		if fused {
			jumpToExit = c.emitJump(fusedOp)
		} else {
			_, condType, err := c.Compile(n.Condition)
			if err != nil {
				return nil, nil, err
			}
			if _, ok := condType.(*ast.RefType); ok {
				c.emitByte(byte(chunk.OP_DEREF))
			}

			// Exit jump
			jumpToExit = c.emitJump(chunk.OP_JUMP_IF_FALSE)

			c.emitByte(byte(chunk.OP_POP)) // Pop condition
		}

		_, _, err = c.Compile(n.Body)
		if err != nil {
			return nil, nil, err
		}

		// Loop back
		c.emitLoop(loopStart)

		c.patchJump(jumpToExit)
		if !fused {
			c.emitByte(byte(chunk.OP_POP)) // Pop condition at exit
		}

		// Patch Break Jumps
		for _, jump := range loop.BreakJumps {
			c.patchJump(jump)
		}

		// Pop Loop
		c.loops = c.loops[:len(c.loops)-1]
		return c.currentChunk, nil, nil

	case *ast.ForStatement:
		c.setLine(n.Token.Line)

		// 1. Wrapper Scope for iterator variables
		c.beginScope()

		// 2. Compile Collection
		_, colType, err := c.Compile(n.Collection)
		if err != nil {
			return nil, nil, err
		}

		// Handle Map: transform to keys array
		isMap := false
		if _, ok := colType.(*ast.MapType); ok {
			isMap = true
		}

		if isMap {
			// RC: o slot oculto $map EMPRESTA (sem OP_OWN_LOCAL) — ver a nota
			// do $collection abaixo; a mesma paridade vale para mutar o map
			// durante a iteracao pelas chaves.
			c.addLocal(" $map", colType) // Consumes Map from stack

			// Get 'keys' global
			nameConst := c.makeConstant(value.NewString("keys"))
			c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, nameConst)

			// Get '$map' local
			slot := len(c.locals) - 1 // The last local added
			c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(slot))

			// Call keys(map)
			c.emitBytes(byte(chunk.OP_CALL), 1)
		}

		// 3. Store Collection in Local ($collection)
		// RC: decisao deliberada — $collection (e $map) EMPRESTAM, nao possuem.
		// Possuir daria Owners+1 a colecao durante o laco, e `arr[i] = x` no
		// corpo passaria a CLONAR: a iteracao continuaria no array VELHO,
		// divergindo do merge-base (conferido no binario: a mutacao durante a
		// iteracao E observada pelos itens seguintes). O emprestimo aqui e
		// sound: slots ocultos (nome com espaco) sao inalcancaveis por
		// identificador do usuario — nenhum funil de escrita, captura ou `ref`
		// os alcanca, entao nunca ha release sobre eles (a maquinaria do laco
		// so LE estes slots). Escalares ($index/$len) seriam indiferentes
		// (Retain e no-op), mas ficam no mesmo regime por consistencia.
		c.addLocal(" $collection", nil)

		// 4. Init Index ($index = 0)
		c.emitConstant(value.NewInt(0))
		c.addLocal(" $index", &ast.PrimitiveType{Name: "int"})

		// 5. Init Length ($len = len($collection))
		c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-2)) // $collection is at -2 (since $index is at -1)
		c.emitByte(byte(chunk.OP_LEN))
		c.addLocal(" $len", &ast.PrimitiveType{Name: "int"})

		// 6. Loop Setup
		loopStart := len(c.currentChunk.Code)
		loop := &Loop{EnclosingLocals: len(c.locals), BreakJumps: []int{}, ContinueTarget: -1}
		c.loops = append(c.loops, loop)

		// 7. Condition: $index < $len
		c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-2)) // $index
		c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-1)) // $len
		c.emitByte(byte(chunk.OP_LESS_INT))

		// Exit Jump
		jumpToExit := c.emitJump(chunk.OP_JUMP_IF_FALSE)
		c.emitByte(byte(chunk.OP_POP)) // Pop condition

		// 8. Get Item -> User Variable
		c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-3)) // $collection
		c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-2)) // $index
		c.emitByte(byte(chunk.OP_GET_INDEX))

		// Body Scope
		c.beginScope()
		// RC (spec §4.2): TODO vinculo local de composto retem — a variavel de
		// for-each nao e excecao. O bind roda DENTRO do corpo, entao o
		// OP_OWN_LOCAL executa a cada iteracao: cada elemento recebe exatamente
		// um retain aqui, e o release e pago pelo bind da iteracao seguinte
		// (bindOwnedSlot solta o objeto da entrada anterior do slot) ou pelo
		// fim do frame (ultimo elemento). Colecao vazia: o bind nunca roda —
		// nem retain, nem entrada, nem release. Com o slot possuidor desde o
		// nascimento, a captura por closure produz caixa possuidora e o rebind
		// usa o store contado — Owns volta a coincidir com "tipo nao-ref".
		c.emitByte(byte(chunk.OP_OWN_LOCAL))
		// Ruling R5: a variavel do laco recebe o tipo do elemento da colecao
		// quando ele e estaticamente conhecido (array -> elemento, map -> chave,
		// que e o que o laco produz depois da reescrita para `keys(m)`). Colecao
		// de tipo desconhecido continua produzindo variavel sem tipo — nil aqui
		// significa "nao sei", como antes. Sem isso nenhuma chamada generica
		// ancorada na variavel do laco consegue inferir T.
		c.addOwnedLocal(n.Identifier, forEachElementType(colType)) // User variable (consumes Item from stack)

		// 9. Compile Body
		_, _, err = c.Compile(n.Body)
		if err != nil {
			return nil, nil, err
		}

		c.endScope() // Pops User Variable

		// continue: chega aqui com a mesma pilha da saida normal do corpo
		// ([$collection, $index, $len]) — a variavel do laco e os locais do
		// corpo ja foram descartados pelo emitLocalsExit do ContinueStmt.
		for _, jump := range loop.ContinueJumps {
			c.patchJump(jump)
		}

		// 10. Increment Index
		c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-2)) // $index
		c.emitConstant(value.NewInt(1))
		c.emitByte(byte(chunk.OP_ADD_INT))
		c.emitBytes(byte(chunk.OP_SET_LOCAL), byte(len(c.locals)-2)) // set $index
		c.emitByte(byte(chunk.OP_POP))

		// 11. Loop Back
		c.emitLoop(loopStart)

		// 12. Patch Exit
		c.patchJump(jumpToExit)
		c.emitByte(byte(chunk.OP_POP)) // Pop condition at exit

		// 13. Patch Break Jumps (mesmo tratamento do while)
		// O alvo tem de ficar DEPOIS do pop da condicao e ANTES do endScope: no
		// break a condicao nao esta na pilha (o BreakStmt so estoura os locais
		// alem de EnclosingLocals, isto e, a variavel do laco e o que o corpo
		// declarou), entao o break chega aqui com a mesma pilha da saida normal
		// — [$collection, $index, $len] — e paga os pops do escopo wrapper
		// junto com ela.
		for _, jump := range loop.BreakJumps {
			c.patchJump(jump)
		}

		// 14. Pop Loop — sem isto o for continua sendo o laco "corrente" depois
		// do seu fim: um break posterior miraria este laco ja encerrado (jump
		// nunca patchado, operando 0xffff) e `break` fora de laco deixaria de
		// ser erro de compilacao.
		c.loops = c.loops[:len(c.loops)-1]

		c.endScope() // Close Wrapper Scope ($collection, $index, $len)

		return c.currentChunk, nil, nil

	case *ast.WhenStatement:
		c.setLine(n.Token.Line)

		// 1. Compile Cases setup
		// We need to push 3 values per case: [Channel, Value, Mode]
		// Mode: 0=Recv, 1=Send, 2=Default

		// Map case index to jump/body logic later
		type CaseInfo struct {
			Index int
			Node  *ast.CaseClause
		}
		cases := []CaseInfo{}

		for i, cc := range n.Cases {
			cases = append(cases, CaseInfo{Index: i, Node: cc})

			if cc.IsDefault {
				// Default Case: [Null, Null, 2]
				c.emitByte(byte(chunk.OP_NULL)) // Chan
				c.emitByte(byte(chunk.OP_NULL)) // Val
				c.emitConstant(value.NewInt(2)) // Mode
				continue
			}

			// Check Condition
			// Can be AssignStmt (Recv) or ExpressionStmt (Recv or Send)
			var callExpr *ast.CallExpression
			var isAssign bool
			// var assignTarget ast.Expression // Removed

			if assign, ok := cc.Condition.(*ast.AssignStmt); ok {
				isAssign = true
				_ = assign.Target // Suppress unused for now
				// Value should be CallExpression recv(c)
				if call, ok := assign.Value.(*ast.CallExpression); ok {
					callExpr = call
				}
			} else if exprStmt, ok := cc.Condition.(*ast.ExpressionStmt); ok {
				if call, ok := exprStmt.Expression.(*ast.CallExpression); ok {
					callExpr = call
				}
			}
			// Note: If assignment, it MUST be recv.
			// If ExpressionStmt, can be recv (discard result) or send.

			if callExpr == nil {
				return nil, nil, fmt.Errorf("[line %d] invalid case condition: expected chan_send(...) or chan_recv(...)", c.currentLine)
			}

			funcName := ""
			if ident, ok := callExpr.Function.(*ast.Identifier); ok {
				funcName = ident.Value
			}

			if funcName == "chan_recv" {
				// Recv Case: [Chan, Null, 0]
				if len(callExpr.Arguments) != 1 {
					return nil, nil, fmt.Errorf("[line %d] chan_recv expects 1 argument", c.currentLine)
				}
				// Compile Channel
				_, _, err := c.Compile(callExpr.Arguments[0])
				if err != nil {
					return nil, nil, err
				}

				c.emitByte(byte(chunk.OP_NULL)) // Val (unused for recv)
				c.emitConstant(value.NewInt(0)) // Mode 0

			} else if funcName == "chan_send" {
				// Send Case: [Chan, Val, 1]
				if isAssign {
					return nil, nil, fmt.Errorf("[line %d] cannot assign result of chan_send", c.currentLine)
				}
				if len(callExpr.Arguments) != 2 {
					return nil, nil, fmt.Errorf("[line %d] chan_send expects 2 arguments", c.currentLine)
				}
				// Compile Channel
				_, _, err := c.Compile(callExpr.Arguments[0])
				if err != nil {
					return nil, nil, err
				}
				// Compile Value
				_, _, err = c.Compile(callExpr.Arguments[1])
				if err != nil {
					return nil, nil, err
				}

				c.emitConstant(value.NewInt(1)) // Mode 1

			} else {
				return nil, nil, fmt.Errorf("[line %d] invalid case call: expected chan_send or chan_recv, got %s", c.currentLine, funcName)
			}
		}

		// 2. Emit OP_SELECT
		count := len(n.Cases)
		if count > 255 {
			return nil, nil, fmt.Errorf("too many cases in when statement")
		}
		c.emitBytes(byte(chunk.OP_SELECT), byte(count))

		c.beginScope()
		// Determine types? Dynamic.
		// RC: slots ocultos do select EMPRESTAM (mesma nota do $collection do
		// for-each): inalcancaveis pelo usuario, apenas lidos pela maquinaria.
		// O binding de case (visivel) e quem retem, via OP_OWN_LOCAL abaixo.
		c.addLocal(" $sel_idx", &ast.PrimitiveType{Name: "int"}) // Stack[-3] -> local 0
		c.addLocal(" $sel_val", nil)                             // Stack[-2] -> local 1
		c.addLocal(" $sel_ok", &ast.PrimitiveType{Name: "bool"}) // Stack[-1] -> local 2

		idxSlot := len(c.locals) - 3
		valSlot := len(c.locals) - 2
		// okSlot := len(c.locals) - 1

		endJumps := []int{}

		for i, cc := range n.Cases {
			// Check if Index == i
			c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(idxSlot))
			c.emitConstant(value.NewInt(int64(i)))
			c.emitByte(byte(chunk.OP_EQUAL_INT))

			nextJump := c.emitJump(chunk.OP_JUMP_IF_FALSE)
			c.emitByte(byte(chunk.OP_POP)) // Pop comparison result

			// Body
			c.beginScope() // Scope for case body

			// If Assignment: bind Value to var
			if assign, ok := cc.Condition.(*ast.AssignStmt); ok {
				ident := assign.Target.(*ast.Identifier)
				// Create local with value from $sel_val
				c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(valSlot))
				// RC (spec §4.2): o binding de case retem como qualquer let —
				// um `when` dentro de laco reexecuta o bind, e o bindOwnedSlot
				// paga a entrada anterior do slot (mesma mecanica do for-each).
				c.emitByte(byte(chunk.OP_OWN_LOCAL))
				c.addOwnedLocal(ident.Value, nil) // Bind local
			}

			// Compile Block
			_, _, err := c.Compile(cc.Body)
			if err != nil {
				return nil, nil, err
			}

			c.endScope() // Pop case locals

			jumpToEnd := c.emitJump(chunk.OP_JUMP)
			endJumps = append(endJumps, jumpToEnd)

			c.patchJump(nextJump)          // Patch jump to next comparison
			c.emitByte(byte(chunk.OP_POP)) // Pop comparison result (from IF_FALSE jump? No, IF_FALSE doesn't pop in Noxy VM? Yes it does? Check VM.)
		}

		// Fallthrough if no case matched? (Should be impossible if SELECT works)
		// But in case of weirdness, cleanup stack?
		// Locals will be popped by endScope.
		// The 3 hidden locals ($sel_idx, etc) will be popped.

		// Patch all end jumps
		for _, jump := range endJumps {
			c.patchJump(jump)
		}

		c.endScope() // Pops $sel_idx, $sel_val, $sel_ok (3 values)

		return c.currentChunk, nil, nil

	case *ast.BreakStmt:
		if len(c.loops) == 0 {
			return nil, nil, fmt.Errorf("break outside of loop")
		}
		loop := c.loops[len(c.loops)-1]

		// Pop locals
		c.emitLocalsExit(loop.EnclosingLocals)

		// Emit Jump
		jump := c.emitJump(chunk.OP_JUMP)
		loop.BreakJumps = append(loop.BreakJumps, jump)
		return c.currentChunk, nil, nil

	case *ast.ContinueStmt:
		if len(c.loops) == 0 {
			return nil, nil, fmt.Errorf("continue outside of loop")
		}
		loop := c.loops[len(c.loops)-1]
		c.emitLocalsExit(loop.EnclosingLocals)
		if loop.ContinueTarget >= 0 {
			c.emitLoop(loop.ContinueTarget)
		} else {
			loop.ContinueJumps = append(loop.ContinueJumps, c.emitJump(chunk.OP_JUMP))
		}
		return c.currentChunk, nil, nil

	case *ast.UseStmt:
		// §9/I1: `use` aninhado (corpo de funcao/lambda) que traga um template
		// generico e recusado com mensagem acionavel ANTES de qualquer emissao
		// — ver rejectNestedTemplateImport. Sem isto o template entrava no
		// registry no meio da compilacao e a chamada batia no guard defensivo
		// do pass 2, que acusa "bug do compilador" com a linha errada.
		if err := c.rejectNestedTemplateImport(n); err != nil {
			return nil, nil, err
		}

		// 1. Emit Module Name
		nameConst := c.makeConstant(value.NewString(n.Module))
		// 2. Emit Import (Loads module and pushes it to stack)
		c.emitOpWithConstantIndex(chunk.OP_IMPORT, nameConst)

		// 3. Handle Result
		if n.SelectAll {
			// use pkg select *
			exports, loadable := c.discoverModuleExports(n.Module)
			if !loadable && c.enclosing == nil {
				return nil, nil, fmt.Errorf("[line %d] failed to resolve wildcard module '%s'", n.Token.Line, n.Module)
			}
			// §8/R8: templates genericos exportados entram no registry
			// (Module = modulo definidor) em vez de c.globals — importBindingFrom
			// decide por declaracao. Templates nunca tem valor em runtime (a
			// declaracao original nao emite bytecode nenhum), entao ficam
			// ausentes do ExportMap que OP_IMPORT_FROM_ALL le abaixo — sem
			// tratamento especial aqui: a instrucao so importa as chaves que
			// EXISTEM no map do modulo, e simplesmente nao ha chave "processa".
			bindings, _ := c.moduleTopLevelBindings(n.Module)
			for name := range exports {
				if err := c.importBindingFrom(n.Module, bindings, name); err != nil {
					return nil, nil, err
				}
			}
			c.importModuleStructs(n.Module, nil)
			c.emitByte(byte(chunk.OP_IMPORT_FROM_ALL))
		} else if len(n.Selectors) > 0 {
			// use pkg select a, b
			c.importModuleStructs(n.Module, n.Selectors)
			bindings, _ := c.moduleTopLevelBindings(n.Module)
			for _, sel := range n.Selectors {
				if err := c.importBindingFrom(n.Module, bindings, sel); err != nil {
					return nil, nil, err
				}
				if isModuleTemplateDeclaration(bindings, sel) {
					// §8: template generico nao existe como valor em runtime
					// (sem bytecode proprio) — buscar "sel" como propriedade do
					// objeto do modulo lancaria "undefined property" (o
					// ExportMap nunca tem essa chave). O registro acima
					// (importBindingFrom, no GenericRegistry) e tudo que este
					// nome precisa: a instancia concreta nasce como declaracao
					// sintetica comum no two-pass do IMPORTADOR.
					continue
				}
				// DUP the module
				c.emitByte(byte(chunk.OP_DUP))

				// Get Property 'sel'
				selConst := c.makeConstant(value.NewString(sel))
				c.emitOpWithConstantIndex(chunk.OP_GET_PROPERTY, selConst)

				// Set Global 'sel'
				c.emitOpWithConstantIndex(chunk.OP_SET_GLOBAL, selConst)
				c.emitByte(byte(chunk.OP_POP)) // Pop the set value
			}
			// Pop the original Module
			c.emitByte(byte(chunk.OP_POP))
		} else {
			// use pkg.mod [as alias]
			var bindName string
			if n.Alias != "" {
				bindName = n.Alias
			} else {
				// Default: last part of module path
				parts := strings.Split(n.Module, ".")
				if len(parts) > 0 {
					bindName = parts[len(parts)-1]
				} else {
					bindName = n.Module
				}
			}

			nameConst := c.makeConstant(value.NewString(bindName))
			c.importNamespace(n.Module, bindName)
			c.emitOpWithConstantIndex(chunk.OP_SET_GLOBAL, nameConst)
			c.emitByte(byte(chunk.OP_POP)) // Pop module
		}
		return c.currentChunk, nil, nil

	case *ast.ReturnStmt:
		expected := c.funcReturnType
		functionName := c.currentFunctionName

		if n.ReturnValue == nil {
			if expected != nil && expected.String() != "void" {
				return nil, nil, fmt.Errorf(
					"[line %d] function '%s' must return %s",
					n.Token.Line, functionName, expected.String(),
				)
			}
			c.emitByte(byte(chunk.OP_NULL))
			c.emitByte(byte(chunk.OP_RETURN))
			return c.currentChunk, nil, nil
		}

		// §3 target-typing, posicao 2: `return identity` dentro de uma funcao
		// cujo retorno declarado e um tipo de funcao concreto instancia a
		// genérica antes da compilacao normal do valor de retorno.
		if err := c.rewriteIfGenericValue(n.ReturnValue, expected); err != nil {
			return nil, nil, err
		}

		// Target-typing do §7 em posicao de return: a anotacao de retorno da
		// funcao envolvente e ancora para o T que so aparece no retorno do
		// template chamado (`return vazia()`), simetrica a ancora do `let`
		// anotado — mesma disciplina de armar/limpar do LetStmt acima.
		c.setGenericReturnHint(expected, n.ReturnValue)
		c.setArrayElementHint(expected, n.ReturnValue)
		_, actual, err := c.Compile(n.ReturnValue)
		c.genericReturnHint = nil
		c.arrayElementHint = nil
		if err != nil {
			return nil, nil, err
		}
		if expected == nil {
			c.emitByte(byte(chunk.OP_RETURN))
			return c.currentChunk, nil, nil
		}
		if expected.String() == "void" {
			return nil, nil, fmt.Errorf(
				"[line %d] void function '%s' cannot return %s",
				n.Token.Line, functionName, noxyTypeName(actual),
			)
		}
		if ref, ok := actual.(*ast.RefType); ok {
			if _, expectsRef := expected.(*ast.RefType); !expectsRef {
				c.emitByte(byte(chunk.OP_DEREF))
				c.emitByte(byte(chunk.OP_COPY))
				actual = ref.ElementType
			}
		}
		if !c.areStrictTypesCompatible(expected, actual) {
			return nil, nil, fmt.Errorf(
				"[line %d] return type mismatch in '%s': expected %s, got %s",
				n.Token.Line, functionName, expected.String(), noxyTypeName(actual),
			)
		}
		if err := c.emitRuntimeValueType(expected); err != nil {
			return nil, nil, err
		}
		c.emitByte(byte(chunk.OP_RETURN))
		return c.currentChunk, nil, nil

	case *ast.FunctionStatement:
		if len(n.TypeParams) > 0 {
			if c.scopeDepth > 0 || c.enclosing != nil {
				return nil, nil, fmt.Errorf("[line %d] declaração genérica só é permitida no top level", n.Token.Line)
			}
			if err := c.registerFuncTemplate(n.Name, &FuncTemplate{Decl: n, Module: c.moduleName}, n.Token.Line); err != nil {
				return nil, nil, err
			}
			return c.currentChunk, nil, nil
		}
		c.setLine(n.Token.Line)

		// §4, terceira familia de hooks: parametro/retorno de funcao NAO-generica
		// podem anotar um struct generico (`func conta(cs: Caixa<int>[]) -> int`).
		// Resolve antes de registrar o tipo do global — o tipo publicado tem de ser
		// o mesmo que o pass 2 vai derivar do AST reescrito.
		if err := c.resolveSignatureAnnotations(n.Parameters, &n.ReturnType, n.Token.Line); err != nil {
			return nil, nil, err
		}
		c.globals[n.Name] = newFunctionType(n.Parameters, n.ReturnType)

		fnObj, fnCompiler, err := c.compileFunction(n.Name, n.Parameters, n.Body, n.ReturnType)
		if err != nil {
			return nil, nil, err
		}

		funcIndex := c.makeConstant(fnObj)
		c.emitOpWithConstantIndex(chunk.OP_CLOSURE, funcIndex)
		c.emitClosureUpvalues(fnCompiler)

		nameConst := c.makeConstant(value.NewString(n.Name))
		c.emitOpWithConstantIndex(chunk.OP_SET_GLOBAL, nameConst)
		c.emitByte(byte(chunk.OP_POP))

		return c.currentChunk, nil, nil

	case *ast.FunctionLiteral:
		c.setLine(n.Token.Line)

		fnName := n.Name
		if fnName == "" {
			fnName = "anonymous"
		}
		// Mesmo hook do FunctionStatement: a assinatura de um literal tambem pode
		// anotar instancias de struct generico.
		if err := c.resolveSignatureAnnotations(n.Parameters, &n.ReturnType, n.Token.Line); err != nil {
			return nil, nil, err
		}

		fnObj, fnCompiler, err := c.compileFunction(fnName, n.Parameters, n.Body, n.ReturnType) // Literal return type? n.ReturnType? FunctionLiteral needs return type field if typed. Assuming inferred/any if nil.
		if err != nil {
			return nil, nil, err
		}
		if err != nil {
			return nil, nil, err
		}

		funcIndex := c.makeConstant(fnObj)
		c.emitOpWithConstantIndex(chunk.OP_CLOSURE, funcIndex)
		c.emitClosureUpvalues(fnCompiler)

		return c.currentChunk, newFunctionType(n.Parameters, n.ReturnType), nil

	case *ast.BlockStatement:
		c.beginScope()
		for _, stmt := range n.Statements {
			_, _, err := c.Compile(stmt)
			if err != nil {
				return nil, nil, err
			}
		}
		c.endScope()
		return c.currentChunk, nil, nil

	case *ast.DeferStmt:
		c.setLine(n.Token.Line)
		return c.compileCallExpression(n.Call, emitDeferredCall(n.Token.Line))

	case *ast.CallExpression:
		return c.compileCallExpression(n, emitImmediateCall)

	case nil:
		// Skip
		return c.currentChunk, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported node type %T", n)
	}
}

func (c *Compiler) emitCall(argCount int, emission callEmission, static bool) {
	op := chunk.OP_CALL
	if static {
		op = chunk.OP_CALL_STATIC
	}
	line := c.currentLine
	if emission.deferred {
		op = chunk.OP_DEFER
		line = emission.registrationLine
	}
	c.currentChunk.Write(byte(op), line)
	c.currentChunk.Write(byte(argCount), line)
}

func (c *Compiler) compileCallExpression(call *ast.CallExpression, emission callEmission) (*chunk.Chunk, ast.NoxyType, error) {
	if handled, resultType, err := c.compileBuiltinCall(call, emission); handled {
		return c.currentChunk, resultType, err
	}

	// Check for special functions: chan_send, chan_recv.
	if ident, ok := call.Function.(*ast.Identifier); ok {
		if ident.Value == "chan_send" {
			if len(call.Arguments) != 2 {
				return nil, nil, fmt.Errorf("[line %d] chan_send expects 2 arguments", c.currentLine)
			}
			_, _, err := c.Compile(call.Function)
			if err != nil {
				return nil, nil, err
			}

			// Compile Arg 0 (Channel).
			_, chType, err := c.Compile(call.Arguments[0])
			if err != nil {
				return nil, nil, err
			}

			// Verify it is a channel OR any.
			var isAnyChannel bool
			chanType, ok := chType.(*ast.ChanType)
			if !ok {
				if chType != nil && chType.String() == "any" {
					isAnyChannel = true
				} else {
					typeStr := "unknown/nil"
					if chType != nil {
						typeStr = chType.String()
					}
					return nil, nil, fmt.Errorf("[line %d] first argument to chan_send must be a channel, got %s", c.currentLine, typeStr)
				}
			}

			// Compile Arg 1 (Value).
			_, valType, err := c.Compile(call.Arguments[1])
			if err != nil {
				return nil, nil, err
			}

			// Verify Type Match (only if not any).
			if !isAnyChannel {
				if !c.areTypesCompatible(chanType.ElementType, valType) {
					return nil, nil, fmt.Errorf("[line %d] cannot send %s to %s", c.currentLine, valType.String(), chType.String())
				}
			}

			c.emitCall(2, emission, false)
			return c.currentChunk, valType, nil // send returns value sent
		} else if ident.Value == "chan_recv" {
			if len(call.Arguments) != 1 {
				return nil, nil, fmt.Errorf("[line %d] chan_recv expects 1 argument", c.currentLine)
			}
			_, _, err := c.Compile(call.Function)
			if err != nil {
				return nil, nil, err
			}

			_, chType, err := c.Compile(call.Arguments[0])
			if err != nil {
				return nil, nil, err
			}

			var retType ast.NoxyType

			chanType, ok := chType.(*ast.ChanType)
			if !ok {
				if chType.String() == "any" {
					retType = &ast.PrimitiveType{Name: "any"}
				} else {
					return nil, nil, fmt.Errorf("[line %d] argument to chan_recv must be a channel, got %s", c.currentLine, chType.String())
				}
			} else {
				retType = chanType.ElementType
			}

			c.emitCall(1, emission, false)
			return c.currentChunk, retType, nil
		} else if ident.Value == "addr" {
			if emission.deferred {
				return nil, nil, fmt.Errorf("[line %d] cannot defer addr: addr does not produce a callable", c.currentLine)
			}
			// addr(ref x) debug function.
			if len(call.Arguments) != 1 {
				return nil, nil, fmt.Errorf("[line %d] addr expects 1 argument", c.currentLine)
			}
			// We expect a Reference on the stack (ObjRef), without auto-dereference.
			_, argType, err := c.Compile(call.Arguments[0])
			if err != nil {
				return nil, nil, err
			}

			if _, isRef := argType.(*ast.RefType); !isRef {
				return nil, nil, fmt.Errorf("[line %d] addr() requires a reference. Try 'addr(ref %s)'", c.currentLine, call.Arguments[0].String())
			}

			c.emitByte(byte(chunk.OP_ADDR))
			return c.currentChunk, &ast.PrimitiveType{Name: "string"}, nil
		}
	}

	// §8/§9: `use m` (namespace, sem select) seguido de `m.processa(...)`.
	// O template nunca existe como valor em runtime (declaracao generica nao
	// emite bytecode) e o member access nao carrega tipo estatico nenhum —
	// sem esta interceptacao o caminho normal so falharia mais tarde, em
	// runtime, com "undefined property" generico. Checa ANTES da
	// interceptacao de call site generico abaixo porque o callee aqui e um
	// *ast.MemberAccessExpression, nunca um *ast.Identifier — os dois hooks
	// nao competem pelo mesmo formato de callee.
	if memberCall, isMemberCall := call.Function.(*ast.MemberAccessExpression); isMemberCall {
		if leftIdent, isIdent := memberCall.Left.(*ast.Identifier); isIdent {
			if slot, _ := c.resolveLocal(leftIdent.Value); slot == -1 {
				if upvalue, _ := c.resolveUpvalue(leftIdent.Value); upvalue == -1 {
					if module, isNamespace := c.namespaceImports[leftIdent.Value]; isNamespace {
						if c.moduleExportsGenericTemplateName(module, memberCall.Member) {
							return nil, nil, fmt.Errorf(
								"[line %d] template genérico '%s' não é acessível via namespace — use select",
								c.currentLine, memberCall.Member,
							)
						}
					}
				}
			}
		}
	}

	// Interceptacao de call site generico (§4): tem de vir ANTES do caminho
	// normal, que compilaria o callee e leria o tipo CRU do template (com
	// TypeParamType) do registro de globals. Depois da interceptacao o
	// identificador ja aponta para a instancia monomorfizada e o caminho
	// normal resolve um global comum.
	//
	// No pass 2 nada dispara: os nomes reescritos nao sao chaves do registro.
	// Um local/parametro/UPVALUE que sombreia o nome do template tambem nao
	// dispara — quem vence e o binding mais interno, como no caminho normal.
	// O guard e isShadowedByLocal (locais E upvalues), o mesmo que as outras
	// familias de hook usam (generics_target.go): checar so resolveLocal
	// deixava passar o nome CAPTURADO por uma closure (`let id = dobro`
	// seguido de `func() ... id(v) ... end`), e a chamada era interceptada
	// como generica em vez de chamar o valor capturado — codigo errado em
	// silencio, sem erro nenhum.
	if callee, ok := call.Function.(*ast.Identifier); ok {
		registry := c.registryOrInit()
		if template, isTemplate := registry.Funcs[callee.Value]; isTemplate {
			if !c.isShadowedByLocal(callee.Value) {
				if err := c.compileGenericCallSite(call, callee, template); err != nil {
					return nil, nil, err
				}
			}
		} else if structTemplate, isStructTemplate := registry.Structs[callee.Value]; isStructTemplate {
			// Construtor de struct generico: `Caixa(41)`. Mesmo hook, mesma regra
			// de sombreamento — um local ou upvalue com o nome do template vence.
			if !c.isShadowedByLocal(callee.Value) {
				if err := c.compileGenericConstructorSite(call, callee, structTemplate); err != nil {
					return nil, nil, err
				}
			}
		}
	}

	// Normal call.
	_, fnType, err := c.Compile(call.Function)
	if err != nil {
		return nil, nil, err
	}

	funcType, isExact := fnType.(*ast.FunctionType)
	if isExact && len(call.Arguments) != len(funcType.Params) {
		return nil, nil, fmt.Errorf(
			"[line %d] function '%s' expects %d arguments, got %d",
			c.currentLine, callableName(call.Function), len(funcType.Params), len(call.Arguments),
		)
	}

	// modesProven começa igual a isExact e é derrubado se algum argumento
	// deixa um modo (ref vs. valor) sem prova em tempo de compilação — ex.:
	// `ref a` passado para um parâmetro `any` (areStrictTypesCompatible
	// aceita `any` incondicionalmente, então isExact continua true, mas
	// ninguém verificou que o valor não é uma ref). Só modesProven decide
	// OP_CALL_STATIC; nesse caso caímos para OP_CALL, que ainda roda
	// validateParameterModes em tempo de execução.
	modesProven := isExact
	for i, arg := range call.Arguments {
		if isExact {
			if expectedRef, ok := funcType.Params[i].(*ast.RefType); ok {
				actualElement, err := c.compileReferenceArgument(arg)
				if err != nil {
					return nil, nil, err
				}
				if _, isNull := arg.(*ast.NullLiteral); isNull {
					continue
				}
				if !c.areStrictTypesCompatible(expectedRef.ElementType, actualElement) {
					actual := &ast.RefType{ElementType: actualElement}
					return nil, nil, fmt.Errorf(
						"[line %d] argument %d to '%s': expected %s, got %s",
						c.currentLine, i+1, callableName(call.Function), expectedRef.String(), actual.String(),
					)
				}
				if err := c.emitRuntimeValueType(funcType.Params[i]); err != nil {
					return nil, nil, err
				}
				continue
			}
		}

		// §3 target-typing, posicao 5 (parte nao-generica: callee comum
		// recebendo um template de funcao nu, `func aplicaSimples(fn:
		// func(int)->int)` chamado com `aplicaSimples(identity)`): quando o
		// tipo do parametro e conhecido e concreto, e o alvo; sem alvo
		// conhecido (isExact falso), rewriteIfGenericValue erra pedindo
		// anotacao — exatamente a regra do §9 para identificador de template
		// em posicao de valor sem tipo concreto. Callees GENERICOS (`aplica`)
		// ja tiveram seus argumentos-template resolvidos e reescritos em
		// compileGenericCallSite antes de chegar aqui; o registro nao tem mais
		// a chave crua, entao este hook e um no-op para eles.
		var argTarget ast.NoxyType
		if isExact {
			argTarget = funcType.Params[i]
		}
		if err := c.rewriteIfGenericValue(arg, argTarget); err != nil {
			return nil, nil, err
		}

		_, argType, err := c.Compile(arg)
		if err != nil {
			return nil, nil, err
		}
		explicitReference := false
		if prefix, ok := arg.(*ast.PrefixExpression); ok {
			explicitReference = prefix.Operator == "ref"
		}
		if ref, ok := argType.(*ast.RefType); ok && !explicitReference {
			c.emitByte(byte(chunk.OP_DEREF))
			argType = ref.ElementType
		}
		if isExact {
			if _, stillRef := argType.(*ast.RefType); stillRef {
				if _, expectedIsRef := funcType.Params[i].(*ast.RefType); !expectedIsRef {
					modesProven = false
				}
			}
		}
		if isExact && !c.areStrictTypesCompatible(funcType.Params[i], argType) {
			return nil, nil, fmt.Errorf(
				"[line %d] argument %d to '%s': expected %s, got %s",
				c.currentLine, i+1, callableName(call.Function),
				funcType.Params[i].String(), noxyTypeName(argType),
			)
		}
		if isExact {
			if err := c.emitRuntimeValueType(funcType.Params[i]); err != nil {
				return nil, nil, err
			}
		}
	}

	c.emitCall(len(call.Arguments), emission, modesProven)
	if isExact {
		return c.currentChunk, funcType.Return, nil
	}
	if fnType == nil {
		return c.currentChunk, nil, nil
	}
	return c.currentChunk, &ast.PrimitiveType{Name: "any"}, nil
}

func (c *Compiler) memberType(owner ast.NoxyType, member string) ast.NoxyType {
	owner = unwrapRefType(owner)
	primitive, ok := owner.(*ast.PrimitiveType)
	if !ok {
		return nil
	}
	definition, ok := c.structs[primitive.Name]
	if !ok {
		return nil
	}
	for _, field := range definition.FieldsList {
		if field.Name == member {
			return field.Type
		}
	}
	return nil
}

func (c *Compiler) compileReferenceArgument(expression ast.Expression) (ast.NoxyType, error) {
	targetType, err := c.compileReferenceArgumentValue(expression)
	if err != nil || targetType == nil {
		return targetType, err
	}
	if runtimeType := c.runtimeTypeInfo(targetType); runtimeType != nil {
		typeConstant := c.makeConstant(value.NewRuntimeTypeInfo(runtimeType))
		if typeConstant > 65535 {
			return nil, fmt.Errorf("[line %d] too many constants for reference target metadata", c.currentLine)
		}
		c.emitByte(byte(chunk.OP_MARK_REF_TARGET_TYPE))
		c.emitByte(byte((typeConstant >> 8) & 0xff))
		c.emitByte(byte(typeConstant & 0xff))
	}
	return targetType, nil
}

func (c *Compiler) compileReferenceArgumentValue(expression ast.Expression) (ast.NoxyType, error) {
	if prefix, ok := expression.(*ast.PrefixExpression); ok && prefix.Operator == "ref" {
		expression = prefix.Right
	}

	switch target := expression.(type) {
	case *ast.Identifier:
		if slot, declared := c.resolveLocal(target.Value); slot != -1 {
			if ref, ok := declared.(*ast.RefType); ok {
				c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(slot))
				return ref.ElementType, nil
			}
			// RC: a caixa aberta sobre o slot herda a condicao do slot. Slot
			// nao-possuidor (hoje, apenas os de tipo `ref`) produz caixa
			// EMPRESTADA, para que a escrita via esse ref nao solte o que o
			// slot nunca reteve (dec a menos).
			if c.localOwns(slot) {
				c.emitBytes(byte(chunk.OP_REF_LOCAL), byte(slot))
			} else {
				c.emitBytes(byte(chunk.OP_REF_LOCAL_BORROW), byte(slot))
			}
			c.locals[slot].IsCaptured = true
			return declared, nil
		}
		if upvalue, declared := c.resolveUpvalue(target.Value); upvalue != -1 {
			if ref, ok := declared.(*ast.RefType); ok {
				c.emitBytes(byte(chunk.OP_GET_UPVALUE), byte(upvalue))
				return ref.ElementType, nil
			}
			c.emitBytes(byte(chunk.OP_REF_UPVALUE), byte(upvalue))
			return declared, nil
		}
		name := c.makeConstant(value.NewString(target.Value))
		if declared, ok := c.resolveGlobalType(target.Value); ok {
			if ref, ok := declared.(*ast.RefType); ok {
				c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, name)
				return ref.ElementType, nil
			}
			c.emitOpWithConstantIndex(chunk.OP_REF_GLOBAL, name)
			return declared, nil
		}
		c.emitOpWithConstantIndex(chunk.OP_REF_GLOBAL, name)
		return nil, nil
	case *ast.MemberAccessExpression:
		// CoW: um ref para dentro de um contêiner fixa a identidade do
		// contêiner — a base precisa ser unicizada na criação do ref para a
		// escrita através dele não vazar em cópias pendentes.
		owner, _, err := c.compileLValueBase(target.Left)
		if err != nil {
			return nil, err
		}
		element := c.memberType(owner, target.Member)
		name := c.makeConstant(value.NewString(target.Member))
		if ref, ok := element.(*ast.RefType); ok {
			c.emitOpWithConstantIndex(chunk.OP_CONTEXT_REF_PROPERTY, name)
			return ref.ElementType, nil
		}
		c.emitOpWithConstantIndex(chunk.OP_REF_PROPERTY, name)
		return element, nil
	case *ast.IndexExpression:
		// CoW: base do ref unicizada na criação (ver caso MemberAccess acima)
		container, _, err := c.compileLValueBase(target.Left)
		if err != nil {
			return nil, err
		}
		element := indexElementType(container)
		_, indexType, err := c.Compile(target.Index)
		if err != nil {
			return nil, err
		}
		if _, ok := indexType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
		}
		indexType = unwrapRefType(indexType)
		switch collection := unwrapRefType(container).(type) {
		case *ast.ArrayType:
			expected := &ast.PrimitiveType{Name: "int"}
			if !c.areStrictTypesCompatible(expected, indexType) {
				return nil, fmt.Errorf(
					"[line %d] array reference index must be int, got %s",
					c.currentLine, noxyTypeName(indexType),
				)
			}
		case *ast.MapType:
			if !c.areStrictTypesCompatible(collection.KeyType, indexType) {
				return nil, fmt.Errorf(
					"[line %d] map reference key must be %s, got %s",
					c.currentLine, noxyTypeName(collection.KeyType), noxyTypeName(indexType),
				)
			}
		}
		if ref, ok := element.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_CONTEXT_REF_INDEX))
			return ref.ElementType, nil
		}
		c.emitByte(byte(chunk.OP_REF_INDEX))
		return element, nil
	case *ast.NullLiteral:
		c.emitByte(byte(chunk.OP_NULL))
		return nil, nil
	case *ast.CallExpression:
		_, result, err := c.Compile(target)
		if err != nil {
			return nil, err
		}
		if ref, ok := result.(*ast.RefType); ok {
			return ref.ElementType, nil
		}
		return nil, fmt.Errorf(
			"[line %d] reference argument '%s' is not addressable\n  hint: use a variable, property, index, or null",
			c.currentLine, expression.String(),
		)
	default:
		return nil, fmt.Errorf(
			"[line %d] reference argument '%s' is not addressable\n  hint: use a variable, property, index, or null",
			c.currentLine, expression.String(),
		)
	}
}

func (c *Compiler) setLine(line int) {
	if line > 0 {
		c.currentLine = line
	}
}

func (c *Compiler) emitByte(b byte) {
	c.currentChunk.Write(b, c.currentLine)
}

func (c *Compiler) emitBytes(b1, b2 byte) {
	c.emitByte(b1)
	c.emitByte(b2)
}

// emitOpWithConstantIndex emits an opcode whose operand is an index into the
// constant pool, always as a 16-bit big-endian value. OP_GET_GLOBAL,
// OP_SET_GLOBAL, OP_GET_PROPERTY, OP_SET_PROPERTY, OP_IMPORT, OP_CLOSURE,
// OP_REF_GLOBAL, OP_REF_PROPERTY, and OP_CONTEXT_REF_PROPERTY all read a name
// or function constant this way. A single-byte operand silently truncates
// past 255 constants: a chunk with, say, 256 distinct global names truncates
// index 256 to 0, so OP_GET_GLOBAL reads whatever constant happens to sit at
// index 0 instead of the one the compiler meant, corrupting variable
// resolution with no compile-time signal. 255 constants is not a large
// program — every distinct string literal, every global name reference (not
// just every declaration), and every imported struct's runtime-type metadata
// each claims one slot, and AddConstant never deduplicates. This is the
// pattern OP_CONSTANT_LONG and OP_JUMP already use for the same reason;
// unlike OP_CONSTANT, there is no short form to keep here; that a matching
// module-scoped struct-field fix pushed one ordinary stdlib test over 255
// constants and none of its constant-index reads at the low end of the pool
// happened to be spot-checked is what surfaced this.
func (c *Compiler) emitOpWithConstantIndex(op chunk.OpCode, index int) {
	if index < 0 || index > 65535 {
		panic("constant index out of range for a 16-bit operand")
	}
	c.emitByte(byte(op))
	c.emitByte(byte((index >> 8) & 0xff))
	c.emitByte(byte(index & 0xff))
}

// fusedIntCompareJump mapeia o operador da CONDICAO para o opcode que salta
// quando a condicao FALHA (e o jump-if-false fundido): `i < n` continua no
// corpo quando vale e salta para fora com GE.
func fusedIntCompareJump(operator string) (chunk.OpCode, bool) {
	switch operator {
	case "<":
		return chunk.OP_JUMP_IF_GE_INT, true
	case "<=":
		return chunk.OP_JUMP_IF_GT_INT, true
	case ">":
		return chunk.OP_JUMP_IF_LE_INT, true
	case ">=":
		return chunk.OP_JUMP_IF_LT_INT, true
	case "==":
		return chunk.OP_JUMP_IF_NE_INT, true
	case "!=":
		return chunk.OP_JUMP_IF_EQ_INT, true
	}
	return 0, false
}

// tryCompileFusedCondition tenta compilar cond como (left, right) ints puros e
// devolve o opcode de salto fundido a emitir com emitJump. Emissao
// especulativa: se um dos lados nao for estaticamente int, o bytecode dos
// operandos e DESFEITO (TruncateTo) e o chamador segue o caminho generico.
// Constantes adicionadas na especulacao ficam orfas no pool — inofensivo, mas
// note que um operando que e um literal de funcao orfa a FUNCAO INTEIRA
// compilada (um ObjFunction com seu proprio chunk, retido pelo modulo pelo
// resto da vida do programa) e conta no tamanho de GlobalCache() (dimensionado
// por len(Constants)) — ainda inofensivo (o cache so cresce, nunca e indexado
// por essas entradas mortas), so mais bytes do que uma constante escalar orfa.
func (c *Compiler) tryCompileFusedCondition(cond ast.Expression) (chunk.OpCode, bool, error) {
	infix, ok := cond.(*ast.InfixExpression)
	if !ok {
		return 0, false, nil
	}
	jumpOp, ok := fusedIntCompareJump(infix.Operator)
	if !ok {
		return 0, false, nil
	}
	checkpoint := len(c.currentChunk.Code)
	_, leftType, err := c.Compile(infix.Left)
	if err != nil {
		// Sem TruncateTo aqui: um erro aborta a compilacao inteira (o
		// chamador propaga err e nenhum bytecode deste Chunk chega a ser
		// executado), entao nao ha invariante de "pilha do bytecode emitido"
		// a preservar — diferente dos dois `if ... TruncateTo` abaixo, que
		// tratam um retorno SEM erro (tipo nao-int) onde a compilacao
		// continua e o caminho generico precisa reemitir do zero.
		return 0, false, err
	}
	if leftType == nil || leftType.String() != "int" {
		c.currentChunk.TruncateTo(checkpoint)
		return 0, false, nil
	}
	_, rightType, err := c.Compile(infix.Right)
	if err != nil {
		// Mesmo raciocinio: erro aborta a compilacao inteira.
		return 0, false, err
	}
	if rightType == nil || rightType.String() != "int" {
		c.currentChunk.TruncateTo(checkpoint)
		return 0, false, nil
	}
	return jumpOp, true, nil
}

func (c *Compiler) emitJump(op chunk.OpCode) int {
	c.emitByte(byte(op))
	c.emitByte(0xff)
	c.emitByte(0xff)
	return len(c.currentChunk.Code) - 2
}

func (c *Compiler) patchJump(offset int) {
	jump := len(c.currentChunk.Code) - offset - 2
	if jump > 65535 {
		panic("Jump too large")
	}
	c.currentChunk.Code[offset] = byte((jump >> 8) & 0xff)
	c.currentChunk.Code[offset+1] = byte(jump & 0xff)
}

func (c *Compiler) emitLoop(loopStart int) {
	c.emitByte(byte(chunk.OP_LOOP))
	offset := len(c.currentChunk.Code) - loopStart + 2
	if offset > 65535 {
		panic("Loop too large")
	}
	c.emitByte(byte((offset >> 8) & 0xff))
	c.emitByte(byte(offset & 0xff))
}

func (c *Compiler) makeConstant(v value.Value) int {
	i := c.currentChunk.AddConstant(v)
	return i
}

func (c *Compiler) emitConstant(v value.Value) {
	index := c.makeConstant(v)
	if index <= 255 {
		c.emitBytes(byte(chunk.OP_CONSTANT), byte(index))
	} else if index <= 65535 {
		c.emitByte(byte(chunk.OP_CONSTANT_LONG))
		c.emitByte(byte((index >> 8) & 0xff))
		c.emitByte(byte(index & 0xff))
	} else {
		panic("Too many constants in one chunk > 65535")
	}
}

func (c *Compiler) beginScope() {
	c.scopeDepth++
}

// emitLocalsExit emite o descarte dos locais a partir do indice keep SEM
// remove-los da tabela do compilador — para break/continue, que saem do
// escopo em runtime mas nao em compilacao. Mesma regra do endScope: local
// capturado fecha a caixa (OP_CLOSE_UPVALUE), os demais OP_POP. Com OP_POP
// cru o upvalue de um `let` do corpo ficava aberto sobre um slot que a
// proxima iteracao reusa (a closure passava a ler o valor dela).
func (c *Compiler) emitLocalsExit(keep int) {
	for i := len(c.locals) - 1; i >= keep; i-- {
		if c.locals[i].IsCaptured {
			c.emitByte(byte(chunk.OP_CLOSE_UPVALUE))
		} else {
			c.emitByte(byte(chunk.OP_POP))
		}
	}
}

func (c *Compiler) endScope() {
	exitingDepth := c.scopeDepth
	for len(c.scopedStructs) > 0 && c.scopedStructs[len(c.scopedStructs)-1].Depth == exitingDepth {
		binding := c.scopedStructs[len(c.scopedStructs)-1]
		c.scopedStructs = c.scopedStructs[:len(c.scopedStructs)-1]
		if binding.Existed {
			c.structs[binding.Name] = binding.Prior
		} else {
			delete(c.structs, binding.Name)
		}
	}
	c.scopeDepth--
	// Pop locals from stack
	for len(c.locals) > 0 && c.locals[len(c.locals)-1].Depth > c.scopeDepth {
		if c.locals[len(c.locals)-1].IsCaptured {
			c.emitByte(byte(chunk.OP_CLOSE_UPVALUE))
		} else {
			c.emitByte(byte(chunk.OP_POP))
		}
		c.locals = c.locals[:len(c.locals)-1]
	}
}

// addLocal declara um vinculo local que NAO retem o que guarda (Owns=false — o
// default seguro). Vinculos que retem devem usar addOwnedLocal.
func (c *Compiler) addLocal(name string, t ast.NoxyType) {
	c.locals = append(c.locals, Local{Name: name, Depth: c.scopeDepth, Line: c.currentLine, Type: t})
}

// addOwnedLocal declara um vinculo local cujo slot RETEM o composto que guarda
// — usar somente onde o inc correspondente e de fato emitido (OP_OWN_LOCAL) ou
// feito pelo runtime (retain de parametro sem `ref`).
func (c *Compiler) addOwnedLocal(name string, t ast.NoxyType) {
	c.locals = append(c.locals, Local{Name: name, Depth: c.scopeDepth, Line: c.currentLine, Type: t, Owns: true})
}

// localOwns responde a pergunta que os funis de escrita fazem: este slot retem
// o que guarda? Indice fora de faixa responde false (direcao segura).
func (c *Compiler) localOwns(index int) bool {
	if index < 0 || index >= len(c.locals) {
		return false
	}
	return c.locals[index].Owns
}

// tryFuseLocalIntIncrement funde `i = i + K` / `i = i - K` (i local int
// POSSUIDOR — slot ref nunca funde; K literal int em [-128,127]) em
// OP_INC_LOCAL_INT. Retorna true se emitiu (nada mais a fazer no site).
// Sem emissao especulativa: todas as checagens sao sintaticas/de simbolo.
func (c *Compiler) tryFuseLocalIntIncrement(ident *ast.Identifier, valueExpr ast.Expression) bool {
	arg, localType := c.resolveLocal(ident.Value)
	if arg == -1 || arg > 255 || !c.localOwns(arg) {
		return false
	}
	prim, ok := localType.(*ast.PrimitiveType)
	if !ok || prim.Name != "int" {
		return false
	}
	infix, ok := valueExpr.(*ast.InfixExpression)
	if !ok || (infix.Operator != "+" && infix.Operator != "-") {
		return false
	}
	left, ok := infix.Left.(*ast.Identifier)
	if !ok || left.Value != ident.Value {
		return false
	}
	lit, ok := infix.Right.(*ast.IntegerLiteral)
	if !ok {
		return false
	}
	delta := lit.Value
	if infix.Operator == "-" {
		delta = -delta
	}
	if delta < -128 || delta > 127 {
		return false
	}
	c.emitBytes(byte(chunk.OP_INC_LOCAL_INT), byte(arg))
	c.emitByte(byte(int8(delta)))
	return true
}

func (c *Compiler) emitDefaultInit(t ast.NoxyType) error {
	switch typ := t.(type) {
	case *ast.PrimitiveType:
		switch typ.Name {
		case "int":
			c.emitConstant(value.NewInt(0))
		case "float":
			c.emitConstant(value.NewFloat(0.0))
		case "bool":
			c.emitByte(byte(chunk.OP_FALSE))
		case "string":
			c.emitConstant(value.NewString(""))
		case "bytes":
			c.emitConstant(value.NewBytes(""))
		default:
			c.emitByte(byte(chunk.OP_NULL))
		}
	case *ast.ArrayType:
		if typ.Size > 0 {
			// Um default + N -> OP_ARRAY_FILL: nao empilha N elementos (antes
			// estourava a pilha de operandos em N > ~2047 e truncava o operando
			// de 16 bits de OP_ARRAY em N > 65535).
			if err := c.emitDefaultInit(typ.ElementType); err != nil {
				return err
			}
			c.emitConstant(value.NewInt(int64(typ.Size)))
			c.emitByte(byte(chunk.OP_ARRAY_FILL))
		} else {
			// Empty array (dynamic)
			c.emitByte(byte(chunk.OP_ARRAY))
			c.emitByte(0)
			c.emitByte(0)
		}
	case *ast.MapType:
		c.emitByte(byte(chunk.OP_MAP))
		c.emitByte(0)
		c.emitByte(0)
	default:
		c.emitByte(byte(chunk.OP_NULL))
	}
	return nil
}

func (c *Compiler) resolveLocal(name string) (int, ast.NoxyType) {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].Name == name {
			return i, c.locals[i].Type
		}
	}
	return -1, nil
}

func (c *Compiler) resolveGlobalType(name string) (ast.NoxyType, bool) {
	t, ok := c.globals[name]
	return t, ok
}

func referenceAssignmentTypeError(line int, name string, expected, actual ast.NoxyType) error {
	return fmt.Errorf(
		"[line %d] cannot assign %s to %s\n  hint: use '*%s = ...' to update the referenced value",
		line, noxyTypeName(actual), noxyTypeName(expected), name,
	)
}

// referenceSlotAssignmentTypeError e a variante para alvos que sao SLOTS
// (campo de struct = "field", elemento de array = "element", valor de map =
// "entry"). O hint cobre os dois caminhos legitimos da tabela de §2.3: apontar
// o slot para um valor novo ('x = ref novo' — uma variavel, porque `ref`
// exige L-value e o compilador a promove para a heap) e sobrescrever o
// referente ('*x = ...'). Variavel `ref T` segue com
// referenceAssignmentTypeError (spec §2.3 documenta aquele hint).
func referenceSlotAssignmentTypeError(line int, name, slotKind string, expected, actual ast.NoxyType) error {
	return fmt.Errorf(
		"[line %d] cannot assign %s to %s\n  hint: to point the %s at a new value, bind it to a variable first and use '%s = ref novo'; to overwrite the referenced value use '*%s = ...'",
		line, noxyTypeName(actual), noxyTypeName(expected), slotKind, name, name,
	)
}

// derefReadHint e o espelho de referenceAssignmentTypeError para a direcao
// inversa: RHS `ref T` num alvo que espera `T`. Atribuicao nao faz
// auto-deref (spec §2.3, Type-Based Assignment) — a leitura pede '*'
// explicito. Devolve "" quando o deref nao consertaria o programa, para o
// mismatch generico nao sugerir orientacao errada.
func (c *Compiler) derefReadHint(expected, actual ast.NoxyType, rhs ast.Expression) string {
	refVal, isRef := actual.(*ast.RefType)
	if !isRef || expected == nil {
		return ""
	}
	if _, expectedIsRef := expected.(*ast.RefType); expectedIsRef {
		return ""
	}
	if !c.areTypesCompatible(expected, refVal.ElementType) {
		return ""
	}
	if ident, ok := rhs.(*ast.Identifier); ok {
		return fmt.Sprintf("\n  hint: use '*%s' to read the referenced value", ident.Value)
	}
	return "\n  hint: use '*' to read the referenced value"
}

// rejectMixedRefComparison aplica a regra "em `==`/`!=` um ref nunca e
// dereferenciado implicitamente" ao caso misto estatico: exatamente um dos
// lados e `ref T` e o outro e um valor de tipo conhecido. Ref vs null
// pergunta sobre o proprio ref, e `any`/tipo desconhecido e fronteira
// dinamica (pode carregar um ref em runtime, comparacao de identidade
// legitima) — ambos passam. O erro aponta o deref explicito.
func (c *Compiler) rejectMixedRefComparison(n *ast.InfixExpression, leftType, rightType ast.NoxyType) error {
	leftRef := isReferenceType(leftType)
	rightRef := isReferenceType(rightType)
	if leftRef == rightRef {
		return nil
	}
	refType, other, refExpr := leftType, rightType, n.Left
	if rightRef {
		refType, other, refExpr = rightType, leftType, n.Right
	}
	if other == nil || isNullType(other) || other.String() == "any" {
		return nil
	}
	hint := "\n  hint: use '*' to compare the referenced value"
	if ident, ok := refExpr.(*ast.Identifier); ok {
		hint = fmt.Sprintf("\n  hint: use '*%s' to compare the referenced value", ident.Value)
	}
	return fmt.Errorf(
		"[line %d] cannot compare %s with %s: a ref is never implicitly dereferenced in '%s'%s",
		c.currentLine, noxyTypeName(refType), noxyTypeName(other), n.Operator, hint,
	)
}

func isReferenceType(t ast.NoxyType) bool {
	_, ok := t.(*ast.RefType)
	return ok
}

func assignmentTargetName(expression ast.Expression) string {
	switch target := expression.(type) {
	case *ast.Identifier:
		return target.Value
	case *ast.StringLiteral:
		return fmt.Sprintf("%q", target.Value)
	case *ast.MemberAccessExpression:
		return assignmentTargetName(target.Left) + "." + target.Member
	case *ast.IndexExpression:
		return assignmentTargetName(target.Left) + "[" + assignmentTargetName(target.Index) + "]"
	default:
		return expression.String()
	}
}

func (c *Compiler) areTypesCompatible(expected, actual ast.NoxyType) bool {
	if expected == nil {
		return true
	}
	if actual == nil {
		return !c.containsCallableType(expected, nil)
	}
	if isNullType(actual) {
		return c.acceptsNull(expected)
	}
	if isBareFunctionType(expected) {
		return isCallableType(actual)
	}
	if isBareFunctionType(actual) {
		return isBareFunctionType(expected) || isAny(expected)
	}
	if _, ok := expected.(*ast.FunctionType); ok {
		return c.areStrictTypesCompatible(expected, actual)
	}
	if expected.String() == actual.String() {
		return true
	}
	if isAny(expected) || isAny(actual) {
		return true
	}
	if expectedMap, ok := expected.(*ast.MapType); ok {
		actualMap, ok := actual.(*ast.MapType)
		return ok && c.areTypesCompatible(expectedMap.KeyType, actualMap.KeyType) &&
			c.areTypesCompatible(expectedMap.ValueType, actualMap.ValueType)
	}
	if expectedArray, ok := expected.(*ast.ArrayType); ok {
		actualArray, ok := actual.(*ast.ArrayType)
		return ok && (expectedArray.Size == 0 || expectedArray.Size == actualArray.Size) &&
			c.areTypesCompatible(expectedArray.ElementType, actualArray.ElementType)
	}
	return false
}

func isAny(t ast.NoxyType) bool {
	primitive, ok := t.(*ast.PrimitiveType)
	return ok && primitive.Name == "any"
}

func (c *Compiler) resolveUpvalue(name string) (int, ast.NoxyType) {
	if c.enclosing == nil {
		return -1, nil
	}

	// 1. Check immediate parent's locals
	local, localType := c.enclosing.resolveLocal(name)
	if local != -1 {
		c.enclosing.locals[local].IsCaptured = true // Mark as captured!
		return c.addUpvalue(uint8(local), true, localType), localType
	}

	// 2. Check immediate parent's upvalues
	upvalue, upvalueType := c.enclosing.resolveUpvalue(name)
	if upvalue != -1 {
		return c.addUpvalue(uint8(upvalue), false, upvalueType), upvalueType
	}

	return -1, nil
}

func (c *Compiler) addUpvalue(index uint8, isLocal bool, upvalueType ast.NoxyType) int {
	// Check for existing upvalue
	for i, u := range c.upvalues {
		if u.Index == index && u.IsLocal == isLocal {
			return i
		}
	}

	if len(c.upvalues) >= 255 {
		// Error: too many upvalues
	}

	c.upvalues = append(c.upvalues, Upvalue{Index: index, IsLocal: isLocal, Type: upvalueType})
	return len(c.upvalues) - 1
}

// emitClosureUpvalues emite a tabela de descritores de upvalue do OP_CLOSURE
// (pares [is_local, index], encoding intocado) e, em seguida, um
// OP_MARK_UPVALUE_BORROW para cada upvalue cujo TIPO DECLARADO e `ref T`.
//
// RC: a caixa de um upvalue aberto sobre um slot `ref` empresta — nao possui —
// o que guarda, e essa condicao tem de vir do compilador (estatica), nunca de
// uma inspecao da lista de slots possuidos do frame em runtime: um slot
// possuido cujo ocupante era null/escalar na hora da captura nao esta na lista
// (Retain falha em nao-composto) e seria marcado emprestado por engano (retain
// devido pulado = under-count), e indices de slot sao reusados entre blocos
// irmaos sem poda da lista, entao a entrada morta de um irmao faria um slot
// realmente emprestado parecer possuido (release indevido = dec a menos).
// As marcas saem depois dos descritores e antes de qualquer uso do valor: o
// corpo da closure so roda quando ela e chamada, e o fechamento da caixa
// acontece no fim do escopo/frame — os dois depois daqui.
func (c *Compiler) emitClosureUpvalues(fnCompiler *Compiler) {
	for _, up := range fnCompiler.upvalues {
		isLocal := byte(0)
		if up.IsLocal {
			isLocal = 1
		}
		c.emitByte(isLocal)
		c.emitByte(up.Index)
	}
	for i, up := range fnCompiler.upvalues {
		if _, isRefBinding := up.Type.(*ast.RefType); isRefBinding {
			c.emitBytes(byte(chunk.OP_MARK_UPVALUE_BORROW), byte(i))
		}
	}
}

func (c *Compiler) compileFunction(name string, params []*ast.Parameter, body *ast.BlockStatement, returnType ast.NoxyType) (value.Value, *Compiler, error) {
	restoreBindings := c.applyProgramBindings()
	defer restoreBindings()

	fnCompiler := NewChild(c)
	fnCompiler.scopeDepth = 1    // Inside function body
	fnCompiler.addLocal("", nil) // Reserve slot 0 for function instance
	declaredReturn := normalizeReturnType(returnType)
	fnCompiler.funcReturnType = declaredReturn
	fnCompiler.currentFunctionName = name

	paramsInfo := []value.ParamInfo{}
	for _, param := range params {
		isRef := false
		if _, ok := param.Type.(*ast.RefType); ok {
			isRef = true
		}
		// RC: callPreparedClosure retem (e registra em frame.Owned) o slot de
		// cada parametro SEM `ref`; parametro `ref` e emprestimo e e pulado la.
		// O flag Owns aqui espelha exatamente essa decisao do runtime.
		if isRef {
			fnCompiler.addLocal(param.Name, param.Type)
		} else {
			fnCompiler.addOwnedLocal(param.Name, param.Type)
		}
		fnCompiler.locals[len(fnCompiler.locals)-1].IsParam = true // Mark as param
		paramsInfo = append(paramsInfo, value.ParamInfo{
			IsRef:    isRef,
			TypeName: param.Type.String(),
		})
	}
	if declaredReturn.String() != "void" && !blockGuaranteesReturn(body) {
		return value.Value{}, nil, fmt.Errorf(
			"[line %d] function '%s' may finish without returning %s",
			body.Token.Line, name, declaredReturn.String(),
		)
	}

	_, _, err := fnCompiler.Compile(body)
	if err != nil {
		return value.Value{}, nil, err
	}

	if declaredReturn.String() == "void" {
		fnCompiler.emitBytes(byte(chunk.OP_NULL), byte(chunk.OP_RETURN))
	}

	upvalueCount := len(fnCompiler.upvalues)
	fnObj := value.NewFunction(name, len(params), upvalueCount, paramsInfo, fnCompiler.currentChunk, nil)
	fnObj.Obj.(*value.ObjFunction).RuntimeType = c.runtimeTypeInfo(newFunctionType(params, declaredReturn))

	return fnObj, fnCompiler, nil
}

func (c *Compiler) applyProgramBindings() func() {
	if len(c.programBindings) == 0 {
		return func() {}
	}
	type priorBinding struct {
		typeInfo ast.NoxyType
		exists   bool
	}
	prior := make(map[string]priorBinding, len(c.programBindings))
	for name, bindingType := range c.programBindings {
		current, exists := c.globals[name]
		prior[name] = priorBinding{typeInfo: current, exists: exists}
		c.globals[name] = bindingType
	}
	return func() {
		for name, binding := range prior {
			if binding.exists {
				c.globals[name] = binding.typeInfo
			} else {
				delete(c.globals, name)
			}
		}
	}
}
