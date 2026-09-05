package ext

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/estevaofon/noxy/internal/ext/exttest"
	"github.com/estevaofon/noxy/internal/value"
)

func loadTestModule(t *testing.T, concurrency string) *Module {
	t.Helper()
	m, err := LoadModule(context.Background(), exttest.BuildGuest(t, ""), testManifest(t, concurrency), wasiPermits)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	return m
}

func TestCallSha256RoundTrip(t *testing.T) {
	m := loadTestModule(t, "single")
	// Ida-e-volta completo usa fnIndex 3 (sha256): o retorno do guest e um
	// valor NXB legitimo (bytes). O echo (fnIndex 0) devolve o payload de
	// args verbatim — que NAO e um valor NXB unico — e por isso serve de
	// fixture para o teste de violacao de protocolo mais abaixo.
	sum, err := m.Call(context.Background(), 3, []value.Value{value.NewBytes("abc")})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if sum.Type != value.VAL_BYTES || len(sum.Obj.(string)) != 32 {
		t.Fatalf("sha256 must return 32 bytes, got %#v", sum)
	}
}

func TestCallEchoBytesRoundTrip(t *testing.T) {
	m := loadTestModule(t, "single")
	// fnIndex 6: copia pura sem compute, fixture do gate de overhead da §11
	// (BenchmarkExtRoundTrip1KB). Verifica que o guest devolve os mesmos
	// bytes que recebeu, encapsulados como NXB bytes valido.
	got, err := m.Call(context.Background(), 6, []value.Value{value.NewBytes("abc")})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got.Type != value.VAL_BYTES || got.Obj.(string) != "abc" {
		t.Fatalf("echobytes must return the input verbatim, got %#v", got)
	}
}

func TestCallFailBecomesError(t *testing.T) {
	m := loadTestModule(t, "single")
	_, err := m.Call(context.Background(), 1, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' failed: boom from guest") {
		t.Fatalf("declared failure must carry the guest message, got %v", err)
	}
	// Falha declarada NAO envenena: a proxima chamada funciona.
	if _, err := m.Call(context.Background(), 3, []value.Value{value.NewBytes("x")}); err != nil {
		t.Fatalf("call after fail: %v", err)
	}
}

func TestCallTrapPoisonsSingleMode(t *testing.T) {
	m := loadTestModule(t, "single")
	_, err := m.Call(context.Background(), 2, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' trapped") {
		t.Fatalf("trap must surface as trapped error, got %v", err)
	}
	_, err = m.Call(context.Background(), 3, []value.Value{value.NewBytes("x")})
	if err == nil || !strings.Contains(err.Error(), "poisoned by an earlier trap") {
		t.Fatalf("single mode must stay poisoned, got %v", err)
	}
}

func TestCallTrapReplacesStatelessInstance(t *testing.T) {
	m := loadTestModule(t, "stateless")
	if _, err := m.Call(context.Background(), 2, nil); err == nil {
		t.Fatal("trap must error")
	}
	// Stateless: instancia envenenada e descartada, a proxima chamada cria
	// uma fresca (spec §6).
	if _, err := m.Call(context.Background(), 3, []value.Value{value.NewBytes("x")}); err != nil {
		t.Fatalf("stateless must recover after trap: %v", err)
	}
}

func TestCallStatelessConcurrent(t *testing.T) {
	m := loadTestModule(t, "stateless")
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Call(context.Background(), 3, []value.Value{value.NewBytes("payload")})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent call: %v", err)
		}
	}
}

func TestCallProtocolViolation(t *testing.T) {
	m := loadTestModule(t, "single")
	// guest_echo (fnIndex 0) declara "any" e devolve o payload de args cru,
	// que nao e um valor NXB unico: violacao de protocolo (o decode falha),
	// nomeando a extensao. (A checagem de tipo declarado tem teste proprio
	// abaixo — aqui returns = "any" a pula.)
	_, err := m.Call(context.Background(), 0, []value.Value{value.NewInt(1)})
	if err == nil || !strings.Contains(err.Error(), "guest") {
		t.Fatalf("protocol violation must name the extension, got %v", err)
	}
}

func TestCallDeclaredTypeMismatch(t *testing.T) {
	m := loadTestModule(t, "single")
	// guest_badtype (fnIndex 5) declara returns = "int" no manifesto mas
	// devolve uma string NXB valida: checkDeclaredReturn TEM de recusar.
	_, err := m.Call(context.Background(), 5, nil)
	if err == nil || !strings.Contains(err.Error(), `declared return type "int"`) {
		t.Fatalf("declared-type mismatch must be enforced, got %v", err)
	}
}

func TestCallTimeoutBecomesTrap(t *testing.T) {
	m, err := LoadModule(context.Background(), exttest.BuildGuest(t, ""), testManifest(t, "single"),
		LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}, CallTimeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	start := time.Now()
	_, err = m.Call(context.Background(), 4, nil) // guest_loop: for {}
	if err == nil || !strings.Contains(err.Error(), "trapped") {
		t.Fatalf("infinite loop must become a trap via context cancellation, got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("timeout did not bound the call")
	}
}

func TestCallStatelessTrapDoesNotDeadlock(t *testing.T) {
	// MaxInstances = 1: o trap fecha a instancia SEM devolve-la ao pool; a
	// chamada seguinte so avanca se a VAGA (slot) for liberada. Regressao
	// do lost-wakeup apontado na revisao do plano. Rodar com -race.
	m, err := LoadModule(context.Background(), exttest.BuildGuest(t, ""), testManifest(t, "stateless"),
		LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}, MaxInstances: 1})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	if _, err := m.Call(context.Background(), 2, nil); err == nil {
		t.Fatal("trap must error")
	}
	done := make(chan error, 1)
	go func() {
		_, callErr := m.Call(context.Background(), 3, []value.Value{value.NewBytes("x")})
		done <- callErr
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("call after trap: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("deadlock: slot was not released after poisoned instance")
	}
}
