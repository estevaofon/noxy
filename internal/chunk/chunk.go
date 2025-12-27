package chunk

import (
	"fmt"
	"noxy-vm/internal/value"
)

type OpCode byte

const (
	OP_CONSTANT OpCode = iota
	OP_NULL
	OP_POP
	OP_JUMP
	OP_JUMP_IF_FALSE
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
)

func (op OpCode) String() string {
	switch op {
	case OP_CONSTANT:
		return "OP_CONSTANT"
	case OP_NULL:
		return "OP_NULL"
	case OP_POP:
		return "OP_POP"
	case OP_JUMP:
		return "OP_JUMP"
	case OP_JUMP_IF_FALSE:
		return "OP_JUMP_IF_FALSE"
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
	case OP_DUP:
		return "OP_DUP"
	case OP_ARRAY:
		return "OP_ARRAY"
	case OP_MAP:
		return "OP_MAP"
	case OP_ZEROS:
		return "OP_ZEROS"
	default:
		return fmt.Sprintf("OP_%d", op)
	}
}

type Chunk struct {
	Code      []byte
	Constants []value.Value
	Lines     []int
}

func New() *Chunk {
	return &Chunk{
		Code:      []byte{},
		Constants: []value.Value{},
		Lines:     []int{},
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
	case OP_NEGATE:
		return c.simpleInstruction("OP_NEGATE", offset)
	case OP_PRINT:
		return c.simpleInstruction("OP_PRINT", offset)
	case OP_JUMP:
		return c.shortInstruction("OP_JUMP", offset)
	case OP_JUMP_IF_FALSE:
		return c.shortInstruction("OP_JUMP_IF_FALSE", offset)
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
