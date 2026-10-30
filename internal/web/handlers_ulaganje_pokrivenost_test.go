package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/models"
)

// operativnaZaUlaganje je operativna baza s letvom primjerovo i očitanjima
// 1. 3. 2026.: tri dojavljena, jedno ručno, jedno sumnjivo i jedno bez
// vrijednosti. Vrata je dobivaju kao bazu za ulaganje.
func operativnaZaUlaganje(t *testing.T, o *okolinaVrata) *sql.DB {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "operativna.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('5f0c1b2a-0000-4000-8000-000000000001', 'primjerovo', 'Primjerovo', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	dan := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	cm := func(v int) *int { return &v }
	for _, r := range []struct {
		sat       int
		cm        *int
		izvor, kv string
	}{
		{6, cm(120), models.ReadingSourceAutomatic, ""},
		{7, cm(122), models.ReadingSourceAutomatic, ""},
		{8, cm(125), models.ReadingSourceAutomatic, ""},
		{9, cm(126), models.ReadingSourceManual, ""},
		{10, cm(400), models.ReadingSourceAutomatic, models.QualityUncertain},
		{11, nil, models.ReadingSourceAutomatic, ""},
	} {
		if _, err := baza.Exec(`INSERT INTO readings (id, station_id, measured_at, level_cm, source, quality, user_id, created_at, updated_at)
			VALUES (?, '5f0c1b2a-0000-4000-8000-000000000001', ?, ?, ?, ?, '', ?, ?)`, uuid.NewString(), dan.Add(time.Duration(r.sat)*time.Hour), r.cm, r.izvor, r.kv, dan, dan); err != nil {
			t.Fatal(err)
		}
	}
	o.h.SetOcitanja(func() *sql.DB { return baza }, func() string { return "cvor-primjer" })
	return baza
}

// idPrimjerova je broj letve primjerovo u operativnoj bazi
const idPrimjerova = "5f0c1b2a-0000-4000-8000-000000000001"

// ulaganjeForma je obrazac ulaganja za letvu i razdoblje
func ulaganjeForma(letva, od, do string) url.Values {
	return url.Values{"ul_letva": {letva}, "ul_od": {od}, "ul_do": {do}}
}

// Pregled kaže što bi ušlo i ne mijenja ništa; krivo zadano i letva koje
// nema vraćaju poruku, prazno razdoblje to kaže.
func TestPregledUlaganja(t *testing.T) {
	o := novaOkolinaVrata(t)
	pregled := func(u *models.User, p *models.UserPermissions, forma url.Values) (int, string) {
		w := o.zovi(o.h.PregledUlaganja, u, p, "/administracija/ulaganje/pregled", forma)
		return w.Code, htmlUTekst(w.Body.String())
	}
	if kod, _ := pregled(o.marko, o.markoOvl, ulaganjeForma("primjerovo", "2026-03-01", "2026-03-01")); kod != http.StatusForbidden {
		t.Errorf("osoba bez letve: %d", kod)
	}
	if _, html := pregled(o.pero, o.peroOvl, ulaganjeForma("primjerovo", "2026-03-01", "2026-03-01")); porukaGreske(html) != "Ovaj čvor ne može ulagati — nema stablo s datotekama ili ne zna gdje arhiva stoji." {
		t.Errorf("bez operativne baze: %q", porukaGreske(html))
	}

	baza := operativnaZaUlaganje(t, o)
	for _, s := range []struct {
		opis  string
		forma url.Values
		ceka  string
	}{
		{"bez letve", ulaganjeForma(" ", "2026-03-01", "2026-03-01"), "zadajte letvu i razdoblje"},
		{"bez razdoblja", ulaganjeForma("primjerovo", "", "2026-03-01"), "zadajte letvu i razdoblje"},
		{"krivi od", ulaganjeForma("primjerovo", "1.3.2026.", "2026-03-01"), "datum „od“ se ne čita: "},
		{"krivi do", ulaganjeForma("primjerovo", "2026-03-01", "sutra"), "datum „do“ se ne čita: "},
		{"do prije od", ulaganjeForma("primjerovo", "2026-03-02", "2026-03-01"), "„do“ je prije „od“"},
		{"nepoznata letva", ulaganjeForma("nepoznato", "2026-03-01", "2026-03-01"), `postaja "nepoznato" nije u registru`},
	} {
		_, html := pregled(o.pero, o.peroOvl, s.forma)
		if g := porukaGreske(html); !strings.HasPrefix(g, s.ceka) {
			t.Errorf("%s: %q", s.opis, g)
		}
	}

	kod, html := pregled(o.pero, o.peroOvl, ulaganjeForma(" primjerovo ", "2026-03-01", "2026-03-01"))
	if kod != http.StatusOK || porukaGreske(html) != "" {
		t.Fatalf("pregled: %d %s", kod, porukeStranice(html))
	}
	for _, treba := range []string{"Primjerovo · 6 očitanja koja još nisu uložena", "Ručno s letve", "Sumnjivo", "Bez vrijednosti",
		`name="ul_od" value="2026-03-01"`, `name="ul_letva" value="primjerovo"`, "Uloži i izgradi"} {
		if !strings.Contains(html, treba) {
			t.Errorf("pregled nema %q", treba)
		}
	}
	var neulozeno int
	if err := baza.QueryRow(`SELECT count(*) FROM readings WHERE izdanje = ''`).Scan(&neulozeno); err != nil || neulozeno != 6 {
		t.Errorf("pregled je nešto označio: neuloženih %d (%v)", neulozeno, err)
	}

	_, html = pregled(o.pero, o.peroOvl, ulaganjeForma("primjerovo", "2026-04-01", "2026-04-30"))
	if !strings.Contains(html, "U tom razdoblju nema očitanja koja još nisu uložena.") {
		t.Errorf("prazno razdoblje: %s", porukeStranice(html))
	}
}

// Ulaganje zapisuje očitanja u stablo, gradi letvu, provjerava arhivu i
// označava uloženo; uz izdavača odmah gleda izdanje. Pospremanje zatim
// briše uloženo iz operative i javlja razmjeni broj obrisanih verzija.
func TestUlaganjeIPospremanje(t *testing.T) {
	o, _, z := okolinaUpisa(t)
	baza := operativnaZaUlaganje(t, o)
	var javljeno []int
	o.h.SetNakonPospremanja(func(n int) { javljeno = append(javljeno, n) })

	if w := o.zovi(o.h.UloziOcitanja, o.marko, o.markoOvl, "/administracija/ulaganje", ulaganjeForma("primjerovo", "2026-03-01", "2026-03-01")); w.Code != http.StatusForbidden {
		t.Errorf("ulaganje bez letve: %d", w.Code)
	}
	for _, s := range []struct {
		opis  string
		forma url.Values
		ceka  string
	}{
		{"krivo razdoblje", ulaganjeForma("primjerovo", "2026-03-02", "2026-03-01"), "„do“ je prije „od“"},
		{"nepoznata letva", ulaganjeForma("nepoznato", "2026-03-01", "2026-03-01"), `postaja "nepoznato" nije u registru`},
		{"prazno razdoblje", ulaganjeForma("primjerovo", "2026-04-01", "2026-04-02"), "U tom razdoblju nema nijednog očitanja koje bi ušlo u arhivu."},
	} {
		w := o.zovi(o.h.UloziOcitanja, o.pero, o.peroOvl, "/administracija/ulaganje", s.forma)
		if g := porukaGreske(htmlUTekst(w.Body.String())); g != s.ceka {
			t.Errorf("%s: %q", s.opis, g)
		}
	}

	// bez izdanja oznaka je današnji dan (UTC); dan se čita prije i poslije
	// posla, da prijelaz ponoći ne smeta
	danPrije := time.Now().UTC().Format("2006-01-02")
	w := o.zovi(o.h.UloziOcitanja, o.pero, o.peroOvl, "/administracija/ulaganje", ulaganjeForma("primjerovo", "2026-03-01", "2026-03-01"))
	p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero)
	danPoslije := time.Now().UTC().Format("2006-01-02")
	if p.Greska() != "" {
		t.Fatalf("posao ulaganja: %s\n%s", p.Greska(), p.Dnevnik())
	}
	if s := p.Stanje().Sazetak; s != "4 vrijednosti su uložene; primjerovo je ponovno izgrađena. Ništa još nije obrisano iz operative." {
		t.Errorf("sažetak ulaganja: %q", s)
	}
	ishod, ok := p.Plod().(*ishodUvoza)
	if !ok || ishod.Letva != "primjerovo" || !strings.Contains(ishod.Put, filepath.Join(o.koren, "primjerica", "primjerovo")) || ishod.Izdanja == nil || !ishod.Izdanja.Probno {
		t.Errorf("ishod ulaganja: %+v", p.Plod())
	}
	if len(z.pozivi) != 1 || z.pozivi[0] != "primjerovo/true" {
		t.Errorf("izdanje se gleda probno jednom: %v", z.pozivi)
	}
	if strings.Contains(p.Dnevnik(), "sastavljam") {
		t.Error("probno izdavanje usred posla piše u dnevnik posla")
	}
	oznake := map[string]int{}
	rows, err := baza.Query(`SELECT izdanje FROM readings`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var iz string
		_ = rows.Scan(&iz) // probni podaci su ispravni; kvar bi pokazala provjera niže
		oznake[iz]++
	}
	rows.Close()
	if oznake[""] != 2 || len(oznake) != 2 {
		t.Fatalf("sumnjivo i prazno ne smiju biti označeni kao uloženo: %v", oznake)
	}
	for iz, n := range oznake {
		if iz != "" && (n != 4 || (iz != danPrije && iz != danPoslije)) {
			t.Errorf("uloženo %d s oznakom %q", n, iz)
		}
	}

	// pospremanje
	pospremi := func(u *models.User, p *models.UserPermissions, postaja string) string {
		return htmlUTekst(o.zovi(o.h.Pospremi, u, p, "/administracija/ulaganje/pospremi", url.Values{"postaja": {postaja}}).Body.String())
	}
	if w := o.zovi(o.h.Pospremi, o.marko, o.markoOvl, "/", url.Values{"postaja": {idPrimjerova}}); w.Code != http.StatusForbidden {
		t.Errorf("pospremanje bez letve: %d", w.Code)
	}
	if g := porukaGreske(pospremi(o.pero, o.peroOvl, " ")); g != "Nije zadano koju letvu pospremiti." {
		t.Errorf("bez letve: %q", g)
	}
	pp := posaoSaStranice(t, o.h.poslovi, pospremi(o.pero, o.peroOvl, idPrimjerova), o.pero)
	if pp.Greska() != "" {
		t.Fatalf("posao pospremanja: %s", pp.Greska())
	}
	if s := pp.Stanje().Sazetak; s != "Obrisano za primjerovo: 4 očitanja i 0 verzija; prostor se vraća nakon VACUUM na Održavanju baze." {
		t.Errorf("sažetak pospremanja: %q", s)
	}
	if len(javljeno) != 1 || javljeno[0] != 0 {
		t.Errorf("razmjeni javljeno %v", javljeno)
	}
	var ostalo int
	if err := baza.QueryRow(`SELECT count(*) FROM readings`).Scan(&ostalo); err != nil || ostalo != 2 {
		t.Errorf("nakon pospremanja ostalo %d očitanja (%v), a moraju ostati samo dva neuložena", ostalo, err)
	}

	// drugo pospremanje iste letve nema što pospremiti: posao pada i ništa
	// ne javlja razmjeni
	pp = posaoSaStranice(t, o.h.poslovi, pospremi(o.pero, o.peroOvl, idPrimjerova), o.pero)
	if pp.Greska() != "ta letva nema uloženih očitanja koja bi se pospremila" || len(javljeno) != 1 {
		t.Errorf("drugo pospremanje: %q, javljeno %v", pp.Greska(), javljeno)
	}
}

// Bez operativne baze ni pospremanje ni ulaganje ne rade, i to kažu.
func TestUlaganjeBezBaze(t *testing.T) {
	o := novaOkolinaVrata(t)
	w := o.zovi(o.h.UloziOcitanja, o.pero, o.peroOvl, "/", ulaganjeForma("primjerovo", "2026-03-01", "2026-03-01"))
	if g := porukaGreske(htmlUTekst(w.Body.String())); g != "Ovaj čvor ne može ulagati — nema stablo s datotekama ili ne zna gdje arhiva stoji." {
		t.Errorf("ulaganje bez baze: %q", g)
	}
	w = o.zovi(o.h.Pospremi, o.pero, o.peroOvl, "/", url.Values{"postaja": {idPrimjerova}})
	if g := porukaGreske(htmlUTekst(w.Body.String())); g != "Ovaj čvor ne može pospremati — ne zna gdje arhiva stoji." {
		t.Errorf("pospremanje bez baze: %q", g)
	}
}

// Niz bez datoteke miče se samo uz sve četiri oznake niza; ishod micanja
// vraća na vrata s brojem maknutih vrijednosti, a greška ostaje na stranici.
func TestMakniSirotana(t *testing.T) {
	o := novaOkolinaVrata(t)
	forma := url.Values{"letva": {"primjerovo"}, "izvor": {"stari"}, "velicina": {"vodostaj"}, "vrsta": {"dnevni"}}
	makni := func(u *models.User, p *models.UserPermissions, f url.Values) (int, string, string) {
		w := o.zovi(o.h.MakniSirotana, u, p, "/administracija/uvoz-niza/makni-niz", f)
		return w.Code, mustUnescape(w.Header().Get("Location")), porukaGreske(htmlUTekst(w.Body.String()))
	}
	if kod, _, _ := makni(o.marko, o.markoOvl, forma); kod != http.StatusForbidden {
		t.Errorf("bez letve: %d", kod)
	}
	if _, _, g := makni(o.pero, o.peroOvl, forma); g != "Ovaj čvor ne može uređivati arhivu." {
		t.Errorf("bez micanja: %q", g)
	}
	var trazeno []string
	greska := errors.New("arhiva je zaključana")
	o.h.SetMakniNiz(func(letva, izvor, velicina, vrsta string) (int, error) {
		trazeno = append(trazeno, strings.Join([]string{letva, izvor, velicina, vrsta}, "/"))
		return 7, greska
	})
	nepotpuna := url.Values{"letva": {"primjerovo"}, "izvor": {"stari"}, "velicina": {" "}, "vrsta": {"dnevni"}}
	if _, _, g := makni(o.pero, o.peroOvl, nepotpuna); g != "Nije zadano koji niz maknuti." || len(trazeno) != 0 {
		t.Errorf("nepotpun niz: %q, traženo %v", g, trazeno)
	}
	if _, _, g := makni(o.pero, o.peroOvl, forma); g != "niz se nije dao maknuti: arhiva je zaključana" {
		t.Errorf("greška micanja: %q", g)
	}
	greska = nil
	kod, l, _ := makni(o.pero, o.peroOvl, forma)
	if kod != http.StatusSeeOther || l != "/administracija/uvoz-niza?success=Maknut niz primjerovo · stari · vodostaj · dnevni (7 vrijednosti). U stablu ga ionako nema, pa se arhiva i dalje da izgraditi iznova." {
		t.Errorf("micanje: %d %s", kod, l)
	}
	if len(trazeno) != 2 || trazeno[1] != "primjerovo/stari/vodostaj/dnevni" {
		t.Errorf("traženo micanje: %v", trazeno)
	}
}
