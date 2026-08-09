package vm

import (
	"database/sql"
	"fmt"

	"noxy-vm/internal/value"

	_ "modernc.org/sqlite"
)

func (vm *VM) defineSQLiteBuiltins() {
	// SQLite Native Functions
	vm.DefineNative("sqlite_open", func(args []value.Value) value.Value {
		if len(args) != 2 {
			return value.NewNull()
		} // path, wrapper struct
		path := args[0].String()
		structInst, ok := args[1].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		structDef := structInst.Struct

		db, err := sql.Open("sqlite", path)
		openVal := true
		if err != nil {
			openVal = false
		} else {
			if err = db.Ping(); err != nil {
				openVal = false
			}
		}

		vm.shared.DbLock.Lock()
		id := vm.shared.NextDbID
		vm.shared.NextDbID++
		vm.shared.DbHandles[id] = db
		vm.shared.DbLock.Unlock()

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["handle"] = value.NewInt(int64(id))
		inst.Fields["open"] = value.NewBool(openVal)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.DefineNative("sqlite_close", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		dbInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}

		handle := int(dbInst.Fields["handle"].AsInt)

		vm.shared.DbLock.Lock()
		defer vm.shared.DbLock.Unlock()

		if db, ok := vm.shared.DbHandles[handle]; ok {
			db.Close()
			delete(vm.shared.DbHandles, handle)
			dbInst.Fields["open"] = value.NewBool(false)
		}
		return value.NewNull()
	})

	vm.DefineNative("sqlite_exec", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		}
		dbInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		sqlStr := args[1].String()

		resTmplInst, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct := resTmplInst.Struct

		handle := int(dbInst.Fields["handle"].AsInt)

		vm.shared.DbLock.Lock()
		db, ok := vm.shared.DbHandles[handle]
		vm.shared.DbLock.Unlock() // Unlock for Exec

		if ok {
			result, err := db.Exec(sqlStr)
			resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
			if err != nil {
				resInst.Fields["ok"] = value.NewBool(false)
				resInst.Fields["error"] = value.NewString(err.Error())
				resInst.Fields["rows_affected"] = value.NewInt(0)
				resInst.Fields["last_insert_id"] = value.NewInt(0)
			} else {
				rowsAffected, _ := result.RowsAffected()
				lastId, _ := result.LastInsertId()
				resInst.Fields["ok"] = value.NewBool(true)
				resInst.Fields["error"] = value.NewString("")
				resInst.Fields["rows_affected"] = value.NewInt(rowsAffected)
				resInst.Fields["last_insert_id"] = value.NewInt(lastId)
			}
			return value.Value{Type: value.VAL_OBJ, Obj: resInst}
		}
		// Invalid handle
		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["ok"] = value.NewBool(false)
		resInst.Fields["error"] = value.NewString("invalid database handle")
		resInst.Fields["rows_affected"] = value.NewInt(0)
		resInst.Fields["last_insert_id"] = value.NewInt(0)
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})

	vm.DefineNative("sqlite_exec_params", func(args []value.Value) value.Value {
		if len(args) < 4 {
			return value.NewNull()
		}
		dbInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		sqlStr := args[1].String()
		paramsArray, ok := args[2].Obj.(*value.ObjArray)
		if !ok {
			return value.NewNull()
		}

		resTmplInst, ok := args[3].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct := resTmplInst.Struct

		handle := int(dbInst.Fields["handle"].AsInt)

		vm.shared.DbLock.Lock()
		db, ok := vm.shared.DbHandles[handle]
		vm.shared.DbLock.Unlock()

		if ok {
			// Convert params
			queryArgs := make([]interface{}, len(paramsArray.Elements))
			for i, val := range paramsArray.Elements {
				switch val.Type {
				case value.VAL_INT:
					queryArgs[i] = val.AsInt
				case value.VAL_FLOAT:
					queryArgs[i] = val.AsFloat
				case value.VAL_BOOL:
					queryArgs[i] = val.AsBool
				case value.VAL_NULL:
					queryArgs[i] = nil
				case value.VAL_OBJ:
					if b, ok := val.Obj.(string); ok {
						queryArgs[i] = b
					} else {
						queryArgs[i] = val.String()
					}
				default:
					queryArgs[i] = val.String()
				}
			}

			result, err := db.Exec(sqlStr, queryArgs...)
			resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
			if err != nil {
				resInst.Fields["ok"] = value.NewBool(false)
				resInst.Fields["error"] = value.NewString(err.Error())
				resInst.Fields["rows_affected"] = value.NewInt(0)
				resInst.Fields["last_insert_id"] = value.NewInt(0)
			} else {
				rowsAffected, _ := result.RowsAffected()
				lastId, _ := result.LastInsertId()
				resInst.Fields["ok"] = value.NewBool(true)
				resInst.Fields["error"] = value.NewString("")
				resInst.Fields["rows_affected"] = value.NewInt(rowsAffected)
				resInst.Fields["last_insert_id"] = value.NewInt(lastId)
			}
			return value.Value{Type: value.VAL_OBJ, Obj: resInst}
		}
		// Invalid handle
		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["ok"] = value.NewBool(false)
		resInst.Fields["error"] = value.NewString("invalid database handle")
		resInst.Fields["rows_affected"] = value.NewInt(0)
		resInst.Fields["last_insert_id"] = value.NewInt(0)
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})

	vm.DefineNative("sqlite_prepare", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		} // db, sql, stmt wrapper
		dbInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		sqlStr := args[1].String()
		stmtInst, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		stmtStructDef := stmtInst.Struct

		handle := int(dbInst.Fields["handle"].AsInt)

		vm.shared.DbLock.Lock()
		db, ok := vm.shared.DbHandles[handle]
		vm.shared.DbLock.Unlock()

		if ok {
			stmt, err := db.Prepare(sqlStr)
			if err == nil {
				vm.shared.DbLock.Lock()
				id := vm.shared.NextStmtID
				vm.shared.NextStmtID++
				vm.shared.StmtHandles[id] = stmt
				vm.shared.StmtParams[id] = make(map[int]interface{})
				vm.shared.DbLock.Unlock()

				inst := value.NewInstance(stmtStructDef).Obj.(*value.ObjInstance)
				inst.Fields["handle"] = value.NewInt(int64(id))
				return value.Value{Type: value.VAL_OBJ, Obj: inst}
			}
		}
		return value.NewNull()
	})

	bindFunc := func(args []value.Value, val interface{}) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		}
		stmtInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		idx := int(args[1].AsInt)

		handle := int(stmtInst.Fields["handle"].AsInt)

		vm.shared.DbLock.Lock()
		defer vm.shared.DbLock.Unlock()

		if _, ok := vm.shared.StmtHandles[handle]; ok {
			if vm.shared.StmtParams[handle] == nil {
				vm.shared.StmtParams[handle] = make(map[int]interface{})
			}
			vm.shared.StmtParams[handle][idx] = val
		}
		return value.NewNull()
	}

	vm.DefineNative("sqlite_bind_text", func(args []value.Value) value.Value {
		return bindFunc(args, args[2].String())
	})
	vm.DefineNative("sqlite_bind_float", func(args []value.Value) value.Value {
		return bindFunc(args, args[2].AsFloat)
	})
	vm.DefineNative("sqlite_bind_int", func(args []value.Value) value.Value {
		return bindFunc(args, args[2].AsInt)
	})

	vm.DefineNative("sqlite_step_exec", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		stmtInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resTmplInst, ok := args[1].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct := resTmplInst.Struct

		handle := int(stmtInst.Fields["handle"].AsInt)

		vm.shared.DbLock.Lock()
		stmt, ok := vm.shared.StmtHandles[handle]
		var params map[int]interface{}
		if ok {
			origParams := vm.shared.StmtParams[handle]
			params = make(map[int]interface{})
			for k, v := range origParams {
				params[k] = v
			}
		}
		vm.shared.DbLock.Unlock()

		if ok {
			// params := vm.stmtParams[handle] // Replaced by copy
			var maxIdx int
			for k := range params {
				if k > maxIdx {
					maxIdx = k
				}
			}
			argsList := make([]interface{}, maxIdx)
			for k, v := range params {
				if k > 0 && k <= maxIdx {
					argsList[k-1] = v
				}
			}
			result, err := stmt.Exec(argsList...)

			resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
			if err != nil {
				resInst.Fields["ok"] = value.NewBool(false)
				resInst.Fields["error"] = value.NewString(err.Error())
				resInst.Fields["rows_affected"] = value.NewInt(0)
				resInst.Fields["last_insert_id"] = value.NewInt(0)
			} else {
				rowsAffected, _ := result.RowsAffected()
				lastId, _ := result.LastInsertId()
				resInst.Fields["ok"] = value.NewBool(true)
				resInst.Fields["error"] = value.NewString("")
				resInst.Fields["rows_affected"] = value.NewInt(rowsAffected)
				resInst.Fields["last_insert_id"] = value.NewInt(lastId)
			}
			return value.Value{Type: value.VAL_OBJ, Obj: resInst}
		}

		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["ok"] = value.NewBool(false)
		resInst.Fields["error"] = value.NewString("invalid statement handle")
		resInst.Fields["rows_affected"] = value.NewInt(0)
		resInst.Fields["last_insert_id"] = value.NewInt(0)
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})

	vm.DefineNative("sqlite_reset", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		stmtInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		handle := int(stmtInst.Fields["handle"].AsInt)

		vm.shared.DbLock.Lock()
		defer vm.shared.DbLock.Unlock()

		if _, ok := vm.shared.StmtHandles[handle]; ok {
			vm.shared.StmtParams[handle] = make(map[int]interface{})
		}
		return value.NewNull()
	})

	vm.DefineNative("sqlite_finalize", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		stmtInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		handle := int(stmtInst.Fields["handle"].AsInt)

		vm.shared.DbLock.Lock()
		defer vm.shared.DbLock.Unlock()

		if stmt, ok := vm.shared.StmtHandles[handle]; ok {
			stmt.Close()
			delete(vm.shared.StmtHandles, handle)
			delete(vm.shared.StmtParams, handle)
		}
		return value.NewNull()
	})

	vm.DefineNative("sqlite_query", func(args []value.Value) value.Value {
		if len(args) < 4 {
			return value.NewNull()
		} // db, sql, tmplQueryResult, tmplRow

		dbInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		sqlStr := args[1].String()

		resTmplInst, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct := resTmplInst.Struct

		rowTmplInst, ok := args[3].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		rowStruct := rowTmplInst.Struct

		handle := int(dbInst.Fields["handle"].AsInt)

		vm.shared.DbLock.Lock()
		db, ok := vm.shared.DbHandles[handle]
		vm.shared.DbLock.Unlock()

		if ok {
			rows, err := db.Query(sqlStr)
			if err != nil {
				// Return QueryResult with ok=false and error message
				resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
				resInst.Fields["columns"] = value.NewArray(nil)
				resInst.Fields["rows"] = value.NewArray(nil)
				resInst.Fields["row_count"] = value.NewInt(0)
				resInst.Fields["ok"] = value.NewBool(false)
				resInst.Fields["error"] = value.NewString(err.Error())
				return value.Value{Type: value.VAL_OBJ, Obj: resInst}
			}
			defer rows.Close()

			cols, _ := rows.Columns()
			colVals := make([]value.Value, len(cols))
			for i, c := range cols {
				colVals[i] = value.NewString(c)
			}

			var rowInsts []value.Value

			for rows.Next() {
				// Scan to interface{}
				dest := make([]interface{}, len(cols))
				destPtrs := make([]interface{}, len(cols))
				for i := range dest {
					destPtrs[i] = &dest[i]
				}

				rows.Scan(destPtrs...)

				rowVals := make([]value.Value, len(cols))
				for i, v := range dest {
					// Convert Go type to Noxy value
					switch tv := v.(type) {
					case nil:
						rowVals[i] = value.NewNull()
					case int64:
						rowVals[i] = value.NewInt(tv)
					case float64:
						rowVals[i] = value.NewFloat(tv)
					case string:
						rowVals[i] = value.NewString(tv)
					case []byte:
						rowVals[i] = value.NewString(string(tv))
					default:
						rowVals[i] = value.NewString(fmt.Sprintf("%v", tv))
					}
				}

				// Create Row instance
				rowInst := value.NewInstance(rowStruct).Obj.(*value.ObjInstance)
				rowInst.Fields["values"] = value.NewArray(rowVals)
				rowInsts = append(rowInsts, value.Value{Type: value.VAL_OBJ, Obj: rowInst})
			}

			// Create QueryResult instance with ok=true
			resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
			resInst.Fields["columns"] = value.NewArray(colVals)
			resInst.Fields["rows"] = value.NewArray(rowInsts)
			resInst.Fields["row_count"] = value.NewInt(int64(len(rowInsts)))
			resInst.Fields["ok"] = value.NewBool(true)
			resInst.Fields["error"] = value.NewString("")

			return value.Value{Type: value.VAL_OBJ, Obj: resInst}
		}
		// DB handle not found - return error result
		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["columns"] = value.NewArray(nil)
		resInst.Fields["rows"] = value.NewArray(nil)
		resInst.Fields["row_count"] = value.NewInt(0)
		resInst.Fields["ok"] = value.NewBool(false)
		resInst.Fields["error"] = value.NewString("invalid database handle")
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})
}
