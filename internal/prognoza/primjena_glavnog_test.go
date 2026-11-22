package prognoza

import (
	"bytes"
	"database/sql"
	"strings"
	"testing"
)

// probaniNacrtCilja sprema, priprema i proba nacrt letve cilj.
func probaniNacrtCilja(t *testing.T, arhiva, baza *sql.DB, z ZapisLetve, ime string) NacrtLetve {
	t.Helper()
	nacrtGlavne(t, arhiva, baza, z)
	n, err := ProbajGlavniNacrt(arhiva, baza, otvoriProbnu(t, ime+"-prije.db"), otvoriProbnu(t, ime+"-poslije.db"), "cilj", "08:05", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// zapisSKasnjenjem je račun cilja s jednom razinom i upisanim kašnjenjem.
func zapisSKasnjenjem(sati int) ZapisLetve {
	z := glavniZapis("cilj", "protok", "ulaz")
	z.Racun.Razine = RazineLetve{Postoci: []float64{0, 100}}
	z.Racun.Rucno.Kasnjenje = []RucnoKasnjenje{{Razina: 0, Sati: sati}}
	return z
}

// Primjena upiše račun letve glavnog lanca u tablice modela, promašaje letvi
// kojima ih je proba promijenila i redak letve, sve odjednom. Ostali računi
// ostaju isti, nacrt ostaje, a vraćeni model poništava primjenu.
func TestPrimjenaGlavneLetve(t *testing.T) {
	arhiva, baza := lanacSNizvodnom(t)
	_, polazni, err := SastaviModel(baza)
	if err != nil {
		t.Fatal(err)
	}
	nizvodnaPrije, err := InaciceLetve(baza, "nizvodna")
	if err != nil {
		t.Fatal(err)
	}
	promjena := PromjenaLetve{Kad: "22. 11. 2026. 15:00", Tko: "Iva Proba", Razlog: "proba kašnjenja"}
	nacrtGlavne(t, arhiva, baza, zapisSKasnjenjem(2))
	if _, err := PrimijeniGlavniNacrt(baza, otvoriProbnu(t, "r0.db"), "cilj", promjena, OkvirModela{}); !porukaIma(err, "nije proban") {
		t.Errorf("primjena bez probe: %v", err)
	}
	n := probaniNacrtCilja(t, arhiva, baza, zapisSKasnjenjem(2), "a")
	if _, err := PrimijeniGlavniNacrt(baza, otvoriProbnu(t, "r1.db"), "cilj", PromjenaLetve{Razlog: " "}, OkvirModela{}); !porukaIma(err, "upiši razlog") {
		t.Errorf("primjena bez razloga: %v", err)
	}
	if _, err := PrimijeniGlavniNacrt(baza, baza, "cilj", promjena, OkvirModela{}); !porukaIma(err, "nije nova") {
		t.Errorf("radna baza s modelom: %v", err)
	}
	bezSektora := OkvirModela{ImaSektor: func(string) bool { return false }}
	if _, err := PrimijeniGlavniNacrt(baza, otvoriProbnu(t, "r2.db"), "cilj", promjena, bezSektora); !porukaIma(err, "nema sektor") {
		t.Errorf("primjena nacrta koji ne prolazi provjeru: %v", err)
	}
	prije, err := PrimijeniGlavniNacrt(baza, otvoriProbnu(t, "r3.db"), "cilj", promjena, OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	in, err := InaciceLetve(baza, "cilj")
	if err != nil || len(in) != 1 || len(in[0]) != 1 || in[0][0].Ulazi[0].PomakH != 2 {
		t.Fatalf("račun letve nakon primjene: %+v, %v", in, err)
	}
	if nizvodna, err := InaciceLetve(baza, "nizvodna"); err != nil || len(nizvodna) != len(nizvodnaPrije) ||
		nizvodna[0][0].Ulazi[0] != nizvodnaPrije[0][0].Ulazi[0] || nizvodna[0][0].Odsjecak != nizvodnaPrije[0][0].Odsjecak {
		t.Errorf("račun druge letve se promijenio: %+v", nizvodna)
	}
	promasaji, err := Promasaji(baza)
	if err != nil {
		t.Fatal(err)
	}
	for kljuc, zeljeni := range n.Proba.Lanac.Promasaji {
		letva, velicina, _ := strings.Cut(kljuc, "|")
		upisani := promasaji[Izvor{Letva: letva, Velicina: velicina}]
		if len(upisani) != len(zeljeni) || upisani[24].Rasap != zeljeni[24].Rasap {
			t.Errorf("promašaji letve %s: %d upisanih, %d iz probe", letva, len(upisani), len(zeljeni))
		}
	}
	glavni, odbijeni, err := GlavniZapisi(baza, OkvirModela{})
	redak := glavni["cilj"]
	if err != nil || len(odbijeni) != 0 || len(redak.Brojevi.Inacice) != 0 || len(redak.Promjene) != 1 || redak.Promjene[0].Razlog != "proba kašnjenja" ||
		redak.Racun.Rucno.Kasnjenje[0].Sati != 2 || !ImaPrethodniZapis(redak) {
		t.Fatalf("redak letve: %+v, odbijeni %v, %v", redak, odbijeni, err)
	}
	if nacrti, err := Nacrti(baza); err != nil || nacrti["cilj"].Proba == nil {
		t.Errorf("nacrt mora ostati dok izdavanje ne uspije: %v", err)
	}
	// prije prvog retka prethodni je račun iz programa
	prethodni, err := PrethodniGlavni(baza, "cilj")
	if err != nil || prethodni.Racun.Ulazi[0] != "ulaz" || len(prethodni.Racun.Rucno.Kasnjenje) != 0 || len(prethodni.Promjene) != 0 {
		t.Errorf("prethodni račun: %+v, %v", prethodni, err)
	}
	for _, letva := range []string{"nizvodna", "ulaz"} {
		if _, err := PrethodniGlavni(baza, letva); !porukaIma(err, "nema prethodnu inačicu") {
			t.Errorf("prethodni račun letve %s: %v", letva, err)
		}
	}
	// proba je mjerila stari model, pa isti nacrt više ne vrijedi
	if _, err := PrimijeniGlavniNacrt(baza, otvoriProbnu(t, "r4.db"), "cilj", promjena, OkvirModela{}); !porukaIma(err, "promijenio nakon probe") {
		t.Errorf("primjena nakon promjene modela: %v", err)
	}
	// Pripremi model poslije namješta letvu iz retka
	var w bytes.Buffer
	if _, err := NamjestiLanac(arhiva, baza, OpcijeNamjestanja{Dnevnik: &w}); err != nil || !strings.Contains(w.String(), "cilj ← ulaz (iz zapisa)") {
		t.Errorf("Pripremi model nakon primjene: %v\n%s", err, w.String())
	}
	if in, err := InaciceLetve(baza, "cilj"); err != nil || in[0][0].Ulazi[0].PomakH != 2 {
		t.Errorf("Pripremi model mora zadržati kašnjenje iz retka: %+v, %v", in, err)
	}
	// druga primjena pamti prvu kao prethodnu
	probaniNacrtCilja(t, arhiva, baza, zapisSKasnjenjem(4), "b")
	druga := PromjenaLetve{Kad: "22. 11. 2026. 16:00", Tko: "Iva Proba", Razlog: "bliže izmjerenom"}
	if _, err := PrimijeniGlavniNacrt(baza, otvoriProbnu(t, "r5.db"), "cilj", druga, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	prethodni, err = PrethodniGlavni(baza, "cilj")
	if err != nil || prethodni.Racun.Rucno.Kasnjenje[0].Sati != 2 {
		t.Errorf("prethodni nakon druge primjene: %+v, %v", prethodni, err)
	}
	if glavni, _, err := GlavniZapisi(baza, OkvirModela{}); err != nil || len(glavni["cilj"].Promjene) != 2 {
		t.Errorf("promjene nakon druge primjene: %+v, %v", glavni["cilj"].Promjene, err)
	}
	// model kakav je bio prije prve primjene poništava sve
	if err := PrimiModel(baza, prije); err != nil {
		t.Fatal(err)
	}
	if _, sad, err := SastaviModel(baza); err != nil || sad != polazni {
		t.Errorf("vraćeni model nije polazni: %v", err)
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := PrimijeniGlavniNacrt(baza, otvoriProbnu(t, "r6.db"), "cilj", promjena, OkvirModela{}); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
	if _, err := PrethodniGlavni(baza, "cilj"); err == nil {
		t.Error("zatvorena baza mora javiti grešku i za prethodni račun")
	}
}

// Greška u slaganju novog modela ne dira model u bazi.
func TestPrimjenaGlavneLetveNeOstavljaPolaModela(t *testing.T) {
	arhiva, baza := lanacSNizvodnom(t)
	_, polazni, err := SastaviModel(baza)
	if err != nil {
		t.Fatal(err)
	}
	probaniNacrtCilja(t, arhiva, baza, zapisSKasnjenjem(2), "a")
	radna := otvoriProbnu(t, "radna.db")
	if _, err := radna.Exec(`DROP TABLE promasaji`); err != nil {
		t.Fatal(err)
	}
	promjena := PromjenaLetve{Kad: "15:00", Tko: "Iva Proba", Razlog: "proba"}
	if _, err := PrimijeniGlavniNacrt(baza, radna, "cilj", promjena, OkvirModela{}); err == nil {
		t.Fatal("radna baza bez tablice mora javiti grešku")
	}
	if _, sad, err := SastaviModel(baza); err != nil || sad != polazni {
		t.Errorf("neuspjela primjena promijenila je model: %v", err)
	}
	prije, _, err := SastaviModel(baza)
	if err != nil {
		t.Fatal(err)
	}
	n, err := probaniGlavniNacrt(baza, "cilj")
	if err != nil {
		t.Fatal(err)
	}
	zatvorena := otvoriProbnu(t, "zatvorena.db")
	if err := zatvorena.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sloziNoviModel(zatvorena, prije, n, n.Zapis); err == nil {
		t.Error("zatvorena radna baza mora javiti grešku")
	}
	if _, err := zamijeniRacun(zatvorena, n.Zapis); err == nil {
		t.Error("zatvorena baza mora javiti grešku pri zamjeni računa")
	}
	// redci modela koji se ne daju pročitati prekidaju primjenu, pripremu i provjeru nacrta
	if _, err := baza.Exec(`DROP TABLE model_letve; CREATE TABLE model_letve (a TEXT, b TEXT, c TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := PrimijeniGlavniNacrt(baza, otvoriProbnu(t, "r7.db"), "cilj", promjena, OkvirModela{}); err == nil {
		t.Error("nečitljivi redci moraju prekinuti primjenu")
	}
	if _, err := PripremiGlavnuLetvu(arhiva, baza, n.Zapis, OkvirModela{}); err == nil {
		t.Error("nečitljivi redci moraju prekinuti pripremu")
	}
	if err := ProvjeriNacrt(baza, n.Zapis, OkvirModela{}); err == nil {
		t.Error("nečitljivi redci moraju prekinuti provjeru nacrta")
	}
	// model koji se ne da pročitati prekida primjenu i probu
	if _, err := baza.Exec(`DROP TABLE operateri`); err != nil {
		t.Fatal(err)
	}
	if _, err := PrimijeniGlavniNacrt(baza, otvoriProbnu(t, "r8.db"), "cilj", promjena, OkvirModela{}); err == nil {
		t.Error("model koji se ne da pročitati mora prekinuti primjenu")
	}
	if _, err := ProbajGlavniNacrt(arhiva, baza, otvoriProbnu(t, "r9.db"), otvoriProbnu(t, "r10.db"), "cilj", "08:05", func(string) {}); err == nil {
		t.Error("model koji se ne da pročitati mora prekinuti probu")
	}
	if err := zamijeniPromasaje(zatvorena, nil, "15:00"); err == nil {
		t.Error("zatvorena baza mora javiti grešku pri upisu promašaja")
	}
}

// Promašaji letvi koje proba nije promijenila ostaju kakvi su; ostalima se
// zamijene svi dosezi, i oni kojih u probi više nema.
func TestZamjenaPromasajaSamoPromijenjenih(t *testing.T) {
	baza := otvoriProbnu(t, "p.db")
	if err := SpremiPromasaje(baza, []Promasaj{
		{Letva: "cilj", Velicina: "protok", DosegH: 24, Rasap: 5, Slucaja: 200}, {Letva: "cilj", Velicina: "protok", DosegH: 48, Rasap: 8, Slucaja: 200},
		{Letva: "druga", Velicina: "vodostaj", DosegH: 24, Rasap: 3, Slucaja: 200}}, "staro"); err != nil {
		t.Fatal(err)
	}
	novi := map[string]map[int]Promasaj{"cilj|protok": {24: {Letva: "cilj", Velicina: "protok", DosegH: 24, Rasap: 4, Slucaja: 210}}}
	if err := zamijeniPromasaje(baza, novi, "novo"); err != nil {
		t.Fatal(err)
	}
	p, err := Promasaji(baza)
	cilj, druga := p[Izvor{Letva: "cilj", Velicina: "protok"}], p[Izvor{Letva: "druga", Velicina: "vodostaj"}]
	if err != nil || len(cilj) != 1 || cilj[24].Rasap != 4 || len(druga) != 1 || druga[24].Rasap != 3 {
		t.Errorf("promašaji nakon zamjene: %v, %v", p, err)
	}
}
