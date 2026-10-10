package repository

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/models"
	"gocop/internal/sadrzaj"
)

// Izvornik lista predanog na dva čvora, Odluke (8. 10. 2026.), pitanje 7:
// izvornik je PDF lista kako je potpisan, pa ostaje uz list svog potpisa, a
// ovjera ide na PDF lista koji se ovjerava. Osobe su izmišljene: vodočuvar
// Ivo Ivić, rukovoditeljica Ana Anić.

// spremisteZaProbu otvara spremište sadržaja za test i vraća prijašnje na
// kraju; spremište je jedno za program, pa ga dijele svi probni čvorovi
func spremisteZaProbu(t *testing.T) {
	t.Helper()
	s, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	nuzno(t, err)
	prijasnje := Spremiste()
	SetSpremiste(s)
	t.Cleanup(func() {
		SetSpremiste(prijasnje)
		_ = s.Zatvori()
	})
}

// izvornikIve sprema na čvoru PDF uz list Ive pod oznakom
func izvornikIve(t *testing.T, n *cvor, listID, pdf string) {
	t.Helper()
	nuzno(t, NewVodocuvarRepository(n.db, n.rec).SaveIzvornik(ctxRaz, &models.IzvornikLista{ListID: listID, PDF: []byte(pdf), Sazetak: pdf}))
}

// pdfIve čita PDF izvornika lista na čvoru; prazno kad izvornika nema
func pdfIve(t *testing.T, n *cvor, listID string) string {
	t.Helper()
	iz, err := NewVodocuvarRepository(n.db, n.rec).GetIzvornik(ctxRaz, listID)
	nuzno(t, err)
	if iz == nil {
		return ""
	}
	return string(iz.PDF)
}

// razmijeniSve razmjenjuje svaki par čvorova u oba smjera
func razmijeniSve(t *testing.T, cvorovi ...*cvor) {
	t.Helper()
	for _, a := range cvorovi {
		for _, b := range cvorovi {
			if a != b {
				posalji(t, a, b)
			}
		}
	}
}

// Ivo preda list na poslužitelju i na laptopu, svaki s izvornikom. Treći
// čvor prvo primi samo laptop, pa Ana ondje ovjeri jedini list koji vidi,
// laptopov. Kad svi razmijene sve, na svakom čvoru list prvog potpisa ima
// izvornik poslužitelja, bez ovjere, a list drugog potpisa izvornik laptopa
// s Aninom ovjerom.
func TestIzvornikOstajeUzListSvogPotpisa(t *testing.T) {
	spremisteZaProbu(t)
	a, b, c := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop"), noviCvor(t, "treci")
	x := models.IDListaVodocuvara(ivoIvic, danIve)
	predajIvu(t, a, "predan na poslužitelju")
	izvornikIve(t, a, x, "%PDF poslužitelj")
	drugi := predajIvu(t, b, "predan na laptopu")
	izvornikIve(t, b, x, "%PDF laptop")

	posalji(t, b, c)
	ovjeriIvu(t, c, x)
	izvornikIve(t, c, x, "%PDF laptop, ovjera Ane")
	if got := pdfIve(t, c, x); got != "%PDF laptop, ovjera Ane" {
		t.Fatalf("treći čvor prije razmjene: %q", got)
	}

	razmijeniSve(t, a, b, c)
	razmijeniSve(t, a, b, c)
	y := models.IDDvostrukogLista(x, *drugi.PredanoAt)
	for _, n := range []*cvor{a, b, c} {
		prvi, dvojnik := listoviPoOznaci(t, n, x, y)
		if prvi.Opis != "predan na poslužitelju" || prvi.Potvrden() || dvojnik.Opis != "predan na laptopu" || !dvojnik.Potvrden() {
			t.Errorf("čvor %s: prvi %+v\ndrugi %+v", n.ime, prvi, dvojnik)
		}
		if got := pdfIve(t, n, x); got != "%PDF poslužitelj" {
			t.Errorf("čvor %s: izvornik prvog lista %q", n.ime, got)
		}
		if got := pdfIve(t, n, y); got != "%PDF laptop, ovjera Ane" {
			t.Errorf("čvor %s: izvornik drugog lista %q", n.ime, got)
		}
	}

	// ovjera prvog lista na laptopu ide na izvornik poslužitelja i ne dira
	// izvornik drugog lista
	ovjeriIvu(t, b, x)
	izvornikIve(t, b, x, "%PDF poslužitelj, ovjera Ane")
	razmijeniSve(t, a, b, c)
	for _, n := range []*cvor{a, b, c} {
		if got := pdfIve(t, n, x); got != "%PDF poslužitelj, ovjera Ane" {
			t.Errorf("čvor %s: izvornik prvog lista nakon ovjere %q", n.ime, got)
		}
		if got := pdfIve(t, n, y); got != "%PDF laptop, ovjera Ane" {
			t.Errorf("čvor %s: izvornik drugog lista nakon ovjere prvog %q", n.ime, got)
		}
	}

	// obnova površine iz knjige daje isto
	if _, err := ReplaySurface(ctxRaz, c.db, c.rec); err != nil {
		t.Fatal(err)
	}
	if pdfIve(t, c, x) != "%PDF poslužitelj, ovjera Ane" || pdfIve(t, c, y) != "%PDF laptop, ovjera Ane" {
		t.Errorf("nakon obnove površine: %q, %q", pdfIve(t, c, x), pdfIve(t, c, y))
	}
}

// listoviPoOznaci vraća dva lista Ive na čvoru: pod oznakom lista i pod
// oznakom drugog potpisa
func listoviPoOznaci(t *testing.T, n *cvor, x, y string) (prvi, drugi models.VodocuvarskiList) {
	t.Helper()
	repo := NewVodocuvarRepository(n.db, n.rec)
	for _, id := range []string{x, y} {
		l, err := repo.Get(ctxRaz, id)
		nuzno(t, err)
		if l == nil {
			t.Fatalf("čvor %s nema list %s", n.ime, id)
		}
		if id == x {
			prvi = *l
		} else {
			drugi = *l
		}
	}
	return prvi, drugi
}

// List bez oznake osobe i dana (nastao prije nje) predan na dva čvora: drugi
// list stoji pod oznakom izvedenom iz oznake tog lista. Njegova ovjera i
// izvornik ovjere idu u njegov zapis, jer se zapis prvog lista iz njega ne
// može izvesti; površina tada drugi list i izvornik slaže iz tog zapisa.
func TestDrugiListStarogListaImaSvojZapis(t *testing.T) {
	spremisteZaProbu(t)
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	stari := "01900000-0000-7000-8000-0000000000c1"
	predajStari := func(n *cvor, opis string) *models.VodocuvarskiList {
		t.Helper()
		l := listIve(opis)
		l.ID = stari
		kad := time.Now()
		l.PredanoAt = &kad
		nuzno(t, NewVodocuvarRepository(n.db, n.rec).Predaj(ctxRaz, l, nil))
		izvornikIve(t, n, stari, "%PDF "+opis)
		return l
	}
	predajStari(a, "predan na poslužitelju")
	drugi := predajStari(b, "predan na laptopu")
	razmijeniSve(t, a, b)
	y := models.IDDvostrukogLista(stari, *drugi.PredanoAt)
	ovjeriIvu(t, a, y)
	izvornikIve(t, a, y, "%PDF predan na laptopu, ovjera Ane")
	var vlastitih int
	nuzno(t, a.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity_id = ?`, y).Scan(&vlastitih))
	if vlastitih != 2 {
		t.Errorf("drugi list starog lista ima %d vlastitih verzija, očekivane dvije (list i izvornik)", vlastitih)
	}
	razmijeniSve(t, a, b)
	for _, n := range []*cvor{a, b} {
		if _, err := ReplaySurface(ctxRaz, n.db, n.rec); err != nil {
			t.Fatal(err)
		}
		prvi, dvojnik := listoviPoOznaci(t, n, stari, y)
		if prvi.Opis != "predan na poslužitelju" || dvojnik.Opis != "predan na laptopu" || !dvojnik.Potvrden() {
			t.Errorf("čvor %s: prvi %+v\ndrugi %+v", n.ime, prvi, dvojnik)
		}
		if pdfIve(t, n, stari) != "%PDF predan na poslužitelju" || pdfIve(t, n, y) != "%PDF predan na laptopu, ovjera Ane" {
			t.Errorf("čvor %s: izvornici %q, %q", n.ime, pdfIve(t, n, stari), pdfIve(t, n, y))
		}
	}
}

// Izvornik bez potpisa (čvor 0.0.34) ostaje uz list pod svojom oznakom, a
// izvornik potpisa kojega među listovima nema također; obrisan izvornik
// miče se s površine
func TestListIzvornika(t *testing.T) {
	kad := time.Date(2026, time.October, 14, 15, 0, 0, 0, models.Zagreb)
	kasnije := kad.Add(time.Hour)
	s := slozeniListovi{
		glavni:   &models.VodocuvarskiList{ID: "x", PredanoAt: &kad},
		dvojnici: []models.VodocuvarskiList{{ID: "y", PredanoAt: &kasnije}},
	}
	nepoznat := kad.Add(2 * time.Hour)
	for _, slucaj := range []struct {
		iz   models.IzvornikLista
		want string
	}{
		{models.IzvornikLista{ListID: "x"}, "x"},
		{models.IzvornikLista{ListID: "y", PredanoAt: &kad}, "x"},
		{models.IzvornikLista{ListID: "x", PredanoAt: &kasnije}, "y"},
		{models.IzvornikLista{ListID: "x", PredanoAt: &nepoznat}, "x"},
	} {
		if got := listIzvornika("x", s, slucaj.iz); got != slucaj.want {
			t.Errorf("izvornik %+v: list %s, očekivan %s", slucaj.iz, got, slucaj.want)
		}
	}
	nacrt := slozeniListovi{glavni: &models.VodocuvarskiList{ID: "x"}}
	if got := listIzvornika("x", nacrt, models.IzvornikLista{ListID: "x", PredanoAt: &kad}); got != "x" {
		t.Errorf("izvornik uz nacrt: %s", got)
	}
}

// Primjena izvornika javlja grešku kad se verzije ne mogu pročitati ili
// upisati, i ne dira zapis bez izvornika
func TestPrimjenaIzvornikaGreske(t *testing.T) {
	spremisteZaProbu(t)
	a := noviCvor(t, "posluzitelj")
	tx, err := a.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	x := models.IDListaVodocuvara(ivoIvic, danIve)
	upisi := func(entitet, id string, arhivirano any, payload string) {
		t.Helper()
		_, err := tx.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, ?, ?, 'posluzitelj', '', ?, ?, ?, 1, '')`, uuid.Must(uuid.NewV7()).String(), entitet, id, arhivirano, payload, time.Now().UTC())
		nuzno(t, err)
	}
	// zapis bez izvornika: ništa se ne upisuje ni ne briše
	nuzno(t, primijeniIzvornikeIzPovijesti(ctxRaz, tx, x))

	prekinut, odustani := context.WithCancel(ctxRaz)
	odustani()
	if err := primijeniIzvornikeIzPovijesti(prekinut, tx, x); err == nil {
		t.Error("prekinut upit nije javio grešku")
	}
	if _, err := verzijeIzvornikaTx(prekinut, tx, x); err == nil {
		t.Error("prekinut upit izvornika nije javio grešku")
	}
	if err := upisiIzvornikLista(prekinut, tx, x, "drugi", nil); err == nil {
		t.Error("prekinuto brojanje vlastitih verzija nije javilo grešku")
	}

	osteceni := models.IDListaVodocuvara(anaAnic, danIve)
	upisi(EntityVodocuvarskiIzvornici, osteceni, "nije broj", `{}`)
	if _, err := verzijeIzvornikaTx(ctxRaz, tx, osteceni); err == nil {
		t.Error("oštećen redak nije javio grešku")
	}
	upisi(EntityVodocuvarskiIzvornici, x, 0, `"nije izvornik"`)
	if err := primijeniIzvornikeIzPovijesti(ctxRaz, tx, x); err == nil {
		t.Error("sadržaj koji nije izvornik nije javio grešku")
	}

	// bajtovi kojima otisak ne odgovara ne ulaze u spremište, a izvornik
	// ni na površinu
	treci := models.IDListaVodocuvara(ivoIvic, danIve.AddDate(0, 0, 1))
	pogresan, _ := json.Marshal(models.IzvornikLista{ListID: treci, PDF: []byte("%PDF"), Otisak: "pogresan"})
	upisi(EntityVodocuvarskiIzvornici, treci, 0, string(pogresan))
	if err := primijeniIzvornikeIzPovijesti(ctxRaz, tx, treci); err == nil {
		t.Error("izvornik s pogrešnim otiskom nije javio grešku")
	}
	var ima int
	nuzno(t, tx.QueryRow(`SELECT count(*) FROM vodocuvarski_izvornici WHERE list_id = ?`, treci).Scan(&ima))
	if ima != 0 {
		t.Error("izvornik s pogrešnim otiskom upisan je na površinu")
	}

	// obrisan izvornik miče se s površine
	cetvrti := models.IDListaVodocuvara(ivoIvic, danIve.AddDate(0, 0, 2))
	_, err = tx.Exec(izvornikListaUpsert, cetvrti, "otisak", 4, "application/pdf", "", time.Now().UTC())
	nuzno(t, err)
	obrisan, _ := json.Marshal(models.IzvornikLista{ListID: cetvrti, Otisak: "otisak"})
	upisi(EntityVodocuvarskiIzvornici, cetvrti, 1, string(obrisan))
	nuzno(t, primijeniIzvornikeIzPovijesti(ctxRaz, tx, cetvrti))
	nuzno(t, tx.QueryRow(`SELECT count(*) FROM vodocuvarski_izvornici WHERE list_id = ?`, cetvrti).Scan(&ima))
	if ima != 0 {
		t.Error("obrisan izvornik ostao je na površini")
	}
}

// Izvornik predanog lista nosi u knjizi potpis lista i ide u njegov zapis;
// izvornik lista kojega u tablici nema ide u zapis pod oznakom iz
// izvornika, bez kanala i bez potpisa
func TestZapisIzvornika(t *testing.T) {
	spremisteZaProbu(t)
	a := noviCvor(t, "posluzitelj")
	l := predajIvu(t, a, "predan")
	izvornikIve(t, a, l.ID, "%PDF predan")
	nepoznat := "01900000-0000-7000-8000-0000000000c9"
	izvornikIve(t, a, nepoznat, "%PDF bez lista")
	for _, slucaj := range []struct {
		id, kanal string
		potpis    *time.Time
	}{
		{l.ID, listChannel(l), l.PredanoAt},
		{nepoznat, "", nil},
	} {
		var kanal, payload string
		nuzno(t, a.db.QueryRow(`SELECT channel, payload FROM record_versions WHERE entity = ? AND entity_id = ?`,
			EntityVodocuvarskiIzvornici, slucaj.id).Scan(&kanal, &payload))
		var iz models.IzvornikLista
		nuzno(t, json.Unmarshal([]byte(payload), &iz))
		if kanal != slucaj.kanal || (iz.PredanoAt == nil) != (slucaj.potpis == nil) || (iz.PredanoAt != nil && !iz.PredanoAt.Equal(*slucaj.potpis)) {
			t.Errorf("izvornik %s: kanal %q, potpis %v; očekivano %q, %v", slucaj.id, kanal, iz.PredanoAt, slucaj.kanal, slucaj.potpis)
		}
	}
}

// Čvor 0.0.34 drugi list prima kao zadnju verziju zapisa lista, pa ga
// parafira i ovjeri pod njegovom oznakom: drugi list tada ima svoj zapis i
// svoj zapis izvornika, i površina ga slaže iz njih. Ovjera, parafa i
// izvornik koje zatim upiše čvor s novim programom idu u te zapise, pa ih
// vide svi čvorovi, a ne samo čvor koji ih je upisao, i ostaju i nakon
// obnove površine.
func TestUpisNaDrugiListSVlastitimZapisomStizeSvima(t *testing.T) {
	spremisteZaProbu(t)
	a, b, stari := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop"), noviCvor(t, "stari")
	predajIvu(t, a, "predan na poslužitelju")
	predajIvu(t, b, "predan na laptopu")
	_, drugi := dvaListaIve(t, a, b)

	// čvor 0.0.34 upisuje verziju pod oznakom drugog lista, s parafom Ane
	parafiran := drugi
	parafiran.Parafe = []models.Parafa{{UserID: anaAnic, Ime: "Ana Anić", Kad: time.Now()}}
	tx, err := stari.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	_, err = stari.rec.RecordIn(ctxRaz, tx, listChannel(&parafiran), EntityVodocuvarski, drugi.ID, parafiran)
	nuzno(t, err)
	_, err = stari.rec.RecordIn(ctxRaz, tx, listChannel(&parafiran), EntityVodocuvarskiIzvornici, drugi.ID,
		models.IzvornikLista{ListID: drugi.ID, Sazetak: "stari", UpdatedAt: time.Now().UTC()})
	nuzno(t, err)
	nuzno(t, tx.Commit())
	posalji(t, stari, a)
	posalji(t, stari, b)

	// Ana na laptopu ovjeri drugi list i spremi izvornik ovjere
	ovjeriIvu(t, b, drugi.ID)
	izvornikIve(t, b, drugi.ID, "%PDF predan na laptopu, ovjera Ane")
	razmijeniSve(t, a, b)
	for _, n := range []*cvor{a, b} {
		if _, err := ReplaySurface(ctxRaz, n.db, n.rec); err != nil {
			t.Fatal(err)
		}
		l, err := NewVodocuvarRepository(n.db, n.rec).Get(ctxRaz, drugi.ID)
		nuzno(t, err)
		if l == nil || !l.Potvrden() || len(l.Parafe) != 1 || l.Opis != "predan na laptopu" {
			t.Errorf("čvor %s: drugi list %+v", n.ime, l)
		}
		if pdf := pdfIve(t, n, drugi.ID); pdf != "%PDF predan na laptopu, ovjera Ane" {
			t.Errorf("čvor %s: izvornik drugog lista %q", n.ime, pdf)
		}
	}
}
