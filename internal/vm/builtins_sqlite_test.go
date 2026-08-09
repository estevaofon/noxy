package vm

import (
	"path/filepath"
	"testing"

	"noxy-vm/internal/value"
)

type sqliteTestDefinitions struct {
	database    value.Value
	statement   value.Value
	row         value.Value
	queryResult value.Value
	execResult  value.Value
}

func newSQLiteTestDefinitions() sqliteTestDefinitions {
	return sqliteTestDefinitions{
		database:    value.NewStruct("Database", []string{"handle", "open"}),
		statement:   value.NewStruct("Statement", []string{"handle"}),
		row:         value.NewStruct("Row", []string{"values"}),
		queryResult: value.NewStruct("QueryResult", []string{"columns", "rows", "row_count", "ok", "error"}),
		execResult:  value.NewStruct("ExecResult", []string{"ok", "error", "rows_affected", "last_insert_id"}),
	}
}

func sqliteTemplate(definition value.Value) value.Value {
	return value.NewInstance(definition.Obj.(*value.ObjStruct))
}

func requireSQLiteExecResult(t *testing.T, got value.Value, definitions sqliteTestDefinitions, ok bool, errorText string) *value.ObjInstance {
	t.Helper()
	result := requireBuiltinInstance(t, got, definitions.execResult)
	assertBuiltinValue(t, result.Fields["ok"], value.NewBool(ok))
	assertBuiltinValue(t, result.Fields["error"], value.NewString(errorText))
	return result
}

func TestSQLiteBuiltinsTemporaryDatabaseLifecycle(t *testing.T) {
	machine := New()
	defer func() {
		machine.shared.DbLock.Lock()
		defer machine.shared.DbLock.Unlock()
		for handle, statement := range machine.shared.StmtHandles {
			_ = statement.Close()
			delete(machine.shared.StmtHandles, handle)
			delete(machine.shared.StmtParams, handle)
		}
		for handle, database := range machine.shared.DbHandles {
			_ = database.Close()
			delete(machine.shared.DbHandles, handle)
		}
	}()
	definitions := newSQLiteTestDefinitions()
	databasePath := filepath.Join(t.TempDir(), "stateful-builtins.sqlite")
	databaseTemplate := sqliteTemplate(definitions.database)
	databaseValue := callBuiltin(t, machine, "sqlite_open", value.NewString(databasePath), databaseTemplate)
	database := requireBuiltinInstance(t, databaseValue, definitions.database)
	assertBuiltinValue(t, database.Fields["open"], value.NewBool(true))
	databaseHandle := int(database.Fields["handle"].AsInt)
	if _, ok := machine.shared.DbHandles[databaseHandle]; !ok {
		t.Fatalf("database handle %d is not registered", databaseHandle)
	}

	createResult := requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec",
		databaseValue,
		value.NewString("CREATE TABLE entries (id INTEGER PRIMARY KEY, name TEXT NOT NULL, score REAL NOT NULL)"),
		sqliteTemplate(definitions.execResult),
	), definitions, true, "")
	assertBuiltinValue(t, createResult.Fields["rows_affected"], value.NewInt(0))

	paramsResult := requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec_params",
		databaseValue,
		value.NewString("INSERT INTO entries (id, name, score) VALUES (?, ?, ?)"),
		value.NewArray([]value.Value{value.NewInt(1), value.NewString("alpha"), value.NewFloat(1.25)}),
		sqliteTemplate(definitions.execResult),
	), definitions, true, "")
	assertBuiltinValue(t, paramsResult.Fields["rows_affected"], value.NewInt(1))

	statementValue := callBuiltin(t, machine, "sqlite_prepare",
		databaseValue,
		value.NewString("INSERT INTO entries (id, name, score) VALUES (?, ?, ?)"),
		sqliteTemplate(definitions.statement),
	)
	statement := requireBuiltinInstance(t, statementValue, definitions.statement)
	statementHandle := int(statement.Fields["handle"].AsInt)
	if _, ok := machine.shared.StmtHandles[statementHandle]; !ok {
		t.Fatalf("statement handle %d is not registered", statementHandle)
	}

	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_int", statementValue, value.NewInt(1), value.NewInt(2)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_text", statementValue, value.NewInt(2), value.NewString("beta")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_float", statementValue, value.NewInt(3), value.NewFloat(2.5)), value.NewNull())
	stepResult := requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_step_exec", statementValue, sqliteTemplate(definitions.execResult)), definitions, true, "")
	assertBuiltinValue(t, stepResult.Fields["rows_affected"], value.NewInt(1))

	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_reset", statementValue), value.NewNull())
	if got := len(machine.shared.StmtParams[statementHandle]); got != 0 {
		t.Fatalf("statement parameter count after reset = %d, want 0", got)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_int", statementValue, value.NewInt(1), value.NewInt(3)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_text", statementValue, value.NewInt(2), value.NewString("gamma")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_float", statementValue, value.NewInt(3), value.NewFloat(3.75)), value.NewNull())
	requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_step_exec", statementValue, sqliteTemplate(definitions.execResult)), definitions, true, "")

	query := requireBuiltinInstance(t, callBuiltin(t, machine, "sqlite_query",
		databaseValue,
		value.NewString("SELECT id, name, score FROM entries ORDER BY id"),
		sqliteTemplate(definitions.queryResult),
		sqliteTemplate(definitions.row),
	), definitions.queryResult)
	assertBuiltinValue(t, query.Fields["ok"], value.NewBool(true))
	assertBuiltinValue(t, query.Fields["error"], value.NewString(""))
	assertBuiltinValue(t, query.Fields["row_count"], value.NewInt(3))
	assertBuiltinArray(t, query.Fields["columns"], []value.Value{value.NewString("id"), value.NewString("name"), value.NewString("score")})
	rows := requireBuiltinArray(t, query.Fields["rows"])
	wantRows := [][]value.Value{
		{value.NewInt(1), value.NewString("alpha"), value.NewFloat(1.25)},
		{value.NewInt(2), value.NewString("beta"), value.NewFloat(2.5)},
		{value.NewInt(3), value.NewString("gamma"), value.NewFloat(3.75)},
	}
	if len(rows.Elements) != len(wantRows) {
		t.Fatalf("query row count = %d, want %d", len(rows.Elements), len(wantRows))
	}
	for index, want := range wantRows {
		row := requireBuiltinInstance(t, rows.Elements[index], definitions.row)
		assertBuiltinArray(t, row.Fields["values"], want)
	}

	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_finalize", statementValue), value.NewNull())
	if _, ok := machine.shared.StmtHandles[statementHandle]; ok {
		t.Fatalf("finalized statement handle %d remains registered", statementHandle)
	}
	if _, ok := machine.shared.StmtParams[statementHandle]; ok {
		t.Fatalf("finalized statement parameters %d remain registered", statementHandle)
	}
	invalidStep := requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_step_exec", statementValue, sqliteTemplate(definitions.execResult)), definitions, false, "invalid statement handle")
	assertBuiltinValue(t, invalidStep.Fields["rows_affected"], value.NewInt(0))
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_int", statementValue, value.NewInt(1), value.NewInt(99)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_reset", statementValue), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_finalize", statementValue), value.NewNull())

	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_close", databaseValue), value.NewNull())
	assertBuiltinValue(t, database.Fields["open"], value.NewBool(false))
	if _, ok := machine.shared.DbHandles[databaseHandle]; ok {
		t.Fatalf("closed database handle %d remains registered", databaseHandle)
	}
	requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec", databaseValue, value.NewString("SELECT 1"), sqliteTemplate(definitions.execResult)), definitions, false, "invalid database handle")
	requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec_params", databaseValue, value.NewString("SELECT ?"), value.NewArray([]value.Value{value.NewInt(1)}), sqliteTemplate(definitions.execResult)), definitions, false, "invalid database handle")
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_prepare", databaseValue, value.NewString("SELECT 1"), sqliteTemplate(definitions.statement)), value.NewNull())
	invalidQuery := requireBuiltinInstance(t, callBuiltin(t, machine, "sqlite_query", databaseValue, value.NewString("SELECT 1"), sqliteTemplate(definitions.queryResult), sqliteTemplate(definitions.row)), definitions.queryResult)
	assertBuiltinValue(t, invalidQuery.Fields["ok"], value.NewBool(false))
	assertBuiltinValue(t, invalidQuery.Fields["error"], value.NewString("invalid database handle"))
	assertBuiltinValue(t, invalidQuery.Fields["row_count"], value.NewInt(0))
}
