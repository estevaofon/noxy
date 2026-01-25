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
	IsParam    bool
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
	enclosing      *Compiler
	currentChunk   *chunk.Chunk
	locals         []Local
	globals        map[string]ast.NoxyType
	upvalues       []Upvalue
	scopeDepth     int
	loops          []*Loop
	currentLine    int
	FileName       string
	funcReturnType ast.NoxyType // Expected return type for current function context
	structs        map[string]*ast.StructStatement
}

func New() *Compiler {
	return NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), "")
}

func NewWithState(globals map[string]ast.NoxyType, structs map[string]*ast.StructStatement, fileName string) *Compiler {
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
	}
	c.currentChunk.FileName = fileName
	return c
}

func NewChild(parent *Compiler) *Compiler {
	c := &Compiler{
		enclosing:    parent,
		currentChunk: chunk.New(),
		locals:       []Local{},
		globals:      parent.globals,
		structs:      parent.structs,
		upvalues:     []Upvalue{},
		scopeDepth:   0,
		loops:        []*Loop{},
		currentLine:  parent.currentLine,
		FileName:     parent.FileName,
	}
	c.currentChunk.FileName = parent.FileName
	return c
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
				local := c.locals[arg]
				if refType, isRef := localType.(*ast.RefType); isRef && local.IsParam {
					// Assignment to a Reference Parameter (Locals are Pointers)
					// Logic:
					// 1. If RHS is also RefType and matches: REBIND (Pointer Update) -> OP_SET_LOCAL
					// 2. If RHS is ValueType and matches ElemType: UPDATE (Value Update) -> OP_STORE_VIA_REF

					isRefVal := false
					if valType != nil {
						_, isRefVal = valType.(*ast.RefType)
					}

					// 1. REBIND Check
					if isRefVal {
						// Types must match EXACTLY (ref T = ref T)
						if c.areTypesCompatible(refType, valType) {
							// REBINDING local reference
							c.emitBytes(byte(chunk.OP_SET_LOCAL), byte(arg))
							c.emitByte(byte(chunk.OP_POP))
							return c.currentChunk, nil, nil
						}
					}

					// 2. UPDATE Check (Auto-Deref RHS if needed?)
					// If RHS is RefType but we want Value, we deref RHS.
					// Original code did this. Let's keep it.
					if valRef, valIsRef := valType.(*ast.RefType); valIsRef {
						c.emitByte(byte(chunk.OP_DEREF)) // Turn Ref<T> into T
						valType = valRef.ElementType
					}

					if !c.areTypesCompatible(refType.ElementType, valType) {
						return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to reference '%s': expected %s (rebind) or %s (update), got %s", c.currentLine, ident.Value, refType.String(), refType.ElementType.String(), valType.String())
					}
					// UPDATE Value via Ref
					c.emitBytes(byte(chunk.OP_STORE_VIA_REF), byte(arg))
				} else {
					if !c.areTypesCompatible(localType, valType) {
						return nil, nil, fmt.Errorf("[line %d] type mismatch in assignment to '%s': expected %s, got %s", c.currentLine, ident.Value, localType.String(), valType.String())
					}
					c.emitBytes(byte(chunk.OP_SET_LOCAL), byte(arg))
					c.emitByte(byte(chunk.OP_POP))
				}
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

			// Auto-dereference collection if Ref
			if _, ok := leftType.(*ast.RefType); ok {
				c.emitByte(byte(chunk.OP_DEREF))
			}

			// 2. Compile Index
			// TODO: check index type?
			_, idxType, err := c.Compile(indexExp.Index)
			if err != nil {
				return nil, nil, err
			}

			// Auto-dereference index if Ref
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
				// Allow if 'any'
				if leftType != nil && leftType.String() != "any" {
					return nil, nil, fmt.Errorf("[line %d] index assignment on non-array/map type: %s", c.currentLine, leftType.String())
				}
			}

			c.emitByte(byte(chunk.OP_SET_INDEX))
			c.emitByte(byte(chunk.OP_POP))

		} else if memberExp, ok := n.Target.(*ast.MemberAccessExpression); ok {
			// Struct Field Assignment: obj.field = val
			// Stack Order: [Object, Value] -> OP_SET_PROPERTY

			// 1. Compile Object
			_, leftType, err := c.Compile(memberExp.Left)
			if err != nil {
				return nil, nil, err
			}

			// Auto-dereference if left is a Ref
			if _, ok := leftType.(*ast.RefType); ok {
				c.emitByte(byte(chunk.OP_DEREF))
			}

			// 2. Compile Value
			_, valType, err := c.Compile(n.Value) // Capturing valType
			if err != nil {
				return nil, nil, err
			}

			// RESOLVE FIELD TYPE:
			// Look up struct field type to enforce safety / enable update logic
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

			// TYPE-BASED ASSIGNMENT LOGIC:
			// 1. If Field is Ref:
			if fieldType != nil {
				if _, isRefField := fieldType.(*ast.RefType); isRefField {
					// Check Value Type
					// Allow NULL to be assigned to Ref (Rebind to null)
					isRefVal := false
					if valType != nil {
						_, isRefVal = valType.(*ast.RefType)
					}

					if isRefVal || valType == nil {
						// A) REBIND: ref field = ref val OR null -> OP_SET_PROPERTY (Change Pointer)
						// Must match ref types if not null
						if valType != nil {
							if !c.areTypesCompatible(fieldType, valType) {
								return nil, nil, fmt.Errorf("[line %d] type mismatch in rebind: expected %s, got %s", c.currentLine, fieldType.String(), valType.String())
							}
						}
						// Proceed to Standard Set Property (below)
					} else {
						// B) UPDATE: ref field = val -> Auto-Deref (*field = val)
						// Check if value matches ElementType
						// fieldType is Ref<T>, valType is T?
						refField := fieldType.(*ast.RefType)
						if c.areTypesCompatible(refField.ElementType, valType) {
							// Emit UPDATE logic:
							// Emit UPDATE logic: [Obj, Value]
							// Use OP_SET_PROPERTY_DEREF which handles: Obj.Field (Ref) -> *Ref = Value
							nameConst := c.makeConstant(value.NewString(memberExp.Member))
							c.emitBytes(byte(chunk.OP_SET_PROPERTY_DEREF), byte(nameConst))
							c.emitByte(byte(chunk.OP_POP)) // Result of assignment?

							// Wait, SET_PROPERTY returns Val. POP removes it (as Stmt).
							// SET_PROPERTY_DEREF should also return Val.

							return c.currentChunk, nil, nil

						} else {
							return nil, nil, fmt.Errorf("[line %d] type mismatch: expected %s (rebind) or %s (update), got %s", c.currentLine, fieldType.String(), refField.ElementType.String(), valType.String())
						}
					}

				}
				// Not a ref field, fallthrough to standard logic
			}
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

		// Create Constructor Signature
		paramTypes := []ast.NoxyType{}
		for _, f := range n.FieldsList {
			paramTypes = append(paramTypes, f.Type)
		}
		structType := &ast.FunctionType{
			Params: paramTypes,
			Return: &ast.PrimitiveType{Name: n.Name},
		}

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
			c.emitBytes(byte(chunk.OP_SET_GLOBAL), byte(nameConst))
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
		c.emitBytes(byte(chunk.OP_GET_PROPERTY), byte(nameConst))

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
					// Mixed values: Promote to ANY
					valType = &ast.PrimitiveType{Name: "any"}
					// Verify this new type is compatible with previous? "any" is compatible with everything in our logic.
					// But we need to ensure verify future elements?
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

		if _, ok := leftType.(*ast.RefType); ok {
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

		if _, ok := rightType.(*ast.RefType); ok {
			// Always deref ref types before comparison (including null comparison)
			c.emitByte(byte(chunk.OP_DEREF))
			if ref, ok := rightType.(*ast.RefType); ok {
				rightType = ref.ElementType
			}
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
		// Handle 'ref' operator specially - don't compile Right first
		if n.Operator == "ref" {
			// Handle 'ref' operator: ref x
			// Same logic as CallExpression 'isRefParam' block basically, but generalized?
			// But for now, just support what's needed.
			// Actually, 'ref x' should return a RefType wrapping type of x.

			// We MUST check what 'Right' is.
			if ident, ok := n.Right.(*ast.Identifier); ok {
				if argSlot, argType := c.resolveLocal(ident.Value); argSlot != -1 {
					if _, isRef := argType.(*ast.RefType); isRef {
						c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(argSlot))
					} else {
						c.emitBytes(byte(chunk.OP_REF_LOCAL), byte(argSlot))
						c.locals[argSlot].IsCaptured = true // Mark as captured
					}
					return c.currentChunk, &ast.RefType{ElementType: argType}, nil
				} else {
					// Global or Upvalue logic...
					// Upvalue...
					if -1 != c.resolveUpvalue(ident.Value) {
						return nil, nil, fmt.Errorf("[line %d] captured variables cannot be taken by reference", c.currentLine)
					}
					// Global
					nameConst := c.makeConstant(value.NewString(ident.Value))
					c.emitBytes(byte(chunk.OP_REF_GLOBAL), byte(nameConst))

					// Type?
					var t ast.NoxyType = &ast.PrimitiveType{Name: "any"}
					if gt, ok := c.resolveGlobalType(ident.Value); ok {
						t = gt
					}
					return c.currentChunk, &ast.RefType{ElementType: t}, nil
				}
			} else if memberExp, ok := n.Right.(*ast.MemberAccessExpression); ok {
				_, leftType, err := c.Compile(memberExp.Left)
				if err != nil {
					return nil, nil, err
				}
				if _, ok := leftType.(*ast.RefType); ok {
					c.emitByte(byte(chunk.OP_DEREF))
				}

				nameConst := c.makeConstant(value.NewString(memberExp.Member))
				c.emitBytes(byte(chunk.OP_REF_PROPERTY), byte(nameConst))
				return c.currentChunk, &ast.RefType{ElementType: &ast.PrimitiveType{Name: "any"}}, nil // Type approximation
			} else if indexExp, ok := n.Right.(*ast.IndexExpression); ok {
				_, leftType, err := c.Compile(indexExp.Left)
				if err != nil {
					return nil, nil, err
				}
				if _, ok := leftType.(*ast.RefType); ok {
					c.emitByte(byte(chunk.OP_DEREF))
				}

				_, idxType, err := c.Compile(indexExp.Index)
				if err != nil {
					return nil, nil, err
				}
				if _, ok := idxType.(*ast.RefType); ok {
					c.emitByte(byte(chunk.OP_DEREF))
				}

				c.emitByte(byte(chunk.OP_REF_INDEX))
				return c.currentChunk, &ast.RefType{ElementType: &ast.PrimitiveType{Name: "any"}}, nil
			} else {
				return nil, nil, fmt.Errorf("[line %d] invalid operand for 'ref' operator", c.currentLine)
			}
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
		_, condType, err := c.Compile(n.Condition)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := condType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
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

		_, condType, err := c.Compile(n.Condition)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := condType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
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
			c.addLocal(" $map", colType) // Consumes Map from stack

			// Get 'keys' global
			nameConst := c.makeConstant(value.NewString("keys"))
			c.emitBytes(byte(chunk.OP_GET_GLOBAL), byte(nameConst))

			// Get '$map' local
			slot := len(c.locals) - 1 // The last local added
			c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(slot))

			// Call keys(map)
			c.emitBytes(byte(chunk.OP_CALL), 1)
		}

		// 3. Store Collection in Local ($collection)
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
			_, valType, err := c.Compile(n.ReturnValue)
			if err != nil {
				return nil, nil, err
			}

			// Auto-dereference if returning Ref but function expects Value
			if c.funcReturnType != nil {
				// Check if func returns Ref?
				_, expectingRef := c.funcReturnType.(*ast.RefType)
				// Check if val is Ref
				_, isRef := valType.(*ast.RefType)

				if isRef && !expectingRef {
					// Implicit Dereference
					c.emitByte(byte(chunk.OP_DEREF))
					// Implicit Copy to ensure Value Semantics isolation on return
					c.emitByte(byte(chunk.OP_COPY))
				}
			}

		} else {
			c.emitByte(byte(chunk.OP_NULL))
		}
		c.emitByte(byte(chunk.OP_RETURN))
		return c.currentChunk, nil, nil

	case *ast.FunctionStatement:
		c.setLine(n.Token.Line)

		// Construct FunctionType for the global registry (Pre-register for recursion)
		paramTypes := []ast.NoxyType{}
		for _, p := range n.Parameters {
			paramTypes = append(paramTypes, p.Type)
		}
		// Return type undefined for now (any/void), ast doesn't strictly enforce it yet
		funcType := &ast.FunctionType{
			Params: paramTypes,
			Return: &ast.PrimitiveType{Name: "any"},
		}
		// Store in Global
		c.globals[n.Name] = funcType

		fnObj, fnCompiler, err := c.compileFunction(n.Name, n.Parameters, n.Body, n.ReturnType)
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

		fnObj, fnCompiler, err := c.compileFunction(fnName, n.Parameters, n.Body, n.ReturnType) // Literal return type? n.ReturnType? FunctionLiteral needs return type field if typed. Assuming inferred/any if nil.
		if err != nil {
			return nil, nil, err
		}
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

		// Construct FunctionType
		paramTypes := []ast.NoxyType{}
		for _, p := range n.Parameters {
			paramTypes = append(paramTypes, p.Type)
		}
		funcType := &ast.FunctionType{
			Params: paramTypes,
			Return: &ast.PrimitiveType{Name: "any"},
		}

		return c.currentChunk, funcType, nil

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
		// Check for special functions: chan_send, chan_recv
		if ident, ok := n.Function.(*ast.Identifier); ok {
			if ident.Value == "chan_send" {
				if len(n.Arguments) != 2 {
					return nil, nil, fmt.Errorf("[line %d] chan_send expects 2 arguments", c.currentLine)
				}
				_, _, err := c.Compile(n.Function)
				if err != nil {
					return nil, nil, err
				}

				// 2. Compile Arg 0 (Channel)
				_, chType, err := c.Compile(n.Arguments[0])
				if err != nil {
					return nil, nil, err
				}

				// Verify it is a channel OR any
				var isAnyChannel bool
				chanType, ok := chType.(*ast.ChanType)
				if !ok {
					// Check if it is 'any'
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

				// 3. Compile Arg 1 (Value)
				_, valType, err := c.Compile(n.Arguments[1])
				if err != nil {
					return nil, nil, err
				}

				// Verify Type Match (only if not any)
				if !isAnyChannel {
					if !c.areTypesCompatible(chanType.ElementType, valType) {
						return nil, nil, fmt.Errorf("[line %d] cannot send %s to %s", c.currentLine, valType.String(), chType.String())
					}
				}

				// Emit Call
				c.emitBytes(byte(chunk.OP_CALL), 2)
				return c.currentChunk, valType, nil // send returns value sent
			} else if ident.Value == "chan_recv" {
				if len(n.Arguments) != 1 {
					return nil, nil, fmt.Errorf("[line %d] chan_recv expects 1 argument", c.currentLine)
				}
				// 1. Compile Function
				_, _, err := c.Compile(n.Function)
				if err != nil {
					return nil, nil, err
				}

				// 2. Compile Arg (Channel)
				_, chType, err := c.Compile(n.Arguments[0])
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

				// Emit Call
				c.emitBytes(byte(chunk.OP_CALL), 1)
				return c.currentChunk, retType, nil
			}
		}

		// Normal Call
		_, fnType, err := c.Compile(n.Function)
		if err != nil {
			return nil, nil, err
		}

		funcType, isFunc := fnType.(*ast.FunctionType)

		// Compile Arguments
		for i, arg := range n.Arguments {
			isRefParam := false
			if isFunc && i < len(funcType.Params) {
				if _, ok := funcType.Params[i].(*ast.RefType); ok {
					isRefParam = true
				}
			}

			if isRefParam {
				// Handle explicit 'ref' operator in arguments
				// e.g. func(ref x) called as foo(ref x) or foo(x)
				// If user wrote 'ref x', it comes as PrefixExpression("-", x) (wait, token is REF?)
				// AST uses "ref" string for operator?
				// Let's check PrefixExpression below.
				// But first, UNWRAP if it is a "ref" prefix expression.

				actualArg := arg
				if prefixExp, ok := arg.(*ast.PrefixExpression); ok {
					if prefixExp.Operator == "ref" {
						actualArg = prefixExp.Right
					}
				}

				if ident, ok := actualArg.(*ast.Identifier); ok {
					if argSlot, argType := c.resolveLocal(ident.Value); argSlot != -1 {
						if _, isRef := argType.(*ast.RefType); isRef {
							// Already a reference, just pass it along
							c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(argSlot))
						} else {
							c.emitBytes(byte(chunk.OP_REF_LOCAL), byte(argSlot))
							c.locals[argSlot].IsCaptured = true // Mark as captured so it survives stack pop
						}
					} else if -1 != c.resolveUpvalue(ident.Value) {
						return nil, nil, fmt.Errorf("[line %d] captured variables (upvalues) cannot be passed by reference yet", c.currentLine)
					} else {
						// Check global type
						if globalType, ok := c.resolveGlobalType(ident.Value); ok {
							if _, isRef := globalType.(*ast.RefType); isRef {
								nameConst := c.makeConstant(value.NewString(ident.Value))
								c.emitBytes(byte(chunk.OP_GET_GLOBAL), byte(nameConst))
							} else {
								// Global value, create ref to it
								nameConst := c.makeConstant(value.NewString(ident.Value))
								c.emitBytes(byte(chunk.OP_REF_GLOBAL), byte(nameConst))
							}
						} else {
							// Unknown global (imported dynamic?), assume we need ref if param says so
							nameConst := c.makeConstant(value.NewString(ident.Value))
							c.emitBytes(byte(chunk.OP_REF_GLOBAL), byte(nameConst))
						}
					}
				} else if memberExp, ok := actualArg.(*ast.MemberAccessExpression); ok {
					// Member Access Ref: obj.prop
					_, leftType, err := c.Compile(memberExp.Left)
					if err != nil {
						return nil, nil, err
					}
					// Deref base object if ref
					if _, ok := leftType.(*ast.RefType); ok {
						c.emitByte(byte(chunk.OP_DEREF))
					}

					nameConst := c.makeConstant(value.NewString(memberExp.Member))
					c.emitBytes(byte(chunk.OP_REF_PROPERTY), byte(nameConst))
				} else if indexExp, ok := actualArg.(*ast.IndexExpression); ok {
					// Index Ref: arr[i]

					var leftType ast.NoxyType
					_, leftType, err = c.Compile(indexExp.Left) // Container
					if err != nil {
						return nil, nil, err
					}
					if _, ok := leftType.(*ast.RefType); ok {
						c.emitByte(byte(chunk.OP_DEREF))
					}

					var idxType ast.NoxyType
					_, idxType, err = c.Compile(indexExp.Index) // Index
					if err != nil {
						return nil, nil, err
					}
					if _, ok := idxType.(*ast.RefType); ok {
						c.emitByte(byte(chunk.OP_DEREF))
					}

					c.emitByte(byte(chunk.OP_REF_INDEX))
				} else if _, ok := actualArg.(*ast.NullLiteral); ok {
					// Pass NULL for Ref
					c.emitByte(byte(chunk.OP_NULL))
				} else {
					return nil, nil, fmt.Errorf("[line %d] argument %d is 'ref', must pass a variable, property, index, or null", c.currentLine, i+1)
				}
			} else {
				_, argType, err := c.Compile(arg)
				if err != nil {
					return nil, nil, err
				}
				if _, ok := argType.(*ast.RefType); ok {
					c.emitByte(byte(chunk.OP_DEREF))
				}
			}
		}

		// Emit Call
		c.emitBytes(byte(chunk.OP_CALL), byte(len(n.Arguments)))
		return c.currentChunk, &ast.PrimitiveType{Name: "any"}, nil // Return type unknown for now

	case nil:
		// Skip
		return c.currentChunk, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported node type %T", n)
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

func (c *Compiler) resolveGlobalType(name string) (ast.NoxyType, bool) {
	t, ok := c.globals[name]
	return t, ok
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
	// Structural Check for Maps and Arrays
	if expMap, ok := expected.(*ast.MapType); ok {
		if actMap, ok := actual.(*ast.MapType); ok {
			// Check Keys
			if !c.areTypesCompatible(expMap.KeyType, actMap.KeyType) {
				return false
			}
			// Check Values
			// If expected value is 'any', accept any actual value type
			if isAny(expMap.ValueType) {
				return true
			}
			return c.areTypesCompatible(expMap.ValueType, actMap.ValueType)
		}
	}

	if expArr, ok := expected.(*ast.ArrayType); ok {
		if actArr, ok := actual.(*ast.ArrayType); ok {
			// If expected element is 'any', accept any actual element type
			if isAny(expArr.ElementType) {
				return true
			}
			return c.areTypesCompatible(expArr.ElementType, actArr.ElementType)
		}
	}

	// 'any' type compatibility (if we had it explicitly)
	if expStr == "any" || isAny(expected) {
		return true // Expected 'any', accept anything
	}
	// 'any' in actual? (Unsafe? or Dynamic?)
	if actStr == "any" || isAny(actual) {
		return true // Actual is 'any', allow assignment to anything (dynamic)? Or strict?
		// Let's allow it for flexibility (like TypeScript 'any')
	}

	return false
}

func isAny(t ast.NoxyType) bool {
	if t == nil {
		return false
	}
	if pt, ok := t.(*ast.PrimitiveType); ok {
		if pt.Name == "any" || pt.Name == "func" {
			return true
		}
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
	}

	c.upvalues = append(c.upvalues, Upvalue{Index: index, IsLocal: isLocal})
	return len(c.upvalues) - 1
}

func (c *Compiler) compileFunction(name string, params []*ast.Parameter, body *ast.BlockStatement, returnType ast.NoxyType) (value.Value, *Compiler, error) {
	fnCompiler := NewChild(c)
	fnCompiler.scopeDepth = 1    // Inside function body
	fnCompiler.addLocal("", nil) // Reserve slot 0 for function instance
	fnCompiler.funcReturnType = returnType

	paramsInfo := []value.ParamInfo{}
	for _, param := range params {
		fnCompiler.addLocal(param.Name, param.Type)
		fnCompiler.locals[len(fnCompiler.locals)-1].IsParam = true // Mark as param
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
