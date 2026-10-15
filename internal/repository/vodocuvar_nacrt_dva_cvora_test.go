package repository

import (
	"encoding/json"
	"testing"
	"time"

	"gocop/internal/models"
)

// Nacrt istog lista s dva čvora, Odluke (8. 10. 2026.), pitanje 7: nacrt
// koji je nastao bez vodočuvarova upisa (rukovoditeljev „Upiši”, akt o
// obrani ili objava prijave na čvoru koji vodočuvarov list još nema) ne
// prepisuje ono što je vodočuvar napisao, a njegove bilješke i prijave
// ostaju na listu. Osobe su izmišljene: vodočuvar Ivo Ivić, rukovoditeljica
// Ana Anić.

// nacrtIve sprema na čvoru nacrt koji je Ivo napisao u zadano vrijeme, kao
// spremanje obrasca lista
func nacrtIve(t *testing.T, n *cvor, opis string, napisano time.Time) *models.VodocuvarskiList {
	t.Helper()
	l := listIve(opis)
	l.Zapazanja = "procjeđivanje kod rkm 12"
	l.Zadaci = []models.ZadatakNaListu{{ID: "zadatak-ustava", Tekst: "pregledati ustavu", Status: models.ZadatakObavljen, Obavljeno: "pregledana"}}
	l.NapisanoAt = &napisano
	nuzno(t, NewVodocuvarRepository(n.db, n.rec).Save(ctxRaz, l))
	return l
}

// Ivo piše nacrt na laptopu, a Ana zatim na poslužitelju, koji Ivin list
// još nema, upiše bilješku. Nakon razmjene list na oba čvora ima Ivin tekst
// i stanje zadataka i Aninu bilješku; kad Ivo zatim spremi novi tekst,
// vrijedi on, a bilješka ostaje.
func TestNacrtRukovoditeljaNeBriseTekstVodocuvara(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	nacrtIve(t, a, "obilazak nasipa Batina", time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb))
	nacrtSUpisom(t, b, "pojačati noćnu ophodnju", time.Date(2026, time.October, 14, 10, 0, 0, 0, models.Zagreb))

	l := isteKnjige(t, a, b)
	if l.Opis != "obilazak nasipa Batina" || l.Zapazanja != "procjeđivanje kod rkm 12" || len(l.Zadaci) != 1 || l.Zadaci[0].Status != models.ZadatakObavljen {
		t.Errorf("rukovoditeljev nacrt prepisao je vodočuvarov tekst: %+v", l)
	}
	if !imaUpis(l, "pojačati noćnu ophodnju") {
		t.Errorf("bilješka rukovoditeljice izgubljena: %+v", l.Upisi)
	}

	// Ivo na laptopu mijenja tekst nakon razmjene: vrijedi njegov novi upis
	novi := jedanListIve(t, a)
	napisano := time.Date(2026, time.October, 14, 15, 0, 0, 0, models.Zagreb)
	novi.Opis, novi.Zapazanja, novi.NapisanoAt = "obilazak nasipa Batina i Draž", "", &napisano
	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Save(ctxRaz, &novi))
	if l := isteKnjige(t, a, b); l.Opis != "obilazak nasipa Batina i Draž" || l.Zapazanja != "" || !imaUpis(l, "pojačati noćnu ophodnju") {
		t.Errorf("novi vodočuvarov upis: %+v", l)
	}
}

// Isto vrijedi za akt o obrani i objavu prijave: prijava objavljena na
// poslužitelju, na listu koji ondje nastaje bez Ivina teksta, ostaje upisana
// na listu, a Ivin tekst s laptopa ostaje.
func TestPrijavaSDrugogCvoraOstajeNaListu(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	nacrtIve(t, a, "obilazak nasipa", time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb))
	sPrijavom := listIve("")
	sPrijavom.Prijave = []models.PrijavaNaListu{{ID: "prijava-rampa", Oznaka: "B-7/2026", Naslov: "Oštećena rampa", Kad: time.Date(2026, time.October, 14, 11, 0, 0, 0, models.Zagreb)}}
	nuzno(t, NewVodocuvarRepository(b.db, b.rec).Save(ctxRaz, sPrijavom))

	l := isteKnjige(t, a, b)
	if l.Opis != "obilazak nasipa" || len(l.Prijave) != 1 || l.Prijave[0].ID != "prijava-rampa" {
		t.Errorf("list s prijavom s drugog čvora: %+v", l)
	}
}

// Spajanje prijava: prijava koje na listu nema dodaje se, ista se ne
// udvostručuje, a dodane se slažu po vremenu.
func TestSpajanjePrijavaLista(t *testing.T) {
	prva := models.PrijavaNaListu{ID: "p1", Kad: time.Date(2026, time.October, 14, 8, 0, 0, 0, time.UTC)}
	druga := models.PrijavaNaListu{ID: "p2", Kad: time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)}
	if got := spojiPrijave([]models.PrijavaNaListu{druga}, []models.PrijavaNaListu{druga, prva}); len(got) != 2 || got[0].ID != "p1" || got[1].ID != "p2" {
		t.Errorf("spojene prijave: %+v", got)
	}
	if got := spojiPrijave(nil, nil); got != nil {
		t.Errorf("bez prijava: %+v", got)
	}
}

// Ivo na laptopu napiše pa obriše nacrt, a za to vrijeme Ana na
// poslužitelju upiše bilješku za isti dan. Brisanje odnosi samo ono što je
// laptop znao: nakon razmjene list s Aninom bilješkom ostaje na oba čvora.
// Kad laptop zatim zna za bilješku i obriše list, brisanje vrijedi svugdje.
func TestBrisanjeNaDrugomCvoruNeBriseBiljesku(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	nacrt := nacrtIve(t, b, "nacrt na laptopu", time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb))
	nacrtSUpisom(t, a, "pojačati noćnu ophodnju", time.Date(2026, time.October, 14, 10, 0, 0, 0, models.Zagreb))
	repoB := NewVodocuvarRepository(b.db, b.rec)
	nuzno(t, repoB.Delete(ctxRaz, nacrt.ID))

	l := isteKnjige(t, a, b)
	if !imaUpis(l, "pojačati noćnu ophodnju") || l.Opis != "" {
		t.Errorf("brisanje na laptopu odnijelo je bilješku s poslužitelja: %+v", l)
	}
	nuzno(t, repoB.Delete(ctxRaz, nacrt.ID))
	posalji(t, b, a)
	if la, lb := listoviIve(t, a), listoviIve(t, b); len(la) != 0 || len(lb) != 0 {
		t.Errorf("brisanje lista kojemu je laptop znao bilješku: %d i %d listova", len(la), len(lb))
	}
}

// Ivo na poslužitelju obriše nacrt, a na laptopu je bez veze napisao noviji
// tekst za isti dan: brisanje ne odnosi tekst za koji poslužitelj nije znao.
func TestBrisanjeNeOdnosiNovijiNacrtSDrugogCvora(t *testing.T) {
	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	stari := nacrtIve(t, a, "stari nacrt", time.Date(2026, time.October, 14, 8, 0, 0, 0, models.Zagreb))
	posalji(t, a, b)
	novi := jedanListIve(t, b)
	napisano := time.Date(2026, time.October, 14, 12, 0, 0, 0, models.Zagreb)
	novi.Opis, novi.NapisanoAt = "noviji nacrt na laptopu", &napisano
	nuzno(t, NewVodocuvarRepository(b.db, b.rec).Save(ctxRaz, &novi))
	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Delete(ctxRaz, stari.ID))

	if l := isteKnjige(t, a, b); l.Opis != "noviji nacrt na laptopu" {
		t.Errorf("brisanje starog nacrta odnijelo je noviji: %+v", l)
	}
}

// Što koja verzija pokriva: nacrt pokriva kasnija verzija koja ima sve
// njegove bilješke i prijave i nije starijeg vodočuvarova upisa; predan list
// samo verzija istog potpisa. Vrijeme upisa iz baze (nanosekunde) ostaje
// isto, pa brisanje spremljenog nacrta pokriva i nacrt s istim upisom.
func TestPokrivanjeVerzijaLista(t *testing.T) {
	rano, kasno := time.Date(2026, time.October, 14, 8, 0, 0, 0, time.UTC), time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)
	nacrt := func(n *time.Time, upisi ...models.UpisRukovoditelja) verzijaLista {
		return verzijaLista{list: models.VodocuvarskiList{NapisanoAt: n, Upisi: upisi}}
	}
	biljeska := upisAne("bilješka", rano)
	slucajevi := []struct {
		ime     string
		w, v    verzijaLista
		pokriva bool
	}{
		{"noviji upis vodočuvara", nacrt(&kasno), nacrt(&rano), true},
		{"stariji upis vodočuvara", nacrt(&rano), nacrt(&kasno), false},
		{"nacrt bez upisa ne pokriva upis", nacrt(nil), nacrt(&rano), false},
		{"bez bilješke", nacrt(&kasno), nacrt(&rano, biljeska), false},
		{"s bilješkom", nacrt(&kasno, biljeska), nacrt(&rano, biljeska), true},
		{"prijava", verzijaLista{}, verzijaLista{list: models.VodocuvarskiList{Prijave: []models.PrijavaNaListu{{ID: "p1"}}}}, false},
		{"nacrt ne pokriva predan", nacrt(&kasno), verzijaLista{list: models.VodocuvarskiList{PredanoAt: &rano}}, false},
		{"drugi potpis", verzijaLista{list: models.VodocuvarskiList{PredanoAt: &kasno}}, verzijaLista{list: models.VodocuvarskiList{PredanoAt: &rano}}, false},
		{"isti potpis", verzijaLista{list: models.VodocuvarskiList{PredanoAt: &rano}}, verzijaLista{list: models.VodocuvarskiList{PredanoAt: &rano}}, true},
	}
	for _, s := range slucajevi {
		if got := pokriva(s.w, s.v); got != s.pokriva {
			t.Errorf("%s: pokriva %v, očekivano %v", s.ime, got, s.pokriva)
		}
	}

	a, b := noviCvor(t, "posluzitelj"), noviCvor(t, "laptop")
	l := listIve("nacrt")
	sad := time.Now()
	l.NapisanoAt = &sad
	repo := NewVodocuvarRepository(a.db, a.rec)
	nuzno(t, repo.Save(ctxRaz, l))
	if iz := jedanListIve(t, a); iz.NapisanoAt == nil || !iz.NapisanoAt.Equal(sad) {
		t.Fatalf("vrijeme upisa iz baze: %v, upisano %v", iz.NapisanoAt, sad)
	}
	posalji(t, a, b)
	nuzno(t, repo.Delete(ctxRaz, l.ID))
	posalji(t, a, b)
	if n := len(listoviIve(t, b)); n != 0 {
		t.Errorf("obrisan nacrt s upisom vodočuvara ostao je na drugom čvoru: %d", n)
	}
}

// Nacrt za dan koji još nije počeo nema broja, a broj dobiva prvim
// spremanjem kad njegov dan počne (Odluke (8. 10. 2026.), pitanje 5). Ivo
// na laptopu navečer dopuni nacrt za sutra (bez broja), a Ana ujutro na
// poslužitelju, koji ima raniji nacrt, upiše bilješku, i list ondje dobije
// broj 41. Nakon razmjene vrijedi Ivin tekst, a broj 41 ostaje: inače bi ga
// knjiga poslužitelja izgubila i mogla dati drugom listu.
func TestBrojNacrtaSDrugogCvoraOstaje(t *testing.T) {
	a, b := noviCvor(t, "laptop"), noviCvor(t, "posluzitelj")
	plan := listIve("plan obilaska")
	plan.Broj, plan.NapisanoAt = 0, ptrVrijeme(time.Date(2026, time.October, 13, 18, 0, 0, 0, models.Zagreb))
	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Save(ctxRaz, plan))
	isteKnjige(t, a, b)

	dopuna := jedanListIve(t, a)
	dopuna.Opis, dopuna.NapisanoAt = "plan obilaska i košnje", ptrVrijeme(time.Date(2026, time.October, 13, 23, 0, 0, 0, models.Zagreb))
	nuzno(t, NewVodocuvarRepository(a.db, a.rec).Save(ctxRaz, &dopuna))
	upis := jedanListIve(t, b)
	upis.Broj = 41
	upis.Upisi = append(upis.Upisi, upisAne("pojačati ophodnju", time.Date(2026, time.October, 14, 7, 0, 0, 0, models.Zagreb)))
	nuzno(t, NewVodocuvarRepository(b.db, b.rec).Save(ctxRaz, &upis))

	if l := isteKnjige(t, a, b); l.Broj != 41 || l.Opis != "plan obilaska i košnje" || !imaUpis(l, "pojačati ophodnju") {
		t.Errorf("nakon razmjene: broj %d, opis %q, upisi %+v", l.Broj, l.Opis, l.Upisi)
	}
}

func ptrVrijeme(t time.Time) *time.Time { return &t }

// Broj složenog lista: broj verzije koja vrijedi ostaje kakav jest (kako se
// broj dodjeljuje za tekući i prošli dan, ova dorada ne mijenja); bez njega
// najmanji broj iz drugih verzija, neovisno o redoslijedu; bez ijednog
// broja nacrt ostaje bez broja.
func TestBrojIzVerzija(t *testing.T) {
	verzije := func(brojevi ...int) []verzijaLista {
		var out []verzijaLista
		for _, b := range brojevi {
			out = append(out, verzijaLista{list: models.VodocuvarskiList{Broj: b}})
		}
		return out
	}
	for _, s := range []struct {
		vrijedi int
		zive    []verzijaLista
		broj    int
	}{
		{42, verzije(42, 41), 42},
		{0, verzije(0, 43, 41), 41},
		{0, verzije(41, 0, 43), 41},
		{0, verzije(0, 0), 0},
	} {
		if got := brojIzVerzija(s.vrijedi, s.zive); got != s.broj {
			t.Errorf("broj %d uz %+v: %d, očekivan %d", s.vrijedi, s.zive, got, s.broj)
		}
	}
}

// list034 je dnevni list kakav ga zapisuje čvor 0.0.34: bez polja
// napisano_at, koje taj program ne poznaje
type list034 struct {
	ID     string    `json:"id"`
	UserID string    `json:"user_id"`
	Sektor string    `json:"sektor"`
	AreaID int       `json:"area_id"`
	Datum  time.Time `json:"datum"`
	Opis   string    `json:"opis"`
}

// Čvor 0.0.34 polje napisano_at ne poznaje, ali ga knjiga pri upisu
// prenosi iz prethodne verzije (polja koja program ne poznaje). Ivina
// izmjena nacrta na takvom čvoru zato nosi isti trenutak upisa kao verzija
// koju je izmijenila, pa nakon razmjene vrijedi ona, a ne starija.
func TestIzmjenaNaCvoru034NosiTrenutakUpisa(t *testing.T) {
	a, stari := noviCvor(t, "novi"), noviCvor(t, "stari")
	napisano := time.Date(2026, time.October, 14, 9, 0, 0, 0, models.Zagreb)
	l := nacrtIve(t, a, "tekst s novog", napisano)
	posalji(t, a, stari)

	tx, err := stari.db.BeginTx(ctxRaz, nil)
	nuzno(t, err)
	_, err = stari.rec.RecordIn(ctxRaz, tx, listChannel(l), EntityVodocuvarski, l.ID,
		list034{ID: l.ID, UserID: l.UserID, Sektor: l.Sektor, AreaID: l.AreaID, Datum: l.Datum, Opis: "tekst sa starog"})
	nuzno(t, err)
	nuzno(t, tx.Commit())
	var payload string
	nuzno(t, stari.db.QueryRow(`SELECT payload FROM record_versions WHERE entity = ? AND entity_id = ? ORDER BY version_id DESC LIMIT 1`,
		EntityVodocuvarski, l.ID).Scan(&payload))
	var zapisan models.VodocuvarskiList
	nuzno(t, json.Unmarshal([]byte(payload), &zapisan))
	if zapisan.NapisanoAt == nil || !zapisan.NapisanoAt.Equal(napisano) {
		t.Fatalf("izmjena s čvora 0.0.34 nije prenijela napisano_at: %s", payload)
	}
	posalji(t, stari, a)
	if l := jedanListIve(t, a); l.Opis != "tekst sa starog" {
		t.Errorf("novi čvor nakon izmjene na čvoru 0.0.34: %q", l.Opis)
	}
}
