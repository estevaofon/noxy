package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// compileTry abaixa `try expr` (spec §7, issue #105 item 2). expr deve ter
// tipo errors::Result<U> e a funcao corrente deve devolver errors::Result<V>:
//
//	<expr>                       ; r
//	OP_DUP                       ; r r
//	OP_GET_PROPERTY ok           ; r ok
//	OP_JUMP_IF_FALSE Lfail       ; r ok
//	OP_POP                       ; r
//	OP_GET_PROPERTY value        ; u            (tipo estatico U)
//	OP_JUMP Lend
//	Lfail: OP_POP                ; r
//	OP_GET_PROPERTY failure      ; f
//	OP_GET_GLOBAL Result<V>      ; f ctor
//	OP_SWAP  OP_FALSE  OP_SWAP   ; ctor false f
//	OP_NULL  OP_SWAP             ; ctor false null f
//	OP_CALL 3                    ; Result<V>(false, null, f)
//	OP_RETURN                    ; os defer rodam como em qualquer return
//	Lend:
//
// Nenhuma mudanca na VM alem de OP_SWAP: OP_RETURN ja executa os defer do
// frame e descarta os temporarios abaixo do resultado.
func (c *Compiler) compileTry(n *ast.TryExpression) (*chunk.Chunk, ast.NoxyType, error) {
	c.setLine(n.Token.Line)
	if c.funcReturnType == nil {
		return nil, nil, fmt.Errorf("[line %d] 'try' outside a function", c.currentLine)
	}
	expected := c.funcReturnType
	if _, isResult := c.resultTypeArgs(expected); !isResult {
		return nil, nil, fmt.Errorf("[line %d] 'try' requires the enclosing function to return Result<T> (found %s)", c.currentLine, noxyTypeName(expected))
	}
	_, valueType, err := c.Compile(n.Value)
	if err != nil {
		return nil, nil, err
	}
	innerArgs, isResult := c.resultTypeArgs(valueType)
	if !isResult {
		return nil, nil, fmt.Errorf("[line %d] 'try' expects a Result<T>, got %s", c.currentLine, noxyTypeName(valueType))
	}
	ctorName := expected.(*ast.PrimitiveType).Name

	c.emitByte(byte(chunk.OP_DUP))
	c.emitOpWithConstantIndex(chunk.OP_GET_PROPERTY, c.makeConstant(value.NewString("ok")))
	jumpFail := c.emitJump(chunk.OP_JUMP_IF_FALSE)
	c.emitByte(byte(chunk.OP_POP))
	c.emitOpWithConstantIndex(chunk.OP_GET_PROPERTY, c.makeConstant(value.NewString("value")))
	jumpEnd := c.emitJump(chunk.OP_JUMP)

	c.patchJump(jumpFail)
	c.emitByte(byte(chunk.OP_POP))
	c.emitOpWithConstantIndex(chunk.OP_GET_PROPERTY, c.makeConstant(value.NewString("failure")))
	c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, c.makeConstant(value.NewString(ctorName)))
	c.emitByte(byte(chunk.OP_SWAP))
	c.emitByte(byte(chunk.OP_FALSE))
	c.emitByte(byte(chunk.OP_SWAP))
	c.emitByte(byte(chunk.OP_NULL))
	c.emitByte(byte(chunk.OP_SWAP))
	c.emitCall(3, emitImmediateCall, false)
	if err := c.emitRuntimeValueType(expected); err != nil {
		return nil, nil, err
	}
	c.emitByte(byte(chunk.OP_RETURN))

	c.patchJump(jumpEnd)
	return c.currentChunk, innerArgs[0], nil
}
