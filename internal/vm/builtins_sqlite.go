package vm

import (
	"database/sql"
	"fmt"

	"noxy-vm/internal/value"

	_ "modernc.org/sqlite"
)

func (vm *VM) defineSQLiteBuiltins() {
	vm.DefineContextualNative("sqlite_open", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 2 {
			return value.NewNull(), nil
		}
		structInst, ok := args[1].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}

		database, openErr := sql.Open("sqlite", args[0].String())
		open := openErr == nil
		if open {
			open = database.Ping() == nil
		}
		handle := machine.shared.Databases.add(&DatabaseResource{
			database: database,
			closed:   database == nil,
		})

		instance := value.NewInstance(structInst.Struct).Obj.(*value.ObjInstance)
		instance.Fields["handle"] = value.NewInt(int64(handle))
		instance.Fields["open"] = value.NewBool(open)
		return value.Value{Type: value.VAL_OBJ, Obj: instance}, nil
	})

	vm.DefineContextualNative("sqlite_close", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 1 {
			return value.NewNull(), nil
		}
		databaseInstance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}

		resource, exists := machine.shared.Databases.remove(int(databaseInstance.Fields["handle"].AsInt))
		if exists {
			resource.stateMu.Lock()
			database := resource.database
			resource.closed = true
			resource.stateMu.Unlock()
			if database != nil {
				_ = database.Close()
			}
			databaseInstance.Fields["open"] = value.NewBool(false)
		}
		return value.NewNull(), nil
	})

	vm.DefineContextualNative("sqlite_exec", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 3 {
			return value.NewNull(), nil
		}
		databaseInstance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultTemplate, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}

		database, valid := sqliteDatabase(machine, databaseInstance)
		if !valid {
			return sqliteExecError(resultTemplate.Struct, "invalid database handle"), nil
		}
		result, execErr := database.Exec(args[1].String())
		return sqliteExecResult(resultTemplate.Struct, result, execErr), nil
	})

	vm.DefineContextualNative("sqlite_exec_params", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 4 {
			return value.NewNull(), nil
		}
		databaseInstance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		parameters, ok := args[2].Obj.(*value.ObjArray)
		if !ok {
			return value.NewNull(), nil
		}
		resultTemplate, ok := args[3].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}

		database, valid := sqliteDatabase(machine, databaseInstance)
		if !valid {
			return sqliteExecError(resultTemplate.Struct, "invalid database handle"), nil
		}
		queryArguments := make([]interface{}, len(parameters.Elements))
		for index, parameter := range parameters.Elements {
			queryArguments[index] = sqliteParameter(parameter)
		}
		result, execErr := database.Exec(args[1].String(), queryArguments...)
		return sqliteExecResult(resultTemplate.Struct, result, execErr), nil
	})

	vm.DefineContextualNative("sqlite_prepare", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 3 {
			return value.NewNull(), nil
		}
		databaseInstance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		statementTemplate, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}

		database, valid := sqliteDatabase(machine, databaseInstance)
		if !valid {
			return value.NewNull(), nil
		}
		statement, prepareErr := database.Prepare(args[1].String())
		if prepareErr != nil {
			return value.NewNull(), nil
		}
		handle := machine.shared.Statements.add(&StatementResource{
			statement:  statement,
			parameters: make(map[int]interface{}),
		})
		instance := value.NewInstance(statementTemplate.Struct).Obj.(*value.ObjInstance)
		instance.Fields["handle"] = value.NewInt(int64(handle))
		return value.Value{Type: value.VAL_OBJ, Obj: instance}, nil
	})

	vm.DefineContextualNative("sqlite_bind_text", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 3 {
			return value.NewNull(), nil
		}
		return bindSQLiteParameter(machine, args, args[2].String()), nil
	})
	vm.DefineContextualNative("sqlite_bind_float", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 3 {
			return value.NewNull(), nil
		}
		return bindSQLiteParameter(machine, args, args[2].AsFloat), nil
	})
	vm.DefineContextualNative("sqlite_bind_int", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 3 {
			return value.NewNull(), nil
		}
		return bindSQLiteParameter(machine, args, args[2].AsInt), nil
	})

	vm.DefineContextualNative("sqlite_step_exec", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		statementInstance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultTemplate, ok := args[1].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}

		resource, exists := machine.shared.Statements.get(int(statementInstance.Fields["handle"].AsInt))
		if !exists {
			return sqliteExecError(resultTemplate.Struct, "invalid statement handle"), nil
		}
		resource.mu.Lock()
		if resource.closed || resource.statement == nil {
			resource.mu.Unlock()
			return sqliteExecError(resultTemplate.Struct, "invalid statement handle"), nil
		}
		statement := resource.statement
		maxIndex := 0
		for index := range resource.parameters {
			if index > maxIndex {
				maxIndex = index
			}
		}
		parameters := make([]interface{}, maxIndex)
		for index := 1; index <= maxIndex; index++ {
			parameters[index-1] = resource.parameters[index]
		}
		resource.parameters = make(map[int]interface{})
		resource.mu.Unlock()

		result, execErr := statement.Exec(parameters...)
		return sqliteExecResult(resultTemplate.Struct, result, execErr), nil
	})

	vm.DefineContextualNative("sqlite_reset", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), nil
		}
		statementInstance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resource, exists := machine.shared.Statements.get(int(statementInstance.Fields["handle"].AsInt))
		if exists {
			resource.mu.Lock()
			if !resource.closed && resource.statement != nil {
				resource.parameters = make(map[int]interface{})
			}
			resource.mu.Unlock()
		}
		return value.NewNull(), nil
	})

	vm.DefineContextualNative("sqlite_finalize", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), nil
		}
		statementInstance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resource, exists := machine.shared.Statements.remove(int(statementInstance.Fields["handle"].AsInt))
		if !exists {
			return value.NewNull(), nil
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
		return value.NewNull(), nil
	})

	vm.DefineContextualNative("sqlite_query", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 4 {
			return value.NewNull(), nil
		}
		databaseInstance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultTemplate, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		rowTemplate, ok := args[3].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}

		database, valid := sqliteDatabase(machine, databaseInstance)
		if !valid {
			return sqliteQueryError(resultTemplate.Struct, "invalid database handle"), nil
		}
		rows, queryErr := database.Query(args[1].String())
		if queryErr != nil {
			return sqliteQueryError(resultTemplate.Struct, queryErr.Error()), nil
		}
		defer rows.Close()

		columns, _ := rows.Columns()
		columnValues := make([]value.Value, len(columns))
		for index, column := range columns {
			columnValues[index] = value.NewString(column)
		}
		var rowValues []value.Value
		for rows.Next() {
			destination := make([]interface{}, len(columns))
			destinationPointers := make([]interface{}, len(columns))
			for index := range destination {
				destinationPointers[index] = &destination[index]
			}
			_ = rows.Scan(destinationPointers...)

			values := make([]value.Value, len(columns))
			for index, item := range destination {
				converted, convertErr := sqliteValueChecked(item)
				if convertErr != nil {
					return sqliteQueryError(resultTemplate.Struct, convertErr.Error()), nil
				}
				values[index] = converted
			}
			row := value.NewInstance(rowTemplate.Struct).Obj.(*value.ObjInstance)
			row.Fields["values"] = value.NewArray(values)
			rowValues = append(rowValues, value.Value{Type: value.VAL_OBJ, Obj: row})
		}

		result := value.NewInstance(resultTemplate.Struct).Obj.(*value.ObjInstance)
		result.Fields["columns"] = value.NewArray(columnValues)
		result.Fields["rows"] = value.NewArray(rowValues)
		result.Fields["row_count"] = value.NewInt(int64(len(rowValues)))
		result.Fields["ok"] = value.NewBool(true)
		result.Fields["error"] = value.NewString("")
		return value.Value{Type: value.VAL_OBJ, Obj: result}, nil
	})
}

func sqliteDatabase(machine *VM, instance *value.ObjInstance) (*sql.DB, bool) {
	resource, ok := machine.shared.Databases.get(int(instance.Fields["handle"].AsInt))
	if !ok {
		return nil, false
	}
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	if resource.closed || resource.database == nil {
		return nil, false
	}
	return resource.database, true
}

func bindSQLiteParameter(machine *VM, args []value.Value, parameter interface{}) value.Value {
	statementInstance, ok := args[0].Obj.(*value.ObjInstance)
	if !ok {
		return value.NewNull()
	}
	resource, exists := machine.shared.Statements.get(int(statementInstance.Fields["handle"].AsInt))
	if !exists {
		return value.NewNull()
	}
	resource.mu.Lock()
	defer resource.mu.Unlock()
	if resource.closed || resource.statement == nil {
		return value.NewNull()
	}
	if resource.parameters == nil {
		resource.parameters = make(map[int]interface{})
	}
	resource.parameters[int(args[1].AsInt)] = parameter
	return value.NewNull()
}

func sqliteParameter(parameter value.Value) interface{} {
	switch parameter.Type {
	case value.VAL_INT:
		return parameter.AsInt
	case value.VAL_FLOAT:
		return parameter.AsFloat
	case value.VAL_BOOL:
		return parameter.AsBool
	case value.VAL_NULL:
		return nil
	case value.VAL_BYTES:
		if payload, ok := parameter.Obj.(string); ok {
			return payload
		}
		return parameter.String()
	case value.VAL_OBJ:
		if text, ok := parameter.Obj.(string); ok {
			return text
		}
		return parameter.String()
	default:
		return parameter.String()
	}
}

func sqliteExecResult(definition *value.ObjStruct, result sql.Result, err error) value.Value {
	if err != nil {
		return sqliteExecError(definition, err.Error())
	}
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	instance := value.NewInstance(definition).Obj.(*value.ObjInstance)
	instance.Fields["ok"] = value.NewBool(true)
	instance.Fields["error"] = value.NewString("")
	instance.Fields["rows_affected"] = value.NewInt(rowsAffected)
	instance.Fields["last_insert_id"] = value.NewInt(lastInsertID)
	return value.Value{Type: value.VAL_OBJ, Obj: instance}
}

func sqliteExecError(definition *value.ObjStruct, errorText string) value.Value {
	instance := value.NewInstance(definition).Obj.(*value.ObjInstance)
	instance.Fields["ok"] = value.NewBool(false)
	instance.Fields["error"] = value.NewString(errorText)
	instance.Fields["rows_affected"] = value.NewInt(0)
	instance.Fields["last_insert_id"] = value.NewInt(0)
	return value.Value{Type: value.VAL_OBJ, Obj: instance}
}

func sqliteQueryError(definition *value.ObjStruct, errorText string) value.Value {
	instance := value.NewInstance(definition).Obj.(*value.ObjInstance)
	instance.Fields["columns"] = value.NewArray(nil)
	instance.Fields["rows"] = value.NewArray(nil)
	instance.Fields["row_count"] = value.NewInt(0)
	instance.Fields["ok"] = value.NewBool(false)
	instance.Fields["error"] = value.NewString(errorText)
	return value.Value{Type: value.VAL_OBJ, Obj: instance}
}

func sqliteValue(item interface{}) value.Value {
	switch typed := item.(type) {
	case nil:
		return value.NewNull()
	case int64:
		return value.NewInt(typed)
	case float64:
		return value.NewFloat(typed)
	case string:
		return value.NewString(typed)
	case []byte:
		return value.NewString(string(typed))
	default:
		return value.NewString(fmt.Sprintf("%v", typed))
	}
}

// sqliteValueChecked converts a scanned column, rejecting TEXT and BLOB
// payloads that are not valid UTF-8 rather than letting them become a Noxy
// string that decodes to U+FFFD.
func sqliteValueChecked(item interface{}) (value.Value, error) {
	switch typed := item.(type) {
	case string:
		if err := requireValidUTF8("sqlite.query", typed); err != nil {
			return value.NewNull(), err
		}
		return value.NewString(typed), nil
	case []byte:
		if err := requireValidUTF8("sqlite.query", string(typed)); err != nil {
			return value.NewNull(), err
		}
		return value.NewString(string(typed)), nil
	}
	return sqliteValue(item), nil
}
