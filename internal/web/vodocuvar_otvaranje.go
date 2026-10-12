package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// Trenutak otvaranja obrasca dnevnog lista (Odluke (11. 10. 2026.)). Obrazac
// za list koji još ne postoji nosi potpisanu oznaku: tko je i kad otvorio
// obrazac za koji dan. Spremanje poslije ponoći s oznakom tog dana sprema
// list na njegov dan, pa se upisano ne gubi. Potpis je HMAC s tajnom koja
// nastaje pri pokretanju poslužitelja i nigdje se ne zapisuje: oznaku nitko
// ne može izmisliti ni prepraviti za drugi dan, a nakon ponovnog pokretanja
// stare oznake ne vrijede, pa je spremanje tada kao i prije.
type oznakeOtvaranja struct{ tajna []byte }

func noveOznakeOtvaranja() *oznakeOtvaranja {
	t := make([]byte, 32)
	_, _ = rand.Read(t) // crypto/rand.Read ne vraća grešku i uvijek popuni t
	return &oznakeOtvaranja{tajna: t}
}

// oznaka je potpisani trenutak otvaranja obrasca kad za osobu i dan; bez
// tajne prazna
func (o *oznakeOtvaranja) oznaka(userID string, dan, kad time.Time) string {
	if o == nil {
		return ""
	}
	sadrzaj := userID + "|" + dan.In(models.Zagreb).Format("2006-01-02") + "|" + strconv.FormatInt(kad.Unix(), 10)
	return base64.RawURLEncoding.EncodeToString([]byte(sadrzaj)) + "." + o.potpis(sadrzaj)
}

func (o *oznakeOtvaranja) potpis(sadrzaj string) string {
	m := hmac.New(sha256.New, o.tajna)
	m.Write([]byte(sadrzaj))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// otvoreno je trenutak iz oznake kad je oznaka ispravno potpisana i
// pripada osobi i danu; inače nula
func (o *oznakeOtvaranja) otvoreno(oznaka, userID string, dan time.Time) time.Time {
	sadrzaj, ok := o.provjeren(oznaka)
	if !ok {
		return time.Time{}
	}
	polja := strings.Split(sadrzaj, "|")
	if len(polja) != 3 || polja[0] != userID || polja[1] != dan.In(models.Zagreb).Format("2006-01-02") {
		return time.Time{}
	}
	sek, err := strconv.ParseInt(polja[2], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sek, 0)
}

// provjeren je sadržaj oznake kad joj potpis odgovara tajni
func (o *oznakeOtvaranja) provjeren(oznaka string) (string, bool) {
	dio, potpis, ok := strings.Cut(oznaka, ".")
	if o == nil || !ok {
		return "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(dio)
	if err != nil || !hmac.Equal([]byte(potpis), []byte(o.potpis(string(b)))) {
		return "", false
	}
	return string(b), true
}
