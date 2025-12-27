package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

type Local struct {
	Name  string
	Depth int
}

type Compiler struct {
	currentChunk *chunk.Chunk
	locals       []Local
	scopeDepth   int
}

func New() *Compiler {
	return &Compiler{
		currentChunk: chunk.New(),
		locals:       []Local{},
		scopeDepth:   0,
	}
}

func (c *Compiler) Compile(node ast.Node) (*chunk.Chunk, error) {
	switch n := node.(type) {
	case *ast.Program:
		for _, stmt := range n.Statements {
			if _, err := c.Compile(stmt); err != nil {
				return nil, err
			}
		}
	case *ast.LetStmt:
		// Compile initializer
		if _, err := c.Compile(n.Value); err != nil {
			return nil, err
		}

		if c.scopeDepth > 0 {
			// Local variable
			c.addLocal(n.Name.Value)
			// Do NOT pop. The value stays on stack and becomes the local variable.
		} else {
			// Global
			nameConstant := c.makeConstant(value.NewString(n.Name.Value))
			c.emitBytes(byte(chunk.OP_SET_GLOBAL), nameConstant)
			c.emitByte(byte(chunk.OP_POP))
		}

	case *ast.ExpressionStmt:
		if _, err := c.Compile(n.Expression); err != nil {
			return nil, err
		}
		c.emitByte(byte(chunk.OP_POP)) // Pop expression result (stmt)

	case *ast.IntegerLiteral:
		// Convert int64 to Value
		constant := c.makeConstant(value.NewInt(n.Value))
		c.emitBytes(byte(chunk.OP_CONSTANT), constant)

	case *ast.FloatLiteral:
		constant := c.makeConstant(value.NewFloat(n.Value))
		c.emitBytes(byte(chunk.OP_CONSTANT), constant)

	case *ast.Boolean:
		if n.Value {
			c.emitByte(byte(chunk.OP_TRUE))
		} else {
			c.emitByte(byte(chunk.OP_FALSE))
		}

	case *ast.StringLiteral:
		constant := c.makeConstant(value.NewString(n.Value))
		c.emitBytes(byte(chunk.OP_CONSTANT), constant)

	case *ast.AssignStmt:
		if ident, ok := n.Target.(*ast.Identifier); ok {
			// Compile value
			if _, err := c.Compile(n.Value); err != nil {
				return nil, err
			}
			// Handle global assignment (Identifier)
			// Check local
			if arg := c.resolveLocal(ident.Value); arg != -1 {
				c.emitBytes(byte(chunk.OP_SET_LOCAL), byte(arg))
				c.emitByte(byte(chunk.OP_POP))
			} else {
				// Global
				nameConstant := c.makeConstant(value.NewString(ident.Value))
				c.emitBytes(byte(chunk.OP_SET_GLOBAL), nameConstant)
				c.emitByte(byte(chunk.OP_POP)) // Statement assignment pops result
			}
		} else if indexExp, ok := n.Target.(*ast.IndexExpression); ok {
			// Array Assignment: arr[i] = val
			// Compile Array
			if _, err := c.Compile(indexExp.Left); err != nil {
				return nil, err
			}
			// Compile Index
			if _, err := c.Compile(indexExp.Index); err != nil {
				return nil, err
			}
			// Compile Value
			if _, err := c.Compile(n.Value); err != nil {
				return nil, err
			}
			// Emit SET_INDEX
			c.emitByte(byte(chunk.OP_SET_INDEX))
			c.emitByte(byte(chunk.OP_POP)) // Pop result
		} else if memberExp, ok := n.Target.(*ast.MemberAccessExpression); ok {
			// Struct Field Assignment: obj.field = val
			if _, err := c.Compile(memberExp.Left); err != nil {
				return nil, err
			}
			// Value
			if _, err := c.Compile(n.Value); err != nil {
				return nil, err
			}
			// Field Name
			nameConst := c.makeConstant(value.NewString(memberExp.Member))
			c.emitBytes(byte(chunk.OP_SET_PROPERTY), nameConst)
			c.emitByte(byte(chunk.OP_POP))
		} else {
			return nil, fmt.Errorf("assignment target not supported yet")
		}

	case *ast.StructStatement:
		fields := []string{}
		for _, f := range n.FieldsList {
			fields = append(fields, f.Name)
		}
		structObj := value.NewStruct(n.Name, fields)
		structConst := c.makeConstant(structObj)
		c.emitBytes(byte(chunk.OP_CONSTANT), structConst)

		// Defines a global for the struct type (constructor)
		nameConst := c.makeConstant(value.NewString(n.Name))
		c.emitBytes(byte(chunk.OP_SET_GLOBAL), nameConst)
		c.emitByte(byte(chunk.OP_POP))

	case *ast.MemberAccessExpression:
		if _, err := c.Compile(n.Left); err != nil {
			return nil, err
		}
		nameConst := c.makeConstant(value.NewString(n.Member))
		c.emitBytes(byte(chunk.OP_GET_PROPERTY), nameConst)

	case *ast.ArrayLiteral:
		for _, el := range n.Elements {
			if _, err := c.Compile(el); err != nil {
				return nil, err
			}
		}
		// Count
		count := len(n.Elements)
		if count > 65535 {
			return nil, fmt.Errorf("array literal too large")
		}
		c.emitByte(byte(chunk.OP_ARRAY))
		c.emitByte(byte((count >> 8) & 0xff))
		c.emitByte(byte(count & 0xff))

	case *ast.IndexExpression:
		if _, err := c.Compile(n.Left); err != nil {
			return nil, err
		}
		if _, err := c.Compile(n.Index); err != nil {
			return nil, err
		}
		c.emitByte(byte(chunk.OP_GET_INDEX))

	case *ast.Identifier:
		// Check local
		if arg := c.resolveLocal(n.Value); arg != -1 {
			c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(arg))
		} else {
			// Global
			nameConstant := c.makeConstant(value.NewString(n.Value))
			c.emitBytes(byte(chunk.OP_GET_GLOBAL), nameConstant)
		}

	case *ast.InfixExpression:
		if _, err := c.Compile(n.Left); err != nil {
			return nil, err
		}
		if _, err := c.Compile(n.Right); err != nil {
			return nil, err
		}

		switch n.Operator {
		case "+":
			c.emitByte(byte(chunk.OP_ADD))
		case "-":
			c.emitByte(byte(chunk.OP_SUBTRACT))
		case "*":
			c.emitByte(byte(chunk.OP_MULTIPLY))
		case "/":
			c.emitByte(byte(chunk.OP_DIVIDE))
		case ">":
			c.emitByte(byte(chunk.OP_GREATER))
		case "<":
			c.emitByte(byte(chunk.OP_LESS))
		case "==":
			c.emitByte(byte(chunk.OP_EQUAL))
		case "!=": // != is NOT EQUAL
			c.emitByte(byte(chunk.OP_EQUAL))
			c.emitByte(byte(chunk.OP_NOT))
		case ">=": // >= is NOT LESS
			c.emitByte(byte(chunk.OP_LESS))
			c.emitByte(byte(chunk.OP_NOT))
		case "<=": // <= is NOT GREATER
			c.emitByte(byte(chunk.OP_GREATER))
			c.emitByte(byte(chunk.OP_NOT))
		case "|":
			c.emitByte(byte(chunk.OP_OR))
		case "&":
			c.emitByte(byte(chunk.OP_AND))
		case "%":
			c.emitByte(byte(chunk.OP_MODULO))
		default:
			return nil, fmt.Errorf("unknown operator %s", n.Operator)
		}

	case *ast.PrefixExpression:
		if _, err := c.Compile(n.Right); err != nil {
			return nil, err
		}
		if n.Operator == "-" {
			c.emitByte(byte(chunk.OP_NEGATE))
		} else if n.Operator == "!" {
			c.emitByte(byte(chunk.OP_NOT))
		} else if n.Operator == "ref" {
			// No-op for ref in expression?
			// Just pass the value (which is likely an object/pointer).
		}

	case *ast.NullLiteral:
		c.emitByte(byte(chunk.OP_NULL))

	case *ast.ZerosLiteral:
		if _, err := c.Compile(n.Size); err != nil {
			return nil, err
		}
		c.emitByte(byte(chunk.OP_ZEROS))

		// Handle AND/OR via logic?
		// InfixExpression generic handler is above.
		// I should modify InfixExpression case to handle AND/OR specifically if I want short circuit.
		// The current switch is at End. I need to move it UP or handle it special.
		// OR just use bool logic if I add OP_AND/OP_OR.
		// Let's add simple OP_AND / OP_OR to chunk. Simpler for now. Short circuit is optional for basic functionality (unless side effects matter).
		// Given the constraints and time, I'll add OP_AND / OP_OR to Chunk.

	case *ast.IfStatement:
		// Compile condition
		if _, err := c.Compile(n.Condition); err != nil {
			return nil, err
		}

		// Emit JumpIfFalse
		jumpToElse := c.emitJump(chunk.OP_JUMP_IF_FALSE)

		// Compile Then block (Consequence)
		c.emitByte(byte(chunk.OP_POP)) // Pop condition value (since we entered THEN)

		if _, err := c.Compile(n.Consequence); err != nil {
			return nil, err
		}

		// Emit Jump to End (skip Else)
		jumpToEnd := c.emitJump(chunk.OP_JUMP)

		// Patch Else jump
		c.patchJump(jumpToElse)

		c.emitByte(byte(chunk.OP_POP)) // Pop condition value (if we jumped here, condition was false)

		// Compile Else block (Alternative)
		if n.Alternative != nil {
			if _, err := c.Compile(n.Alternative); err != nil {
				return nil, err
			}
		}

		// Patch End jump
		c.patchJump(jumpToEnd)

	case *ast.WhileStatement:
		loopStart := len(c.currentChunk.Code)

		if _, err := c.Compile(n.Condition); err != nil {
			return nil, err
		}

		// Exit jump
		jumpToExit := c.emitJump(chunk.OP_JUMP_IF_FALSE)

		c.emitByte(byte(chunk.OP_POP)) // Pop condition

		if _, err := c.Compile(n.Body); err != nil {
			return nil, err
		}

		// Loop back
		c.emitLoop(loopStart)

		c.patchJump(jumpToExit)
		c.emitByte(byte(chunk.OP_POP)) // Pop condition at exit

	case *ast.ReturnStmt:
		if n.ReturnValue != nil {
			if _, err := c.Compile(n.ReturnValue); err != nil {
				return nil, err
			}
		} else {
			c.emitByte(byte(chunk.OP_NULL))
		}
		c.emitByte(byte(chunk.OP_RETURN))

	case *ast.FunctionStatement:
		// Create new compiler
		fnCompiler := New()
		fnCompiler.scopeDepth = 1 // Inside function body

		// Reserve slot 0 for function instance (recursion/closures)
		fnCompiler.addLocal("")

		// Add parameters as locals
		for _, param := range n.Parameters {
			fnCompiler.addLocal(param.Value)
		}

		// Compile body
		// Note: Body is BlockStatement. Compile it will handle statements.
		// BUT BlockStatement usually doesn't create new scope in `Compile` unless we tell it to?
		// Better: just compile statements inside.
		// Or assume BlockStatement will work fine.
		// One detail: Function body usually is a block.

		// We explicitly compile body statements to avoid extra scope nesting if BlockStatement adds one?
		// My BlockStatement compiler: for stmt in statements { compile(stmt) }. It does NOT call beginScope/endScope.
		// So we are good.

		if _, err := fnCompiler.Compile(n.Body); err != nil {
			return nil, err
		}

		// Implicit return null if end of function
		fnCompiler.emitBytes(byte(chunk.OP_NULL), byte(chunk.OP_RETURN))

		// Create Function Object
		fnObj := value.NewFunction(n.Name, len(n.Parameters), fnCompiler.currentChunk)

		// Emit Constant for Function
		fnConst := c.makeConstant(fnObj)
		c.emitBytes(byte(chunk.OP_CONSTANT), fnConst)

		// Store in Global
		nameConst := c.makeConstant(value.NewString(n.Name))
		c.emitBytes(byte(chunk.OP_SET_GLOBAL), nameConst)
		c.emitByte(byte(chunk.OP_POP)) // Consume function value from stack (since it's a stmt)

	case *ast.BlockStatement:
		c.beginScope()
		for _, stmt := range n.Statements {
			if _, err := c.Compile(stmt); err != nil {
				return nil, err
			}
		}
		c.endScope()

	case *ast.CallExpression:
		// Compile Function (Identifier or Expression)
		if _, err := c.Compile(n.Function); err != nil {
			return nil, err
		}

		// Compile Arguments
		for _, arg := range n.Arguments {
			if _, err := c.Compile(arg); err != nil {
				return nil, err
			}
		}

		// Emit Call
		c.emitBytes(byte(chunk.OP_CALL), byte(len(n.Arguments)))

	case nil:
		// Skip
	default:
		return nil, fmt.Errorf("unsupported node type %T", n)
	}

	// c.emitByte(byte(chunk.OP_RETURN)) // Added in Program only
	return c.currentChunk, nil
}

func (c *Compiler) emitByte(b byte) {
	c.currentChunk.Write(b, 0) // TODO: track lines
}

func (c *Compiler) emitBytes(b1, b2 byte) {
	c.emitByte(b1)
	c.emitByte(b2)
}

func (c *Compiler) emitJump(op chunk.OpCode) int {
	c.emitByte(byte(op))
	c.emitByte(0xff)
	c.emitByte(0xff)
	return len(c.currentChunk.Code) - 2
}

func (c *Compiler) patchJump(offset int) {
	// -2 to adjust for the jump instruction itself ip advancement?
	// Jump is relative to IP AFTER reading the offset.
	// Current len(c.currentChunk.Code) is target.
	// IP when jump executes is offset + 2.
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

func (c *Compiler) makeConstant(v value.Value) byte {
	i := c.currentChunk.AddConstant(v)
	if i > 255 {
		// handle large constants
		panic("too many constants")
	}
	return byte(i)
}

func (c *Compiler) beginScope() {
	c.scopeDepth++
}

func (c *Compiler) endScope() {
	c.scopeDepth--
	// Pop locals from stack
	for len(c.locals) > 0 && c.locals[len(c.locals)-1].Depth > c.scopeDepth {
		c.emitByte(byte(chunk.OP_POP))
		c.locals = c.locals[:len(c.locals)-1]
	}
}

func (c *Compiler) addLocal(name string) {
	c.locals = append(c.locals, Local{Name: name, Depth: c.scopeDepth})
}

func (c *Compiler) resolveLocal(name string) int {
	for i := len(c.locals) - 1; i >= 0; i-- {
		if c.locals[i].Name == name {
			return i
		}
	}
	return -1
}
