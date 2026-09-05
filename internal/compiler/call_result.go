package compiler

import (
	"fmt"
	"strings"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/value"
)

// call_result (issue #105 item 2): a fronteira sincrona de erro devolve
// errors::Result<R>, com R o retorno ESTATICO do callee — funcao, construtor
// ou valor de funcao com assinatura exata; nativo central com tipo em
// coreBuiltinReturnTypes; literal de funcao. void ou desconhecido dao
// Result<any>. O compilador instancia Result<R> (mesma fila dos genericos) e
// prefixa dois argumentos ocultos — o construtor da instancia e o de
// Failure — que a VM usa para montar instancias reais (call_result_envelope.go).
//
// Exige `use errors select *` (ou pelo menos Result e Failure) em escopo:
// sem o template nao ha tipo estatico nem envelope, e a chamada e erro.

const errorsModule = "errors"

func (c *Compiler) resultTemplate() (*StructTemplate, bool) {
	tpl, ok := c.registryOrInit().Structs["Result"]
	if !ok || tpl.Module != errorsModule {
		return nil, false
	}
	return tpl, true
}

func (c *Compiler) callResultScopeError() error {
	return fmt.Errorf("[line %d] call_result needs 'use errors select *' in scope: its result is errors.Result<T>", c.currentLine)
}

// resultTypeArgs devolve [R] quando t e a instancia errors::Result<R>. Le R
// do campo `value: T?` da declaracao da instancia (a fila de genericos so
// existe no pass 1; a declaracao vale nos dois passes).
func (c *Compiler) resultTypeArgs(t ast.NoxyType) ([]ast.NoxyType, bool) {
	primitive, ok := t.(*ast.PrimitiveType)
	if !ok || !strings.HasPrefix(primitive.Name, errorsModule+"::Result<") {
		return nil, false
	}
	decl := c.structDeclaration(primitive.Name)
	if decl == nil {
		return nil, false
	}
	for _, field := range decl.FieldsList {
		if field.Name == "value" {
			elem, _ := nonNull(field.Type)
			return []ast.NoxyType{elem}, true
		}
	}
	return nil, false
}

// resultInstance garante a instancia errors::Result<R> e devolve seu nome.
func (c *Compiler) resultInstance(arg ast.NoxyType) (string, error) {
	tpl, ok := c.resultTemplate()
	if !ok {
		return "", c.callResultScopeError()
	}
	if _, declared := c.globals["Failure"]; !declared {
		return "", c.callResultScopeError()
	}
	args := []ast.NoxyType{resultValueType(arg)}
	if c.pass1 {
		return c.ensureStructInstance(tpl, args, c.currentLine)
	}
	// Pass 2: a instancia ja foi enfileirada e prependada pelo pass 1; aqui
	// so se resolve o nome (mesma regra dos demais sites de genericos).
	name := instanceName(tpl.Module, tpl.Decl.Name, args)
	if c.structDeclaration(name) == nil {
		return "", fmt.Errorf("[line %d] instancia '%s' de call_result chegou ao pass 2 sem monomorfização — bug do compilador de genéricos", c.currentLine, name)
	}
	return name, nil
}

// resultValueType normaliza o R de Result<R>: void/desconhecido -> any.
func resultValueType(t ast.NoxyType) ast.NoxyType {
	if t == nil || isNullType(t) {
		return &ast.PrimitiveType{Name: "any"}
	}
	if primitive, ok := t.(*ast.PrimitiveType); ok && primitive.Name == "void" {
		return &ast.PrimitiveType{Name: "any"}
	}
	return t
}

// calleeReturnType e o retorno estatico do callee de call_result, ou nil.
func (c *Compiler) calleeReturnType(callee ast.Expression) ast.NoxyType {
	switch fn := callee.(type) {
	case *ast.Identifier:
		var t ast.NoxyType
		if slot, localType := c.resolveLocal(fn.Value); slot != -1 {
			t = localType
		} else if slot, upvalueType := c.resolveUpvalue(fn.Value); slot != -1 {
			t = upvalueType
		} else if globalType, ok := c.globals[fn.Value]; ok {
			t = globalType
		} else {
			return builtinReturnType(fn.Value, nil)
		}
		if signature, ok := t.(*ast.FunctionType); ok {
			return signature.Return
		}
	case *ast.FunctionLiteral:
		return fn.ReturnType
	}
	return nil
}

func (c *Compiler) compileCallResult(call *ast.CallExpression, emission callEmission) (ast.NoxyType, error) {
	if len(call.Arguments) == 0 {
		return nil, fmt.Errorf("[line %d] call_result expects a callable", c.currentLine)
	}
	instance, err := c.resultInstance(c.calleeReturnType(call.Arguments[0]))
	if err != nil {
		return nil, err
	}
	// call_result, depois os dois construtores ocultos, depois fn e args —
	// cada `ref x` compila como expressao (cria a referencia, R1).
	c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString("call_result")))
	c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString(instance)))
	c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString("Failure")))
	for _, arg := range call.Arguments {
		if _, _, err := c.Compile(arg); err != nil {
			return nil, err
		}
	}
	c.emitCall(len(call.Arguments)+2, emission, false)
	return &ast.PrimitiveType{Name: instance}, nil
}
