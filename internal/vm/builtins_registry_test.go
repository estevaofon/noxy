package vm

import (
	"reflect"
	"sort"
	"testing"

	"noxy-vm/internal/value"
)

func TestBuiltinRegistrySnapshot(t *testing.T) {
	machine := New()
	expected := []string{
		"append", "base62_decode", "base62_encode", "base64_decode",
		"base64_encode", "chan_close", "chan_is_closed", "chan_recv",
		"chan_send", "contains", "crypto_aes256_gcm_decrypt",
		"crypto_aes256_gcm_encrypt", "crypto_pbkdf2_sha256",
		"crypto_random_bytes", "delete", "fmt", "has_key", "hex",
		"hex_decode", "hex_encode", "input", "io_close", "io_exists",
		"io_mkdir", "io_open", "io_read", "io_read_bytes", "io_read_lines",
		"io_remove", "io_stat", "io_write", "iprint", "json_dumps",
		"json_loads", "json_parse", "keys", "length", "make_chan",
		"make_wg", "net_accept", "net_close", "net_connect", "net_listen",
		"net_recv", "net_select", "net_send", "net_setblocking", "ord",
		"pop", "print", "slice", "spawn", "sqlite_bind_float",
		"sqlite_bind_int", "sqlite_bind_text", "sqlite_close", "sqlite_exec",
		"sqlite_exec_params", "sqlite_finalize", "sqlite_open",
		"sqlite_prepare", "sqlite_query", "sqlite_reset", "sqlite_step_exec",
		"strings_char_at", "strings_contains", "strings_count",
		"strings_ends_with", "strings_from_char_code", "strings_index_of",
		"strings_is_alnum", "strings_is_alpha", "strings_is_digit",
		"strings_is_empty", "strings_is_space", "strings_join_count",
		"strings_pad_left", "strings_repeat", "strings_replace",
		"strings_replace_first", "strings_reverse", "strings_split",
		"strings_starts_with", "strings_substring", "strings_to_lower",
		"strings_to_upper", "strings_trim", "sys_argv", "sys_exec",
		"sys_exec_output", "sys_exit", "sys_getcwd", "sys_getenv",
		"sys_load_plugin", "sys_os", "sys_setenv", "sys_sleep",
		"time_add_days", "time_add_seconds", "time_after", "time_before",
		"time_days_in_month", "time_diff", "time_diff_duration", "time_format",
		"time_format_custom", "time_format_date", "time_format_time",
		"time_from_timestamp", "time_is_leap_year", "time_make_datetime",
		"time_month_name", "time_now", "time_now_datetime", "time_now_ms",
		"time_parse", "time_parse_date", "time_sleep", "time_to_timestamp",
		"time_weekday_name", "to_bytes", "to_float", "to_int", "to_str",
		"wg_add", "wg_done", "wg_wait",
	}

	machine.shared.GlobalsLock.RLock()
	actual := make([]string, 0, len(machine.shared.Globals))
	for name, global := range machine.shared.Globals {
		if global.Type == value.VAL_NATIVE {
			actual = append(actual, name)
		}
	}
	machine.shared.GlobalsLock.RUnlock()
	sort.Strings(actual)

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("native registry mismatch\nactual:   %#v\nexpected: %#v", actual, expected)
	}
}

func TestBuiltinNativeSignatures(t *testing.T) {
	machine := New()
	tests := []struct {
		name       string
		arity      int
		params     []value.ParamInfo
		returnType string
	}{
		{
			name: "delete", arity: 2,
			params:     []value.ParamInfo{{IsRef: true, TypeName: "ref map"}, {TypeName: "any"}},
			returnType: "void",
		},
		{
			name: "append", arity: 2,
			params:     []value.ParamInfo{{IsRef: true, TypeName: "ref array"}},
			returnType: "void",
		},
		{
			name: "pop", arity: 1,
			params:     []value.ParamInfo{{IsRef: true, TypeName: "ref array"}},
			returnType: "any",
		},
		{
			name: "json_loads", arity: 2,
			params:     []value.ParamInfo{{TypeName: "string"}, {IsRef: true, TypeName: "ref any"}},
			returnType: "bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			native := requireBuiltin(t, machine, tt.name)
			if native.Signature == nil {
				t.Fatalf("builtin %q has no native signature", tt.name)
			}
			if native.Signature.Arity != tt.arity {
				t.Errorf("arity = %d, want %d", native.Signature.Arity, tt.arity)
			}
			if native.Signature.Variadic {
				t.Error("signature is variadic, want fixed arity")
			}
			if !reflect.DeepEqual(native.Signature.Params, tt.params) {
				t.Errorf("params = %#v, want %#v", native.Signature.Params, tt.params)
			}
			if native.Signature.ReturnType != tt.returnType {
				t.Errorf("return type = %q, want %q", native.Signature.ReturnType, tt.returnType)
			}
		})
	}
}
