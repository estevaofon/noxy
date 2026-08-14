package vm

import (
	"fmt"
	"io"
	"net"
	"time"

	"noxy-vm/internal/value"
)

func socketValue(fd int, address string, port int, open bool) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"fd":   value.NewInt(int64(fd)),
		"addr": value.NewString(address),
		"port": value.NewInt(int64(port)),
		"open": value.NewBool(open),
	})
}

func netResult(ok bool, data string, count int, message string) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":    value.NewBool(ok),
		"data":  value.NewBytes(data),
		"count": value.NewInt(int64(count)),
		"error": value.NewString(message),
	})
}

func selectResult(readyRead []value.Value) value.Value {
	read := make([]value.Value, 64)
	for index := range read {
		if index < len(readyRead) {
			read[index] = readyRead[index]
		} else {
			read[index] = value.NewNull()
		}
	}
	empty := make([]value.Value, 64)
	for index := range empty {
		empty[index] = value.NewNull()
	}
	return value.NewMapWithData(map[string]value.Value{
		"read":        value.NewArray(read),
		"read_count":  value.NewInt(int64(len(readyRead))),
		"write":       value.NewArray(empty),
		"write_count": value.NewInt(0),
		"error":       value.NewArray(empty),
		"error_count": value.NewInt(0),
	})
}

func acceptConnection(resource *ListenerResource) (net.Conn, error) {
	resource.acceptMu.Lock()
	defer resource.acceptMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.listener == nil {
		resource.stateMu.Unlock()
		return nil, net.ErrClosed
	}
	if resource.bufferedAccept != nil {
		connection := resource.bufferedAccept
		resource.bufferedAccept = nil
		resource.stateMu.Unlock()
		return connection, nil
	}
	listener := resource.listener
	resource.stateMu.Unlock()
	return listener.Accept()
}

func receiveSocket(resource *SocketResource, size int) value.Value {
	resource.readMu.Lock()
	defer resource.readMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection == nil {
		resource.stateMu.Unlock()
		return netResult(false, "", 0, "invalid socket")
	}
	connection := resource.connection
	buffered := resource.bufferedRead
	resource.bufferedRead = nil
	resource.stateMu.Unlock()

	buffer := make([]byte, size)
	read := 0
	if len(buffered) != 0 {
		copy(buffer, buffered)
		read = len(buffered)
	}
	if read < size {
		additional, readErr := connection.Read(buffer[read:])
		if additional > 0 {
			read += additional
		}
		if readErr != nil && read == 0 && additional == 0 {
			if readErr == io.EOF {
				return netResult(true, "", 0, "")
			}
			return netResult(false, "", 0, readErr.Error())
		}
	}
	return netResult(true, string(buffer[:read]), read, "")
}

func sendSocket(resource *SocketResource, data string) value.Value {
	resource.writeMu.Lock()
	defer resource.writeMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection == nil {
		resource.stateMu.Unlock()
		return netResult(false, "", 0, "invalid socket")
	}
	connection := resource.connection
	resource.stateMu.Unlock()

	written, writeErr := connection.Write([]byte(data))
	if writeErr != nil {
		return netResult(false, "", 0, writeErr.Error())
	}
	return netResult(true, "", written, "")
}

func selectListener(resource *ListenerResource, timeout time.Duration) bool {
	resource.acceptMu.Lock()
	defer resource.acceptMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.listener == nil {
		resource.stateMu.Unlock()
		return false
	}
	if resource.bufferedAccept != nil {
		resource.stateMu.Unlock()
		return true
	}
	listener := resource.listener
	resource.stateMu.Unlock()

	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		return false
	}
	_ = tcpListener.SetDeadline(time.Now().Add(timeout))
	connection, acceptErr := listener.Accept()
	_ = tcpListener.SetDeadline(time.Time{})
	if acceptErr != nil {
		return false
	}

	resource.stateMu.Lock()
	if resource.closed {
		resource.stateMu.Unlock()
		_ = connection.Close()
		return false
	}
	resource.bufferedAccept = connection
	resource.stateMu.Unlock()
	return true
}

func selectSocket(resource *SocketResource, timeout time.Duration) bool {
	resource.readMu.Lock()
	defer resource.readMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection == nil {
		resource.stateMu.Unlock()
		return false
	}
	if len(resource.bufferedRead) != 0 {
		resource.stateMu.Unlock()
		return true
	}
	connection := resource.connection
	resource.stateMu.Unlock()

	_ = connection.SetReadDeadline(time.Now().Add(timeout))
	buffer := make([]byte, 1)
	read, readErr := connection.Read(buffer)
	_ = connection.SetReadDeadline(time.Time{})
	if readErr != nil || read == 0 {
		return false
	}

	resource.stateMu.Lock()
	if resource.closed {
		resource.stateMu.Unlock()
		return false
	}
	resource.bufferedRead = buffer[:read]
	resource.stateMu.Unlock()
	return true
}

func closeListener(resource *ListenerResource) {
	resource.stateMu.Lock()
	if resource.closed {
		resource.stateMu.Unlock()
		return
	}
	resource.closed = true
	listener := resource.listener
	buffered := resource.bufferedAccept
	resource.bufferedAccept = nil
	resource.stateMu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	if buffered != nil {
		_ = buffered.Close()
	}
}

func closeSocket(resource *SocketResource) {
	resource.stateMu.Lock()
	if resource.closed {
		resource.stateMu.Unlock()
		return
	}
	resource.closed = true
	connection := resource.connection
	resource.bufferedRead = nil
	resource.stateMu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
}

func (vm *VM) defineNetworkBuiltins() {
	vm.DefineContextualNative("net_listen", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		host := args[0].String()
		port := int(args[1].AsInt)
		listener, listenErr := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if listenErr != nil {
			return socketValue(-1, host, port, false), nil
		}
		handle := machine.shared.Listeners.add(&ListenerResource{listener: listener})
		return socketValue(handle, host, port, true), nil
	})

	vm.DefineContextualNative("net_accept", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), nil
		}
		socket, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull(), nil
		}
		fdValue, exists := socket.Get("fd")
		if !exists {
			return value.NewNull(), nil
		}
		resource, exists := machine.shared.Listeners.get(int(fdValue.AsInt))
		if !exists {
			return socketValue(-1, "", 0, false), nil
		}
		connection, acceptErr := acceptConnection(resource)
		if acceptErr != nil {
			return socketValue(-1, "", 0, false), nil
		}
		handle := machine.shared.Sockets.add(&SocketResource{connection: connection})
		return socketValue(handle, connection.RemoteAddr().String(), 0, true), nil
	})

	vm.DefineContextualNative("net_connect", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		host := args[0].String()
		port := int(args[1].AsInt)
		connection, connectErr := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 5*time.Second)
		if connectErr != nil {
			return socketValue(-1, host, port, false), nil
		}
		handle := machine.shared.Sockets.add(&SocketResource{connection: connection})
		return socketValue(handle, host, port, true), nil
	})

	vm.DefineContextualNative("net_recv", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		socket, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull(), nil
		}
		fdValue, _ := socket.Get("fd")
		resource, exists := machine.shared.Sockets.get(int(fdValue.AsInt))
		if !exists {
			return netResult(false, "", 0, "invalid socket"), nil
		}
		return receiveSocket(resource, int(args[1].AsInt)), nil
	})

	vm.DefineContextualNative("net_send", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		socket, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			fmt.Printf("DEBUG: net_send args[0] not map: %T %v\n", args[0].Obj, args[0].Obj)
			return value.NewNull(), nil
		}
		fdValue, _ := socket.Get("fd")
		data := args[1].String()
		if args[1].Type == value.VAL_BYTES {
			data = args[1].Obj.(string)
		}
		resource, exists := machine.shared.Sockets.get(int(fdValue.AsInt))
		if !exists {
			return netResult(false, "", 0, "invalid socket"), nil
		}
		return sendSocket(resource, data), nil
	})

	vm.DefineContextualNative("net_close", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), nil
		}
		fd := 0
		if args[0].Type == value.VAL_INT {
			fd = int(args[0].AsInt)
		} else if args[0].Type == value.VAL_OBJ {
			if socket, ok := args[0].Obj.(*value.ObjMap); ok {
				if fdValue, found := socket.Get("fd"); found {
					fd = int(fdValue.AsInt)
				}
			}
		} else {
			return value.NewNull(), nil
		}
		if resource, exists := machine.shared.Listeners.remove(fd); exists {
			closeListener(resource)
			return value.NewNull(), nil
		}
		if resource, exists := machine.shared.Sockets.remove(fd); exists {
			closeSocket(resource)
		}
		return value.NewNull(), nil
	})

	vm.DefineContextualNative("net_setblocking", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		if _, err := nativeVM(context); err != nil {
			return value.NewNull(), err
		}
		return value.NewNull(), nil
	})

	vm.DefineContextualNative("net_select", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 4 {
			return value.NewNull(), nil
		}
		timeoutMilliseconds := int(args[3].AsInt)
		if timeoutMilliseconds < 1 {
			timeoutMilliseconds = 1
		}
		timeout := time.Millisecond * time.Duration(timeoutMilliseconds)
		readyRead := make([]value.Value, 0)
		if array, ok := args[0].Obj.(*value.ObjArray); args[0].Type == value.VAL_OBJ && ok {
			for _, candidate := range array.Elements {
				if candidate.Type != value.VAL_OBJ {
					continue
				}
				handle := int64(-1)
				if socket, ok := candidate.Obj.(*value.ObjMap); ok {
					if fdValue, exists := socket.Get("fd"); exists {
						handle = fdValue.AsInt
					}
				} else if instance, ok := candidate.Obj.(*value.ObjInstance); ok {
					if fdValue, exists := instance.Fields["fd"]; exists {
						handle = fdValue.AsInt
					}
				}
				if handle == -1 {
					continue
				}
				ready := false
				if listener, exists := machine.shared.Listeners.get(int(handle)); exists {
					ready = selectListener(listener, timeout)
				} else if socket, exists := machine.shared.Sockets.get(int(handle)); exists {
					ready = selectSocket(socket, timeout)
				}
				if ready {
					readyRead = append(readyRead, candidate)
				}
			}
		}
		return selectResult(readyRead), nil
	})
}
