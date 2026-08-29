package vm

import (
	"strings"
	"testing"
)

// Issue #120 item 1: wrappers da stdlib cujo nativo devolve null como
// resultado de dado dizem isso na assinatura — `time.parse`/`parse_date`
// -> DateTime?, `sqlite.prepare` -> Statement?. Com `use m select *` o nome
// importado carrega o tipo declarado, entao o contrato e estatico.

func TestTimeParseReturnsNullableDateTime(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "use time select *\nlet d: DateTime = parse(\"2024-02-29 14:05:06\")\n")
	if err == nil || !strings.Contains(err.Error(), "expected DateTime, got DateTime?") {
		t.Fatalf("want the nullable contract in the static type, got %v", err)
	}
}

func TestTimeParseNullOnBadInputIsTestable(t *testing.T) {
	got := captureVMSource(t, "use time select *\nlet d = parse(\"not-a-date\")\nlet ok = parse_date(\"2024-02-29\")\nlet bad = parse_date(\"2024-02-30\")\nlet score: int = 0\nif d == null then\n    score = score + 1\nend\nif ok != null then\n    score = score + 10 * ok.day\nend\nif bad == null then\n    score = score + 100\nend\ntest_report(score)\n")
	testExpectedObject(t, 1+290+100, got)
}

func TestSqlitePrepareReturnsNullableStatement(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "use sqlite select *\nlet db: Database = open(\":memory:\")\nlet stmt: Statement = prepare(db, \"SELECT 1\")\n")
	if err == nil || !strings.Contains(err.Error(), "expected Statement, got Statement?") {
		t.Fatalf("want the nullable contract in the static type, got %v", err)
	}
}

func TestSqlitePrepareNullOnBadSqlIsTestable(t *testing.T) {
	got := captureVMSource(t, "use sqlite select *\nlet db: Database = open(\":memory:\")\nlet bad = prepare(db, \"SELECT * FROM tabela_inexistente\")\nlet ok = prepare(db, \"SELECT 1\")\nlet score: int = 0\nif bad == null then\n    score = score + 1\nend\nif ok != null then\n    if ok.handle > 0 then\n        score = score + 10\n    end\n    finalize(ok)\nend\nclose(db)\ntest_report(score)\n")
	testExpectedObject(t, 11, got)
}

// Issue #121: em crypto so `aes256_gcm_decrypt` tem null como resultado de
// dado (autenticacao falhou, dados curtos demais) — a assinatura diz
// `-> bytes?`. Os outros nulls (n <= 0, iteracoes/tamanho <= 0, chave que
// nao tem 32 bytes) eram validacao de argumento e viraram erro tipado do
// native: nenhum wrapper `-> bytes` deixa null passar.

func TestCryptoDecryptReturnsNullableBytes(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "use crypto select *\nlet key: bytes = random_bytes(32)\nlet plain: bytes = aes256_gcm_decrypt(key, b\"x\")\n")
	if err == nil || !strings.Contains(err.Error(), "expected bytes, got bytes?") {
		t.Fatalf("want the nullable contract in the static type, got %v", err)
	}
}

func TestCryptoDecryptNullOnUndecryptableDataIsTestable(t *testing.T) {
	got := captureVMSource(t, `use crypto select *
let key: bytes = random_bytes(32)
let sealed: bytes = aes256_gcm_encrypt(key, b"segredo")
let opened = aes256_gcm_decrypt(key, sealed)
let garbage = aes256_gcm_decrypt(key, b"123456789012xx")
let short = aes256_gcm_decrypt(key, b"x")
let score: int = 0
if opened != null then
    if opened == b"segredo" then
        score = score + 1
    end
end
if garbage == null then
    score = score + 10
end
if short == null then
    score = score + 100
end
test_report(score)
`)
	testExpectedObject(t, 111, got)
}

func TestCryptoInvalidArgumentsRaiseInsteadOfNull(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"random_bytes zero", "use crypto select *\nlet z: bytes = random_bytes(0)\n", "crypto_random_bytes: n must be > 0, got 0"},
		{"random_bytes member form", "use crypto\nlet z: bytes = crypto.random_bytes(-1)\n", "crypto_random_bytes: n must be > 0, got -1"},
		{"pbkdf2 zero iterations", "use crypto select *\nlet k: bytes = pbkdf2_sha256(\"pw\", b\"salt\", 0, 32)\n", "crypto_pbkdf2_sha256: iterations must be > 0, got 0"},
		{"pbkdf2 zero length", "use crypto select *\nlet k: bytes = pbkdf2_sha256(\"pw\", b\"salt\", 1000, 0)\n", "crypto_pbkdf2_sha256: key length must be > 0, got 0"},
		{"encrypt short key", "use crypto select *\nlet e: bytes = aes256_gcm_encrypt(b\"short\", b\"texto\")\n", "crypto_aes256_gcm_encrypt: key must be 32 bytes, got 5"},
		{"decrypt short key", "use crypto select *\nlet d = aes256_gcm_decrypt(b\"short\", b\"texto\")\n", "crypto_aes256_gcm_decrypt: key must be 32 bytes, got 5"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := interpretOrCompileErr(t, New(), test.source)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want runtime error containing %q, got %v", test.want, err)
			}
		})
	}
}
