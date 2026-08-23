package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"noxy-vm/internal/ext"
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
		shared.Ext = make(map[string]*ext.Module)
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
	wasmPath := filepath.Join(dir, manifest.Wasm)
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("extension %q: %w", manifest.Name, err)
	}
	if err := vm.verifyExtensionSum(dir, manifest, wasmBytes); err != nil {
		return err
	}

	module, err := ext.LoadModule(context.Background(), wasmBytes, manifest,
		ext.LoaderConfig{PermittedImports: extensionLoaderPermits})
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
				return module.Call(context.Background(), index, args)
			})
	}
	shared.Ext[dir] = module
	shared.ExtNames[manifest.Name] = dir
	return nil
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

// verifyExtensionSum e preenchido na tarefa de noxy.sum; ate la, aceita.
func (vm *VM) verifyExtensionSum(dir string, manifest *ext.Manifest, wasmBytes []byte) error {
	return nil
}
