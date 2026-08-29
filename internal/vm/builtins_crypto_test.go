package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// requireBuiltinError chama o native esperando erro tipado cuja mensagem
// contem want. Issue #121: os sentinelas null de validacao de argumento
// viraram erro — sob o wrapper `-> bytes` da stdlib um null passava em
// silencio e so quebrava longe da causa.
func requireBuiltinError(t *testing.T, machine *VM, want string, name string, args ...value.Value) {
	t.Helper()
	got, err := invokeBuiltin(t, machine, name, args...)
	if err == nil {
		t.Fatalf("%s with %d args = %s, want error containing %q", name, len(args), got.String(), want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %q, want it to contain %q", name, err.Error(), want)
	}
}

func requireBytesOfLength(t *testing.T, name string, got value.Value, want int) {
	t.Helper()
	if got.Type != value.VAL_BYTES {
		t.Fatalf("%s type = %v, want bytes", name, got.Type)
	}
	if length := len(got.Obj.(string)); length != want {
		t.Fatalf("%s length = %d, want %d", name, length, want)
	}
}

func TestCryptoRandomBytesShape(t *testing.T) {
	machine := New()
	requireBytesOfLength(t, "crypto_random_bytes", callBuiltin(t, machine, "crypto_random_bytes", value.NewInt(32)), 32)
}

func TestCryptoRandomBytesRejectsInvalidLength(t *testing.T) {
	machine := New()
	requireBuiltinError(t, machine, "crypto_random_bytes: expects exactly 1 argument, got 0", "crypto_random_bytes")
	requireBuiltinError(t, machine, "crypto_random_bytes: n must be > 0, got 0", "crypto_random_bytes", value.NewInt(0))
	requireBuiltinError(t, machine, "crypto_random_bytes: n must be > 0, got -1", "crypto_random_bytes", value.NewInt(-1))
	requireBuiltinError(t, machine, "crypto_random_bytes: n must be an int, got string", "crypto_random_bytes", value.NewString("16"))
}

func TestCryptoPBKDF2Shape(t *testing.T) {
	machine := New()
	derived := callBuiltin(t, machine, "crypto_pbkdf2_sha256",
		value.NewString("fixed-password"), value.NewBytes("fixed-salt"), value.NewInt(100), value.NewInt(24),
	)
	requireBytesOfLength(t, "crypto_pbkdf2_sha256", derived, 24)
}

func TestCryptoPBKDF2RejectsInvalidArguments(t *testing.T) {
	machine := New()
	password, salt := value.NewString("password"), value.NewBytes("salt")
	requireBuiltinError(t, machine, "crypto_pbkdf2_sha256: iterations must be > 0, got 0",
		"crypto_pbkdf2_sha256", password, salt, value.NewInt(0), value.NewInt(24))
	requireBuiltinError(t, machine, "crypto_pbkdf2_sha256: key length must be > 0, got -3",
		"crypto_pbkdf2_sha256", password, salt, value.NewInt(1), value.NewInt(-3))
	requireBuiltinError(t, machine, "crypto_pbkdf2_sha256: iterations must be an int, got string",
		"crypto_pbkdf2_sha256", password, salt, value.NewString("1000"), value.NewInt(24))
	requireBuiltinError(t, machine, "crypto_pbkdf2_sha256: expects exactly 4 arguments, got 3",
		"crypto_pbkdf2_sha256", password, salt, value.NewInt(1))
}

func aes256GCMFixture(t *testing.T, machine *VM) (key, plaintext, ciphertext value.Value) {
	t.Helper()
	key = value.NewBytes("0123456789abcdef0123456789abcdef")
	plaintext = value.NewBytes("stateful builtin round trip\x00")
	ciphertext = callBuiltin(t, machine, "crypto_aes256_gcm_encrypt", key, plaintext)
	if ciphertext.Type != value.VAL_BYTES {
		t.Fatalf("encrypt type = %v, want bytes", ciphertext.Type)
	}
	if got, minimum := len(ciphertext.Obj.(string)), 12+16; got < minimum {
		t.Fatalf("encrypted payload length = %d, want at least nonce+tag length %d", got, minimum)
	}
	return key, plaintext, ciphertext
}

func TestAES256GCMRoundTrip(t *testing.T) {
	machine := New()
	key, plaintext, ciphertext := aes256GCMFixture(t, machine)
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_decrypt", key, ciphertext), plaintext)
}

func TestAES256GCMRejectsWrongKeyLength(t *testing.T) {
	machine := New()
	_, plaintext, ciphertext := aes256GCMFixture(t, machine)
	shortKey := value.NewBytes("short")
	requireBuiltinError(t, machine, "crypto_aes256_gcm_encrypt: key must be 32 bytes, got 5",
		"crypto_aes256_gcm_encrypt", shortKey, plaintext)
	requireBuiltinError(t, machine, "crypto_aes256_gcm_decrypt: key must be 32 bytes, got 5",
		"crypto_aes256_gcm_decrypt", shortKey, ciphertext)
	requireBuiltinError(t, machine, "crypto_aes256_gcm_encrypt: expects exactly 2 arguments, got 1",
		"crypto_aes256_gcm_encrypt", shortKey)
	requireBuiltinError(t, machine, "crypto_aes256_gcm_decrypt: expects exactly 2 arguments, got 1",
		"crypto_aes256_gcm_decrypt", shortKey)
}

// null e resultado de DADO do decrypt (o wrapper diz `-> bytes?`): dados
// curtos demais para conter o nonce, nonce sem tag, tag adulterada — nada
// disso e erro de argumento, e a chave certa nao muda o veredito.
func TestAES256GCMDecryptReturnsNullForUndecryptableData(t *testing.T) {
	machine := New()
	key, _, ciphertext := aes256GCMFixture(t, machine)
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_decrypt", key, value.NewBytes("short nonce")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_decrypt", key, value.NewBytes("123456789012")), value.NewNull())

	tampered := []byte(ciphertext.Obj.(string))
	tampered[len(tampered)-1] ^= 0xff
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_decrypt", key, value.NewBytes(string(tampered))), value.NewNull())
}
