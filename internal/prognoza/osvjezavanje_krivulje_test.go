package prognoza

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// satU je sat od epohe za zadani datum u podne UTC.
func satU(godina int, mjesec time.Month, dan int) int64 {
	return time.Date(godina, mjesec, dan, 12, 0, 0, 0, time.UTC).Unix() / 3600
}

// Prognoza u drugoj veličini ide kroz krivulju koja tada vrijedi, s
// granicama provučenima kroz nju. Sat bez krivulje i vrijednost izvan nje
// se preskaču; granica izvan krivulje ne briše vrijednost nego pada na nju.
func TestUDrugojVeliciniKrozKrivulju(t *testing.T) {
	arhiva := arhivaKrivulja(t)
	dodajPravcuKrivulju(t, arhiva, 1, "proba", "2020-01-01", "")
	o := &Osvjezivac{Arhiva: arhiva}
	ctx := context.Background()
	sat := satU(2026, 10, 1)
	izdana := func(ciljni int64, v, d, g float64) Izdana {
		return Izdana{Letva: "proba", Velicina: "vodostaj", Izdano: sat, Ciljni: ciljni, Vrijednost: v, Dolje: d, Gore: g, Model: "lanac-1"}
	}
	got := o.uDrugojVelicini(ctx, []Izdana{
		izdana(satU(2019, 6, 1), 150, 140, 160), // krivulja još ne vrijedi
		izdana(sat+1, 150, 140, 160),
		izdana(sat+2, 900, 890, 910), // daleko iznad krivulje
		izdana(sat+3, 495, 485, 515), // gornja granica na produljenju
		izdana(sat+4, 300, 290, 700), // gornja granica izvan krivulje
	})
	protok := func(ciljni int64, v, d, g float64, izvan bool) Izdana {
		i := izdana(ciljni, v, d, g)
		i.Velicina, i.Racunata, i.Izvan = "protok", true, izvan
		return i
	}
	want := []Izdana{
		protok(sat+1, 200, 190, 210, false),
		protok(sat+3, 545, 535, 565, true),
		protok(sat+4, 350, 350, 350, false),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vodostaj u protok:\n%+v\nželim\n%+v", got, want)
	}

	// Protok u vodostaj; ispod umjerenog raspona krivulja se produlji i to
	// se označi.
	got = o.uDrugojVelicini(ctx, []Izdana{
		{Letva: "proba", Velicina: "protok", Izdano: sat, Ciljni: sat + 1, Vrijednost: 300, Dolje: 250, Gore: 350},
		{Letva: "proba", Velicina: "protok", Izdano: sat, Ciljni: sat + 2, Vrijednost: 60, Dolje: 40, Gore: 80},
	})
	if len(got) != 2 || got[0].Velicina != "vodostaj" || got[0].Vrijednost != 250 || got[0].Dolje != 200 || got[0].Gore != 300 || got[0].Izvan || !got[0].Racunata {
		t.Errorf("protok u vodostaj: %+v", got)
	}
	if len(got) == 2 && (got[1].Vrijednost != 10 || got[1].Dolje != -10 || !got[1].Izvan) {
		t.Errorf("protok ispod umjerenog: %+v", got[1])
	}

	// Bez izdanih, bez krivulje letve i bez arhive nema ništa.
	if got := o.uDrugojVelicini(ctx, nil); got != nil {
		t.Errorf("bez izdanih: %+v", got)
	}
	bez := izdana(sat+1, 150, 140, 160)
	bez.Letva = "bez-krivulje"
	if got := o.uDrugojVelicini(ctx, []Izdana{bez}); got != nil {
		t.Errorf("letva bez krivulje: %+v", got)
	}
	if got := (&Osvjezivac{}).uDrugojVelicini(ctx, []Izdana{izdana(sat+1, 150, 140, 160)}); got != nil {
		t.Errorf("bez arhive: %+v", got)
	}
}

// Vrh lanca stoji u satu izdavanja svojim mjerenjem, i kad kasni do
// ZaostatakVrha (tada zadnjim), uz istu vrijednost u drugoj veličini kad
// letva ima krivulju. Vrh koji kasni više ne stoji.
func TestSidraVrhovaKasnjenje(t *testing.T) {
	arhiva := arhivaKrivulja(t)
	dodajPravcuKrivulju(t, arhiva, 1, "kasni", "2000-01-01", "")
	o := &Osvjezivac{Arhiva: arhiva, Model: "proba"}
	sada := satU(2026, 10, 1)
	nizovi := map[Izvor]Niz{
		{Letva: "svjeza", Velicina: "vodostaj"}: ravanNiz(sada-10, sada, 100, 1),
		{Letva: "kasni", Velicina: "vodostaj"}:  ravanNiz(sada-10, sada-ZaostatakVrha, 150, 0),
		{Letva: "stoji", Velicina: "vodostaj"}:  ravanNiz(sada-10, sada-ZaostatakVrha-1, 300, 0),
	}
	vrhovi := map[Izvor]bool{}
	for iz := range nizovi {
		vrhovi[iz] = true
	}
	got := o.sidraVrhova(context.Background(), nizovi, vrhovi, sada)
	sort.Slice(got, func(i, j int) bool { return got[i].Letva+got[i].Velicina < got[j].Letva+got[j].Velicina })
	sidro := func(letva, vel string, v float64) Izdana {
		return Izdana{Letva: letva, Velicina: vel, Izdano: sada, Ciljni: sada, Vrijednost: v, Dolje: v, Gore: v, Model: "proba"}
	}
	q := sidro("kasni", "protok", 200)
	q.Racunata = true
	want := []Izdana{q, sidro("kasni", "vodostaj", 150), sidro("svjeza", "vodostaj", 110)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sidra:\n%+v\nželim\n%+v", got, want)
	}
}

// Krivulje se čitaju najnovija prva, s odsječcima redom; pokvaren redak
// krivulje ili odsječka, ili arhiva bez tablice odsječaka, javljaju grešku.
func TestKrivuljeIzArhiveGreske(t *testing.T) {
	ctx := context.Background()
	arhiva := arhivaKrivulja(t)
	dodajPravcuKrivulju(t, arhiva, 1, "proba", "2000-01-01", "2019-12-31")
	dodajPravcuKrivulju(t, arhiva, 2, "proba", "2020-01-01", "")
	k, err := KrivuljeIzArhive(ctx, arhiva, "proba")
	if err != nil || len(k) != 2 || k[0].ID != 2 || k[1].VrijediDo != "2019-12-31" || len(k[0].Odsjecci) != 1 || k[0].Odsjecci[0].P3 != 50 {
		t.Fatalf("krivulje %+v, %v", k, err)
	}

	if _, err := arhiva.Exec(`INSERT INTO hq_odsjecci VALUES (2, 'pet', 600, 'polinom', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := KrivuljeIzArhive(ctx, arhiva, "proba"); err == nil {
		t.Error("nečitljiv odsječak prošao je bez greške")
	}
	if _, err := arhiva.Exec(`INSERT INTO hq_krivulje VALUES ('tri', 'pokvarena', '2020-01-01', '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := KrivuljeIzArhive(ctx, arhiva, "pokvarena"); err == nil {
		t.Error("nečitljiva krivulja prošla je bez greške")
	}
	if _, err := arhiva.Exec(`DROP TABLE hq_odsjecci`); err != nil {
		t.Fatal(err)
	}
	if _, err := KrivuljeIzArhive(ctx, arhiva, "proba"); err == nil || !strings.Contains(err.Error(), "hq_odsjecci") {
		t.Errorf("bez tablice odsječaka: %v", err)
	}
}

// probniIshod je izdanje s jednom satnom, jednom dnevnom vrijednošću i
// rezervom u izboru.
func probniIshod(sada int64, letva string, v float64) *Ishod {
	return &Ishod{Sada: sada,
		Izdane: []Izdana{{Letva: letva, Velicina: "vodostaj", Izdano: sada, Ciljni: sada + 1, Vrijednost: v, Dolje: v - 1, Gore: v + 1, Model: ModelLanac}},
		Dnevne: []DnevnaIzdana{{Letva: letva, Izdano: sada, Dan: 1, Ciljni: sada + 24, Vrijednost: v, Dolje: v - 5, Gore: v + 5, Model: ModelDnevni}},
		Izbor:  map[string]Izbor{letva: {Inacica: 1, Opis: "rezerva: x umjesto y"}},
	}
}

// Ponovno izdavanje (Iznova) gazi staro izdanje cijelo: letva koja se više
// ne računa ne ostaje uz novo, a staro ide među ranije pod svojom verzijom.
func TestZapisiIznovaGaziStaroIzdanje(t *testing.T) {
	baza := praznaBazaPrognoza(t)
	const sada = int64(495000)
	o := &Osvjezivac{Baza: baza, Cvor: "cvor-proba"}
	prvi := probniIshod(sada, "stara", 100)
	if err := o.Zapisi(prvi); err != nil || prvi.Verzija != 1 {
		t.Fatalf("prvo izdanje: verzija %d, %v", prvi.Verzija, err)
	}
	o.Iznova = true
	drugi := probniIshod(sada, "nova", 200)
	if err := o.Zapisi(drugi); err != nil || drugi.Verzija != 2 {
		t.Fatalf("ponovno izdanje: verzija %d, %v", drugi.Verzija, err)
	}
	izdanje, err := Izdanje(baza, sada)
	if err != nil || len(izdanje) != 1 || len(izdanje["nova"]) != 1 {
		t.Fatalf("izdanje nakon ponovnog: %+v, %v", izdanje, err)
	}
	var ranijih, verzija int
	if err := baza.QueryRow(`SELECT count(*), max(verzija) FROM izdane_ranije WHERE letva = 'stara'`).Scan(&ranijih, &verzija); err != nil || ranijih != 1 || verzija != 1 {
		t.Errorf("ranije: %d redaka, verzija %d, %v", ranijih, verzija, err)
	}
	izbor, err := IzborIzdanja(baza, sada)
	if _, ima := izbor["stara"]; err != nil || ima || izbor["nova"].Inacica != 1 {
		t.Errorf("izbor nakon ponovnog: %+v, %v", izbor, err)
	}
	if cvor, _, _ := ZadnjiIzdavac(baza); cvor != "cvor-proba" {
		t.Errorf("izdavač %q", cvor)
	}
}

// Svaki korak zapisa javlja svoju grešku, a zapis o izdanju kaže da je
// zapeo on.
func TestZapisiJavljaGreskuSvakogKoraka(t *testing.T) {
	const sada = int64(495000)
	for _, s := range []struct {
		ime, tablica string
		iznova       bool
		poruka       string
	}{
		{"brisanje starog", "izdane_ranije", true, "izdane_ranije"},
		{"satne", "izdane", false, "izdana proba"},
		{"izbor", "izbor", false, "izbor"},
		{"dnevne", "dnevne", false, "dnevna proba"},
		{"zapis o izdanju", "izdanja", false, "zapis o izdanju"},
	} {
		baza := praznaBazaPrognoza(t)
		if _, err := baza.Exec(`DROP TABLE ` + s.tablica); err != nil {
			t.Fatal(err)
		}
		o := &Osvjezivac{Baza: baza, Iznova: s.iznova}
		ishod := probniIshod(sada, "proba", 100)
		if err := o.Zapisi(ishod); err == nil || !strings.Contains(err.Error(), s.poruka) {
			t.Errorf("%s: %v, želim grešku s %q", s.ime, err, s.poruka)
		}
		if ishod.Verzija != 0 {
			t.Errorf("%s: verzija %d iako zapis nije uspio", s.ime, ishod.Verzija)
		}
	}
	// prazno izdanje ne piše ništa, ni u bazu bez tablica
	baza := praznaBazaPrognoza(t)
	if _, err := baza.Exec(`DROP TABLE izdane`); err != nil {
		t.Fatal(err)
	}
	if err := (&Osvjezivac{Baza: baza}).Zapisi(&Ishod{Sada: sada}); err != nil {
		t.Errorf("prazno izdanje: %v", err)
	}
	if err := (&Osvjezivac{Baza: baza}).Zapisi(nil); err != nil {
		t.Errorf("bez ishoda: %v", err)
	}
}
