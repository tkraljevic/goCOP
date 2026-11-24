package web

import (
	"strings"
	"time"

	"gocop/internal/models"
)

// oblikPolja je oblik vrijednosti polja datuma i vremena (datetime-local)
const oblikPolja = "2006-01-02T15:04"

// minutaPolja vraća vrijednost polja datuma i vremena bez sekundi. Safari
// pri upisu u prazno polje uzme trenutno vrijeme sa sekundama i zadrži ih
// skrivene kad osoba promijeni sat i minute, pa polje šalje npr.
// „2026-11-24T12:00:23”; vrijedi minuta koju osoba vidi
func minutaPolja(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > len(oblikPolja) && s[len(oblikPolja)] == ':' {
		return s[:len(oblikPolja)]
	}
	return s
}

// vrijemeIzPolja čita polje datuma i vremena po zagrebačkom vremenu
func vrijemeIzPolja(s string) (time.Time, error) {
	return time.ParseInLocation(oblikPolja, minutaPolja(s), models.Zagreb)
}
