package ext

import (
	"context"
	"fmt"
	"strings"

	"noxy-vm/internal/value"
)

// acquire devolve uma instancia pronta e a funcao de release. No modo
// single, serializa por mutex; no stateless, um semaforo de vagas (slots)
// governa a capacidade: a vaga volta SEMPRE no release — inclusive quando a
// instancia foi envenenada e fechada — senao um trap com o pool esgotado
// deixaria goroutinas bloqueadas para sempre (lost wakeup; revisao do plano).
func (m *Module) acquire(ctx context.Context) (*instance, func(poisoned bool), error) {
	if m.pool == nil {
		m.mu.Lock()
		if m.failed {
			m.mu.Unlock()
			return nil, nil, fmt.Errorf("extension '%s' is poisoned by an earlier trap", m.Manifest.Name)
		}
		inst := m.single
		release := func(poisoned bool) {
			if poisoned {
				m.failed = true
				m.single = nil
				inst.mod.Close(context.Background())
			}
			m.mu.Unlock()
		}
		return inst, release, nil
	}

	<-m.slots // vaga de capacidade; devolvida incondicionalmente no release
	var inst *instance
	select {
	case inst = <-m.pool:
	default:
		created, err := m.newInstance(ctx)
		if err != nil {
			m.slots <- struct{}{}
			return nil, nil, err
		}
		inst = created
	}
	release := func(poisoned bool) {
		if poisoned {
			inst.mod.Close(context.Background())
		} else {
			m.pool <- inst
		}
		m.slots <- struct{}{}
	}
	return inst, release, nil
}

func (m *Module) Call(ctx context.Context, fnIndex int, args []value.Value) (value.Value, error) {
	name := m.Manifest.Name
	encoded, err := EncodeArgs(args, m.limits)
	if err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': %w", name, err)
	}
	inst, release, err := m.acquire(ctx)
	if err != nil {
		return value.NewNull(), err
	}
	poisoned := false
	defer func() { release(poisoned) }()

	state := &callState{}
	// Timeout por chamada: com WithCloseOnContextDone(true) no runtime, o
	// cancelamento derruba o guest em execucao — um loop infinito vira trap.
	timedCtx, cancel := context.WithTimeout(ctx, m.callTimeout)
	defer cancel()
	callCtx := context.WithValue(timedCtx, callStateKey{}, state)

	argsPtr := uint64(0)
	if len(encoded) != 0 {
		results, err := inst.alloc.Call(callCtx, uint64(len(encoded)))
		if err != nil {
			poisoned = true
			return value.NewNull(), fmt.Errorf("extension '%s' trapped: %v", name, err)
		}
		argsPtr = results[0]
		if !inst.mod.Memory().Write(uint32(argsPtr), encoded) {
			// nx_alloc devolveu um ponteiro que a propria memoria do guest nao
			// sustenta: o alocador esta mentindo, o que e equivalente a um trap.
			// Envenena para nao devolver ao pool uma instancia com estado de
			// alocador inconsistente (a entrada de argsPtr fica orfa, mas isso
			// deixa de importar — a instancia nunca mais e reusada).
			poisoned = true
			return value.NewNull(), fmt.Errorf("extension '%s': nx_alloc returned an out-of-memory region", name)
		}
	}

	results, err := inst.call.Call(callCtx, uint64(fnIndex), argsPtr, uint64(len(encoded)))
	if err != nil {
		poisoned = true
		return value.NewNull(), fmt.Errorf("extension '%s' trapped: %v", name, err)
	}
	// Os args so sao liberados DEPOIS de copiar o retorno: liberar antes
	// funcionaria hoje (o guest nao roda com o host no controle), mas e
	// fragilidade gratuita — revisao do plano.
	//
	// Um nx_free que trapa e tao grave quanto qualquer outro trap: o
	// alocador do guest pode ter ficado em estado inconsistente. O erro NAO
	// pode ser descartado — envenena a instancia (ela e fechada no release)
	// e vira o erro devolvido a chamada (achado de revisao).
	freeArgs := func() error {
		if len(encoded) != 0 {
			if _, err := inst.free.Call(callCtx, argsPtr, uint64(len(encoded))); err != nil {
				return err
			}
		}
		return nil
	}

	packed := results[0]
	if packed == 0 {
		if ferr := freeArgs(); ferr != nil {
			poisoned = true
			return value.NewNull(), fmt.Errorf("extension '%s' trapped: %v", name, ferr)
		}
		if state.failed {
			return value.NewNull(), fmt.Errorf("extension '%s' failed: %s", name, state.failMsg)
		}
		return value.NewNull(), fmt.Errorf("extension '%s': call returned 0 without nx_fail", name)
	}
	retPtr := uint32(packed >> 32)
	retLen := uint32(packed & 0xffffffff)
	data, ok := inst.mod.Memory().Read(retPtr, retLen)
	if !ok {
		if ferr := freeArgs(); ferr != nil {
			poisoned = true
			return value.NewNull(), fmt.Errorf("extension '%s' trapped: %v", name, ferr)
		}
		return value.NewNull(), fmt.Errorf("extension '%s': result region out of guest memory", name)
	}
	// Copia antes do free: data aponta para a memoria linear do guest.
	owned := make([]byte, len(data))
	copy(owned, data)
	if _, err := inst.free.Call(callCtx, uint64(retPtr), uint64(retLen)); err != nil {
		poisoned = true
		return value.NewNull(), fmt.Errorf("extension '%s' trapped: %v", name, err)
	}
	if ferr := freeArgs(); ferr != nil {
		poisoned = true
		return value.NewNull(), fmt.Errorf("extension '%s' trapped: %v", name, ferr)
	}

	result, err := DecodeValue(owned, m.limits)
	if err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': invalid result: %w", name, err)
	}
	declared := ""
	if fnIndex >= 0 && fnIndex < len(m.Manifest.Exports) {
		declared = m.Manifest.Exports[fnIndex].Returns
	}
	if err := checkDeclaredReturn(result, declared); err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': %w", name, err)
	}
	return result, nil
}

// checkDeclaredReturn confere a forma do valor devolvido contra o tipo
// declarado no manifesto (spec §6, "protocol violation"): uma extensao
// mentirosa e pega na fronteira, nao a jusante.
func checkDeclaredReturn(v value.Value, declared string) error {
	switch {
	case declared == "" || declared == "any":
		return nil
	case declared == "void":
		if v.Type != value.VAL_NULL {
			return fmt.Errorf("declared void but returned a value")
		}
	case declared == "int":
		if v.Type != value.VAL_INT {
			return typeMismatch(declared)
		}
	case declared == "float":
		if v.Type != value.VAL_FLOAT {
			return typeMismatch(declared)
		}
	case declared == "bool":
		if v.Type != value.VAL_BOOL {
			return typeMismatch(declared)
		}
	case declared == "string":
		if v.Type != value.VAL_OBJ {
			return typeMismatch(declared)
		}
		if _, ok := v.Obj.(string); !ok {
			return typeMismatch(declared)
		}
	case declared == "bytes":
		if v.Type != value.VAL_BYTES {
			return typeMismatch(declared)
		}
	case strings.HasSuffix(declared, "[]"):
		if v.Type != value.VAL_OBJ {
			return typeMismatch(declared)
		}
		if _, ok := v.Obj.(*value.ObjArray); !ok {
			return typeMismatch(declared)
		}
	default:
		// map[...]... e nomes de struct chegam como map (spec §3).
		if v.Type != value.VAL_OBJ {
			return typeMismatch(declared)
		}
		if _, ok := v.Obj.(*value.ObjMap); !ok {
			return typeMismatch(declared)
		}
	}
	return nil
}

func typeMismatch(declared string) error {
	return fmt.Errorf("result does not match declared return type %q", declared)
}
