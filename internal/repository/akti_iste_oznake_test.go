package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Akti iste oznake u mješovitim podacima: dva akta iste oznake, akt s
// drugom ovjerom i akt čiju oznaku nosi druga ovjera drugog akta jesu u
// sukobu; ista oznaka u drugom sektoru ili godini, akt bez para i nacrti
// nisu (Odluke (8. 10. 2026.), pitanje 4)
func TestAktiIsteOznakeUMjesovitimPodacima(t *testing.T) {
	_, repo := bazaAkata(t)
	ctx := context.Background()
	spremi := func(a *models.Akt) {
		t.Helper()
		if err := repo.SaveAkt(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	akt := func(sektor string, godina, broj int, kod, cvor string) *models.Akt {
		a := &models.Akt{Sektor: sektor, AreaID: 34, Godina: godina, Broj: broj, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
			Status: models.AktOvjeren, OvjeraKod: kod, Cvor: cvor, Vrijedi: time.Date(godina, 10, 6, 3, 0, 0, 0, models.Zagreb), IzradenoAt: time.Now()}
		spremi(a)
		return a
	}
	prvi := akt("B", 2026, 1, "A1", "cop-osijek")
	tudi := akt("B", 2026, 1, "U1", "server")
	drugi := akt("B", 2026, 2, "A2", "cop-osijek")
	akt("B", 2026, 4, "A4", "cop-osijek")
	akt("D", 2026, 1, "D1", "cop-osijek")
	akt("B", 2027, 1, "A7", "cop-osijek")
	nacrtAkta(t, repo, "Batina")
	nacrtAkta(t, repo, "Vukovar")
	// isti nacrt ovjeren na dva čvora: B-3/2026 ostaje u knjizi, na
	// površini je B-5/2026
	treci := akt("B", 2026, 3, "L3", "laptop")
	treci.Broj, treci.OvjeraKod, treci.Cvor = 5, "A5", "cop-osijek"
	spremi(treci)
	// druga ovjera nosi oznaku B-2/2026, koju na površini nosi drugi
	peti := akt("B", 2026, 2, "L2", "laptop")
	peti.Broj, peti.OvjeraKod, peti.Cvor = 6, "A6", "cop-osijek"
	spremi(peti)

	iste, err := repo.AktiIsteOznake(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hoce := []*models.Akt{prvi, tudi, drugi, treci, peti}
	if len(iste) != len(hoce) {
		t.Fatalf("akti iste oznake: %d, a treba %d: %+v", len(iste), len(hoce), iste)
	}
	for i, a := range hoce {
		if iste[i].ID != a.ID || iste[i].Oznaka() != a.Oznaka() {
			t.Errorf("%d. akt iste oznake: %s (%s), a treba %s (%s)", i+1, iste[i].Oznaka(), iste[i].ID, a.Oznaka(), a.ID)
		}
	}
}

// Sukobi oznaka čitaju se na svakoj stranici akata (popis, akt, nacrt). Upit
// koji za svaki akt s brojem ponovno prolazi knjigu verzija raste s
// umnoškom akata i verzija: s tisuću akata u sezoni stranica čeka sekundama.
// Skupovi oznaka zato se slažu jednom, pa plan upita nema koreliranih
// podupita (Odluke (8. 10. 2026.), pitanje 4).
func TestAktiIsteOznakeBezKoreliranihPodupita(t *testing.T) {
	baza, _ := bazaAkata(t)
	rows, err := baza.Query(`EXPLAIN QUERY PLAN ` + aktiIsteOznakeUpit)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, roditelj, nekoristeno int
		var korak string
		if err := rows.Scan(&id, &roditelj, &nekoristeno, &korak); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, korak)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, korak := range plan {
		if strings.Contains(korak, "CORRELATED") {
			t.Errorf("korak plana ponavlja se za svaki akt: %q\nplan:\n%s", korak, strings.Join(plan, "\n"))
		}
	}
}
