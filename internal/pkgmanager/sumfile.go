package pkgmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SumFile e a forma minima (M1) do noxy.sum: uma linha por artefato de
// extensao, "<pkg> <arquivo> sha256:<hex>". O formato completo (fontes,
// TOFU, assinaturas) e spec separada — spec §8 e §15.
type SumFile struct {
	Entries map[string]string
}

func sumKey(pkg, file string) string { return pkg + " " + file }

func ParseSumFile(path string) (*SumFile, error) {
	s := &SumFile{Entries: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || !strings.HasPrefix(fields[2], "sha256:") {
			return nil, fmt.Errorf("noxy.sum: malformed line %q", line)
		}
		s.Entries[sumKey(fields[0], fields[1])] = strings.TrimPrefix(fields[2], "sha256:")
	}
	return s, nil
}

func (s *SumFile) Set(pkg, file, hexDigest string) {
	s.Entries[sumKey(pkg, file)] = hexDigest
}

func (s *SumFile) Lookup(pkg, file string) (string, bool) {
	digest, ok := s.Entries[sumKey(pkg, file)]
	return digest, ok
}

func (s *SumFile) Save(path string) error {
	lines := make([]string, 0, len(s.Entries))
	for key, digest := range s.Entries {
		lines = append(lines, key+" sha256:"+digest)
	}
	sort.Strings(lines)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// SumFilePath e o UNICO resolvedor do caminho do noxy.sum: escrita
// (pkgmanager) e leitura (vm) passam por aqui — caminhos divergentes fariam
// a verificacao cair em silencio no ramo "sem entrada" (revisao do plano).
func SumFilePath(root string) string {
	return filepath.Join(root, "noxy.sum")
}
