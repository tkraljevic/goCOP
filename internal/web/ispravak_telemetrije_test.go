package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Letve i vodostaji u ovom testu su izmišljeni.

// Okvir o ispravku telemetrije: velik ispravak traži teren, mali samo kaže da
// se ispravlja, a ručno očitanje koje odstupa čeka potvrdu. Bez ičega okvira
// nema.
func TestOkvirIspravkaTelemetrije(t *testing.T) {
	kad := time.Date(2026, 11, 19, 6, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		ime    string
		s      models.StanjeIspravka
		naslov string
		tekst  []string
	}{
		{"teren", models.StanjeIspravka{PomakCm: -43}, "Mjerni uređaj treba ispraviti na terenu.",
			[]string{"Pokazuje 43 cm više od ručnog očitanja.", "ispravlja za -43 cm", "Ispravak prestaje sam"}},
		{"teren, uređaj pokazuje manje", models.StanjeIspravka{PomakCm: 96}, "Mjerni uređaj treba ispraviti na terenu.",
			[]string{"Pokazuje 96 cm manje od ručnog očitanja.", "ispravlja za +96 cm"}},
		{"mali ispravak", models.StanjeIspravka{PomakCm: -7}, "Očitanja mjernog uređaja se ispravljaju.",
			[]string{"Pokazuje 7 cm više od ručnog očitanja.", "ispravlja za -7 cm"}},
		{"čeka potvrdu", models.StanjeIspravka{ImaRucno: true, RucnoKad: kad, RucnoCm: 312, SirovoCm: 355}, "Ručno očitanje odstupa od mjernog uređaja.",
			[]string{"Ručno očitanje od 19. 11. u 07:00 je 312 cm, a uređaj je tada pokazivao 355 cm.", "kad razliku potvrdi sljedeće ručno očitanje"}},
	} {
		o := ispravakLetve(s.s)
		if o == nil || o.Naslov != s.naslov {
			t.Errorf("%s: okvir %+v", s.ime, o)
			continue
		}
		for _, dio := range s.tekst {
			if !strings.Contains(o.Tekst, dio) {
				t.Errorf("%s: u tekstu nema %q: %q", s.ime, dio, o.Tekst)
			}
		}
	}
	if o := ispravakLetve(models.StanjeIspravka{ImaRucno: true, RucnoCm: 100, SirovoCm: 103}); o != nil {
		t.Errorf("ručno unutar točnosti dalo je okvir: %+v", o)
	}
}

// Na stranici letve stoji okvir i uz očitanje vrijednost uređaja, a u popisu
// znak uz letvu čiji uređaj treba ispraviti na terenu. Letva bez ispravka nema
// ni jedno ni drugo.
func TestIspravakNaStraniciIUPopisu(t *testing.T) {
	// letva je izuzeta iz provjere kvara, kao akumulacija; ispravak vrijedi i za nju
	mux, donji, srednji, repo := stranicaKvara(t, kvaroviZadani{izuzete: map[string]bool{"donji-brod": true}})
	cm, sirovo := 312, 355
	if _, err := repo.ImportBatch(context.Background(), []models.Reading{{ID: uuid.New(), StationID: donji.ID.String(),
		MeasuredAt: time.Now().Add(-10 * time.Minute).Truncate(time.Minute), LevelCm: &cm, SirovoCm: &sirovo,
		Source: models.ReadingSourceImport, Origin: "hdv.voda.hr"}}); err != nil {
		t.Fatal(err)
	}
	html := zoviKao(t, mux, "/readings/station/"+donji.ID.String())
	for _, dio := range []string{"data-ispravak-telemetrije", "Mjerni uređaj treba ispraviti na terenu.", "Pokazuje 43 cm više od ručnog očitanja.",
		"uređaj 355 cm, ispravljeno -43 cm po ručnom očitanju"} {
		if !strings.Contains(html, dio) {
			t.Errorf("stranica letve nema %q", dio)
		}
	}
	if html := zoviKao(t, mux, "/readings/station/"+srednji.ID.String()); strings.Contains(html, "data-ispravak-telemetrije") {
		t.Error("letva bez ispravka ima okvir")
	}
	popis := zoviKao(t, mux, "/readings")
	if n := strings.Count(popis, "data-znak-terena"); n != 1 {
		t.Errorf("znakova terena u popisu: %d, želim 1", n)
	}
	if !strings.Contains(popis, `title="Pokazuje 43 cm više od ručnog očitanja."`) {
		t.Error("znak u popisu ne kaže koliko uređaj odstupa")
	}
}
