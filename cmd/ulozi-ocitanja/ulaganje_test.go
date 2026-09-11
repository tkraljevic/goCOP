package main

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gocop/internal/arhiva"
	"gocop/internal/models"
)

func ocit(sat int, cm int, kvaliteta, napomena, vrsta string) models.Reading {
	v := cm
	return models.Reading{
		ID:         uuid.New(),
		MeasuredAt: time.Date(2026, 9, 10, sat, 0, 0, 0, time.UTC),
		LevelCm:    &v, Quality: kvaliteta, Note: napomena, VrstaBiljeske: vrsta,
	}
}

// Sumnjivo se ne ulaže: arhiva nema mjesto za sumnju po vrijednosti, pa bi
// ondje izgledalo kao mjerenje. Rekonstruirano ide odvojeno, jer arhiva
// preračun ionako vodi zasebno i uvijek na kraju reda povjerenja.
func TestRazvrstavanjeCuvaRazlikuIzmedjuMjerenogISumnjivog(t *testing.T) {
	iz := razvrstaj([]models.Reading{
		ocit(1, -86, models.QualityMeasured, "", ""),
		ocit(2, -87, "", "", ""), // prazna kvaliteta je izmjereno
		ocit(3, -88, models.QualityReconstructed, "", ""),
		ocit(4, -89, models.QualityUncertain, "", ""),
		{ID: uuid.New(), MeasuredAt: time.Now(), Quality: models.QualityMeasured}, // bez vodostaja
	})
	if len(iz.mjereno) != 2 {
		t.Errorf("izmjerenih %d, očekivana dva", len(iz.mjereno))
	}
	if len(iz.preracunato) != 1 {
		t.Errorf("rekonstruiranih %d", len(iz.preracunato))
	}
	if iz.sumnjivo != 1 {
		t.Errorf("sumnjivih %d", iz.sumnjivo)
	}
	if iz.bezVrijednosti != 1 {
		t.Errorf("bez vrijednosti %d", iz.bezVrijednosti)
	}
	// Sumnjivo i ono bez vrijednosti NE smiju se označiti kao uložena, inače
	// bi ih zaboravljanje obrisalo a nigdje ih ne bi bilo.
	if len(iz.ulozeniID) != 3 {
		t.Errorf("označeno za ulaganje %d, očekivana tri", len(iz.ulozeniID))
	}
}

// Bilješka prelazi uz vrijednost — to je jedino zbog čega se ručno očitanje i
// pamti nakon što broj ode u arhivu.
func TestBiljeskaPrelaziUzVrijednost(t *testing.T) {
	iz := razvrstaj([]models.Reading{
		ocit(4, -90, models.QualityMeasured, "očitan minimum", models.BiljeskaDno),
		ocit(5, -89, models.QualityMeasured, "", ""),
	})
	if len(iz.biljeske) != 1 {
		t.Fatalf("bilježaka %d", len(iz.biljeske))
	}
	b := biljeskeZa("vukovar", "satni", iz.biljeske)
	if b[0].Vrsta != models.BiljeskaDno || b[0].Tekst != "očitan minimum" {
		t.Errorf("bilješka %+v", b[0])
	}
	if b[0].Korak != "satni" || b[0].Letva != "vukovar" {
		t.Errorf("ključ bilješke %+v", b[0])
	}
	// Dnevni niz nosi korak dnevni, inače bilješka ne bi našla svoju vrijednost.
	if d := biljeskeZa("vukovar", "jutarnji", iz.biljeske); d[0].Korak != "dnevni" {
		t.Errorf("korak za jutarnji niz: %q", d[0].Korak)
	}
}

// Vrsta niza se pogađa iz gustoće: jedno dnevno očitanje je jutarnje, satna
// telemetrija je satni niz. Kriv izbor stvorio bi drugi niz umjesto dopune.
func TestVrstaNizaSePogadjaIzGustoce(t *testing.T) {
	var satni []arhiva.Redak
	for i := 0; i < 48; i++ {
		satni = append(satni, arhiva.Redak{Vrijeme: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)})
	}
	if got := pogodiVrstu(satni); got != "satni" {
		t.Errorf("48 vrijednosti u dva dana → %q", got)
	}
	var dnevni []arhiva.Redak
	for i := 0; i < 10; i++ {
		dnevni = append(dnevni, arhiva.Redak{Vrijeme: time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC).AddDate(0, 0, i)})
	}
	if got := pogodiVrstu(dnevni); got != "jutarnji" {
		t.Errorf("10 vrijednosti u deset dana → %q", got)
	}
	if got := pogodiVrstu(nil); got != "jutarnji" {
		t.Errorf("prazno → %q", got)
	}
}
