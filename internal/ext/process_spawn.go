package ext

import (
	"context"
	"fmt"
)

// Stub ate a Task 8 (spawner real): NewProcess precisa compilar antes.
func execSpawner(path string) spawnFunc {
	return func(context.Context) (procConn, error) {
		return nil, fmt.Errorf("process spawner not implemented (Task 8)")
	}
}
