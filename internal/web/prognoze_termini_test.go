package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
)

// Čipovi profila su sada, +12 h i isti dani kao stupci kartica letvi, s
// dosegom od sata izdanja do 7 h tog dana.
func TestTerminiProfila(t *testing.T) {
	izdano := time.Date(2026, 10, 31, 8, 0, 0, 0, models.Zagreb)
	dani, ciljevi := daniPregleda(izdano)
	termini := terminiProfila(dani, ciljevi, izdano.Unix()/3600)
	if len(termini) != 8 {
		t.Fatalf("%d termina, želim sada, +12 h i šest dana", len(termini))
	}
	var naslovi, oznake []string
	for _, tm := range termini {
		naslovi, oznake = append(naslovi, tm.Naslov), append(oznake, tm.Oznaka)
	}
	if got := strings.Join(naslovi, " | "); got != "sada | +12 h | ned 1. 11. | pon 2. 11. | uto 3. 11. | sri 4. 11. | čet 5. 11. | pet 6. 11." {
		t.Errorf("naslovi čipova: %s", got)
	}
	if got := strings.Join(oznake, " "); got != "sada 12h 1 2 3 4 5 6" {
		t.Errorf("oznake u adresi: %s", got)
	}
	// 31. 10. u 8 h → 1. 11. u 7 h je 23 sata kasnije, a svaki idući dan 24 više.
	for k, tm := range termini[2:] {
		if tm.Doseg != 23+24*k {
			t.Errorf("%s: doseg %d h, želim %d", tm.Naslov, tm.Doseg, 23+24*k)
		}
	}
	if termini[0].Doseg != 0 || termini[1].Doseg != 12 || termini[3].Opis != "odabrani dan, pon 2. 11. u 7 h" {
		t.Errorf("sada %d, +12 h %d, opis %q", termini[0].Doseg, termini[1].Doseg, termini[3].Opis)
	}
}

// Termin iz adrese: poznat se bira, a prazan, nepoznat ili onaj iz kojeg
// nema dana je „sada”.
func TestOdabraniTermin(t *testing.T) {
	izdano := time.Date(2026, 10, 31, 8, 0, 0, 0, models.Zagreb)
	dani, ciljevi := daniPregleda(izdano)
	termini := terminiProfila(dani, ciljevi, izdano.Unix()/3600)
	for upit, zeli := range map[string]string{"": "sada", "sada": "sada", "12h": "+12 h", "2": "pon 2. 11.",
		"6": "pet 6. 11.", "7": "sada", "0": "sada", "pon": "sada", "<script>": "sada"} {
		if got := odabraniTermin(termini, upit); got.Naslov != zeli {
			t.Errorf("?dan=%s → %q, želim %q", upit, got.Naslov, zeli)
		}
	}
	if got := odabraniTermin(nil, "2"); got.Oznaka != terminSada || got.Doseg != 0 {
		t.Errorf("bez izdanja: %+v", got)
	}
}

// Čip dana crta vrijednost istog sata kao stupac kartice: profil ima crtu za
// svaki termin, a u njoj je broj iz niza prikaza (tablice) u 7 h tog dana;
// „sada” je zadnje mjerenje.
func TestCipDanaCrtaSvojDoseg(t *testing.T) {
	p := novaProbaPrikaza(t)
	_, prikaz := p.tablica(models.PrognozaKombinacija)
	nula := func(x float64) *float64 { return &x }
	postaje := map[string]models.Station{
		"botovo":         {Code: "botovo", Name: "Botovo", Watercourse: "Drava", Stationing: "rkm 200,0", ZeroDatum: nula(120)},
		"terezino-polje": {Code: "terezino-polje", Name: "Terezino Polje", Watercourse: "Drava", Stationing: "rkm 100,0", ZeroDatum: nula(110)},
	}
	jutro := Jutro{Cm: map[string]float64{"botovo": 100, "terezino-polje": -300}}
	mjesec := NulaProfila{Cm: map[string]float64{"botovo": 90, "terezino-polje": -290}}
	profili := uzduzniProfili(postaje, p.pregled, prikaz, p.izdano, nil, nil, nil, mjesec, jutro)
	if len(profili) != 1 {
		t.Fatalf("%d profila", len(profili))
	}
	var n nizProfila
	if err := json.Unmarshal([]byte(profili[0].Niz), &n); err != nil {
		t.Fatal(err)
	}
	dani, ciljevi := daniPregleda(p.izdano)
	for _, tm := range terminiProfila(dani, ciljevi, p.izdanoH) {
		i := -1
		for k, d := range n.Sati {
			if d == tm.Doseg {
				i = k
			}
		}
		if i < 0 {
			t.Errorf("%s (%d h): nema crte", tm.Naslov, tm.Doseg)
			continue
		}
		zeli := 100.0 // Botovo sada
		if tm.Doseg > 0 {
			zeli = prikaz["botovo"].Cm[tm.Doseg].Vrijednost
		}
		if v := n.V[0][i]; v == nil || fmt.Sprintf("%.1f", *v) != fmt.Sprintf("%.1f", zeli) {
			t.Errorf("%s: Botovo na profilu %v, u nizu prikaza %.1f", tm.Naslov, v, zeli)
		}
	}
}

// Bez skripte stranica crta termin iz adrese: istaknut čip (poveznica
// ?dan=), plava crta tog dosega, opis u legendi i isti stupac u kartici
// letve. Klizača, gumba ▶ i stare legende s gumbima dosega više nema.
func TestStranicaProfilOdabraniTermin(t *testing.T) {
	letve := letveDrave()
	termini := []TerminProfila{
		{Oznaka: "sada", Naslov: "sada", Opis: "sada, zadnje mjerenje"},
		{Oznaka: "12h", Naslov: "+12 h", Opis: "za 12 h", Doseg: 12},
		{Oznaka: "1", Naslov: "pon 2. 11.", Opis: "odabrani dan, pon 2. 11. u 7 h", Doseg: 24},
		{Oznaka: "2", Naslov: "uto 3. 11.", Opis: "odabrani dan, uto 3. 11. u 7 h", Doseg: 48},
	}
	odabran := odabraniTermin(termini, "2")
	p := crtajUzduzni("Drava", letve, uscaDrave, braneDrave()...)
	p.JutroOpis, p.Odabrano = "jutro 2. 11. u 7 h", odabran.Doseg
	html := iscrtaj(t, "prognoze.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "Ivka Proba"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Izdano: "2.11.2026. u 08:00", Udio: 90, Bliski: BliziDosezi,
		Profili: []*UzduzniProfil{p}, Termini: termini, Termin: odabran,
		Dani: []string{"pon 2. 11.", "uto 3. 11."},
		Tablice: []TablicaPrognoza{{Naslov: "Drava", Letve: []LetvaPrognoze{{
			Kod: "botovo", Naziv: "Botovo", Voda: "Drava", Racuna: "vodostaj", SadaCm: "210", ImaTermina: true,
			Vrijednosti: []VrijednostPrognoze{{DosegH: 6, Ima: true, Cm: "212"}, {DosegH: 12, Ima: true, Cm: "214"}},
			Dani:        []CelijaDana{{Naslov: "pon 2. 11.", Termin: "1", Cm: "236"}, {Naslov: "uto 3. 11.", Termin: "2", Cm: "247"}},
		}}}},
	})
	imaSve(t, html, "odabran uto 3. 11.", []string{
		`href="?dan=2#profil-drava" class="uzd-cip" data-termin="2" data-doseg="48"`,
		`<span class="uzd-legenda-termin">plavo: odabrani dan, uto 3. 11. u 7 h</span>`,
		`<h2 class="uzd-naslov">Uzdužni profil Drave</h2>`, `<span class="uzd-redak-naslov">Uzdužni profil Drave</span>`,
		"Ljubičasto: jutro 2. 11. u 7 h", "isprekidano: uobičajena voda (10 godina)",
		"Više na stranici O prognozi",
		"otvara se preko cijelog zaslona", `class="uzd-gumb uzd-ispis"`, `class="uzd-gumb uzd-puni"`,
	})
	cipovi := html[strings.Index(html, `<nav class="uzd-termini"`):]
	cipovi = cipovi[:strings.Index(cipovi, "</nav>")]
	if n := strings.Count(cipovi, `aria-current="true"`); n != 1 || !regexp.MustCompile(`data-termin="2"[^>]*aria-current="true">uto 3. 11.</a>`).MatchString(html) {
		t.Errorf("istaknut mora biti samo čip uto 3. 11.; aria-current %d puta", n)
	}
	if !strings.Contains(html, `class="uzd-crta uzd-odabrana" data-doseg="48"`) || strings.Count(html, "uzd-crta uzd-odabrana") != 1 {
		t.Error("plava crta nije ona za 48 h, ili ih je više")
	}
	if !strings.Contains(html, `class="uzd-pojas uzd-odabrana" data-doseg="48"`) {
		t.Error("raspon odabranog termina nije vidljiv")
	}
	odabrane := regexp.MustCompile(`class="prog-termin[^"]*prog-odabran"\s+data-termin="([^"]+)"`).FindAllStringSubmatch(html, -1)
	if len(odabrane) != 1 || odabrane[0][1] != "2" {
		t.Errorf("u kartici mora biti istaknut stupac uto 3. 11.: %v", odabrane)
	}
	nemaNista(t, html, "bez klizača", []string{`type="range"`, "uzd-klizac", "▶", "uzd-legenda-gumb", "data-nula-y",
		"na profilu nema prognoze</span>", "reg-card uzduzni"})

	// Termin za koji na profilu nema crte (dnevni model ne seže do svih
	// letvi, ili u tjednu promjene sata šesti dan pada na 145. sat) legenda
	// tako i kaže, umjesto da obeća plavu crtu.
	bez := TerminProfila{Oznaka: "6", Naslov: "pon 26. 10.", Opis: "odabrani dan, pon 26. 10. u 7 h", Doseg: 145}
	p.Odabrano = bez.Doseg
	html = iscrtaj(t, "prognoze.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "Ivka Proba"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Izdano: "x", Profili: []*UzduzniProfil{p}, Termini: append(termini, bez), Termin: bez,
	})
	imaSve(t, html, "termin bez crte", []string{`<span class="uzd-legenda-termin">pon 26. 10.: za taj termin na profilu nema prognoze</span>`})
	if strings.Contains(html, "uzd-crta uzd-odabrana") {
		t.Error("termin bez crte, a jedna crta je odabrana")
	}

	// „+12 h” ističe stupac +12 h, a „sada” nijedan.
	html = iscrtaj(t, "prognoze.html", PrognozePageData{
		CurrentUser: &models.User{FullName: "Ivka Proba"}, Permissions: &models.UserPermissions{IsGlobalAdmin: true},
		ActiveNav: "prognoze", Izdano: "x", Termini: termini, Termin: odabraniTermin(termini, "12h"),
		Tablice: []TablicaPrognoza{{Naslov: "Drava", Letve: []LetvaPrognoze{{Kod: "botovo", Naziv: "Botovo", ImaTermina: true,
			Vrijednosti: []VrijednostPrognoze{{DosegH: 6, Ima: true, Cm: "212"}, {DosegH: 12, Ima: true, Cm: "214"}}}}}},
	})
	if odabrane = regexp.MustCompile(`class="prog-termin[^"]*prog-odabran"\s+data-termin="([^"]+)"`).FindAllStringSubmatch(html, -1); len(odabrane) != 1 || odabrane[0][1] != "12h" {
		t.Errorf("+12 h: istaknuto %v", odabrane)
	}
}

// Kroz poslužitelj: ?dan= bira stupac u karticama letvi, a nepoznat dan
// vraća na „sada”, kad nijedan stupac nije istaknut.
func TestStranicaPrognozaDanIzAdrese(t *testing.T) {
	p := noviPosluziteljOcitanja(t)
	izdano := time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix() / 3600
	c, err := OtvoriPrognoze(bazaPrognoza(t, izdano))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	p.s.SetPrognoze(c)
	odabrano := regexp.MustCompile(`class="prog-termin[^"]*prog-odabran"\s+data-termin="([^"]+)"`)
	w := p.posalji("dežurni", http.MethodGet, "/prognoze?dan=3", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("pregled: %d", w.Code)
	}
	nadeno := odabrano.FindAllStringSubmatch(w.Body.String(), -1)
	if len(nadeno) == 0 {
		t.Fatal("?dan=3: nijedan stupac nije istaknut")
	}
	for _, m := range nadeno {
		if m[1] != "3" {
			t.Errorf("?dan=3 ističe stupac %s", m[1])
		}
	}
	for _, upit := range []string{"/prognoze?dan=99", "/prognoze", "/prognoze?dan=sada"} {
		if w = p.posalji("dežurni", http.MethodGet, upit, nil, ""); odabrano.MatchString(w.Body.String()) {
			t.Errorf("%s: istaknut je stupac, a termin je „sada”", upit)
		}
	}
}

// U tjednu kad sat prelazi na zimsko vrijeme šesti dan pada na 145. sat od
// izdanja u 7 h, a niz prikaza ide do 144.: čip ostaje, crte nema, pa
// legenda kaže da za taj termin prognoze na profilu nema.
func TestTerminiUTjednuPromjeneSata(t *testing.T) {
	izdano := time.Date(2026, 10, 20, 7, 0, 0, 0, models.Zagreb)
	dani, ciljevi := daniPregleda(izdano)
	termini := terminiProfila(dani, ciljevi, izdano.Unix()/3600)
	zadnji := termini[len(termini)-1]
	if zadnji.Naslov != "pon 26. 10." || zadnji.Doseg != SatiPrikaza+1 {
		t.Errorf("šesti dan %q za %d h", zadnji.Naslov, zadnji.Doseg)
	}
	p := &UzduzniProfil{Crte: []ProfilCrta{{Doseg: 0}, {Doseg: 120}}}
	if !p.ImaCrtu(120) || p.ImaCrtu(zadnji.Doseg) {
		t.Error("ImaCrtu ne prepoznaje koje crte ima profil")
	}
}

// Naslov profila je u genitivu: „Uzdužni profil Drave”, „… Dunava”; ime
// koje se ne da promijeniti ostaje u nominativu iza točke.
func TestNaslovProfila(t *testing.T) {
	for ime, want := range map[string]string{
		"Drava": "Uzdužni profil Drave", "Dunav": "Uzdužni profil Dunava", "Karašica": "Uzdužni profil Karašice",
		"Brodska rijeka": "Uzdužni profil Brodske rijeke", "Drava i Mura": "Uzdužni profil Drave i Mure",
		"ostalo": "Uzdužni profil · ostalo", "Kanal 2": "Uzdužni profil · Kanal 2", "": "Uzdužni profil · ",
	} {
		if got := (&UzduzniProfil{Ime: ime}).Naslov(); got != want {
			t.Errorf("%q: %q, želim %q", ime, got, want)
		}
	}
}
