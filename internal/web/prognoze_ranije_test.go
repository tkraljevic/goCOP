package web

import (
	"context"
	"database/sql"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	webassets "gocop/web"
)

// Sati dvaju izdanja u bazi za izvoz ranije prognoze: 5. 10. 2026. u 10 h
// i 9. 10. 2026. u 19 h po mjesnom vremenu.
var (
	satStarog = time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC).Unix() / 3600
	satNovog  = time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC).Unix() / 3600
)

// bazaDvaIzdanja: postojeća fikstura s izdanjem 5. 10. (vrijednosti 30 +
// sat) i novije izdanje 9. 10. s drugim brojkama (500 + sat). Svako ima svoj
// zapis, dnevnu prognozu (77 i 777) i mađarsku prognozu koju je izdavač tada
// imao (222 i 333); staro izdanje nosi upozorenje, novo nijedno.
func bazaDvaIzdanja(t *testing.T) string {
	t.Helper()
	put := bazaPrognoza(t, satStarog)
	db, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var izdane []prognoza.Izdana
	for _, letva := range []string{"belisce", "donji-miholjac"} {
		for sat := int64(0); sat <= 72; sat++ {
			v := 500 + float64(sat)
			izdane = append(izdane, prognoza.Izdana{Letva: letva, Velicina: "vodostaj", Izdano: satNovog, Ciljni: satNovog + sat,
				Vrijednost: v, Dolje: v - 2, Gore: v + 2, Model: "proba"})
		}
	}
	if err := prognoza.SpremiIzdane(db, izdane); err != nil {
		t.Fatal(err)
	}
	for _, iz := range []struct {
		sat    int64
		cm, hu float64
	}{{satStarog, 77, 222}, {satNovog, 777, 333}} {
		var dnevne []prognoza.DnevnaIzdana
		for dan := 0; dan <= 6; dan++ {
			dnevne = append(dnevne, prognoza.DnevnaIzdana{Letva: "belisce", Izdano: iz.sat, Dan: dan,
				Ciljni: iz.sat + int64(24*dan), Vrijednost: iz.cm, Dolje: iz.cm - 5, Gore: iz.cm + 5, Model: prognoza.ModelDnevni})
		}
		if err := prognoza.SpremiDnevne(db, dnevne); err != nil {
			t.Fatal(err)
		}
		_, ciljevi := daniPregleda(satOdEpohe(iz.sat))
		if _, err := db.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost, raspon) VALUES (?, 'belisce', ?, ?, ?, 0)`,
			prognoza.Podrijetlo, iz.sat-2, ciljevi[0], iz.hu); err != nil {
			t.Fatal(err)
		}
	}
	upozorenja := map[int64][]prognoza.Upozorenje{
		satStarog: {{Vrsta: prognoza.VrstaUlaz, Tezina: prognoza.TezinaUpozorenje, Letva: "moslavina", Zadnje: satStarog - 11,
			Upitno: []prognoza.Upitno{{Letva: "belisce", OdSata: 7}}}},
		satNovog: {},
	}
	for _, sat := range []int64{satStarog, satNovog} {
		if _, err := prognoza.SpremiZapisIzdanja(db, prognoza.ZapisIzdanja{Izdano: sat, Cvor: "cvor-izdavac",
			Nastalo: satOdEpohe(sat).Add(20 * time.Minute), Upozorenja: upozorenja[sat]}); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

// rukovateljRanijih je rukovatelj prognoza nad bazom s dva izdanja; kiša po
// slivovima javlja iz kojeg je izdanja tražena.
func rukovateljRanijih(t *testing.T) *PrognozeHandler {
	t.Helper()
	c, err := OtvoriPrognoze(bazaDvaIzdanja(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	h := NewPrognozeHandler(nil, func() *CitacPrognoza { return c }, nil)
	h.kisa = func() KisaSlivovaFunc {
		return func(ctx context.Context) ([]prognoza.StanjeSliva, map[string][]prognoza.DnevnaIzdana, error) {
			palo := 99.0
			if sat, ranije := IzdanjeZahtjeva(ctx); ranije && sat == satStarog {
				palo = 11
			}
			return []prognoza.StanjeSliva{{Sliv: "Z", Razina: 1, Palo72: palo, Dolazi48: 1}}, nil, nil
		}
	}
	return h
}

// izvezi poziva izvoz s upitom i vraća odgovor.
func izvezi(h *PrognozeHandler, upit string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.IzvoziPrognoze(w, httptest.NewRequest(http.MethodGet, "/prognoze.xlsx"+upit, nil))
	return w
}

// Izvoz ranijeg izdanja nosi brojke, dnevnu prognozu, upozorenja, kišu i
// mađarsku prognozu toga izdanja, a ne zadnjega; bez dana i sata izvoz je
// kao dosad.
func TestIzvozRanijegIzdanja(t *testing.T) {
	h := rukovateljRanijih(t)
	w := izvezi(h, "?dan=2026-10-05&sat=10")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if d := w.Header().Get("Content-Disposition"); !strings.Contains(d, "prognoza_izdanje_2026-10-05_10h.xlsx") {
		t.Errorf("ime datoteke: %s", d)
	}
	staro := tekstZipa(t, w.Body.Bytes())
	for _, want := range []string{"izdano 5.10.2026. u 10:00 (ranije izdanje)", "Ranije izdanje, izvezeno",
		"Prognoza izdana 5.10.2026. u 10:00, izračunata u 10:20.", ">36<", ">42<", ">77<", ">222<", "nema mjerenja", "palo 72 h 11 mm"} {
		if !strings.Contains(staro, want) {
			t.Errorf("u izvozu ranijeg izdanja nema %q", want)
		}
	}
	for _, nema := range []string{">506<", ">777<", ">333<", "nije izdana;", "Zadnja prognoza izdana je"} {
		if strings.Contains(staro, nema) {
			t.Errorf("u izvozu ranijeg izdanja je %q", nema)
		}
	}

	w = izvezi(h, "")
	if w.Code != http.StatusOK {
		t.Fatalf("zadnje izdanje: status %d: %s", w.Code, w.Body.String())
	}
	if d := w.Header().Get("Content-Disposition"); strings.Contains(d, "izdanje") {
		t.Errorf("ime datoteke zadnjeg izdanja: %s", d)
	}
	zadnje := tekstZipa(t, w.Body.Bytes())
	for _, want := range []string{">506<", ">777<", ">333<", "palo 72 h 99 mm"} {
		if !strings.Contains(zadnje, want) {
			t.Errorf("u izvozu zadnjeg izdanja nema %q", want)
		}
	}
	for _, nema := range []string{"ranije izdanje", ">222<", "nema mjerenja"} {
		if strings.Contains(zadnje, nema) {
			t.Errorf("u izvozu zadnjeg izdanja je %q", nema)
		}
	}
}

// Sat bez izdanja daje najbliže ranije, s napomenom; zastarjelost se računa
// prema traženom satu. Prije prvog izdanja i uz krivi upit izvoza nema.
func TestIzvozRanijegBezIzdanjaUSatu(t *testing.T) {
	h := rukovateljRanijih(t)
	w := izvezi(h, "?dan=2026-10-05&sat=15")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if d := w.Header().Get("Content-Disposition"); !strings.Contains(d, "prognoza_izdanje_2026-10-05_10h.xlsx") {
		t.Errorf("ime datoteke: %s", d)
	}
	txt := tekstZipa(t, w.Body.Bytes())
	for _, want := range []string{"Za 5.10.2026. u 15:00 prognoza nije izdana; najbliže ranije izdanje je 5.10.2026. u 10:00, izračunato u 10:20.",
		">36<", "Zadnja prognoza izdana je za 5. 10. u 10 h — prije 5 h"} {
		if !strings.Contains(txt, want) {
			t.Errorf("u izvozu nema %q", want)
		}
	}
	if strings.Contains(txt, ">506<") {
		t.Error("izvezeno novije izdanje")
	}

	w = izvezi(h, "?dan=2026-10-01&sat=9")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "najstarije izdanje u bazi je 5.10.2026. u 10:00") {
		t.Errorf("prije prvog izdanja: %d %s", w.Code, w.Body.String())
	}
	for _, upit := range []string{"?dan=5.10.2026.&sat=9", "?dan=2026-10-05&sat=24", "?dan=2026-10-05"} {
		if w := izvezi(h, upit); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", upit, w.Code)
		}
	}
	prazan := NewPrognozeHandler(nil, nil, nil)
	if w := izvezi(prazan, "?dan=2026-10-05&sat=10"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "Nijedna prognoza još nije izdana.") {
		t.Errorf("bez baze: %d %s", w.Code, w.Body.String())
	}
}

// Poruka kaže kad je izvezeno izdanje izračunato, s danom kad to nije dan
// izdanja; bez zapisa o izdanju to ne zna.
func TestPorukaRanijegIzdanja(t *testing.T) {
	iz := RanijeIzdanje{Trazeno: satOdEpohe(satStarog), Izdano: satOdEpohe(satStarog),
		Nastalo: time.Date(2026, 10, 5, 8, 41, 0, 0, time.UTC)}
	if got, want := iz.Poruka(), "Prognoza izdana 5.10.2026. u 10:00, izračunata u 10:41."; got != want {
		t.Errorf("%q\nželim\n%q", got, want)
	}
	kasnije := RanijeIzdanje{Trazeno: satOdEpohe(satStarog + 5), Izdano: satOdEpohe(satStarog),
		Nastalo: time.Date(2026, 10, 6, 6, 12, 0, 0, time.UTC)}
	if got, want := kasnije.Poruka(), "Za 5.10.2026. u 15:00 prognoza nije izdana; najbliže ranije izdanje je "+
		"5.10.2026. u 10:00, izračunato 6.10.2026. u 08:12."; got != want {
		t.Errorf("%q\nželim\n%q", got, want)
	}
	if got, want := (RanijeIzdanje{Trazeno: iz.Trazeno, Izdano: iz.Izdano}).Poruka(), "Prognoza izdana 5.10.2026. u 10:00."; got != want {
		t.Errorf("bez zapisa: %q", got)
	}
	if !strings.Contains(iz.Napomena(satOdEpohe(satNovog)), "Ranije izdanje, izvezeno 9.10.2026. u 19:00. Prognoza izdana") {
		t.Errorf("napomena: %q", iz.Napomena(satOdEpohe(satNovog)))
	}
	var nema *CitacPrognoza
	if nema.Na(satStarog) != nil {
		t.Error("prikovan čitač bez baze")
	}
	if _, ima := nema.OpisIzdanja(satOdEpohe(satStarog)); ima {
		t.Error("opis izdanja bez baze")
	}
	if _, ranije := IzdanjeZahtjeva(context.Background()); ranije {
		t.Error("zahtjev bez ranijeg izdanja ga ima")
	}
	if sat, ranije := IzdanjeZahtjeva(UzRanijeIzdanje(context.Background(), iz)); !ranije || sat != satStarog {
		t.Errorf("sat ranijeg izdanja %d %v", sat, ranije)
	}
}

// Postavke nude izvoz ranije prognoze svakome tko može izvoziti: kažu od kad
// baza čuva izdanja, a za traženi sat koje se izdanje izvozi.
func TestPostavkeRanijaPrognoza(t *testing.T) {
	h := rukovateljRanijih(t)
	h.SetPostavke(predlozakPostavki(t))
	stranica := func(upit string) (int, string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/prognoze/postavke"+upit, nil)
		r = r.WithContext(context.WithValue(r.Context(), contextKeyUser, &models.User{FullName: "Ana Anić"}))
		h.ShowPostavke(w, r)
		return w.Code, w.Body.String()
	}
	kod, html := stranica("")
	if kod != http.StatusOK {
		t.Fatalf("postavke bez kruga preuzimanja: %d — izvoz ranije prognoze mora biti dostupan", kod)
	}
	for _, want := range []string{`id="ranije"`, "najstarije je od 5.10.2026. u 10:00", `min="2026-10-05"`, `name="sat"`, "23:00"} {
		if !strings.Contains(html, want) {
			t.Errorf("odjeljak nema %q", want)
		}
	}
	if strings.Contains(html, "prog-ranije-ishod") {
		t.Error("ishod stoji, a ništa nije traženo")
	}
	_, html = stranica("?dan=2026-10-05&sat=12")
	for _, want := range []string{"Za 5.10.2026. u 12:00 prognoza nije izdana; najbliže ranije izdanje je 5.10.2026. u 10:00, izračunato u 10:20.",
		`href="/prognoze.xlsx?dan=2026-10-05&amp;sat=12"`, `value="2026-10-05"`, `<option value="12" selected>`} {
		if !strings.Contains(html, want) {
			t.Errorf("ishod nema %q", want)
		}
	}
	_, html = stranica("?dan=2026-10-01&sat=9")
	if !strings.Contains(html, "ni ranije nema sačuvane prognoze") || strings.Contains(html, "/prognoze.xlsx?") {
		t.Error("prije prvog izdanja nudi se izvoz")
	}
	_, html = stranica("?dan=nije&sat=9")
	if !strings.Contains(html, "Ranije izdanje: dan nije ispravan datum.") {
		t.Error("krivi dan bez poruke")
	}

	// Gumb Postavke stoji na pregledu čim ima prognoze, i bez kruga preuzimanja.
	pregled := iscrtaj(t, "prognoze.html", PrognozePageData{CurrentUser: &models.User{FullName: "Ana Anić"}, ActiveNav: "prognoze",
		Izdano: "5.10.2026. u 10:00", Tablice: []TablicaPrognoza{{Naslov: "Pritoke", Letve: []LetvaPrognoze{{Kod: "gornja", Naziv: "Gornji Brod"}}}}})
	if !strings.Contains(pregled, `href="/prognoze/postavke"`) {
		t.Error("na pregledu s prognozom nema gumba Postavke")
	}

	prazan := NewPrognozeHandler(nil, nil, nil)
	prazan.SetPostavke(predlozakPostavki(t))
	w := httptest.NewRecorder()
	prazan.ShowPostavke(w, httptest.NewRequest(http.MethodGet, "/prognoze/postavke", nil))
	if w.Code != http.StatusSeeOther {
		t.Errorf("bez izdanja i bez kruga postavke ne vode na pregled: %d", w.Code)
	}
}

// predlozakPostavki učitava stranicu „Postavke prognoze” kao poslužitelj.
func predlozakPostavki(t *testing.T) *template.Template {
	t.Helper()
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := template.New("base.html").Funcs(templateFuncs()).
		ParseFS(templatesFS, DijeloviPredloska("prognoze_postavke.html")...)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

// Isto izdanje piše se jednako na izdavaču i na čvoru koji ga je primio
// nadoknadom (samo zadnju verziju sata): ni jedan ne broji koliko je puta
// sat izdan, jer primatelj to ne zna, nego kaže kad je izvezeno izdanje
// izračunao izdavač.
func TestPorukaRanijegIstaNaSvimCvorovima(t *testing.T) {
	sat := satStarog
	prvi, drugi := time.Date(2026, 10, 5, 8, 12, 0, 0, time.UTC), time.Date(2026, 10, 5, 8, 41, 0, 0, time.UTC)
	izdane := []prognoza.Izdana{{Letva: "belisce", Velicina: "vodostaj", Izdano: sat, Ciljni: sat + 1,
		Vrijednost: 40, Dolje: 38, Gore: 42, Model: "proba"}}
	otvori := func(t *testing.T) (string, *sql.DB) {
		put := filepath.Join(t.TempDir(), "prognoze.db")
		db, err := prognoza.Otvori(put)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return put, db
	}
	putIzdavaca, izdavac := otvori(t)
	for _, nastalo := range []time.Time{prvi, drugi} {
		if err := prognoza.SpremiIzdane(izdavac, izdane); err != nil {
			t.Fatal(err)
		}
		if _, err := prognoza.SpremiZapisIzdanja(izdavac, prognoza.ZapisIzdanja{Izdano: sat, Nastalo: nastalo, Cvor: "cvor-izdavac"}); err != nil {
			t.Fatal(err)
		}
	}
	putPrimatelja, primatelj := otvori(t)
	if _, err := prognoza.PrimiIzdanje(primatelj, "v9", prognoza.PaketIzdanja{Zapis: prognoza.ZapisIzdanja{Izdano: sat, Verzija: 2,
		Nastalo: drugi, Cvor: "cvor-izdavac"}}); err != nil {
		t.Fatal(err)
	}
	if err := prognoza.SpremiIzdane(primatelj, izdane); err != nil {
		t.Fatal(err)
	}
	poruka := func(put string) string {
		c, err := OtvoriPrognoze(put)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		iz, ok := nadjiRanije(c, satOdEpohe(sat))
		if !ok {
			t.Fatal("izdanja nema")
		}
		return iz.Poruka()
	}
	naIzdavacu, naPrimatelju := poruka(putIzdavaca), poruka(putPrimatelja)
	if naIzdavacu != naPrimatelju {
		t.Errorf("izdavač: %q\nprimatelj: %q", naIzdavacu, naPrimatelju)
	}
	if want := "Prognoza izdana 5.10.2026. u 10:00, izračunata u 10:41."; naIzdavacu != want {
		t.Errorf("%q\nželim\n%q", naIzdavacu, want)
	}
}

// Sat koji još nije došao nema izdanja ni ranijeg ni zadnjeg: izvoz ga ne
// daje kao „najbliže ranije” zadnje izdanje (koje bi tada izgledalo
// zastarjelo, sa svim vrijednostima upitnima), nego kaže da sat nije došao.
func TestRanijeIzdanjeSatKojiNijeDosao(t *testing.T) {
	h := rukovateljRanijih(t)
	h.SetPostavke(predlozakPostavki(t))
	buduci := time.Now().In(models.Zagreb).Add(6 * time.Hour)
	upit := "?dan=" + buduci.Format("2006-01-02") + "&sat=" + strconv.Itoa(buduci.Hour())
	w := izvezi(h, upit)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "još nije došao") {
		t.Errorf("izvoz za sat koji nije došao: %d %s", w.Code, w.Header().Get("Content-Disposition"))
	}
	r := httptest.NewRequest(http.MethodGet, "/prognoze/postavke"+upit, nil)
	r = r.WithContext(context.WithValue(r.Context(), contextKeyUser, &models.User{FullName: "Ana Anić"}))
	wp := httptest.NewRecorder()
	h.ShowPostavke(wp, r)
	html := wp.Body.String()
	if !strings.Contains(html, "još nije došao") || strings.Contains(html, "/prognoze.xlsx?") || strings.Contains(html, "nije izdana;") {
		t.Errorf("postavke za sat koji nije došao nude izvoz ili ne kažu zašto")
	}
	// Tekući sat je dopušten: izdanje mu je zadnje ili najbliže ranije.
	sada := time.Now().In(models.Zagreb)
	if w := izvezi(h, "?dan="+sada.Format("2006-01-02")+"&sat="+strconv.Itoa(sada.Hour())); w.Code != http.StatusOK {
		t.Errorf("tekući sat: %d %s", w.Code, w.Body.String())
	}
}
