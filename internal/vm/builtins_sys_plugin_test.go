package vm

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// sys_load_plugin avisa UMA vez por processo que esta deprecado (spec
// §10.1); o comando inexistente faz a chamada devolver false como antes.
func TestSysLoadPluginWarnsDeprecationOnce(t *testing.T) {
	pluginDeprecationWarned.Store(false)
	machine := New()
	load := func() {
		callBuiltin(t, machine, "sys_load_plugin", value.NewString("nope"), value.NewString("noxy-plugin-does-not-exist"))
	}
	first := captureConcurrencyStderr(t, load)
	if !strings.Contains(first, "warning: sys_load_plugin is deprecated since v0.23.0 and will be removed in v0.25.0") {
		t.Fatalf("first call must warn, got %q", first)
	}
	second := captureConcurrencyStderr(t, load)
	if strings.Contains(second, "deprecated") {
		t.Fatalf("warn once per process, got %q", second)
	}
}
