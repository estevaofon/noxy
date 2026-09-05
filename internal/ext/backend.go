package ext

import (
	"context"

	"github.com/estevaofon/noxy/internal/value"
)

// Backend e a fronteira que o VM enxerga de uma extensao carregada, seja
// qual for o transporte (wasm em processo ou plugin por processo — spec
// 2026-08-29 §1). Call e seguro para uso concorrente; Close e chamado uma
// vez, na saida do hospedeiro.
type Backend interface {
	Call(ctx context.Context, fnIndex int, args []value.Value) (value.Value, error)
	Close(ctx context.Context) error
}

var _ Backend = (*Module)(nil)
