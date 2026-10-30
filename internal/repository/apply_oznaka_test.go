package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Svaki entitet koji primjena zna upisati ima pravilo za oznaku u sadržaju:
// redak u oznakeUSadrzaju ili slaganje iz povijesti (oznakeIzPovijesti),
// samo jedno od toga, i nijedno pravilo za entitet koji primjene ne znaju.
func TestOznakaUSadrzajuZaSvePrimjene(t *testing.T) {
	for e, p := range primjene {
		if p.upis == nil {
			continue
		}
		_, uSadrzaju := oznakeUSadrzaju[e]
		if uSadrzaju == oznakeIzPovijesti[e] {
			t.Errorf("%s: oznaka u sadržaju %v, iz povijesti %v; treba točno jedno", e, uSadrzaju, oznakeIzPovijesti[e])
		}
	}
	for e := range oznakeUSadrzaju {
		if primjene[e].upis == nil {
			t.Errorf("oznaka u sadržaju za %s, a primjena ga ne upisuje", e)
		}
	}
	for e := range oznakeIzPovijesti {
		if primjene[e].upis == nil {
			t.Errorf("oznaka iz povijesti za %s, a primjena ga ne upisuje", e)
		}
	}
}

// Za svaki entitet: verzija koju je drugi čvor upisao kroz repozitorij nosi
// u sadržaju oznaku svog zapisa. Ista verzija pod drugom oznakom zapisa ne
// mijenja površinu primatelja, ni pod oznakom iz sadržaja ni pod oznakom
// zapisa, a ostaje u njegovoj knjizi. Ispravna razmjena istih zapisa
// provjerava se u TestRazmjenaSvihEntiteta.
func TestPrimjenaUzimaOznakuZapisa(t *testing.T) {
	zaSlucajeveRazmjene(t)
	for _, s := range slucajeviRazmjene() {
		t.Run(s.entitet, func(t *testing.T) { provjeriOznakuZapisa(t, s) })
	}
}

func provjeriOznakuZapisa(t *testing.T, s slucajRazmjene) {
	a, b := noviCvor(t, "ured"), noviCvor(t, "server")
	s.napravi(t, a)
	id := jedinoIzKnjige(t, a, s.entitet)
	top, err := a.rec.Latest(ctxRaz, s.entitet, id)
	nuzno(t, err)
	if !istaOznaka(*top) || oznakeIzPovijesti[s.entitet] && !verzijaListaPodOznakom(t, *top) {
		t.Fatalf("verzija upisana kroz repozitorij nosi u sadržaju drugu oznaku: %s", top.Payload)
	}

	var arg any = id
	if s.broj {
		n, err := strconv.Atoi(id)
		nuzno(t, err)
		arg = n
	}
	prije := redak(t, b, s, arg)
	drugi := *top
	drugi.EntityID = "drugi-" + id
	drugi.VersionID = noviVid(t)
	primi(t, b, []ledger.Version{drugi})

	if poslije := redak(t, b, s, arg); !reflect.DeepEqual(prije, poslije) {
		t.Errorf("zapis %s se promijenio: prije %v, poslije %v", id, prije, poslije)
	}
	if r := redak(t, b, s, drugi.EntityID); r != nil {
		t.Errorf("na površini je zapis %s: %v", drugi.EntityID, r)
	}
	if u, err := b.rec.Latest(ctxRaz, s.entitet, drugi.EntityID); err != nil || u.VersionID != drugi.VersionID {
		t.Errorf("verzija nije ostala u knjizi: %v %v", u, err)
	}
}

// verzijaListaPodOznakom provjerava verziju dnevnog lista ili izvornika
// njegovim pravilom oznake
func verzijaListaPodOznakom(t *testing.T, v ledger.Version) bool {
	t.Helper()
	if v.Entity == EntityVodocuvarski {
		return listPodOznakom(v.EntityID, *teret[models.VodocuvarskiList](t, v.Payload))
	}
	return izvornikPodOznakom(v.EntityID, *teret[models.IzvornikLista](t, v.Payload))
}

func noviVid(t *testing.T) string {
	t.Helper()
	u, err := uuid.NewV7()
	nuzno(t, err)
	return u.String()
}

// ovjeren akt i verzija istog akta pod drugom oznakom zapisa
func ovjerenAktIDrugaOznaka(t *testing.T) (pravi, drugi ledger.Version) {
	t.Helper()
	a := models.Akt{ID: "akt-1", Sektor: "B", AreaID: 34, Godina: 2026, Radnja: models.AktUspostava, Stupanj: models.PhasePrep,
		Status: models.AktOvjeren, Broj: 1, Potpis: "potpis B-1/2026", Vrijedi: time.Date(2026, 10, 6, 3, 0, 0, 0, models.Zagreb)}
	pravi = verzijaZa(t, EntityAkti, a.ID, noviVid(t), a)
	nacrt := a
	nacrt.Status, nacrt.Broj, nacrt.Potpis, nacrt.Napomena = models.AktNacrt, 0, "", "nacrt"
	drugi = verzijaZa(t, EntityAkti, "drugi-id", noviVid(t), nacrt)
	return pravi, drugi
}

// Ovjeren akt ostaje ovjeren kad stigne nacrt istog akta pod drugom
// oznakom zapisa: kojim god redom verzije stignu, zajedno ili jedna po
// jedna, i nakon obnove površine iz knjige.
func TestOvjerenAktIDrugaOznakaUSadrzaju(t *testing.T) {
	ctx := context.Background()
	pravi, drugi := ovjerenAktIDrugaOznaka(t)
	for ime, paketi := range map[string][][]ledger.Version{
		"ovjera prva":    {{pravi}, {drugi}},
		"ovjera druga":   {{drugi}, {pravi}},
		"jednim paketom": {{drugi, pravi}},
	} {
		t.Run(ime, func(t *testing.T) {
			baza, repo := bazaAkata(t)
			for _, p := range paketi {
				_, err := repo.rec.Apply(ctx, p)
				nuzno(t, err)
				nuzno(t, ApplyVersions(ctx, baza, repo.rec, p))
			}
			ostajeOvjeren(t, repo, "nakon primanja")
			_, err := ReplaySurface(ctx, baza, repo.rec)
			nuzno(t, err)
			ostajeOvjeren(t, repo, "nakon obnove površine")
			if n := brojRedaka(t, baza, `SELECT COUNT(*) FROM akti`); n != 1 {
				t.Errorf("na površini %d akata", n)
			}
		})
	}
}

func ostajeOvjeren(t *testing.T, repo *AktiRepository, kad string) {
	t.Helper()
	x, err := repo.GetAkt(context.Background(), "akt-1")
	if err != nil || x == nil || x.Status != models.AktOvjeren || x.Broj != 1 || x.Potpis == "" {
		t.Errorf("%s: akt %+v %v", kad, x, err)
	}
}

// Oznaka se iz sadržaja čita kao što je čita primjena: i polje čije se ime
// razlikuje samo u velikim i malim slovima, i oznaka u drugom obliku zapisa
// (UUID velikim slovima) koju primjena upisuje u kanonskom obliku.
func TestOznakaUSadrzajuCitaSeKaoUPrimjeni(t *testing.T) {
	ctx := context.Background()
	baza, repo := bazaAkata(t)
	pravi, drugi := ovjerenAktIDrugaOznaka(t)
	drugi.Payload = bytes.Replace(drugi.Payload, []byte(`"id":"akt-1"`), []byte(`"id":"drugi-id","ID":"akt-1"`), 1)
	for _, v := range []ledger.Version{pravi, drugi} {
		_, err := repo.rec.Apply(ctx, []ledger.Version{v})
		nuzno(t, err)
		nuzno(t, ApplyVersions(ctx, baza, repo.rec, []ledger.Version{v}))
	}
	ostajeOvjeren(t, repo, "polje drugog imena")

	c := noviCvor(t, "b")
	u := userVersion{User: models.User{ID: uuid.New(), Username: "proba2", FullName: "Ana Anić", IsActive: true,
		OrgType: "HRVATSKE_VODE", CreatedAt: time.Now(), UpdatedAt: time.Now()}, PasswordHash: "x"}
	primi(t, c, []ledger.Version{verzijaZa(t, EntityUsers, u.ID.String(), noviVid(t), u)})
	velika := u
	velika.FullName = "Drugo ime"
	sadrzaj, err := json.Marshal(velika)
	nuzno(t, err)
	sadrzaj = bytes.Replace(sadrzaj, []byte(u.ID.String()), []byte(strings.ToUpper(u.ID.String())), 1)
	v := ledger.Version{VersionID: noviVid(t), Entity: EntityUsers, EntityID: strings.ToUpper(u.ID.String()), NodeID: "ured",
		Payload: sadrzaj, SchemaVersion: ledger.SchemaVersion}
	primi(t, c, []ledger.Version{v})
	var ime string
	nuzno(t, c.db.QueryRow(`SELECT full_name FROM users WHERE id = ?`, u.ID.String()).Scan(&ime))
	if ime != "Ana Anić" {
		t.Errorf("račun pod oznakom velikim slovima promijenio je račun: %q", ime)
	}
}

// Kad zadnja verzija zapisa nosi u sadržaju drugu oznaku, na površini je
// zadnja verzija s oznakom zapisa, kojim god redom stigle.
func TestDrugaOznakaNaZadnjojVerziji(t *testing.T) {
	sluzba := func(oznaka, naziv string) models.Sluzba {
		return models.Sluzba{ID: oznaka, Vrsta: "policija", Naziv: naziv}
	}
	prva := verzijaZa(t, EntitySluzbe, "sluzba-1", vid(1), sluzba("sluzba-1", "Prva"))
	druga := verzijaZa(t, EntitySluzbe, "sluzba-1", vid(2), sluzba("sluzba-2", "Druga"))
	for ime, paketi := range map[string][][]ledger.Version{
		"jedna po jedna": {{prva}, {druga}},
		"jednim paketom": {{prva, druga}},
		"samo druga":     {{druga}},
	} {
		t.Run(ime, func(t *testing.T) {
			c := noviCvor(t, "b")
			for _, p := range paketi {
				primi(t, c, p)
			}
			if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM sluzbe WHERE id = 'sluzba-2'`); n != 0 {
				t.Errorf("na površini je služba iz sadržaja druge oznake")
			}
			ocekivano := 0
			if len(paketi) > 1 || len(paketi[0]) > 1 {
				ocekivano = 1
			}
			if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM sluzbe WHERE id = 'sluzba-1' AND naziv = 'Prva'`); n != ocekivano {
				t.Errorf("služba-1 s prvom verzijom: %d, očekivano %d", n, ocekivano)
			}
		})
	}
}

// Opoziv pod drugom oznakom zapisa ne zauzme mjesto opoziva čiju oznaku
// nosi u sadržaju, i kad obje verzije stignu istim paketom.
func TestOpozivIDrugaOznakaUSadrzaju(t *testing.T) {
	c := noviCvor(t, "b")
	pravi := opozivProbe("laptop", "KLJUC")
	primi(t, c, []ledger.Version{
		verzijaZa(t, entitetOpozivi, "drugi-1", vid(1), pravi),
		verzijaZa(t, entitetOpozivi, pravi.ID, vid(2), pravi),
	})
	var kljuc string
	nuzno(t, c.db.QueryRow(`SELECT public_key FROM opozivi WHERE id = ?`, pravi.ID).Scan(&kljuc))
	if kljuc != "KLJUC" {
		t.Errorf("opoziv: ključ %q", kljuc)
	}
	if n := redakaNaPovrsini(t, c, "opozivi"); n != 1 {
		t.Errorf("opoziva na površini: %d", n)
	}
}

// Primjena jedne verzije (i povratak djece obrisanog roditelja) upisuje
// živu verziju samo s oznakom zapisa u sadržaju; spomenik i sadržaj koji se
// ne da pročitati ne provjerava.
func TestApplyOneProvjeravaOznaku(t *testing.T) {
	c := noviCvor(t, "b")
	nuzno(t, primijeniUTx(t, c, verzijaZa(t, EntitySluzbe, "drugi", vid(1), models.Sluzba{ID: "sluzba-1", Naziv: "S"})))
	if n := redakaNaPovrsini(t, c, "sluzbe"); n != 0 {
		t.Errorf("verzija s drugom oznakom upisala je %d službi", n)
	}
	nuzno(t, primijeniUTx(t, c, verzijaZa(t, EntitySluzbe, "sluzba-1", vid(2), models.Sluzba{ID: "sluzba-1", Naziv: "S"})))
	if n := redakaNaPovrsini(t, c, "sluzbe"); n != 1 {
		t.Errorf("verzija s oznakom zapisa upisala je %d službi", n)
	}
	spomenik := verzijaZa(t, EntitySluzbe, "sluzba-1", vid(3), models.Sluzba{ID: "nesto-drugo"})
	spomenik.Archived = true
	nuzno(t, primijeniUTx(t, c, spomenik))
	if n := redakaNaPovrsini(t, c, "sluzbe"); n != 0 {
		t.Errorf("spomenik nije maknuo službu: %d", n)
	}
	if istaOznaka(ledger.Version{Entity: EntitySluzbe, EntityID: "s", Payload: []byte(`[1]`)}) != true {
		t.Error("sadržaj koji se ne da pročitati prepušta se primjeni")
	}
	if istaOznaka(ledger.Version{Entity: "entitet-novijeg-programa", EntityID: "s", Payload: []byte(`{"id":"x"}`)}) != true {
		t.Error("entitet bez pravila oznake ide primjeni")
	}
}

// Verzija s drugom oznakom zabilježi se u dnevnik jednom, i kad se primjena
// ponovi.
func TestDrugaOznakaJednomUDnevniku(t *testing.T) {
	var dnevnik bytes.Buffer
	prije := log.Writer()
	log.SetOutput(&dnevnik)
	t.Cleanup(func() { log.SetOutput(prije) })
	v := verzijaZa(t, EntitySluzbe, "drugi", noviVid(t), models.Sluzba{ID: "sluzba-1"})
	for range 3 {
		if istaOznaka(v) {
			t.Fatal("verzija s drugom oznakom prošla je provjeru")
		}
	}
	if n := strings.Count(dnevnik.String(), v.VersionID); n != 1 {
		t.Errorf("u dnevniku %d puta: %s", n, dnevnik.String())
	}
	if !strings.Contains(dnevnik.String(), `"sluzba-1"`) || !strings.Contains(dnevnik.String(), "ostaje u knjizi") {
		t.Errorf("zapis u dnevniku: %s", dnevnik.String())
	}
}

// Dnevni list i njegov izvornik smiju u sadržaju nositi oznaku zapisa ili,
// predan, oznaku lista drugog potpisa izvedenu iz nje; ništa drugo.
func TestListIIzvornikPodOznakom(t *testing.T) {
	predano := time.Date(2026, 3, 10, 18, 0, 0, 123, time.UTC)
	dvojnik := models.IDDvostrukogLista("list-1", predano)
	for _, s := range []struct {
		ime     string
		l       models.VodocuvarskiList
		smije   bool
		izvorID string
	}{
		{"oznaka zapisa", models.VodocuvarskiList{ID: "list-1"}, true, "list-1"},
		{"list drugog potpisa", models.VodocuvarskiList{ID: dvojnik, PredanoAt: &predano}, true, dvojnik},
		{"nacrt s oznakom drugog potpisa", models.VodocuvarskiList{ID: dvojnik}, false, ""},
		{"drugi list", models.VodocuvarskiList{ID: "list-2", PredanoAt: &predano}, false, "list-2"},
	} {
		if got := listPodOznakom("list-1", s.l); got != s.smije {
			t.Errorf("list, %s: %v", s.ime, got)
		}
		iz := models.IzvornikLista{ListID: s.l.ID, PredanoAt: s.l.PredanoAt}
		if got := izvornikPodOznakom("list-1", iz); got != s.smije {
			t.Errorf("izvornik, %s: %v", s.ime, got)
		}
	}
}

// Spomenik veze dionice i letve miče vezu svoje oznake, i kad u sadržaju
// nosi drugu vezu; sadržaj s vezom iste oznake ili bez veze miče je kao i
// dosad.
func TestSpomenikVezeDioniceILetvePoOznaci(t *testing.T) {
	c := noviCvor(t, "b")
	druga := uuid.MustParse("01900000-0000-7000-8000-00000000a0ff")
	for _, letva := range []uuid.UUID{osnovaLetva, druga} {
		if letva == druga {
			_, err := c.db.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES (?, 'druga', 'Druga', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`, letva.String())
			nuzno(t, err)
		}
		_, err := c.db.Exec(`INSERT INTO section_stations (section_code, station_id, created_at) VALUES (?, ?, '2026-01-01 00:00:00')`, osnovaDionica, letva.String())
		nuzno(t, err)
	}
	spomenik := verzijaZa(t, EntitySectionStations, sectionStationKey(osnovaDionica, druga), vid(1),
		map[string]string{"section_code": osnovaDionica, "station_id": osnovaLetva.String()})
	spomenik.Archived = true
	nuzno(t, primijeniUTx(t, c, spomenik))
	if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM section_stations WHERE station_id = '`+osnovaLetva.String()+`'`); n != 1 {
		t.Errorf("veza iz sadržaja maknuta: ostalo %d", n)
	}
	if n := brojRedaka(t, c.db, `SELECT COUNT(*) FROM section_stations WHERE station_id = '`+druga.String()+`'`); n != 0 {
		t.Errorf("veza oznake zapisa nije maknuta: ostalo %d", n)
	}
	isti := verzijaZa(t, EntitySectionStations, sectionStationKey(osnovaDionica, osnovaLetva), vid(2),
		map[string]string{"section_code": osnovaDionica, "station_id": osnovaLetva.String()})
	isti.Archived = true
	nuzno(t, primijeniUTx(t, c, isti))
	if n := redakaNaPovrsini(t, c, "section_stations"); n != 0 {
		t.Errorf("veza iste oznake nije maknuta: ostalo %d", n)
	}
}

// Bilješka arhive koja u knjizi nosi drugu oznaku u sadržaju ne dobiva
// trenutak kad se s njega makne bilješka koja je ondje stajala.
func TestTrenutakNizaIDrugaOznaka(t *testing.T) {
	c := noviCvor(t, "b")
	kad := time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC)
	biljeska := func(id uuid.UUID, tekst string, izmijenjeno time.Time) models.ArhivaBiljeska {
		return models.ArhivaBiljeska{ID: id, Letva: "proba-letva", Velicina: "vodostaj", Korak: "satni", Vrijeme: kad,
			Vrsta: "napomena", Tekst: tekst, CreatedAt: izmijenjeno, UpdatedAt: izmijenjeno}
	}
	prava, tudja, oznaka := uuid.New(), uuid.New(), uuid.New()
	stoji := verzijaZa(t, EntityBiljeske, prava.String(), vid(1), biljeska(prava, "prava", kad))
	druga := verzijaZa(t, EntityBiljeske, oznaka.String(), vid(2), biljeska(tudja, "druga", kad.Add(time.Hour)))
	primi(t, c, []ledger.Version{stoji, druga})
	if n := redakaNaPovrsini(t, c, "arhiva_biljeske"); n != 1 {
		t.Fatalf("bilješki na površini: %d", n)
	}
	brisanje := stoji
	brisanje.VersionID, brisanje.Archived = vid(3), true
	primi(t, c, []ledger.Version{brisanje})
	if n := redakaNaPovrsini(t, c, "arhiva_biljeske"); n != 0 {
		t.Errorf("trenutak je dobio bilješku s drugom oznakom: %d", n)
	}
}

// Greška čitanja povijesti vraća se kao greška primjene, bez verzije za
// površinu.
func TestZadnjaSIstomOznakomGreskaKnjige(t *testing.T) {
	c := noviCvor(t, "b")
	otkazan, prekini := context.WithCancel(context.Background())
	prekini()
	v := verzijaZa(t, EntitySluzbe, "sluzba-1", vid(1), models.Sluzba{ID: "drugi"})
	if _, primijeni, err := zadnjaSIstomOznakom(otkazan, c.rec, v); err == nil || primijeni {
		t.Errorf("otkazano čitanje knjige: %v %v", primijeni, err)
	}
}
