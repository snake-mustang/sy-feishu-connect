package util

import "strings"

func Chunks(s string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = 3500
	}
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}
	var out []string
	for len(runes) > maxRunes {
		cut := maxRunes
		for i := maxRunes; i > maxRunes-300 && i > 0; i-- {
			if runes[i-1] == '\n' {
				cut = i
				break
			}
		}
		out = append(out, strings.TrimSpace(string(runes[:cut])))
		runes = runes[cut:]
	}
	if tail := strings.TrimSpace(string(runes)); tail != "" {
		out = append(out, tail)
	}
	return out
}
