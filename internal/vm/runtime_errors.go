package vm

import (
	"fmt"
	"strings"

	"noxy-vm/internal/chunk"
)

// SourceLocation identifies a Noxy source position associated with a runtime error.
type SourceLocation struct {
	File string
	Line int
}

func (location SourceLocation) String() string {
	return fmt.Sprintf("%s:line %d", location.File, location.Line)
}

// RuntimeError adds Noxy source context and a captured Noxy stack to an
// underlying runtime failure.
type RuntimeError struct {
	Location SourceLocation
	Message  string
	Cause    error
	Stack    string
}

func (err *RuntimeError) Error() string {
	prefix := fmt.Sprintf("[%s] %s", err.Location, err.Message)
	return renderErrorCause(prefix, err.Cause)
}

func (err *RuntimeError) Unwrap() error {
	return err.Cause
}

// DeferredError describes a failure from a call registered by defer.
type DeferredError struct {
	Registration SourceLocation
	Cause        error
}

func (err DeferredError) Error() string {
	prefix := fmt.Sprintf("defer registered at %s failed", err.Registration)
	return renderErrorCause(prefix, err.Cause)
}

func (err DeferredError) Unwrap() error {
	return err.Cause
}

// UnwindError keeps the original failure and each deferred failure in execution order.
type UnwindError struct {
	Primary  error
	Deferred []DeferredError
}

func (err *UnwindError) Error() string {
	if err == nil {
		return "<nil>"
	}

	parts := make([]string, 0, len(err.Deferred)+1)
	if err.Primary != nil {
		parts = append(parts, err.Primary.Error())
	}
	for _, deferred := range err.Deferred {
		parts = append(parts, deferred.Error())
	}
	if len(parts) == 0 {
		return "runtime unwind failed"
	}
	return strings.Join(parts, "\n")
}

func (err *UnwindError) Unwrap() []error {
	if err == nil {
		return nil
	}
	causes := make([]error, 0, len(err.Deferred)+1)
	if err.Primary != nil {
		causes = append(causes, err.Primary)
	}
	for index := range err.Deferred {
		causes = append(causes, &err.Deferred[index])
	}
	return causes
}

func sourceLocation(c *chunk.Chunk, ip int) SourceLocation {
	location := SourceLocation{File: "?"}
	if c == nil {
		return location
	}

	location.File = c.FileName
	if ip > 0 && ip <= len(c.Lines) {
		location.Line = c.Lines[ip-1]
	}
	return location
}

// captureNoxyStack renderiza os frames Noxy vivos, do mais recente para o mais
// antigo. Para em vm.stackCaptureFloor — 0 fora de uma fronteira de
// call_result, ou seja, pilha completa como sempre; dentro da fronteira, o
// piso e o frame count do chamador, o que corta exatamente os frames abaixo
// (e inclusive) do ponto onde call_result foi chamado.
func (vm *VM) captureNoxyStack(activeChunk *chunk.Chunk, activeIP int) string {
	floor := vm.stackCaptureFloor
	if floor < 0 {
		floor = 0
	}
	if floor > vm.frameCount {
		floor = vm.frameCount
	}
	frames := make([]string, 0, vm.frameCount-floor)
	for i := vm.frameCount - 1; i >= floor; i-- {
		frame := &vm.frames[i]
		if frame.Closure == nil || frame.Closure.Function == nil {
			continue
		}
		c, _ := frame.Closure.Function.Chunk.(*chunk.Chunk)
		ip := frame.IP
		if i == vm.frameCount-1 {
			c = activeChunk
			ip = activeIP
		}
		location := sourceLocation(c, ip)
		frames = append(frames, fmt.Sprintf("[%s] in %s", location, frame.Closure.Function.Name))
	}
	return strings.Join(frames, "\n")
}

func deepestRuntimeStack(err error) string {
	if err == nil {
		return ""
	}

	current := ""
	if runtimeErr, ok := err.(*RuntimeError); ok {
		current = runtimeErr.Stack
	}

	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range wrapped.Unwrap() {
			if stack := deepestRuntimeStack(cause); stack != "" {
				return stack
			}
		}
	case interface{ Unwrap() error }:
		if stack := deepestRuntimeStack(wrapped.Unwrap()); stack != "" {
			return stack
		}
	}

	return current
}

func (vm *VM) runtimeErrorCause(c *chunk.Chunk, ip int, cause error, format string, args ...interface{}) error {
	stack := vm.captureNoxyStack(c, ip)
	if causeStack := deepestRuntimeStack(cause); causeStack != "" {
		stack = causeStack
	}
	return &RuntimeError{
		Location: sourceLocation(c, ip),
		Message:  fmt.Sprintf(format, args...),
		Cause:    cause,
		Stack:    stack,
	}
}

func (vm *VM) runtimeErrorAtCurrentFrame(format string, args ...interface{}) error {
	var activeChunk *chunk.Chunk
	activeIP := 0
	if frame := vm.currentFrame; frame != nil && frame.Closure != nil && frame.Closure.Function != nil {
		activeChunk, _ = frame.Closure.Function.Chunk.(*chunk.Chunk)
		activeIP = frame.IP
	}
	return vm.runtimeErrorCause(activeChunk, activeIP, nil, format, args...)
}

func renderErrorCause(prefix string, cause error) string {
	if cause == nil {
		return prefix
	}
	causeText := cause.Error()
	if !strings.Contains(causeText, "\n") {
		return prefix + ": " + causeText
	}
	return prefix + ":\n" + indentError(causeText, "  ")
}

func indentError(text, indent string) string {
	return indent + strings.ReplaceAll(text, "\n", "\n"+indent)
}

// AdvisedError separa o conselho de uso da mensagem de erro capturável: o
// texto do erro fica limpo (Failure.message, task failure map), e só a saída
// fatal do topo (cmd/noxy) imprime o Advice.
type AdvisedError struct {
	Err    error
	Advice string
}

func (err *AdvisedError) Error() string { return err.Err.Error() }
func (err *AdvisedError) Unwrap() error { return err.Err }
