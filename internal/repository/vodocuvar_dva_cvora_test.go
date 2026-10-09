package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Dva dnevna lista istog vodočuvara za isti dan, Odluke (8. 10. 2026.),
// pitanje 7: oznaka novog lista izvodi se iz osobe i dana, pa svaki čvor
// stvara isti list, a razmjena vidi jedan list u dvije verzije. Predan list
// uvijek ima prednost pred nacrtom s drugog čvora, a bilješke rukovoditelja
// s druge verzije ne gube se. Osobe su izmišljene: vodočuvar Ivo Ivić,
// rukovoditeljica branjenog područja Ana Anić.

var (
	ivoIvic = "01900000-0000-7000-8000-0000000017a1"
	anaAnic = "01900000-0000-7000-8000-0000000017a2"
	danIve  = time.Date(2026, time.October, 14, 0, 0, 0, 0, models.Zagreb)
)

// listIve je nov list Ive Ivića za 14. 10., bez oznake
func listIve(opis string) *models.VodocuvarskiList {
	return &models.VodocuvarskiList{UserID: ivoIvic, Ime: "Ivo Ivić", Sektor: "B", AreaID: 16, Datum: danIve, Broj: 41,
		Od: "07:00", Do: "15:00", Opis: opis}
}

// upisAne je bilješka rukovoditeljice u zadano vrijeme
func upisAne(tekst string, kad time.Time) models.UpisRukovoditelja {
	return models.UpisRukovoditelja{UserID: anaAnic, Ime: "Ana Anić", Funkcija: "Rukovoditeljica branjenog područja", Kad: kad, Tekst: tekst}
}

// predajIvu predaje list Ive Ivića na čvoru
func predajIvu(t *testing.T, n *cvor, opis string) *models.VodocuvarskiList {
	t.Helper()
	l := listIve(opis)
	kad := time.Now()
	l.PredanoAt = &kad
	nuzno(t, NewVodocuvarRepository(n.db, n.rec).Predaj(ctxRaz, l, nil))
	return l
}

// nacrtSUpisom sprema na čvoru nacrt lista s bilješkom rukovoditeljice, kao
// „Upiši” na listu kojega ondje još nema
func nacrtSUpisom(t *testing.T, n *cvor, tekst string, kad time.Time) *models.VodocuvarskiList {
	t.Helper()
	l := listIve("")
	l.Upisi = []models.UpisRukovoditelja{upisAne(tekst, kad)}
	nuzno(t, NewVodocuvarRepository(n.db, n.rec).Save(ctxRaz, l))
	return l
}

// listoviIve vraća sve listove Ive za 14. 10. na čvoru
func listoviIve(t *testing.T, n *cvor) []models.VodocuvarskiList {
	t.Helper()
	l, err := NewVodocuvarRepository(n.db, n.rec).ListoviDana(ctxRaz, ivoIvic, danIve)
	nuzno(t, err)
	return l
}

// jedanListIve provjerava da čvor ima točno jedan list Ive za 14. 10.
func jedanListIve(t *testing.T, n *cvor) models.VodocuvarskiList {
	t.Helper()
	l := listoviIve(t, n)
	if len(l) != 1 {
		t.Fatalf("čvor %s ima %d listova za 14. 10., očekivan jedan", n.ime, len(l))
	}
	return l[0]
}

// isteKnjige razmjenjuje čvorove u oba smjera i provjerava da oba imaju
// isti list
func isteKnjige(t *testing.T, a, b *cvor) models.VodocuvarskiList {
	t.Helper()
	posalji(t, a, b)
	posalji(t, b, a)
	la, lb := jedanListIve(t, a), jedanListIve(t, b)
	ja, _ := json.Marshal(la)
	jb, _ := json.Marshal(lb)
	if string(ja) != string(jb) {
		t.Errorf("čvorovi pokazuju različit list:\n%s: %s\n%s: %s", a.ime, ja, b.ime, jb)
	}
	return la
}

func imaUpis(l models.VodocuvarskiList, tekst string) bool {
	for _, u := range l.Upisi {
		if u.Tekst == tekst {
			return true
		}
	}
	return false
}

// Isti vodočuvar, isti dan: oba čvora daju listu istu oznaku, izvedenu iz
// osobe i dana, pa nakon razmjene postoji jedan list, ne dva.
func TestNovListIsteOsobeIDanaImaIstuOznaku(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	la, lb := listIve("na laptopu"), listIve("na poslužitelju")
	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Save(ctxRaz, la))
	nuzno(t, NewVodocuvarRepository(b.db, b.rec).Save(ctxRaz, lb))
	if la.ID != lb.ID || la.ID != models.IDListaVodocuvara(ivoIvic, danIve) {
		t.Fatalf("oznake lista: %s i %s", la.ID, lb.ID)
	}
	if _, err := uuid.Parse(la.ID); err != nil {
		t.Errorf("oznaka nije UUID: %v", err)
	}
	// oznaka se ne smije mijenjati: moraju je jednako izvesti svi čvorovi
	if id := models.IDListaVodocuvara(ivoIvic, danIve.UTC()); id != "8f882520-d0ac-5514-bdfd-7f5fa70b4714" {
		t.Errorf("oznaka lista se promijenila: %s", id)
	}
	if models.IDListaVodocuvara(ivoIvic, danIve.AddDate(0, 0, 1)) == la.ID || models.IDListaVodocuvara(anaAnic, danIve) == la.ID {
		t.Error("drugi dan ili druga osoba moraju imati drugu oznaku")
	}
	// nijedan nacrt nema vodočuvarov upis noviji od drugoga: vrijedi zadnji
	if l := isteKnjige(t, a, b); l.Opis != "na poslužitelju" {
		t.Errorf("vrijedi nacrt %q, očekivan zadnji", l.Opis)
	}
}

// Ivo preda list na laptopu bez veze, a Ana zatim na poslužitelju upiše
// bilješku za taj dan, što ondje stvori nacrt (primjer 3 iz pitanja 7).
// Nacrt je noviji, ali predan list ima prednost: na oba čvora list je
// predan, s Ivinim opisom i s Aninom bilješkom.
func TestPredanListImaPrednostPredNovijimNacrtom(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	predan := predajIvu(t, a, "obilazak nasipa Batina")
	nacrtSUpisom(t, b, "pojačati noćnu ophodnju kod Batine", time.Date(2026, time.October, 14, 22, 0, 0, 0, models.Zagreb))

	l := isteKnjige(t, a, b)
	if !l.Predan() || !l.PredanoAt.Equal(*predan.PredanoAt) || l.Opis != "obilazak nasipa Batina" {
		t.Errorf("predan list izgubio je prednost: %+v", l)
	}
	if !imaUpis(l, "pojačati noćnu ophodnju kod Batine") {
		t.Errorf("bilješka rukovoditeljice izgubljena: %+v", l.Upisi)
	}
}

// Ana upiše bilješku na poslužitelju prije nego Ivo preda list na laptopu:
// predan list je noviji, a bilješka s nacrta i dalje ostaje na njemu.
func TestBiljeskaSNacrtaOstajeNaPredanomListu(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	nacrtSUpisom(t, b, "ophodnja nasipa svaka 2 sata", time.Date(2026, time.October, 13, 22, 0, 0, 0, models.Zagreb))
	predajIvu(t, a, "ophodnja obavljena")

	l := isteKnjige(t, a, b)
	if !l.Predan() || l.Opis != "ophodnja obavljena" || !imaUpis(l, "ophodnja nasipa svaka 2 sata") {
		t.Errorf("predan list s bilješkom s nacrta: %+v", l)
	}
	// ovjera na poslužitelju upisuje se kako je pročitana, s bilješkom
	repoB := NewVodocuvarRepository(b.db, b.rec)
	ovjeren := jedanListIve(t, b)
	kad := time.Now()
	ovjeren.PotvrdioID, ovjeren.Potvrdio, ovjeren.PotvrdenoAt = anaAnic, "Ana Anić", &kad
	nuzno(t, repoB.Save(ctxRaz, &ovjeren))
	if l := isteKnjige(t, a, b); !l.Potvrden() || !imaUpis(l, "ophodnja nasipa svaka 2 sata") {
		t.Errorf("ovjeren list nakon razmjene: %+v", l)
	}
}

// Dva nacrta s dva čvora bez vodočuvarova upisa: vrijedi zadnji, a bilješke
// s oba ostaju, složene po vremenu upisa. Nacrt s vodočuvarovim upisom ima
// prednost (vodocuvar_nacrt_dva_cvora_test.go).
func TestDvaNacrtaZadnjiVrijediBiljeskeOstaju(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	nacrtSUpisom(t, a, "druga bilješka", time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb))
	nacrtSUpisom(t, b, "prva bilješka", time.Date(2026, time.October, 14, 8, 0, 0, 0, models.Zagreb))
	l := isteKnjige(t, a, b)
	if l.Predan() || len(l.Upisi) != 2 || l.Upisi[0].Tekst != "prva bilješka" || l.Upisi[1].Tekst != "druga bilješka" {
		t.Errorf("dva nacrta: %+v", l.Upisi)
	}
}

// Ivo na poslužitelju obriše svoj nacrt, a na laptopu je list već predao:
// brisanje nacrta ne miče predan list.
func TestBrisanjeNacrtaNeMicePredanList(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	predajIvu(t, a, "obilazak nasipa")
	nacrt := listIve("nacrt na poslužitelju")
	repoB := NewVodocuvarRepository(b.db, b.rec)
	nuzno(t, repoB.Save(ctxRaz, nacrt))
	nuzno(t, repoB.Delete(ctxRaz, nacrt.ID))
	if l := isteKnjige(t, a, b); !l.Predan() || l.Opis != "obilazak nasipa" {
		t.Errorf("brisanje nacrta maknulo je predan list: %+v", l)
	}
}

// Obrisan nacrt odnosi svoje bilješke: list koji vodočuvar zatim napiše
// ispočetka, s istom oznakom, nema ih ni nakon obnove površine iz knjige.
func TestObrisanNacrtNeVracaBiljeske(t *testing.T) {
	a := noviCvor(t, "posluzitelj")
	repo := NewVodocuvarRepository(a.db, a.rec)
	stari := nacrtSUpisom(t, a, "bilješka na obrisanom nacrtu", time.Date(2026, time.October, 14, 8, 0, 0, 0, models.Zagreb))
	nuzno(t, repo.Delete(ctxRaz, stari.ID))
	novi := listIve("napisan ispočetka")
	nuzno(t, repo.Save(ctxRaz, novi))
	if novi.ID != stari.ID {
		t.Fatalf("nov list za isti dan dobio je drugu oznaku: %s, %s", novi.ID, stari.ID)
	}
	if _, err := ReplaySurface(ctxRaz, a.db, a.rec); err != nil {
		t.Fatal(err)
	}
	if l := jedanListIve(t, a); l.Opis != "napisan ispočetka" || len(l.Upisi) != 0 {
		t.Errorf("list nakon obnove površine: %+v", l)
	}
	// list obrisan na jednom čvoru briše se i na drugom
	b := noviCvor(t, "laptop")
	posalji(t, a, b)
	nuzno(t, repo.Delete(ctxRaz, novi.ID))
	posalji(t, a, b)
	if l := listoviIve(t, b); len(l) != 0 {
		t.Errorf("obrisan nacrt ostao je na drugom čvoru: %+v", l)
	}
}

// Isti skup verzija daje isti list bez obzira na to kojim su redom
// stigle: treći čvor koji prvo primi nacrt, a četvrti koji prvo primi
// predan list, pokazuju isto.
func TestListNeOvisiORedoslijeduPrimanja(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	predajIvu(t, a, "obilazak nasipa")
	nacrtSUpisom(t, b, "bilješka", time.Date(2026, time.October, 14, 12, 0, 0, 0, models.Zagreb))
	c, d := noviCvor(t, "treci"), noviCvor(t, "cetvrti")
	posalji(t, b, c)
	posalji(t, a, c)
	posalji(t, a, d)
	posalji(t, b, d)
	lc, ld := jedanListIve(t, c), jedanListIve(t, d)
	if !reflect.DeepEqual(lc, ld) || !lc.Predan() || !imaUpis(lc, "bilješka") {
		t.Errorf("redoslijed primanja mijenja list:\n%+v\n%+v", lc, ld)
	}
}

// Kad za dan postoje dva lista s različitim oznakama (nastali prije
// jedinstvene oznake ili na čvoru starijeg programa), „Današnji list” i
// spremanje uzimaju predan list, a od dva jednaka stariji. Dvojnici se samo
// prijavljuju, ne brišu.
func TestDvaListaIstogDanaPredanPaStariji(t *testing.T) {
	a := noviCvor(t, "posluzitelj")
	repo := NewVodocuvarRepository(a.db, a.rec)
	stari := func(id, opis string, predan bool, stvoren time.Time) {
		t.Helper()
		l := listIve(opis)
		l.ID, l.CreatedAt = id, stvoren
		if predan {
			l.PredanoAt = &stvoren
		}
		_, err := a.db.Exec(vodocuvarskiUpsert, vodocuvarskiArgs(l)...)
		nuzno(t, err)
	}
	t0 := time.Date(2026, time.October, 14, 8, 0, 0, 0, time.UTC)
	stari("01900000-0000-7000-8000-0000000000b2", "noviji nacrt", false, t0.Add(time.Hour))
	stari("01900000-0000-7000-8000-0000000000b1", "stariji nacrt", false, t0)
	if l, err := repo.ZaDan(ctxRaz, ivoIvic, danIve); err != nil || l.Opis != "stariji nacrt" {
		t.Errorf("od dva nacrta mora se uzeti stariji: %+v %v", l, err)
	}
	stari("01900000-0000-7000-8000-0000000000b3", "predan", true, t0.Add(2*time.Hour))
	if l, err := repo.ZaDan(ctxRaz, ivoIvic, danIve); err != nil || l.Opis != "predan" {
		t.Errorf("predan list mora imati prednost: %+v %v", l, err)
	}
	stari("01900000-0000-7000-8000-0000000000b0", "kasnije predan, a stariji", true, t0.Add(-time.Hour))
	if l, err := repo.ZaDan(ctxRaz, ivoIvic, danIve); err != nil || l.Opis != "kasnije predan, a stariji" {
		t.Errorf("od dva predana mora se uzeti stariji: %+v %v", l, err)
	}
	// drugi dan nema dvojnika i ne ulazi u popis
	jedini := listIve("jedini list sutradan")
	jedini.Datum = danIve.AddDate(0, 0, 1)
	nuzno(t, repo.Save(ctxRaz, jedini))
	dvojnici, err := repo.Dvojnici(ctxRaz)
	nuzno(t, err)
	if len(dvojnici) != 4 || dvojnici[0].Opis != "kasnije predan, a stariji" || dvojnici[3].Opis != "noviji nacrt" {
		t.Errorf("dvojnici: %d %+v", len(dvojnici), dvojnici)
	}
	var n int
	nuzno(t, a.db.QueryRow(`SELECT count(*) FROM vodocuvarski_listovi`).Scan(&n))
	if n != 5 {
		t.Errorf("listova u bazi: %d, ništa se ne briše", n)
	}
	if l, err := repo.ZaDan(ctxRaz, anaAnic, danIve); err != nil || l != nil {
		t.Errorf("dan bez lista: %+v %v", l, err)
	}
	// greške čitanja: prekinut upit i oštećen redak
	prekinut, odustani := context.WithCancel(ctxRaz)
	odustani()
	if _, err := repo.ZaDan(prekinut, ivoIvic, danIve); err == nil {
		t.Error("prekinut upit nije javio grešku")
	}
	_, err = a.db.Exec(`UPDATE vodocuvarski_listovi SET predano_at = 'nije vrijeme' WHERE id = ?`, "01900000-0000-7000-8000-0000000000b1")
	nuzno(t, err)
	if _, err := repo.Dvojnici(ctxRaz); err == nil {
		t.Error("oštećen redak nije javio grešku")
	}
}

// Spajanje verzija: bez verzija nema lista; od verzija iste vrste vrijedi
// zadnja; arhivirana predana verzija miče list; isti upis se ne udvostručuje.
func TestSpajanjeVerzijaLista(t *testing.T) {
	kad := time.Now()
	nacrt := func(opis string, upisi ...models.UpisRukovoditelja) verzijaLista {
		return verzijaLista{list: models.VodocuvarskiList{Opis: opis, Upisi: upisi}}
	}
	predan := func(opis string) verzijaLista {
		return verzijaLista{list: models.VodocuvarskiList{Opis: opis, PredanoAt: &kad}}
	}
	if l := spojiVerzijeLista(nil); l != nil {
		t.Errorf("bez verzija: %+v", l)
	}
	if i := vrijediVerzija([]verzijaLista{predan("prvi"), nacrt("nacrt"), predan("drugi"), nacrt("noviji nacrt")}); i != 2 {
		t.Errorf("vrijedi verzija %d, očekivana zadnja predana", i)
	}
	obrisan := predan("obrisan")
	obrisan.arhivirana = true
	if l := spojiVerzijeLista([]verzijaLista{predan("predan"), obrisan, nacrt("kasniji nacrt")}); l != nil {
		t.Errorf("arhivirana predana verzija mora maknuti list: %+v", l)
	}
	u := upisAne("bilješka", time.Date(2026, time.October, 14, 8, 0, 0, 0, models.Zagreb))
	isti := u
	isti.Kad = u.Kad.UTC()
	l := spojiVerzijeLista([]verzijaLista{nacrt("a", u), nacrt("b", isti)})
	if l == nil || l.Opis != "b" || len(l.Upisi) != 1 {
		t.Errorf("isti upis u dvije verzije: %+v", l)
	}
	if got := spojiUpise(nil, nil); got != nil {
		t.Errorf("bez upisa: %+v", got)
	}
}

// Primjena lista javlja grešku kad se verzije ne mogu pročitati: prekinut
// upit, oštećen redak i sadržaj koji nije list.
func TestPrimjenaListaGreske(t *testing.T) {
	a := noviCvor(t, "posluzitelj")
	id := models.IDListaVodocuvara(ivoIvic, danIve)
	tx, err := a.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	defer tx.Rollback()
	prekinut, odustani := context.WithCancel(ctxRaz)
	odustani()
	if err := primijeniListIzPovijesti(prekinut, tx, id); err == nil {
		t.Error("prekinut upit nije javio grešku")
	}
	upisi := func(listID string, arhivirano any, payload string) {
		t.Helper()
		_, err := tx.Exec(`INSERT INTO record_versions (version_id, entity, entity_id, node_id, supersedes, archived, payload, created_at, schema_version, channel)
			VALUES (?, ?, ?, 'posluzitelj', '', ?, ?, ?, 1, '')`, uuid.Must(uuid.NewV7()).String(), EntityVodocuvarski, listID, arhivirano, payload, time.Now().UTC())
		nuzno(t, err)
	}
	upisi(id, 0, `"nije list"`)
	if err := primijeniListIzPovijesti(ctxRaz, tx, id); err == nil {
		t.Error("sadržaj koji nije list nije javio grešku")
	}
	osteceni := models.IDListaVodocuvara(anaAnic, danIve)
	upisi(osteceni, "nije broj", `{}`)
	if _, err := verzijeListaTx(ctxRaz, tx, osteceni); err == nil {
		t.Error("oštećen redak nije javio grešku")
	}
	if !primjenjujeSe(&ledger.Version{VersionID: "b"}, ledger.Version{VersionID: "a", Entity: EntityVodocuvarski}) ||
		primjenjujeSe(&ledger.Version{VersionID: "b"}, ledger.Version{VersionID: "a", Entity: EntityZadaci}) {
		t.Error("starija verzija ide na površinu samo za dnevni list vodočuvara")
	}
}
