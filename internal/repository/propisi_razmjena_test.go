package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Razmjena propisa među računalima, bez mreže (noviCvor, posalji,
// razmijeni). Svi primjeri su izmišljeni (Odluke (25. 11. 2026.)).

// propisSIzmjenom sprema izmišljeni zakon i njegovu izmjenu na čvoru
func propisSIzmjenom(t *testing.T, c *cvor) (*models.Propis, *models.IzmjenaPropisa) {
	t.Helper()
	repo := NewPropisiRepository(c.db, c.rec)
	p := zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	iz := izmjenaNasipa(p, nnIzmjene)
	nuzno(t, repo.SpremiIzmjenu(ctxRaz, iz))
	return p, iz
}

// Propis i izmjena stižu na drugo računalo s istim kazalom, pa pretraga
// ondje nalazi i tekst izmjene; ponovno pokretanje ništa ne mijenja
// (Odluke (25. 11. 2026.)).
func TestPropisSIzmjenomIKazalomStizeNaDrugiCvor(t *testing.T) {
	sNedovrsenima(t)
	ured, server := noviCvor(t, "ured"), noviCvor(t, "server")
	p, iz := propisSIzmjenom(t, ured)
	posalji(t, ured, server)
	if n := brojKazala(t, server, p.ID, ""); n != len(p.Dijelovi) {
		t.Errorf("kazalo propisa na primatelju ima %d redaka", n)
	}
	pogoci, err := NewPropisiRepository(server.db, server.rec).Trazi(ctxRaz, []string{"obrambeni"}, p.ID, 0)
	if err != nil || len(pogoci) != 1 || pogoci[0].IzmjenaID != iz.ID {
		t.Errorf("pretraga izmjene na primatelju: %+v %v", pogoci, err)
	}
	slozni(t, ured, server)
}

// Izmjena koja stigne prije svog propisa čeka među nedovršenima i upiše se
// čim stigne propis (Odluke (25. 11. 2026.)).
func TestIzmjenaPrijePropisaCekaPaSeUpise(t *testing.T) {
	sNedovrsenima(t)
	ured, server := noviCvor(t, "ured"), noviCvor(t, "server")
	p, iz := propisSIzmjenom(t, ured)
	verzijaPropisa, err := ured.rec.Latest(ctxRaz, EntityPropisi, p.ID)
	nuzno(t, err)
	verzijaIzmjene, err := ured.rec.Latest(ctxRaz, EntityPropisiIzmjene, iz.ID)
	nuzno(t, err)
	if err := primiPaket(t, server, *verzijaIzmjene); err == nil {
		t.Fatal("izmjena bez propisa prošla je strani ključ")
	}
	if !nedovrsen(EntityPropisiIzmjene + "|" + iz.ID) {
		t.Fatal("izmjena ne čeka propis")
	}
	nuzno(t, primiPaket(t, server, *verzijaPropisa))
	if x, err := NewPropisiRepository(server.db, server.rec).Izmjena(ctxRaz, iz.ID); err != nil || x == nil {
		t.Fatalf("izmjena nije upisana kad je stigao propis: %+v %v", x, err)
	}
	slozni(t, ured, server)
}

// Isti akt učitan na dva računala daje jedan zapis: nakon razmjene oba
// računala imaju isti propis, zadnje čitanje (Odluke (25. 11. 2026.), O9).
func TestIstiAktNaDvaCvoraJedanZapis(t *testing.T) {
	sNedovrsenima(t)
	ured, laptop := noviCvor(t, "ured"), noviCvor(t, "laptop")
	nuzno(t, NewPropisiRepository(ured.db, ured.rec).Spremi(ctxRaz, zakonNasipa(nnZakona)))
	drugi := zakonNasipa(nnZakona)
	drugi.Skupina = "Ostali"
	nuzno(t, NewPropisiRepository(laptop.db, laptop.rec).Spremi(ctxRaz, drugi))
	razmijeni(t, ured, laptop)
	razmijeni(t, laptop, ured)
	for _, c := range []*cvor{ured, laptop} {
		if n := brojRedaka(t, c.db, `SELECT count(DISTINCT entity_id) FROM record_versions WHERE entity = ?`, EntityPropisi); n != 1 {
			t.Errorf("%s: %d zapisa propisa u knjizi", c.ime, n)
		}
		if x, err := NewPropisiRepository(c.db, c.rec).Get(ctxRaz, nnZakona.ID()); err != nil || x == nil || x.Skupina != "Ostali" {
			t.Errorf("%s: na površini nije zadnje čitanje: %+v %v", c.ime, x, err)
		}
	}
	slozni(t, ured, laptop)
}

// Uklonjen propis odnosi izmjene: i onu na površini i onu koja je samo u
// knjizi. Spomenici nose razlog, a površina i kazalo prazni su na izvoru i
// na računalu koje primi uklanjanje (Odluke (25. 11. 2026.), O7).
func TestUklanjanjePropisaArhiviraIzmjene(t *testing.T) {
	sNedovrsenima(t)
	ured, server := noviCvor(t, "ured"), noviCvor(t, "server")
	repo := NewPropisiRepository(ured.db, ured.rec)
	p, iz := propisSIzmjenom(t, ured)
	samoUKnjizi := izmjenaNasipa(p, models.IzdanjeNN{Godina: 2026, Broj: 60, Akt: 900})
	nuzno(t, repo.SpremiIzmjenu(ctxRaz, samoUKnjizi))
	_, err := ured.db.Exec(`DELETE FROM propisi_izmjene WHERE id = ?`, samoUKnjizi.ID)
	nuzno(t, err)
	posalji(t, ured, server)

	u := models.Uklanjanje{Razlog: "učitan je krivi zakon", Tko: "Pero Perić", Kad: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	nuzno(t, repo.Ukloni(ctxRaz, p.ID, u))
	if r := razlogUklanjanja(t, ured, EntityPropisi, p.ID); r == nil || r.Razlog != u.Razlog || r.Tko != u.Tko {
		t.Errorf("spomenik propisa: %+v", r)
	}
	if r := razlogUklanjanja(t, ured, EntityPropisiIzmjene, iz.ID); r == nil || r.Razlog != u.Razlog {
		t.Errorf("spomenik izmjene: %+v", r)
	}
	if zivaUKnjizi(t, ured, EntityPropisiIzmjene, samoUKnjizi.ID) {
		t.Error("izmjena koja je bila samo u knjizi ostala je živa")
	}
	posalji(t, ured, server)
	for _, c := range []*cvor{ured, server} {
		for _, upit := range []string{
			`SELECT count(*) FROM propisi WHERE id = '` + p.ID + `'`,
			`SELECT count(*) FROM propisi_izmjene`,
			`SELECT count(*) FROM propisi_dijelovi`,
		} {
			if ima(t, c, upit) {
				t.Errorf("%s: nakon uklanjanja ostalo je nešto (%s)", c.ime, upit)
			}
		}
	}
	novi := noviCvor(t, "novi")
	posalji(t, ured, novi)
	slozni(t, ured, server, novi)
}

// ukloniDokTerenDodajeIzmjenu: ured učita propis, teren ga primi i doda
// izmjenu, a ured u isto vrijeme ukloni propis; zatim razmjena u oba smjera
func ukloniDokTerenDodajeIzmjenu(t *testing.T) (ured, teren *cvor, p *models.Propis, iz *models.IzmjenaPropisa) {
	t.Helper()
	ured, teren = noviCvor(t, "ured"), noviCvor(t, "teren")
	p = zakonNasipa(nnZakona)
	nuzno(t, NewPropisiRepository(ured.db, ured.rec).Spremi(ctxRaz, p))
	razmijeni(t, ured, teren)
	iz = izmjenaNasipa(p, nnIzmjene)
	nuzno(t, NewPropisiRepository(teren.db, teren.rec).SpremiIzmjenu(ctxRaz, iz))
	nuzno(t, NewPropisiRepository(ured.db, ured.rec).Ukloni(ctxRaz, p.ID, models.Uklanjanje{Razlog: "zamijenjen", Tko: "Pero Perić"}))
	razmijeni(t, teren, ured)
	razmijeni(t, ured, teren)
	return ured, teren, p, iz
}

// Brisanje ima prednost (Odluke (13. 10. 2026.)): izmjena koju je teren
// dodao propisu koji je ured upravo uklonio ne ostaje ni na jednom
// računalu, ništa ne čeka, a novi čvorovi dobiju isto.
func TestBrisanjePropisaImaPrednostPredIzmjenom(t *testing.T) {
	sNedovrsenima(t)
	ured, teren, _, iz := ukloniDokTerenDodajeIzmjenu(t)
	for _, c := range []*cvor{ured, teren} {
		if ima(t, c, `SELECT count(*) FROM propisi_izmjene`) || ima(t, c, `SELECT count(*) FROM propisi_dijelovi`) {
			t.Errorf("%s: izmjena uklonjenog propisa ostala je na površini", c.ime)
		}
		if !zivaUKnjizi(t, c, EntityPropisiIzmjene, iz.ID) {
			t.Errorf("%s: izmjena terena mora ostati živa u knjizi", c.ime)
		}
	}
	noviOdUreda, noviOdTerena := noviCvor(t, "novi od ureda"), noviCvor(t, "novi od terena")
	posalji(t, ured, noviOdUreda)
	posalji(t, teren, noviOdTerena)
	slozni(t, ured, teren, noviOdUreda, noviOdTerena)
}

// Kad ured isti akt učita ponovno, vraća se i izmjena koja je u knjizi
// živa, na uredu odmah i na terenu razmjenom (zabiljeziSDjecom, vratiDjecu).
func TestPonovnoUcitavanjeVracaIzmjene(t *testing.T) {
	sNedovrsenima(t)
	ured, teren, p, iz := ukloniDokTerenDodajeIzmjenu(t)
	nuzno(t, NewPropisiRepository(ured.db, ured.rec).Spremi(ctxRaz, zakonNasipa(nnZakona)))
	razmijeni(t, ured, teren)
	for _, c := range []*cvor{ured, teren} {
		izmjene, err := NewPropisiRepository(c.db, c.rec).Izmjene(ctxRaz, p.ID)
		if err != nil || len(izmjene) != 1 || izmjene[0].ID != iz.ID {
			t.Errorf("%s: izmjena se nije vratila s propisom: %+v %v", c.ime, izmjene, err)
		}
		if n := brojKazala(t, c, p.ID, iz.ID); n != len(iz.Dijelovi) {
			t.Errorf("%s: kazalo vraćene izmjene ima %d redaka", c.ime, n)
		}
	}
	novi := noviCvor(t, "novi nakon povratka")
	posalji(t, ured, novi)
	slozni(t, ured, teren, novi)
}

// Propisi i izmjene su u zajedničkom kanalu, i spomenici, pa stižu na svako
// računalo bez obzira na pretplatu (Odluke (25. 11. 2026.), O1).
func TestPropisJeUZajednickomKanalu(t *testing.T) {
	ured := noviCvor(t, "ured")
	p, iz := propisSIzmjenom(t, ured)
	nuzno(t, NewPropisiRepository(ured.db, ured.rec).UkloniIzmjenu(ctxRaz, iz.ID, models.Uklanjanje{Razlog: "proba"}))
	nuzno(t, NewPropisiRepository(ured.db, ured.rec).Ukloni(ctxRaz, p.ID, models.Uklanjanje{Razlog: "proba"}))
	if n := brojRedaka(t, ured.db, `SELECT count(*) FROM record_versions WHERE entity IN (?, ?) AND channel <> ''`,
		EntityPropisi, EntityPropisiIzmjene); n != 0 {
		t.Errorf("%d verzija propisa nije u zajedničkom kanalu", n)
	}
	if n := brojRedaka(t, ured.db, `SELECT count(*) FROM record_versions WHERE entity IN (?, ?)`, EntityPropisi, EntityPropisiIzmjene); n != 4 {
		t.Errorf("u knjizi je %d verzija propisa i izmjene, a treba 4", n)
	}
}

// Sažimanje knjige čuva svaki različit tekst propisa (po otisku dijelova),
// a briše samo stariju verziju s istim tekstom: stari tekst ostaje u
// povijesti (plan Propisa).
func TestSazimanjeCuvaSveTekstovePropisa(t *testing.T) {
	c := noviCvor(t, "ured")
	repo := NewPropisiRepository(c.db, c.rec)
	p := zakonNasipa(nnZakona)
	nuzno(t, repo.Spremi(ctxRaz, p))
	prviTekst := p.Otisak
	p.Skupina = "Ostali"
	nuzno(t, repo.Spremi(ctxRaz, p))
	p.Dijelovi[2].Stavci[0].Tekst = "Ovim se Zakonom uređuje čuvanje nasipa i ustava."
	nuzno(t, repo.Spremi(ctxRaz, p))
	_, err := c.db.Exec(`UPDATE record_versions SET created_at = ?`, time.Now().AddDate(0, 0, -200))
	nuzno(t, err)
	obrisano, err := SazmiKnjigu(ctxRaz, c.db, c.rec, time.Now().AddDate(0, 0, -90))
	if err != nil || obrisano != 1 {
		t.Fatalf("sažimanje briše samo prvu verziju istog teksta: obrisano %d, %v", obrisano, err)
	}
	ostaju := map[string]bool{}
	rows, err := c.db.Query(`SELECT json_extract(payload, '$.otisak') FROM record_versions WHERE entity = ?`, EntityPropisi)
	nuzno(t, err)
	defer rows.Close()
	for rows.Next() {
		var o string
		nuzno(t, rows.Scan(&o))
		ostaju[o] = true
	}
	nuzno(t, rows.Err())
	if len(ostaju) != 2 || !ostaju[prviTekst] || !ostaju[p.Otisak] {
		t.Errorf("nakon sažimanja ostaju otisci %v; trebaju oba teksta", ostaju)
	}
}

// Računalo na 0.0.68 propis drži samo u knjizi. Nakon nadogradnje obnova
// površine pri pokretanju stavlja propis i izmjenu na površinu, s kazalom
// (Odluke (25. 11. 2026.)).
func TestObnovaPovrsineVracaPropisIKazalo(t *testing.T) {
	sNedovrsenima(t)
	ured, stari := noviCvor(t, "ured"), noviCvor(t, "stari")
	propisSIzmjenom(t, ured)
	verzije, err := ured.rec.Since(ctxRaz, "", 0)
	nuzno(t, err)
	_, err = stari.rec.Apply(ctxRaz, verzije)
	nuzno(t, err)
	o, err := ponovnoPokretanje(t, stari)
	nuzno(t, err)
	if o.Upisano[EntityPropisi] != 1 || o.Upisano[EntityPropisiIzmjene] != 1 {
		t.Errorf("obnova je upisala %v", o.Upisano)
	}
	slozni(t, ured, stari)
}

// Propis od oko 600 KB, s tekstom punim znakova <, > i & (JSON ih piše
// šest puta dulje), sprema se i stiže na drugo računalo; verzija za slanje
// daleko je ispod granice primatelja (63 MiB).
func TestPropisOd600KBProlaziRazmjenu(t *testing.T) {
	sNedovrsenima(t)
	ured, server := noviCvor(t, "ured"), noviCvor(t, "server")
	p := zakonNasipa(nnZakona)
	p.Dijelovi[2].Stavci[0].Tekst = strings.Repeat("razina <5 & >3 ", 40000)
	nuzno(t, NewPropisiRepository(ured.db, ured.rec).Spremi(ctxRaz, p))
	v, err := ured.rec.Latest(ctxRaz, EntityPropisi, p.ID)
	nuzno(t, err)
	zaSlanje, err := json.Marshal(v)
	nuzno(t, err)
	if len(v.Payload) < 600_000 || len(zaSlanje) > 63<<20 {
		t.Errorf("sadržaj %d B, za slanje %d B", len(v.Payload), len(zaSlanje))
	}
	posalji(t, ured, server)
	x, err := NewPropisiRepository(server.db, server.rec).Get(ctxRaz, p.ID)
	if err != nil || x == nil || x.Dijelovi[2].Stavci[0].Tekst != p.Dijelovi[2].Stavci[0].Tekst {
		t.Fatalf("velik propis nije stigao cijeli: %v", err)
	}
}
