package pkgmanager

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// fetcher guarda clones temporarios por (modulo, versao) durante um sync
// (spec §4.3): nenhum pacote e clonado duas vezes na mesma versao, e so o
// que for escolhido pelo MVS e promovido para noxy_libs.
type fetcher struct {
	libs   string
	out    io.Writer
	clones map[string]string // módulo@versão → diretório temporário (sem .git)
	refs   map[string]string // módulo@ref pedido → versão resolvida
}

func newFetcher(libs string, out io.Writer) *fetcher {
	return &fetcher{libs: libs, out: out, clones: map[string]string{}, refs: map[string]string{}}
}

func fetchKey(module, version string) string { return module + "@" + version }

// resolve traduz o ref pedido (HEAD, tag, sha, branch) na versao gravavel
// (spec §4.1). Tag nao clona; HEAD consulta as tags remotas e so clona se
// nao ha tag semver; sha/branch clonam para calcular a pseudo-versao.
func (f *fetcher) resolve(module, ref string) (string, error) {
	if ref == "" {
		ref = HeadVersion
	}
	if v, ok := f.refs[fetchKey(module, ref)]; ok {
		return v, nil
	}
	if IsSemverTag(ref) || IsPseudoVersion(ref) {
		v, err := NormalizeVersion(ref)
		if err != nil {
			return "", err
		}
		f.refs[fetchKey(module, ref)] = v
		return v, nil
	}
	if ref == HeadVersion {
		out, err := listTags(gitURLFor(module))
		if err != nil {
			return "", fmt.Errorf("%s: %w", module, err)
		}
		if tag, ok := newestSemverTag(out); ok {
			v, _ := NormalizeVersion(tag)
			f.refs[fetchKey(module, ref)] = v
			return v, nil
		}
	}
	// Sem tag (ou ref e sha/branch): clone, checkout e pseudo-versao.
	tmp, err := f.freshClone(module)
	if err != nil {
		return "", err
	}
	if ref != HeadVersion {
		if err := gitCheckout(tmp, ref); err != nil {
			os.RemoveAll(tmp)
			return "", fmt.Errorf("%s: failed to checkout %s: %w", module, ref, err)
		}
	}
	when, sha, base, err := commitInfo(tmp)
	if err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("%s: %w", module, err)
	}
	version := PseudoVersion(base, when, sha)
	if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
		fmt.Fprintf(f.out, "Warning: failed to remove .git directory: %s\n", err)
	}
	f.clones[fetchKey(module, version)] = tmp
	f.refs[fetchKey(module, ref)] = version
	return version, nil
}

// dir devolve um clone pronto (sem .git) do modulo na versao dada,
// clonando se ainda nao ha um.
func (f *fetcher) dir(module, version string) (string, error) {
	if d, ok := f.clones[fetchKey(module, version)]; ok {
		return d, nil
	}
	tmp, err := f.freshClone(module)
	if err != nil {
		return "", err
	}
	ref := version
	if sha := pseudoSHA(version); sha != "" {
		ref = sha
	}
	if err := gitCheckout(tmp, ref); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("%s@%s: failed to checkout: %w", module, version, err)
	}
	if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
		fmt.Fprintf(f.out, "Warning: failed to remove .git directory: %s\n", err)
	}
	f.clones[fetchKey(module, version)] = tmp
	return tmp, nil
}

// freshClone clona em noxy_libs/.get-<repo>-*: temporario irmao do destino,
// para o os.Rename final ser no mesmo sistema de arquivos (spec §4.3).
func (f *fetcher) freshClone(module string) (string, error) {
	if err := os.MkdirAll(f.libs, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(f.libs, ".get-"+filepath.Base(module)+"-")
	if err != nil {
		return "", err
	}
	if err := gitClone(gitURLFor(module), tmp); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("%s: failed to clone package: %w", module, err)
	}
	return tmp, nil
}

// promote instala o clone em target substituindo o que havia (clone fresco,
// spec §5.1 passo 3) e o tira do cache.
func (f *fetcher) promote(module, version, target string) error {
	key := fetchKey(module, version)
	src, ok := f.clones[key]
	if !ok {
		return fmt.Errorf("%s@%s: no clone to install", module, version)
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, target); err != nil {
		return fmt.Errorf("failed to install package: %w", err)
	}
	delete(f.clones, key)
	return nil
}

func (f *fetcher) cleanup() {
	for key, d := range f.clones {
		os.RemoveAll(d)
		delete(f.clones, key)
	}
}

// cleanStaleTemps remove .get-* deixados por um sync morto (spec §3.4).
func cleanStaleTemps(libs string) {
	entries, err := os.ReadDir(libs)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".get-") {
			os.RemoveAll(filepath.Join(libs, e.Name()))
		}
	}
}

var commitInfo = gitCommitInfo

// gitCommitInfo le, ANTES de remover .git: data e sha do commit corrente e a
// tag semver base (ancestral mais proxima com nome v<digitos>...; "" se nao ha).
func gitCommitInfo(dir string) (time.Time, string, string, error) {
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%ct %H").Output()
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("git log: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return time.Time{}, "", "", fmt.Errorf("git log: unexpected output %q", out)
	}
	secs, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("git log: %w", err)
	}
	base := ""
	if described, err := exec.Command("git", "-C", dir, "describe", "--tags", "--match", "v[0-9]*", "--abbrev=0").Output(); err == nil {
		if tag := strings.TrimSpace(string(described)); IsSemverTag(tag) {
			base = tag
		}
	}
	return time.Unix(secs, 0).UTC(), fields[1], base, nil
}
