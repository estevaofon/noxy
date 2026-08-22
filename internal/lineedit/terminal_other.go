//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package lineedit

// Stdin e nil fora dos sistemas POSIX suportados. No Windows o console em
// modo cooked ja oferece edicao de linha e historico (setas) para leituras de
// linha convencionais, entao o REPL segue com bufio.Scanner la.
func Stdin() *Editor { return nil }
