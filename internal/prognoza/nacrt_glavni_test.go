package prognoza

import (
	"bytes"
	"database/sql"
	"math"
	"testing"
	"time"
)

// lanacSNizvodnom je lanac probe cilj ← ulaz i nizvodna ← cilj, namješten u bazi
// prognoza, s arhivom i postavkama probe koje pristaju arhivi.
func lanacSNizvodnom(t *testing.T) (arhiva, baza *sql.DB) {
	t.Helper()
	postaviLanacProbe(t)
	tokoviProbe(t, Racun{Letva: "cilj:protok", Ulazi: []string{"ulaz:protok"}},
		Racun{Letva: "nizvodna:protok", Ulazi: []string{"cilj:protok"}})
	postavi(t, &Operateri, nil)
	postavi(t, &NamjestiDo, int64(0))
	postavi(t, &opcijeProbeLanca, func() OpcijeProvjere {
		return OpcijeProvjere{Od: time.Unix(1000*3600, 0), Do: time.Unix(5000*3600, 0), Korak: 12, Najdalje: 96, Glacenje: 6}
	})
	nizovi := arhivaIzmisljeneVeze(t)
	nizvodna := map[int64]float64{}
	for h := int64(12); h < 6000; h++ {
		nizvodna[h] = 0.5*nizovi["cilj:protok"][h-7] + 20 + 3*math.Sin(float64(h)/5)
	}
	nizovi["nizvodna:protok"] = nizvodna
	arhiva = arhivaSatnih(t, nizovi)
	baza = otvoriProbnu(t, "p.db")
	var w bytes.Buffer
	if _, err := NamjestiLanac(arhiva, baza, OpcijeNamjestanja{Dnevnik: &w}); err != nil {
		t.Fatalf("%v\n%s", err, w.String())
	}
	return arhiva, baza
}

// nacrtGlavne sprema i priprema nacrt letve cilj.
func nacrtGlavne(t *testing.T, arhiva, baza *sql.DB, z ZapisLetve) NacrtLetve {
	t.Helper()
	if err := SpremiNacrt(baza, NacrtLetve{Zapis: z, Tko: "Iva Proba", Kad: "08:00"}, OkvirModela{}); err != nil {
		t.Fatal(err)
	}
	n, err := PripremiNacrt(arhiva, baza, z.Letva, "08:01", OkvirModela{})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Nacrt letve glavnog lanca sprema se i priprema kao i svaki drugi, ali po
// pravilima lanca: doseg je cijelo izdanje, a brojevi ostaju u nacrtu.
func TestPripremaNacrtaGlavneLetve(t *testing.T) {
	arhiva, baza := lanacSNizvodnom(t)
	if err := SpremiNacrt(baza, NacrtLetve{Zapis: glavniZapis("cilj", "vodostaj", "ulaz")}, OkvirModela{}); !porukaIma(err, "vodi kao protok") {
		t.Errorf("nacrt s krivom veličinom: %v", err)
	}
	n := nacrtGlavne(t, arhiva, baza, glavniZapis("cilj", "protok", "ulaz"))
	if n.Priprema == nil || n.Zapis.Racun.Doseg != najveciDosegZapisa || len(n.Zapis.Brojevi.Inacice) != 1 ||
		n.Zapis.Brojevi.Inacice[0][0].Ulazi[0].PomakH != 5 || n.Priprema.Ucenje.Sati == 0 || len(n.Priprema.Cvorovi) < 2 {
		t.Errorf("pripremljen nacrt: %+v", n)
	}
	uzivo, err := InaciceLetve(baza, "cilj")
	if err != nil || len(uzivo) != 1 || uzivo[0][0].Ulazi[0].PomakH != 5 {
		t.Errorf("priprema ne smije dirati model: %+v, %v", uzivo, err)
	}
	namjestanjeTraje.Lock()
	_, err = PripremiGlavnuLetvu(arhiva, baza, n.Zapis, OkvirModela{})
	namjestanjeTraje.Unlock()
	if !porukaIma(err, "već traje") {
		t.Errorf("priprema dok traje namještanje: %v", err)
	}
	if _, err := PripremiGlavnuLetvu(arhiva, baza, glavniZapis("cilj", "vodostaj", "ulaz"), OkvirModela{}); !porukaIma(err, "vodi kao protok") {
		t.Errorf("priprema krive veličine: %v", err)
	}
	if _, err := PripremiGlavnuLetvu(arhiva, baza, glavniZapis("cilj", "protok", "nema-niza"), OkvirModela{}); err == nil {
		t.Error("ulaz bez niza mora javiti grešku")
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := PripremiGlavnuLetvu(arhiva, baza, n.Zapis, OkvirModela{}); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
	if err := ProvjeriNacrt(baza, n.Zapis, OkvirModela{}); err == nil {
		t.Error("provjera nacrta uz zatvorenu bazu mora javiti grešku")
	}
}

// Proba letve glavnog lanca mjeri cijeli lanac sadašnjim modelom i modelom s
// izmjenom. Izmjena koja kvari račun vidi se na letvi i nizvodno, a promašaji
// za primjenu su samo oni koji su se promijenili. Model se ne mijenja.
func TestProbaNacrtaGlavneLetve(t *testing.T) {
	arhiva, baza := lanacSNizvodnom(t)
	_, otisak, err := SastaviModel(baza)
	if err != nil {
		t.Fatal(err)
	}
	// kašnjenje upisano krivo: voda stiže za 5 sati, a račun tvrdi da stiže odmah
	krivo := glavniZapis("cilj", "protok", "ulaz")
	krivo.Racun.Razine = RazineLetve{Postoci: []float64{0, 100}}
	krivo.Racun.Rucno.Kasnjenje = []RucnoKasnjenje{{Razina: 0, Sati: 0}}
	nacrtGlavne(t, arhiva, baza, krivo)
	var koraci []string
	n, err := ProbajGlavniNacrt(arhiva, baza, otvoriProbnu(t, "prije.db"), otvoriProbnu(t, "poslije.db"), "cilj", "08:05",
		func(sto string) { koraci = append(koraci, sto) })
	if err != nil {
		t.Fatal(err)
	}
	l := n.Proba.Lanac
	if l == nil || len(koraci) != 2 || l.Model != otisak || len(n.Proba.Dosezi) == 0 || len(l.Prije) != len(n.Proba.Dosezi) {
		t.Fatalf("proba: %+v, koraci %v", n.Proba, koraci)
	}
	if prije, poslije := l.Prije[0], n.Proba.Dosezi[0]; prije.Doseg != 6 || poslije.Promasaj <= prije.Promasaj || prije.Slucaja == 0 {
		t.Errorf("krivo kašnjenje mora povećati promašaj: prije %+v, poslije %+v", prije, poslije)
	}
	if len(l.Nizvodno) == 0 || l.Nizvodno[0].Letva != "nizvodna" || l.Nizvodno[0].Doseg != 24 || l.Nizvodno[0].Poslije <= l.Nizvodno[0].Prije {
		t.Errorf("nizvodno: %+v", l.Nizvodno)
	}
	if len(l.Promasaji) != 2 || len(l.Promasaji["cilj|protok"]) == 0 || len(l.Promasaji["nizvodna|protok"]) == 0 {
		t.Errorf("promašaji za primjenu: %v", l.Promasaji)
	}
	if _, sad, err := SastaviModel(baza); err != nil || sad != otisak {
		t.Errorf("proba ne smije mijenjati model: %v", err)
	}
	nacrti, err := Nacrti(baza)
	if err != nil || nacrti["cilj"].Proba == nil || nacrti["cilj"].Proba.Lanac == nil {
		t.Errorf("proba nije upisana u nacrt: %v", err)
	}

	// izmjena koja ništa ne mijenja: letvi isti brojevi, nizvodno ništa
	nacrtGlavne(t, arhiva, baza, glavniZapis("cilj", "protok", "ulaz"))
	n, err = ProbajGlavniNacrt(arhiva, baza, otvoriProbnu(t, "prije2.db"), otvoriProbnu(t, "poslije2.db"), "cilj", "08:10", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if l := n.Proba.Lanac; len(l.Nizvodno) != 0 || len(l.Promasaji) != 1 || l.Prije[0].Promasaj != n.Proba.Dosezi[0].Promasaj {
		t.Errorf("proba bez izmjene: %+v", l)
	}
}

// Proba se odbija kad račun nije pripremljen, kad letva nije u lancu i kad
// privremena baza nije nova; greška čitanja se vraća.
func TestProbaGlavneLetveOdbija(t *testing.T) {
	arhiva, baza := lanacSNizvodnom(t)
	proba := func(letva string, prije, poslije *sql.DB) error {
		_, err := ProbajGlavniNacrt(arhiva, baza, prije, poslije, letva, "08:05", func(string) {})
		return err
	}
	if err := proba("cilj", otvoriProbnu(t, "a.db"), otvoriProbnu(t, "b.db")); !porukaIma(err, "nije pripremljen") {
		t.Errorf("bez nacrta: %v", err)
	}
	if err := proba("ulaz", otvoriProbnu(t, "c.db"), otvoriProbnu(t, "d.db")); !porukaIma(err, "nije u glavnom lancu") {
		t.Errorf("letva izvan lanca: %v", err)
	}
	nacrtGlavne(t, arhiva, baza, glavniZapis("cilj", "protok", "ulaz"))
	if err := proba("cilj", baza, otvoriProbnu(t, "e.db")); !porukaIma(err, "nije nova") {
		t.Errorf("privremena baza s modelom: %v", err)
	}
	zatvorena := otvoriProbnu(t, "f.db")
	if err := zatvorena.Close(); err != nil {
		t.Fatal(err)
	}
	if err := proba("cilj", otvoriProbnu(t, "g.db"), zatvorena); err == nil {
		t.Error("zatvorena privremena baza mora javiti grešku")
	}
	if err := arhiva.Close(); err != nil {
		t.Fatal(err)
	}
	if err := proba("cilj", otvoriProbnu(t, "h.db"), otvoriProbnu(t, "i.db")); err == nil {
		t.Error("zatvorena arhiva mora javiti grešku")
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if err := proba("cilj", otvoriProbnu(t, "j.db"), otvoriProbnu(t, "k.db")); err == nil {
		t.Error("zatvorena baza mora javiti grešku")
	}
}
