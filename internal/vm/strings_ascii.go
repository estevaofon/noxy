package vm

import "unicode/utf8"

// isASCII responde se s pode ser indexada por byte: todo byte < 0x80 e um
// code point inteiro, entao indice em runes == indice em bytes. Qualquer byte
// >= 0x80 — continuacao multibyte ou lixo invalido — devolve false e o site
// cai no caminho []rune de sempre. Sem alocacao (issue #66, item 2).
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
