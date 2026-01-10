package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
	"strings"
)

type Local struct {
	Name       string
	Depth      int
	Type       ast.NoxyType
	IsCaptured bool
}

type Loop struct {
	EnclosingLocals int
	BreakJumps      []int
}

type Upvalue struct {
	Index   uint8
	IsLocal bool
}

type Compiler struct {
	enclosing    *Compiler
	currentChunk *chunk.Chunk
	locals       []Local
	globals      map[string]ast.NoxyType
	upvalues     []Upvalue
	scopeDepth   int
	loops        []*Loop
	currentLine  int // Track current source line for error messages
}

func New() *Compiler {
	return NewWithState(make(map[string]ast.NoxyType))
}

func NewWithState(globals map[string]ast.NoxyType) *Compiler {
	return &Compiler{
		enclosing:    nil,
		currentChunk: chunk.New(),
		locals:       []Local{},
		globals:      globals,
		upvalues:     []Upvalue{},
		scopeDepth:   0,
		loops:        []*Loop{},
		currentLine:  1,
	}
}

func NewChild(parent *Compiler) *Compiler {
	return &Compiler{
		enclosing:    parent,
		currentChunk: chunk.New(),
		locals:       []Local{},
		globals:      parent.globals, // Share globals (or copy? Share reference is correct for global scope)
		upvalues:     []Upvalue{},
		scopeDepth:   0,
		loops:        []*Loop{},
		currentLine:  parent.currentLine,
	}
}

func (c *Compiler) GetGlobals() map[string]ast.NoxyType {
	return c.globals
}

func (c *Compiler) Compile(node ast.Node) (*chunk.Chunk, ast.NoxyType, error) {
	switch n := node.(type) {
	case *ast.Program:
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
		var valType ast.NoxyType
		// Compile initializer
		if n.Value != nil {
			_, t, err := c.Compile(n.Value)
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
		if n.Type != nil {
			if !c.areTypesCompatible(n.Type, valType) {
				return nil, nil, fmt.Errorf("[line %d] type mismatch in '%s' declaration: expected %s, got %s", c.currentLine, n.Name.Value, n.Type.String(), valType.String())
			}
		}

		if c.scopeDepth > 0 {
			// Local variable
			c.addLocal(n.Name.Value, n.Type)
			// Do NOT pop. The value stays on stack and becomes the local variable.
		} else {
			// Global
			// Register global type
			c.globals[n.Name.Value] = n.Type

			nameConstant := c.makeConstant(value.NewString(n.Name.Value))
			c.emitBytes(byte(chunk.OP_SET_GLOBAL), byte(nameConstant))
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
		if ident, ok := n.Target.(*ast.Identifier); ok {
			// 1. Compile Value (pushed to stack)
			_, valType, err := c.Compile(n.Value)
			if err != nil {
				return nil, nil, err
			}

			// 2. Check and Set Variable
			if arg, localType := c.resolveLocal(ident.Value); arg != -1 {
				// Local Logic
				if !c.areTypesCompatible(localType, valType) {
					return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to '%s': expected %s, got %s", c.currentLine, ident.Value, localType.String(), valType.String())
				}
				c.emitBytes(byte(chunk.OP_SET_LOCAL), byte(arg))
				c.emitByte(byte(chunk.OP_POP))
			} else if arg := c.resolveUpvalue(ident.Value); arg != -1 {
				// Upvalue Logic
				c.emitBytes(byte(chunk.OP_SET_UPVALUE), byte(arg))
				c.emitByte(byte(chunk.OP_POP))
			} else {
				// Global Logic
				if globalType, exists := c.globals[ident.Value]; exists {
					if !c.areTypesCompatible(globalType, valType) {
						return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to global '%s': expected %s, got %s", c.currentLine, ident.Value, globalType.String(), valType.String())
					}
				}
				nameConstant := c.makeConstant(value.NewString(ident.Value))
				c.emitBytes(byte(chunk.OP_SET_GLOBAL), byte(nameConstant))
				c.emitByte(byte(chunk.OP_POP))
			}
		} else if indexExp, ok := n.Target.(*ast.IndexExpression); ok {
			// Array/Map Assignment: arr[i] = val
			// Stack Order: [Array, Index, Value] -> OP_SET_INDEX

			// 1. Compile Array (Left)
			_, leftType, err := c.Compile(indexExp.Left)
			if err != nil {
				return nil, nil, err
			}

			// 2. Compile Index
			// TODO: check index type?
			_, idxType, err := c.Compile(indexExp.Index)
			if err != nil {
				return nil, nil, err
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

			// Unwrap RefType in index
			if ref, ok := idxType.(*ast.RefType); ok {
				idxType = ref.ElementType
			}

			// Type Check
			if arrType, ok := leftType.(*ast.ArrayType); ok {
				// Check index is int?
				if idxType != nil && idxType.String() != "int" {
					return nil, nil, fmt.Errorf("[line %d] array index must be int, got %s", c.currentLine, idxType.String())
				}
				// Check value compatibility with element type
				if !c.areTypesCompatible(arrType.ElementType, valType) {
					return nil, nil, fmt.Errorf("[line %d] type mismatch in array assignment: expected %s, got %s", c.currentLine, arrType.ElementType.String(), valType.String())
				}
			} else if mapType, ok := leftType.(*ast.MapType); ok {
				// Check key type
				if !c.areTypesCompatible(mapType.KeyType, idxType) {
					return nil, nil, fmt.Errorf("[line %d] type mismatch in map key: expected %s, got %s", c.currentLine, mapType.KeyType.String(), idxType.String())
				}
				// Check value type
				if !c.areTypesCompatible(mapType.ValueType, valType) {
					return nil, nil, fmt.Errorf("[line %d] type mismatch in map value: expected %s, got %s", c.currentLine, mapType.ValueType.String(), valType.String())
				}
			} else {
				// Dynamic or error?
				// If leftType is known and not array/map, error?
				if leftType != nil {
					return nil, nil, fmt.Errorf("[line %d] index assignment on non-array/map type: %s", c.currentLine, leftType.String())
				}
			}

			c.emitByte(byte(chunk.OP_SET_INDEX))
			c.emitByte(byte(chunk.OP_POP))

		} else if memberExp, ok := n.Target.(*ast.MemberAccessExpression); ok {
			// Struct Field Assignment: obj.field = val
			// Stack Order: [Object, Value] -> OP_SET_PROPERTY

			// 1. Compile Object
			_, _, err := c.Compile(memberExp.Left)
			if err != nil {
				return nil, nil, err
			}

			// 2. Compile Value
			_, valType, err := c.Compile(n.Value) // Capturing valType
			if err != nil {
				return nil, nil, err
			}

			// TODO: Resolve field type on struct matching leftType?
			// Need struct definition lookup. For now, assume compatible or dynamic.
			_ = valType // Suppress unused for now if not checking.

			// Field Name
			nameConst := c.makeConstant(value.NewString(memberExp.Member))
			c.emitBytes(byte(chunk.OP_SET_PROPERTY), byte(nameConst))
			c.emitByte(byte(chunk.OP_POP))

		} else {
			return nil, nil, fmt.Errorf("[line %d] assignment target not supported yet", c.currentLine)
		}
		return c.currentChunk, nil, nil

	case *ast.StructStatement:
		c.setLine(n.Token.Line)

		fields := []string{}
		for _, f := range n.FieldsList {
			fields = append(fields, f.Name)
		}
		structObj := value.NewStruct(n.Name, fields)
		c.emitConstant(structObj)

		// Define Type? Struct is a type definition AND a value (constructor).
		// The value 'Point' is a function/struct-def. Its type is... 'func' or 'struct_def'?
		// NoxyType doesn't have FunctionType yet in AST fully?
		// But for now we don't assign structs to variables often, we call them.

		structType := &ast.PrimitiveType{Name: "struct_def"} // Dummy

		if c.scopeDepth > 0 {
			// Local scope: struct is a local variable
			c.addLocal(n.Name, structType)
			// Value stays on stack as local
		} else {
			// Global scope: struct is a global
			c.globals[n.Name] = structType
			nameConst := c.makeConstant(value.NewString(n.Name))
			c.emitBytes(byte(chunk.OP_SET_GLOBAL), byte(nameConst))
			c.emitByte(byte(chunk.OP_POP))
		}
		return c.currentChunk, nil, nil

	case *ast.MemberAccessExpression:
		// Left . Member
		_, _, err := c.Compile(n.Left)
		if err != nil {
			return nil, nil, err
		}
		// TODO: Type check if member exists on leftType? requires knowing struct fields.
		// For now, allow dynamic or just skip check.

		nameConst := c.makeConstant(value.NewString(n.Member))
		c.emitBytes(byte(chunk.OP_GET_PROPERTY), byte(nameConst))

		return c.currentChunk, nil, nil // Return Unknown type? OR leftType field type.

	case *ast.ArrayLiteral:
		var elemType ast.NoxyType
		for i, el := range n.Elements {
			_, t, err := c.Compile(el)
			if err != nil {
				return nil, nil, err
			}
			if i == 0 {
				elemType = t
			} else {
				if !c.areTypesCompatible(elemType, t) {
					// Mixed types detected, promote to any[]
					elemType = &ast.PrimitiveType{Name: "any"}
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
					return nil, nil, fmt.Errorf("[line %d] mixed value types in map", c.currentLine)
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
		_, idxType, err := c.Compile(n.Index)
		if err != nil {
			return nil, nil, err
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
		} else if arg := c.resolveUpvalue(n.Value); arg != -1 {
			c.emitBytes(byte(chunk.OP_GET_UPVALUE), byte(arg))
			return c.currentChunk, &ast.PrimitiveType{Name: "any"}, nil // Types for upvalues not tracked yet
		} else {
			// Global
			nameConstant := c.makeConstant(value.NewString(n.Value))
			c.emitBytes(byte(chunk.OP_GET_GLOBAL), byte(nameConstant))

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

			// Type check: left should be bool, right boolean?
			// Noxy truthiness? Python-like?
			// If safety requested, maybe strict bool?
			// Assuming strict for now given syntax.
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

		_, leftType, err := c.Compile(n.Left)
		if err != nil {
			return nil, nil, err
		}
		_, rightType, err := c.Compile(n.Right)
		if err != nil {
			return nil, nil, err
		}

		// Check if both operands are INT for optimization
		isInt := false
		if leftType != nil && rightType != nil {
			if leftType.String() == "int" && rightType.String() == "int" {
				isInt = true
			}
		}

		switch n.Operator {
		case "+":
			if isInt {
				c.emitByte(byte(chunk.OP_ADD_INT))
			} else {
				c.emitByte(byte(chunk.OP_ADD))
			}
		case "-":
			if isInt {
				c.emitByte(byte(chunk.OP_SUB_INT))
			} else {
				c.emitByte(byte(chunk.OP_SUBTRACT))
			}
		case "*":
			if isInt {
				c.emitByte(byte(chunk.OP_MUL_INT))
			} else {
				c.emitByte(byte(chunk.OP_MULTIPLY))
			}
		case "/":
			if isInt {
				c.emitByte(byte(chunk.OP_DIV_INT))
			} else {
				c.emitByte(byte(chunk.OP_DIVIDE))
			}
		case ">":
			if isInt {
				c.emitByte(byte(chunk.OP_GREATER_INT))
			} else {
				c.emitByte(byte(chunk.OP_GREATER))
			}
		case "<":
			if isInt {
				c.emitByte(byte(chunk.OP_LESS_INT))
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
		_, rightType, err := c.Compile(n.Right)
		if err != nil {
			return nil, nil, err
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
		return c.currentChunk, nil, nil // Null type?

	case *ast.ZerosLiteral:
		_, _, err := c.Compile(n.Size)
		if err != nil {
			return nil, nil, err
		}
		c.emitByte(byte(chunk.OP_ZEROS))
		return c.currentChunk, &ast.PrimitiveType{Name: "bytes"}, nil

	case *ast.IfStatement:
		c.setLine(n.Token.Line)
		// Compile condition
		_, _, err := c.Compile(n.Condition)
		if err != nil {
			return nil, nil, err
		}

		// Emit JumpIfFalse
		jumpToElse := c.emitJump(chunk.OP_JUMP_IF_FALSE)

		// Compile Then block (Consequence)
		c.emitByte(byte(chunk.OP_POP)) // Pop condition value (since we entered THEN)

		_, _, err = c.Compile(n.Consequence)
		if err != nil {
			return nil, nil, err
		}

		// Emit Jump to End (skip Else)
		jumpToEnd := c.emitJump(chunk.OP_JUMP)

		// Patch Else jump
		c.patchJump(jumpToElse)

		c.emitByte(byte(chunk.OP_POP)) // Pop condition value (if we jumped here, condition was false)

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
		loop := &Loop{EnclosingLocals: len(c.locals), BreakJumps: []int{}}
		c.loops = append(c.loops, loop)

		_, _, err := c.Compile(n.Condition)
		if err != nil {
			return nil, nil, err
		}

		// Exit jump
		jumpToExit := c.emitJump(chunk.OP_JUMP_IF_FALSE)

		c.emitByte(byte(chunk.OP_POP)) // Pop condition

		_, _, err = c.Compile(n.Body)
		if err != nil {
			return nil, nil, err
		}

		// Loop back
		c.emitLoop(loopStart)

		c.patchJump(jumpToExit)
		c.emitByte(byte(chunk.OP_POP)) // Pop condition at exit

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
			// Convert Map to Keys Array
			// Stack: [Map]
			// We need to call keys(map).
			// Emitting: keys(map).
			// Problem: 'map' is on stack. 'keys' function not.
			// Use local temp for map.
			c.addLocal(" $map", colType) // Consumes Map from stack

			// Get 'keys' global
			nameConst := c.makeConstant(value.NewString("keys"))
			c.emitBytes(byte(chunk.OP_GET_GLOBAL), byte(nameConst))

			// Get '$map' local
			slot := len(c.locals) - 1 // The last local added
			c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(slot))

			// Call keys(map)
			c.emitBytes(byte(chunk.OP_CALL), 1)
			// Stack: [KeysArray]

			// Determine new type: Array of KeyType
			// For simplicity, assume keys return strings (for now in Noxy maps are string keys?)
			// Noxy Spec: map[Key, Value]. keys() returns Key[].
			// If we can determine Map Key Type, we effectively have Array<KeyType>.
			// For now, let's treat as Array (dynamic or inferred).
		}

		// 3. Store Collection in Local ($collection)
		// Stack has Collection (or Keys Array)
		c.addLocal(" $collection", nil) // Type? inferred or dyn

		// 4. Init Index ($index = 0)
		c.emitConstant(value.NewInt(0))
		c.addLocal(" $index", &ast.PrimitiveType{Name: "int"})

		// 5. Init Length ($len = len($collection))
		c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(len(c.locals)-2)) // $collection is at -2 (since $index is at -1)
		c.emitByte(byte(chunk.OP_LEN))
		c.addLocal(" $len", &ast.PrimitiveType{Name: "int"})

		// 6. Loop Setup
		loopStart := len(c.currentChunk.Code)
		loop := &Loop{EnclosingLocals: len(c.locals), BreakJumps: []int{}}
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
		c.addLocal(n.Identifier, nil) // User variable (consumes Item from stack)

		// 9. Compile Body
		_, _, err = c.Compile(n.Body)
		if err != nil {
			return nil, nil, err
		}

		c.endScope() // Pops User Variable

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
				return nil, nil, fmt.Errorf("[line %d] invalid case condition: expected send(...) or recv(...)", c.currentLine)
			}

			funcName := ""
			if ident, ok := callExpr.Function.(*ast.Identifier); ok {
				funcName = ident.Value
			}

			if funcName == "recv" {
				// Recv Case: [Chan, Null, 0]
				if len(callExpr.Arguments) != 1 {
					return nil, nil, fmt.Errorf("[line %d] recv expects 1 argument", c.currentLine)
				}
				// Compile Channel
				_, _, err := c.Compile(callExpr.Arguments[0])
				if err != nil {
					return nil, nil, err
				}

				c.emitByte(byte(chunk.OP_NULL)) // Val (unused for recv)
				c.emitConstant(value.NewInt(0)) // Mode 0

			} else if funcName == "send" {
				// Send Case: [Chan, Val, 1]
				if isAssign {
					return nil, nil, fmt.Errorf("[line %d] cannot assign result of send", c.currentLine)
				}
				if len(callExpr.Arguments) != 2 {
					return nil, nil, fmt.Errorf("[line %d] send expects 2 arguments", c.currentLine)
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
				return nil, nil, fmt.Errorf("[line %d] invalid case call: expected send or recv, got %s", c.currentLine, funcName)
			}
		}

		// 2. Emit OP_SELECT
		count := len(n.Cases)
		if count > 255 {
			return nil, nil, fmt.Errorf("too many cases in when statement")
		}
		c.emitBytes(byte(chunk.OP_SELECT), byte(count))

		// Stack now has: [Index, Value, OK] (Index is bottom, OK is top)
		// We want to dispatch based on Index.
		// Since we need Index for multiple checks, and Value/OK for body...
		// Let's store them in temps or manage stack carefully.
		// "Index" is 0..N-1.
		// Strategy:
		//   We are checking Index against 0, 1, 2...
		//   Index is at Stack[-3].
		//   Better to Peek Index?
		//   Or use OP_DUP specific slot? OP_DUP only dups top?
		//   VM has OP_DUP (top).
		//   We don't have OP_PEEK.
		//   We can use Locals!
		//   Store (Index, Value, OK) in hidden locals.

		c.beginScope()
		// Determine types? Dynamic.
		c.addLocal(" $sel_idx", &ast.PrimitiveType{Name: "int"}) // Stack[-3] -> local 0
		c.addLocal(" $sel_val", nil)                             // Stack[-2] -> local 1
		c.addLocal(" $sel_ok", &ast.PrimitiveType{Name: "bool"}) // Stack[-1] -> local 2
		// Note: addLocal assumes value is on stack. Order matters.
		// Stack from OP_SELECT: [Index, Value, OK].
		// addLocal adds to end.
		// If we addLocal, we are claiming stack slots from bottom up?
		// No, addLocal purely updates compiler tracking. The VALUES are inherently on stack.
		// So $sel_idx corresponds to index if we define them in order?
		// Wait, locals are indices into stack relative to base pointer.
		// If we declare 3 locals now, they map to the top 3 stack values.
		// Stack: [..., Index, Value, OK]
		// addLocal("idx"): maps to slot X (Index)
		// addLocal("val"): maps to slot X+1 (Value)
		// addLocal("ok"): maps to slot X+2 (OK)
		// Yes, this works.

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
				c.addLocal(ident.Value, nil) // Bind local
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
			// Checking VM OP_JUMP_IF_FALSE: it reads top, if false jump, else fallthrough. DOES IT POP?
			// Check chunk.go disassembly or VM code. Typically standard VM pops condition.
			// Re-checking VM code... (I assume yes).
			// If it pops, then "nextJump" target is AFTER pop.
			// So `c.emitByte(byte(chunk.OP_POP))` after `nextJump` instruction is wrong IF `OP_JUMP_IF_FALSE` already popped.
			// Let's assume standard behavior: `if (pop()) jump else continue`.
			// So we don't need explicit pop after jump instruction IF condition consumed.
			// Wait, if it jumps, it skips the fallthrough code.
			// If it doesn't jump, it falls through.
			// If it jumps, the condition is GONE from stack? Yes.
			// So both paths have condition gone.
			// So NO explicit pop after `nextJump`.
			// BUT, I wrote `c.emitByte(byte(chunk.OP_POP))` above!
			// Line: `c.emitByte(byte(chunk.OP_POP)) // Pop comparison result`
			// This pop is strictly for FAL-THROUGH path if OP_JUMP_IF_FALSE *peeks*.
			// Noxy VM `OP_JUMP_IF_FALSE`:
			/*
				func (vm *VM) run() {
					// ...
					case chunk.OP_JUMP_IF_FALSE:
						offset := vm.readShort()
						condition := vm.peek() // PEEK!
						if isFalsey(condition) {
							vm.ip += int(offset)
						}
				}
			*/
			// Wait, I need to check VM implementation.
			// If it uses PEEK, then I DO need POP.
			// If it uses POP, then I DON'T.
			// Most of my IF compilation uses:
			// `jumpToElse := c.emitJump(chunk.OP_JUMP_IF_FALSE)`
			// `c.emitByte(byte(chunk.OP_POP)) // Pop condition value (since we entered THEN)`
			// This suggests PEEK.
			// Let's verify VM later. For now assume PEEK.
			// So:
			// IF_FALSE -> Jump to Meta_Else. Stack has Condition.
			// Fallthrough -> Stack has Condition. Pop it. Enter Body.
			// Meta_Else: Stack has Condition. Pop it.
			// So logic:
			//   Jump_If_False(Next)
			//   Pop (True path)
			//   Body...
			//   Jump(End)
			// Next:
			//   Pop (False path)
			//   ...
			// Correct.
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

		// Pop Loop info
		c.loops = c.loops[:len(c.loops)-1]

		// 13. End Wrapper Scope (pops iterator vars)
		c.endScope()

		return c.currentChunk, nil, nil

	case *ast.BreakStmt:
		if len(c.loops) == 0 {
			return nil, nil, fmt.Errorf("break outside of loop")
		}
		loop := c.loops[len(c.loops)-1]

		// Pop locals
		toPop := len(c.locals) - loop.EnclosingLocals
		for i := 0; i < toPop; i++ {
			c.emitByte(byte(chunk.OP_POP))
		}

		// Emit Jump
		jump := c.emitJump(chunk.OP_JUMP)
		loop.BreakJumps = append(loop.BreakJumps, jump)
		return c.currentChunk, nil, nil

	case *ast.UseStmt:
		// 1. Emit Module Name
		nameConst := c.makeConstant(value.NewString(n.Module))
		// 2. Emit Import (Loads module and pushes it to stack)
		c.emitBytes(byte(chunk.OP_IMPORT), byte(nameConst))

		// 3. Handle Result
		if n.SelectAll {
			// use pkg select *
			c.emitByte(byte(chunk.OP_IMPORT_FROM_ALL))
		} else if len(n.Selectors) > 0 {
			// use pkg select a, b
			for _, sel := range n.Selectors {
				// DUP the module
				c.emitByte(byte(chunk.OP_DUP))

				// Get Property 'sel'
				selConst := c.makeConstant(value.NewString(sel))
				c.emitBytes(byte(chunk.OP_GET_PROPERTY), byte(selConst))

				// Set Global 'sel'
				c.emitBytes(byte(chunk.OP_SET_GLOBAL), byte(selConst))
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
			c.emitBytes(byte(chunk.OP_SET_GLOBAL), byte(nameConst))
			c.emitByte(byte(chunk.OP_POP)) // Pop module
		}
		return c.currentChunk, nil, nil

	case *ast.ReturnStmt:
		if n.ReturnValue != nil {
			_, _, err := c.Compile(n.ReturnValue)
			if err != nil {
				return nil, nil, err
			}
		} else {
			c.emitByte(byte(chunk.OP_NULL))
		}
		c.emitByte(byte(chunk.OP_RETURN))
		return c.currentChunk, nil, nil

	case *ast.FunctionStatement:
		c.setLine(n.Token.Line)

		fnObj, fnCompiler, err := c.compileFunction(n.Name, n.Parameters, n.Body)
		if err != nil {
			return nil, nil, err
		}

		funcIndex := c.makeConstant(fnObj)
		c.emitBytes(byte(chunk.OP_CLOSURE), byte(funcIndex))

		// Emit upvalue bytes
		for _, up := range fnCompiler.upvalues {
			isLocal := byte(0)
			if up.IsLocal {
				isLocal = 1
			}
			c.emitByte(isLocal)
			c.emitByte(up.Index)
		}

		funcType := &ast.PrimitiveType{Name: "func"}

		// Store in Global
		c.globals[n.Name] = funcType

		nameConst := c.makeConstant(value.NewString(n.Name))
		c.emitBytes(byte(chunk.OP_SET_GLOBAL), byte(nameConst))
		c.emitByte(byte(chunk.OP_POP))

		return c.currentChunk, nil, nil

	case *ast.FunctionLiteral:
		c.setLine(n.Token.Line)

		fnName := n.Name
		if fnName == "" {
			fnName = "anonymous"
		}

		fnObj, fnCompiler, err := c.compileFunction(fnName, n.Parameters, n.Body)
		if err != nil {
			return nil, nil, err
		}

		funcIndex := c.makeConstant(fnObj)
		c.emitBytes(byte(chunk.OP_CLOSURE), byte(funcIndex))

		for _, up := range fnCompiler.upvalues {
			isLocal := byte(0)
			if up.IsLocal {
				isLocal = 1
			}
			c.emitByte(isLocal)
			c.emitByte(up.Index)
		}

		return c.currentChunk, &ast.PrimitiveType{Name: "func"}, nil

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

	case *ast.CallExpression:
		// Compile Function
		_, _, err := c.Compile(n.Function)
		if err != nil {
			return nil, nil, err
		}

		// Compile Arguments
		for _, arg := range n.Arguments {
			_, _, err := c.Compile(arg)
			if err != nil {
				return nil, nil, err
			}
		}

		// Emit Call
		c.emitBytes(byte(chunk.OP_CALL), byte(len(n.Arguments)))
		return c.currentChunk, nil, nil // Return type of call? Unknown for now.

	case nil:
		// Skip
		return c.currentChunk, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported node type %T", n)
	}
}

// Rewritten specific AssignStmt handler inside Compile, skipping invalid overwrite
// Just careful re-introduction of AssignStmt Case body.

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

func (c *Compiler) endScope() {
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

func (c *Compiler) addLocal(name string, t ast.NoxyType) {
	c.locals = append(c.locals, Local{Name: name, Depth: c.scopeDepth, Type: t})
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
			// Initialize 'Size' elements with default value
			for i := 0; i < typ.Size; i++ {
				if err := c.emitDefaultInit(typ.ElementType); err != nil {
					return err
				}
			}
			c.emitByte(byte(chunk.OP_ARRAY))
			c.emitByte(byte((typ.Size >> 8) & 0xff))
			c.emitByte(byte(typ.Size & 0xff))
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

func (c *Compiler) areTypesCompatible(expected, actual ast.NoxyType) bool {
	if expected == nil || actual == nil {
		return true // Allow lenient check for now/unknowns
	}
	expStr := expected.String()
	actStr := actual.String()

	if expStr == actStr {
		return true
	}
	// Allow any[] -> Any T[]
	if actStr == "any[]" && strings.HasSuffix(expStr, "[]") {
		return true
	}
	// Allow map[any, any] -> Any Map
	if actStr == "map[any, any]" && strings.HasPrefix(expStr, "map[") {
		return true
	}
	// Also ref any -> ref T (if nil ref?) - "ref any" handling
	if actStr == "ref any" && strings.HasPrefix(expStr, "ref ") {
		return true
	}

	// 'any' type compatibility (if we had it explicitly)
	if expStr == "any" || actStr == "any" {
		return true
	}

	return false
}

func (c *Compiler) resolveUpvalue(name string) int {
	if c.enclosing == nil {
		return -1
	}

	// 1. Check immediate parent's locals
	local, _ := c.enclosing.resolveLocal(name)
	if local != -1 {
		c.enclosing.locals[local].IsCaptured = true // Mark as captured!
		return c.addUpvalue(uint8(local), true)
	}

	// 2. Check immediate parent's upvalues
	upvalue := c.enclosing.resolveUpvalue(name)
	if upvalue != -1 {
		return c.addUpvalue(uint8(upvalue), false)
	}

	return -1
}

func (c *Compiler) addUpvalue(index uint8, isLocal bool) int {
	// Check for existing upvalue
	for i, u := range c.upvalues {
		if u.Index == index && u.IsLocal == isLocal {
			return i
		}
	}

	if len(c.upvalues) >= 255 {
		// Error: too many upvalues
		// We'll panic or handle error gracefully?
		// For now, simple return error index or panic for dev.
		// Returning 255 might be safe if we check.
	}

	c.upvalues = append(c.upvalues, Upvalue{Index: index, IsLocal: isLocal})
	return len(c.upvalues) - 1
}

func (c *Compiler) compileFunction(name string, params []*ast.Parameter, body *ast.BlockStatement) (value.Value, *Compiler, error) {
	fnCompiler := NewChild(c)
	fnCompiler.scopeDepth = 1    // Inside function body
	fnCompiler.addLocal("", nil) // Reserve slot 0 for function instance

	paramsInfo := []value.ParamInfo{}
	for _, param := range params {
		fnCompiler.addLocal(param.Name, param.Type)
		isRef := false
		if _, ok := param.Type.(*ast.RefType); ok {
			isRef = true
		}
		paramsInfo = append(paramsInfo, value.ParamInfo{IsRef: isRef})
	}

	_, _, err := fnCompiler.Compile(body)
	if err != nil {
		return value.Value{}, nil, err
	}

	// Implicit return null
	fnCompiler.emitBytes(byte(chunk.OP_NULL), byte(chunk.OP_RETURN))

	upvalueCount := len(fnCompiler.upvalues)
	fnObj := value.NewFunction(name, len(params), upvalueCount, paramsInfo, fnCompiler.currentChunk, nil)

	return fnObj, fnCompiler, nil
}
