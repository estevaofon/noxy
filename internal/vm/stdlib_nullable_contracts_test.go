package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
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

func TestNamespaceCallIntoNonNullableSlotIsCheckedAtRuntime(t *testing.T) {
	// Pela forma de namespace (`sqlite.prepare`) o membro nao tem tipo
	// estatico: a guarda de tipo desconhecido e quem segura o null.
	err := interpretOrCompileErr(t, New(), "use sqlite\nlet db: sqlite.Database = sqlite.open(\":memory:\")\nlet stmt: sqlite.Statement = sqlite.prepare(db, \"SELECT * FROM tabela_inexistente\")\n")
	if err == nil || !strings.Contains(err.Error(), "expected Statement, got null\n  hint: declare the slot as 'Statement?' to allow null") {
		t.Fatalf("want the runtime null guard, got %v", err)
	}
	_ = value.NewNull
}
