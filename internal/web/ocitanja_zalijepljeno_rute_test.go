package web

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/javnivodostaji"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// bezPrava je prijavljena osoba bez ijedne dužnosti: Iva Ivić ne upisuje
// očitanja ni na jednoj letvi
func bezPrava() (*models.User, *models.UserPermissions) {
	u := models.User{ID: uuid.New(), FullName: "Iva Ivić", IsActive: true}
	return &u, models.NewUserPermissions(u)
}

// naLetvi zove rukovatelja letve iz putanje /readings/station/{id}/…
func naLetvi(h http.HandlerFunc, r *http.Request, id string, u *models.User, p *models.UserPermissions) *httpOdgovor {
	r.SetPathValue("id", id)
	w := odgovor(h, r, u, p)
	return &httpOdgovor{w.Code, w.Header().Get("Location"), htmlUTekst(w.Body.String())}
}

// httpOdgovor je status, preusmjerenje i stranica bez HTML zapisa znakova
type httpOdgovor struct {
	status   int
	location string
	html     string
}

// poruka vraća putanju preusmjerenja i poruku iz njezina upita
func (o *httpOdgovor) poruka(t *testing.T, kljuc string) (string, string) {
	t.Helper()
	if o.status != http.StatusSeeOther {
		t.Fatalf("očekivano preusmjerenje, dobiveno %d", o.status)
	}
	u, err := url.Parse(o.location)
	if err != nil {
		t.Fatal(err)
	}
	return u.Path, u.Query().Get(kljuc)
}

// zagreb je trenutak po našem satu
func zagreb(g int, m time.Month, d, h int) time.Time {
	return time.Date(g, m, d, h, 0, 0, 0, models.Zagreb)
}

const ispisLetve = "Vodostaj (cm) - satni podaci za razdoblje 07.09.2026. - 07.09.2026.\n" +
	"Primjerica - Primjerovo (DHMZ)\n" +
	"07.09.2026. 00 h    -118\n" +
	"07.09.2026. 01 h    -118\n" +
	"07.09.2026. 02 h    -119\n" +
	"07.09.2026. 03 h    x119\n"

// Zalijepljeni ispis s letve uspoređuje se s onim što letva već ima: koliko
// je novih, koliko istih, koliko s drugom vrijednošću i koliko nečitljivih.
// Isto se dobije iz obrasca, iz višedijelnog obrasca i iz Excel ispisa, a
// ništa se ne upisuje.
func TestZalijepljenaOcitanjaPregled(t *testing.T) {
	o := novaOkolinaPrava(t)
	o.h.SetUvoz(predlozakStranice(t, "uvoz_ocitanja.html"))
	ctx := context.Background()
	for sat, cm := range map[int]int{0: -118, 1: -100} {
		v := cm
		if err := o.ocitanja.Create(ctx, &models.Reading{ID: uuid.New(), StationID: o.st.ID.String(),
			MeasuredAt: zagreb(2026, 9, 7, sat).UTC(), LevelCm: &v, Source: models.ReadingSourceManual, UserID: o.pero.ID.String()}); err != nil {
			t.Fatal(err)
		}
	}
	prije, _, _, _ := o.ocitanja.Stats(ctx)
	id := o.st.ID.String()
	putanja := "/readings/station/" + id + "/zalijepi"
	excel := napraviXLSX(t, [][]string{
		{"Vodostaj za razdoblje 07.09.2026. - 07.09.2026."},
		{"Vrijeme očitanja", "Rbr.", "Primjerica - Primjerovo (DHMZ)"},
		{"07.09.2026. 00:00 ", "0", "-118"}, {"07.09.2026. 01:00 ", "1", "-118"}, {"07.09.2026. 02:00 ", "2", "-119"},
	})
	for _, s := range []struct {
		opis   string
		r      *http.Request
		greska string
	}{
		{"običan obrazac", obicniObrazac(putanja, url.Values{"tekst": {ispisLetve}}.Encode()), "1 nečitljiv"},
		{"višedijelni obrazac", viseDijelni(t, putanja, map[string]string{"tekst": ispisLetve}), "1 nečitljiv"},
		{"tekstna datoteka s BOM-om", viseDijelni(t, putanja, nil, datotekaObrasca{"datoteka", "ispis.txt", "\ufeff" + ispisLetve}), "1 nečitljiv"},
		{"Excel ispis", viseDijelni(t, putanja, map[string]string{"tekst": "zanemaruje se"}, datotekaObrasca{"datoteka", "ispis.xlsx", string(excel)}), "sve čitljivo"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			odg := naLetvi(o.h.HandleZalijepiPregled, s.r, id, o.pero, o.peroOvl)
			if odg.status != http.StatusOK {
				t.Fatalf("pregled: %d %s", odg.status, odg.location)
			}
			for _, treba := range []string{"Primjerovo · satna očitanja", "novih <strong>1</strong>", "već upisanih <strong>1</strong>",
				"1 s drugom vrijednošću", s.greska} {
				if !strings.Contains(odg.html, treba) {
					t.Errorf("pregled nema %q", treba)
				}
			}
		})
	}
	if poslije, _, _, _ := o.ocitanja.Stats(ctx); poslije != prije {
		t.Errorf("pregled je upisao očitanja: %d → %d", prije, poslije)
	}
}

// Pregled zalijepljenog ne otvara se za letvu koje nema ni bez prava, a
// prazan unos, stari Excel, Excel koji to nije ili nije ispis s letve i tekst
// bez ijednog retka s datumom vraćaju na letvu s porukom.
func TestZalijepljenaOcitanjaPregledOdbija(t *testing.T) {
	o := novaOkolinaPrava(t)
	o.h.SetUvoz(predlozakStranice(t, "uvoz_ocitanja.html"))
	id := o.st.ID.String()
	iva, ivaOvl := bezPrava()
	if odg := naLetvi(o.h.HandleZalijepiPregled, obicniObrazac("/", "tekst=x"), uuid.NewString(), o.pero, o.peroOvl); odg.status != http.StatusNotFound {
		t.Errorf("letva koje nema: %d", odg.status)
	}
	datoteka := func(ime, sadrzaj string) *http.Request {
		return viseDijelni(t, "/", nil, datotekaObrasca{"datoteka", ime, sadrzaj})
	}
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		r      *http.Request
		poruka string
	}{
		{"bez prava", iva, ivaOvl, obicniObrazac("/", "tekst="+url.QueryEscape(ispisLetve)), "Nemate pravo upisivati očitanja na Primjerovo"},
		{"prazno", o.pero, o.peroOvl, viseDijelni(t, "/", map[string]string{"tekst": "  "}, datotekaObrasca{"datoteka", "prazno.txt", ""}),
			"Zalijepi očitanja ili odaberi datoteku"},
		{"stari Excel", o.pero, o.peroOvl, datoteka("ispis.xls", "\xd0\xcf\x11\xe0ostatak"),
			"Ovo je stari Excel oblik (.xls). Spremi datoteku kao .xlsx pa je ponovno pošalji."},
		{"Excel koji to nije", o.pero, o.peroOvl, datoteka("ispis.xlsx", "PK nije zip"), "zip"},
		{"Excel koji nije ispis s letve", o.pero, o.peroOvl, datoteka("ispis.xlsx", string(napraviXLSX(t, [][]string{{"nešto"}, {"1", "2"}}))),
			"u tablici nema stupca „Vrijeme očitanja“"},
		{"bez ijednog retka", o.pero, o.peroOvl, obicniObrazac("/", "tekst=Vodostaj+%28cm%29"), "nijedan redak nije prepoznat"},
		{"prevelika datoteka", o.pero, o.peroOvl, datoteka("ispis.txt", strings.Repeat("x", 16<<20+1)), "datoteka je veća od 16 MB"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			odg := naLetvi(o.h.HandleZalijepiPregled, s.r, id, s.u, s.p)
			putanja, poruka := odg.poruka(t, "error")
			if putanja != "/readings/station/"+id || !strings.Contains(poruka, s.poruka) {
				t.Errorf("%s: %s %q", s.opis, putanja, poruka)
			}
		})
	}
}

// Potvrda upisuje samo retke s čitljivim vremenom i brojem, pod podrijetlom
// koje je stiglo ili zadanim; ista potvrda drugi put ne udvostručuje ništa i
// to kaže.
func TestZalijepljenaOcitanjaPotvrda(t *testing.T) {
	o := novaOkolinaPrava(t)
	id := o.st.ID.String()
	forma := url.Values{
		"kad":        {"2026-09-06T22:00:00Z", "nije vrijeme", "2026-09-06T23:00:00Z", "2026-09-07T00:00:00Z"},
		"vrijednost": {"-118", "5", "x", "-120,0"},
	}
	potvrdi := func(f url.Values) *httpOdgovor {
		return naLetvi(o.h.HandleZalijepiPotvrda, obicniObrazac("/", f.Encode()), id, o.pero, o.peroOvl)
	}
	if _, p := potvrdi(forma).poruka(t, "success"); p != "Upisano 2 od 2 očitanja." {
		t.Errorf("prva potvrda: %q", p)
	}
	if _, p := potvrdi(forma).poruka(t, "success"); p != "Upisano 0 od 2 očitanja. Ostala su već bila upisana." {
		t.Errorf("ponovljena potvrda: %q", p)
	}
	popis, err := o.ocitanja.List(context.Background(), repository.ReadingFilter{StationID: id})
	if err != nil {
		t.Fatal(err)
	}
	upisano := map[string]models.Reading{}
	for _, rd := range popis {
		if rd.Source == models.ReadingSourceImport {
			upisano[rd.MeasuredAt.UTC().Format(time.RFC3339)] = rd
		}
	}
	prvo, drugo := upisano["2026-09-06T22:00:00Z"], upisano["2026-09-07T00:00:00Z"]
	if len(upisano) != 2 || prvo.LevelCm == nil || *prvo.LevelCm != -118 || drugo.LevelCm == nil || *drugo.LevelCm != -120 {
		t.Fatalf("upisano: %+v", upisano)
	}
	if prvo.Origin != "letva.voda.hr — zalijepljeno" || prvo.SourceRef != "letva.voda.hr — zalijepljeno:2026-09-06T22:00:00Z" ||
		prvo.UserID != o.pero.ID.String() {
		t.Errorf("podrijetlo upisanog: %+v", prvo)
	}

	forma.Set("podrijetlo", " HIS-2000 — ispis ")
	forma["kad"], forma["vrijednost"] = []string{"2026-09-07T01:00:00Z"}, []string{"-121"}
	potvrdi(forma)
	popis, _ = o.ocitanja.List(context.Background(), repository.ReadingFilter{StationID: id})
	nadjeno := false
	for _, rd := range popis {
		nadjeno = nadjeno || rd.Origin == "HIS-2000 — ispis" && rd.SourceRef == "HIS-2000 — ispis:2026-09-07T01:00:00Z"
	}
	if !nadjeno {
		t.Error("zadano podrijetlo nije zapisano uz očitanje")
	}
}

// Potvrda za letvu koje nema je 404; bez prijave ili s neispravnim obrascem,
// bez ijednog čitljivog retka i bez prava vraća na letvu s greškom.
func TestZalijepljenaOcitanjaPotvrdaOdbija(t *testing.T) {
	o := novaOkolinaPrava(t)
	id := o.st.ID.String()
	iva, ivaOvl := bezPrava()
	if odg := naLetvi(o.h.HandleZalijepiPotvrda, obicniObrazac("/", ""), uuid.NewString(), o.pero, o.peroOvl); odg.status != http.StatusNotFound {
		t.Errorf("letva koje nema: %d", odg.status)
	}
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		tijelo string
		poruka string
	}{
		{"bez prijave", nil, o.peroOvl, "kad=2026-09-06T22:00:00Z&vrijednost=1", "Neispravan zahtjev"},
		{"neispravan obrazac", o.pero, o.peroOvl, "kad=%zz", "Neispravan zahtjev"},
		{"ništa čitljivo", o.pero, o.peroOvl, "kad=jucer&vrijednost=1", "Nijedno očitanje nije potvrđeno"},
		{"bez prava", iva, ivaOvl, "kad=2026-09-06T22:00:00Z&vrijednost=1", "nemate pravo upisivati očitanja na Primjerovo"},
	} {
		t.Run(s.opis, func(t *testing.T) {
			putanja, poruka := naLetvi(o.h.HandleZalijepiPotvrda, obicniObrazac("/", s.tijelo), id, s.u, s.p).poruka(t, "error")
			if putanja != "/readings/station/"+id || poruka != s.poruka {
				t.Errorf("%s %q", putanja, poruka)
			}
		})
	}
	if n, _, _, _ := o.ocitanja.Stats(context.Background()); n != 2 {
		t.Errorf("odbijena potvrda upisala je očitanja: %d", n)
	}
}

// Preuzimanje s javne stranice na zahtjev: letva koje nema je 404, bez prava
// i bez veze s javnom stranicom ne kreće, a neuspjelo preuzimanje kaže zašto.
func TestPreuzimanjeSJavneStraniceOdbija(t *testing.T) {
	o := novaOkolinaPrava(t)
	iva, ivaOvl := bezPrava()
	admin := &models.UserPermissions{IsGlobalAdmin: true, User: *o.pero}
	if odg := naLetvi(o.h.HandlePreuzmiJavno, obicniObrazac("/", ""), "nije-uuid", o.pero, o.peroOvl); odg.status != http.StatusNotFound {
		t.Errorf("letva koje nema: %d", odg.status)
	}
	id := o.st.ID.String()
	if _, p := naLetvi(o.h.HandlePreuzmiJavno, obicniObrazac("/", ""), id, iva, ivaOvl).poruka(t, "error"); p != "Nemate pravo upisivati očitanja na Primjerovo" {
		t.Errorf("bez prava: %q", p)
	}
	if _, p := naLetvi(o.h.HandlePreuzmiJavno, obicniObrazac("/", ""), id, o.pero, o.peroOvl).poruka(t, "error"); p !=
		"Letva nije povezana ni s javnom stranicom ni s telemetrijom; poveži je u obrascu letve" {
		t.Errorf("bez uvoznika: %q", p)
	}
	uvoznik := javnivodostaji.NoviUvoznik(repository.NewJavniSpremiste(o.baza, o.ocitanja), nil)
	o.h.SetJavniUvoz(func() *javnivodostaji.Uvoznik { return uvoznik })
	st := &models.Station{ID: uuid.New(), Code: "nepoznatovo", Name: "Nepoznatovo", JavniURL: "https://nepoznato.primjer.test/letva"}
	if err := o.stanice.CreateStation(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	putanja, p := naLetvi(o.h.HandlePreuzmiJavno, obicniObrazac("/", ""), st.ID.String(), o.pero, admin).poruka(t, "error")
	if putanja != "/readings/station/"+st.ID.String() || p != "Preuzimanje s javne stranice nije uspjelo: nijedan čitač ne prepoznaje adresu https://nepoznato.primjer.test/letva" {
		t.Errorf("neuspjelo preuzimanje: %s %q", putanja, p)
	}
	if s, ok := uvoznik.Stanje(st.ID.String()); !ok || s.Greska == "" {
		t.Error("neuspjelo preuzimanje nije zapisano u stanje letve")
	}
}

// Pregled zalijepljenog koji se ne da iscrtati je greška poslužitelja.
func TestZalijepljenaOcitanjaPregledBezPredloska(t *testing.T) {
	o := novaOkolinaPrava(t)
	o.h.SetUvoz(template.Must(template.New("prazan").Parse("")))
	odg := naLetvi(o.h.HandleZalijepiPregled, obicniObrazac("/", url.Values{"tekst": {ispisLetve}}.Encode()), o.st.ID.String(), o.pero, o.peroOvl)
	if odg.status != http.StatusInternalServerError {
		t.Errorf("pregled bez predloška: %d", odg.status)
	}
}
