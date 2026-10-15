package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Dnevni list prenesen iz ranije evidencije (BP16) i list s drugog čvora za
// isti dan, Odluke (8. 10. 2026.), pitanje 7. Oznaka lista izvedena je iz
// osobe i dana i za prenesen list, pa uvoz na poslužitelju i list koji je
// drugi čvor napisao za isti dan pišu isti zapis. Prenesen list je zaključen
// prijenosom, kao predan list: nacrt s drugog čvora i brisanje nacrta ga ne
// zamjenjuju, a bilješke s nacrta ostaju na njemu. Osobe su izmišljene:
// vodočuvar Ivo Ivić, rukovoditeljica Ana Anić.

// oznakaObilaska je oznaka zadatka obilaska iz BP16, kako je daje uvoz
func oznakaObilaska(obilazak string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/bp16:evidencije_obilaska:"+obilazak)).String()
}

// uveziObilazak upisuje na čvoru obilazak iz BP16 kako to radi uvoz
// (importer/bp16, RunObilasci): na postojeći list dana, a bez njega na nov
// list zaključen prijenosom, sa zadatkom i fotografijom
func uveziObilazak(t *testing.T, n *cvor, obilazak string) *models.VodocuvarskiList {
	t.Helper()
	repo := NewVodocuvarRepository(n.db, n.rec)
	l, err := repo.ZaDan(ctxRaz, ivoIvic, danIve)
	nuzno(t, err)
	if l == nil {
		l = &models.VodocuvarskiList{UserID: ivoIvic, Ime: "Ivo Ivić", Sektor: "B", AreaID: 16, Datum: danIve,
			Rekonstrukcija: true, Izvor: "bp16:evidencije_obilaska", Prilike: "sunčano, 18 °C", Cvor: n.ime}
	}
	z := oznakaObilaska(obilazak)
	l.Zadaci = append(l.Zadaci, models.ZadatakNaListu{ID: z, Tekst: "obilazak " + obilazak, Zadao: "Ana Anić", ZadanoAt: danIve,
		Status: models.ZadatakObavljen, Obavljeno: "obiđeno"})
	l.Prilozi = append(l.Prilozi, models.PrilogLista{ID: z + "-foto", Naziv: "fotografija 1", Vrsta: "image/jpeg", Bajtova: 3,
		Sadrzaj: "otisak-" + obilazak, ZadatakID: z})
	nuzno(t, repo.Save(ctxRaz, l))
	return l
}

// imaObilazak javlja nosi li list zadatak obilaska i njegovu fotografiju
func imaObilazak(l models.VodocuvarskiList, obilazak string) bool {
	z := oznakaObilaska(obilazak)
	return zakljucioZadatakNaListu(l, z) && len(l.PriloziZadatka(z)) == 1
}

// zakljucioZadatakNaListu javlja stoji li zadatak na listu
func zakljucioZadatakNaListu(l models.VodocuvarskiList, id string) bool {
	for _, z := range l.Zadaci {
		if z.ID == id {
			return true
		}
	}
	return false
}

// prenesenSObilaskom provjerava da je list prenesen iz ranije evidencije,
// s obilaskom i vremenskim prilikama iz uvoza
func prenesenSObilaskom(t *testing.T, l models.VodocuvarskiList, obilazak string) {
	t.Helper()
	if !l.Rekonstrukcija || l.Predan() || l.Prilike != "sunčano, 18 °C" || !imaObilazak(l, obilazak) {
		t.Errorf("prenesen list nije sačuvan: rekonstrukcija=%v zadaci=%d prilozi=%d prilike=%q", l.Rekonstrukcija, len(l.Zadaci), len(l.Prilozi), l.Prilike)
	}
}

// Uvoz na poslužitelju prenese list Ive za 14. 10.; Ana na laptopu, koji
// taj list još nema, upiše bilješku za isti dan, što ondje stvori nacrt.
// Nakon razmjene oba čvora pokazuju prenesen list s obilaskom i
// fotografijom, a Anina bilješka stoji na njemu. Isto vrijedi i kad laptop
// razmijeni prvi.
func TestPrenesenListImaPrednostPredNacrtomSDrugogCvora(t *testing.T) {
	for _, laptopPrvi := range []bool{false, true} {
		a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
		uveziObilazak(t, a, "101")
		nacrtSUpisom(t, b, "provjeriti ustavu na kanalu", time.Date(2026, time.October, 14, 20, 0, 0, 0, models.Zagreb))
		if laptopPrvi {
			posalji(t, b, a)
		}
		l := isteKnjige(t, a, b)
		prenesenSObilaskom(t, l, "101")
		if !imaUpis(l, "provjeriti ustavu na kanalu") {
			t.Errorf("bilješka s nacrta izgubljena: %+v", l.Upisi)
		}
	}
}

// Vodočuvarov vlastiti nacrt s drugog čvora (spremljen obrazac, s
// vremenom upisa) ni brisanje nacrta s drugog čvora ne mijenjaju prenesen
// list, kao što ne mijenjaju ni predan list.
func TestPrenesenListNeZamjenjujeNiVodocuvarovNacrtNiBrisanje(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	uveziObilazak(t, a, "101")
	nacrt := listIve("napisano na laptopu")
	kad := time.Now()
	nacrt.NapisanoAt = &kad
	repoB := NewVodocuvarRepository(b.db, b.rec)
	nuzno(t, repoB.Save(ctxRaz, nacrt))
	l := isteKnjige(t, a, b)
	prenesenSObilaskom(t, l, "101")
	if l.Opis != "" {
		t.Errorf("nacrt s laptopa prepisao je prenesen list: %q", l.Opis)
	}

	c, d := noviCvor(t, "posluzitelj2"), noviCvor(t, "laptop2")
	uveziObilazak(t, c, "102")
	brisan := listIve("nacrt koji se briše")
	repoD := NewVodocuvarRepository(d.db, d.rec)
	nuzno(t, repoD.Save(ctxRaz, brisan))
	nuzno(t, repoD.Delete(ctxRaz, brisan.ID))
	prenesenSObilaskom(t, isteKnjige(t, c, d), "102")
}

// Nakon razmjene Ana na laptopu upiše još jednu bilješku: upis ide na
// prenesen list i nosi njegov obilazak, a sažimanje knjige na laptopu ga
// ne briše. Čvor koji primi samo sažetu knjigu vidi isti list.
func TestPrenesenListOstajeNakonUpisaISazimanja(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	uveziObilazak(t, a, "101")
	nacrtSUpisom(t, b, "prva bilješka", time.Date(2026, time.October, 14, 8, 0, 0, 0, models.Zagreb))
	isteKnjige(t, a, b)
	repoB := NewVodocuvarRepository(b.db, b.rec)
	l, err := repoB.ZaDan(ctxRaz, ivoIvic, danIve)
	nuzno(t, err)
	l.Upisi = append(l.Upisi, upisAne("druga bilješka", time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb)))
	nuzno(t, repoB.Save(ctxRaz, l))
	ostariVerzije(t, b)
	if _, err := SazmiKnjigu(ctxRaz, b.db, b.rec, time.Now().AddDate(0, 0, -90)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReplaySurface(ctxRaz, b.db, b.rec); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*cvor{b, noviCvor(t, "novi")} {
		if n.ime == "novi" {
			posalji(t, b, n)
		}
		l := jedanListIve(t, n)
		prenesenSObilaskom(t, l, "101")
		if !imaUpis(l, "prva bilješka") || !imaUpis(l, "druga bilješka") {
			t.Errorf("čvor %s: bilješke na prenesenom listu: %+v", n.ime, l.Upisi)
		}
	}
}

// Ivo preda list na laptopu, a uvoz na poslužitelju, koji predan list još
// nema, za isti dan prenese obilazak iz BP16. Predan list ima prednost, a
// obilazak s fotografijom stoji na njemu, kao kad uvoz na istom čvoru
// upiše obilazak na postojeći list dana. Ishod ne ovisi o redoslijedu
// primanja.
func TestPredanListNosiObilazakPrenesenSDrugogCvora(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	uveziObilazak(t, a, "101")
	predan := predajIvu(t, b, "obilazak nasipa Batina")
	l := isteKnjige(t, a, b)
	if !l.Predan() || !l.PredanoAt.Equal(*predan.PredanoAt) || l.Opis != "obilazak nasipa Batina" || l.Rekonstrukcija || !imaObilazak(l, "101") {
		t.Errorf("predan list s prenesenim obilaskom: %+v", l)
	}
	c, d := noviCvor(t, "treci"), noviCvor(t, "cetvrti")
	posalji(t, a, c)
	posalji(t, b, c)
	posalji(t, b, d)
	posalji(t, a, d)
	if lc, ld := jedanListIve(t, c), jedanListIve(t, d); !lc.Predan() || !imaObilazak(lc, "101") || len(lc.Zadaci) != len(ld.Zadaci) || len(lc.Prilozi) != len(ld.Prilozi) {
		t.Errorf("redoslijed primanja mijenja list:\n%+v\n%+v", lc, ld)
	}
}

// Isti dan prenesen na dva čvora s različitim obilascima: list nosi oba,
// kao da su uvezeni na istom čvoru.
func TestPrenesenNaDvaCvoraNosiObilaskeOba(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "posluzitelj2")
	uveziObilazak(t, a, "101")
	uveziObilazak(t, b, "102")
	l := isteKnjige(t, a, b)
	prenesenSObilaskom(t, l, "101")
	if !imaObilazak(l, "102") || len(l.Zadaci) != 2 || len(l.Prilozi) != 2 {
		t.Errorf("obilasci s dva čvora: %+v", l.Zadaci)
	}
	// kasniji uvoz na istom čvoru nosi i raniji obilazak, pa raniju verziju
	// pokriva i ne udvostručuje ga
	uveziObilazak(t, a, "103")
	if l := isteKnjige(t, a, b); len(l.Zadaci) != 3 || len(l.Prilozi) != 3 {
		t.Errorf("obilasci nakon još jednog uvoza: %d zadataka, %d priloga", len(l.Zadaci), len(l.Prilozi))
	}
}

// Prednost i pokrivanje verzija s prenesenim listom: predan pred prenesenim,
// prenesen pred nacrtom, a od dva jednaka kasniji; prenesen list pokriva
// samo prenesen list koji nosi sve njegove obilaske, a nacrt ga ne pokriva.
func TestPrednostIPokrivanjePrenesenogLista(t *testing.T) {
	kad := time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)
	zadatak := []models.ZadatakNaListu{{ID: "z1"}}
	prilog := []models.PrilogLista{{ID: "f1", ZadatakID: "z1"}}
	nacrt := verzijaLista{list: models.VodocuvarskiList{NapisanoAt: &kad}}
	predan := verzijaLista{list: models.VodocuvarskiList{PredanoAt: &kad, Zadaci: zadatak, Prilozi: prilog}}
	prenesenL := verzijaLista{list: models.VodocuvarskiList{Rekonstrukcija: true, Zadaci: zadatak, Prilozi: prilog}}
	bezPriloga := verzijaLista{list: models.VodocuvarskiList{Rekonstrukcija: true, Zadaci: zadatak}}
	prednost := []struct {
		ime      string
		v, w     verzijaLista
		prednost bool
	}{
		{"prenesen pred nacrtom", prenesenL, nacrt, true},
		{"nacrt ne pred prenesenim", nacrt, prenesenL, false},
		{"predan pred prenesenim", predan, prenesenL, true},
		{"prenesen ne pred predanim", prenesenL, predan, false},
		{"od dva prenesena kasniji", prenesenL, bezPriloga, true},
		{"od dva predana kasniji", predan, predan, true},
	}
	for _, s := range prednost {
		if got := imaPrednost(s.v, s.w); got != s.prednost {
			t.Errorf("%s: prednost %v, očekivano %v", s.ime, got, s.prednost)
		}
	}
	pokrivanje := []struct {
		ime     string
		w, v    verzijaLista
		pokriva bool
	}{
		{"nacrt ne pokriva prenesen", nacrt, prenesenL, false},
		{"predan ne pokriva prenesen", predan, prenesenL, false},
		{"prenesen bez fotografije ne pokriva", bezPriloga, prenesenL, false},
		{"prenesen bez zadatka ne pokriva", verzijaLista{list: models.VodocuvarskiList{Rekonstrukcija: true}}, bezPriloga, false},
		{"prenesen s obilascima pokriva", prenesenL, bezPriloga, true},
		{"prenesen pokriva nacrt", prenesenL, verzijaLista{}, true},
	}
	for _, s := range pokrivanje {
		if got := pokriva(s.w, s.v); got != s.pokriva {
			t.Errorf("%s: pokriva %v, očekivano %v", s.ime, got, s.pokriva)
		}
	}
	// bez novih obilazaka list ostaje kakav je, a dodani se ne upisuju u
	// verziju iz koje je list složen
	osnova := make([]models.ZadatakNaListu, 1, 4)
	osnova[0] = models.ZadatakNaListu{ID: "z0"}
	if z := dodajNove(osnova, osnova, oznakaZadatka); &z[0] != &osnova[0] || len(z) != 1 {
		t.Errorf("bez novih zadataka popis se mijenja: %+v", z)
	}
	if z := dodajNove(osnova, zadatak, oznakaZadatka); len(z) != 2 || &z[0] == &osnova[0] || osnova[:2][1].ID != "" {
		t.Errorf("dodan zadatak: %+v, osnova %+v", z, osnova[:2])
	}
}
