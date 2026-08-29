// internal/ext/testdata/processguest/main.go
// Guest de teste do backend de processo, escrito com o SDK: compilado em
// tempo de teste por exttest.BuildProcessGuest. A ordem dos exports esta
// no manifesto de process_guest_test.go.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/estevaofon/noxy/sdk/noxyplugin"
)

func main() {
	p := noxyplugin.New()
	p.Handle("guest_echo", func(_ context.Context, args noxyplugin.Args) (any, error) {
		if len(args) == 0 {
			return nil, nil
		}
		return args[0], nil
	})
	p.Handle("guest_add", noxyplugin.Func2(func(_ context.Context, a, b int64) (int64, error) { return a + b, nil }))
	p.Handle("guest_fail", noxyplugin.Func1(func(_ context.Context, msg string) (any, error) { return nil, errors.New(msg) }))
	p.Handle("guest_sleep_ms", noxyplugin.Func1(func(ctx context.Context, ms int64) (any, error) {
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))
	p.Handle("guest_block", noxyplugin.Func0(func(context.Context) (any, error) {
		time.Sleep(10 * time.Second) // ignora o cancel de proposito
		return nil, nil
	}))
	p.Handle("guest_exit", noxyplugin.Func1(func(_ context.Context, code int64) (any, error) {
		os.Exit(int(code))
		return nil, nil
	}))
	p.Handle("guest_log", noxyplugin.Func1(func(_ context.Context, msg string) (any, error) {
		noxyplugin.Logf(noxyplugin.LevelInfo, "%s", msg)
		return nil, nil
	}))
	p.Handle("guest_panic", noxyplugin.Func0(func(context.Context) (any, error) { panic("kaboom") }))
	p.Handle("guest_bytes", noxyplugin.Func1(func(_ context.Context, b []byte) ([]byte, error) { return b, nil }))
	p.Handle("guest_pid", noxyplugin.Func0(func(context.Context) (int64, error) { return int64(os.Getpid()), nil }))
	p.Handle("guest_print", noxyplugin.Func1(func(_ context.Context, s string) (any, error) {
		fmt.Println(s) // vai para stderr: Main protege o stdout do protocolo
		return nil, nil
	}))
	p.Handle("guest_badtype", noxyplugin.Func0(func(context.Context) (string, error) { return "not an int", nil }))
	p.Handle("guest_noop", noxyplugin.Func0(func(context.Context) (any, error) { return nil, nil }))
	p.Main()
}
