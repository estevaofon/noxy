package vm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"

	"noxy-vm/internal/value"
)

func (vm *VM) defineCryptoBuiltins() {
	// Crypto Module - Native implementations for cryptographic operations
	vm.DefineNative("crypto_random_bytes", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		n := int(args[0].Int())
		if n <= 0 {
			return value.NewNull()
		}

		bytes := make([]byte, n)
		if _, err := rand.Read(bytes); err != nil {
			return value.NewNull()
		}

		return value.NewBytes(string(bytes))
	})

	vm.DefineNative("crypto_pbkdf2_sha256", func(args []value.Value) value.Value {
		// args: (senha: string, salt: bytes, iteracoes: int, tamanho: int)
		if len(args) < 4 {
			return value.NewNull()
		}

		senha := args[0].String()
		var salt []byte
		if args[1].Type == value.VAL_BYTES {
			salt = []byte(args[1].Obj.(string))
		} else {
			salt = []byte(args[1].String())
		}
		iteracoes := int(args[2].Int())
		tamanho := int(args[3].Int())

		if iteracoes <= 0 || tamanho <= 0 {
			return value.NewNull()
		}

		chave, err := pbkdf2.Key(sha256.New, senha, salt, iteracoes, tamanho)
		if err != nil {
			return value.NewNull()
		}
		return value.NewBytes(string(chave))
	})

	vm.DefineNative("crypto_aes256_gcm_encrypt", func(args []value.Value) value.Value {
		// args: (chave: bytes, texto: bytes) -> bytes (nonce + ciphertext + tag)
		if len(args) < 2 {
			return value.NewNull()
		}

		var chave []byte
		if args[0].Type == value.VAL_BYTES {
			chave = []byte(args[0].Obj.(string))
		} else {
			chave = []byte(args[0].String())
		}

		var texto []byte
		if args[1].Type == value.VAL_BYTES {
			texto = []byte(args[1].Obj.(string))
		} else {
			texto = []byte(args[1].String())
		}

		// Validate key size (32 bytes for AES-256)
		if len(chave) != 32 {
			return value.NewNull()
		}

		block, err := aes.NewCipher(chave)
		if err != nil {
			return value.NewNull()
		}

		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return value.NewNull()
		}

		// Generate random nonce (12 bytes for GCM)
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return value.NewNull()
		}

		// Seal: result = nonce + ciphertext + tag
		resultado := gcm.Seal(nonce, nonce, texto, nil)
		return value.NewBytes(string(resultado))
	})

	vm.DefineNative("crypto_aes256_gcm_decrypt", func(args []value.Value) value.Value {
		// args: (chave: bytes, dados: bytes) -> bytes (plaintext)
		if len(args) < 2 {
			return value.NewNull()
		}

		var chave []byte
		if args[0].Type == value.VAL_BYTES {
			chave = []byte(args[0].Obj.(string))
		} else {
			chave = []byte(args[0].String())
		}

		var dados []byte
		if args[1].Type == value.VAL_BYTES {
			dados = []byte(args[1].Obj.(string))
		} else {
			dados = []byte(args[1].String())
		}

		// Validate key size (32 bytes for AES-256)
		if len(chave) != 32 {
			return value.NewNull()
		}

		block, err := aes.NewCipher(chave)
		if err != nil {
			return value.NewNull()
		}

		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return value.NewNull()
		}

		nonceSize := gcm.NonceSize()
		if len(dados) < nonceSize {
			return value.NewNull()
		}

		// Separate nonce from ciphertext
		nonce := dados[:nonceSize]
		ciphertext := dados[nonceSize:]

		// Open: decrypt and validate authentication
		texto, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return value.NewNull()
		}

		return value.NewBytes(string(texto))
	})
}
