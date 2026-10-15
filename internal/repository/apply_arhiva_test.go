package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Isti trenutak niza zapisan je dvaput, s dva identiteta, i obje bilješke su
// žive u knjizi. Tablica na trenutku drži samo jednu. Novi čvor je prvo
// upisivao onu koja je prva stigla, a druga je pri svakoj razmjeni padala na
// jedinstvenom ključu. Sad na svakom čvoru ostaje novija, ma kojim redom
// verzije stigle, i nijedna ne javlja grešku.

var trenutakProbe = time.Date(2026, 9, 7, 5, 0, 0, 0, time.UTC)

func biljeskaZa(t *testing.T, id, versionID string, izmijenjeno time.Time, tekst string) ledger.Version {
	t.Helper()
	return verzijaZa(t, EntityBiljeske, id, versionID, models.ArhivaBiljeska{
		ID: uuid.MustParse(id), Letva: "probna", Velicina: "vodostaj", Korak: "satni",
		Vrijeme: trenutakProbe, Tekst: tekst, Tko: "Proba Probić", CreatedAt: izmijenjeno, UpdatedAt: izmijenjeno})
}

func ispravakZa(t *testing.T, id, versionID string, izmijenjeno time.Time, vrijednost float64) ledger.Version {
	t.Helper()
	return verzijaZa(t, EntityIspravci, id, versionID, models.ArhivaIspravak{
		ID: uuid.MustParse(id), Letva: "probna", Velicina: "vodostaj", Korak: "satni",
		Vrijeme: trenutakProbe, Vrijednost: vrijednost, Razlog: "očitano s letve", Ispravio: "Proba Probić",
		CreatedAt: izmijenjeno, UpdatedAt: izmijenjeno})
}

// naTrenutku vraća identitete zapisa koje tablica drži na trenutku probe
func naTrenutku(t *testing.T, c *cvor, tablica string) []string {
	t.Helper()
	rows, err := c.db.Query(`SELECT id FROM `+tablica+` WHERE letva = 'probna' AND velicina = 'vodostaj' AND korak = 'satni'`)
	nuzno(t, err)
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		nuzno(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	nuzno(t, rows.Err())
	return ids
}

// redoslijedi su načini na koje dvije verzije stižu: odvojeno, svaki put
// drugim redom, i u istom paketu
func redoslijedi(a, b ledger.Version) map[string][][]ledger.Version {
	return map[string][][]ledger.Version{
		"starija pa novija": {{a}, {b}},
		"novija pa starija": {{b}, {a}},
		"isti paket":        {{b, a}},
	}
}

func provjeriPobjednika(t *testing.T, tablica string, a, b ledger.Version, pobjednik string) {
	t.Helper()
	for ime, paketi := range redoslijedi(a, b) {
		t.Run(ime, func(t *testing.T) {
			sNedovrsenima(t)
			c := noviCvor(t, "novi-ured")
			for _, p := range paketi {
				if err := primiPaket(t, c, p...); err != nil {
					t.Fatalf("primjena: %v", err)
				}
			}
			if got := naTrenutku(t, c, tablica); len(got) != 1 || got[0] != pobjednik {
				t.Errorf("na trenutku je %v, a treba biti samo %s", got, pobjednik)
			}
		})
	}
}

func TestDvijeBiljeskeNaIstomTrenutku(t *testing.T) {
	ranije := time.Date(2026, 9, 11, 7, 9, 0, 0, time.UTC)
	kasnije := time.Date(2026, 9, 11, 7, 13, 0, 0, time.UTC)
	// starija bilješka ima veći identitet, pa odlučuje vrijeme izmjene
	starija := biljeskaZa(t, "01a08f4c-0000-7000-8000-00000000000f", vid(1), ranije, "letva.voda.hr")
	novija := biljeskaZa(t, "01a08f50-0000-7000-8000-000000000001", vid(2), kasnije, "letva")
	provjeriPobjednika(t, "arhiva_biljeske", starija, novija, novija.EntityID)
}

func TestDvijeBiljeskeIstogVremenaIzmjene(t *testing.T) {
	kad := time.Date(2026, 9, 11, 7, 13, 0, 0, time.UTC)
	manji := biljeskaZa(t, "01a08f50-0000-7000-8000-00000000000a", vid(1), kad, "prva")
	veci := biljeskaZa(t, "01a08f50-0000-7000-8000-00000000000b", vid(2), kad, "druga")
	provjeriPobjednika(t, "arhiva_biljeske", manji, veci, veci.EntityID)
}

func TestDvaIspravkaNaIstomTrenutku(t *testing.T) {
	ranije := time.Date(2026, 9, 11, 7, 9, 0, 0, time.UTC)
	kasnije := time.Date(2026, 9, 11, 7, 13, 0, 0, time.UTC)
	starija := ispravakZa(t, "01a08f4c-0000-7000-8000-00000000000f", vid(1), ranije, 312)
	novija := ispravakZa(t, "01a08f50-0000-7000-8000-000000000001", vid(2), kasnije, 315)
	provjeriPobjednika(t, "arhiva_ispravci", starija, novija, novija.EntityID)
}

// Nova verzija bilješke koja je izgubila trenutak (izmjena na čvoru koji je
// držao nju) ponovno ga dobiva, jer je sad ona novija.
func TestIzmijenjenaBiljeskaVracaTrenutak(t *testing.T) {
	sNedovrsenima(t)
	c := noviCvor(t, "novi-ured")
	a := biljeskaZa(t, "01a08f4c-0000-7000-8000-00000000000f", vid(1), time.Date(2026, 9, 11, 7, 9, 0, 0, time.UTC), "prva")
	b := biljeskaZa(t, "01a08f50-0000-7000-8000-000000000001", vid(2), time.Date(2026, 9, 11, 7, 13, 0, 0, time.UTC), "druga")
	a2 := biljeskaZa(t, a.EntityID, vid(3), time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC), "prva, izmijenjena")
	nuzno(t, primiPaket(t, c, a, b))
	nuzno(t, primiPaket(t, c, a2))
	if got := naTrenutku(t, c, "arhiva_biljeske"); len(got) != 1 || got[0] != a.EntityID {
		t.Errorf("na trenutku je %v, a treba biti izmijenjena %s", got, a.EntityID)
	}
}

// Primljena verzija s pokvarenim sadržajem javlja grešku i ne upisuje ništa
func TestPokvarenSadrzajJavljaGresku(t *testing.T) {
	c := noviCvor(t, "novi-ured")
	for _, entitet := range []string{EntityBiljeske, EntityIspravci, EntityJournalSheets, EntityJournalEntries} {
		t.Run(entitet, func(t *testing.T) {
			tx, err := c.db.BeginTx(ctxRaz, nil)
			nuzno(t, err)
			defer tx.Rollback()
			v := ledger.Version{Entity: entitet, EntityID: "x", Payload: json.RawMessage(`{"id":`)}
			if err := applyOne(ctxRaz, tx, v); err == nil {
				t.Error("pokvaren sadržaj prošao je bez greške")
			}
		})
	}
}
