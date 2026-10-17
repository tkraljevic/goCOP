package web

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/arhiva"
	"gocop/internal/db"
	"gocop/internal/models"
	"gocop/internal/poslovi"
)

// izdavacVrata bilježi što je izdavanje dobilo i vraća zadani ishod
type izdavacVrata struct {
	pozivi []string
	iz     arhiva.IzvjestajIzdanja
	greska error
}

func (z *izdavacVrata) izdaj(letva string, probno bool, w io.Writer) (arhiva.IzvjestajIzdanja, error) {
	z.pozivi = append(z.pozivi, fmt.Sprintf("%s/%v", letva, probno))
	_, _ = fmt.Fprintf(w, "sastavljam %q\n", letva)
	iz := z.iz
	iz.Probno = probno
	return iz, z.greska
}

// gradnjaVrata bilježi građene letve, piše u dnevnik posla i vraća zadano
type gradnjaVrata struct {
	letve    []string
	sirotani []arhiva.Sirotan
	greska   error
}

func (g *gradnjaVrata) izgradi(letva string, w io.Writer) (arhiva.Izvjestaj, error) {
	g.letve = append(g.letve, letva)
	_, _ = fmt.Fprintf(w, "gradim %s\n", letva)
	return arhiva.Izvjestaj{Sirotani: g.sirotani}, g.greska
}

// okolinaUpisa je okolina vrata koja gradi i izdaje kroz zapisivače
func okolinaUpisa(t *testing.T) (*okolinaVrata, *gradnjaVrata, *izdavacVrata) {
	t.Helper()
	o := novaOkolinaVrata(t)
	g, z := &gradnjaVrata{}, &izdavacVrata{iz: arhiva.IzvjestajIzdanja{Promijenjenih: 1,
		Redci: []arhiva.RedIzdanja{{Letva: "primjerovo", Prije: 1, Izdanje: 2, Novo: true, Otisak: "a1b2c3d4e5", Zapisa: 3}}}}
	o.h.izgradi = g.izgradi
	paketi := t.TempDir()
	o.h.SetIzdavanje(func() string { return paketi }, z.izdaj, func() (arhiva.Katalog, error) { return arhiva.Katalog{}, nil }, nil)
	return o, g, z
}

// stranicaPosla otvara vrata s brojem posla, kako to radi preglednik kad posao završi
func (o *okolinaVrata) stranicaPosla(u *models.User, p *models.UserPermissions, id string) string {
	r := httptestGet("/administracija/uvoz-niza?posao=" + url.QueryEscape(id))
	return htmlUTekst(odgovor(o.h.ShowUvoz, r, u, p).Body.String())
}

func httptestGet(putanja string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, putanja, nil)
	return r
}

// nizUStablu čita zapisani niz letve primjerovo iz izvora i vrste
func (o *okolinaVrata) nizUStablu(t *testing.T, izvor string) string {
	t.Helper()
	puts, _ := filepath.Glob(filepath.Join(o.koren, "primjerica", "primjerovo", "primjerovo_"+izvor+"_vodostaj_dnevni_*.csv"))
	if len(puts) != 1 {
		t.Fatalf("niz %s u stablu: %v", izvor, puts)
	}
	return procitaj(t, puts[0])
}

// Potvrđen niz dopunjuje ono što letva već ima i gradi letvu u pozadini; uz
// izdavača odmah se gleda bi li letva dobila novo izdanje. Stranica posla
// poslije pokazuje ispis gradnje, ishod i prijedlog izdanja.
func TestUpisNizaDopunjujeIGradi(t *testing.T) {
	o, g, z := okolinaUpisa(t)
	o.zatecenPrimjerova(t)
	id := o.posalji(t, o.pero, o.peroOvl, "primjerovo_probni_vodostaj_dnevni.csv", dnevniPrimjerova)
	w := odgovor(o.h.UpisiUvoz, obicniObrazac("/administracija/uvoz-niza/upisi", "id="+id), o.pero, o.peroOvl)
	html := htmlUTekst(w.Body.String())
	p := posaoSaStranice(t, o.h.poslovi, html, o.pero)
	put := filepath.Join(o.koren, "primjerica", "primjerovo", "primjerovo_probni_vodostaj_dnevni_2026.csv")
	if p.Greska() != "" || p.Stanje().Sazetak != "Zapisano u "+put+"; primjerovo je ponovno izgrađena." {
		t.Fatalf("posao upisa: %q %q", p.Greska(), p.Stanje().Sazetak)
	}
	if got := o.nizUStablu(t, "probni"); got != "datum;vodostaj_cm\n2026-02-28;118\n2026-03-01;120\n2026-03-02;125\n2026-03-03;131\n" {
		t.Errorf("dopuna mora zadržati 28.02. i uzeti novu vrijednost 01.03.:\n%s", got)
	}
	if len(g.letve) != 1 || g.letve[0] != "primjerovo" || len(z.pozivi) != 1 || z.pozivi[0] != "primjerovo/true" {
		t.Errorf("gradnja %v, izdavanje %v", g.letve, z.pozivi)
	}
	ishod, ok := p.Plod().(*ishodUvoza)
	if !ok || ishod.Put != put || ishod.Letva != "primjerovo" || ishod.Cekaizvor != "" || ishod.Izdanja == nil || !ishod.Izdanja.Probno {
		t.Errorf("ishod upisa: %+v", p.Plod())
	}
	if _, ima := uvozi.uzmi(id, o.pero.ID.String()); ima {
		t.Error("upisan odabir i dalje čeka")
	}

	stranica := o.stranicaPosla(o.pero, o.peroOvl, p.ID)
	for _, treba := range []string{"Zapisano u " + put, "gradim primjerovo", "Što bi se izdalo", "· primjerovo", "v1 → v2", "Izdaj primjerovo"} {
		if !strings.Contains(stranica, treba) {
			t.Errorf("stranica posla nema %q", treba)
		}
	}
	if rePosaoNaStranici.MatchString(stranica) {
		t.Error("gotov posao crta traku")
	}
}

// Zamjena baca zatečeno i ostavlja samo novu datoteku. Izvor koji ne ulazi u
// spoj stranica posla posebno istakne, kao i nizove koji su ostali bez
// datoteke; izdanje koje se ne da provjeriti samo se zapiše u dnevnik.
func TestUpisNizaZamjenjujeINoviIzvorCeka(t *testing.T) {
	o, g, z := okolinaUpisa(t)
	o.zatecenPrimjerova(t)
	z.greska = errors.New("katalog je zaključan")
	g.sirotani = []arhiva.Sirotan{{Letva: "primjerovo", Izvor: "stari", Velicina: "vodostaj", Vrsta: "dnevni", Zapisa: 7}}
	id := o.posalji(t, o.pero, o.peroOvl, "satni.csv", dnevniPrimjerova)
	forma := url.Values{"id": {id}, "letva": {"primjerovo"}, "izvor": {"cekajuci"}, "velicina": {"vodostaj"},
		"vrsta": {"dnevni"}, "nacin": {"zamijeni"}}
	w := odgovor(o.h.UpisiUvoz, obicniObrazac("/administracija/uvoz-niza/upisi", forma.Encode()), o.pero, o.peroOvl)
	p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero)
	if p.Greska() != "" {
		t.Fatalf("posao upisa: %s", p.Greska())
	}
	if got := o.nizUStablu(t, "cekajuci"); got != "datum;vodostaj_cm\n2026-03-01;120\n2026-03-02;125\n2026-03-03;131\n" {
		t.Errorf("zamjena:\n%s", got)
	}
	if !strings.Contains(p.Dnevnik(), "izdanje se nije dalo provjeriti: katalog je zaključan") {
		t.Errorf("dnevnik posla: %q", p.Dnevnik())
	}
	ishod, _ := p.Plod().(*ishodUvoza)
	if ishod == nil || ishod.Cekaizvor != "cekajuci" || ishod.Izdanja != nil || len(ishod.Sirotani) != 1 {
		t.Errorf("ishod: %+v", ishod)
	}
	stranica := o.stranicaPosla(o.pero, o.peroOvl, p.ID)
	for _, treba := range []string{"Izvor <strong class=\"mono\">cekajuci</strong> arhiva još ne poznaje", "Nizovi bez datoteke u stablu.",
		"stari · vodostaj · dnevni"} {
		if !strings.Contains(stranica, treba) {
			t.Errorf("stranica posla nema %q", treba)
		}
	}

	// Zamjena istog niza odnese ono čega u novoj datoteci nema.
	id = o.posalji(t, o.pero, o.peroOvl, "primjerovo_probni_vodostaj_dnevni.csv", dnevniPrimjerova)
	w = odgovor(o.h.UpisiUvoz, obicniObrazac("/", "id="+id+"&nacin=zamijeni"), o.pero, o.peroOvl)
	posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero)
	if got := o.nizUStablu(t, "probni"); strings.Contains(got, "2026-02-28") || !strings.Contains(got, "2026-03-01;120") {
		t.Errorf("zamjena istog niza:\n%s", got)
	}
}

// Potvrda niza ne prolazi bez prava, s isteklim odabirom ni s tuđom letvom;
// letva koja se ne zna, neupotrebljiv dio naziva, nova letva bez sliva i sliv
// koji ne može biti mapa vraćaju poruku, a odabir i dalje čeka.
func TestUpisNizaOdbija(t *testing.T) {
	o, g, _ := okolinaUpisa(t)
	idAna := o.posalji(t, o.ana, o.anaOvl, "satni.csv", dnevniPrimjerova)
	idPero := o.posalji(t, o.pero, o.peroOvl, "satni.csv", dnevniPrimjerova)
	niz := func(id string, polja ...string) string {
		v := url.Values{"id": {id}, "izvor": {"probni"}, "velicina": {"vodostaj"}, "vrsta": {"dnevni"}}
		for i := 0; i+1 < len(polja); i += 2 {
			v.Set(polja[i], polja[i+1])
		}
		return v.Encode()
	}
	for _, s := range []struct {
		opis   string
		u      *models.User
		p      *models.UserPermissions
		tijelo string
		status int
		poruka string
	}{
		{"bez ijedne letve", o.marko, o.markoOvl, niz(idAna), http.StatusForbidden, "unosi administrator"},
		{"neispravan obrazac", o.ana, o.anaOvl, "id=%zz", http.StatusOK, "invalid URL escape"},
		{"istekao odabir", o.ana, o.anaOvl, niz("nema-ga"), http.StatusOK, "Odabir je istekao ili više ne postoji. Odaberite datoteku ponovno."},
		{"tuđa letva", o.ana, o.anaOvl, niz(idAna, "letva", "drugovo"), http.StatusOK,
			"letva drugovo nije na vašim dionicama — njezine podatke uvozi administrator njezina područja"},
		// tekst se ne zaključava: vidi TestUpisNizaBezLetveKodOperatera
		{"letva se ne zna", o.ana, o.anaOvl, niz(idAna), http.StatusOK, ""},
		{"bez letve ni globalni", o.pero, o.peroOvl, niz(idPero), http.StatusOK, "letva nije zadana"},
		{"nova letva bez sliva", o.pero, o.peroOvl, niz(idPero, "letva", "novovo"), http.StatusOK,
			"Nije zadan sliv — nova letva mora doći sa slivom, inače nema gdje stati."},
		{"sliv nije mapa", o.pero, o.peroOvl, niz(idPero, "letva", "novovo", "sliv", "nov sliv"), http.StatusOK,
			`upis nije uspio: sliv "nov sliv" nije upotrebljiv kao naziv mape`},
	} {
		t.Run(s.opis, func(t *testing.T) {
			w := odgovor(o.h.UpisiUvoz, obicniObrazac("/administracija/uvoz-niza/upisi", s.tijelo), s.u, s.p)
			if w.Code != s.status || !strings.Contains(htmlUTekst(w.Body.String()), s.poruka) {
				t.Errorf("status %d, očekivan %d s porukom %q: %s", w.Code, s.status, s.poruka, porukeStranice(w.Body.String()))
			}
			if s.poruka == "" && porukaGreske(w.Body.String()) == "" {
				t.Errorf("odbijena potvrda nema poruku o grešci: %s", porukeStranice(w.Body.String()))
			}
			if rePosaoNaStranici.MatchString(w.Body.String()) {
				t.Error("odbijena potvrda pokrenula je gradnju")
			}
		})
	}
	for id, u := range map[string]*models.User{idAna: o.ana, idPero: o.pero} {
		if _, ima := uvozi.uzmi(id, u.ID.String()); !ima {
			t.Errorf("odbijena potvrda potrošila je odabir %s", u.FullName)
		}
	}
	if d := o.datoteke(t); len(d) != 0 || len(g.letve) != 0 {
		t.Errorf("odbijena potvrda zapisala je %v, gradila %v", d, g.letve)
	}
}

// Potvrda bez letve kaže operateru isto što i globalnom administratoru: da
// letva nije zadana.
func TestUpisNizaBezLetveKodOperatera(t *testing.T) {
	o, _, _ := okolinaUpisa(t)
	id := o.posalji(t, o.ana, o.anaOvl, "satni.csv", dnevniPrimjerova)
	forma := url.Values{"id": {id}, "izvor": {"probni"}, "velicina": {"vodostaj"}, "vrsta": {"dnevni"}}
	w := odgovor(o.h.UpisiUvoz, obicniObrazac("/administracija/uvoz-niza/upisi", forma.Encode()), o.ana, o.anaOvl)
	if g := porukaGreske(w.Body.String()); !strings.Contains(g, "letva nije zadana") {
		t.Errorf("potvrda bez letve kod operatera: %q", g)
	}
}

// Kad gradnja nakon upisa padne, posao kaže da je datoteka ipak zapisana i
// gdje, a stranica posla to javlja kao grešku s ispisom gradnje.
func TestUpisNizaGradnjaPada(t *testing.T) {
	o, g, _ := okolinaUpisa(t)
	g.greska = errors.New("arhiva je zaključana")
	id := o.posalji(t, o.pero, o.peroOvl, "primjerovo_probni_vodostaj_dnevni.csv", dnevniPrimjerova)
	w := odgovor(o.h.UpisiUvoz, obicniObrazac("/", "id="+id), o.pero, o.peroOvl)
	p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero)
	put := filepath.Join(o.koren, "primjerica", "primjerovo", "primjerovo_probni_vodostaj_dnevni_2026.csv")
	if zeli := "datoteka je zapisana u " + put + ", ali gradnja letve nije uspjela: arhiva je zaključana"; p.Greska() != zeli {
		t.Errorf("pad gradnje: %q", p.Greska())
	}
	stranica := o.stranicaPosla(o.pero, o.peroOvl, p.ID)
	if g := porukaGreske(stranica); !strings.HasPrefix(g, "Izgradnja letve primjerovo nije uspjela: datoteka je zapisana u ") ||
		!strings.Contains(stranica, "gradim primjerovo") {
		t.Errorf("stranica palog posla: %q", g)
	}
}

// Stranica posla: posao koji traje daje samo svoj broj za traku, tuđi i
// nepoznat posao se ne pokazuju, a gotovo izdavanje jedne letve nosi tu letvu.
func TestStranicaPoslaUvoza(t *testing.T) {
	o, _, _ := okolinaUpisa(t)
	pusti := make(chan struct{})
	traje := o.h.poslovi.Pokreni("Izgradnja letve primjerovo", o.pero.ID.String(), "", func(*poslovi.Posao) error {
		<-pusti
		return nil
	})
	stranica := o.stranicaPosla(o.pero, o.peroOvl, traje.ID)
	if !strings.Contains(stranica, `data-posao="`+traje.ID+`"`) || !strings.Contains(stranica, "Izgradnja letve primjerovo") {
		t.Error("posao koji traje nema traku")
	}
	if s := o.stranicaPosla(o.ana, o.anaOvl, traje.ID); rePosaoNaStranici.MatchString(s) {
		t.Error("tuđi posao se vidi")
	}
	close(pusti)
	pricekajPosao(t, traje)
	if s := o.stranicaPosla(o.pero, o.peroOvl, "nema-ga"); rePosaoNaStranici.MatchString(s) || porukaGreske(s) != "" {
		t.Error("nepoznat posao nešto pokazuje")
	}

	izdanje := o.h.poslovi.Pokreni("Izdavanje letve primjerovo", o.pero.ID.String(), "", func(p *poslovi.Posao) error {
		p.Zavrsi("1 letva je dobila novo izdanje u /paketi; 0 je nepromijenjeno.", &arhiva.IzvjestajIzdanja{Promijenjenih: 1,
			Redci: []arhiva.RedIzdanja{{Letva: "primjerovo", Prije: 2, Izdanje: 3, Novo: true, Otisak: "f0e1d2c3b4"}}})
		return nil
	})
	pricekajPosao(t, izdanje)
	stranica = o.stranicaPosla(o.pero, o.peroOvl, izdanje.ID)
	for _, treba := range []string{"1 letva je dobila novo izdanje u /paketi", "Što je izdano", "· primjerovo", "v2 → v3", "Provjeri primjerovo"} {
		if !strings.Contains(stranica, treba) {
			t.Errorf("stranica izdanja nema %q", treba)
		}
	}
}

// Izdavanje ide u pozadini pod nazivom koji kaže što se radi: provjera ili
// izdavanje, jedne letve ili cijele arhive. Čvor koji ne izdaje to kaže, a
// izdavanje koje padne javlja se kao pad posla.
func TestIzdavanjeKaoPosao(t *testing.T) {
	bez := novaOkolinaVrata(t)
	w := odgovor(bez.h.Izdaj, obicniObrazac("/administracija/izdavanje", ""), bez.pero, bez.peroOvl)
	if g := porukaGreske(htmlUTekst(w.Body.String())); g != "Ovaj čvor ne izdaje arhivu — nije mu zadana mapa u koju bi izdavao." {
		t.Errorf("čvor koji ne izdaje: %q", g)
	}

	o, _, z := okolinaUpisa(t)
	for _, s := range []struct {
		h      http.HandlerFunc
		letva  string
		naziv  string
		poziv  string
		probno bool
	}{
		{o.h.ProvjeriIzdanja, "", "Provjera izdanja", "/true", true},
		{o.h.ProvjeriIzdanja, "primjerovo", "Provjera izdanja — primjerovo", "primjerovo/true", true},
		{o.h.Izdaj, "", "Izdavanje cijele arhive", "/false", false},
		{o.h.Izdaj, " primjerovo ", "Izdavanje letve primjerovo", "primjerovo/false", false},
	} {
		z.pozivi = nil
		w := odgovor(s.h, obicniObrazac("/administracija/izdavanje", "letva="+url.QueryEscape(s.letva)), o.pero, o.peroOvl)
		p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero)
		iz, _ := p.Plod().(*arhiva.IzvjestajIzdanja)
		if p.Naziv != s.naziv || len(z.pozivi) != 1 || z.pozivi[0] != s.poziv || iz == nil || iz.Probno != s.probno ||
			p.Stanje().Sazetak != sazetakIzdanja(*iz) || !strings.Contains(p.Dnevnik(), "sastavljam") {
			t.Errorf("%s: posao %q, pozivi %v, plod %+v", s.naziv, p.Naziv, z.pozivi, iz)
		}
	}
	z.greska = errors.New("mapa paketa je puna")
	w = odgovor(o.h.Izdaj, obicniObrazac("/administracija/izdavanje", ""), o.pero, o.peroOvl)
	if p := posaoSaStranice(t, o.h.poslovi, w.Body.String(), o.pero); p.Greska() != "mapa paketa je puna" {
		t.Errorf("pad izdavanja: %q", p.Greska())
	}
}

// Vrata uz izdavača pokazuju katalog izdanja s vremenom zadnjeg izdavanja po
// našem satu; katalog koji se ne čita javlja se na vrhu stranice.
func TestVrataPokazujuKatalog(t *testing.T) {
	o, _, z := okolinaUpisa(t)
	katalog := arhiva.Katalog{Izdao: "cvor-primjer", Nastalo: time.Date(2026, 9, 11, 9, 30, 0, 0, time.UTC),
		Paketi: []arhiva.UPaketu{{Letva: "primjerovo", Izdanje: 2}}}
	var greska error
	o.h.SetIzdavanje(func() string { return "/paketi-primjer" }, z.izdaj, func() (arhiva.Katalog, error) { return katalog, greska }, nil)
	stranica := htmlUTekst(odgovor(o.h.ShowUvoz, httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl).Body.String())
	for _, treba := range []string{"Izdanja arhive", "11.09.2026. 11:30", "cvor-primjer", "/paketi-primjer"} {
		if !strings.Contains(stranica, treba) {
			t.Errorf("vrata nemaju %q", treba)
		}
	}
	greska = errors.New("katalog.json je oštećen")
	stranica = htmlUTekst(odgovor(o.h.ShowUvoz, httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl).Body.String())
	if g := porukaGreske(stranica); g != "Katalog izdanja se ne čita: katalog.json je oštećen" {
		t.Errorf("katalog koji se ne čita: %q", g)
	}
}

// Uz operativnu bazu vrata nude ulaganje očitanja i popis uloženog koje čeka
// pospremanje; baza koja se ne da pitati javlja grešku, a arhiva koja se ne
// čita kaže da se nizovi bez datoteke ne daju provjeriti.
func TestVrataUlaganjaIProvjereArhive(t *testing.T) {
	o := novaOkolinaVrata(t)
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "operativna.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO stations (id, code, name, created_at, updated_at) VALUES ('s-primjer', 'primjerovo', 'Primjerovo', '2026-01-01', '2026-01-01')`,
		`INSERT INTO readings (id, station_id, measured_at, level_cm, source, user_id, created_at, updated_at, izdanje)
		 VALUES ('r-primjer', 's-primjer', '2026-03-01T06:00:00Z', 120, 'RUČNO', '', '2026-03-01', '2026-03-01', '4')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	trenutna := baza
	o.h.SetOcitanja(func() *sql.DB { return trenutna }, func() string { return "cvor-primjer" })
	stranica := htmlUTekst(odgovor(o.h.ShowUvoz, httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl).Body.String())
	for _, treba := range []string{"Očitanja iz programa", "Uloženo, čeka pospremanje", "Pospremi primjerovo", "izdanje 4"} {
		if !strings.Contains(stranica, treba) {
			t.Errorf("vrata nemaju %q: %s", treba, porukeStranice(stranica))
		}
	}

	prazna, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "prazna.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { prazna.Close() })
	trenutna = prazna
	stranica = htmlUTekst(odgovor(o.h.ShowUvoz, httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl).Body.String())
	if g := porukaGreske(stranica); !strings.HasPrefix(g, "Popis uloženog se ne čita: ") || !strings.Contains(g, "readings") {
		t.Errorf("baza bez tablica: %q", g)
	}

	// Arhiva koja nije baza: nizovi bez datoteke se ne daju provjeriti.
	if err := os.WriteFile(o.arhivaPut, []byte("ovo nije baza"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.h.SetOcitanja(nil, nil)
	stranica = htmlUTekst(odgovor(o.h.ShowUvoz, httptestGet("/administracija/uvoz-niza"), o.pero, o.peroOvl).Body.String())
	if g := porukaGreske(stranica); !strings.HasPrefix(g, "Nizovi bez datoteke se ne daju provjeriti: ") {
		t.Errorf("arhiva koja nije baza: %q", g)
	}
}

// Vrata po zadanom ništa ne javljaju nakon pospremanja, a poslužitelj im daje
// brojanje u razmjeni (peers.ObrisanoIzKnjige).
func TestNakonPospremanjaJavljaRazmjeni(t *testing.T) {
	o := novaOkolinaVrata(t)
	o.h.nakonPospremanja(5)
	var javljeno []int
	o.h.SetNakonPospremanja(func(n int) { javljeno = append(javljeno, n) })
	o.h.nakonPospremanja(5)
	if len(javljeno) != 1 || javljeno[0] != 5 {
		t.Errorf("javljeno %v, očekivano [5]", javljeno)
	}
}
