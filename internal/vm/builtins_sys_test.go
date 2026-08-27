package vm

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// sysCatCommand returns a Noxy exec_output command string that dumps the
// raw bytes of path to stdout unmodified — no shell text-encoding layer
// (e.g. echo's console code page) sits between the file's bytes and the
// captured output, so this is used to deliver both valid and invalid UTF-8
// byte sequences to sys.exec_output with byte-for-byte fidelity.
func sysCatCommand(path string) string {
	if runtime.GOOS == "windows" {
		return "type " + path
	}
	return "cat " + path
}

func TestSafeSystemBuiltinsPreserveProcessStateContracts(t *testing.T) {
	machine := New()
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_os"), value.NewString(runtime.GOOS))

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_getcwd"), value.NewString(workingDirectory))

	environmentKey := "NOXY_VM_STATEFUL_BUILTIN_TEST"
	t.Setenv(environmentKey, "before")
	// EnvResult carries an error field alongside value/ok: a lookup that finds
	// a non-UTF-8 environment value reports it there. On the ordinary success
	// path the field must be present and empty.
	environmentDefinition := value.NewStruct("EnvResult", []string{"value", "ok", "error"})
	before := requireBuiltinInstance(t, callBuiltin(t, machine, "sys_getenv", value.NewString(environmentKey), environmentDefinition), environmentDefinition)
	assertBuiltinValue(t, before.Field("value"), value.NewString("before"))
	assertBuiltinValue(t, before.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, before.Field("error"), value.NewString(""))
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_setenv", value.NewString(environmentKey), value.NewString("after")), value.NewBool(true))
	after := requireBuiltinInstance(t, callBuiltin(t, machine, "sys_getenv", value.NewString(environmentKey), environmentDefinition), environmentDefinition)
	assertBuiltinValue(t, after.Field("value"), value.NewString("after"))
	assertBuiltinValue(t, after.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, after.Field("error"), value.NewString(""))

	arguments := requireBuiltinArray(t, callBuiltin(t, machine, "sys_argv"))
	if len(arguments.Elements) != len(os.Args) {
		t.Fatalf("sys_argv length = %d, want %d", len(arguments.Elements), len(os.Args))
	}
	for index, argument := range os.Args {
		assertBuiltinValue(t, arguments.Elements[index], value.NewString(argument))
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_sleep", value.NewInt(0)), value.NewNull())
}

func TestSystemExecutionAndPluginErrorsDoNotInvokeExternalPrograms(t *testing.T) {
	machine := New()

	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec"), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec", value.NewString("")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec", value.NewString(""), value.NewNull()), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec_output"), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec_output", value.NewString("")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec_output", value.NewString(""), value.NewNull()), value.NewNull())

	nonexistentPlugin := filepath.Join(t.TempDir(), "missing-plugin-binary")
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_load_plugin"), value.NewBool(false))
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_load_plugin", value.NewString("missing"), value.NewString(nonexistentPlugin)), value.NewBool(false))
}

func TestSysExecOutputRejectsInvalidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dirty.bin")
	if err := os.WriteFile(path, []byte("hel\xffo world"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "use sys\n" +
		"let r: sys.SysResult = sys.exec_output(" + strconv.Quote(sysCatCommand(path)) + ")\n" +
		"test_report(to_str(!r.ok) + \"|\" + r.error)"
	captured := captureVMSource(t, source)
	report, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	if !strings.HasPrefix(report, "true|") {
		t.Fatalf("sys.exec_output on invalid UTF-8 reported %q, want ok=false", report)
	}
	if !strings.Contains(report, "UTF-8") {
		t.Fatalf("error = %q, want it to mention UTF-8", report)
	}
}

func TestSysExecOutputAcceptsValidAccentedOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean.txt")
	content := "acentuação e emoji \U0001F600"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "use sys\n" +
		"let r: sys.SysResult = sys.exec_output(" + strconv.Quote(sysCatCommand(path)) + ")\n" +
		"test_report(to_str(r.ok) + \"|\" + r.output + \"|\" + r.error)"
	captured := captureVMSource(t, source)
	report, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	want := "true|" + content + "|"
	if report != want {
		t.Fatalf("sys.exec_output on valid accented output reported %q, want %q", report, want)
	}
}

// TestSysExecOutputRejectsInvalidUTF8OnNonZeroExit proves the UTF-8 check
// runs regardless of exit code. Before the fix, requireValidUTF8 was only
// called on the ok-exit branch, so a command that exits non-zero (an
// entirely ordinary case: a crashing script, a tool that writes partial or
// binary output before failing) bypassed validation entirely and its raw,
// unfiltered invalid bytes flowed straight into r.output with ok=false and
// an EMPTY error — indistinguishable from any other ordinary failure.
func TestSysExecOutputRejectsInvalidUTF8OnNonZeroExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dirty-nonzero.bin")
	if err := os.WriteFile(path, []byte("hel\xffo world"), 0o600); err != nil {
		t.Fatal(err)
	}
	var failingCommand string
	if runtime.GOOS == "windows" {
		failingCommand = sysCatCommand(path) + " & exit 1"
	} else {
		failingCommand = sysCatCommand(path) + "; exit 1"
	}
	source := "use sys\n" +
		"let r: sys.SysResult = sys.exec_output(" + strconv.Quote(failingCommand) + ")\n" +
		"test_report(to_str(!r.ok) + \"|\" + r.error + \"|\" + to_str(r.output == \"\"))"
	captured := captureVMSource(t, source)
	report, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	if !strings.HasPrefix(report, "true|") {
		t.Fatalf("sys.exec_output on non-zero exit with invalid UTF-8 reported %q, want ok=false", report)
	}
	if !strings.Contains(report, "UTF-8") {
		t.Fatalf("error = %q, want it to mention UTF-8", report)
	}
	if !strings.HasSuffix(report, "|true") {
		t.Fatalf("sys.exec_output on non-zero exit with invalid UTF-8 reported %q, want the raw invalid bytes cleared from output (not silently wrapped)", report)
	}
}

// TestSysExecOutputNonZeroExitWithValidOutputReportsEmptyError proves the
// fix above does not regress the ordinary non-zero-exit case: a command
// that exits non-zero but produces valid (non-garbage) output must still
// report ok=false with an EMPTY error, and the output must still be
// preserved rather than blanked out.
func TestSysExecOutputNonZeroExitWithValidOutputReportsEmptyError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean-nonzero.txt")
	content := "acentuação e emoji \U0001F600"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var failingCommand string
	if runtime.GOOS == "windows" {
		failingCommand = sysCatCommand(path) + " & exit 1"
	} else {
		failingCommand = sysCatCommand(path) + "; exit 1"
	}
	source := "use sys\n" +
		"let r: sys.SysResult = sys.exec_output(" + strconv.Quote(failingCommand) + ")\n" +
		"test_report(to_str(r.ok) + \"|\" + r.output + \"|\" + r.error)"
	captured := captureVMSource(t, source)
	report, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	want := "false|" + content + "|"
	if report != want {
		t.Fatalf("sys.exec_output on non-zero exit with valid output reported %q, want %q", report, want)
	}
}

func TestSysExecOutputPreservesFailureToStartShape(t *testing.T) {
	// A command that cannot be resolved inside the shell is the portable
	// stand-in here for "the process did not complete": simulating the
	// shell binary itself being unavailable is not reliable across
	// platforms, but this exercises the same pre-existing err != nil path
	// (see the comment on sys_exec_output) and must leave error empty.
	// Stderr is redirected to the null device: the shell's own "command
	// not found" diagnostic text is locale-dependent (confirmed on a
	// non-English Windows install, cmd.exe emits it in the console's OEM
	// code page rather than UTF-8, which the new unconditional validation
	// correctly flags — that is the separate, expected behavior covered by
	// the invalid-UTF-8 tests above, not what this test exercises). This
	// test wants the "process produced no meaningful output" shape with
	// guaranteed-valid (here, empty) output on every locale.
	unresolvedCommand := "noxy_nonexistent_command_xyz_12345 2>/dev/null"
	if runtime.GOOS == "windows" {
		unresolvedCommand = "noxy_nonexistent_command_xyz_12345 2>nul"
	}
	source := "use sys\n" +
		"let r: sys.SysResult = sys.exec_output(" + strconv.Quote(unresolvedCommand) + ")\n" +
		"test_report(to_str(r.ok) + \"|\" + r.error)"
	captured := captureVMSource(t, source)
	report, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	if report != "false|" {
		t.Fatalf("sys.exec_output on an unrunnable command reported %q, want %q (pre-existing ok=false meaning unchanged, error empty)", report, "false|")
	}
}

func TestSysGetenvRejectsInvalidUTF8(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Confirmed empirically: Windows environment variables are stored
		// as UTF-16 internally, and os.Setenv converts a Go string to
		// UTF-16 via a rune-by-rune ([]rune) conversion that silently
		// replaces every invalid UTF-8 byte with U+FFFD before the value
		// ever reaches the OS. os.LookupEnv therefore can never observe
		// genuinely invalid UTF-8 on this platform — there is no way to
		// reach sys_getenv's new failure branch here.
		t.Skip("Windows os.Setenv sanitizes invalid UTF-8 to U+FFFD before storage; sys_getenv can never observe invalid UTF-8 on this platform")
	}
	key := "NOXY_SYS_GETENV_INVALID_UTF8_TEST"
	t.Setenv(key, "hel\xffo")
	source := "use sys\n" +
		"let r: sys.EnvResult = sys.getenv(" + strconv.Quote(key) + ")\n" +
		"test_report(to_str(!r.ok) + \"|\" + r.error)"
	captured := captureVMSource(t, source)
	report, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	if !strings.HasPrefix(report, "true|") {
		t.Fatalf("sys.getenv on invalid UTF-8 reported %q, want ok=false", report)
	}
	if !strings.Contains(report, "UTF-8") {
		t.Fatalf("error = %q, want it to mention UTF-8", report)
	}
}

func TestSysGetenvAcceptsValidAccentedValue(t *testing.T) {
	key := "NOXY_SYS_GETENV_ACCENTED_TEST"
	envValue := "acentuação e emoji \U0001F600"
	t.Setenv(key, envValue)
	source := "use sys\n" +
		"let r: sys.EnvResult = sys.getenv(" + strconv.Quote(key) + ")\n" +
		"test_report(to_str(r.ok) + \"|\" + r.value + \"|\" + r.error)"
	captured := captureVMSource(t, source)
	report, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	want := "true|" + envValue + "|"
	if report != want {
		t.Fatalf("sys.getenv on a valid accented value reported %q, want %q", report, want)
	}
}

func TestSysGetenvPreservesUnsetMeaning(t *testing.T) {
	key := "NOXY_SYS_GETENV_DEFINITELY_UNSET_TEST_VAR"
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	source := "use sys\n" +
		"let r: sys.EnvResult = sys.getenv(" + strconv.Quote(key) + ")\n" +
		"test_report(to_str(r.ok) + \"|\" + r.error)"
	captured := captureVMSource(t, source)
	report, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	if report != "false|" {
		t.Fatalf("sys.getenv on an unset variable reported %q, want %q (pre-existing ok=false meaning unchanged, error empty)", report, "false|")
	}
}
