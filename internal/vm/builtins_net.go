package vm

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"time"

	"noxy-vm/internal/value"
)

type deadlineListener interface {
	net.Listener
	SetDeadline(time.Time) error
}

var networkDialTimeout = net.DialTimeout

var defaultNetworkPoller = networkPoller{
	platform: systemNetworkPlatform(),
	now:      time.Now,
	sleep:    time.Sleep,
}

func validateNetworkTimeout(milliseconds int64) (time.Duration, error) {
	if milliseconds <= 0 {
		return 0, fmt.Errorf("network timeout must be positive")
	}
	const maximum = int64(math.MaxInt64) / int64(time.Millisecond)
	if milliseconds > maximum {
		return 0, fmt.Errorf("network timeout is too large")
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func networkSocketDescriptor(socket value.Value) (int, error) {
	if socket.Type != value.VAL_OBJ {
		return 0, fmt.Errorf("invalid socket")
	}
	var descriptor value.Value
	var exists bool
	switch object := socket.Obj.(type) {
	case *value.ObjMap:
		if object == nil {
			return 0, fmt.Errorf("invalid socket")
		}
		descriptor, exists = object.Get("fd")
	case *value.ObjInstance:
		if object == nil {
			return 0, fmt.Errorf("invalid socket")
		}
		descriptor, exists = object.Get("fd")
	default:
		return 0, fmt.Errorf("invalid socket")
	}
	if !exists || descriptor.Type != value.VAL_INT {
		return 0, fmt.Errorf("invalid socket")
	}
	handle := int(descriptor.Int())
	if int64(handle) != descriptor.Int() {
		return 0, fmt.Errorf("invalid socket")
	}
	return handle, nil
}

func effectiveNetworkDeadline(now time.Time, timeout time.Duration) time.Time {
	if timeout > 0 {
		return now.Add(timeout)
	}
	return time.Time{}
}

func configureSocketTimeout(resource *SocketResource, timeout time.Duration) error {
	resource.deadlineMu.Lock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection == nil {
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return fmt.Errorf("invalid socket")
	}
	resource.deadlineGeneration++
	generation := resource.deadlineGeneration
	connection := resource.connection
	oldReadDeadline := resource.lastReadDeadline
	oldWriteDeadline := resource.lastWriteDeadline
	now := time.Now()
	readDeadline := effectiveNetworkDeadline(now, timeout)
	writeDeadline := effectiveNetworkDeadline(now, timeout)
	resource.stateMu.Unlock()

	readErr := connection.SetReadDeadline(readDeadline)
	resource.stateMu.Lock()
	if resource.closed || resource.deadlineGeneration != generation {
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return net.ErrClosed
	}
	if readErr == nil {
		resource.lastReadDeadline = readDeadline
	}
	resource.stateMu.Unlock()
	if readErr != nil {
		rollbackErr := connection.SetReadDeadline(oldReadDeadline)
		resource.stateMu.Lock()
		if resource.closed || resource.deadlineGeneration != generation {
			resource.stateMu.Unlock()
			resource.deadlineMu.Unlock()
			return errors.Join(readErr, rollbackErr, net.ErrClosed)
		}
		if rollbackErr == nil {
			resource.lastReadDeadline = oldReadDeadline
			resource.stateMu.Unlock()
			resource.deadlineMu.Unlock()
			return readErr
		}
		detached, _ := detachSocketLocked(resource)
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		detachErr := finishSocketDetach(detached)
		return errors.Join(readErr, rollbackErr, detachErr)
	}

	writeErr := connection.SetWriteDeadline(writeDeadline)
	resource.stateMu.Lock()
	if resource.closed || resource.deadlineGeneration != generation {
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return net.ErrClosed
	}
	if writeErr == nil {
		resource.lastWriteDeadline = writeDeadline
		resource.ioTimeout = timeout
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return nil
	}
	resource.stateMu.Unlock()
	readRollbackErr := connection.SetReadDeadline(oldReadDeadline)
	resource.stateMu.Lock()
	if resource.closed || resource.deadlineGeneration != generation {
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return errors.Join(writeErr, readRollbackErr, net.ErrClosed)
	}
	resource.stateMu.Unlock()
	writeRollbackErr := connection.SetWriteDeadline(oldWriteDeadline)
	resource.stateMu.Lock()
	if resource.closed || resource.deadlineGeneration != generation {
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return errors.Join(writeErr, readRollbackErr, writeRollbackErr, net.ErrClosed)
	}
	if readRollbackErr == nil && writeRollbackErr == nil {
		resource.lastReadDeadline = oldReadDeadline
		resource.lastWriteDeadline = oldWriteDeadline
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return writeErr
	}
	detached, _ := detachSocketLocked(resource)
	resource.stateMu.Unlock()
	resource.deadlineMu.Unlock()
	detachErr := finishSocketDetach(detached)
	return errors.Join(writeErr, readRollbackErr, writeRollbackErr, detachErr)
}

func configureListenerTimeout(resource *ListenerResource, timeout time.Duration) error {
	resource.deadlineMu.Lock()

	resource.stateMu.Lock()
	if resource.closed || resource.listener == nil {
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return fmt.Errorf("invalid socket")
	}
	resource.deadlineGeneration++
	generation := resource.deadlineGeneration
	listener := resource.listener
	oldDeadline := resource.lastAcceptDeadline
	deadline := effectiveNetworkDeadline(time.Now(), timeout)
	resource.stateMu.Unlock()

	applicationErr := listener.SetDeadline(deadline)
	resource.stateMu.Lock()
	if resource.closed || resource.deadlineGeneration != generation {
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return net.ErrClosed
	}
	if applicationErr == nil {
		resource.lastAcceptDeadline = deadline
		resource.ioTimeout = timeout
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return nil
	}
	resource.stateMu.Unlock()

	rollbackErr := listener.SetDeadline(oldDeadline)
	resource.stateMu.Lock()
	if resource.closed || resource.deadlineGeneration != generation {
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return errors.Join(applicationErr, rollbackErr, net.ErrClosed)
	}
	if rollbackErr == nil {
		resource.lastAcceptDeadline = oldDeadline
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		return applicationErr
	}
	detached, _ := detachListenerLocked(resource)
	resource.stateMu.Unlock()
	resource.deadlineMu.Unlock()
	detachErr := finishListenerDetach(detached)
	return errors.Join(applicationErr, rollbackErr, detachErr)
}

func configureNetworkTimeout(machine *VM, socket value.Value, timeout time.Duration) error {
	handle, err := networkSocketDescriptor(socket)
	if err != nil {
		return err
	}
	if listener, exists := machine.shared.Listeners.get(handle); exists {
		return configureListenerTimeout(listener, timeout)
	}
	if connection, exists := machine.shared.Sockets.get(handle); exists {
		return configureSocketTimeout(connection, timeout)
	}
	return fmt.Errorf("invalid socket")
}

func networkErrorMessage(err error) string {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return "operation timed out"
	}
	return err.Error()
}

func prepareSocketRead(resource *SocketResource) (net.Conn, error) {
	resource.deadlineMu.Lock()
	defer resource.deadlineMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection == nil {
		resource.stateMu.Unlock()
		return nil, fmt.Errorf("invalid socket")
	}
	resource.deadlineGeneration++
	generation := resource.deadlineGeneration
	connection := resource.connection
	deadline := effectiveNetworkDeadline(time.Now(), resource.ioTimeout)
	resource.stateMu.Unlock()

	if err := connection.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	if resource.closed || resource.deadlineGeneration != generation {
		return nil, net.ErrClosed
	}
	resource.lastReadDeadline = deadline
	return connection, nil
}

func prepareSocketWrite(resource *SocketResource) (net.Conn, error) {
	resource.deadlineMu.Lock()
	defer resource.deadlineMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection == nil {
		resource.stateMu.Unlock()
		return nil, fmt.Errorf("invalid socket")
	}
	resource.deadlineGeneration++
	generation := resource.deadlineGeneration
	connection := resource.connection
	deadline := effectiveNetworkDeadline(time.Now(), resource.ioTimeout)
	resource.stateMu.Unlock()

	if err := connection.SetWriteDeadline(deadline); err != nil {
		return nil, err
	}
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	if resource.closed || resource.deadlineGeneration != generation {
		return nil, net.ErrClosed
	}
	resource.lastWriteDeadline = deadline
	return connection, nil
}

func prepareListenerAccept(resource *ListenerResource) (deadlineListener, error) {
	resource.deadlineMu.Lock()
	defer resource.deadlineMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.listener == nil {
		resource.stateMu.Unlock()
		return nil, fmt.Errorf("invalid socket")
	}
	resource.deadlineGeneration++
	generation := resource.deadlineGeneration
	listener := resource.listener
	deadline := effectiveNetworkDeadline(time.Now(), resource.ioTimeout)
	resource.stateMu.Unlock()

	if err := listener.SetDeadline(deadline); err != nil {
		return nil, err
	}
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	if resource.closed || resource.deadlineGeneration != generation {
		return nil, net.ErrClosed
	}
	resource.lastAcceptDeadline = deadline
	return listener, nil
}

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

func acceptConnection(resource *ListenerResource) (net.Conn, error) {
	resource.acceptMu.Lock()
	defer resource.acceptMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.listener == nil {
		resource.stateMu.Unlock()
		return nil, net.ErrClosed
	}
	resource.stateMu.Unlock()

	listener, err := prepareListenerAccept(resource)
	if err != nil {
		return nil, err
	}
	connection, err := listener.Accept()
	if err != nil {
		return nil, err
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		_ = connection.Close()
		return nil, err
	}
	resource.stateMu.Lock()
	closed := resource.closed || resource.listener != listener
	resource.stateMu.Unlock()
	if closed {
		_ = connection.Close()
		return nil, net.ErrClosed
	}
	return connection, nil
}

func receiveSocket(resource *SocketResource, size int) value.Value {
	resource.readMu.Lock()
	defer resource.readMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection == nil {
		resource.stateMu.Unlock()
		return netResult(false, "", 0, "invalid socket")
	}
	if size <= 0 {
		resource.stateMu.Unlock()
		return netResult(true, "", 0, "")
	}
	resource.stateMu.Unlock()

	connection, deadlineErr := prepareSocketRead(resource)
	if deadlineErr != nil {
		return netResult(false, "", 0, networkErrorMessage(deadlineErr))
	}

	resource.stateMu.Lock()
	if resource.closed || resource.connection != connection {
		resource.stateMu.Unlock()
		return netResult(false, "", 0, "invalid socket")
	}
	resource.stateMu.Unlock()

	buffer := make([]byte, size)
	read, readErr := connection.Read(buffer)
	if readErr != nil && read == 0 {
		if readErr == io.EOF {
			return netResult(true, "", 0, "")
		}
		return netResult(false, "", 0, networkErrorMessage(readErr))
	}
	return netResult(true, string(buffer[:read]), read, "")
}

func sendSocket(resource *SocketResource, data string) value.Value {
	resource.writeMu.Lock()
	defer resource.writeMu.Unlock()

	connection, deadlineErr := prepareSocketWrite(resource)
	if deadlineErr != nil {
		return netResult(false, "", 0, networkErrorMessage(deadlineErr))
	}

	written, writeErr := connection.Write([]byte(data))
	if writeErr != nil {
		return netResult(false, "", written, networkErrorMessage(writeErr))
	}
	return netResult(true, "", written, "")
}

func closeListener(resource *ListenerResource) {
	resource.stateMu.Lock()
	detached, ok := detachListenerLocked(resource)
	resource.stateMu.Unlock()
	if ok {
		_ = finishListenerDetach(detached)
	}
}

func closeSocket(resource *SocketResource) {
	resource.stateMu.Lock()
	detached, ok := detachSocketLocked(resource)
	resource.stateMu.Unlock()
	if ok {
		_ = finishSocketDetach(detached)
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
		port := int(args[1].Int())
		listener, listenErr := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if listenErr != nil {
			return socketValue(-1, host, port, false), nil
		}
		configuredListener, ok := listener.(deadlineListener)
		if !ok {
			_ = listener.Close()
			return socketValue(-1, host, port, false), nil
		}
		boundPort := port
		if address, addressOK := listener.Addr().(*net.TCPAddr); addressOK {
			boundPort = address.Port
		}
		handle := machine.shared.Listeners.add(&ListenerResource{listener: configuredListener})
		return socketValue(handle, host, boundPort, true), nil
	})

	vm.DefineContextualNative("net_accept", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 1 {
			return value.NewNull(), fmt.Errorf("net_accept expects exactly 1 argument")
		}
		listenerFD, descriptorErr := networkSocketDescriptor(args[0])
		if descriptorErr != nil {
			return value.NewNull(), descriptorErr
		}
		resource, exists := machine.shared.Listeners.get(listenerFD)
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
		port := int(args[1].Int())
		connection, connectErr := networkDialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 5*time.Second)
		if connectErr != nil {
			return socketValue(-1, host, port, false), nil
		}
		if err := connection.SetDeadline(time.Time{}); err != nil {
			_ = connection.Close()
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
		if len(args) != 2 {
			return value.NewNull(), fmt.Errorf("net_recv expects exactly 2 arguments")
		}
		handle, descriptorErr := networkSocketDescriptor(args[0])
		if descriptorErr != nil {
			return value.NewNull(), descriptorErr
		}
		resource, exists := machine.shared.Sockets.get(handle)
		if !exists {
			return netResult(false, "", 0, "invalid socket"), nil
		}
		return receiveSocket(resource, int(args[1].Int())), nil
	})

	vm.DefineContextualNative("net_send", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 2 {
			return value.NewNull(), fmt.Errorf("net_send expects exactly 2 arguments")
		}
		handle, descriptorErr := networkSocketDescriptor(args[0])
		if descriptorErr != nil {
			return value.NewNull(), descriptorErr
		}
		data := args[1].String()
		if args[1].Type == value.VAL_BYTES {
			data = args[1].Obj.(string)
		}
		resource, exists := machine.shared.Sockets.get(handle)
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
			fd = int(args[0].Int())
		} else if args[0].Type == value.VAL_OBJ {
			if handle, descriptorErr := networkSocketDescriptor(args[0]); descriptorErr == nil {
				fd = handle
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
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 2 || args[1].Type != value.VAL_BOOL || !args[1].Bool() {
			return value.NewNull(), nil
		}
		if err := configureNetworkTimeout(machine, args[0], 0); err != nil {
			return value.NewNull(), err
		}
		return value.NewNull(), nil
	})

	vm.DefineContextualNative("net_settimeout", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 2 {
			return value.NewNull(), fmt.Errorf("net_settimeout expects exactly 2 arguments")
		}
		if args[1].Type != value.VAL_INT {
			return value.NewNull(), fmt.Errorf("network timeout must be an int")
		}
		timeout, err := validateNetworkTimeout(args[1].Int())
		if err != nil {
			return value.NewNull(), err
		}
		if err := configureNetworkTimeout(machine, args[0], timeout); err != nil {
			return value.NewNull(), err
		}
		return value.NewNull(), nil
	})

	vm.DefineNative("net_socket_set", func([]value.Value) value.Value {
		return fixedNetworkSet(nil)
	})

	vm.DefineContextualNative("net_select", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		sets, timeout, err := validateNetworkPollArguments(args)
		if err != nil {
			return value.NewNull(), err
		}
		return defaultNetworkPoller.Poll(machine.shared, sets, timeout)
	})
}
