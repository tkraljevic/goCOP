package prognoza

import (
	"database/sql"
	"math"
	"slices"
	"testing"
)

// arhivaDnevnogGlavnog je arhiva s dnevnim nizovima cilja „donja”, uzvodne
// „gornja”, pritoke i kišomjera međusliva B, dovoljno duga za model iz programa.
func arhivaDnevnogGlavnog(t *testing.T) *sql.DB {
	t.Helper()
	arhiva, _ := arhivaUDatoteci(t)
	pritoka := func(d int64) float64 { return 120 + 50*math.Sin(float64(d)/5.3) }
	upisiDnevne(t, arhiva, "gornja", "vodostaj", 4500, valIzmisljeni)
	upisiDnevne(t, arhiva, "pritoka", "vodostaj", 4500, pritoka)
	upisiDnevne(t, arhiva, "donja", "vodostaj", 4500, func(d int64) float64 { return valIzmisljeni(d-2) + 0.4*pritoka(d-1) + 20 })
	upisiDnevne(t, arhiva, "tocka-b", "oborina", 4500, kisaIzmisljena)
	return arhiva
}

var tockeB = []OborinskaTocka{{Code: "tocka-b", Sliv: "B", Tezina: 1}}

// Dnevni blok letve ide od nacrta preko probe do retka u modelu. Proba mjeri
// nove ulaze i sadašnje na istim danima, primjena traži probu i razlog, a
// uklanjanje vraća letvu na program.
func TestNacrtProbaIPrimjenaDnevnogBloka(t *testing.T) {
	bezDnevnePostave(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"gornja"}, Slivovi: []string{"B"}}})
	arhiva, baza := arhivaDnevnogGlavnog(t), otvoriProbnu(t, "p.db")
	if err := SpremiDnevniNacrt(baza, NacrtDnevnog{Letva: "donja", Blok: DnevniLetve{Ulazi: []string{"donja"}}}); !porukaIma(err, "sama sebi ulaz") {
		t.Errorf("neispravan blok: %v", err)
	}
	if _, err := ProbajDnevniGlavni(arhiva, baza, tockeB, "donja", "08:00"); !porukaIma(err, "nema nacrt dnevnog modela") {
		t.Errorf("proba bez nacrta: %v", err)
	}
	nacrt := NacrtDnevnog{Letva: "donja", Tko: "Iva Proba", Kad: "07:55",
		Blok: DnevniLetve{Ulazi: []string{"gornja", "pritoka"}, Slivovi: []string{"B"}, OdDana: pok(3)}}
	if err := SpremiDnevniNacrt(baza, nacrt); err != nil {
		t.Fatal(err)
	}
	if satni, err := Nacrti(baza); err != nil || len(satni) != 0 {
		t.Errorf("nacrt dnevnog bloka nije nacrt satnog računa: %v, %v", satni, err)
	}
	promjena := PromjenaLetve{Kad: "22. 11. 2026. 17:00", Tko: "Iva Proba", Razlog: "pritoka nosi dio vala"}
	if err := PrimijeniDnevniGlavni(baza, "donja", promjena); !porukaIma(err, "nije proban") {
		t.Errorf("primjena bez probe: %v", err)
	}
	n, err := ProbajDnevniGlavni(arhiva, baza, tockeB, "donja", "08:00")
	if err != nil {
		t.Fatal(err)
	}
	p := n.Proba
	if p == nil || len(p.Dani) != DnevniDosezi || len(p.Prije) != DnevniDosezi || len(p.Razdoblje) != 2 || p.Dani[0].Slucaja == 0 ||
		!slices.Contains(p.Natpisi, NatpisCistiModel) || !slices.Contains(p.Natpisi, NatpisTocnaKisa) || slices.Contains(p.Natpisi, NatpisBezUzvodnog) {
		t.Fatalf("proba: %+v", p)
	}
	// pritoka objašnjava dio vala, pa je s njom prvi dan točniji
	if p.Dani[0].Promasaj >= p.Prije[0].Promasaj {
		t.Errorf("s pritokom mora biti točnije: %.2f prema %.2f", p.Dani[0].Promasaj, p.Prije[0].Promasaj)
	}
	if err := PrimijeniDnevniGlavni(baza, "donja", PromjenaLetve{Razlog: "  "}); !porukaIma(err, "upiši razlog") {
		t.Errorf("primjena bez razloga: %v", err)
	}
	if err := PrimijeniDnevniGlavni(baza, "donja", promjena); err != nil {
		t.Fatal(err)
	}
	zapisi, _, err := ZapisiLetvi(baza)
	z := zapisi["donja"]
	blok, errBlok := z.DnevniBlok()
	if err != nil || errBlok != nil || z.Metoda != MetodaDnevni || !slices.Equal(blok.Ulazi, []string{"gornja", "pritoka"}) || *blok.OdDana != 3 ||
		len(z.Promjene) != 1 || z.Promjene[0].Razlog != "pritoka nosi dio vala" {
		t.Fatalf("redak nakon primjene: %+v, %v %v", z, err, errBlok)
	}
	if nacrti, err := DnevniNacrti(baza); err != nil || len(nacrti) != 0 {
		t.Errorf("nacrt se nakon primjene briše: %v, %v", nacrti, err)
	}
	if c, _ := sadasnjiDnevniCilj("donja"); !slices.Equal(c.Ulazi, []string{"gornja", "pritoka"}) || !DnevniRacunIzZapisa("donja") {
		t.Errorf("primijenjeni blok mora odmah vrijediti: %+v", c)
	}
	if d, _ := DnevnaOdDanaLetve("donja"); d != 3 {
		t.Errorf("dan prijelaza iz primijenjenog bloka: %d", d)
	}
	if err := UkloniDnevniGlavni(baza, "donja", PromjenaLetve{}); !porukaIma(err, "upiši razlog") {
		t.Errorf("uklanjanje bez razloga: %v", err)
	}
	if err := UkloniDnevniGlavni(baza, "donja", promjena); err != nil {
		t.Fatal(err)
	}
	if zapisi, _, err := ZapisiLetvi(baza); err != nil || len(zapisi) != 0 || DnevniRacunIzZapisa("donja") {
		t.Errorf("redak samo s dnevnim blokom nakon uklanjanja nestaje: %v, %v", zapisi, err)
	}
	if err := UkloniDnevniGlavni(baza, "donja", promjena); !porukaIma(err, "nema dnevni model iz zapisa") {
		t.Errorf("uklanjanje bez bloka: %v", err)
	}
}

// Letva glavnog lanca dobiva dnevni blok u retku uz svoj satni račun iz
// programa; uklanjanje bloka ostavlja redak, a primjena satnog računa blok.
func TestDnevniBlokULetviGlavnogLanca(t *testing.T) {
	bezDnevnePostave(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"gornja"}, Slivovi: []string{"B"}}})
	tokoviProbe(t, Racun{Letva: "donja", Ulazi: []string{"gornja"}})
	postavi(t, &Velicine, map[string]string{"donja": "vodostaj", "gornja": "vodostaj"})
	postavi(t, &Rezerve, map[string][][]string{})
	arhiva, baza := arhivaDnevnogGlavnog(t), otvoriProbnu(t, "p.db")
	if err := SpremiDnevniNacrt(baza, NacrtDnevnog{Letva: "donja", Blok: DnevniLetve{Ulazi: []string{"gornja", "pritoka"}, Slivovi: []string{"B"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajDnevniGlavni(arhiva, baza, tockeB, "donja", "08:00"); err != nil {
		t.Fatal(err)
	}
	promjena := PromjenaLetve{Kad: "17:00", Tko: "Iva Proba", Razlog: "pritoka"}
	if err := PrimijeniDnevniGlavni(baza, "donja", promjena); err != nil {
		t.Fatal(err)
	}
	glavni, odbijeni, err := GlavniZapisi(baza, OkvirModela{})
	z := glavni["donja"]
	if err != nil || len(odbijeni) != 0 || z.Metoda != MetodaLanac || !slices.Equal(z.Racun.Ulazi, []string{"gornja"}) || !DnevniRacunIzZapisa("donja") {
		t.Fatalf("redak letve glavnog lanca s dnevnim blokom: %+v, %v, %v", z, odbijeni, err)
	}
	// primjena satnog računa ne dira dnevni blok
	satni := glavniZapis("donja", "vodostaj", "gornja")
	if novi := zapisZaGlavnu(satni, glavni, promjena); string(novi.Dnevni) != string(z.Dnevni) || len(novi.Promjene) != 2 {
		t.Errorf("dnevni blok nakon primjene satnog računa: %s", novi.Dnevni)
	}
	if err := UkloniDnevniGlavni(baza, "donja", promjena); err != nil {
		t.Fatal(err)
	}
	glavni, _, err = GlavniZapisi(baza, OkvirModela{})
	if err != nil || len(glavni) != 1 || len(glavni["donja"].Promjene) != 2 || DnevniRacunIzZapisa("donja") {
		t.Errorf("redak nakon uklanjanja dnevnog bloka: %+v, %v", glavni, err)
	}
}

// Proba se odbija za letvu koja nije cilj dnevnog modela i kad se arhiva ne da
// pročitati; sadašnji model koji se ne da izmjeriti ostavlja probu bez usporedbe.
func TestProbaDnevnogBlokaRubovi(t *testing.T) {
	bezDnevnePostave(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"nema-arhive"}}})
	arhiva, baza := arhivaDnevnogGlavnog(t), otvoriProbnu(t, "p.db")
	if err := SpremiDnevniNacrt(baza, NacrtDnevnog{Letva: "donja", Blok: DnevniLetve{Ulazi: []string{"gornja"}}}); err != nil {
		t.Fatal(err)
	}
	n, err := ProbajDnevniGlavni(arhiva, baza, nil, "donja", "08:00")
	if err != nil || len(n.Proba.Dani) != DnevniDosezi || n.Proba.Prije != nil {
		t.Errorf("proba kad se sadašnji model ne da izmjeriti: %+v, %v", n.Proba, err)
	}
	if err := SpremiDnevniNacrt(baza, NacrtDnevnog{Letva: "donja", Blok: DnevniLetve{Ulazi: []string{"bez-niza"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajDnevniGlavni(arhiva, baza, nil, "donja", "08:00"); err == nil {
		t.Error("ulaz bez dnevnog niza mora javiti grešku")
	}
	// nacrt letve koja više nije cilj dnevnog modela
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "druga", Ulazi: []string{"gornja"}}})
	if _, err := ProbajDnevniGlavni(arhiva, baza, nil, "donja", "08:00"); !porukaIma(err, "nije cilj dnevnog modela") {
		t.Errorf("letva izvan dnevnog modela: %v", err)
	}
	if err := PrimijeniDnevniGlavni(baza, "donja", PromjenaLetve{Razlog: "proba"}); err == nil {
		t.Error("primjena nacrta bez probe ili izvan modela mora javiti grešku")
	}
	if _, err := baza.Exec(`INSERT INTO model_nacrt (letva, sadrzaj) VALUES ('dnevni:pokvaren', '{nije json')`); err != nil {
		t.Fatal(err)
	}
	if nacrti, err := DnevniNacrti(baza); err != nil || len(nacrti) != 1 {
		t.Errorf("pokvaren redak nacrta se preskače: %v, %v", nacrti, err)
	}
	if err := arhiva.Close(); err != nil {
		t.Fatal(err)
	}
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"gornja"}}})
	if _, err := ProbajDnevniGlavni(arhiva, baza, nil, "donja", "08:00"); err == nil {
		t.Error("zatvorena arhiva mora javiti grešku")
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	for ime, err := range map[string]error{
		"nacrti":   func() error { _, err := DnevniNacrti(baza); return err }(),
		"proba":    func() error { _, err := ProbajDnevniGlavni(arhiva, baza, nil, "donja", "08:00"); return err }(),
		"primjena": PrimijeniDnevniGlavni(baza, "donja", PromjenaLetve{Razlog: "proba"}),
		"ukloni":   UkloniDnevniGlavni(baza, "donja", PromjenaLetve{Razlog: "proba"}),
		"brisanje": ObrisiDnevniNacrt(baza, "donja"),
		"spremi":   SpremiDnevniNacrt(baza, NacrtDnevnog{Letva: "donja", Blok: DnevniLetve{Ulazi: []string{"gornja"}}}),
	} {
		if err == nil {
			t.Errorf("%s: zatvorena baza mora javiti grešku", ime)
		}
	}
}

// Letvi s ispravkom po uzvodnim letvama proba kaže da on uz druge ulaze prestaje
// vrijediti; uz iste ulaze ne kaže.
func TestNatpisBezUzvodnog(t *testing.T) {
	iz, _ := ciljIzPrograma("batina")
	isti := DnevniCilj{Letva: "batina", Ulazi: slices.Clone(iz.Ulazi), Slivovi: slices.Clone(iz.Slivovi)}
	if slices.Contains(natpisiProbeBloka(nil, isti), NatpisBezUzvodnog) {
		t.Error("isti ulazi ne gase uzvodni ispravak")
	}
	drugi := DnevniCilj{Letva: "batina", Ulazi: []string{"mohacs"}, Slivovi: slices.Clone(iz.Slivovi)}
	if n := natpisiProbeBloka([]string{NatpisTocnaKisa}, drugi); !slices.Contains(n, NatpisBezUzvodnog) || n[0] != NatpisTocnaKisa {
		t.Errorf("drugi ulazi gase uzvodni ispravak: %v", n)
	}
}

// Grane grešaka: nacrt koji se ne da zapisati, tablica nacrta i redaka koja se
// ne da pročitati, arhiva koja se ne da pročitati.
func TestDnevniBlokGreskeCitanjaIPisanja(t *testing.T) {
	bezDnevnePostave(t)
	postavi(t, &DnevniCiljevi, []DnevniCilj{{Letva: "donja", Ulazi: []string{"gornja"}}})
	arhiva, baza := arhivaDnevnogGlavnog(t), otvoriProbnu(t, "p.db")
	if err := upisiDnevniNacrt(baza, NacrtDnevnog{Letva: "donja", Blok: DnevniLetve{VisokaVoda: pok(math.NaN())}}); err == nil {
		t.Error("nacrt koji se ne da zapisati mora javiti grešku")
	}
	if err := SpremiDnevniNacrt(baza, NacrtDnevnog{Letva: "donja", Blok: DnevniLetve{Ulazi: []string{"gornja", "pritoka"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbajDnevniGlavni(arhiva, baza, nil, "donja", "08:00"); err != nil {
		t.Fatal(err)
	}
	// redci modela koji se ne daju pročitati prekidaju primjenu i uklanjanje
	if _, err := baza.Exec(`DROP TABLE model_letve; CREATE TABLE model_letve (a TEXT, b TEXT, c TEXT)`); err != nil {
		t.Fatal(err)
	}
	promjena := PromjenaLetve{Razlog: "proba"}
	if err := PrimijeniDnevniGlavni(baza, "donja", promjena); err == nil {
		t.Error("nečitljivi redci moraju prekinuti primjenu")
	}
	if err := UkloniDnevniGlavni(baza, "donja", promjena); err == nil {
		t.Error("nečitljivi redci moraju prekinuti uklanjanje")
	}
	if n, err := DnevniNacrti(baza); err != nil || n["donja"].Proba == nil {
		t.Errorf("nacrt nakon neuspjele primjene ostaje: %v", err)
	}
	// tablica nacrta s retkom koji se ne da pročitati i bez stupca
	if _, err := baza.Exec(`DROP TABLE model_nacrt; CREATE TABLE model_nacrt (letva TEXT, sadrzaj TEXT);
		INSERT INTO model_nacrt VALUES ('dnevni:donja', NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := DnevniNacrti(baza); err == nil {
		t.Error("redak bez sadržaja mora javiti grešku")
	}
	if _, err := baza.Exec(`DROP TABLE model_nacrt; CREATE TABLE model_nacrt (nesto TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := DnevniNacrti(baza); err == nil {
		t.Error("tablica bez stupaca mora javiti grešku")
	}
	if err := arhiva.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := izmjeriDnevniCilj(arhiva, nil, DnevniCilj{Letva: "donja", Ulazi: []string{"gornja"}}, 3000, 3700); err == nil {
		t.Error("zatvorena arhiva mora javiti grešku")
	}
	if err := redomDoGreske(); err != nil {
		t.Errorf("bez koraka nema greške: %v", err)
	}
}
