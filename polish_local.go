package main

import (
	"strings"
	"unicode"
)

func polishBoundary(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) }
func polishSeparator(r rune) bool {
	return unicode.IsSpace(r) || r == '，' || r == '、' || r == ','
}
func localPolish(text, mode string) string {
	if mode == "off" {
		return text
	}
	// Keep word-internal fillers and sentence-final 啊; remove separators after a deleted filler.
	runes := []rune(strings.TrimSpace(text))
	out := make([]rune, 0, len(runes))
	for i := 0; i < len(runes); {
		r := runes[i]
		left := i == 0 || polishBoundary(runes[i-1])
		right := i+1 == len(runes) || polishBoundary(runes[i+1])
		remove := strings.ContainsRune("嗯呃额唔", r) && left && right
		// Preserve common lexical uses such as 啊哈、啊呀 and 啊Q.
		lexicalA := r == '啊' && i+1 < len(runes) && strings.ContainsRune("哈呀哟唷Qq", runes[i+1])
		if r == '啊' && !lexicalA && left {
			remove = true
		}
		i++
		if remove {
			for i < len(runes) && polishSeparator(runes[i]) {
				i++
			}
			continue
		}
		out = append(out, r)
	}
	text = string(out)
	for _, s := range []string{"那个", "就是"} {
		for strings.Contains(text, s+s) {
			text = strings.ReplaceAll(text, s+s, s)
		}
	}
	if mode == "aggressive" {
		rs := []rune(text)
		out = out[:0]
		atStart := true

		for i := 0; i < len(rs); {
			if atStart && i+1 < len(rs) {
				word := string(rs[i : i+2])
				if word == "那个" || word == "就是" || word == "然后" {
					i += 2
					// Only consume following separators when a filler was actually removed.
					for i < len(rs) && (unicode.IsSpace(rs[i]) || rs[i] == '，' || rs[i] == ',') {
						i++
					}
					continue
				}
			}
			r := rs[i]
			out = append(out, r)
			if !unicode.IsSpace(r) {
				atStart = r == '，' || r == ',' || r == '。' || r == '！' || r == '？' || r == '\n'
			}
			if r == '\n' {
				atStart = true
			}
			i++
		}
		text = string(out)
	}
	out = nil
	for _, r := range []rune(text) {
		if len(out) > 0 && unicode.IsPunct(r) && out[len(out)-1] == r {
			continue
		}
		out = append(out, r)
	}
	return strings.TrimSpace(strings.TrimLeftFunc(string(out), polishSeparator))
}
