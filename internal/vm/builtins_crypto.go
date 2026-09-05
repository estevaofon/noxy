package vm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/estevaofon/noxy/internal/value"
)

// Issue #121: nenhuma destas natives devolve null por argumento invalido.
// Sob o wrapper `-> bytes` de crypto.nx o null passava em silencio — a
// forma `m.f()` nao tem tipo estatico e o `select` confiava na assinatura —
// e so quebrava longe da causa. Argumento invalido (n <= 0, iteracoes ou
// tamanho <= 0, chave que nao tem 32 bytes) e erro tipado; o unico null que
// sobra e resultado de DADO: decrypt que nao autentica, e o wrapper diz
// `-> bytes?`.

func cryptoArity(native string, args []value.Value, want int) error {
	if len(args) == want {
		return nil
	}
	plural := "s"
	if want == 1 {
		plural = ""
	}
	return fmt.Errorf("%s: expects exactly %d argument%s, got %d", native, want, plural, len(args))
}

func cryptoIntArgument(native, label string, arg value.Value) (int64, error) {
	if arg.Type != value.VAL_INT {
		return 0, fmt.Errorf("%s: %s must be an int, got %s", native, label, runtimeTypeName(arg))
	}
	return arg.Int(), nil
}

func cryptoBytesArgument(arg value.Value) []byte {
	if arg.Type == value.VAL_BYTES {
		return []byte(arg.Obj.(string))
	}
	return []byte(arg.String())
}

func cryptoAES256GCM(native string, key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%s: key must be 32 bytes, got %d", native, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", native, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", native, err)
	}
	return gcm, nil
}

func (vm *VM) defineCryptoBuiltins() {
	vm.DefineContextualNative("crypto_random_bytes", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		const native = "crypto_random_bytes"
		if err := cryptoArity(native, args, 1); err != nil {
			return value.NewNull(), err
		}
		n, err := cryptoIntArgument(native, "n", args[0])
		if err != nil {
			return value.NewNull(), err
		}
		if n <= 0 {
			return value.NewNull(), fmt.Errorf("%s: n must be > 0, got %d", native, n)
		}
		buffer := make([]byte, n)
		if _, err := rand.Read(buffer); err != nil {
			return value.NewNull(), fmt.Errorf("%s: %w", native, err)
		}
		return value.NewBytes(string(buffer)), nil
	})

	vm.defineValueNativeErr("crypto_pbkdf2_sha256", func(args []value.Value) (value.Value, error) {
		// args: (senha: string, salt: bytes, iteracoes: int, tamanho: int)
		const native = "crypto_pbkdf2_sha256"
		if err := cryptoArity(native, args, 4); err != nil {
			return value.NewNull(), err
		}
		password := args[0].String()
		salt := cryptoBytesArgument(args[1])
		iterations, err := cryptoIntArgument(native, "iterations", args[2])
		if err != nil {
			return value.NewNull(), err
		}
		length, err := cryptoIntArgument(native, "key length", args[3])
		if err != nil {
			return value.NewNull(), err
		}
		if iterations <= 0 {
			return value.NewNull(), fmt.Errorf("%s: iterations must be > 0, got %d", native, iterations)
		}
		if length <= 0 {
			return value.NewNull(), fmt.Errorf("%s: key length must be > 0, got %d", native, length)
		}
		key, deriveErr := pbkdf2.Key(sha256.New, password, salt, int(iterations), int(length))
		if deriveErr != nil {
			return value.NewNull(), fmt.Errorf("%s: %w", native, deriveErr)
		}
		return value.NewBytes(string(key)), nil
	})

	vm.defineValueNativeErr("crypto_aes256_gcm_encrypt", func(args []value.Value) (value.Value, error) {
		// args: (chave: bytes, texto: bytes) -> bytes (nonce + ciphertext + tag)
		const native = "crypto_aes256_gcm_encrypt"
		if err := cryptoArity(native, args, 2); err != nil {
			return value.NewNull(), err
		}
		gcm, err := cryptoAES256GCM(native, cryptoBytesArgument(args[0]))
		if err != nil {
			return value.NewNull(), err
		}
		// Nonce aleatorio (12 bytes no GCM), unico por operacao
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return value.NewNull(), fmt.Errorf("%s: %w", native, err)
		}
		// Seal: resultado = nonce + ciphertext + tag
		sealed := gcm.Seal(nonce, nonce, cryptoBytesArgument(args[1]), nil)
		return value.NewBytes(string(sealed)), nil
	})

	vm.defineValueNativeErr("crypto_aes256_gcm_decrypt", func(args []value.Value) (value.Value, error) {
		// args: (chave: bytes, dados: bytes) -> bytes? (plaintext; null se nao autentica)
		const native = "crypto_aes256_gcm_decrypt"
		if err := cryptoArity(native, args, 2); err != nil {
			return value.NewNull(), err
		}
		gcm, err := cryptoAES256GCM(native, cryptoBytesArgument(args[0]))
		if err != nil {
			return value.NewNull(), err
		}
		data := cryptoBytesArgument(args[1])
		nonceSize := gcm.NonceSize()
		if len(data) < nonceSize {
			// Curto demais para conter o nonce: nao e um ciphertext — resultado
			// de dado, como a tag que nao confere.
			return value.NewNull(), nil
		}
		plaintext, openErr := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
		if openErr != nil {
			return value.NewNull(), nil
		}
		return value.NewBytes(string(plaintext)), nil
	})
}
