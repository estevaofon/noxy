//go:build !windows

package console

// EnsureLineInput is a no-op outside Windows: POSIX terminal state is managed
// per-descriptor via termios and the shell restores it between commands.
func EnsureLineInput() {}
