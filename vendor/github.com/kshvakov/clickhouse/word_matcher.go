package clickhouse

import (
	"strings"
	"unicode"
)

// wordMatcher is a simple automata to match a single word (case insensitive)
type wordMatcher struct {
	word     []rune
	position uint8
}

// newMatcher returns matcher for word needle
func newMatcher(needle string) *wordMatcher {
	return &wordMatcher{word: []rune(strings.ToUpper(needle)),
		position: 0}
}

func (m *wordMatcher) matchRune(r rune) bool {
	if m.word[m.position] == unicode.ToUpper(r) {
		if m.position == uint8(len(m.word)-1) {
			m.position = 0
			return true
		}
		m.position++
	} else {
		m.position = 0
	}
	return false
}
