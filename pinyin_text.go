package main

import (
	"strings"
	"unicode/utf8"
)

func normalizePinyin(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\'' || r == ',' || r == '\t' {
			return -1
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}, s)
}
func validPinyin(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}
func validPhrase(key, text string) bool {
	return validPinyin(normalizePinyin(key)) && text != "" && len(text) <= 4096 && utf8.ValidString(text) && !strings.ContainsAny(text, "\x00\r\t")
}
