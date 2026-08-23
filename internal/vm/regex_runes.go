package vm

import "unicode/utf8"

// runeMark é um checkpoint byteOff -> runeIdx já visitado, para converter
// offsets fora de ordem sem rescanear a string desde o começo.
type runeMark struct {
	byteOff int
	runeIdx int
}

// runeConverter traduz offsets de BYTE (como o regexp do Go devolve) em
// índices de RUNA (como o Noxy indexa strings — ver strings_index_of em
// builtins_strings.go). ASCII curto-circuita: byte == runa (issue #66,
// item 2). Offsets sempre chegam em fronteira de runa porque vêm do
// próprio regexp sobre s.
type runeConverter struct {
	s     string
	ascii bool
	marks []runeMark // ordenado por byteOff; marks[0] = {0, 0}
}

func newRuneConverter(s string) *runeConverter {
	return &runeConverter{s: s, ascii: isASCII(s), marks: []runeMark{{0, 0}}}
}

func (converter *runeConverter) index(byteOff int) int {
	if converter.ascii {
		return byteOff
	}
	// Maior mark com byteOff <= alvo (busca binária).
	lo, hi := 0, len(converter.marks)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if converter.marks[mid].byteOff <= byteOff {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	mark := converter.marks[lo]
	runeIdx := mark.runeIdx + utf8.RuneCountInString(converter.s[mark.byteOff:byteOff])
	if byteOff > converter.marks[len(converter.marks)-1].byteOff {
		converter.marks = append(converter.marks, runeMark{byteOff, runeIdx})
	}
	return runeIdx
}
