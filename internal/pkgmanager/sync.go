package pkgmanager

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"noxy-vm/internal/ext"
)

type SyncOptions struct {
	Locked bool
	Out    io.Writer
}

const stampFile = ".noxy-sync"

// Sync e o comando unico da spec §5.1: le noxy.mod e noxy.sum na raiz,
// recalcula o fechamento (MVS), instala o que falta em noxy_libs, verifica
// hashes, poda o que ele mesmo instalou e regrava lock, carimbo e (se um
// HEAD foi pinado) o noxy.mod.
func Sync(root string, opts SyncOptions) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if _, err := os.Stat(filepath.Join(root, "noxy.mod")); err != nil {
		return fmt.Errorf("no noxy.mod in %s", root)
	}
	libs := filepath.Join(root, NoxyLibsDir)
	cleanStaleTemps(libs)
	f := newFetcher(libs, opts.Out)
	defer f.cleanup()
	return syncWith(root, opts, f)
}

// syncWith reutiliza um *fetcher ja existente — Get reutiliza o fetcher. O
// chamador e o dono de f: syncWith nunca chama f.cleanup(), quem chamou e
// que deve dar defer nisso; f.out deve ser o mesmo io.Writer de opts.Out,
// senao a saida dos dois se separa (relatos de clone caindo num, "cached"/
// "installed" caindo no outro).
func syncWith(root string, opts SyncOptions, f *fetcher) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	libs := filepath.Join(root, NoxyLibsDir)

	modPath := filepath.Join(root, "noxy.mod")
	cfg, err := ParseModFile(modPath)
	if err != nil {
		return err
	}
	if err := checkNoxyVersion(cfg, "noxy.mod"); err != nil {
		return err
	}
	lock, err := ParseSumFile(SumFilePath(root))
	if err != nil {
		return err
	}
	stamp := readStamp(libs)

	// Copia do require antes do fechamento: computeClosure pina HEADs no
	// mapa de cfg (spec §4.2); so regravamos noxy.mod se algo mudou ali.
	requireBefore := make(map[string]string, len(cfg.Require))
	for m, v := range cfg.Require {
		requireBefore[m] = v
	}

	closure, err := computeClosure(closureInput{Root: cfg, Lock: lock, Stamp: stamp, Libs: libs, Fetch: f, Locked: opts.Locked, Out: opts.Out})
	if err != nil {
		return err
	}
	if opts.Locked {
		if err := lockMatches(closure, lock); err != nil {
			return err
		}
	}
	fmt.Fprintf(opts.Out, "Resolved %d package%s\n", len(closure), plural(len(closure)))

	modules := sortedKeys(closure)
	width := 0
	for _, m := range modules {
		if n := len(m) + 1 + len(closure[m]); n > width {
			width = n
		}
	}
	for _, m := range modules {
		v := closure[m]
		label := fmt.Sprintf("%-*s", width, m+" "+v)
		if installedMatches(libs, lock, stamp, m, v) && platformAssetPresent(libs, lock, m) {
			fmt.Fprintf(opts.Out, "%s  cached\n", label)
			continue
		}
		detail, err := install(libs, lock, stamp, f, m, v, opts.Out)
		if err != nil {
			return err
		}
		fmt.Fprintf(opts.Out, "%s  installed%s\n", label, detail)
	}

	// Poda (spec §5.3): so o que o carimbo diz que o sync instalou.
	for _, m := range sortedKeys(stamp) {
		if _, keep := closure[m]; keep {
			continue
		}
		target := filepath.Join(libs, filepath.FromSlash(LocalPath(m)))
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		removeEmptyParents(target, libs)
		fmt.Fprintf(opts.Out, "Removed %s %s\n", m, stamp[m])
		delete(stamp, m)
	}
	for _, m := range lock.Modules() {
		if _, keep := closure[m]; !keep {
			lock.DropModule(m)
		}
	}
	if err := writeStamp(libs, stamp); err != nil {
		return err
	}
	if opts.Locked {
		// §5.2: pacote ausente do disco mas presente no lock e instalado
		// normalmente (o install loop acima ja rodou), mas so a ESCRITA do
		// lock e recusada — se o install trouxe linhas que o noxy.sum em
		// disco nao tinha (ex.: artefatos de extensao perdidos), --locked
		// nao pode "consertar" isso escrevendo; erro em vez de salvar.
		if _, changed, err := lockDiff(SumFilePath(root), lock); err != nil {
			return err
		} else if changed {
			return fmt.Errorf("noxy.sum is out of date with noxy.mod; run 'noxy --sync' without --locked")
		}
	} else if err := saveIfChanged(SumFilePath(root), lock); err != nil {
		return err
	}
	if requireChanged(requireBefore, cfg.Require) {
		if err := cfg.Save(modPath); err != nil {
			return err
		}
	}
	fmt.Fprintln(opts.Out, "Done.")
	return nil
}

func requireChanged(before, after map[string]string) bool {
	if len(before) != len(after) {
		return true
	}
	for m, v := range after {
		if before[m] != v {
			return true
		}
	}
	return false
}

// install traz (m, v) para noxy_libs: clone (ou o temporario do MVS), hash
// de arvore conferido com o lock, assets de extensao, promocao, carimbo e
// linhas do lock. Devolve o sufixo da linha de saida.
func install(libs string, lock *SumFile, stamp map[string]string, f *fetcher, m, v string, out io.Writer) (string, error) {
	src, err := f.dir(m, v)
	if err != nil {
		return "", err
	}
	digest, err := TreeHash(src)
	if err != nil {
		return "", fmt.Errorf("%s@%s: %w", m, v, err)
	}
	if lockVer, want, ok := lock.TreeHash(m); ok && lockVer == v && want != digest {
		return "", fmt.Errorf("%s %s: tree hash mismatch — noxy.sum has sha256:%s, download has sha256:%s", m, v, want, digest)
	}
	manifest, manifestData, err := readManifest(src)
	if err != nil {
		return "", fmt.Errorf("%s@%s: %w", m, v, err)
	}
	artifacts := map[string]string{}
	detail := ""
	if manifest != nil {
		artifacts["noxy_ext.toml"] = sha256Hex(manifestData)
		switch manifest.Kind {
		case ext.KindProcess:
			if IsPseudoVersion(v) {
				return "", fmt.Errorf("%s: process extensions are installed from a tagged release, not %s", m, v)
			}
			base, err := releaseBaseURL(m, v)
			if err != nil {
				return "", err
			}
			sums, err := fetchProcessBinaries(httpClient, base, manifest, src, runtime.GOOS, runtime.GOARCH, out)
			if err != nil {
				return "", fmt.Errorf("%s@%s: %w", m, v, err)
			}
			for asset, d := range sums {
				artifacts["bin/"+asset] = d
			}
			if asset, ok := manifest.BinaryFor(runtime.GOOS, runtime.GOARCH); ok {
				detail = " (bin/" + asset + ")"
			}
		default:
			wasm, err := os.ReadFile(filepath.Join(src, manifest.Wasm))
			if err != nil {
				return "", fmt.Errorf("%s@%s: %w", m, v, err)
			}
			artifacts[manifest.Wasm] = sha256Hex(wasm)
		}
		if lockVer, _ := lock.Version(m); lockVer == v {
			for file, d := range artifacts {
				if want, ok := lock.Lookup(m, file); ok && want != d {
					return "", fmt.Errorf("%s %s: artifact mismatch for %s — noxy.sum has sha256:%s, download has sha256:%s", m, v, file, want, d)
				}
			}
		}
		if len(manifest.Capabilities) != 0 {
			fmt.Fprintf(out, "%s declares: %s\n", manifest.Name, strings.Join(manifest.Capabilities, ", "))
		}
	}
	target := filepath.Join(libs, filepath.FromSlash(LocalPath(m)))
	if err := f.promote(m, v, target); err != nil {
		return "", err
	}
	stamp[m] = v
	if err := writeStamp(libs, stamp); err != nil { // logo apos a promocao (spec §3.4)
		return "", err
	}
	lock.DropModule(m)
	lock.SetTree(m, v, digest)
	for file, d := range artifacts {
		lock.SetArtifact(m, v, file, d)
	}
	return detail, nil
}

// platformAssetPresent: extensao por processo "cached" tambem precisa do
// binario desta plataforma no disco com o hash do lock — senao um bin/
// apagado ficaria "cached" para sempre e o runtime mandaria rodar --sync.
func platformAssetPresent(libs string, lock *SumFile, m string) bool {
	dir := filepath.Join(libs, filepath.FromSlash(LocalPath(m)))
	manifest, _, err := readManifest(dir)
	if err != nil {
		return false // manifesto ilegivel: reinstalar e a resposta segura
	}
	if manifest == nil || manifest.Kind != ext.KindProcess {
		return true
	}
	asset, ok := manifest.BinaryFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, "bin", asset))
	if err != nil {
		return false
	}
	want, ok := lock.Lookup(m, "bin/"+asset)
	return ok && want == sha256Hex(data)
}

// lockMatches e a recusa do --locked (spec §5.2).
func lockMatches(closure map[string]string, lock *SumFile) error {
	outOfDate := fmt.Errorf("noxy.sum is out of date with noxy.mod; run 'noxy --sync' without --locked")
	for m, v := range closure {
		lv, _, ok := lock.TreeHash(m)
		if !ok || lv != v {
			return outOfDate
		}
	}
	for _, m := range lock.Modules() {
		if _, ok := closure[m]; !ok {
			return outOfDate
		}
	}
	return nil
}

// stampWarn e o destino do aviso de carimbo corrompido (spec §3.4); costura
// de teste, producao e os.Stderr.
var stampWarn io.Writer = os.Stderr

// readStamp: carimbo ausente ou corrompido conta como vazio (nada e podado),
// com aviso em stampWarn no caso corrompido — uma linha que nao tem
// exatamente dois campos derruba o carimbo inteiro, nao so a linha (spec
// §3.4): um carimbo parcialmente ilegivel nao pode fingir que sabe o que foi
// instalado.
func readStamp(libs string) map[string]string {
	stamp := map[string]string{}
	data, err := os.ReadFile(filepath.Join(libs, stampFile))
	if err != nil {
		return stamp
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			fmt.Fprintf(stampWarn, "warning: %s/%s is corrupted; nothing will be pruned\n", NoxyLibsDir, stampFile)
			return map[string]string{}
		}
		stamp[f[0]] = f[1]
	}
	return stamp
}

// writeStamp so toca o disco quando o carimbo mudou (espelha saveIfChanged):
// um projeto sem dependencias nao ganha nem noxy_libs/ nem .noxy-sync so por
// ter rodado --sync.
func writeStamp(libs string, stamp map[string]string) error {
	fresh := renderStamp(stamp)
	path := filepath.Join(libs, stampFile)
	old, err := os.ReadFile(path)
	switch {
	case err == nil:
		if sameLines(old, fresh) {
			return nil
		}
	case os.IsNotExist(err):
		if len(fresh) == 0 {
			return nil
		}
	default:
		return err
	}
	if err := os.MkdirAll(libs, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, fresh, 0o644)
}

func renderStamp(stamp map[string]string) []byte {
	if len(stamp) == 0 {
		return nil
	}
	lines := make([]string, 0, len(stamp))
	for m, v := range stamp {
		lines = append(lines, m+" "+v)
	}
	sort.Strings(lines)
	return []byte(strings.Join(lines, "\n") + "\n")
}

func removeEmptyParents(path, stop string) {
	for dir := filepath.Dir(path); dir != stop && strings.HasPrefix(dir, stop); dir = filepath.Dir(dir) {
		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
	}
}

// lockDiff renderiza lock e diz se o resultado difere do que esta em path —
// mesma regra de "sem mudanca" do saveIfChanged (arquivo ausente + lock
// vazio nao conta como diferenca), reaproveitada pela recusa de escrita do
// --locked (spec §5.2).
func lockDiff(path string, lock *SumFile) (fresh []byte, changed bool, err error) {
	fresh, err = lock.render()
	if err != nil {
		return nil, false, err
	}
	old, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		return fresh, !sameLines(old, fresh), nil
	case os.IsNotExist(readErr):
		return fresh, len(lock.entries) != 0, nil
	default:
		return nil, false, readErr
	}
}

// saveIfChanged nao reescreve um lock cujo conteudo nao mudou (spec §5.1):
// compara render() com o arquivo em disco, sem arquivo temporario. Um
// projeto sem dependencias e sem noxy.sum previo nao ganha um noxy.sum vazio
// so por ter rodado --sync.
func saveIfChanged(path string, lock *SumFile) error {
	fresh, changed, err := lockDiff(path, lock)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return os.WriteFile(path, fresh, 0o644)
}

// sameLines compara conteudo ignorando CRLF: no Windows, core.autocrlf=true
// faz o checkout de noxy.sum com CRLF e uma comparacao byte a byte fazia
// --locked recusar um lock identico (falha vista no CI). O parser ja tolera
// CRLF linha a linha; a decisao "mudou?" precisa da mesma tolerancia.
func sameLines(a, b []byte) bool {
	crlf, lf := []byte("\r\n"), []byte("\n")
	return bytes.Equal(bytes.ReplaceAll(a, crlf, lf), bytes.ReplaceAll(b, crlf, lf))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
