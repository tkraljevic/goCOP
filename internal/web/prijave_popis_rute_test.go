package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Popis prijava po crtežu: isti filtri i parametri, kartica s kratkim mjestom,
// rekonstrukcija u boji upozorenja, a poruka iz adrese samo jednom (ispisuje je
// osnovni predložak). Podaci su izmišljeni.
func TestPopisPrijavaPoCrtezu(t *testing.T) {
	pr := novaProbaPrijava(t)
	v := pr.osoba(t, "ivan.primjer", "Ivan Primjer", models.Duty{Role: models.RoleWaterGuard})
	p := pr.objavljena(t, v, "Oštećena rampa", "P.1.1")
	p.Objekt, p.Stacionaza = "nasip Prvi", "4+300"
	p.Datum = time.Date(2026, 10, 6, 12, 0, 0, 0, models.Zagreb)
	lat, lon := 45.7, 18.8
	p.Latitude, p.Longitude = &lat, &lon
	if err := pr.repo.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}

	s := pr.get(t, v, "/prijave?success=Spremljeno").Body.String()
	for _, zeli := range []string{
		`name="sektor"`, `name="podrucje"`, `name="status"`, `name="godina"`, `name="q"`, "Traži",
		"Branjeno područje", "BP 1: Primjerica", `href="/prijave/nova"`, "1 prijava",
		"nasip Prvi · P.1.1 · st. 4&#43;300", "6. 10. 2026.", "Ivan Primjer · BP 1", `title="označeno na karti"`,
		`<span class="oznaka-stanja pozor" title="preneseno iz ranije evidencije">rekonstrukcija</span>`,
		`data-naziv-stranice>Prijave i obavijesti s terena`, `class="povratna-poveznica">← Dokumenti`,
		// Širina natpisa: bez nje dodir na natpis u čipu ne otvara izbor.
		`style="--natpis-znakova: 6"`, `style="--natpis-znakova: 18"`,
	} {
		if !strings.Contains(s, zeli) {
			t.Errorf("na popisu nema %q", zeli)
		}
	}
	if n := strings.Count(s, "Spremljeno"); n != 1 {
		t.Errorf("poruka iz adrese ispisana je %d puta", n)
	}
	if strings.Contains(s, "breadcrumb") {
		t.Error("staza je ostala uz povratnu poveznicu")
	}
}

// „Još nemate prijava.” stoji samo kad je to istina. Izbor koji ne daje ništa,
// prazan zadani sektor kod osobe koja ima prijave negdje drugdje i osoba koja ne
// vodi dnevnik i dalje čitaju rečenicu o izboru.
func TestPrazanPopisPrijava(t *testing.T) {
	pr := novaProbaPrijava(t)
	const josNema, nemaIzbora = "Još nemate prijava.", "Nema prijava za odabrani izbor."
	nov := pr.osoba(t, "ivan.primjer", "Ivan Primjer", models.Duty{Role: models.RoleWaterGuard})
	s := pr.get(t, nov, "/prijave").Body.String()
	if !strings.Contains(s, josNema) || strings.Contains(s, nemaIzbora) {
		t.Error("vodočuvar bez prijava, bez izbora: treba reći da još nema prijava")
	}
	if !strings.Contains(s, `name="sektor"`) {
		t.Error("filtri ostaju i na praznom popisu")
	}
	for _, adresa := range []string{"/prijave?status=NACRT", "/prijave?sektor=P", "/prijave?q=rampa", "/prijave?godina=2022", "/prijave?podrucje=1"} {
		s := pr.get(t, nov, adresa).Body.String()
		if !strings.Contains(s, nemaIzbora) || !strings.Contains(s, "Novu prijavu otvarate gumbom gore ili s Terena.") || strings.Contains(s, josNema) {
			t.Errorf("%s: prazan izbor mora ostati rečenica o izboru", adresa)
		}
	}

	// prijava iz sektora u kojem je radio: zadani sektor je prazan, ali prijave ima
	premjesten := pr.osoba(t, "ante.primjer", "Ante Primjer", models.Duty{Role: models.RoleWaterGuard})
	stara := pr.objavljena(t, premjesten, "Stara prijava", "")
	stara.Sektor = "Q"
	if err := pr.repo.Save(context.Background(), stara); err != nil {
		t.Fatal(err)
	}
	if s := pr.get(t, premjesten, "/prijave").Body.String(); !strings.Contains(s, nemaIzbora) || strings.Contains(s, josNema) {
		t.Error("vodočuvar s prijavom u drugom sektoru nije bez prijava")
	}

	podrucje := 1
	rukovoditelj := pr.osoba(t, "josip.primjer", "Josip Primjer", models.Duty{Role: models.RoleAreaLeader, AreaID: &podrucje})
	s = pr.get(t, rukovoditelj, "/prijave?sektor=Q&status=RIJESENA").Body.String()
	if !strings.Contains(s, nemaIzbora) || strings.Contains(s, "Novu prijavu otvarate") || strings.Contains(s, `href="/prijave/nova"`) {
		t.Error("tko ne vodi dnevnik: samo rečenica o izboru, bez gumba Nova prijava")
	}
}
