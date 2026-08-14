package vm

import (
	"fmt"
	"strings"

	"noxy-vm/internal/chunk"
)

type RuntimeError struct {
	Rendered string
	Stack    string
}

func (failure *RuntimeError) Error() string {
	return failure.Rendered
}

func runtimeLocation(c *chunk.Chunk, ip int) (string, int) {
	file := "?"
	line := 0
	if c != nil {
		file = c.FileName
		if ip > 0 && ip <= len(c.Lines) {
			line = c.Lines[ip-1]
		}
	}
	return file, line
}

func (vm *VM) captureNoxyStack(activeChunk *chunk.Chunk, activeIP int) string {
	frames := make([]string, 0, vm.frameCount)
	for i := vm.frameCount - 1; i >= 0; i-- {
		frame := vm.frames[i]
		if frame == nil || frame.Closure == nil || frame.Closure.Function == nil {
			continue
		}
		c, _ := frame.Closure.Function.Chunk.(*chunk.Chunk)
		ip := frame.IP
		if i == vm.frameCount-1 {
			c = activeChunk
			ip = activeIP
		}
		file, line := runtimeLocation(c, ip)
		frames = append(frames, fmt.Sprintf("[%s:line %d] in %s", file, line, frame.Closure.Function.Name))
	}
	return strings.Join(frames, "\n")
}

func (vm *VM) newRuntimeError(c *chunk.Chunk, ip int, message string) error {
	file, line := runtimeLocation(c, ip)
	return &RuntimeError{
		Rendered: fmt.Sprintf("[%s:line %d] %s", file, line, message),
		Stack:    vm.captureNoxyStack(c, ip),
	}
}
