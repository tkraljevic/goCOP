package models

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Iz upita ide samo izbor, a nikad tekst koji je osoba upisala ni ono čime se
// ulazi u program
func TestOcistiStranicuOstavljaSamoIzbore(t *testing.T) {
	for ulaz, zeli := range map[string]string{
		"/prognoze?sektor=B&vodotok=Drava":                                     "/prognoze?sektor=B&vodotok=Drava",
		"/readings?token=tajno&sektor=B":                                       "/readings?sektor=B",
		"/x?Access_Token=a&reset_token=b&csrf_id=c":                            "/x",
		"/x?lozinka=a&pin=1&kod=2&KEY=3&session_id=4":                          "/x",
		"/x?api_key=A&kljuc_mreze=B&pwd=C&pass=D&access_key=E&otp=F&jwt=G":     "/x",
		"/posta/novo?za=primatelj%40voda.hr&predmet=Povjerljivo&tekst=Sadrzaj": "/posta/novo",
		"/posta?mapa=inbox&trazi=disciplinski":                                 "/posta?mapa=inbox",
		"/dogadjanja?osoba=Ana+Anic":                                           "/dogadjanja",
		"/users?q=Anic":                                                        "/users",
		"/sections?area=16&page=2":                                             "/sections?area=16&page=2",
		"/x?kodovi=2&sektor=B":                                                 "/x?sektor=B",
		"/profile?success=Lozinka+je+promijenjena":                             "/profile",
		"/profile#lozinka":                                                     "/profile",
		"/x?a=%zz":                                                             "/x",
		"https://tudja.stranica.hr/x":                                          "",
		"//tudja.stranica.hr/x":                                                "",
		`/\tudja.stranica.hr`:                                                  "",
		"javascript:alert(1)":                                                  "",
		"prognoze":                                                             "",
		"":                                                                     "",
		"/stations/ab cd":                                                      "/stations/ab%20cd",
		"/x?q=" + strings.Repeat("a", najviseStranice):                         "/x",
		// dugačka vrijednost nije izbor, ni pod dopuštenim imenom
		"/x?sektor=" + strings.Repeat("a", najviseVrijednostiUpita+1) + "&dan=1": "/x?dan=1",
	} {
		if got := OcistiStranicu(ulaz); got != zeli {
			t.Errorf("OcistiStranicu(%q) = %q, a treba %q", ulaz, got, zeli)
		}
	}
}

// Zapis s drugog računala ne prolazi čišćenje na ulazu, pa poveznica sama
// odbija sve što ne vodi na stranicu ovog programa
func TestPoveznicaSamoNaOvajProgram(t *testing.T) {
	for stranica, zeli := range map[string]string{
		"/prognoze?sektor=B":  "/prognoze?sektor=B",
		"//tudja.stranica.hr": "",
		`/\tudja.stranica.hr`: "",
		"https://tudja.hr":    "",
		"javascript:alert(1)": "",
		"/x\nSet-Cookie: a=b": "",
		"":                    "",
	} {
		p := PrijavaGreske{Stranica: stranica}
		if got := p.Poveznica(); got != zeli {
			t.Errorf("Poveznica(%q) = %q, a treba %q", stranica, got, zeli)
		}
	}
}

func TestOcistiGreske(t *testing.T) {
	cisto := OcistiGreske([]string{
		"17:41:08 Greška za /api/x?token=tajno&a=1",
		"17:41:09 token=tajno pin=1234 lozinka=abc",
		"  ",
		"redak\ns novim\tredom",
	})
	if len(cisto) != 3 {
		t.Fatalf("%d grešaka: %q", len(cisto), cisto)
	}
	if cisto[0] != "17:41:08 Greška za /api/x" {
		t.Errorf("upit u adresi: %q", cisto[0])
	}
	if strings.Contains(cisto[1], "tajno") || strings.Contains(cisto[1], "1234") || strings.Contains(cisto[1], "abc") {
		t.Errorf("osjetljivo u poruci: %q", cisto[1])
	}
	if cisto[2] != "redak s novim redom" {
		t.Errorf("jedan redak: %q", cisto[2])
	}
	puno := make([]string, 20)
	for i := range puno {
		puno[i] = strings.Repeat("š", najviseJedneGreske)
	}
	cisto = OcistiGreske(puno)
	if len(cisto) != najviseGresakaStranice {
		t.Errorf("%d grešaka", len(cisto))
	}
	if len(cisto[0]) > najviseJedneGreske || !utf8.ValidString(cisto[0]) {
		t.Errorf("predugačko ili rezano usred znaka: %d", len(cisto[0]))
	}
}

// Što god stigne u poruci greške, vrijednosti uz osjetljiva imena ne prolaze
func TestOcistiGreskeMiceOsjetljivo(t *testing.T) {
	for _, ulaz := range []string{"access_token=abc1", "secret=abc2 tajna=abc2", "Authorization: Bearer abc2", "lozinka: abc2", "/x#session_id=abc2", "api-key=abc2", "x-auth-token: abc2"} {
		cisto := OcistiGreske([]string{ulaz})
		if len(cisto) != 1 || strings.Contains(cisto[0], "abc") {
			t.Errorf("%q ostavlja osjetljivo: %q", ulaz, cisto)
		}
	}
	obicna := "17:41:08 Greška skripte (/static/js/app.js:120)"
	if cisto := OcistiGreske([]string{obicna}); len(cisto) != 1 || cisto[0] != obicna {
		t.Errorf("obična poruka promijenjena: %q", cisto)
	}
}

func TestPrijavaGreskeVrijeme(t *testing.T) {
	// 16:42 UTC je u studenom 17:42 u Zagrebu
	kad := time.Date(2026, 11, 6, 16, 42, 0, 0, time.UTC)
	if got := (&PrijavaGreske{CreatedAt: kad}).Kad(); got != "6. 11. 2026. u 17:42" {
		t.Errorf("%q", got)
	}
	if got := (&OdgovorNaPrijavu{CreatedAt: kad}).Kad(); got != "6. 11. 2026." {
		t.Errorf("%q", got)
	}
}

func TestOgraniciNaGraniciZnaka(t *testing.T) {
	if got := ogranici("kratko", 100); got != "kratko" {
		t.Errorf("%q", got)
	}
	if got := ogranici("ššš", 3); got != "š" {
		t.Errorf("rezano usred znaka: %q", got)
	}
}

func TestOcistiTekst(t *testing.T) {
	if got := OcistiTekst("  prvi\r\ndrugi\x00\x07\t \n"); got != "prvi\ndrugi" {
		t.Errorf("%q", got)
	}
	if got := OcistiNazivStranice("Prognoze\n ·\tDrava"); got != "Prognoze · Drava" {
		t.Errorf("%q", got)
	}
	if got := OcistiZaslon(strings.Repeat("9", 100)); len(got) != najviseZaslona {
		t.Errorf("%d", len(got))
	}
}

func TestPrijavaGreskeNatpisi(t *testing.T) {
	p := PrijavaGreske{Vrsta: VrstaPrijaveGreska, Stranica: "/karta"}
	if p.Naslov() != "Greška na stranici /karta" || p.VrstaNaziv() != "Greška" {
		t.Errorf("%q %q", p.Naslov(), p.VrstaNaziv())
	}
	p = PrijavaGreske{Vrsta: VrstaPrijavePrijedlog, NazivStranice: "Karta", Sustav: "Windows", Cvor: "laptop"}
	if p.Naslov() != "Prijedlog za stranicu Karta" || p.VrstaNaziv() != "Prijedlog" || p.Racunalo() != "Windows · laptop" {
		t.Errorf("%q %q %q", p.Naslov(), p.VrstaNaziv(), p.Racunalo())
	}
	if (&PrijavaGreske{Cvor: "laptop"}).Racunalo() != "laptop" || (&PrijavaGreske{Sustav: "iOS"}).Racunalo() != "iOS" {
		t.Error("računalo bez jednog dijela")
	}
	for stanje, zeli := range map[string][2]string{
		StanjePrijaveNova:     {"Nova", "pg-stanje-nova"},
		StanjePrijaveURadu:    {"U radu", "pg-stanje-rad"},
		StanjePrijaveRijeseno: {"Riješeno", "pg-stanje-rijeseno"},
		"novo_s_novijeg":      {"Nova", "pg-stanje-nova"},
	} {
		x := PrijavaGreske{Stanje: stanje}
		if x.StanjeNaziv() != zeli[0] || x.StanjeRazred() != zeli[1] {
			t.Errorf("%s: %q %q", stanje, x.StanjeNaziv(), x.StanjeRazred())
		}
	}
	if JeStanjePrijave("novo_s_novijeg") || !JeStanjePrijave(StanjePrijaveURadu) {
		t.Error("JeStanjePrijave")
	}
	if StanjeZaPrikaz("novo_s_novijeg") != StanjePrijaveNova || StanjeZaPrikaz(StanjePrijaveURadu) != StanjePrijaveURadu {
		t.Error("StanjeZaPrikaz")
	}
}

// Opis slike s drugog računala ostaje samo kad bi ga program i sam napisao
func TestOcistiOpisSlike(t *testing.T) {
	dobar := strings.Repeat("a1", 32)
	for _, s := range []struct {
		ime     string
		otisak  string
		vrsta   string
		bajtova int
		ostaje  bool
	}{
		{"dobar", dobar, SlikaPrijavePNG, 10, true},
		{"najveći", dobar, SlikaPrijaveWebP, NajvecaSlikaPrijave, true},
		{"prevelik", dobar, SlikaPrijaveJPEG, NajvecaSlikaPrijave + 1, false},
		{"bez veličine", dobar, SlikaPrijavePNG, 0, false},
		{"SVG", dobar, "image/svg+xml", 10, false},
		{"PDF", dobar, "application/pdf", 10, false},
		{"kratak otisak", "abc", SlikaPrijavePNG, 10, false},
		{"velika slova", strings.ToUpper(dobar), SlikaPrijavePNG, 10, false},
		{"put u otisku", strings.Repeat("../", 21) + "a", SlikaPrijavePNG, 10, false},
	} {
		p := PrijavaGreske{SlikaOtisak: s.otisak, SlikaVrsta: s.vrsta, SlikaBajtova: s.bajtova}
		p.OcistiOpisSlike()
		if ostalo := p.SlikaOtisak != ""; ostalo != s.ostaje {
			t.Errorf("%s: ostalo %v", s.ime, ostalo)
		}
		if !s.ostaje && (p.SlikaVrsta != "" || p.SlikaBajtova != 0) {
			t.Errorf("%s: nešto je ostalo: %+v", s.ime, p)
		}
	}
}

func TestPorukaKad(t *testing.T) {
	p := PrijavaGreske{}
	if p.PorukaKad() != "" {
		t.Error("bez poruke")
	}
	kad := time.Date(2026, 11, 6, 16, 42, 0, 0, time.UTC)
	p.PorukaPoslana = &kad
	if p.PorukaKad() != "6. 11. 2026. u 17:42" {
		t.Errorf("%q", p.PorukaKad())
	}
}

// Rok slike vrijedi samo za riješenu prijavu, od trenutka kad je riješena
func TestSlikaUklonjenaU(t *testing.T) {
	rijesena := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	sad := rijesena.Add(RokSlikePrijave)
	for _, s := range []struct {
		ime     string
		p       PrijavaGreske
		kad     time.Time
		obrisna bool
	}{
		{"prije roka", PrijavaGreske{SlikaOtisak: "a", Stanje: StanjePrijaveRijeseno, UpdatedAt: rijesena}, sad.Add(-time.Second), false},
		{"na roku", PrijavaGreske{SlikaOtisak: "a", Stanje: StanjePrijaveRijeseno, UpdatedAt: rijesena}, sad, true},
		{"nova", PrijavaGreske{SlikaOtisak: "a", Stanje: StanjePrijaveNova, UpdatedAt: rijesena}, sad.AddDate(1, 0, 0), false},
		{"u radu", PrijavaGreske{SlikaOtisak: "a", Stanje: StanjePrijaveURadu, UpdatedAt: rijesena}, sad.AddDate(1, 0, 0), false},
		{"nepoznato stanje", PrijavaGreske{SlikaOtisak: "a", Stanje: "odbijeno", UpdatedAt: rijesena}, sad.AddDate(1, 0, 0), false},
		{"vraćena nakon roka", PrijavaGreske{SlikaOtisak: "a", Stanje: StanjePrijaveURadu, UpdatedAt: sad, SlikaIstekla: true}, sad, true},
		{"bez slike", PrijavaGreske{Stanje: StanjePrijaveRijeseno, UpdatedAt: rijesena}, sad, false},
	} {
		if got := s.p.SlikaUklonjenaU(s.kad); got != s.obrisna {
			t.Errorf("%s: %v", s.ime, got)
		}
	}
	nedavno := PrijavaGreske{SlikaOtisak: "a", Stanje: StanjePrijaveRijeseno, UpdatedAt: time.Now()}
	stara := PrijavaGreske{SlikaOtisak: "a", Stanje: StanjePrijaveRijeseno, UpdatedAt: time.Now().Add(-RokSlikePrijave - time.Minute)}
	if nedavno.SlikaUklonjena() || !stara.SlikaUklonjena() {
		t.Error("za stranice")
	}
	if NajvecaSlikaPrijave != 2<<20 {
		t.Errorf("najveća slika: %d", NajvecaSlikaPrijave)
	}
}
