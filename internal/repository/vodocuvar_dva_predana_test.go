package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Isti list predan na dva čvora, Odluke (8. 10. 2026.), pitanje 7: predani
// listovi su potpisani dokumenti, pa kad su predana oba, oba ostaju s
// oznakom „dvostruki list – vidi i list br. …”, i nijedan se ne briše. List
// prvog potpisa ostaje pod oznakom lista, s ovjerom; list kasnijeg potpisa
// stoji pod oznakom izvedenom iz oznake lista i potpisa. Osobe su izmišljene:
// vodočuvar Ivo Ivić, rukovoditeljica Ana Anić.

// ovjeriIvu ovjerava list Ive na čvoru, kako ga čvor pokazuje pod oznakom
func ovjeriIvu(t *testing.T, n *cvor, id string) {
	t.Helper()
	repo := NewVodocuvarRepository(n.db, n.rec)
	l, err := repo.Get(ctxRaz, id)
	nuzno(t, err)
	if l == nil {
		t.Fatalf("čvor %s nema list %s", n.ime, id)
	}
	kad := time.Now()
	l.PotvrdioID, l.Potvrdio, l.PotvrdenoAt = anaAnic, "Ana Anić", &kad
	nuzno(t, repo.Save(ctxRaz, l))
}

// dvaListaIve razmjenjuje čvorove u oba smjera i provjerava da oba imaju
// ista dva lista Ive za 14. 10.: prvi predan pod oznakom lista, drugi pod
// izvedenom oznakom
func dvaListaIve(t *testing.T, a, b *cvor) (prvi, drugi models.VodocuvarskiList) {
	t.Helper()
	posalji(t, a, b)
	posalji(t, b, a)
	la, lb := listoviIve(t, a), listoviIve(t, b)
	if len(la) != 2 || len(lb) != 2 {
		t.Fatalf("listova za 14. 10.: %s %d, %s %d; očekivana dva", a.ime, len(la), b.ime, len(lb))
	}
	ja, _ := json.Marshal(la)
	jb, _ := json.Marshal(lb)
	if string(ja) != string(jb) {
		t.Errorf("čvorovi pokazuju različite listove:\n%s\n%s", ja, jb)
	}
	x := models.IDListaVodocuvara(ivoIvic, danIve)
	for _, l := range la {
		if l.ID == x {
			prvi = l
		} else {
			drugi = l
		}
	}
	if prvi.ID == "" || !prvi.Predan() || !drugi.Predan() || drugi.ID != models.IDDvostrukogLista(x, *drugi.PredanoAt) {
		t.Fatalf("dva predana lista: %+v\n%+v", prvi, drugi)
	}
	return prvi, drugi
}

// Ivo preda list na poslužitelju i Ana ga ondje ovjeri; laptop, koji taj
// list još nema, preda isti dan kasnije. Nakon razmjene oba čvora imaju oba
// predana lista: prvi s ovjerom, drugi pod izvedenom oznakom, i oba se
// prijavljuju kao dvojnici.
func TestDvaPredanaIsteOznakeOstajuOba(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	predajIvu(t, a, "predan na poslužitelju")
	ovjeriIvu(t, a, models.IDListaVodocuvara(ivoIvic, danIve))
	predajIvu(t, b, "predan na laptopu")
	// treći čvor prvo primi samo laptop: ondje je laptopov list jedini
	c := noviCvor(t, "treci")
	posalji(t, b, c)
	if l := jedanListIve(t, c); l.Opis != "predan na laptopu" {
		t.Errorf("čvor sa samo jednim potpisom: %+v", l)
	}

	prvi, drugi := dvaListaIve(t, a, b)
	if prvi.Opis != "predan na poslužitelju" || !prvi.Potvrden() || drugi.Opis != "predan na laptopu" || drugi.Potvrden() {
		t.Errorf("prvi %+v\ndrugi %+v", prvi, drugi)
	}
	for _, n := range []*cvor{a, b} {
		d, err := NewVodocuvarRepository(n.db, n.rec).Dvojnici(ctxRaz)
		nuzno(t, err)
		if len(d) != 2 {
			t.Errorf("čvor %s prijavljuje %d dvojnika, očekivana dva", n.ime, len(d))
		}
	}

	// Ana ovjeri i drugi list, na laptopu: ovjera se upisuje u zapis lista,
	// pa je vide oba čvora, a prvi list ostaje ovjeren
	ovjeriIvu(t, b, drugi.ID)
	if _, drugi := dvaListaIve(t, a, b); !drugi.Potvrden() || drugi.Opis != "predan na laptopu" {
		t.Errorf("ovjera drugog lista: %+v", drugi)
	}
	if prvi, _ := dvaListaIve(t, a, b); !prvi.Potvrden() || prvi.Opis != "predan na poslužitelju" {
		t.Errorf("prvi list nakon ovjere drugog: %+v", prvi)
	}
	var vlastitih int
	nuzno(t, a.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity_id = ?`, drugi.ID).Scan(&vlastitih))
	if vlastitih != 0 {
		t.Errorf("drugi list ima %d vlastitih verzija; upisuje se u zapis lista", vlastitih)
	}

	// treći čvor zatim primi poslužitelj: pokazuje isto što i on
	posalji(t, a, c)
	if lc, la := listoviIve(t, c), listoviIve(t, a); len(lc) != 2 {
		t.Errorf("treći čvor: %d listova", len(lc))
	} else if jc, ja := mustJSON(t, lc), mustJSON(t, la); jc != ja {
		t.Errorf("treći čvor pokazuje drukčije:\n%s\n%s", jc, ja)
	}
}

// Brisanje potpisa iz povijesti (arhivirana verzija istog potpisa) miče i
// list pod izvedenom oznakom
func TestDrugiPredaniNestajeSPotpisom(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	predajIvu(t, a, "predan na poslužitelju")
	predajIvu(t, b, "predan na laptopu")
	_, drugi := dvaListaIve(t, a, b)

	tx, err := a.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	if _, err := a.rec.Archive(ctxRaz, tx, EntityVodocuvarski, models.IDListaVodocuvara(ivoIvic, danIve), drugi); err != nil {
		t.Fatal(err)
	}
	nuzno(t, tx.Commit())
	if _, err := ReplaySurface(ctxRaz, a.db, a.rec); err != nil {
		t.Fatal(err)
	}
	if l := jedanListIve(t, a); l.Opis != "predan na poslužitelju" {
		t.Errorf("nakon brisanja drugog potpisa: %+v", l)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	nuzno(t, err)
	return string(b)
}

// sutrasnjiListIve sprema na čvoru list Ive za idući dan, u istom kanalu
func sutrasnjiListIve(t *testing.T, n *cvor) {
	t.Helper()
	sutra := listIve("sutradan na " + n.ime)
	sutra.Datum = danIve.AddDate(0, 0, 1)
	nuzno(t, NewVodocuvarRepository(n.db, n.rec).Save(ctxRaz, sutra))
}

// verzijeSTekstom broji verzije lista Ive za 14. 10. kojima sadržaj nosi tekst
func verzijeSTekstom(t *testing.T, n *cvor, tekst string) int {
	t.Helper()
	var broj int
	nuzno(t, n.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ? AND payload LIKE ?`,
		EntityVodocuvarski, models.IDListaVodocuvara(ivoIvic, danIve), "%"+tekst+"%").Scan(&broj))
	return broj
}

// Sažimanje knjige ne briše nijedan od dva potpisa istog lista: zapis koji
// drži dva potpisa ne sažima se, pa oba lista ostaju i nakon obnove
// površine i na čvoru koji primi samo sažetu knjigu. Ostali zapisi se
// sažimaju kao i dosad.
func TestSazimanjeCuvaObaPredana(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	predajIvu(t, a, "predan na poslužitelju")
	ovjeriIvu(t, a, models.IDListaVodocuvara(ivoIvic, danIve))
	predajIvu(t, b, "predan na laptopu")
	// oba čvora zatim pišu u istom kanalu, pa predaje nisu njihove zadnje
	// verzije, koje sažimanje čuva zbog granice razmjene
	sutrasnjiListIve(t, a)
	sutrasnjiListIve(t, b)
	sutrasnjiListIve(t, b)
	posalji(t, b, a)
	ostariVerzije(t, a)

	if _, err := SazmiKnjigu(ctxRaz, a.db, a.rec, time.Now().AddDate(0, 0, -90)); err != nil {
		t.Fatal(err)
	}
	if verzijeSTekstom(t, a, "predan na laptopu") == 0 || verzijeSTekstom(t, a, "predan na poslužitelju") == 0 {
		t.Fatal("sažimanje je obrisalo potpisan list")
	}
	var sutrasnjih int
	nuzno(t, a.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ?`,
		EntityVodocuvarski, models.IDListaVodocuvara(ivoIvic, danIve.AddDate(0, 0, 1))).Scan(&sutrasnjih))
	if sutrasnjih != 1 {
		t.Errorf("list bez dva potpisa nije sažet: %d verzija", sutrasnjih)
	}
	if _, err := ReplaySurface(ctxRaz, a.db, a.rec); err != nil {
		t.Fatal(err)
	}
	c := noviCvor(t, "novi")
	posalji(t, a, c)
	prvi, drugi := dvaListaIve(t, a, c)
	if !prvi.Potvrden() || prvi.Opis != "predan na poslužitelju" || drugi.Opis != "predan na laptopu" {
		t.Errorf("nakon sažimanja: %+v\n%+v", prvi, drugi)
	}
}

// Primjena lista s dva potpisa javlja grešku kad se list ili drugi list ne
// mogu upisati ili obrisati, i kad se ne može provjeriti ima li drugi list
// svoj zapis
func TestPrimjenaDvaPotpisaGreske(t *testing.T) {
	a := noviCvor(t, "posluzitelj")
	tx, err := a.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	x := models.IDListaVodocuvara(ivoIvic, danIve)
	prvi, drugi := time.Date(2026, time.October, 14, 15, 0, 0, 0, models.Zagreb), time.Date(2026, time.October, 14, 16, 0, 0, 0, models.Zagreb)
	y := models.IDDvostrukogLista(x, drugi)

	prekinut, odustani := context.WithCancel(ctxRaz)
	odustani()
	if err := upisiDvojnike(prekinut, tx, x, slozeniListovi{dvojnici: []models.VodocuvarskiList{{ID: y}}}); err == nil {
		t.Error("prekinut upis drugog lista nije javio grešku")
	}
	if err := upisiDvojnike(prekinut, tx, x, slozeniListovi{potpisi: []time.Time{prvi}}); err == nil {
		t.Error("prekinuto brisanje drugog lista nije javilo grešku")
	}

	for _, kad := range []time.Time{prvi, drugi} {
		l := listIve("predan")
		l.ID, l.PredanoAt = x, &kad
		payload, _ := json.Marshal(l)
		_, err := tx.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, ?, ?, 'posluzitelj', '', 0, ?, ?, 1, '')`, uuid.Must(uuid.NewV7()).String(), EntityVodocuvarski, x, string(payload), time.Now().UTC())
		nuzno(t, err)
	}
	zabrani := func(id string) {
		t.Helper()
		_, err := tx.Exec(`CREATE TRIGGER zabrana BEFORE INSERT ON vodocuvarski_listovi WHEN NEW.id = '` + id + `' BEGIN SELECT RAISE(ABORT, 'zabrana'); END`)
		nuzno(t, err)
	}
	zabrani(y)
	if err := primijeniListIzPovijesti(ctxRaz, tx, x); err == nil {
		t.Error("neuspio upis drugog lista nije javio grešku")
	}
	_, err = tx.Exec(`DROP TRIGGER zabrana`)
	nuzno(t, err)
	zabrani(x)
	if err := primijeniListIzPovijesti(ctxRaz, tx, x); err == nil {
		t.Error("neuspio upis lista nije javio grešku")
	}
}
