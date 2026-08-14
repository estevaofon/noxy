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
		descriptor, exists = object.Fields["fd"]
	default:
		return 0, fmt.Errorf("invalid socket")
	}
	if !exists || descriptor.Type != value.VAL_INT {
		return 0, fmt.Errorf("invalid socket")
	}
	handle := int(descriptor.AsInt)
	if int64(handle) != descriptor.AsInt {
		return 0, fmt.Errorf("invalid socket")
	}
	return handle, nil
}

func effectiveNetworkDeadline(now time.Time, timeout time.Duration, probeBound time.Time) time.Time {
	deadline := time.Time{}
	if timeout > 0 {
		deadline = now.Add(timeout)
	}
	if !probeBound.IsZero() && (deadline.IsZero() || probeBound.Before(deadline)) {
		return probeBound
	}
	return deadline
}

func networkProbeDeadline(started time.Time, selectTimeout time.Duration, ioTimeout time.Duration) time.Time {
	return effectiveNetworkDeadline(started, ioTimeout, started.Add(selectTimeout))
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
	readDeadline := effectiveNetworkDeadline(now, timeout, resource.readProbeDeadline)
	writeDeadline := effectiveNetworkDeadline(now, timeout, time.Time{})
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
		resource.closed = true
		resource.deadlineGeneration++
		resource.connection = nil
		resource.bufferedRead = nil
		resource.readProbeDeadline = time.Time{}
		resource.stateMu.Unlock()
		resource.deadlineMu.Unlock()
		closeErr := connection.Close()
		return errors.Join(readErr, rollbackErr, closeErr)
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
	resource.closed = true
	resource.deadlineGeneration++
	resource.connection = nil
	resource.bufferedRead = nil
	resource.readProbeDeadline = time.Time{}
	resource.stateMu.Unlock()
	resource.deadlineMu.Unlock()
	closeErr := connection.Close()
	return errors.Join(writeErr, readRollbackErr, writeRollbackErr, closeErr)
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
	deadline := effectiveNetworkDeadline(time.Now(), timeout, resource.acceptProbeDeadline)
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
	resource.closed = true
	resource.deadlineGeneration++
	resource.listener = nil
	buffered := resource.bufferedAccept
	resource.bufferedAccept = nil
	resource.acceptProbeDeadline = time.Time{}
	resource.stateMu.Unlock()
	resource.deadlineMu.Unlock()
	listenerCloseErr := listener.Close()
	var bufferedCloseErr error
	if buffered != nil {
		bufferedCloseErr = buffered.Close()
	}
	return errors.Join(applicationErr, rollbackErr, listenerCloseErr, bufferedCloseErr)
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
	deadline := effectiveNetworkDeadline(time.Now(), resource.ioTimeout, resource.readProbeDeadline)
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
	deadline := effectiveNetworkDeadline(time.Now(), resource.ioTimeout, time.Time{})
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
	deadline := effectiveNetworkDeadline(time.Now(), resource.ioTimeout, resource.acceptProbeDeadline)
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
		resource.stateMu.Unlock()
		clearErr := connection.SetDeadline(time.Time{})

		resource.stateMu.Lock()
		if resource.closed || resource.bufferedAccept != connection {
			resource.stateMu.Unlock()
			return nil, net.ErrClosed
		}
		resource.bufferedAccept = nil
		resource.stateMu.Unlock()
		if clearErr != nil {
			_ = connection.Close()
			return nil, clearErr
		}
		return connection, nil
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
	if len(resource.bufferedRead) >= size {
		data := append([]byte(nil), resource.bufferedRead[:size]...)
		resource.bufferedRead = resource.bufferedRead[size:]
		resource.stateMu.Unlock()
		return netResult(true, string(data), len(data), "")
	}
	resource.stateMu.Unlock()

	connection, deadlineErr := prepareSocketRead(resource)
	if deadlineErr != nil {
		resource.stateMu.Lock()
		buffered := append([]byte(nil), resource.bufferedRead...)
		resource.bufferedRead = nil
		resource.stateMu.Unlock()
		if len(buffered) != 0 {
			return netResult(true, string(buffered), len(buffered), "")
		}
		return netResult(false, "", 0, networkErrorMessage(deadlineErr))
	}

	resource.stateMu.Lock()
	if resource.closed || resource.connection != connection {
		resource.stateMu.Unlock()
		return netResult(false, "", 0, "invalid socket")
	}
	buffered := append([]byte(nil), resource.bufferedRead...)
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
		if readErr != nil && read == 0 {
			if readErr == io.EOF {
				return netResult(true, "", 0, "")
			}
			return netResult(false, "", 0, networkErrorMessage(readErr))
		}
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

func beginListenerProbe(resource *ListenerResource, timeout time.Duration) (deadlineListener, bool, error) {
	resource.deadlineMu.Lock()
	defer resource.deadlineMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.listener == nil {
		resource.stateMu.Unlock()
		return nil, true, nil
	}
	resource.deadlineGeneration++
	generation := resource.deadlineGeneration
	listener := resource.listener
	started := time.Now()
	probeBound := started.Add(timeout)
	resource.acceptProbeDeadline = probeBound
	deadline := networkProbeDeadline(started, timeout, resource.ioTimeout)
	resource.stateMu.Unlock()

	setterErr := listener.SetDeadline(deadline)
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	if resource.closed || resource.deadlineGeneration != generation {
		return nil, true, nil
	}
	if setterErr != nil {
		resource.acceptProbeDeadline = time.Time{}
		return nil, false, setterErr
	}
	resource.lastAcceptDeadline = deadline
	return listener, false, nil
}

func finishListenerProbe(resource *ListenerResource, listener deadlineListener, connection net.Conn) (bool, error) {
	resource.deadlineMu.Lock()
	defer resource.deadlineMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.listener != listener {
		resource.stateMu.Unlock()
		if connection != nil {
			_ = connection.Close()
		}
		return true, nil
	}
	if connection != nil {
		resource.bufferedAccept = connection
	}
	resource.acceptProbeDeadline = time.Time{}
	resource.deadlineGeneration++
	generation := resource.deadlineGeneration
	deadline := effectiveNetworkDeadline(time.Now(), resource.ioTimeout, time.Time{})
	resource.stateMu.Unlock()

	setterErr := listener.SetDeadline(deadline)
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	if resource.closed || resource.deadlineGeneration != generation {
		return true, nil
	}
	if setterErr != nil {
		return false, setterErr
	}
	resource.lastAcceptDeadline = deadline
	return false, nil
}

func selectListener(resource *ListenerResource, timeout time.Duration) (bool, error) {
	resource.acceptMu.Lock()
	defer resource.acceptMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.listener == nil {
		resource.stateMu.Unlock()
		return false, nil
	}
	if resource.bufferedAccept != nil {
		resource.stateMu.Unlock()
		return true, nil
	}
	resource.stateMu.Unlock()

	listener, closed, err := beginListenerProbe(resource, timeout)
	if err != nil {
		return false, err
	}
	if closed {
		return false, nil
	}
	connection, acceptErr := listener.Accept()
	ordinarySuccess := acceptErr == nil && connection != nil
	closed, restoreErr := finishListenerProbe(resource, listener, func() net.Conn {
		if ordinarySuccess {
			return connection
		}
		return nil
	}())
	if restoreErr != nil {
		return false, restoreErr
	}
	if closed || !ordinarySuccess {
		return false, nil
	}
	return true, nil
}

func beginSocketProbe(resource *SocketResource, timeout time.Duration) (net.Conn, bool, error) {
	resource.deadlineMu.Lock()
	defer resource.deadlineMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection == nil {
		resource.stateMu.Unlock()
		return nil, true, nil
	}
	resource.deadlineGeneration++
	generation := resource.deadlineGeneration
	connection := resource.connection
	started := time.Now()
	probeBound := started.Add(timeout)
	resource.readProbeDeadline = probeBound
	deadline := networkProbeDeadline(started, timeout, resource.ioTimeout)
	resource.stateMu.Unlock()

	setterErr := connection.SetReadDeadline(deadline)
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	if resource.closed || resource.deadlineGeneration != generation {
		return nil, true, nil
	}
	if setterErr != nil {
		resource.readProbeDeadline = time.Time{}
		return nil, false, setterErr
	}
	resource.lastReadDeadline = deadline
	return connection, false, nil
}

func finishSocketProbe(resource *SocketResource, connection net.Conn, ready []byte) (bool, error) {
	resource.deadlineMu.Lock()
	defer resource.deadlineMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection != connection {
		resource.stateMu.Unlock()
		return true, nil
	}
	if len(ready) != 0 {
		resource.bufferedRead = append(resource.bufferedRead, ready...)
	}
	resource.readProbeDeadline = time.Time{}
	resource.deadlineGeneration++
	generation := resource.deadlineGeneration
	deadline := effectiveNetworkDeadline(time.Now(), resource.ioTimeout, time.Time{})
	resource.stateMu.Unlock()

	setterErr := connection.SetReadDeadline(deadline)
	resource.stateMu.Lock()
	defer resource.stateMu.Unlock()
	if resource.closed || resource.deadlineGeneration != generation {
		return true, nil
	}
	if setterErr != nil {
		return false, setterErr
	}
	resource.lastReadDeadline = deadline
	return false, nil
}

func selectSocket(resource *SocketResource, timeout time.Duration) (bool, error) {
	resource.readMu.Lock()
	defer resource.readMu.Unlock()

	resource.stateMu.Lock()
	if resource.closed || resource.connection == nil {
		resource.stateMu.Unlock()
		return false, nil
	}
	if len(resource.bufferedRead) != 0 {
		resource.stateMu.Unlock()
		return true, nil
	}
	resource.stateMu.Unlock()

	connection, closed, err := beginSocketProbe(resource, timeout)
	if err != nil {
		return false, err
	}
	if closed {
		return false, nil
	}
	buffer := make([]byte, 1)
	read, readErr := connection.Read(buffer)
	ordinarySuccess := readErr == nil && read > 0
	closed, restoreErr := finishSocketProbe(resource, connection, func() []byte {
		if ordinarySuccess {
			return buffer[:read]
		}
		return nil
	}())
	if restoreErr != nil {
		return false, restoreErr
	}
	if closed || !ordinarySuccess {
		return false, nil
	}
	return true, nil
}

func closeListener(resource *ListenerResource) {
	resource.stateMu.Lock()
	if resource.closed {
		resource.stateMu.Unlock()
		return
	}
	resource.closed = true
	resource.deadlineGeneration++
	listener := resource.listener
	resource.listener = nil
	resource.acceptProbeDeadline = time.Time{}
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
	resource.deadlineGeneration++
	connection := resource.connection
	resource.connection = nil
	resource.readProbeDeadline = time.Time{}
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
		configuredListener, ok := listener.(deadlineListener)
		if !ok {
			_ = listener.Close()
			return socketValue(-1, host, port, false), nil
		}
		handle := machine.shared.Listeners.add(&ListenerResource{listener: configuredListener})
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
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) != 2 || args[1].Type != value.VAL_BOOL || !args[1].AsBool {
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
		timeout, err := validateNetworkTimeout(args[1].AsInt)
		if err != nil {
			return value.NewNull(), err
		}
		if err := configureNetworkTimeout(machine, args[0], timeout); err != nil {
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
				var probeErr error
				if listener, exists := machine.shared.Listeners.get(int(handle)); exists {
					ready, probeErr = selectListener(listener, timeout)
				} else if socket, exists := machine.shared.Sockets.get(int(handle)); exists {
					ready, probeErr = selectSocket(socket, timeout)
				}
				if probeErr != nil {
					return value.NewNull(), probeErr
				}
				if ready {
					readyRead = append(readyRead, candidate)
				}
			}
		}
		return selectResult(readyRead), nil
	})
}
