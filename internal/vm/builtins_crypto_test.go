package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func TestCryptoRandomBytesAndPBKDF2Shapes(t *testing.T) {
	machine := New()

	random := callBuiltin(t, machine, "crypto_random_bytes", value.NewInt(32))
	if random.Type != value.VAL_BYTES {
		t.Fatalf("crypto_random_bytes type = %v, want bytes", random.Type)
	}
	if got := len(random.Obj.(string)); got != 32 {
		t.Fatalf("crypto_random_bytes length = %d, want 32", got)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_random_bytes"), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_random_bytes", value.NewInt(0)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_random_bytes", value.NewInt(-1)), value.NewNull())

	derived := callBuiltin(t, machine, "crypto_pbkdf2_sha256",
		value.NewString("fixed-password"), value.NewBytes("fixed-salt"), value.NewInt(100), value.NewInt(24),
	)
	if derived.Type != value.VAL_BYTES {
		t.Fatalf("crypto_pbkdf2_sha256 type = %v, want bytes", derived.Type)
	}
	if got := len(derived.Obj.(string)); got != 24 {
		t.Fatalf("crypto_pbkdf2_sha256 length = %d, want 24", got)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_pbkdf2_sha256",
		value.NewString("password"), value.NewBytes("salt"), value.NewInt(0), value.NewInt(24),
	), value.NewNull())
}

func TestAES256GCMRoundTripAndFailureSentinels(t *testing.T) {
	machine := New()
	key := value.NewBytes("0123456789abcdef0123456789abcdef")
	plaintext := value.NewBytes("stateful builtin round trip\x00")

	ciphertext := callBuiltin(t, machine, "crypto_aes256_gcm_encrypt", key, plaintext)
	if ciphertext.Type != value.VAL_BYTES {
		t.Fatalf("encrypt type = %v, want bytes", ciphertext.Type)
	}
	if got, minimum := len(ciphertext.Obj.(string)), 12+16; got < minimum {
		t.Fatalf("encrypted payload length = %d, want at least nonce+tag length %d", got, minimum)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_decrypt", key, ciphertext), plaintext)

	shortKey := value.NewBytes("short")
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_encrypt", shortKey, plaintext), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_decrypt", shortKey, ciphertext), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_decrypt", key, value.NewBytes("short nonce")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_decrypt", key, value.NewBytes("123456789012")), value.NewNull())

	tampered := []byte(ciphertext.Obj.(string))
	tampered[len(tampered)-1] ^= 0xff
	assertBuiltinValue(t, callBuiltin(t, machine, "crypto_aes256_gcm_decrypt", key, value.NewBytes(string(tampered))), value.NewNull())
}
