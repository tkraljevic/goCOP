package javnivodostaji

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Rubovi kruga preuzimanja: popis letvi koji se ne da pročitati, čvor koji
// vodostaje ne preuzima sam i rok koji istekne usred letvi. Svaki kaže što
// se dogodilo u ispisu kruga, s riječju uz broj u pravom obliku.

// spremisteBezPopisa ne može dati popis letvi
type spremisteBezPopisa struct{ *probnoSpremiste }

func (spremisteBezPopisa) LetveZaPreuzimanje(context.Context) ([]models.Station, error) {
	return nil, errors.New("baza je zaključana")
}

// imaRetke javlja svaki od željenih redaka kojeg ispis kruga nema
func imaRetke(t *testing.T, u *Uvoznik, zeli ...string) {
	t.Helper()
	redci := strings.Join(u.Napredak().Redci, "\n")
	for _, z := range zeli {
		if !strings.Contains(redci, z) {
			t.Errorf("ispis kruga nema %q:\n%s", z, redci)
		}
	}
}

// uvoznikJedneLetve preuzima Batinu s probne stranice i broji koliko je
// puta pozvan izračun nakon kruga
func uvoznikJedneLetve() (*Uvoznik, *int) {
	st := models.Station{ID: uuid.New(), Name: "Batina", JavniUvoz: true,
		JavniURL: "https://mvodostaji.voda.hr/Home/PregledVodostajaPostaje?sektorID=2&bpID=34&postajaID=424"}
	u := NoviUvoznik(&probnoSpremiste{letve: []models.Station{st}, postojeca: map[int64]bool{}}, nil)
	u.Client.HTTP = &http.Client{Transport: probniPrijenos{probnaTablica}}
	nakon := 0
	u.NakonPreuzimanja = func(context.Context) { nakon++ }
	return u, &nakon
}

func TestKrugBezPopisaLetvi(t *testing.T) {
	var zapisnik []string
	u := NoviUvoznik(spremisteBezPopisa{&probnoSpremiste{}}, func(f string, a ...any) { zapisnik = append(zapisnik, f) })
	if n := u.PreuzmiSve(context.Background()); n != 0 {
		t.Errorf("bez popisa letvi upisano %d", n)
	}
	if n := u.Napredak(); n.UTijeku || !strings.HasPrefix(n.Faza, "popis letvi nije uspio: baza je zaključana") || len(zapisnik) != 1 {
		t.Errorf("bez popisa letvi: %+v, zapisnik %q", n, zapisnik)
	}
}

func TestKrugCvoraKojiVodostajeNePreuzima(t *testing.T) {
	u, nakon := uvoznikJedneLetve()
	u.Preuzima = func() bool { return false }
	if n := u.PreuzmiSve(context.Background()); n != 0 {
		t.Errorf("čvor koji ne preuzima upisao je %d", n)
	}
	imaRetke(t, u, "vodostaje s izvora preuzima drugi čvor; stižu razmjenom", "vodostaji preuzeti: 0 letvi, 0 novih očitanja")
	if *nakon != 1 {
		t.Errorf("prognoza nakon kruga pozvana %d puta, želim 1", *nakon)
	}
}

// Rok je već istekao: letve se prekinu iza prve, a prognoza se ne računa.
func TestKrugKojemJeIstekaoRok(t *testing.T) {
	u, nakon := uvoznikJedneLetve()
	rok, otkazi := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer otkazi()
	u.PreuzmiSve(rok)
	imaRetke(t, u, "preuzimanje letvi prekinuto: istekao rok", "vodostaji preuzeti: 1 letva, ")
	if *nakon != 0 {
		t.Errorf("prognoza se računala i kad je rok kruga istekao (%d)", *nakon)
	}
}
