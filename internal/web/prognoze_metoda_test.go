package web

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/xlsxw"
)

// Opis mora nositi brojke iz računa, a ne prepisane: promijeni li se
// poluvrijeme ispravka ili broj analogija, opis se mijenja s njima.
func TestOpisMetodeCitaBrojkeIzRacuna(t *testing.T) {
	var sve strings.Builder
	for _, o := range OpisMetode(68, "COP Osijek") {
		sve.WriteString(o.Naslov + "\n")
		for _, od := range o.Odlomci {
			sve.WriteString(od.Tekst + "\n")
		}
	}
	tekst := sve.String()
	for _, want := range []string{
		"Prognoza COP Osijek",
		"2^(−τ / " + brojHRf(prognoza.PoluvijekIspravka, 0) + ")",
		"K = " + tekstBroja(prognoza.DnevnihAnalogija),
		"[0, " + tekstBroja(prognoza.NajveciPomak) + "] h",
		"68. percentil", "u 32 % stvarna vrijednost", "70 %",
		"linearni spline", "najbližih susjeda", "Q–H",
	} {
		if !strings.Contains(tekst, want) {
			t.Errorf("opis metode nema %q", want)
		}
	}
}

// Tablica postaja: ulazi lanca s kašnjenjem po pojasima i prozorom, raspon iz
// izmjerenih promašaja, ulazi dnevnog modela imenima postaja.
func TestLetveMetodeOpisujuUlaze(t *testing.T) {
	postaje := map[string]models.Station{
		"gornja": {Name: "Gornja"}, "pritok": {Name: "Pritok"},
	}
	pojasi := map[string][]prognoza.Pojas{"donja": {
		{Letva: "donja", Velicina: "vodostaj", R: 0.981, Ulazi: []prognoza.Ulaz{
			{Letva: "gornja", Velicina: "protok", PomakH: 12, Sirina: 5},
			{Letva: "pritok", Velicina: "vodostaj", PomakH: 6, Sirina: 1}}},
		{Letva: "donja", Velicina: "vodostaj", R: 0.993, Ulazi: []prognoza.Ulaz{
			{Letva: "gornja", Velicina: "protok", PomakH: 20, Sirina: 5},
			{Letva: "pritok", Velicina: "vodostaj", PomakH: 6, Sirina: 1}}},
	}}
	promasaji := map[string]map[int]prognoza.Promasaj{"donja": {
		24: {Rasap: 6.4}, 48: {Rasap: 11.2}}}
	tablice := []TablicaPrognoza{{Naslov: "Rijeka", Letve: []LetvaPrognoze{
		{Kod: "gornja", Naziv: "Gornja"}, {Kod: "donja", Naziv: "Donja", Voda: "Rijeka"}}}}

	l := letveMetode(tablice, postaje, pojasi, promasaji)
	if len(l) != 1 {
		t.Fatalf("u tablici %d postaja, a računa se samo jedna", len(l))
	}
	d := l[0]
	if len(d.Satni) != 2 || d.Satni[0] != "Gornja · protok · 12–20 h · prozor 5 h" || d.Satni[1] != "Pritok · vodostaj · 6 h" {
		t.Errorf("ulazi lanca: %q", d.Satni)
	}
	if d.Raspon != "±6 / ±11 / — cm" {
		t.Errorf("raspon: %q", d.Raspon)
	}
	if d.Slaganje != "0,981–0,993" {
		t.Errorf("R: %q", d.Slaganje)
	}
}

func TestStranicaOPrognozi(t *testing.T) {
	html := iscrtaj(t, "prognoze_metoda.html", PrognozeMetodaData{
		CurrentUser: &models.User{FullName: "P"},
		Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav:   "prognoze", Izdaje: "COP Osijek", Izdano: "24.9.2026. u 07:00",
		Odjeljci: OpisMetode(68, "COP Osijek"), RasponDo: DoseziRaspona,
		Suradnja: suradnja, SuradnjaVeza: suradnjaVeza,
		Izvozi: []IzvozDatoteka{{Naziv: "izdanja.csv", URL: "/prognoze/izdanja.csv"}},
		Letve: []LetvaMetode{{Naziv: "Donja", Voda: "Rijeka", Racuna: "vodostaj",
			Satni: []string{"Gornja · protok · 12–20 h · prozor 5 h"}, Raspon: "±6 / ±11 / ±17 cm",
			Dnevni: []string{"Gornja", "Pritok"}, DnevniOd: "3. dana"}},
	})
	for _, want := range []string{"O prognozi", "Satni lanac — građa", "prog-formula", "Nenada Šuvaka", suradnjaVeza, "izdanja.csv",
		"Gornja · protok · 12–20 h · prozor 5 h", "±6 / ±11 / ±17 cm", "Gornja, Pritok", "3. dana",
		"/prognoze.xlsx"} {
		if !strings.Contains(html, want) {
			t.Errorf("stranica O prognozi nema %q", want)
		}
	}
}

// List „O prognozi” u izvozu: opis i tablica postaja, a knjiga se da zapisati.
func TestListMetodeUIzvozu(t *testing.T) {
	k := &xlsxw.Knjiga{}
	listMetode(k, ZaglavljeIzvoza{Organizacija: "Org", Centar: "COP Osijek"}, PrognozeMetodaData{
		Odjeljci: OpisMetode(68, "COP Osijek"), RasponDo: DoseziRaspona,
		Letve: []LetvaMetode{{Naziv: "Donja", Satni: []string{"a", "b"}, Dnevni: []string{"c"}}},
	})
	if len(k.Listovi) != 1 || k.Listovi[0].Naziv != "O prognozi" {
		t.Fatalf("listovi: %+v", k.Listovi)
	}
	var sve []string
	for _, red := range k.Listovi[0].Redci {
		for _, c := range red {
			sve = append(sve, c.Tekst)
		}
	}
	tekst := strings.Join(sve, "\n")
	for _, want := range []string{"O PROGNOZI", "Dnevni model — procjena", "Satni lanac — ulazi", "a\nb"} {
		if !strings.Contains(tekst, want) {
			t.Errorf("list nema %q", want)
		}
	}
	var buf bytes.Buffer
	if err := k.Zapisi(&buf); err != nil {
		t.Fatal(err)
	}
}

// Izvoz izdanja: CSV s točka-zarezom, svaka izdana vrijednost s dosegom;
// izmjereno prazno dok nema očitanja.
func TestIzvozIzdanjaCSV(t *testing.T) {
	put := filepath.Join(t.TempDir(), "p.db")
	db, err := prognoza.Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	if err := prognoza.SpremiIzdane(db, []prognoza.Izdana{
		{Letva: "batina", Velicina: "vodostaj", Izdano: 496832, Ciljni: 496832, Vrijednost: -100, Dolje: -100, Gore: -100, Model: "lanac-1"},
		{Letva: "batina", Velicina: "vodostaj", Izdano: 496832, Ciljni: 496856, Vrijednost: -95.26, Dolje: -101, Gore: -89, Model: "lanac-1"},
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	c, err := OtvoriPrognoze(put)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	h := &PrognozeHandler{citac: func() *CitacPrognoza { return c }}
	rec := httptest.NewRecorder()
	h.IzvoziIzdanja(rec, httptest.NewRequest("GET", "/prognoze/izdanja.csv?od=2026-09-01&do=2026-09-30", nil))
	tijelo := rec.Body.String()
	if rec.Code != 200 || !strings.HasPrefix(tijelo, "\ufeffizdano_utc;letva;velicina;ciljni_utc;doseg_h;") {
		t.Fatalf("zaglavlje: %d %q", rec.Code, tijelo[:min(80, len(tijelo))])
	}
	if !strings.Contains(tijelo, "2026-09-05 08:00;batina;vodostaj;2026-09-06 08:00;24;-95.3;-101;-89;lanac-1;ne;") {
		t.Errorf("nema retka za 24 h:\n%s", tijelo)
	}
	if strings.Count(tijelo, "\n") != 3 {
		t.Errorf("očekivana 2 retka podataka, tijelo:\n%s", tijelo)
	}
}

// Datoteke provjere unatrag poslužuju se samo po imenu koje alat zapisuje.
func TestPodaciSamoHindcast(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hindcast_2023-2025.csv"), []byte("a;b\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "gocop.db"), []byte("x"), 0o644)
	h := &PrognozeHandler{}
	h.SetPodaciDir(func() string { return dir })
	iz := h.izvozi()
	if len(iz) != 2 || iz[1].Naziv != "hindcast_2023-2025.csv" {
		t.Fatalf("izvozi: %+v", iz)
	}
	for ime, kod := range map[string]int{"hindcast_2023-2025.csv": 200, "gocop.db": 404, "../gocop.db": 404} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/prognoze/podaci/x", nil)
		req.SetPathValue("ime", ime)
		h.PosluziPodatke(rec, req)
		if rec.Code != kod {
			t.Errorf("%s: %d, očekivano %d", ime, rec.Code, kod)
		}
	}
}

// Sažetak provjere na valovima čita se u tablicu: po rijeci i modelu, po
// dosegu, s pogreškom, pristranošću i postojanošću; najveći valovi po sidru.
func TestProvjeraValovaIzSazetka(t *testing.T) {
	dir := t.TempDir()
	csv := "\ufeffvrsta;val;letva;doseg_h;izmjereno;prognoza;vrh_prognoze;postojanost;model\n" +
		"vrh;Dunav-2013-06-13;batina;24;772;770;774;760;A\n" +
		"vrh;Dunav-2013-06-13;batina;48;772;760;766;720;A\n" +
		"vrh;Dunav-2006-04-09;batina;24;754;750;752;740;A\n" +
		"vrh;Drava-2014-09-16;belisce;24;591;585;592;570;B\n" +
		"vrh;Drava-2014-09-16;belisce;48;591;570;577;540;B\n" +
		"kroz;Drava-2014-09-16;belisce;24;500;490;;480;B\n"
	os.WriteFile(filepath.Join(dir, "hindcast_valovi_sazetak.csv"), []byte(csv), 0o644)
	p := provjeraValovaIz(dir)
	if p == nil || p.Valova != 3 || len(p.Skupine) != 2 {
		t.Fatalf("provjera: %+v", p)
	}
	d := p.Skupine[0]
	if d.Rijeka != "Dunav" || d.Valova != 2 || len(d.Dosezi) != 2 {
		t.Fatalf("Dunav: %+v", d)
	}
	// 24 h: pogreške +2 i −2 → MAE 2, pristranost 0, postojanost (12+14)/2 = 13.
	if x := d.Dosezi[0]; x.Doseg != 24 || x.N != 2 || x.MAE != 2 || x.Pristranost != 0 || x.MAEPostojanost != 13 {
		t.Errorf("Dunav 24 h: %+v", x)
	}
	if len(p.Najveci) < 2 || p.Najveci[0].Val != "Dunav-2013-06-13" || p.Najveci[0].Pogreske[1] != "-6" || p.Najveci[0].Pogreske[2] != "—" {
		t.Errorf("najveći: %+v", p.Najveci)
	}
	if provjeraValovaIz(t.TempDir()) != nil {
		t.Error("bez datoteke mora biti nil")
	}
}
