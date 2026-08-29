// internal/ext/process_bench_test.go
package ext

import (
	"context"
	"io"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

// benchGuest sobe o guest fora da medicao (o start e da primeira chamada).
func benchGuest(b *testing.B, concurrency string) *Process {
	b.Helper()
	path := exttest.BuildProcessGuest(b)
	p := NewProcess(guestManifest(b, path, concurrency, ""), ProcessConfig{Path: path, NoxyVersion: "bench", Log: io.Discard})
	b.Cleanup(func() { _ = p.Close(context.Background()) })
	if _, err := p.Call(context.Background(), fnNoop, nil); err != nil {
		b.Fatal(err)
	}
	return p
}

// Spec §11: quadro vazio, 1 KB, 1 MB — ao lado dos numeros do wasm em
// docs/EXTENSIONS.md.
func BenchmarkProcessRoundTripEmpty(b *testing.B) {
	p := benchGuest(b, "single")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Call(context.Background(), fnNoop, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func benchBytes(b *testing.B, size int) {
	p := benchGuest(b, "single")
	payload := value.NewBytes(strings.Repeat("x", size))
	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := p.Call(context.Background(), fnBytes, []value.Value{payload})
		if err != nil || len(got.Obj.(string)) != size {
			b.Fatalf("round trip: %v", err)
		}
	}
}

func BenchmarkProcessRoundTrip1KB(b *testing.B) { benchBytes(b, 1<<10) }
func BenchmarkProcessRoundTrip1MB(b *testing.B) { benchBytes(b, 1<<20) }

// Spec §11, aceitacao: com um handler que dorme 1 ms, "concurrent" precisa
// render pelo menos 4x o throughput de "single" (prova da multiplexacao).
// RunParallel usa GOMAXPROCS goroutines — rode com GOMAXPROCS >= 8.
func BenchmarkProcessConcurrent8(b *testing.B) {
	for _, mode := range []string{"single", "concurrent"} {
		b.Run(mode, func(b *testing.B) {
			p := benchGuest(b, mode)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := p.Call(context.Background(), fnSleep, []value.Value{value.NewInt(1)}); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
