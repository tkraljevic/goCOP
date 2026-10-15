package models

import (
	"strings"
	"unicode/utf8"
)

// ZavrsenaRecenica vraća tekst s točkom na kraju, osim kad već završava
// točkom, upitnikom ili uskličnikom. Napomena se upisuje rukom, sa
// završnom točkom ili bez nje; rečenica koja se na nju nastavlja tako ne
// dobije dvije točke ni nijednu.
func ZavrsenaRecenica(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if zadnji, _ := utf8.DecodeLastRuneInString(s); !strings.ContainsRune(".!?", zadnji) {
		s += "."
	}
	return s
}
