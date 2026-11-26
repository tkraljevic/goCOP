package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/propisi"
)

// Testovi propisa rade samo na izmišljenim, kratkim primjerima („Zakon o
// čuvanju nasipa”, „Plan obrane Primjerova”); tekst pravih propisa ne ulazi u
// repozitorij (Odluke (25. 11. 2026.)).

// cvorPropisa je čvor bez oslonca propisa iz noviCvor, da popis vidi samo
// propise koje test sam upiše
func cvorPropisa(t *testing.T, ime string) (*cvor, *PropisiRepository) {
	t.Helper()
	c := noviCvor(t, ime)
	_, err := c.db.Exec(`DELETE FROM propisi`)
	nuzno(t, err)
	return c, NewPropisiRepository(c.db, c.rec)
}

// zakonNasipa je izmišljen zakon s glavom i dva članka
func zakonNasipa(nn models.IzdanjeNN) *models.Propis {
	return &models.Propis{
		ID: nn.ID(), Adresa: "zakon-o-cuvanju-nasipa", Naziv: "Zakon o čuvanju nasipa", Vrsta: "Zakon",
		Donositelj: "Hrvatski sabor", Skupina: "O obrani od poplava", NN: nn, ELI: nn.PutELI(),
		DatumObjave: "2026-03-02", Izvor: models.IzvorPropisaNN, Citac: propisi.InacicaCitaca,
		Ucitao: "Pero Perić", UcitaoID: "pperic",
		Dijelovi: []models.DioPropisa{
			{Oznaka: "uvod", Vrsta: models.DioUvod, Stavci: []models.StavakPropisa{{Tekst: "Na temelju izmišljenog ovlaštenja donosi se"}}},
			{Oznaka: "glava-i", Vrsta: models.DioGlava, Broj: "I.", Naslov: "OPĆE ODREDBE"},
			{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.", Naslov: "Predmet", Roditelj: "glava-i",
				Stavci: []models.StavakPropisa{{Oznaka: "(1)", Tekst: "Ovim se Zakonom uređuje čuvanje nasipa uz Međašnji potok."}}},
			{Oznaka: "clanak-2", Vrsta: models.DioClanak, Broj: "2.", Roditelj: "glava-i",
				Stavci: []models.StavakPropisa{{Tekst: "Pripremno stanje proglašava se kad voda poraste:", Tocke: []models.TockaPropisa{
					{Vrsta: models.TockaBroj, Oznaka: "1.", Tekst: "50 % iznad redovne razine na letvi r_1"},
				}}}},
			{Oznaka: "zavrsetak", Vrsta: models.DioZavrsetak, Stavci: []models.StavakPropisa{{Tekst: "Klasa: izmišljena"}}},
		},
	}
}

// izmjenaNasipa je izmišljena izmjena zakona s jednim člankom
func izmjenaNasipa(p *models.Propis, nn models.IzdanjeNN) *models.IzmjenaPropisa {
	return &models.IzmjenaPropisa{
		ID: p.ID + "-izmjena-" + nn.ID(), PropisID: p.ID, Naziv: "Zakon o izmjeni Zakona o čuvanju nasipa", NN: nn,
		DatumObjave: "2026-06-01", StupaNaSnagu: "2026-06-09", Izvor: models.IzvorPropisaNN, Citac: propisi.InacicaCitaca,
		Dijelovi: []models.DioPropisa{{Oznaka: "clanak-1", Vrsta: models.DioClanak, Broj: "1.",
			Stavci: []models.StavakPropisa{{Tekst: "U članku 2. riječ »nasip« zamjenjuje se riječima »obrambeni nasip«."}}}},
		Upute: []models.UputaIzmjene{{Redni: 1, IzClanka: "clanak-1", Vrsta: models.UputaZamjenaRijeci,
			Cilj: models.CiljUpute{Clanci: []string{"clanak-2"}}, Stari: "nasip", Novi: "obrambeni nasip"}},
	}
}

var (
	nnZakona  = models.IzdanjeNN{Godina: 2026, Broj: 12, Akt: 201}
	nnIzmjene = models.IzdanjeNN{Godina: 2026, Broj: 47, Akt: 808}
)

// brojKazala je broj redaka kazala jednog vlasnika (propis bez izmjena ili
// izmjena)
func brojKazala(t *testing.T, c *cvor, propisID, izmjenaID string) int {
	t.Helper()
	if izmjenaID == "" {
		return brojRedaka(t, c.db, `SELECT count(*) FROM propisi_dijelovi WHERE propis_id = ? AND izmjena_id IS NULL`, propisID)
	}
	return brojRedaka(t, c.db, `SELECT count(*) FROM propisi_dijelovi WHERE izmjena_id = ?`, izmjenaID)
}

// Spremljen propis čita se po oznaci zapisa i po adresi, popis ga pokazuje s
// opsegom iz kazala i brojem izmjena, a kazalo ima po jedan redak za svaki
// dio (Odluke (25. 11. 2026.)).
func TestPropisSeSpremaIProcita(t *testing.T) {
	c, repo := cvorPropisa(t, "ured")
	p := zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	if p.Otisak == "" || p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
		t.Fatalf("spremanje nije postavilo otisak i vremena: %q %v %v", p.Otisak, p.CreatedAt, p.UpdatedAt)
	}
	nuzno(t, repo.SpremiIzmjenu(ctxRaz, izmjenaNasipa(p, nnIzmjene)))

	for _, kljuc := range []string{p.ID, p.Adresa} {
		x, err := repo.PoAdresi(ctxRaz, kljuc)
		if err != nil || x == nil || x.Naziv != p.Naziv || len(x.Dijelovi) != 5 {
			t.Errorf("po adresi %q: %+v %v", kljuc, x, err)
		}
	}
	if x, err := repo.Get(ctxRaz, p.ID); err != nil || x == nil || x.Otisak != p.Otisak || x.Dijelovi[2].Stavci[0].Oznaka != "(1)" {
		t.Errorf("Get: %+v %v", x, err)
	}
	if x, err := repo.Get(ctxRaz, "nn-1999-1-1"); x != nil || err != nil {
		t.Errorf("propis kojeg nema: %+v %v", x, err)
	}
	if x, err := repo.PoAdresi(ctxRaz, "nema-ga"); x != nil || err != nil {
		t.Errorf("adresa koje nema: %+v %v", x, err)
	}
	popis, err := repo.Popis(ctxRaz)
	nuzno(t, err)
	if len(popis) != 1 {
		t.Fatalf("popis: %+v", popis)
	}
	s := popis[0]
	if s.ID != p.ID || s.Adresa != p.Adresa || s.NN != nnZakona || s.ELI != p.ELI || s.Skupina != p.Skupina || s.Izmjena != 1 ||
		s.Opseg != (propisi.Opseg{Glave: 1, Clanci: 2}) || s.CreatedAt.IsZero() {
		t.Errorf("redak popisa: %+v", s)
	}
	if n := brojKazala(t, c, p.ID, ""); n != len(p.Dijelovi) {
		t.Errorf("kazalo ima %d redaka, a propis %d dijelova", n, len(p.Dijelovi))
	}
	if v, err := c.rec.Latest(ctxRaz, EntityPropisi, p.ID); err != nil || v.Channel != "" {
		t.Errorf("verzija propisa: %+v %v", v, err)
	}
}

// Novo čitanje istog akta zamjenjuje kazalo osnovnog teksta, ne dopisuje
// ga, a kazalo izmjene ostaje (Odluke (25. 11. 2026.)).
func TestIspravakZamjenjujeKazalo(t *testing.T) {
	c, repo := cvorPropisa(t, "ured")
	p := zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	iz := izmjenaNasipa(p, nnIzmjene)
	nuzno(t, repo.SpremiIzmjenu(ctxRaz, iz))
	stari := p.Otisak
	p.Dijelovi = p.Dijelovi[:3]
	nuzno(t, repo.Spremi(ctxRaz, p))
	if n := brojKazala(t, c, p.ID, ""); n != 3 {
		t.Errorf("nakon ispravka kazalo ima %d redaka, a treba 3", n)
	}
	if n := brojKazala(t, c, p.ID, iz.ID); n != 1 {
		t.Errorf("kazalo izmjene ima %d redaka, a treba 1", n)
	}
	if p.Otisak == stari {
		t.Error("drugi tekst ima isti otisak")
	}
	if pogoci, err := repo.Trazi(ctxRaz, []string{"pripremn"}, "", 0); err != nil || len(pogoci) != 0 {
		t.Errorf("tekst maknut ispravkom još se nalazi: %+v %v", pogoci, err)
	}
}

// Ponovno spremanje propisa je upsert: izmjene i njihovo kazalo ostaju
// (zamjena retka bi ih kaskadom obrisala).
func TestUpisNeBriseIzmjene(t *testing.T) {
	c, repo := cvorPropisa(t, "ured")
	p := zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	iz := izmjenaNasipa(p, nnIzmjene)
	nuzno(t, repo.SpremiIzmjenu(ctxRaz, iz))
	prvi := p.CreatedAt
	p.Naziv += " (pročišćeno)"
	nuzno(t, repo.Spremi(ctxRaz, p))
	izmjene, err := repo.Izmjene(ctxRaz, p.ID)
	if err != nil || len(izmjene) != 1 || izmjene[0].ID != iz.ID || izmjene[0].Upute[0].Novi != "obrambeni nasip" {
		t.Fatalf("izmjene nakon ponovnog spremanja: %+v %v", izmjene, err)
	}
	if n := brojKazala(t, c, p.ID, iz.ID); n != 1 {
		t.Errorf("kazalo izmjene ima %d redaka", n)
	}
	if !p.CreatedAt.Equal(prvi) {
		t.Errorf("vrijeme nastanka se promijenilo: %v → %v", prvi, p.CreatedAt)
	}
}

// Propis ili izmjena veća od 8 MB ne bi prošla razmjenu, pa se ne sprema:
// nema ni površine ni verzije u knjizi (Odluke (25. 11. 2026.)).
func TestPrevelikPropisSeNeSprema(t *testing.T) {
	c, repo := cvorPropisa(t, "ured")
	p := zakonNasipa(nnZakona)
	p.Dijelovi[2].Stavci[0].Tekst = strings.Repeat("nasip ", NajveciPropis/6+1)
	if err := repo.Spremi(ctxRaz, p); !errors.Is(err, ErrPrevelikPropis) {
		t.Fatalf("prevelik propis: %v", err)
	}
	if ima(t, c, `SELECT count(*) FROM propisi`) || ima(t, c, `SELECT count(*) FROM record_versions`) {
		t.Error("prevelik propis je ostavio trag u bazi ili knjizi")
	}
	p = zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	iz := izmjenaNasipa(p, nnIzmjene)
	iz.Upute[0].Tekst = strings.Repeat("nasip ", NajveciPropis/6+1)
	if err := repo.SpremiIzmjenu(ctxRaz, iz); !errors.Is(err, ErrPrevelikPropis) {
		t.Fatalf("prevelika izmjena: %v", err)
	}
	if ima(t, c, `SELECT count(*) FROM propisi_izmjene`) || ima(t, c, `SELECT count(*) FROM record_versions WHERE entity = ?`, EntityPropisiIzmjene) {
		t.Error("prevelika izmjena je ostavila trag u bazi ili knjizi")
	}
}

// Dva propisa s istom adresom (učitana na dva računala prije razmjene):
// adresa vodi na stariji zapis, a oba ostaju na površini, jer adresa nema
// UNIQUE koji bi srušio primjenu (Odluke (25. 11. 2026.)).
func TestSukobAdresaCitaStarijiZapis(t *testing.T) {
	_, repo := cvorPropisa(t, "ured")
	mladji := zakonNasipa(nnZakona)
	mladji.CreatedAt = time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)
	stariji := zakonNasipa(models.IzdanjeNN{Godina: 2025, Broj: 3, Akt: 40})
	stariji.Naziv = "Zakon o čuvanju nasipa (stari)"
	stariji.CreatedAt = time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	nuzno(t, repo.Spremi(ctxRaz, mladji))
	nuzno(t, repo.Spremi(ctxRaz, stariji))
	x, err := repo.PoAdresi(ctxRaz, "zakon-o-cuvanju-nasipa")
	if err != nil || x == nil || x.ID != stariji.ID || x.Adresa != "zakon-o-cuvanju-nasipa" {
		t.Fatalf("adresa vodi na %+v, %v; treba stariji zapis", x, err)
	}
	// mlađi se čita po oznaci zapisa i nosi je kao adresu, pa poveznice i
	// obrasci na njegovoj stranici ne vode na stariji
	if x, err := repo.PoAdresi(ctxRaz, mladji.ID); err != nil || x == nil || x.ID != mladji.ID || x.Adresa != mladji.ID {
		t.Errorf("mlađi po oznaci zapisa: %+v %v", x, err)
	}
	popis, err := repo.Popis(ctxRaz)
	if err != nil || len(popis) != 2 {
		t.Fatalf("popis: %+v %v", popis, err)
	}
	for _, s := range popis {
		if zeli := map[string]string{stariji.ID: "zakon-o-cuvanju-nasipa", mladji.ID: mladji.ID}[s.ID]; s.Adresa != zeli {
			t.Errorf("popis: %s ima adresu %q, želi %q", s.ID, s.Adresa, zeli)
		}
	}
	// oznaka zapisa ima prednost pred adresom drugog propisa
	treci := zakonNasipa(models.IzdanjeNN{Godina: 2024, Broj: 7, Akt: 1})
	treci.Adresa, treci.CreatedAt = mladji.ID, time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	nuzno(t, repo.Spremi(ctxRaz, treci))
	if x, err := repo.PoAdresi(ctxRaz, mladji.ID); err != nil || x == nil || x.ID != mladji.ID {
		t.Errorf("oznaka zapisa nema prednost: %+v %v", x, err)
	}
}

// Pretraga ide po svedenom tekstu: drugi padež, slova s kvačicama i đ
// nalaze se bez obzira kako su upisani, a % i _ u upitu traže se doslovno
// (Odluke (25. 11. 2026.)).
func TestPretragaPoSvedenomTekstu(t *testing.T) {
	_, repo := cvorPropisa(t, "ured")
	p := zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	for _, s := range []struct {
		korijeni []string
		oznake   string
	}{
		{propisi.Korijeni("pripremnog stanja"), "clanak-2"},
		{propisi.Korijeni("ČUVANJA"), "clanak-1"},
		{propisi.Korijeni("medasnjem"), "clanak-1"},
		{[]string{"Međašnj"}, "clanak-1"},
		{propisi.Korijeni("odredbe"), "glava-i"},
		{[]string{"%"}, "clanak-2"},
		{[]string{"r_1"}, "clanak-2"},
		{[]string{"r_"}, "clanak-2"},
		{[]string{"_"}, "clanak-2"},
		{[]string{"pripremn", "letvi"}, "clanak-2"},
		{[]string{"nasip", "nema"}, ""},
		{nil, ""},
	} {
		pogoci, err := repo.Trazi(ctxRaz, s.korijeni, "", 0)
		nuzno(t, err)
		var oznake []string
		for _, r := range pogoci {
			oznake = append(oznake, r.Oznaka)
		}
		if got := strings.Join(oznake, ","); got != s.oznake {
			t.Errorf("%q: pogoci %q, očekivano %q", s.korijeni, got, s.oznake)
		}
	}
	pogoci, err := repo.Trazi(ctxRaz, []string{"pripremn"}, "", 0)
	nuzno(t, err)
	if r := pogoci[0]; r.PropisID != p.ID || r.IzmjenaID != "" || r.Vrsta != models.DioClanak || r.Broj != "2." ||
		r.Roditelj != "glava-i" || r.Redni != 4 || !strings.Contains(r.Tekst, "Pripremno stanje") {
		t.Errorf("redak kazala: %+v", r)
	}
}

// Pretraga nalazi i tekst izmjene (uz oznaku izmjene), poslije osnovnog
// teksta istog propisa; ograničenje vrijedi po propisu, a pretraga unutar
// propisa ne vidi druge (Odluke (25. 11. 2026.)).
func TestPretragaNalaziTekstIzmjene(t *testing.T) {
	_, repo := cvorPropisa(t, "ured")
	p := zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	iz := izmjenaNasipa(p, nnIzmjene)
	nuzno(t, repo.SpremiIzmjenu(ctxRaz, iz))
	plan := zakonNasipa(models.IzdanjeNN{Godina: 2026, Broj: 13, Akt: 5})
	plan.Naziv, plan.Adresa = "Plan obrane Primjerova", "plan-obrane-primjerova"
	nuzno(t, repo.Spremi(ctxRaz, plan))

	pogoci, err := repo.Trazi(ctxRaz, []string{"nasip"}, p.ID, 0)
	nuzno(t, err)
	if len(pogoci) != 2 || pogoci[0].IzmjenaID != "" || pogoci[1].IzmjenaID != iz.ID || pogoci[1].PropisID != p.ID ||
		pogoci[1].Oznaka != "clanak-1" {
		t.Fatalf("pogoci u propisu: %+v", pogoci)
	}
	sve, err := repo.Trazi(ctxRaz, []string{"nasip"}, "", 0)
	if err != nil || len(sve) != 3 {
		t.Errorf("pogoci u svim propisima: %+v %v", sve, err)
	}
	po1, err := repo.Trazi(ctxRaz, []string{"nasip"}, "", 1)
	if err != nil || len(po1) != 2 || po1[0].PropisID == po1[1].PropisID || po1[0].IzmjenaID != "" || po1[1].IzmjenaID != "" {
		t.Errorf("najviše jedan pogodak po propisu: %+v %v", po1, err)
	}
}

// Izravan pogodak po oznaci dijela gleda samo osnovni tekst, u svim
// propisima ili u jednom (Odluke (25. 11. 2026.)).
func TestDioPoOznaci(t *testing.T) {
	_, repo := cvorPropisa(t, "ured")
	p := zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	nuzno(t, repo.SpremiIzmjenu(ctxRaz, izmjenaNasipa(p, nnIzmjene)))
	plan := zakonNasipa(models.IzdanjeNN{Godina: 2026, Broj: 13, Akt: 5})
	plan.Adresa = "plan-obrane-primjerova"
	nuzno(t, repo.Spremi(ctxRaz, plan))

	if r, err := repo.DioPoOznaci(ctxRaz, "clanak-1", ""); err != nil || len(r) != 2 || r[0].IzmjenaID != "" || r[1].IzmjenaID != "" {
		t.Errorf("članak 1. u svim propisima: %+v %v", r, err)
	}
	if r, err := repo.DioPoOznaci(ctxRaz, "clanak-1", p.ID); err != nil || len(r) != 1 || r[0].PropisID != p.ID || r[0].Naslov != "Predmet" {
		t.Errorf("članak 1. u zakonu: %+v %v", r, err)
	}
	if r, err := repo.DioPoOznaci(ctxRaz, "clanak-300", ""); err != nil || len(r) != 0 {
		t.Errorf("članak kojeg nema: %+v %v", r, err)
	}
}

// Uklonjena izmjena odlazi s površine i iz kazala, a spomenik u knjizi nosi
// razlog. Propis i njegovo kazalo ostaju (Odluke (25. 11. 2026.)).
func TestUklanjanjeIzmjeneMiceKazalo(t *testing.T) {
	c, repo := cvorPropisa(t, "ured")
	p := zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	iz := izmjenaNasipa(p, nnIzmjene)
	nuzno(t, repo.SpremiIzmjenu(ctxRaz, iz))
	u := models.Uklanjanje{Razlog: "učitan krivi akt", Tko: "Pero Perić", Kad: time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)}
	nuzno(t, repo.UkloniIzmjenu(ctxRaz, iz.ID, u))
	if x, err := repo.Izmjena(ctxRaz, iz.ID); x != nil || err != nil {
		t.Errorf("izmjena je i dalje na površini: %+v %v", x, err)
	}
	if n := brojKazala(t, c, p.ID, iz.ID); n != 0 {
		t.Errorf("kazalo izmjene ima %d redaka", n)
	}
	if n := brojKazala(t, c, p.ID, ""); n != len(p.Dijelovi) {
		t.Errorf("kazalo propisa ima %d redaka", n)
	}
	spomenik := razlogUklanjanja(t, c, EntityPropisiIzmjene, iz.ID)
	if spomenik == nil || spomenik.Razlog != u.Razlog || spomenik.Tko != u.Tko || !spomenik.Kad.Equal(u.Kad) {
		t.Errorf("spomenik izmjene: %+v", spomenik)
	}
	prije := brojRedaka(t, c.db, `SELECT count(*) FROM record_versions`)
	nuzno(t, repo.UkloniIzmjenu(ctxRaz, iz.ID, u))
	nuzno(t, repo.Ukloni(ctxRaz, "nn-1999-1-1", u))
	if poslije := brojRedaka(t, c.db, `SELECT count(*) FROM record_versions`); poslije != prije {
		t.Errorf("uklanjanje onoga čega nema upisalo je %d verzija", poslije-prije)
	}
}

// razlogUklanjanja čita uklanjanje iz zadnje verzije zapisa, koja mora biti
// spomenik
func razlogUklanjanja(t *testing.T, c *cvor, entitet, id string) *models.Uklanjanje {
	t.Helper()
	v, err := c.rec.Latest(ctxRaz, entitet, id)
	nuzno(t, err)
	if !v.Archived {
		t.Fatalf("zadnja verzija %s/%s nije spomenik", entitet, id)
	}
	var x struct {
		Uklonjeno *models.Uklanjanje `json:"uklonjeno"`
	}
	nuzno(t, json.Unmarshal(v.Payload, &x))
	return x.Uklonjeno
}

// kvarOkidacem postavlja okidač koji ruši upis u tablicu, za grane greške
// unutar transakcije
func kvarOkidacem(t *testing.T, c *cvor, ime, kada string) {
	t.Helper()
	_, err := c.db.Exec(`CREATE TRIGGER ` + ime + ` ` + kada + ` BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`)
	nuzno(t, err)
}

// Svaka javna metoda javlja grešku baze: zatvorena baza, pokvaren sadržaj
// na površini, kriv stupac i upis koji okidač odbije. Ništa ne ostaje napola
// upisano (Odluke (25. 11. 2026.)).
func TestPropisiKvarBaze(t *testing.T) {
	t.Run("zatvorena baza", func(t *testing.T) {
		c, repo := cvorPropisa(t, "ured")
		nuzno(t, c.db.Close())
		p := zakonNasipa(nnZakona)
		u := models.Uklanjanje{Razlog: "kvar"}
		for ime, err := range map[string]error{
			"Spremi":        repo.Spremi(ctxRaz, p),
			"SpremiIzmjenu": repo.SpremiIzmjenu(ctxRaz, izmjenaNasipa(p, nnIzmjene)),
			"Ukloni":        repo.Ukloni(ctxRaz, p.ID, u),
			"UkloniIzmjenu": repo.UkloniIzmjenu(ctxRaz, p.ID, u),
			"Get":           drugo(repo.Get(ctxRaz, p.ID)),
			"PoAdresi":      drugo(repo.PoAdresi(ctxRaz, p.Adresa)),
			"Izmjena":       drugo(repo.Izmjena(ctxRaz, p.ID)),
			"Izmjene":       drugo(repo.Izmjene(ctxRaz, p.ID)),
			"Popis":         drugo(repo.Popis(ctxRaz)),
			"Trazi":         drugo(repo.Trazi(ctxRaz, []string{"nasip"}, "", 0)),
			"DioPoOznaci":   drugo(repo.DioPoOznaci(ctxRaz, "clanak-1", "")),
		} {
			if err == nil {
				t.Errorf("%s na zatvorenoj bazi nije javio grešku", ime)
			}
		}
	})

	t.Run("pokvaren sadržaj", func(t *testing.T) {
		c, repo := cvorPropisa(t, "ured")
		p := zakonNasipa(nnZakona)
		nuzno(t, repo.Spremi(ctxRaz, p))
		iz := izmjenaNasipa(p, nnIzmjene)
		nuzno(t, repo.SpremiIzmjenu(ctxRaz, iz))
		_, err := c.db.Exec(`UPDATE propisi_izmjene SET sadrzaj = 'nije json'`)
		nuzno(t, err)
		u := models.Uklanjanje{Razlog: "kvar"}
		for ime, err := range map[string]error{
			"Izmjena":       drugo(repo.Izmjena(ctxRaz, iz.ID)),
			"Izmjene":       drugo(repo.Izmjene(ctxRaz, p.ID)),
			"UkloniIzmjenu": repo.UkloniIzmjenu(ctxRaz, iz.ID, u),
			"Ukloni":        repo.Ukloni(ctxRaz, p.ID, u),
		} {
			if err == nil {
				t.Errorf("%s s pokvarenom izmjenom nije javio grešku", ime)
			}
		}
		_, err = c.db.Exec(`UPDATE propisi SET sadrzaj = 'nije json'`)
		nuzno(t, err)
		if err := repo.Ukloni(ctxRaz, p.ID, u); err == nil {
			t.Error("uklanjanje pokvarenog propisa nije javilo grešku")
		}
		if _, err := repo.Get(ctxRaz, p.ID); err == nil {
			t.Error("pokvaren propis pročitan je bez greške")
		}
		_, err = c.db.Exec(`UPDATE propisi SET created_at = 'nije vrijeme'`)
		nuzno(t, err)
		if _, err := repo.Popis(ctxRaz); err == nil {
			t.Error("popis s krivim vremenom nije javio grešku")
		}
		_, err = c.db.Exec(`UPDATE propisi_dijelovi SET redni = 'nije broj'`)
		nuzno(t, err)
		if _, err := repo.DioPoOznaci(ctxRaz, "clanak-1", ""); err == nil {
			t.Error("kazalo s krivim rednim brojem pročitano je bez greške")
		}
		_, err = c.db.Exec(`ALTER TABLE propisi RENAME COLUMN skupina TO skupina_x`)
		nuzno(t, err)
		if _, err := repo.Popis(ctxRaz); err == nil {
			t.Error("popis bez stupca skupina nije javio grešku")
		}
	})

	for _, s := range []struct {
		ime, okidac string
		radnja      func(repo *PropisiRepository, p *models.Propis, iz *models.IzmjenaPropisa) error
	}{
		{"upis propisa", `BEFORE UPDATE ON propisi`, func(repo *PropisiRepository, p *models.Propis, _ *models.IzmjenaPropisa) error {
			return repo.Spremi(ctxRaz, p)
		}},
		{"kazalo propisa", `BEFORE INSERT ON propisi_dijelovi`, func(repo *PropisiRepository, p *models.Propis, _ *models.IzmjenaPropisa) error {
			return repo.Spremi(ctxRaz, p)
		}},
		{"verzija propisa", `BEFORE INSERT ON record_versions WHEN NEW.entity = 'propisi'`,
			func(repo *PropisiRepository, p *models.Propis, _ *models.IzmjenaPropisa) error {
				return repo.Spremi(ctxRaz, p)
			}},
		{"upis izmjene", `BEFORE UPDATE ON propisi_izmjene`, func(repo *PropisiRepository, _ *models.Propis, iz *models.IzmjenaPropisa) error {
			return repo.SpremiIzmjenu(ctxRaz, iz)
		}},
		{"verzija izmjene", `BEFORE INSERT ON record_versions WHEN NEW.entity = 'propisi_izmjene'`,
			func(repo *PropisiRepository, _ *models.Propis, iz *models.IzmjenaPropisa) error {
				return repo.SpremiIzmjenu(ctxRaz, iz)
			}},
		{"spomenik izmjene", `BEFORE INSERT ON record_versions WHEN NEW.entity = 'propisi_izmjene' AND NEW.archived = 1`,
			func(repo *PropisiRepository, p *models.Propis, _ *models.IzmjenaPropisa) error {
				return repo.Ukloni(ctxRaz, p.ID, models.Uklanjanje{Razlog: "kvar"})
			}},
		{"brisanje propisa", `BEFORE DELETE ON propisi`, func(repo *PropisiRepository, p *models.Propis, _ *models.IzmjenaPropisa) error {
			return repo.Ukloni(ctxRaz, p.ID, models.Uklanjanje{Razlog: "kvar"})
		}},
		{"spomenik propisa", `BEFORE INSERT ON record_versions WHEN NEW.entity = 'propisi' AND NEW.archived = 1`,
			func(repo *PropisiRepository, p *models.Propis, _ *models.IzmjenaPropisa) error {
				return repo.Ukloni(ctxRaz, p.ID, models.Uklanjanje{Razlog: "kvar"})
			}},
		{"brisanje izmjene", `BEFORE DELETE ON propisi_izmjene`, func(repo *PropisiRepository, _ *models.Propis, iz *models.IzmjenaPropisa) error {
			return repo.UkloniIzmjenu(ctxRaz, iz.ID, models.Uklanjanje{Razlog: "kvar"})
		}},
		{"spomenik uklonjene izmjene", `BEFORE INSERT ON record_versions WHEN NEW.archived = 1`,
			func(repo *PropisiRepository, _ *models.Propis, iz *models.IzmjenaPropisa) error {
				return repo.UkloniIzmjenu(ctxRaz, iz.ID, models.Uklanjanje{Razlog: "kvar"})
			}},
		{"izmjena bez propisa", "", func(repo *PropisiRepository, _ *models.Propis, _ *models.IzmjenaPropisa) error {
			return repo.SpremiIzmjenu(ctxRaz, izmjenaNasipa(&models.Propis{ID: "nn-1999-1-1"}, nnIzmjene))
		}},
	} {
		t.Run(s.ime, func(t *testing.T) {
			c, repo := cvorPropisa(t, "ured")
			p := zakonNasipa(nnZakona)
			nuzno(t, repo.Spremi(ctxRaz, p))
			iz := izmjenaNasipa(p, nnIzmjene)
			nuzno(t, repo.SpremiIzmjenu(ctxRaz, iz))
			prije := brojRedaka(t, c.db, `SELECT count(*) FROM record_versions`)
			if s.okidac != "" {
				kvarOkidacem(t, c, "kvar", s.okidac)
			}
			if err := s.radnja(repo, p, iz); err == nil {
				t.Fatal("kvar nije javljen")
			}
			if poslije := brojRedaka(t, c.db, `SELECT count(*) FROM record_versions`); poslije != prije {
				t.Errorf("neuspjela radnja upisala je %d verzija", poslije-prije)
			}
			if !ima(t, c, `SELECT count(*) FROM propisi_izmjene WHERE id = ?`, iz.ID) || brojKazala(t, c, p.ID, "") != len(p.Dijelovi) {
				t.Error("neuspjela radnja odnijela je izmjenu ili kazalo")
			}
		})
	}
}

// drugo vraća samo grešku poziva koji vraća i vrijednost
func drugo[T any](_ T, err error) error { return err }
