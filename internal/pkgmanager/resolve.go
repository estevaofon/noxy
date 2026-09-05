package pkgmanager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/estevaofon/noxy/internal/version"
)

type closureInput struct {
	Root   *ModuleConfig
	Lock   *SumFile
	Stamp  map[string]string
	Libs   string
	Fetch  *fetcher
	Locked bool
	Out    io.Writer
}

// checkNoxyVersion recusa um binario mais antigo que a linha "noxy vX" (spec
// §3.1); who identifica o arquivo na mensagem ("noxy.mod" ou o modulo).
func checkNoxyVersion(cfg *ModuleConfig, who string) error {
	if cfg.NoxyVersion == "" {
		return nil
	}
	want, err := ParseVersion(cfg.NoxyVersion)
	if err != nil {
		return fmt.Errorf("%s: %w", who, err)
	}
	have, err := ParseVersion(version.Version)
	if err != nil {
		return err
	}
	if CompareVersions(have, want) < 0 {
		return fmt.Errorf("%s requires noxy %s; this is %s", who, want, have)
	}
	return nil
}

// installedMatches: diretorio em noxy_libs e o que o lock descreve nessa
// versao (carimbo + hash de arvore). E o teste de "cached" do sync e a
// condicao para ler o noxy.mod de uma dependencia do disco (spec §4.3).
func installedMatches(libs string, lock *SumFile, stamp map[string]string, module, ver string) bool {
	if stamp[module] != ver {
		return false
	}
	lockVer, digest, ok := lock.TreeHash(module)
	if !ok || lockVer != ver {
		return false
	}
	got, err := TreeHash(filepath.Join(libs, filepath.FromSlash(LocalPath(module))))
	return err == nil && got == digest
}

// readDepMod devolve o noxy.mod do modulo na versao dada: do disco quando
// instalado e integro, senao de um clone temporario. Sem noxy.mod = folha.
func readDepMod(in closureInput, module, ver string) (*ModuleConfig, error) {
	dir := filepath.Join(in.Libs, filepath.FromSlash(LocalPath(module)))
	if !installedMatches(in.Libs, in.Lock, in.Stamp, module, ver) {
		var err error
		dir, err = in.Fetch.dir(module, ver)
		if err != nil {
			return nil, err
		}
	}
	modPath := filepath.Join(dir, "noxy.mod")
	if _, err := os.Stat(modPath); err != nil {
		return NewModuleConfig(), nil
	}
	cfg, err := ParseModFile(modPath)
	if err != nil {
		return nil, fmt.Errorf("%s@%s: %w", module, ver, err)
	}
	if err := checkNoxyVersion(cfg, module+"@"+ver+" noxy.mod"); err != nil {
		return nil, err
	}
	return cfg, nil
}

// computeClosure e o MVS da spec §4.2: fechamento recalculado das diretas,
// maior versao exigida por modulo, lock como piso das indiretas.
func computeClosure(in closureInput) (map[string]string, error) {
	direct := map[string]bool{}
	for _, m := range in.Root.Requires() {
		direct[m] = true
		if in.Root.Require[m] == HeadVersion {
			if in.Locked {
				return nil, fmt.Errorf("noxy.mod pins %s to HEAD; run 'noxy --sync' to resolve it", m)
			}
			v, err := in.Fetch.resolve(m, HeadVersion)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(in.Out, "Resolved %s to %s\n", m, v)
			in.Root.Require[m] = v
		}
	}

	// required[m][v] = quem pediu (para a mensagem); rootWants[m] = pedido direto.
	required := map[string]map[string]string{}
	add := func(m, v, by string) {
		if required[m] == nil {
			required[m] = map[string]string{}
		}
		if _, dup := required[m][v]; !dup {
			required[m][v] = by
		}
	}
	for m, v := range in.Root.Require {
		add(m, v, "noxy.mod")
	}
	choose := func() (map[string]string, error) {
		chosen := map[string]string{}
		for m, versions := range required {
			if !direct[m] {
				if lv, _, ok := in.Lock.TreeHash(m); ok {
					add(m, lv, "noxy.sum")
				}
			}
			best := ""
			for v := range versions {
				if best == "" {
					best = v
					continue
				}
				bv, err := ParseVersion(best)
				if err != nil {
					return nil, err
				}
				vv, err := ParseVersion(v)
				if err != nil {
					return nil, err
				}
				if CompareVersions(vv, bv) > 0 {
					best = v
				}
			}
			chosen[m] = best
		}
		return chosen, nil
	}

	visited := map[string]bool{}
	for {
		chosen, err := choose()
		if err != nil {
			return nil, err
		}
		progressed := false
		for _, m := range sortedKeys(chosen) {
			v := chosen[m]
			if visited[fetchKey(m, v)] {
				continue
			}
			visited[fetchKey(m, v)] = true
			progressed = true
			cfg, err := readDepMod(in, m, v)
			if err != nil {
				return nil, err
			}
			for _, dep := range cfg.Requires() {
				if dep == m {
					continue // auto-require (quicksort publicado): ignorado, spec §3.1
				}
				dv := cfg.Require[dep]
				if dv == HeadVersion {
					if lv, _, ok := in.Lock.TreeHash(dep); ok {
						dv = lv
					} else if in.Locked {
						return nil, fmt.Errorf("%s requires %s at HEAD and noxy.sum has no version for it; run 'noxy --sync' without --locked", m, dep)
					} else {
						resolved, err := in.Fetch.resolve(dep, HeadVersion)
						if err != nil {
							return nil, err
						}
						dv = resolved
					}
				}
				add(dep, dv, m)
			}
		}
		if !progressed {
			for _, m := range sortedKeys(chosen) {
				v := chosen[m]
				if direct[m] && in.Root.Require[m] != v {
					fmt.Fprintf(in.Out, "%s: noxy.mod requires %s, but %s requires %s; using %s\n",
						m, in.Root.Require[m], required[m][v], v, v)
				}
			}
			return chosen, nil
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
