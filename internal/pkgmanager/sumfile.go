package pkgmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SumEntry e uma linha do noxy.sum v2 (spec §3.2):
//   <modulo> <versao> sha256:<hex>            → File == ""  (hash de arvore)
//   <modulo> <versao> <arquivo> sha256:<hex>  → artefato de extensao
// Linhas v1 ("<github_com/x/y> <arquivo> sha256:<hex>") entram com Version
// "" e sao descartadas no proximo Save.
type SumEntry struct {
	Module, Version, File, Digest string
}

type SumFile struct {
	entries map[string]SumEntry // chave: modulo + "\x00" + arquivo
}

func sumKey(module, file string) string { return module + "\x00" + file }

func (s *SumFile) init() {
	if s.entries == nil {
		s.entries = map[string]SumEntry{}
	}
}

func ParseSumFile(path string) (*SumFile, error) {
	s := &SumFile{}
	s.init()
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
		f := strings.Fields(line)
		last := f[len(f)-1]
		if len(f) < 3 || len(f) > 4 || !strings.HasPrefix(last, "sha256:") {
			return nil, fmt.Errorf("noxy.sum: malformed line %q", line)
		}
		digest := strings.TrimPrefix(last, "sha256:")
		switch {
		case len(f) == 4: // v2 artefato
			ver, err := NormalizeVersion(f[1])
			if err != nil {
				return nil, fmt.Errorf("noxy.sum: malformed line %q: %w", line, err)
			}
			s.entries[sumKey(f[0], f[2])] = SumEntry{f[0], ver, f[2], digest}
		case IsSemverTag(f[1]) || IsPseudoVersion(f[1]): // v2 arvore
			ver, err := NormalizeVersion(f[1])
			if err != nil {
				return nil, fmt.Errorf("noxy.sum: malformed line %q: %w", line, err)
			}
			s.entries[sumKey(f[0], "")] = SumEntry{f[0], ver, "", digest}
		default: // v1: chave local, sem versao
			module := ModulePath(f[0])
			s.entries[sumKey(module, f[1])] = SumEntry{module, "", f[1], digest}
		}
	}
	return s, nil
}

func (s *SumFile) SetTree(module, version, digest string) {
	s.init()
	s.entries[sumKey(module, "")] = SumEntry{module, version, "", digest}
}

func (s *SumFile) SetArtifact(module, version, file, digest string) {
	s.init()
	s.entries[sumKey(module, file)] = SumEntry{module, version, file, digest}
}

func (s *SumFile) DropModule(module string) {
	for k, e := range s.entries {
		if e.Module == module {
			delete(s.entries, k)
		}
	}
}

// Lookup ignora a versao: a VM so conhece o diretorio em disco, e o
// invariante "uma versao por modulo" torna a busca univoca (spec §3.2).
func (s *SumFile) Lookup(module, file string) (string, bool) {
	e, ok := s.entries[sumKey(module, file)]
	return e.Digest, ok
}

func (s *SumFile) TreeHash(module string) (version, digest string, ok bool) {
	e, ok := s.entries[sumKey(module, "")]
	if !ok || e.Version == "" {
		return "", "", false
	}
	return e.Version, e.Digest, true
}

func (s *SumFile) Version(module string) (string, bool) {
	found := false
	version := ""
	for _, e := range s.entries {
		if e.Module == module {
			found = true
			if e.Version != "" {
				version = e.Version
			}
		}
	}
	return version, found
}

func (s *SumFile) Modules() []string {
	seen := map[string]bool{}
	for _, e := range s.entries {
		seen[e.Module] = true
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func (s *SumFile) Artifacts(module string) map[string]string {
	out := map[string]string{}
	for _, e := range s.entries {
		if e.Module == module && e.File != "" {
			out[e.File] = e.Digest
		}
	}
	return out
}

// render monta o corpo v2 ordenado (spec §3.2, migracao): linhas v1
// (Version == "") caem — quem as re-registra e o --sync.
func (s *SumFile) render() ([]byte, error) {
	versions := map[string]string{}
	lines := make([]string, 0, len(s.entries))
	for _, e := range s.entries {
		if e.Version == "" {
			continue
		}
		if prev, ok := versions[e.Module]; ok && prev != e.Version {
			return nil, fmt.Errorf("noxy.sum: two versions of %s (%s and %s)", e.Module, prev, e.Version)
		}
		versions[e.Module] = e.Version
		if e.File == "" {
			lines = append(lines, e.Module+" "+e.Version+" sha256:"+e.Digest)
		} else {
			lines = append(lines, e.Module+" "+e.Version+" "+e.File+" sha256:"+e.Digest)
		}
	}
	sort.Strings(lines)
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

// Save grava so linhas v2, ordenadas.
func (s *SumFile) Save(path string) error {
	data, err := s.render()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SumFilePath e o UNICO resolvedor do caminho do noxy.sum: escrita
// (pkgmanager) e leitura (vm) passam por aqui.
func SumFilePath(root string) string {
	return filepath.Join(root, "noxy.sum")
}

// LocalPath/ModulePath sao o UNICO par de conversores modulo ↔ diretorio em
// noxy_libs (spec §3.2): so o primeiro segmento (host) troca "." por "_";
// hostname nao tem "_", entao a volta e exata.
func LocalPath(module string) string {
	parts := strings.Split(module, "/")
	parts[0] = strings.ReplaceAll(parts[0], ".", "_")
	return strings.Join(parts, "/")
}

func ModulePath(local string) string {
	parts := strings.Split(filepath.ToSlash(local), "/")
	parts[0] = strings.ReplaceAll(parts[0], "_", ".")
	return strings.Join(parts, "/")
}
