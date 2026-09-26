// Package modsrc e a UNICA origem de modulos do Noxy: compilador (descoberta
// de exports e tipos) e VM (carga em runtime) resolvem `use` por Source e
// nao sabem de onde o fonte veio (spec 2026-09-26 §7). Na v1 ha uma
// implementacao, DiskSource; a v2 (bytecode) adiciona a leitura do payload.
package modsrc

import "errors"

type Kind uint8

const (
	KindFile      Kind = iota // arquivo .nx (inclusive <dir>/<dir>.nx e <dir>/main.nx)
	KindDirectory             // diretorio sem entrada: cada .nx e subdiretorio e submodulo
	KindEmbedded              // stdlib embutida (stdlib.FS); Content preenchido
)

type Module struct {
	Name    string // "src.session"
	Kind    Kind
	Path    string // absoluto e limpo; "" para KindEmbedded
	Content string // so KindEmbedded
}

type Entry struct {
	Name  string
	IsDir bool
}

// ErrNotFound: nenhum candidato no disco e nada na stdlib embutida. Quem
// chama acrescenta a dica de `noxy --sync` (pkgmanager.SyncHint).
var ErrNotFound = errors.New("module not found")

type Source interface {
	// Key identifica a origem no cache de modulos da VM (raiz real + search
	// paths + selo): duas fontes com a mesma chave resolvem igual.
	Key() string
	Resolve(name string) (Module, error)
	ReadFile(path string) ([]byte, error)
	ReadDir(path string) ([]Entry, error)
}
