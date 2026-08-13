package vm

import (
	"fmt"
	"io"
	"net"
	"time"

	"noxy-vm/internal/value"
)

func (vm *VM) defineNetworkBuiltins() {
	// Net Native Functions
	vm.DefineNative("net_listen", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		host := args[0].String()
		port := int(args[1].AsInt)
		addr := fmt.Sprintf("%s:%d", host, port)

		listener, err := net.Listen("tcp", addr)
		if err != nil {
			// Return Socket with open=false
			socketFields := map[string]value.Value{
				"fd":   value.NewInt(-1),
				"addr": value.NewString(host),
				"port": value.NewInt(int64(port)),
				"open": value.NewBool(false),
			}
			return value.NewMapWithData(socketFields)
		}

		vm.shared.NetLock.Lock()
		id := vm.shared.NextNetID
		vm.shared.NextNetID++
		vm.shared.NetListeners[id] = listener
		vm.shared.NetLock.Unlock()

		socketFields := map[string]value.Value{
			"fd":   value.NewInt(int64(id)),
			"addr": value.NewString(host),
			"port": value.NewInt(int64(port)),
			"open": value.NewBool(true),
		}
		return value.NewMapWithData(socketFields)
	})

	vm.DefineNative("net_accept", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		sockMap, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull()
		}
		fdVal, exists := sockMap.Get("fd")
		if !exists {
			return value.NewNull()
		}
		fd := int(fdVal.AsInt)

		vm.shared.NetLock.Lock()
		listener, ok := vm.shared.NetListeners[fd]
		vm.shared.NetLock.Unlock()

		if !ok {
			socketFields := map[string]value.Value{
				"fd":   value.NewInt(-1),
				"addr": value.NewString(""),
				"port": value.NewInt(0),
				"open": value.NewBool(false),
			}
			return value.NewMapWithData(socketFields)
		}

		// Check buffered connection from select
		var conn net.Conn
		var err error

		if bufferedConn, ok := vm.netBufferedConns[fd]; ok {
			conn = bufferedConn
			delete(vm.netBufferedConns, fd)
		} else {
			// Accept blocks. Lock is released above.
			conn, err = listener.Accept()
		}

		if err != nil {
			socketFields := map[string]value.Value{
				"fd":   value.NewInt(-1),
				"addr": value.NewString(""),
				"port": value.NewInt(0),
				"open": value.NewBool(false),
			}
			return value.NewMapWithData(socketFields)
		}

		vm.shared.NetLock.Lock()
		id := vm.shared.NextNetID
		vm.shared.NextNetID++
		vm.shared.NetConns[id] = conn
		vm.shared.NetLock.Unlock()

		remoteAddr := conn.RemoteAddr().String()
		socketFields := map[string]value.Value{
			"fd":   value.NewInt(int64(id)),
			"addr": value.NewString(remoteAddr),
			"port": value.NewInt(0),
			"open": value.NewBool(true),
		}
		return value.NewMapWithData(socketFields)
	})

	vm.DefineNative("net_connect", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		host := args[0].String()
		port := int(args[1].AsInt)
		addr := fmt.Sprintf("%s:%d", host, port)

		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			socketFields := map[string]value.Value{
				"fd":   value.NewInt(-1),
				"addr": value.NewString(host),
				"port": value.NewInt(int64(port)),
				"open": value.NewBool(false),
			}
			return value.NewMapWithData(socketFields)
		}

		vm.shared.NetLock.Lock()
		id := vm.shared.NextNetID
		vm.shared.NextNetID++
		vm.shared.NetConns[id] = conn
		vm.shared.NetLock.Unlock()

		socketFields := map[string]value.Value{
			"fd":   value.NewInt(int64(id)),
			"addr": value.NewString(host),
			"port": value.NewInt(int64(port)),
			"open": value.NewBool(true),
		}
		return value.NewMapWithData(socketFields)
	})

	vm.DefineNative("net_recv", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		sockMap, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull()
		}
		fdVal, _ := sockMap.Get("fd")
		fd := int(fdVal.AsInt)
		size := int(args[1].AsInt)

		vm.shared.NetLock.Lock()
		conn, ok := vm.shared.NetConns[fd]
		vm.shared.NetLock.Unlock()

		if !ok {
			resultFields := map[string]value.Value{
				"ok":    value.NewBool(false),
				"data":  value.NewBytes(""),
				"count": value.NewInt(0),
				"error": value.NewString("invalid socket"),
			}
			return value.NewMapWithData(resultFields)
		}

		var n int
		buf := make([]byte, size)

		// Check buffered data from select
		if buffered, ok := vm.netBufferedData[fd]; ok {
			// Copy buffered data
			copy(buf, buffered)
			n = len(buffered)
			delete(vm.netBufferedData, fd)
		}

		// Try to read more if space available
		if n < size {
			// Blocking read (no deadline)
			n2, err2 := conn.Read(buf[n:])
			if n2 > 0 {
				n += n2
			}

			// Ignore timeout errors if we have at least some data
			if err2 != nil {
				if n == 0 {
					// Only return error if we really got nothing
					if err2 != nil && n2 == 0 {
						if err2 == io.EOF {
							// Return ok=true, count=0 for EOF
							resultFields := map[string]value.Value{
								"ok":    value.NewBool(true),
								"data":  value.NewBytes(""),
								"count": value.NewInt(0),
								"error": value.NewString(""),
							}
							return value.NewMapWithData(resultFields)
						}
						resultFields := map[string]value.Value{
							"ok":    value.NewBool(false),
							"data":  value.NewBytes(""),
							"count": value.NewInt(0),
							"error": value.NewString(err2.Error()),
						}
						return value.NewMapWithData(resultFields)
					}
				}
			}
		}

		resultFields := map[string]value.Value{
			"ok":    value.NewBool(true),
			"data":  value.NewBytes(string(buf[:n])),
			"count": value.NewInt(int64(n)),
			"error": value.NewString(""),
		}
		return value.NewMapWithData(resultFields)
	})

	vm.DefineNative("net_send", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		sockMap, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			fmt.Printf("DEBUG: net_send args[0] not map: %T %v\n", args[0].Obj, args[0].Obj)
			return value.NewNull()
		}
		fdVal, _ := sockMap.Get("fd")
		fd := int(fdVal.AsInt)
		var data string
		if args[1].Type == value.VAL_BYTES {
			data = args[1].Obj.(string)
		} else {
			data = args[1].String()
		}

		vm.shared.NetLock.Lock()
		conn, ok := vm.shared.NetConns[fd]
		vm.shared.NetLock.Unlock()

		if !ok {
			resultFields := map[string]value.Value{
				"ok":    value.NewBool(false),
				"data":  value.NewBytes(""),
				"count": value.NewInt(0),
				"error": value.NewString("invalid socket"),
			}
			return value.NewMapWithData(resultFields)
		}

		n, err := conn.Write([]byte(data))
		if err != nil {
			resultFields := map[string]value.Value{
				"ok":    value.NewBool(false),
				"data":  value.NewBytes(""),
				"count": value.NewInt(0),
				"error": value.NewString(err.Error()),
			}
			return value.NewMapWithData(resultFields)
		}

		resultFields := map[string]value.Value{
			"ok":    value.NewBool(true),
			"data":  value.NewBytes(""),
			"count": value.NewInt(int64(n)),
			"error": value.NewString(""),
		}
		return value.NewMapWithData(resultFields)
	})

	vm.DefineNative("net_close", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}

		var fd int
		// Check if arg is int (new style) or map (old style compatibility if needed, but we changed net.nx)
		if args[0].Type == value.VAL_INT {
			fd = int(args[0].AsInt)
		} else if args[0].Type == value.VAL_OBJ {
			// Fallback for old calls? Or just error.
			if sockMap, ok := args[0].Obj.(*value.ObjMap); ok {
				if fdVal, found := sockMap.Get("fd"); found {
					fd = int(fdVal.AsInt)
				}
			}
		} else {
			return value.NewNull()
		}

		vm.shared.NetLock.Lock()
		defer vm.shared.NetLock.Unlock()

		// Try closing as listener
		if listener, ok := vm.shared.NetListeners[fd]; ok {
			listener.Close()
			delete(vm.shared.NetListeners, fd)
			return value.NewNull()
		}

		// Try closing as connection
		if conn, ok := vm.shared.NetConns[fd]; ok {
			conn.Close()
			delete(vm.shared.NetConns, fd)
		}

		return value.NewNull()
	})

	vm.DefineNative("net_setblocking", func(args []value.Value) value.Value {
		// For TCP in Go, blocking is handled at a different level
		// This is a no-op for now, as Go handles timeouts via SetDeadline
		return value.NewNull()
	})

	vm.DefineNative("net_select", func(args []value.Value) value.Value {
		// args: read, write (ignored), err (ignored), timeout
		if len(args) < 4 {
			return value.NewNull() // Or error map
		}

		timeoutMs := int(args[3].AsInt)
		// Minimum 1ms to allow polling
		if timeoutMs < 1 {
			timeoutMs = 1
		}

		// Prepare Result Data
		readyRead := make([]value.Value, 0)

		// Parse Read Set
		readArrVal := args[0]
		if readArrVal.Type == value.VAL_OBJ {
			if arr, ok := readArrVal.Obj.(*value.ObjArray); ok {
				for _, el := range arr.Elements {
					if el.Type == value.VAL_OBJ { // Check if socket (Map or Instance)
						// Extract FD
						var fd int64 = -1

						if m, ok := el.Obj.(*value.ObjMap); ok {
							if f, ok := m.Get("fd"); ok {
								fd = f.AsInt
							}
						} else if inst, ok := el.Obj.(*value.ObjInstance); ok {
							if f, ok := inst.Fields["fd"]; ok {
								fd = f.AsInt
							}
						}

						if fd != -1 {
							isReady := false
							id := int(fd)

							// 1. Check buffers first
							if _, ok := vm.netBufferedConns[id]; ok {
								isReady = true
							} else if _, ok := vm.netBufferedData[id]; ok {
								isReady = true
							} else {
								// 2. Poll
								vm.shared.NetLock.Lock()
								l, isListener := vm.shared.NetListeners[id]
								c, isConn := vm.shared.NetConns[id]
								vm.shared.NetLock.Unlock()

								if isListener {
									if tcpL, ok := l.(*net.TCPListener); ok {
										tcpL.SetDeadline(time.Now().Add(time.Millisecond * time.Duration(timeoutMs)))
										conn, err := l.Accept()
										if err == nil {
											isReady = true
											vm.netBufferedConns[id] = conn
										}
										// Reset deadline?
										tcpL.SetDeadline(time.Time{})
									}
								} else if isConn {
									conn := c
									conn.SetReadDeadline(time.Now().Add(time.Millisecond * time.Duration(timeoutMs)))
									buf := make([]byte, 1) // Peek 1 byte
									n, err := conn.Read(buf)
									if err == nil && n > 0 {
										isReady = true
										// Buffer the data
										vm.netBufferedData[id] = buf[:n]
									}
									// Reset deadline
									conn.SetReadDeadline(time.Time{})
								}
							}

							if isReady {
								readyRead = append(readyRead, el)
							}
						}
					}
				}
			}
		}

		// Construct SelectResult Map
		// struct SelectResult { read: Socket[64], read_count: int, ... }

		// Fill read array up to 64
		resReadArr := make([]value.Value, 64)
		for i := 0; i < 64; i++ {
			if i < len(readyRead) {
				resReadArr[i] = readyRead[i]
			} else {
				resReadArr[i] = value.NewNull()
			}
		}

		// Empties for others
		emptyArr := make([]value.Value, 64)
		for i := 0; i < 64; i++ {
			emptyArr[i] = value.NewNull()
		}

		resFields := map[string]value.Value{
			"read":        value.NewArray(resReadArr),
			"read_count":  value.NewInt(int64(len(readyRead))),
			"write":       value.NewArray(emptyArr),
			"write_count": value.NewInt(0),
			"error":       value.NewArray(emptyArr),
			"error_count": value.NewInt(0),
		}
		return value.NewMapWithData(resFields)
	})
}
