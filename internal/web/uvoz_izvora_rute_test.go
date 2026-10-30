package web

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/uvoz/izvori"
)

const pegelPrimjerova = `[{"timestamp":"2026-08-21T02:00:00+02:00","value":253},{"timestamp":"2026-08-21T03:00:00+02:00","value":255}]`

// Pregled posebnog izvora ne otvara se bez prava; obrazac koji nije
// višedijelni, nepoznat izvor, tuđa letva i datoteka koju format ne čita
// vraćaju poruku, a ništa ne čeka potvrdu.
func TestPosebniIzvorPregledOdbija(t *testing.T) {
	o := novaOkolinaVrata(t)
	pegel := datotekaObrasca{"datoteke", "primjerovo.json", pegelPrimjerova}
	obrazac := func(polja ...string) map[string]string {
		m := map[string]string{"format": "pegelonline", "letva": "primjerovo"}
		for i := 0; i+1 < len(polja); i += 2 {
			m[polja[i]] = polja[i+1]
		}
		return m
	}
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		r      *http.Request
		status int
		poruka string
	}{
		{"bez ijedne letve", o.marko, o.markoOvl, viseDijelni(t, "/", obrazac(), pegel), http.StatusForbidden, "unosi administrator"},
		{"nije višedijelni obrazac", o.pero, o.peroOvl, obicniObrazac("/", "format=pegelonline"), http.StatusOK,
			"datoteke se nisu dale pročitati: request Content-Type isn't multipart/form-data"},
		{"nepoznat izvor", o.pero, o.peroOvl, viseDijelni(t, "/", obrazac("format", "nepoznat"), pegel), http.StatusOK, "Nije odabran izvor."},
		{"tuđa letva", o.ana, o.anaOvl, viseDijelni(t, "/", obrazac("letva", "drugovo"), pegel), http.StatusOK,
			"letva drugovo nije na vašim dionicama — njezine podatke uvozi administrator njezina područja"},
		{"format je ne čita", o.pero, o.peroOvl, viseDijelni(t, "/", obrazac(),
			datotekaObrasca{"datoteke", "primjerovo.txt", pegelPrimjerova}), http.StatusOK, "ne prima datoteke s nastavkom .txt"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			w := odgovor(o.h.PregledIzvora, s.r, s.u, s.p)
			html := htmlUTekst(w.Body.String())
			if w.Code != s.status || !strings.Contains(html, s.poruka) {
				t.Errorf("status %d, očekivan %d s porukom %q: %s", w.Code, s.status, s.poruka, porukeStranice(html))
			}
			if strings.Contains(html, "što bi se upisalo</h2>") {
				t.Error("odbijen pregled pokazuje što bi se upisalo")
			}
		})
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("odbijen pregled zapisao je %v", d)
	}
}

// cekaPegel sprema potvrdu uvoza iz PEGELONLINE-a za letvu primjerovo
func (o *okolinaVrata) cekaPegel(t *testing.T, u *models.User, izvor string, dat ...izvori.Datoteka) string {
	t.Helper()
	f, _ := izvori.Nadji("pegelonline")
	id := novIdUvoza()
	uvoziIzvora.spremi(id, cekaIzvor{format: f.Kod, zadatak: izvori.Zadatak{Koren: o.koren, Sliv: "primjerica",
		Letva: "primjerovo", Izvor: izvor}, datoteke: dat, korisnik: u.ID.String()})
	t.Cleanup(func() { uvoziIzvora.makni(id) })
	return id
}

// Potvrđen posebni izvor upisuje i gradi letvu; za jednu letvu odmah se gleda
// izdanje, a izvor koji ne ulazi u spoj ostaje zapisan u ishodu. Gradnja koja
// padne i uvoz koji se u pozadini ne da izvesti javljaju se kao pad posla.
func TestPosebniIzvorUpisIGradnja(t *testing.T) {
	o, g, z := okolinaUpisa(t)
	pegel := izvori.Datoteka{Ime: "primjerovo.json", Sadrzaj: []byte(pegelPrimjerova)}
	id := o.cekaPegel(t, o.pero, "cekajuci", pegel)
	w := odgovor(o.h.UpisiIzvor, obicniObrazac("/administracija/uvoz-izvora/upisi", "id="+id), o.pero, o.peroOvl)
	p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero)
	ishod, _ := p.Plod().(*ishodUvoza)
	if p.Greska() != "" || p.Stanje().Sazetak != "Upisano i ponovno izgrađeno: primjerovo." || ishod == nil ||
		ishod.Letva != "primjerovo" || ishod.Cekaizvor != "cekajuci" || ishod.Izdanja == nil {
		t.Fatalf("posao upisa: %q %q %+v", p.Greska(), p.Stanje().Sazetak, ishod)
	}
	if len(g.letve) != 1 || len(z.pozivi) != 1 || z.pozivi[0] != "primjerovo/true" {
		t.Errorf("gradnja %v, izdavanje %v", g.letve, z.pozivi)
	}
	if puts, _ := filepath.Glob(filepath.Join(o.koren, "primjerica", "primjerovo", "primjerovo_cekajuci_vodostaj_satni_*.csv")); len(puts) != 1 {
		t.Errorf("niz nije zapisan: %v", puts)
	}

	g.greska = errors.New("arhiva je zaključana")
	id = o.cekaPegel(t, o.pero, "probni", pegel)
	w = odgovor(o.h.UpisiIzvor, obicniObrazac("/", "id="+id), o.pero, o.peroOvl)
	if p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero); p.Greska() != "datoteke su zapisane, ali gradnja letve primjerovo nije uspjela: arhiva je zaključana" {
		t.Errorf("pad gradnje: %q", p.Greska())
	}

	id = o.cekaPegel(t, o.pero, "probni")
	w = odgovor(o.h.UpisiIzvor, obicniObrazac("/", "id="+id), o.pero, o.peroOvl)
	if p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero); p.Greska() != "nije odabrana nijedna datoteka" {
		t.Errorf("uvoz bez datoteke: %q", p.Greska())
	}
}

// lazniHydroViewVrata odgovara kao HydroView: prijava s javnim ključem, jedna
// postaja PRIMJEROVO s radarom i satne vrijednosti 1. ožujka 2001. Račun
// „odbijen” se ne da prijaviti.
func lazniHydroViewVrata(t *testing.T) *httptest.Server {
	t.Helper()
	kljuc, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&kljuc.PublicKey)
	javni := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	pocetak := time.Date(2001, 3, 1, 0, 0, 0, 0, models.Zagreb)
	vrijednosti := [][2]float64{
		{float64(pocetak.Add(6 * time.Hour).Unix()), 1.20},
		{float64(pocetak.Add(7*time.Hour + 20*time.Second).Unix()), 1.25},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "login/get_rsa_key"):
			_ = json.NewEncoder(w).Encode(map[string]string{"rsa_key": javni})
		case strings.HasSuffix(r.URL.Path, "login/get_token"):
			if r.URL.Query().Get("username") == "odbijen" {
				_, _ = fmt.Fprint(w, `{"status":"wrong_credentials"}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"status":"ok","token":"probni-token"}`)
		case r.Header.Get("Authorization") != "Bearer probni-token":
			http.Error(w, "forbidden", http.StatusForbidden)
		case strings.HasSuffix(r.URL.Path, "sites/get_verbose"):
			_, _ = fmt.Fprint(w, `{"status":"ok","sites_get":{"groups_sites":[{"group":{"name":"Primjerica"},"sites":[
				{"site_id":"s1","logger_id":"L1","last_timestamp":0,"site":{"name":"PRIMJEROVO","scan_interval_min":60,"station_id":"P1"}}]}]}}`)
		case strings.HasSuffix(r.URL.Path, "installed_equipment/get"):
			_, _ = fmt.Fprint(w, `{"status":"ok","installed_equipment":{"instruments":[{"name":"LX-80 radar","measurements":[{"msr_id":"m1","qty_id":"#hydro-$7"}]}]}}`)
		case strings.HasSuffix(r.URL.Path, "data/get"):
			od, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
			do, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
			_, _ = fmt.Fprintf(w, "timestamp,%s\n", r.URL.Query().Get("id0"))
			for _, v := range vrijednosti {
				if int64(v[0]) >= od && int64(v[0]) < do {
					_, _ = fmt.Fprintf(w, "%d,%g\n", int64(v[0]), v[1])
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

var hvKljuc = posta.Kljuc([]byte("tajna-probnog-cvora"))

// saHydroViewom daje vratima račune za HydroView: letva primjerovo ima svoj,
// a ostale letve čitaju se računom čvora koji poslužitelj odbija
func (o *okolinaVrata) saHydroViewom(t *testing.T, adresa string) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "hv.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = baza.Close() })
	repo := repository.NewHidroViewRepository(baza)
	for _, r := range []repository.RacunHidroView{
		{Letva: "", Adresa: adresa, Korisnik: "odbijen", Lozinka: zakljucanaLozinka(t, "lozinka-cvora")},
		{Letva: "primjerovo", Adresa: adresa, Korisnik: "primjer", Lozinka: zakljucanaLozinka(t, "lozinka-letve")},
	} {
		if err := repo.Spremi(context.Background(), &r); err != nil {
			t.Fatal(err)
		}
	}
	o.h.hvAdresa = adresa
	o.h.SetHidroView(func() *repository.HidroViewRepository { return repo }, func() []byte { return hvKljuc },
		func(_ context.Context, letva string) string {
			if letva == "primjerovo" {
				return "Primjerovo"
			}
			return ""
		})
}

func zakljucanaLozinka(t *testing.T, lozinka string) []byte {
	t.Helper()
	b, err := posta.Zakljucaj(hvKljuc, lozinka)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Povijest s HydroViewa: provjera samo prebroji što ima, preuzimanje dopiše
// niz u stablo i izgradi letvu. Račun koji se ne da prijaviti i naziv koji
// HydroView ne zna javljaju se kao pad posla.
func TestPovijestSHydroViewa(t *testing.T) {
	o, g, _ := okolinaUpisa(t)
	srv := lazniHydroViewVrata(t)
	o.saHydroViewom(t, srv.URL)
	preuzmi := func(forma url.Values) string {
		w := odgovor(o.h.PreuzmiHidroView, obicniObrazac("/administracija/uvoz-izvora/hidroview", forma.Encode()), o.pero, o.peroOvl)
		return htmlUTekst(w.Body.String())
	}
	razdoblje := url.Values{"letva": {"primjerovo"}, "od": {"2001-03-01"}, "do": {"2001-03-01"}, "probno": {"1"}}
	p := posaoSaStranice(t, o.h.poslovi, preuzmi(razdoblje), o.pero)
	if p.Naziv != "Provjera HydroViewa: primjerovo" || p.Greska() != "" ||
		p.Stanje().Sazetak != "Na HydroViewu za primjerovo ima 2 vrijednosti u razdoblju; ništa nije zapisano." {
		t.Fatalf("provjera: %q %q %q", p.Naziv, p.Greska(), p.Stanje().Sazetak)
	}
	if !strings.Contains(p.Dnevnik(), "PRIMJEROVO (P1), zapisivač L1") || len(g.letve) != 0 || len(o.datoteke(t)) != 0 {
		t.Errorf("provjera je pisala ili gradila: %v %v\n%s", g.letve, o.datoteke(t), p.Dnevnik())
	}

	razdoblje.Del("probno")
	p = posaoSaStranice(t, o.h.poslovi, preuzmi(razdoblje), o.pero)
	if p.Naziv != "Povijest s HydroViewa: primjerovo" || p.Greska() != "" || p.Stanje().Sazetak != "Upisano i ponovno izgrađeno: primjerovo." {
		t.Fatalf("preuzimanje: %q %q %q", p.Naziv, p.Greska(), p.Stanje().Sazetak)
	}
	niz := procitaj(t, filepath.Join(o.koren, "primjerica", "primjerovo", "primjerovo_geolux-radar_vodostaj_satni_2001.csv"))
	if !strings.Contains(niz, ";120\n") || !strings.Contains(niz, ";125\n") || len(g.letve) != 1 {
		t.Errorf("preuzeti niz:\n%s\ngrađeno %v", niz, g.letve)
	}

	// Razdoblje bez ijedne vrijednosti ne zapisuje ništa ni bez provjere.
	p = posaoSaStranice(t, o.h.poslovi, preuzmi(url.Values{"letva": {"primjerovo"}, "od": {"2001-04-01"}, "do": {"2001-04-02"}}), o.pero)
	if p.Stanje().Sazetak != "Na HydroViewu za primjerovo ima 0 vrijednosti u razdoblju; ništa nije zapisano." || len(g.letve) != 1 {
		t.Errorf("prazno razdoblje: %q, građeno %v", p.Stanje().Sazetak, g.letve)
	}

	// Druga letva čita se računom čvora, koji HydroView odbija.
	p = posaoSaStranice(t, o.h.poslovi, preuzmi(url.Values{"letva": {"drugovo"}}), o.pero)
	if p.Greska() != "prijava: prijava odbijena (wrong_credentials)" {
		t.Errorf("odbijena prijava: %q", p.Greska())
	}
	// Naziv koji HydroView ne zna.
	p = posaoSaStranice(t, o.h.poslovi, preuzmi(url.Values{"letva": {"primjerovo"}, "naziv": {"Nepostojevo"}, "od": {"2001-03-01"}}), o.pero)
	if !strings.Contains(p.Greska(), "Nepostojevo") {
		t.Errorf("nepoznat naziv: %q", p.Greska())
	}
}

// Obrazac povijesti s HydroViewa: bez prava se ne otvara, traži letvu i
// sliv, tuđu letvu odbija, nepravilne datume i prazno razdoblje javlja, a bez
// upisanog računa kaže gdje se upisuje. Nijedno odbijanje ne pokreće posao.
func TestPovijestSHydroViewaOdbija(t *testing.T) {
	o := novaOkolinaVrata(t)
	bezRacuna := novaOkolinaVrata(t)
	o.saHydroViewom(t, "http://127.0.0.1:1")
	for _, s := range []struct {
		opis   string
		o      *okolinaVrata
		u      *models.User
		p      *models.UserPermissions
		tijelo string
		status int
		poruka string
	}{
		{"bez ijedne letve", o, o.marko, o.markoOvl, "letva=primjerovo", http.StatusForbidden, "unosi administrator"},
		{"neispravan obrazac", o, o.pero, o.peroOvl, "letva=%zz", http.StatusOK, "invalid URL escape"},
		{"bez letve", o, o.pero, o.peroOvl, "sliv=primjerica", http.StatusOK, "Trebaju letva i sliv."},
		{"letve nema u stablu", o, o.pero, o.peroOvl, "letva=novovo", http.StatusOK, "Trebaju letva i sliv."},
		{"tuđa letva", o, o.ana, o.anaOvl, "letva=drugovo", http.StatusOK,
			"Nemate pravo uvoziti podatke letve drugovo — uvozi ih administrator njezina područja."},
		{"nepravilan datum od", o, o.pero, o.peroOvl, "letva=primjerovo&od=1.3.2001", http.StatusOK, "Datum od nije pravilan."},
		{"nepravilan datum do", o, o.pero, o.peroOvl, "letva=primjerovo&do=2001-13-01", http.StatusOK, "Datum do nije pravilan."},
		{"prazno razdoblje", o, o.pero, o.peroOvl, "letva=primjerovo&od=2001-03-05&do=2001-03-01", http.StatusOK,
			"Razdoblje je prazno: datum od mora biti prije datuma do."},
		{"bez računa", bezRacuna, bezRacuna.pero, bezRacuna.peroOvl, "letva=primjerovo&sliv=primjerica", http.StatusOK,
			"račun za HydroView nije upisan na ovom čvoru (Administracija → Telemetrija)"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			w := odgovor(s.o.h.PreuzmiHidroView, obicniObrazac("/administracija/uvoz-izvora/hidroview", s.tijelo), s.u, s.p)
			html := htmlUTekst(w.Body.String())
			if w.Code != s.status || !strings.Contains(html, s.poruka) {
				t.Errorf("status %d, očekivan %d s porukom %q: %s", w.Code, s.status, s.poruka, porukeStranice(html))
			}
			if rePosaoNaStranici.MatchString(html) {
				t.Error("odbijen obrazac pokrenuo je posao")
			}
		})
	}
}
