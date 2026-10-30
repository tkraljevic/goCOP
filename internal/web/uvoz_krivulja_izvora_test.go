package web

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/db"
	"gocop/internal/hidroview"
	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/repository"
	"gocop/internal/uvoz/izvori"
)

// krivuljePrimjerova je izmišljeni izvoz krivulja iz HIS-2000 u njegovoj
// kodnoj stranici (0x8a je Š): dvije krivulje, prva s polinomskim i
// potencijskim odsječkom, uz kotu nule i koordinate letve.
const krivuljePrimjerova = "Krivulje protoka\r\n\x8aifra;Podsifra;Stare sifre;Naziv;Vodotok;Kota \"0\";Sirina;Duzina;\r\n" +
	"9001;1;;PRIMJEROVO;PRIMJERICA;100,00;45 30 0;18 30 0;\r\n\r\n" +
	"1/1/2001 - 12/31/2009\r\nTip;H1;H2;A;B;C;D;\r\n" +
	"1;100,0000;500,0000;40,0000;300,0000;600,0000;0,0000;\r\n" +
	"0;0,0000;100,0000;50,0000;0,5000;2,0000;10,0000;\r\n\r\n" +
	"1/1/2010 - 12/31/2012\r\nTip;H1;H2;A;B;C;D;\r\n" +
	"1;0,0000;300,0000;40,0000;300,0000;600,0000;0,0000;\r\n"

// Pregled krivulja pokazuje razdoblje, odsječke s koeficijentima kao brojeve
// i kotu nule iz izvoza, a uz to što letva već ima u stablu i kako se izvoz
// s tim spaja: koje se zatečene krivulje razlikuju i koje ostaju.
func TestPregledKrivuljaIZateceno(t *testing.T) {
	o := novaOkolinaVrata(t)
	hq := filepath.Join(o.koren, "primjerica", "primjerovo", "hq")
	if err := os.MkdirAll(hq, 0o755); err != nil {
		t.Fatal(err)
	}
	// 1990. izvoz nema, 2001. je ista, a 2010. ima drukčiji p1.
	zateceno := "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n" +
		"1990-01-01;2000-12-31;0;400;polinom;30;200;500;;DHMZ, HIS-2000;\n" +
		"2001-01-01;2009-12-31;100;500;polinom;40;300;600;;DHMZ, HIS-2000;\n" +
		"2001-01-01;2009-12-31;0;100;potencija;50;2;0,5;10;DHMZ, HIS-2000;\n" +
		"\n" +
		"2010-01-01;;0;300;polinom;41;300;600;;DHMZ, HIS-2000;\n"
	if err := os.WriteFile(filepath.Join(hq, "primjerovo_hq_krivulje.csv"), []byte(zateceno), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := pregledKrivulja("krivulje.csv", []byte(krivuljePrimjerova), "primjerovo", o.koren)
	if err != nil {
		t.Fatal(err)
	}
	if p.Krivulja != 2 || p.Odsjecaka != 3 || p.Potencija != 1 || p.Od != "2001-01-01" || p.Do != "2012-12-31" {
		t.Errorf("pregled: %+v", p)
	}
	if p.KotaNule != "100,00" || p.Sirina != "45 30 0" || p.Duzina != "18 30 0" || p.Letva != "primjerovo" {
		t.Errorf("zaglavlje postaje: %+v", p)
	}
	if p.ZatecenihKrivulja != 3 || p.ZatecenihOdsjecaka != 4 {
		t.Errorf("zatečeno %d krivulja i %d odsječaka, očekivano 3 i 4", p.ZatecenihKrivulja, p.ZatecenihOdsjecaka)
	}
	if p.Ukupno != 3 || strings.Join(p.Razlikuju, ",") != "2010-01-01" || strings.Join(p.Ostaju, ",") != "1990-01-01" {
		t.Errorf("spajanje: ukupno %d, razlikuju %v, ostaju %v", p.Ukupno, p.Razlikuju, p.Ostaju)
	}
	if len(p.Popis) != 2 || len(p.Popis[0].Odsjecci) != 2 || p.Popis[1].Od != "2010-01-01" {
		t.Fatalf("popis krivulja: %+v", p.Popis)
	}
	if p.Popis[0].Razlikuje || !p.Popis[1].Razlikuje {
		t.Errorf("oznaka razlike: 2001. %v, 2010. %v", p.Popis[0].Razlikuje, p.Popis[1].Razlikuje)
	}
	pol, pot := p.Popis[0].Odsjecci[0], p.Popis[0].Odsjecci[1]
	if pol.Oblik != models.OblikPolinom || pol.OdCm != 100 || pol.DoCm != 500 || pol.P1 != 40 || pol.P2 != 300 || pol.P3 != 600 {
		t.Errorf("polinomski odsječak: %+v", pol)
	}
	if pot.Oblik != models.OblikPotencija || pot.P1 != 50 || pot.P2 != 2 || pot.P3 != 0.5 || pot.P4 != 10 {
		t.Errorf("potencijski odsječak: %+v", pot)
	}

	// Letva bez datoteke krivulja nema ništa zatečeno, a letva koje u
	// stablu nema odbija se već u pregledu, jer krivulje ne bi imale kamo.
	p, err = pregledKrivulja("krivulje.csv", []byte(krivuljePrimjerova), "drugovo", o.koren)
	if err != nil || p.ZatecenihKrivulja != 0 || p.ZatecenihOdsjecaka != 0 || p.Ukupno != 2 || p.Razlikuju != nil || p.Ostaju != nil {
		t.Errorf("letva bez krivulja: %+v, %v", p, err)
	}
	if _, err := pregledKrivulja("krivulje.csv", []byte(krivuljePrimjerova), "nepoznata", o.koren); err == nil ||
		err.Error() != "letva nepoznata nije u stablu" {
		t.Errorf("letva koje nema u stablu: %v", err)
	}
}

// Zatečena datoteka krivulja s retkom koji gradnja ne bi pročitala ne spaja
// se: spojena datoteka bi ga tiho izgubila. Poruka cijelim rečenicama kaže
// koji je redak, a put datoteke i razlog idu u dnevnik čvora. Prvi redak
// koji nije zaglavlje, npr. kad datoteka retke dijeli samo znakom CR, je
// redak 1.
func TestPregledKrivuljaNecitljivoZateceno(t *testing.T) {
	o := novaOkolinaVrata(t)
	hq := filepath.Join(o.koren, "primjerica", "primjerovo", "hq")
	if err := os.MkdirAll(hq, 0o755); err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(hq, "primjerovo_hq_krivulje.csv")
	for _, s := range []struct {
		sadrzaj, uDnevniku string
		redak              int
	}{
		{"vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\n\nkratak-redak\n",
			": redak 3: „kratak-redak” ima 1 stupac", 3},
		{"vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;p4;izvor;napomena\r2001-01-01;;0;100;polinom;1;2;3;;DHMZ;\r",
			": redak 1: nije zaglavlje kakvo treba", 1},
	} {
		if err := os.WriteFile(put, []byte(s.sadrzaj), 0o644); err != nil {
			t.Fatal(err)
		}
		var dnevnik bytes.Buffer
		log.SetOutput(&dnevnik)
		t.Cleanup(func() { log.SetOutput(os.Stderr) })
		_, err := pregledKrivulja("krivulje.csv", []byte(krivuljePrimjerova), "primjerovo", o.koren)
		if zeli := fmt.Sprintf("Datoteka krivulja letve primjerovo ima redak koji se ne da pročitati (redak %d). "+
			"Ispravite ga, pa pošaljite izvoz ponovno.", s.redak); err == nil || err.Error() != zeli {
			t.Errorf("nečitljiva zatečena datoteka: %v", err)
		}
		if !strings.Contains(dnevnik.String(), put+s.uDnevniku) {
			t.Errorf("dnevnik čvora: %q", dnevnik.String())
		}
	}
}

// Datoteka koja nije izvoz HIS-a i izvoz koji ne nosi krivulje odbijaju se
// porukom koja kaže što je krivo.
func TestPregledKrivuljaOdbija(t *testing.T) {
	for _, s := range []struct {
		opis, sadrzaj, poruka string
	}{
		{"nije HIS", "datum;vodostaj\n2026-03-01;120\n", "ne čita kao HIS-2000"},
		{"zaglavlje bez krivulja", "Krivulje protoka\r\n", "ne čita kao HIS-2000"},
		{"niz umjesto krivulja", "# letva.voda.hr satni vodostaj\r\n", "nema nijedne krivulje protoka"},
	} {
		if _, err := pregledKrivulja("krivulje.csv", []byte(s.sadrzaj), "primjerovo", t.TempDir()); err == nil ||
			!strings.Contains(err.Error(), s.poruka) {
			t.Errorf("%s: %v", s.opis, err)
		}
	}
}

// Krivulje i snimke korita stoje u mapi letve u stablu; letva koje nema i
// stablo koje se ne da pročitati javljaju grešku umjesto izmišljenog mjesta.
func TestMjestoKrivuljaISnimkiUStablu(t *testing.T) {
	o := novaOkolinaVrata(t)
	put, err := putKrivulja(o.koren, "primjerovo")
	if want := filepath.Join(o.koren, "primjerica", "primjerovo", "hq", "primjerovo_hq_krivulje.csv"); err != nil || put != want {
		t.Errorf("mjesto krivulja %q, %v", put, err)
	}
	mapa, err := mapaProfila(o.koren, "primjerovo")
	if want := filepath.Join(o.koren, "primjerica", "primjerovo", "profil"); err != nil || mapa != want {
		t.Errorf("mapa snimki %q, %v", mapa, err)
	}
	if _, err := putKrivulja(o.koren, "nepoznata"); err == nil || !strings.Contains(err.Error(), "nije u stablu") {
		t.Errorf("letva koje nema: %v", err)
	}
	if _, err := mapaProfila(filepath.Join(o.koren, "nema-ga"), "primjerovo"); err == nil {
		t.Error("stablo koje ne postoji dalo je mapu snimki")
	}
	if !postoji(filepath.Join(o.koren, "primjerica")) || postoji(filepath.Join(o.koren, "nema-ga")) {
		t.Error("postoji javlja krivo")
	}
	if uBroj(" 12,5 ") != 12.5 || uBroj("nije broj") != 0 {
		t.Error("broj s decimalnim zarezom")
	}
}

// Snimka čiji se sustav nije prepoznao traži odgovor, osim kad je dvojnik
// koji ionako ne ulazi; istaknuta je samo nova.
func TestSnimkaTraziOdgovorINova(t *testing.T) {
	for _, s := range []struct {
		snimka        SnimkaZaPrikaz
		odgovor, nova bool
	}{
		{SnimkaZaPrikaz{Stanje: "nova"}, true, true},
		{SnimkaZaPrikaz{Stanje: "nova", Sustav: "HVRS71"}, false, true},
		{SnimkaZaPrikaz{Stanje: "zamjena"}, true, false},
		{SnimkaZaPrikaz{Stanje: "dvojnik"}, false, false},
	} {
		if s.snimka.TraziOdgovor() != s.odgovor || s.snimka.JeNova() != s.nova {
			t.Errorf("%+v: traži odgovor %v, nova %v", s.snimka, s.snimka.TraziOdgovor(), s.snimka.JeNova())
		}
	}
}

// Račun za HydroView: bez upisanog računa ili ključa čvora javlja se gdje se
// upisuje, lozinka zaključana drugim ključem se ne otključava, a letva bez
// svog računa čita se računom čvora.
func TestRacunZaHydroView(t *testing.T) {
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "hv.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = baza.Close() })
	ctx := context.Background()
	kljuc := posta.Kljuc([]byte("tajna-probnog-cvora"))
	repo := repository.NewHidroViewRepository(baza)
	h := &UvozHandler{}

	if _, _, _, err := h.racunHV(ctx, "primjerovo"); err == nil || !strings.Contains(err.Error(), "Administracija → Telemetrija") {
		t.Errorf("bez računa: %v", err)
	}
	h.SetHidroView(func() *repository.HidroViewRepository { return repo }, func() []byte { return kljuc }, nil)
	if _, _, _, err := h.racunHV(ctx, "primjerovo"); err == nil || !strings.Contains(err.Error(), "nije upisan") {
		t.Errorf("prazan popis računa: %v", err)
	}

	zakljucana := func(k []byte, lozinka string) []byte {
		t.Helper()
		b, err := posta.Zakljucaj(k, lozinka)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, r := range []repository.RacunHidroView{
		{Letva: "", Adresa: "https://hv.primjer.test", Korisnik: "cvor", Lozinka: zakljucana(kljuc, "lozinka-cvora")},
		{Letva: "primjerovo", Adresa: "https://primjerovo.primjer.test", Korisnik: "primjerovo", Lozinka: zakljucana(kljuc, "lozinka-letve")},
		{Letva: "drugovo", Korisnik: "drugovo", Lozinka: zakljucana(posta.Kljuc([]byte("drugi-cvor")), "tuđa")},
	} {
		if err := repo.Spremi(ctx, &r); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []struct {
		letva, korisnik, lozinka, adresa string
	}{
		// adresa spremljena uz račun se ne koristi: povijest se uvijek
		// preuzima sa sustava Hrvatskih voda
		{"primjerovo", "primjerovo", "lozinka-letve", hidroview.ZadanaAdresa},
		{"trecevo", "cvor", "lozinka-cvora", hidroview.ZadanaAdresa},
	} {
		k, l, a, err := h.racunHV(ctx, s.letva)
		if err != nil || k != s.korisnik || l != s.lozinka || a != s.adresa {
			t.Errorf("%s: %q %q %q, %v", s.letva, k, l, a, err)
		}
	}
	if _, _, _, err := h.racunHV(ctx, "drugovo"); err == nil || !strings.Contains(err.Error(), "ne da otključati") {
		t.Errorf("lozinka drugog čvora: %v", err)
	}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := h.racunHV(ctx, "primjerovo"); err == nil {
		t.Error("zatvorena baza dala je račun")
	}
}

// Potvrda uvoza iz posebnog izvora traži pravo kao i pregled: bez prava se
// odbija, a odbijena potvrda ne troši odabir i ne pokreće upis.
func TestPotvrdaPosebnogIzvoraTraziPravo(t *testing.T) {
	o := novaOkolinaVrata(t)
	f, ima := izvori.Nadji("pegelonline")
	if !ima {
		t.Fatal("nema formata pegelonline")
	}
	spremi := func(u *models.User, letva string) string {
		id := "probni-" + u.ID.String() + "-" + letva
		uvoziIzvora.spremi(id, cekaIzvor{format: f.Kod, zadatak: izvori.Zadatak{Koren: o.koren, Sliv: "primjerica", Letva: letva},
			datoteke: []izvori.Datoteka{{Ime: letva + ".json", Sadrzaj: []byte("[]")}}, korisnik: u.ID.String()})
		t.Cleanup(func() { uvoziIzvora.makni(id) })
		return id
	}

	id := spremi(o.ana, "drugovo")
	w := o.zovi(o.h.UpisiIzvor, o.ana, o.anaOvl, "/administracija/uvoz-izvora/upisi", url.Values{"id": {id}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "letva drugovo nije na vašim dionicama") ||
		strings.Contains(w.Body.String(), "data-posao=") {
		t.Errorf("tuđa letva: %d", w.Code)
	}
	if _, ima := uvoziIzvora.uzmi(id, o.ana.ID.String()); !ima {
		t.Error("odbijena potvrda potrošila je odabir")
	}

	id = spremi(o.marko, "primjerovo")
	w = o.zovi(o.h.UpisiIzvor, o.marko, o.markoOvl, "/administracija/uvoz-izvora/upisi", url.Values{"id": {id}})
	if w.Code != http.StatusForbidden {
		t.Errorf("bez ijedne letve: %d", w.Code)
	}
	if _, ima := uvoziIzvora.uzmi(id, o.marko.ID.String()); !ima {
		t.Error("odbijena potvrda potrošila je odabir")
	}
	if d := o.datoteke(t); len(d) != 0 {
		t.Errorf("odbijena potvrda zapisala je %v", d)
	}
}

// Popis postaja godišnjaka koji se ne da pročitati odbija se prije provjere
// prava, da se ne bi pisalo na letve koje nitko nije provjerio.
func TestGodisnjakSNeispravnimPopisomPostaja(t *testing.T) {
	h := &UvozHandler{}
	if err := h.smijeSveLetve(&models.UserPermissions{IsGlobalAdmin: true}, izvoriFormat(t, "godisnjak"),
		izvoriZadatak("9001-primjerovo")); err == nil || !strings.Contains(err.Error(), "šifra=letva") {
		t.Errorf("neispravan popis postaja: %v", err)
	}
}

// Posebni izvori čekaju potvrdu kao i vrata: istekli odabiri se pri
// sljedećem spremanju pospremaju, a tuđi se ne daju uzeti.
func TestOdabirPosebnogIzvoraIstjece(t *testing.T) {
	u := izvoriUTijeku{m: map[string]cekaIzvor{}}
	u.m["staro"] = cekaIzvor{korisnik: "pero", istice: time.Now().Add(-time.Minute)}
	u.spremi("novo", cekaIzvor{format: "pegelonline", korisnik: "pero"})
	if _, ima := u.m["staro"]; ima {
		t.Error("istekli odabir je ostao")
	}
	if c, ima := u.uzmi("novo", "pero"); !ima || c.format != "pegelonline" || !c.istice.After(time.Now()) {
		t.Errorf("svoj odabir: %+v %v", c, ima)
	}
	if _, ima := u.uzmi("novo", "ana"); ima {
		t.Error("tuđi odabir se ne smije uzeti")
	}
}
