package web

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Odluke (8. 10. 2026.), pitanje 15: u postavkama prognoze izvoz stare
// prognoze za odabrani dan i sat, isto kao za aktualnu.

// Staro izdanje je 23. 9. 2026. u 07 h, novo dan kasnije; letve i brojke su
// izmišljene. Staro je stiglo razmjenom s čvora „cvor-a”, novo je naše.
var (
	staroIzdanje = time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix() / 3600
	novoIzdanje  = staroIzdanje + 24
)

// bazaDvaIzdanja slaže bazu prognoza s dva izdanja izmišljene letve u lancu:
// satnu prognozu (staro 311 + sat, novo 577 + sat), dnevnu, tuđu i zapis o
// izdanju.
func bazaDvaIzdanja(t *testing.T) *CitacPrognoza {
	t.Helper()
	return citacBaze(t, putDvaIzdanja(t))
}

// citacBaze otvara čitač baze prognoza i zatvara ga kad test završi.
func citacBaze(t *testing.T, put string) *CitacPrognoza {
	t.Helper()
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// putDvaIzdanja slaže bazu iz bazaDvaIzdanja i vraća njezin put.
func putDvaIzdanja(t *testing.T) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "prognoze.db")
	db, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := prognoza.Spremi(db, []prognoza.Pojas{{Letva: "donja", Velicina: "vodostaj", Od: -200, Do: 900, Rasap: 8,
		Ulazi: []prognoza.Ulaz{{Letva: "gornja", Velicina: "vodostaj", PomakH: 7, Sirina: 1, Nagib: 1}}}}, "proba"); err != nil {
		t.Fatal(err)
	}
	for _, iz := range []struct {
		sat          int64
		osnova       float64
		cvor, knjiga string
	}{{staroIzdanje, 311, "cvor-a", "v1"}, {novoIzdanje, 577, "", ""}} {
		var izdane []prognoza.Izdana
		for sat := int64(0); sat <= 72; sat++ {
			v := iz.osnova + float64(sat)
			izdane = append(izdane, prognoza.Izdana{Letva: "donja", Velicina: "vodostaj", Izdano: iz.sat, Ciljni: iz.sat + sat,
				Vrijednost: v, Dolje: v - 1, Gore: v + 1, Model: "proba"})
		}
		if err := prognoza.SpremiIzdane(db, izdane); err != nil {
			t.Fatal(err)
		}
		if err := prognoza.SpremiDnevne(db, []prognoza.DnevnaIzdana{{Letva: "donja", Izdano: iz.sat, Dan: 1,
			Ciljni: iz.sat + 26, Vrijednost: iz.osnova + 50, Dolje: iz.osnova + 40, Gore: iz.osnova + 60, Model: "proba"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO tude (izvor, letva, izdano, ciljni, vrijednost, raspon) VALUES ('hydroinfo.hu', 'donja', ?, ?, ?, 0)`,
			iz.sat, iz.sat+26, iz.osnova+5); err != nil {
			t.Fatal(err)
		}
		if _, err := prognoza.SpremiZapisIzdanja(db, prognoza.ZapisIzdanja{Izdano: iz.sat, Nastalo: time.Unix(iz.sat*3600, 0),
			Cvor: iz.cvor, Knjiga: iz.knjiga}); err != nil {
			t.Fatal(err)
		}
	}
	return put
}

// Čitač stare prognoze vidi samo izdanja do svog sata: satno, dnevno, tuđe
// i izdavača; običan čitač i dalje zadnje.
func TestCitacDoSataVidiStaroIzdanje(t *testing.T) {
	c := bazaDvaIzdanja(t)
	za := func(c *CitacPrognoza) (int64, float64, int64, float64, string) {
		t.Helper()
		kad, letve, err := c.Pregled()
		if err != nil || len(letve) != 1 {
			t.Fatalf("pregled: %v, %d letvi", err, len(letve))
		}
		dnevno, dn, err := c.Dnevno()
		if err != nil || len(dn["donja"]) != 1 {
			t.Fatalf("dnevno: %v, %+v", err, dn)
		}
		tuda := c.Tude("hydroinfo.hu", kad)["donja"]
		if len(tuda) != 1 {
			t.Fatalf("tuđa: %+v", tuda)
		}
		var tv float64
		for _, v := range tuda {
			tv = v.Cm
		}
		return kad.Unix() / 3600, letve[0].Sada["vodostaj"], dnevno.Unix() / 3600, tv, c.Izdavac()
	}
	// Traženi sat 09 h, dva sata poslije starog izdanja: vrijedi staro.
	staro := c.Do(time.Unix(staroIzdanje*3600, 0).Add(2 * time.Hour))
	if kad, sada, dnevno, tuda, cvor := za(staro); kad != staroIzdanje || sada != 311 || dnevno != staroIzdanje || tuda != 316 || cvor != "cvor-a" {
		t.Errorf("staro: izdanje %d, sada %g, dnevno %d, tuđa %g, izdavač %q", kad, sada, dnevno, tuda, cvor)
	}
	if kad, sada, dnevno, tuda, cvor := za(c); kad != novoIzdanje || sada != 577 || dnevno != novoIzdanje || tuda != 582 || cvor != "" {
		t.Errorf("zadnje: izdanje %d, sada %g, dnevno %d, tuđa %g, izdavač %q", kad, sada, dnevno, tuda, cvor)
	}
	// Prije prvog izdanja nema ništa.
	prije := c.Do(time.Unix(staroIzdanje*3600, 0).Add(-time.Hour))
	if kad, letve, err := prije.Pregled(); err != nil || len(letve) != 0 || !kad.IsZero() {
		t.Errorf("prije prvog izdanja: %v %d %v", kad, len(letve), err)
	}
	var nema *CitacPrognoza
	if nema.Do(time.Now()) != nil {
		t.Error("čitač bez baze dao je čitač")
	}
}

// Dan i sat iz adrese su naše vrijeme; bez njih izvoz je zadnjeg izdanja, a
// pogrešni se odbijaju.
func TestZahtjevIzdanja(t *testing.T) {
	r, err := zahtjevIzdanja(httptest.NewRequest("GET", "/prognoze.xlsx", nil))
	if _, staro := trazenoIzdanje(r.Context()); err != nil || staro {
		t.Errorf("bez dana i sata: staro %v, %v", staro, err)
	}
	r, err = zahtjevIzdanja(httptest.NewRequest("GET", "/prognoze.xlsx?dan=2026-09-23&sat=7", nil))
	kad, staro := trazenoIzdanje(r.Context())
	if err != nil || !staro || !kad.Equal(time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("23. 9. u 7 h ljeti je 5 h UTC: %v %v %v", kad, staro, err)
	}
	r, _ = zahtjevIzdanja(httptest.NewRequest("GET", "/prognoze.xlsx?dan=2026-12-23&sat=0", nil))
	if kad, _ := trazenoIzdanje(r.Context()); !kad.Equal(time.Date(2026, 12, 22, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("23. 12. u 0 h zimi je 22. 12. u 23 h UTC: %v", kad)
	}
	for _, adresa := range []string{"?dan=2026-13-01&sat=7", "?dan=23.9.2026.&sat=7", "?dan=2026-09-23",
		"?dan=2026-09-23&sat=24", "?dan=2026-09-23&sat=-1", "?sat=7"} {
		if _, err := zahtjevIzdanja(httptest.NewRequest("GET", "/prognoze.xlsx"+adresa, nil)); err == nil {
			t.Errorf("%s: prihvaćeno", adresa)
		}
	}
}

// Rečenice o kiši uz staru prognozu nema: kiša se čita samo za sada.
func TestKisaZaStaruPrognozu(t *testing.T) {
	h := &PrognozeHandler{}
	if h.kisaZa(context.Background()) != nil {
		t.Error("bez izvora kiše dana je kiša")
	}
	h.kisa = func() KisaSlivovaFunc {
		return func(context.Context) ([]prognoza.StanjeSliva, map[string][]prognoza.DnevnaIzdana, error) {
			return nil, nil, nil
		}
	}
	if h.kisaZa(context.Background()) == nil {
		t.Error("zadnje izdanje: nema kiše")
	}
	staro := context.WithValue(context.Background(), kljucIzdanja{}, time.Now())
	if h.kisaZa(staro) != nil {
		t.Error("stara prognoza: dana je današnja kiša")
	}
}

// sadrzajKnjige raspakira .xlsx u jedan tekst.
func sadrzajKnjige(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var sve strings.Builder
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		x, _ := io.ReadAll(r)
		r.Close()
		sve.Write(x)
	}
	return sve.String()
}

// Izvoz s danom i satom daje istu knjigu iz izdanja koje je tada bilo
// zadnje: brojke, „izdano”, datum dokumenta i ime datoteke su starog
// izdanja. Bez njih izvoz je zadnjeg izdanja, kao i dosad.
func TestIzvozStarePrognoze(t *testing.T) {
	c := bazaDvaIzdanja(t)
	h := &PrognozeHandler{citac: func() *CitacPrognoza { return c }}
	izvezi := func(adresa string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.IzvoziPrognoze(rec, httptest.NewRequest("GET", adresa, nil))
		return rec
	}

	rec := izvezi("/prognoze.xlsx?dan=2026-09-23&sat=9")
	if rec.Code != 200 {
		t.Fatalf("staro izdanje: %d %s", rec.Code, rec.Body.String())
	}
	if ime := rec.Header().Get("Content-Disposition"); !strings.Contains(ime, `filename="prognoza_2026-09-23_07h.xlsx"`) {
		t.Errorf("ime datoteke: %s", ime)
	}
	staro := sadrzajKnjige(t, rec.Body.Bytes())
	for _, want := range []string{"izdano 23.9.2026. u 07:00", "izdanje 23.9.2026. u 07:00", ">311<", ">317<", "23.09.2026."} {
		if !strings.Contains(staro, want) {
			t.Errorf("izvoz starog izdanja nema %q", want)
		}
	}
	for _, nema := range []string{"24.9.2026. u 07:00", ">577<", ">583<"} {
		if strings.Contains(staro, nema) {
			t.Errorf("u izvozu starog izdanja je %q iz novog", nema)
		}
	}

	danas := func() string { return `filename="prognoza_` + time.Now().In(models.Zagreb).Format("2006-01-02") }
	prije := danas()
	rec = izvezi("/prognoze.xlsx")
	if rec.Code != 200 {
		t.Fatalf("zadnje izdanje: %d %s", rec.Code, rec.Body.String())
	}
	if ime := rec.Header().Get("Content-Disposition"); !strings.Contains(ime, prije) && !strings.Contains(ime, danas()) {
		t.Errorf("ime datoteke zadnjeg izdanja nije po današnjem danu: %s", ime)
	}
	if zadnje := sadrzajKnjige(t, rec.Body.Bytes()); !strings.Contains(zadnje, "izdano 24.9.2026. u 07:00") || !strings.Contains(zadnje, ">577<") {
		t.Error("izvoz bez dana i sata nije zadnjeg izdanja")
	}

	if rec = izvezi("/prognoze.xlsx?dan=2026-09-23&sat=25"); rec.Code != 400 || !strings.Contains(rec.Body.String(), "od 0 do 23") {
		t.Errorf("pogrešan sat: %d %s", rec.Code, rec.Body.String())
	}
	if rec = izvezi("/prognoze.xlsx?dan=2026-09-01&sat=7"); rec.Code != 404 || !strings.Contains(rec.Body.String(), "Nijedna prognoza") {
		t.Errorf("prije prvog izdanja: %d %s", rec.Code, rec.Body.String())
	}
	h.citac = nil // čvor bez baze prognoza
	if rec = izvezi("/prognoze.xlsx?dan=2026-09-23&sat=9"); rec.Code != 404 || !strings.Contains(rec.Body.String(), "nije otvorena") {
		t.Errorf("bez baze prognoza: %d %s", rec.Code, rec.Body.String())
	}
}

// Postavke prognoze nude izvoz stare prognoze: dan (danas unaprijed) i sat.
func TestPostavkeNudeIzvozStarePrognoze(t *testing.T) {
	html := iscrtaj(t, "prognoze_postavke.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "P"}, Permissions: &models.UserPermissions{},
		ActiveNav: "prognoze", MozeGenerirati: true, Danas: "2026-10-10",
	})
	for _, want := range []string{`action="/prognoze.xlsx"`, `name="dan"`, `value="2026-10-10"`, `name="sat"`,
		`<option value="7" selected>07 h</option>`, `<option value="23">23 h</option>`, "Izvoz stare prognoze"} {
		if !strings.Contains(html, want) {
			t.Errorf("postavke nemaju %q", want)
		}
	}
}

// Sat izdan iznova (Generiraj, Pripremi model): izvoz stare prognoze daje
// zadnji račun tog sata, a uz „izdano” piše da je sat izdan iznova i kada.
// Dosad je izvoz davao brojke zadnjeg računa bez ikakve oznake, a stranica i
// pomoć obećavale su brojke „onakve kakve su tada izdane”.
func TestIzvozStarePrognozeIzdaneIznova(t *testing.T) {
	put := putDvaIzdanja(t)
	db, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	if err := prognoza.ObrisiIzdanje(db, staroIzdanje); err != nil {
		t.Fatal(err)
	}
	var izdane []prognoza.Izdana
	for sat := int64(0); sat <= 72; sat++ {
		v := 411 + float64(sat)
		izdane = append(izdane, prognoza.Izdana{Letva: "donja", Velicina: "vodostaj", Izdano: staroIzdanje,
			Ciljni: staroIzdanje + sat, Vrijednost: v, Dolje: v - 1, Gore: v + 1, Model: "proba"})
	}
	if err := prognoza.SpremiIzdane(db, izdane); err != nil {
		t.Fatal(err)
	}
	// Ponovni račun 23. 9. u 09:30 po našem vremenu.
	if _, err := prognoza.SpremiZapisIzdanja(db, prognoza.ZapisIzdanja{Izdano: staroIzdanje,
		Nastalo: time.Unix(staroIzdanje*3600, 0).Add(150 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	c := citacBaze(t, put)
	h := &PrognozeHandler{citac: func() *CitacPrognoza { return c }}

	rec := httptest.NewRecorder()
	h.IzvoziPrognoze(rec, httptest.NewRequest("GET", "/prognoze.xlsx?dan=2026-09-23&sat=9", nil))
	if rec.Code != 200 {
		t.Fatalf("staro izdanje: %d %s", rec.Code, rec.Body.String())
	}
	staro := sadrzajKnjige(t, rec.Body.Bytes())
	for _, want := range []string{">411<", "izdano 23.9.2026. u 07:00 (sat izdan iznova 23.9. u 09:30; brojke su zadnjeg računa)"} {
		if !strings.Contains(staro, want) {
			t.Errorf("izvoz sata izdanog iznova nema %q", want)
		}
	}

	// Sat izdan jednom i izvoz zadnjeg izdanja nemaju oznake.
	rec = httptest.NewRecorder()
	h.IzvoziPrognoze(rec, httptest.NewRequest("GET", "/prognoze.xlsx", nil))
	if zadnje := sadrzajKnjige(t, rec.Body.Bytes()); strings.Contains(zadnje, "izdan iznova") {
		t.Error("zadnje izdanje, izdano jednom, označeno je kao izdano iznova")
	}
	if kad, iznova := c.IzdanIznova(novoIzdanje); iznova || !kad.IsZero() {
		t.Errorf("novo izdanje, izdano jednom: %v %v", kad, iznova)
	}
	var nema *CitacPrognoza
	if _, iznova := nema.IzdanIznova(staroIzdanje); iznova {
		t.Error("čitač bez baze javlja sat izdan iznova")
	}

	// Izdanje iz vremena prije zapisa o izdanjima: zna se da je izdano
	// iznova, ali ne i kada.
	db, err = prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM izdanja WHERE izdano = ?`, staroIzdanje); err != nil {
		t.Fatal(err)
	}
	db.Close()
	trazeno, _ := zahtjevIzdanja(httptest.NewRequest("GET", "/prognoze.xlsx?dan=2026-09-23&sat=9", nil))
	if n := h.napomenaIznova(trazeno.Context(), staroIzdanje); n != " (sat izdan iznova; brojke su zadnjeg računa)" {
		t.Errorf("bez zapisa o izdanju: %q", n)
	}
	c.Close()
	if n := h.napomenaIznova(trazeno.Context(), staroIzdanje); n != "" {
		t.Errorf("baza se ne da čitati, a napomena je %q", n)
	}
}
