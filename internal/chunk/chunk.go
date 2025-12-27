package chunk

import (
	"fmt"
	"noxy-vm/internal/value"
)

type OpCode byte

const (
	OP_CONSTANT OpCode = iota
	OP_NULL
	OP_TRUE
	OP_FALSE
	OP_POP
	OP_GET_GLOBAL
	OP_SET_GLOBAL
	OP_GET_LOCAL
	OP_SET_LOCAL
	OP_EQUAL
	OP_GREATER
	OP_LESS
	OP_ADD
	OP_SUBTRACT
	OP_MULTIPLY
	OP_DIVIDE
	OP_NOT
	OP_NEGATE
	OP_PRINT
	OP_JUMP
	OP_JUMP_IF_FALSE
	OP_LOOP
	OP_CALL
	OP_RETURN
	OP_ARRAY
	OP_GET_INDEX
	OP_SET_INDEX
	OP_GET_PROPERTY
	OP_SET_PROPERTY
)

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
