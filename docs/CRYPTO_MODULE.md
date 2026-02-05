# Noxy Crypto Module

O módulo `crypto` fornece funções criptográficas seguras para aplicações Noxy.

## Funções Disponíveis

### SHA-256 Hash
```noxy
crypto.sha256(data: bytes) -> string
```
Calcula o hash SHA-256 dos dados. Retorna string hexadecimal de 64 caracteres.

```noxy
use crypto
let hash: string = crypto.sha256(b"hello")
// "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
```

---

### Random Bytes
```noxy
crypto.random_bytes(n: int) -> bytes
```
Gera `n` bytes criptograficamente seguros usando o CSPRNG do sistema operacional.

```noxy
let salt: bytes = crypto.random_bytes(16)  // 16 bytes aleatórios
let key: bytes = crypto.random_bytes(32)   // 32 bytes para chave AES-256
```

---

### PBKDF2-SHA256 (Derivação de Chave)
```noxy
crypto.pbkdf2_sha256(senha: string, salt: bytes, iteracoes: int, tamanho: int) -> bytes
```
Deriva uma chave criptográfica a partir de uma senha usando PBKDF2 com HMAC-SHA256.

| Parâmetro | Descrição | Recomendação |
|-----------|-----------|--------------|
| `senha` | Senha do usuário | - |
| `salt` | Salt único por usuário | Mínimo 16 bytes |
| `iteracoes` | Número de iterações | 100.000+ para senhas |
| `tamanho` | Tamanho da chave em bytes | 32 para AES-256 |

```noxy
let salt: bytes = crypto.random_bytes(16)
let chave: bytes = crypto.pbkdf2_sha256("senha_forte", salt, 200000, 32)
```

---

### AES-256-GCM Encrypt
```noxy
crypto.aes256_gcm_encrypt(chave: bytes, texto: bytes) -> bytes
```
Criptografa dados usando AES-256-GCM (Authenticated Encryption).

- **Chave**: Deve ter exatamente 32 bytes
- **Retorno**: `nonce (12 bytes) + ciphertext + tag (16 bytes)`
- **Nonce**: Gerado automaticamente (único por operação)

```noxy
let dados: bytes = crypto.aes256_gcm_encrypt(chave, b"segredo")
```

---

### AES-256-GCM Decrypt
```noxy
crypto.aes256_gcm_decrypt(chave: bytes, dados: bytes) -> bytes
```
Descriptografa e valida dados criptografados com AES-256-GCM.

- Retorna `null` se a autenticação falhar (dados corrompidos ou chave errada)
- Valida automaticamente o MAC/tag para detectar adulteração

```noxy
let texto: bytes = crypto.aes256_gcm_decrypt(chave, dados)
if texto == null then
    print("Erro: descriptografia falhou")
end
```

---

## Exemplo Completo: Password Manager

```noxy
use crypto

// 1. Gerar salt único (armazenar junto com os dados)
let salt: bytes = crypto.random_bytes(16)

// 2. Derivar chave da senha mestra
let chave: bytes = crypto.pbkdf2_sha256("minha_senha_mestra", salt, 200000, 32)

// 3. Criptografar uma senha
let senha_facebook: bytes = b"fb_password_123"
let cifrado: bytes = crypto.aes256_gcm_encrypt(chave, senha_facebook)

// 4. Armazenar como hex (para SQLite/JSON)
let hex_cifrado: string = hex_encode(cifrado)

// 5. Recuperar
let cifrado_bytes: bytes = hex_decode(hex_cifrado)
let senha_original: bytes = crypto.aes256_gcm_decrypt(chave, cifrado_bytes)
print(to_str(senha_original))  // "fb_password_123"
```

---

## Segurança

| Componente | Padrão | Uso |
|------------|--------|-----|
| PBKDF2-HMAC-SHA256 | NIST SP 800-132 | Derivação de chave |
| AES-256-GCM | NIST SP 800-38D | Criptografia autenticada |
| crypto/rand | Go stdlib | Geração de números aleatórios |

> **Nota**: Estas funções usam as implementações nativas do Go (`crypto/*`), as mesmas utilizadas em produção por grandes empresas.
