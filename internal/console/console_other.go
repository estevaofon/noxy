//go:build !windows

package console

import "os"

// EnsureLineInput is a no-op outside Windows: POSIX terminal state is managed
// per-descriptor via termios and the shell restores it between commands.
func EnsureLineInput() {}

// EnableANSIStdout reports whether stdout can render ANSI escape sequences.
// Outside Windows every terminal understands them, so being a character
// device is enough; pipes and files stay free of escape bytes.
func EnableANSIStdout() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
