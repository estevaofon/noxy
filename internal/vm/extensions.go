package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"noxy-vm/internal/ext"
	"noxy-vm/internal/pkgmanager"
	"noxy-vm/internal/value"
	"noxy-vm/internal/version"
)

// extensionLoaderPermits permite aos testes liberar modulos de import extras
// (o guest de teste em Go precisa de wasi_snapshot_preview1). Em producao e
// nil ate as capabilities chegarem (M2, spec §9).
var extensionLoaderPermits []string

// ensureExtensionLoaded carrega a extensao declarada em dir/noxy_ext.toml
// uma unica vez por SharedState e registra cada export como native global
// assinada. Nao ha caminho de descarga: modulos vivem ate o processo.
func (vm *VM) ensureExtensionLoaded(dir string) error {
	shared := vm.shared
	shared.ExtMu.Lock()
	defer shared.ExtMu.Unlock()
	if shared.Ext == nil {
		shared.Ext = make(map[string]ext.Backend)
		shared.ExtNames = make(map[string]string)
	}
	if _, loaded := shared.Ext[dir]; loaded {
		return nil
	}

	manifestData, err := os.ReadFile(filepath.Join(dir, "noxy_ext.toml"))
	if err != nil {
		return fmt.Errorf("extension manifest: %w", err)
	}
	manifest, err := ext.ParseManifest(manifestData)
	if err != nil {
		return err
	}
	if err := manifest.CheckMinNoxy(version.Version); err != nil {
		return err
	}
	if other, exists := shared.ExtNames[manifest.Name]; exists && other != dir {
		return fmt.Errorf("extension name %q already loaded from %s", manifest.Name, other)
	}
	// Pre-checagem de TODOS os exports antes de registrar qualquer um:
	// DefineContextualNativeWithSignature usa DefineLocalIfAbsent (o primeiro
	// a escrever vence), entao um export cujo nome ja esteja ligado no Root
	// (native de stdlib ou export de outra extensao) perderia silenciosamente
	// sem essa checagem — falha atomica em vez de sombra silenciosa.
	for _, exp := range manifest.Exports {
		if _, exists := vm.GetGlobal(exp.Name); exists {
			return fmt.Errorf("extension %q: export %q collides with an existing global", manifest.Name, exp.Name)
		}
	}
	var backend ext.Backend
	switch manifest.Kind {
	case ext.KindProcess:
		backend, err = vm.loadProcessBackend(dir, manifest, manifestData)
	default:
		backend, err = vm.loadWasmBackend(dir, manifest, manifestData)
	}
	if err != nil {
		return err
	}
	for i, exp := range manifest.Exports {
		index := i
		sig := value.NativeSignature{
			Arity:      len(exp.Params),
			Params:     make([]value.ParamInfo, len(exp.Params)),
			ReturnType: signatureTypeName(exp.Returns),
		}
		for j, p := range exp.Params {
			sig.Params[j] = value.ParamInfo{TypeName: signatureTypeName(p)}
		}
		vm.DefineContextualNativeWithSignature(exp.Name, sig,
			func(_ value.NativeContext, args []value.Value) (value.Value, error) {
				return backend.Call(context.Background(), index, args)
			})
	}
	shared.Ext[dir] = backend
	shared.ExtNames[manifest.Name] = dir
	return nil
}

// loadWasmBackend e o caminho do M1: le o .wasm, verifica o hash, carrega
// no wazero.
func (vm *VM) loadWasmBackend(dir string, manifest *ext.Manifest, manifestData []byte) (ext.Backend, error) {
	wasmBytes, err := os.ReadFile(filepath.Join(dir, manifest.Wasm))
	if err != nil {
		return nil, fmt.Errorf("extension %q: %w", manifest.Name, err)
	}
	if err := vm.verifyExtensionSum(dir, manifest, manifestData, manifest.Wasm, wasmBytes); err != nil {
		return nil, err
	}
	// Sem campo diagOut no VM ainda: nx_log vai para stderr explicitamente
	// (achado de revisao) ate a spec de diagOut chegar.
	return ext.LoadModule(context.Background(), wasmBytes, manifest,
		ext.LoaderConfig{PermittedImports: extensionLoaderPermits, Log: os.Stderr})
}

// loadProcessBackend resolve o binario da plataforma em bin/, verifica o
// hash e constroi o backend SEM subir o processo (spec §4.1) — o start e da
// primeira chamada.
func (vm *VM) loadProcessBackend(dir string, manifest *ext.Manifest, manifestData []byte) (ext.Backend, error) {
	asset, ok := manifest.BinaryFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return nil, fmt.Errorf("extension %q has no binary for %s/%s (published: %s)",
			manifest.Name, runtime.GOOS, runtime.GOARCH, strings.Join(manifest.PublishedPlatforms(), ", "))
	}
	binPath := filepath.Join(dir, "bin", asset)
	binBytes, err := os.ReadFile(binPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("extension %q: binary bin/%s not found — run 'noxy --sync' to download it", manifest.Name, asset)
		}
		return nil, fmt.Errorf("extension %q: %w", manifest.Name, err)
	}
	if err := vm.verifyExtensionSum(dir, manifest, manifestData, "bin/"+asset, binBytes); err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(binPath)
	if err != nil {
		return nil, err
	}
	return ext.NewProcess(manifest, ext.ProcessConfig{Path: absPath, NoxyVersion: version.Version, Log: os.Stderr}), nil
}

// signatureTypeName mapeia o vocabulario do manifesto para os TypeName que
// call_validation.go entende. M1: escalares passam direto; compostos e
// structs viram "any" (a checagem concreta acontece na fronteira NXB —
// checkDeclaredReturn — e no wrapper .nx tipado).
func signatureTypeName(declared string) string {
	switch declared {
	case "int", "float", "bool", "string", "bytes", "any", "void":
		return declared
	default:
		return "any"
	}
}

// verifyExtensionSum confere o hash do artefato que o backend vai executar
// (`.wasm` ou `bin/<asset>`) contra o noxy.sum do projeto (M1
// trust-on-first-use: spec §15, noxy.sum spec pendente). So se aplica a
// pacotes sob <RootPath>/noxy_libs — layouts de desenvolvimento fora dali
// nao tem entrada e a checagem e ignorada.
//
// O manifesto decide QUAL artefato verificar (manifest.Wasm ou o asset de
// [binaries]), entao ele proprio precisa ser verificado primeiro — senao
// renomear o artefato no manifesto (ex.: trocar "wasm = ..." para apontar
// para um binario nao registrado) contorna a checagem, pois a busca pelo
// novo nome no noxy.sum simplesmente nao encontra entrada e cai no ramo de
// TOFU-allow (achado de revisao).
func (vm *VM) verifyExtensionSum(dir string, manifest *ext.Manifest, manifestData []byte, artifactName string, artifact []byte) error {
	rootAbs := vm.Config.ProjectRoot
	if rootAbs == "" {
		var err error
		if rootAbs, err = filepath.Abs(vm.Config.RootPath); err != nil {
			return err
		}
	}
	libs := filepath.Join(rootAbs, "noxy_libs")
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(libs, dirAbs)
	// "rel == ".."" cobre o proprio noxy_libs; HasPrefix com o separador
	// junto evita o falso positivo de um irmao como "noxy_libs-outro" que
	// comeca com ".." apos Rel mas nao esta de fato fora da arvore (achado
	// de revisao).
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil // fora de noxy_libs (layout de desenvolvimento): sem verificacao
	}
	sums, err := pkgmanager.ParseSumFile(pkgmanager.SumFilePath(rootAbs))
	if err != nil {
		return err
	}
	pkg := pkgmanager.ModulePath(filepath.ToSlash(rel))
	wantManifest, hasManifest := sums.Lookup(pkg, "noxy_ext.toml")
	wantArtifact, hasArtifact := sums.Lookup(pkg, artifactName)
	if !hasManifest && !hasArtifact {
		// TOFU do M1 (spec §15, noxy.sum spec pendente): sem NENHUMA entrada,
		// o load segue, mas nao em silencio — quem roda o script deve saber
		// que esta extensao nunca foi registrada por "noxy --sync" (achado de
		// revisao).
		fmt.Fprintf(os.Stderr, "warning: extension '%s' loaded from noxy_libs without a noxy.sum entry — run 'noxy --sync' to record it\n", manifest.Name)
	}
	if hasManifest {
		manifestSum := sha256.Sum256(manifestData)
		gotManifest := hex.EncodeToString(manifestSum[:])
		if gotManifest != wantManifest {
			return fmt.Errorf("extension artifact mismatch for %s/noxy_ext.toml: noxy.sum has sha256:%s, disk has sha256:%s",
				pkg, wantManifest, gotManifest)
		}
	}
	if !hasArtifact {
		return nil // sem entrada: TOFU do M1 (spec §15, noxy.sum spec pendente)
	}
	sum := sha256.Sum256(artifact)
	got := hex.EncodeToString(sum[:])
	if got != wantArtifact {
		return fmt.Errorf("extension artifact mismatch for %s/%s: noxy.sum has sha256:%s, disk has sha256:%s",
			pkg, artifactName, wantArtifact, got)
	}
	return nil
}

// CloseExtensions encerra todo backend carregado (spec §4.5): plugins por
// processo recebem EOF e sao mortos apos a carencia; modulos wasm fecham o
// runtime. Idempotente; chamado em todo caminho de saida do hospedeiro.
func (s *SharedState) CloseExtensions() {
	s.ExtMu.Lock()
	defer s.ExtMu.Unlock()
	for _, backend := range s.Ext {
		_ = backend.Close(context.Background())
	}
}

func (vm *VM) CloseExtensions() { vm.shared.CloseExtensions() }
