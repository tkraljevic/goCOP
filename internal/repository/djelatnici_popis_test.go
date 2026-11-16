package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Plan Djelatnika, B.7 i B.8: popis je po hrvatskoj abecedi, a filtar
// StanjeUkljucen (usporedba s adresarom, adrese za akte, upis akta na
// listove vodočuvara) ne daje isključene račune. Prije je tražio „active”,
// pa nije filtrirao ništa.
func TestPopisDjelatnikaAbecednoIBezIskljucenih(t *testing.T) {
	c := noviCvor(t, "ured")
	repo := NewUserRepository(c.db, c.rec)
	for i, ime := range []string{"Zoran Primjer", "Čedo Primjer", "Ćiro Primjer", "Dario Primjer", "Đuro Primjer", "Šime Primjer", "Željko Primjer", "Cvijeta Primjer"} {
		u := models.User{ID: uuid.New(), Username: "u" + string(rune('a'+i)), FullName: ime, PasswordHash: "x",
			IsActive: ime != "Dario Primjer", OrgType: models.OrgHrvatskeVode, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		nuzno(t, repo.CreateUser(&u, nil))
	}
	imena := func(status string) string {
		svi, err := repo.ListUsers("", 0, "", "", status)
		nuzno(t, err)
		var out []string
		for _, u := range svi {
			if strings.HasSuffix(u.FullName, " Primjer") {
				out = append(out, strings.Fields(u.FullName)[0])
			}
		}
		return strings.Join(out, ", ")
	}
	if got := imena(""); got != "Cvijeta, Čedo, Ćiro, Dario, Đuro, Šime, Zoran, Željko" {
		t.Errorf("abeceda: %s", got)
	}
	if got := imena(string(StanjeUkljucen)); strings.Contains(got, "Dario") || !strings.Contains(got, "Đuro") {
		t.Errorf("uključeni: %s", got)
	}
}
