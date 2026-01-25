package chunk

import (
	"fmt"
	"noxy-vm/internal/value"
)

type OpCode byte

const (
	OP_CONSTANT OpCode = iota
	OP_CONSTANT_LONG
	OP_NULL
	OP_POP
	OP_JUMP
	OP_JUMP_IF_FALSE
	OP_JUMP_IF_TRUE
	OP_LOOP
	OP_TRUE
	OP_FALSE
	OP_GET_GLOBAL
	OP_SET_GLOBAL
	OP_GET_LOCAL
	OP_SET_LOCAL
	OP_GET_UPVALUE
	OP_SET_UPVALUE
	OP_GET_PROPERTY
	OP_SET_PROPERTY
	OP_GET_INDEX
	OP_SET_INDEX
	OP_ADD
	OP_SUBTRACT
	OP_MULTIPLY
	OP_DIVIDE
	OP_MODULO
	OP_NOT
	OP_NEGATE
	OP_GREATER
	OP_LESS
	OP_EQUAL
	OP_AND
	OP_OR
	OP_BIT_AND
	OP_BIT_OR
	OP_BIT_XOR
	OP_BIT_NOT
	OP_SHIFT_LEFT
	OP_SHIFT_RIGHT
	OP_CALL
	OP_INVOKE
	OP_RETURN
	OP_PRINT
	OP_IMPORT
	OP_IMPORT_FROM_ALL
	OP_DUP
	OP_ARRAY
	OP_MAP
	OP_ZEROS
	OP_LEN
	OP_ADD_INT
	OP_SUB_INT
	OP_MUL_INT
	OP_DIV_INT
	OP_MOD_INT
	OP_LESS_INT
	OP_GREATER_INT
	OP_EQUAL_INT
	OP_SELECT
	OP_CLOSURE       // [const_index] [upvalue_count] [is_local, index]...
	OP_CLOSE_UPVALUE // Explicit instruction to close upvalues
	OP_REF_LOCAL
	OP_REF_GLOBAL
	OP_REF_PROPERTY
	OP_REF_INDEX
	OP_DEREF
	OP_STORE_VIA_REF
	OP_COPY
)

func (op OpCode) String() string {
	switch op {
	case OP_CONSTANT:
		return "OP_CONSTANT"
	case OP_CONSTANT_LONG:
		return "OP_CONSTANT_LONG"
	case OP_NULL:
		return "OP_NULL"
	case OP_POP:
		return "OP_POP"
	case OP_JUMP:
		return "OP_JUMP"
	case OP_JUMP_IF_FALSE:
		return "OP_JUMP_IF_FALSE"
	case OP_JUMP_IF_TRUE:
		return "OP_JUMP_IF_TRUE"
	case OP_LOOP:
		return "OP_LOOP"
	case OP_TRUE:
		return "OP_TRUE"
	case OP_FALSE:
		return "OP_FALSE"
	case OP_GET_GLOBAL:
		return "OP_GET_GLOBAL"
	case OP_SET_GLOBAL:
		return "OP_SET_GLOBAL"
	case OP_GET_LOCAL:
		return "OP_GET_LOCAL"
	case OP_SET_LOCAL:
		return "OP_SET_LOCAL"
	case OP_GET_UPVALUE:
		return "OP_GET_UPVALUE"
	case OP_SET_UPVALUE:
		return "OP_SET_UPVALUE"
	case OP_GET_PROPERTY:
		return "OP_GET_PROPERTY"
	case OP_SET_PROPERTY:
		return "OP_SET_PROPERTY"
	case OP_GET_INDEX:
		return "OP_GET_INDEX"
	case OP_SET_INDEX:
		return "OP_SET_INDEX"
	case OP_ADD:
		return "OP_ADD"
	case OP_SUBTRACT:
		return "OP_SUBTRACT"
	case OP_MULTIPLY:
		return "OP_MULTIPLY"
	case OP_DIVIDE:
		return "OP_DIVIDE"
	case OP_MODULO:
		return "OP_MODULO"
	case OP_NOT:
		return "OP_NOT"
	case OP_NEGATE:
		return "OP_NEGATE"
	case OP_GREATER:
		return "OP_GREATER"
	case OP_LESS:
		return "OP_LESS"
	case OP_EQUAL:
		return "OP_EQUAL"
	case OP_AND:
		return "OP_AND"
	case OP_OR:
		return "OP_OR"
	case OP_BIT_AND:
		return "OP_BIT_AND"
	case OP_BIT_OR:
		return "OP_BIT_OR"
	case OP_BIT_XOR:
		return "OP_BIT_XOR"
	case OP_BIT_NOT:
		return "OP_BIT_NOT"
	case OP_SHIFT_LEFT:
		return "OP_SHIFT_LEFT"
	case OP_SHIFT_RIGHT:
		return "OP_SHIFT_RIGHT"
	case OP_CALL:
		return "OP_CALL"
	case OP_INVOKE:
		return "OP_INVOKE"
	case OP_RETURN:
		return "OP_RETURN"
	case OP_PRINT:
		return "OP_PRINT"
	case OP_IMPORT:
		return "OP_IMPORT"
	case OP_IMPORT_FROM_ALL:
		return "OP_IMPORT_FROM_ALL"
	case OP_CLOSURE:
		return "OP_CLOSURE"
	case OP_CLOSE_UPVALUE:
		return "OP_CLOSE_UPVALUE"
	case OP_REF_LOCAL:
		return "OP_REF_LOCAL"
	case OP_REF_GLOBAL:
		return "OP_REF_GLOBAL"
	case OP_REF_PROPERTY:
		return "OP_REF_PROPERTY"
	case OP_REF_INDEX:
		return "OP_REF_INDEX"
	case OP_DEREF:
		return "OP_DEREF"
	case OP_STORE_VIA_REF:
		return "OP_STORE_VIA_REF"
	case OP_DUP:
		return "OP_DUP"
	case OP_ARRAY:
		return "OP_ARRAY"
	case OP_MAP:
		return "OP_MAP"
	case OP_ADD_INT:
		return "OP_ADD_INT"
	case OP_SUB_INT:
		return "OP_SUB_INT"
	case OP_MUL_INT:
		return "OP_MUL_INT"
	case OP_DIV_INT:
		return "OP_DIV_INT"
	case OP_MOD_INT:
		return "OP_MOD_INT"
	case OP_LESS_INT:
		return "OP_LESS_INT"
	case OP_GREATER_INT:
		return "OP_GREATER_INT"
	case OP_EQUAL_INT:
		return "OP_EQUAL_INT"
	case OP_ZEROS:
		return "OP_ZEROS"
	case OP_LEN:
		return "OP_LEN"
	case OP_SELECT:
		return "OP_SELECT"
	default:
		return fmt.Sprintf("OP_%d", op)
	}
}

type Chunk struct {
	Code      []byte
	Constants []value.Value
	Lines     []int
	FileName  string
}

func New() *Chunk {
	return &Chunk{
		Code:      []byte{},
		Constants: []value.Value{},
		Lines:     []int{},
		FileName:  "",
	}
}

func (c *Chunk) Write(byteCode byte, line int) {
	c.Code = append(c.Code, byteCode)
	c.Lines = append(c.Lines, line)
}

func (c *Chunk) AddConstant(v value.Value) int {
	c.Constants = append(c.Constants, v)
	return len(c.Constants) - 1
}

func (c *Chunk) Disassemble(name string) {
	fmt.Printf("== %s ==\n", name)

	for offset := 0; offset < len(c.Code); {
		offset = c.disassembleInstruction(offset)
	}
}

// DisassembleAll disassembles this chunk and all nested function chunks
func (c *Chunk) DisassembleAll(name string) {
	c.Disassemble(name)

	// Disassemble nested functions
	for _, constant := range c.Constants {
		if constant.Type == value.VAL_FUNCTION {
			if fn, ok := constant.Obj.(*value.ObjFunction); ok {
				if fnChunk, ok := fn.Chunk.(*Chunk); ok {
					fmt.Println()
					fnChunk.DisassembleAll(fn.Name)
				}
			}
		}
	}
}

func (c *Chunk) disassembleInstruction(offset int) int {
	fmt.Printf("%04d ", offset)
	if offset > 0 && c.Lines[offset] == c.Lines[offset-1] {
		fmt.Printf("   | ")
	} else {
		fmt.Printf("%4d ", c.Lines[offset])
	}

	instruction := OpCode(c.Code[offset])
	switch instruction {
	case OP_CONSTANT:
		return c.constantInstruction("OP_CONSTANT", offset)
	case OP_CONSTANT_LONG:
		return c.constantLongInstruction("OP_CONSTANT_LONG", offset)
	case OP_NULL:
		return c.simpleInstruction("OP_NULL", offset)
	case OP_TRUE:
		return c.simpleInstruction("OP_TRUE", offset)
	case OP_FALSE:
		return c.simpleInstruction("OP_FALSE", offset)
	case OP_POP:
		return c.simpleInstruction("OP_POP", offset)
	case OP_GET_GLOBAL:
		return c.constantInstruction("OP_GET_GLOBAL", offset)
	case OP_SET_GLOBAL:
		return c.constantInstruction("OP_SET_GLOBAL", offset)
	case OP_GET_LOCAL:
		return c.byteInstruction("OP_GET_LOCAL", offset)
	case OP_SET_LOCAL:
		return c.byteInstruction("OP_SET_LOCAL", offset)
	case OP_EQUAL:
		return c.simpleInstruction("OP_EQUAL", offset)
	case OP_GREATER:
		return c.simpleInstruction("OP_GREATER", offset)
	case OP_LESS:
		return c.simpleInstruction("OP_LESS", offset)
	case OP_ADD:
		return c.simpleInstruction("OP_ADD", offset)
	case OP_SUBTRACT:
		return c.simpleInstruction("OP_SUBTRACT", offset)
	case OP_MULTIPLY:
		return c.simpleInstruction("OP_MULTIPLY", offset)
	case OP_DIVIDE:
		return c.simpleInstruction("OP_DIVIDE", offset)
	case OP_NOT:
		return c.simpleInstruction("OP_NOT", offset)
	case OP_AND:
		return c.simpleInstruction("OP_AND", offset)
	case OP_OR:
		return c.simpleInstruction("OP_OR", offset)
	case OP_BIT_AND:
		return c.simpleInstruction("OP_BIT_AND", offset)
	case OP_BIT_OR:
		return c.simpleInstruction("OP_BIT_OR", offset)
	case OP_BIT_XOR:
		return c.simpleInstruction("OP_BIT_XOR", offset)
	case OP_BIT_NOT:
		return c.simpleInstruction("OP_BIT_NOT", offset)
	case OP_SHIFT_LEFT:
		return c.simpleInstruction("OP_SHIFT_LEFT", offset)
	case OP_SHIFT_RIGHT:
		return c.simpleInstruction("OP_SHIFT_RIGHT", offset)
	case OP_NEGATE:
		return c.simpleInstruction("OP_NEGATE", offset)
	case OP_PRINT:
		return c.simpleInstruction("OP_PRINT", offset)
	case OP_JUMP:
		return c.shortInstruction("OP_JUMP", offset)
	case OP_JUMP_IF_FALSE:
		return c.shortInstruction("OP_JUMP_IF_FALSE", offset)
	case OP_JUMP_IF_TRUE:
		return c.shortInstruction("OP_JUMP_IF_TRUE", offset)
	case OP_LOOP:
		return c.shortInstruction("OP_LOOP", offset)
	case OP_CALL:
		return c.byteInstruction("OP_CALL", offset)
	case OP_RETURN:
		return c.simpleInstruction("OP_RETURN", offset)
	case OP_ARRAY:
		return c.shortInstruction("OP_ARRAY", offset)
	case OP_GET_INDEX:
		return c.simpleInstruction("OP_GET_INDEX", offset)
	case OP_SET_INDEX:
		return c.simpleInstruction("OP_SET_INDEX", offset)
	case OP_GET_PROPERTY:
		return c.constantInstruction("OP_GET_PROPERTY", offset)
	case OP_SET_PROPERTY:
		return c.constantInstruction("OP_SET_PROPERTY", offset)
	case OP_ZEROS:
		return c.simpleInstruction("OP_ZEROS", offset)
	case OP_LEN:
		return c.simpleInstruction("OP_LEN", offset)
	case OP_MODULO:
		return c.simpleInstruction("OP_MODULO", offset)
	case OP_MAP:
		return c.shortInstruction("OP_MAP", offset)
	case OP_IMPORT:
		return c.constantInstruction("OP_IMPORT", offset)
	case OP_IMPORT_FROM_ALL:
		return c.simpleInstruction("OP_IMPORT_FROM_ALL", offset)
	case OP_DUP:
		return c.simpleInstruction("OP_DUP", offset)
	case OP_ADD_INT:
		return c.simpleInstruction("OP_ADD_INT", offset)
	case OP_SUB_INT:
		return c.simpleInstruction("OP_SUB_INT", offset)
	case OP_MUL_INT:
		return c.simpleInstruction("OP_MUL_INT", offset)
	case OP_DIV_INT:
		return c.simpleInstruction("OP_DIV_INT", offset)
	case OP_MOD_INT:
		return c.simpleInstruction("OP_MOD_INT", offset)
	case OP_LESS_INT:
		return c.simpleInstruction("OP_LESS_INT", offset)
	case OP_GREATER_INT:
		return c.simpleInstruction("OP_GREATER_INT", offset)
	case OP_EQUAL_INT:
		return c.simpleInstruction("OP_EQUAL_INT", offset)
	case OP_SELECT:
		return c.byteInstruction("OP_SELECT", offset)
	case OP_CLOSURE:
		return c.closureInstruction("OP_CLOSURE", offset)
	case OP_CLOSE_UPVALUE:
		return c.simpleInstruction("OP_CLOSE_UPVALUE", offset)
	case OP_REF_LOCAL:
		return c.byteInstruction("OP_REF_LOCAL", offset)
	case OP_REF_GLOBAL:
		return c.constantInstruction("OP_REF_GLOBAL", offset)
	case OP_REF_PROPERTY:
		return c.constantInstruction("OP_REF_PROPERTY", offset)
	case OP_REF_INDEX:
		return c.simpleInstruction("OP_REF_INDEX", offset)
	case OP_DEREF:
		return c.simpleInstruction("OP_DEREF", offset)
	case OP_STORE_VIA_REF:
		return c.byteInstruction("OP_STORE_VIA_REF", offset)
	case OP_COPY:
		return c.simpleInstruction("OP_COPY", offset)
	default:
		fmt.Printf("Unknown opcode %d\n", instruction)
		return offset + 1
	}
}

func (c *Chunk) simpleInstruction(name string, offset int) int {
	fmt.Printf("%s\n", name)
	return offset + 1
}

func (c *Chunk) constantInstruction(name string, offset int) int {
	constant := c.Code[offset+1]
	fmt.Printf("%-16s %4d '", name, constant)
	fmt.Print(c.Constants[constant])
	fmt.Printf("'\n")
	return offset + 2
}

func (c *Chunk) byteInstruction(name string, offset int) int {
	slot := c.Code[offset+1]
	fmt.Printf("%-16s %4d\n", name, slot)
	return offset + 2
}

func (c *Chunk) shortInstruction(name string, offset int) int {
	slot := uint16(c.Code[offset+1])<<8 | uint16(c.Code[offset+2])
	fmt.Printf("%-16s %4d\n", name, slot)
	return offset + 3
}

func (c *Chunk) constantLongInstruction(name string, offset int) int {
	constant := uint16(c.Code[offset+1])<<8 | uint16(c.Code[offset+2])
	fmt.Printf("%-16s %4d '", name, constant)
	if int(constant) < len(c.Constants) {
		fmt.Print(c.Constants[constant])
	} else {
		fmt.Print("?")
	}
	fmt.Printf("'\n")
	return offset + 3
}

func (c *Chunk) closureInstruction(name string, offset int) int {
	offset++
	constant := c.Code[offset]
	offset++
	fmt.Printf("%-16s %4d ", name, constant)
	fmt.Print(c.Constants[constant])
	fmt.Println()

	// Check bounds
	if offset < len(c.Code) {

		val := c.Constants[constant]
		if _, ok := val.Obj.(*value.ObjFunction); ok {
			// fn := val.Obj.(*value.ObjFunction)
			// count = fn.UpvalueCount
		}
	}
	return offset
}
