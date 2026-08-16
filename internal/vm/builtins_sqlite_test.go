package vm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

func cleanupSQLiteResources(t *testing.T, machine *VM) {
	t.Helper()
	t.Cleanup(func() {
		for handle := range machine.shared.Statements.snapshot() {
			resource, ok := machine.shared.Statements.remove(handle)
			if !ok {
				continue
			}
			resource.mu.Lock()
			resource.closed = true
			statement := resource.statement
			resource.statement = nil
			resource.parameters = nil
			resource.mu.Unlock()
			if statement != nil {
				_ = statement.Close()
			}
		}
		for handle := range machine.shared.Databases.snapshot() {
			resource, ok := machine.shared.Databases.remove(handle)
			if !ok {
				continue
			}
			resource.stateMu.Lock()
			resource.closed = true
			database := resource.database
			resource.stateMu.Unlock()
			if database != nil {
				_ = database.Close()
			}
		}
	})
}

type sqliteResourceObservation struct {
	statements int
	databases  int
}

func recordDeferredSQLiteResources(machine *VM, database **DatabaseResource, statement **StatementResource) {
	machine.DefineNative("record_sqlite_resources", func(args []value.Value) value.Value {
		if len(args) != 2 {
			return value.NewNull()
		}
		databaseInstance, databaseOK := args[0].Obj.(*value.ObjInstance)
		statementInstance, statementOK := args[1].Obj.(*value.ObjInstance)
		if !databaseOK || !statementOK {
			return value.NewNull()
		}
		*database, _ = machine.shared.Databases.get(int(databaseInstance.Fields["handle"].AsInt))
		*statement, _ = machine.shared.Statements.get(int(statementInstance.Fields["handle"].AsInt))
		return value.NewNull()
	})
}

func requireDeferredSQLiteClosed(t *testing.T, machine *VM, database *DatabaseResource, statement *StatementResource, path string) {
	t.Helper()
	if got := len(machine.shared.Statements.snapshot()); got != 0 {
		t.Fatalf("statements=%d, want 0", got)
	}
	if got := len(machine.shared.Databases.snapshot()); got != 0 {
		t.Fatalf("databases=%d, want 0", got)
	}
	if database == nil || statement == nil {
		t.Fatalf("resources not recorded: database=%p statement=%p", database, statement)
	}
	statement.mu.Lock()
	statementClosed := statement.closed
	statement.mu.Unlock()
	database.stateMu.Lock()
	databaseClosed := database.closed
	database.stateMu.Unlock()
	if !statementClosed || !databaseClosed {
		t.Fatalf("resources remain open: statement=%v database=%v", statementClosed, databaseClosed)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove deferred database: %v", err)
	}
}

func TestDeferredSQLiteCleanupFinalizesStatementBeforeDatabaseOnEveryExit(t *testing.T) {
	tests := []struct {
		name      string
		suffix    string
		wantError bool
	}{
		{"normal", "", false},
		{"runtime error", "\nlet zero: int = 0\nprint(1 / zero)", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machine := New()
			cleanupSQLiteResources(t, machine)
			path := filepath.Join(t.TempDir(), "deferred.sqlite")
			var database *DatabaseResource
			var statement *StatementResource
			var observations []sqliteResourceObservation
			recordDeferredSQLiteResources(machine, &database, &statement)
			machine.DefineContextualNative("assert_statement_finalized", func(value.NativeContext, []value.Value) (value.Value, error) {
				observations = append(observations, sqliteResourceObservation{
					statements: len(machine.shared.Statements.snapshot()),
					databases:  len(machine.shared.Databases.snapshot()),
				})
				return value.NewNull(), nil
			})
			source := "use sqlite\nlet db: sqlite.Database = sqlite.open(" + strconv.Quote(path) + ")\n" +
				"defer sqlite.close(db)\nlet stmt: sqlite.Statement = sqlite.prepare(db, \"SELECT 1\")\n" +
				"defer assert_statement_finalized()\ndefer sqlite.finalize(stmt)\nrecord_sqlite_resources(db, stmt)" + test.suffix

			err := interpretVMSource(t, machine, source)
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v, wantError=%v", err, test.wantError)
			}
			if len(observations) != 1 || observations[0] != (sqliteResourceObservation{statements: 0, databases: 1}) {
				t.Fatalf("observations=%v, want [{statements:0 databases:1}]", observations)
			}
			requireDeferredSQLiteClosed(t, machine, database, statement, path)
		})
	}
}

func TestDeferredSQLiteResourceCleanupContinuesAfterFailure(t *testing.T) {
	machine := New()
	cleanupSQLiteResources(t, machine)
	path := filepath.Join(t.TempDir(), "deferred-failure.sqlite")
	var database *DatabaseResource
	var statement *StatementResource
	var observations []sqliteResourceObservation
	recordDeferredSQLiteResources(machine, &database, &statement)
	machine.DefineContextualNative("assert_statement_finalized", func(value.NativeContext, []value.Value) (value.Value, error) {
		observations = append(observations, sqliteResourceObservation{
			statements: len(machine.shared.Statements.snapshot()),
			databases:  len(machine.shared.Databases.snapshot()),
		})
		return value.NewNull(), nil
	})
	sentinel := errors.New("sentinel cleanup failure")
	defineCleanupFailureNative(machine, sentinel)
	source := "use sqlite\nlet db: sqlite.Database = sqlite.open(" + strconv.Quote(path) + ")\n" +
		"defer sqlite.close(db)\nlet stmt: sqlite.Statement = sqlite.prepare(db, \"SELECT 1\")\n" +
		"defer assert_statement_finalized()\ndefer sqlite.finalize(stmt)\n" +
		"record_sqlite_resources(db, stmt)\ndefer cleanup_fail()"

	err := interpretVMSource(t, machine, source)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error=%v, want sentinel cleanup failure", err)
	}
	if len(observations) != 1 || observations[0] != (sqliteResourceObservation{statements: 0, databases: 1}) {
		t.Fatalf("observations=%v, want [{statements:0 databases:1}]", observations)
	}
	requireDeferredSQLiteClosed(t, machine, database, statement, path)
}

func TestSQLiteHandlesAreSharedAcrossVMs(t *testing.T) {
	parent := New()
	cleanupSQLiteResources(t, parent)
	child := NewWithShared(parent.shared, parent.Config)
	definitions := newSQLiteTestDefinitions()
	database := callBuiltin(t, parent, "sqlite_open",
		value.NewString(filepath.Join(t.TempDir(), "shared.sqlite")),
		sqliteTemplate(definitions.database),
	)
	defer callBuiltin(t, parent, "sqlite_close", database)
	databaseHandle := int(requireBuiltinInstance(t, database, definitions.database).Fields["handle"].AsInt)
	if _, ok := parent.shared.Databases.get(databaseHandle); !ok {
		t.Fatalf("database handle %d was not published to shared resources", databaseHandle)
	}
	requireSQLiteExecResult(t, callBuiltin(t, parent, "sqlite_exec",
		database,
		value.NewString("CREATE TABLE entries (id INTEGER, name TEXT)"),
		sqliteTemplate(definitions.execResult),
	), definitions, true, "")
	statement := callBuiltin(t, parent, "sqlite_prepare",
		database,
		value.NewString("INSERT INTO entries VALUES (?, ?)"),
		sqliteTemplate(definitions.statement),
	)
	defer callBuiltin(t, parent, "sqlite_finalize", statement)
	statementHandle := int(requireBuiltinInstance(t, statement, definitions.statement).Fields["handle"].AsInt)
	if _, ok := parent.shared.Statements.get(statementHandle); !ok {
		t.Fatalf("statement handle %d was not published to shared resources", statementHandle)
	}
	callBuiltin(t, parent, "sqlite_bind_int", statement, value.NewInt(1), value.NewInt(1))
	callBuiltin(t, child, "sqlite_bind_text", statement, value.NewInt(2), value.NewString("shared"))
	requireSQLiteExecResult(t, callBuiltin(t, child, "sqlite_step_exec",
		statement, sqliteTemplate(definitions.execResult),
	), definitions, true, "")

	query := requireBuiltinInstance(t, callBuiltin(t, parent, "sqlite_query",
		database,
		value.NewString("SELECT id, name FROM entries"),
		sqliteTemplate(definitions.queryResult),
		sqliteTemplate(definitions.row),
	), definitions.queryResult)
	assertBuiltinValue(t, query.Fields["ok"], value.NewBool(true))
	assertBuiltinValue(t, query.Fields["row_count"], value.NewInt(1))
	rows := requireBuiltinArray(t, query.Fields["rows"])
	row := requireBuiltinInstance(t, rows.Elements[0], definitions.row)
	assertBuiltinArray(t, row.Fields["values"], []value.Value{value.NewInt(1), value.NewString("shared")})
}

func TestSQLiteStatementParametersConcurrent(t *testing.T) {
	machine := New()
	cleanupSQLiteResources(t, machine)
	definitions := newSQLiteTestDefinitions()
	database := callBuiltin(t, machine, "sqlite_open",
		value.NewString(filepath.Join(t.TempDir(), "parameters.sqlite")),
		sqliteTemplate(definitions.database),
	)
	defer callBuiltin(t, machine, "sqlite_close", database)
	statement := callBuiltin(t, machine, "sqlite_prepare",
		database,
		value.NewString("SELECT ?, ?"),
		sqliteTemplate(definitions.statement),
	)
	defer callBuiltin(t, machine, "sqlite_finalize", statement)
	statementHandle := int(requireBuiltinInstance(t, statement, definitions.statement).Fields["handle"].AsInt)
	if _, ok := machine.shared.Statements.get(statementHandle); !ok {
		t.Fatalf("statement handle %d was not published to shared resources", statementHandle)
	}

	bindInt := requireBuiltin(t, machine, "sqlite_bind_int")
	bindText := requireBuiltin(t, machine, "sqlite_bind_text")
	start := make(chan struct{})
	errors := make(chan error, 16)
	var workers sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			for iteration := 0; iteration < 100; iteration++ {
				var err error
				if index%2 == 0 {
					_, err = bindInt.Invoke(machine, []value.Value{statement, value.NewInt(1), value.NewInt(int64(index))})
				} else {
					_, err = bindText.Invoke(machine, []value.Value{statement, value.NewInt(2), value.NewString(fmt.Sprint(index))})
				}
				if err != nil {
					errors <- err
					return
				}
			}
		}(worker)
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent bind: %v", err)
	}
	callBuiltin(t, machine, "sqlite_reset", statement)
	callBuiltin(t, machine, "sqlite_finalize", statement)
}

func TestSQLiteBuiltinsTemporaryDatabaseLifecycle(t *testing.T) {
	machine := New()
	cleanupSQLiteResources(t, machine)
	definitions := newSQLiteTestDefinitions()
	databasePath := filepath.Join(t.TempDir(), "stateful-builtins.sqlite")
	databaseTemplate := sqliteTemplate(definitions.database)
	databaseValue := callBuiltin(t, machine, "sqlite_open", value.NewString(databasePath), databaseTemplate)
	database := requireBuiltinInstance(t, databaseValue, definitions.database)
	assertBuiltinValue(t, database.Fields["open"], value.NewBool(true))
	databaseHandle := int(database.Fields["handle"].AsInt)
	if _, ok := machine.shared.Databases.get(databaseHandle); !ok {
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
	statementResource, ok := machine.shared.Statements.get(statementHandle)
	if !ok {
		t.Fatalf("statement handle %d is not registered", statementHandle)
	}

	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_int", statementValue, value.NewInt(1), value.NewInt(2)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_text", statementValue, value.NewInt(2), value.NewString("beta")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_float", statementValue, value.NewInt(3), value.NewFloat(2.5)), value.NewNull())
	stepResult := requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_step_exec", statementValue, sqliteTemplate(definitions.execResult)), definitions, true, "")
	assertBuiltinValue(t, stepResult.Fields["rows_affected"], value.NewInt(1))

	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_reset", statementValue), value.NewNull())
	statementResource.mu.Lock()
	parameterCount := len(statementResource.parameters)
	statementResource.mu.Unlock()
	if parameterCount != 0 {
		t.Fatalf("statement parameter count after reset = %d, want 0", parameterCount)
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
	if _, ok := machine.shared.Statements.get(statementHandle); ok {
		t.Fatalf("finalized statement handle %d remains registered", statementHandle)
	}
	invalidStep := requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_step_exec", statementValue, sqliteTemplate(definitions.execResult)), definitions, false, "invalid statement handle")
	assertBuiltinValue(t, invalidStep.Fields["rows_affected"], value.NewInt(0))
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_bind_int", statementValue, value.NewInt(1), value.NewInt(99)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_reset", statementValue), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_finalize", statementValue), value.NewNull())

	assertBuiltinValue(t, callBuiltin(t, machine, "sqlite_close", databaseValue), value.NewNull())
	assertBuiltinValue(t, database.Fields["open"], value.NewBool(false))
	if _, ok := machine.shared.Databases.get(databaseHandle); ok {
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

func TestSQLiteQueryRejectsInvalidUTF8(t *testing.T) {
	machine := New()
	cleanupSQLiteResources(t, machine)
	definitions := newSQLiteTestDefinitions()
	databaseValue := callBuiltin(t, machine, "sqlite_open",
		value.NewString(filepath.Join(t.TempDir(), "invalid-utf8.sqlite")),
		sqliteTemplate(definitions.database),
	)
	defer callBuiltin(t, machine, "sqlite_close", databaseValue)

	requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec",
		databaseValue,
		value.NewString("CREATE TABLE entries (id INTEGER, value TEXT)"),
		sqliteTemplate(definitions.execResult),
	), definitions, true, "")

	// Bind the invalid byte as a Noxy `bytes` value so it reaches the TEXT
	// column without going through any layer that assumes valid UTF-8.
	insertResult := requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec_params",
		databaseValue,
		value.NewString("INSERT INTO entries (id, value) VALUES (?, ?)"),
		value.NewArray([]value.Value{value.NewInt(1), value.NewBytes("hello\xffworld")}),
		sqliteTemplate(definitions.execResult),
	), definitions, true, "")
	assertBuiltinValue(t, insertResult.Fields["rows_affected"], value.NewInt(1))

	query := requireBuiltinInstance(t, callBuiltin(t, machine, "sqlite_query",
		databaseValue,
		value.NewString("SELECT id, value FROM entries"),
		sqliteTemplate(definitions.queryResult),
		sqliteTemplate(definitions.row),
	), definitions.queryResult)
	assertBuiltinValue(t, query.Fields["ok"], value.NewBool(false))
	assertBuiltinValue(t, query.Fields["row_count"], value.NewInt(0))
	errorText, ok := query.Fields["error"].Obj.(string)
	if !ok {
		t.Fatalf("query error field = %#v, want string", query.Fields["error"])
	}
	if !strings.Contains(errorText, "UTF-8") {
		t.Fatalf("query error = %q, want it to mention UTF-8", errorText)
	}
}

func TestSQLiteQueryAllowsValidAccentedText(t *testing.T) {
	machine := New()
	cleanupSQLiteResources(t, machine)
	definitions := newSQLiteTestDefinitions()
	databaseValue := callBuiltin(t, machine, "sqlite_open",
		value.NewString(filepath.Join(t.TempDir(), "valid-utf8.sqlite")),
		sqliteTemplate(definitions.database),
	)
	defer callBuiltin(t, machine, "sqlite_close", databaseValue)

	requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec",
		databaseValue,
		value.NewString("CREATE TABLE entries (id INTEGER, value TEXT)"),
		sqliteTemplate(definitions.execResult),
	), definitions, true, "")

	insertResult := requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec_params",
		databaseValue,
		value.NewString("INSERT INTO entries (id, value) VALUES (?, ?)"),
		value.NewArray([]value.Value{value.NewInt(1), value.NewString("acentuação")}),
		sqliteTemplate(definitions.execResult),
	), definitions, true, "")
	assertBuiltinValue(t, insertResult.Fields["rows_affected"], value.NewInt(1))

	query := requireBuiltinInstance(t, callBuiltin(t, machine, "sqlite_query",
		databaseValue,
		value.NewString("SELECT id, value FROM entries"),
		sqliteTemplate(definitions.queryResult),
		sqliteTemplate(definitions.row),
	), definitions.queryResult)
	assertBuiltinValue(t, query.Fields["ok"], value.NewBool(true))
	assertBuiltinValue(t, query.Fields["error"], value.NewString(""))
	assertBuiltinValue(t, query.Fields["row_count"], value.NewInt(1))
	rows := requireBuiltinArray(t, query.Fields["rows"])
	row := requireBuiltinInstance(t, rows.Elements[0], definitions.row)
	assertBuiltinArray(t, row.Fields["values"], []value.Value{value.NewInt(1), value.NewString("acentuação")})
}

func TestSQLiteBindsBytesParameterWithoutCorruption(t *testing.T) {
	machine := New()
	cleanupSQLiteResources(t, machine)
	definitions := newSQLiteTestDefinitions()
	databaseValue := callBuiltin(t, machine, "sqlite_open",
		value.NewString(filepath.Join(t.TempDir(), "bytes-param.sqlite")),
		sqliteTemplate(definitions.database),
	)
	defer callBuiltin(t, machine, "sqlite_close", databaseValue)

	requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec",
		databaseValue,
		value.NewString("CREATE TABLE entries (id INTEGER, value TEXT)"),
		sqliteTemplate(definitions.execResult),
	), definitions, true, "")

	requireSQLiteExecResult(t, callBuiltin(t, machine, "sqlite_exec_params",
		databaseValue,
		value.NewString("INSERT INTO entries (id, value) VALUES (?, ?)"),
		value.NewArray([]value.Value{value.NewInt(1), value.NewBytes("hello")}),
		sqliteTemplate(definitions.execResult),
	), definitions, true, "")

	query := requireBuiltinInstance(t, callBuiltin(t, machine, "sqlite_query",
		databaseValue,
		value.NewString("SELECT value FROM entries"),
		sqliteTemplate(definitions.queryResult),
		sqliteTemplate(definitions.row),
	), definitions.queryResult)
	assertBuiltinValue(t, query.Fields["ok"], value.NewBool(true))

	rows, ok := query.Fields["rows"].Obj.(*value.ObjArray)
	if !ok || len(rows.Elements) != 1 {
		t.Fatalf("rows = %#v, want exactly one row", query.Fields["rows"])
	}
	row, ok := rows.Elements[0].Obj.(*value.ObjInstance)
	if !ok {
		t.Fatalf("row = %#v, want *ObjInstance", rows.Elements[0])
	}
	values, ok := row.Fields["values"].Obj.(*value.ObjArray)
	if !ok || len(values.Elements) != 1 {
		t.Fatalf("row values = %#v, want exactly one column", row.Fields["values"])
	}
	stored, ok := values.Elements[0].Obj.(string)
	if !ok || stored != "hello" {
		t.Fatalf("stored value = %#v, want the raw string %q with no b\"...\" wrapper", values.Elements[0], "hello")
	}
}
