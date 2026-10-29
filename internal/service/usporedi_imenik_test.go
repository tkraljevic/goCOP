package service_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/posta"
	"gocop/internal/service"
)

// laziAdresar je adresar tvrtke (EWS ResolveNames) na 127.0.0.1: pamti
// upite redom kojim stižu, a zna i javiti grešku za pojedine upite ili, od
// zadanog upita nadalje, odbiti prijavu kao Exchange kad zaključa račun
type laziAdresar struct {
	srv      *httptest.Server
	kontakti []posta.Kontakt
	pokvaren map[string]string // upit → poruka greške kojom adresar odgovori
	odbijOd  int    // od kojeg upita (1, 2, …) se prijava odbija; 0 = nikad

	mu    sync.Mutex
	upiti []string
}

var upitAdresaru = regexp.MustCompile(`<m:UnresolvedEntry>([^<]*)</m:UnresolvedEntry>`)

func pokreniLaziAdresar(t *testing.T, kontakti ...posta.Kontakt) *laziAdresar {
	t.Helper()
	a := &laziAdresar{kontakti: kontakti}
	a.srv = httptest.NewTLSServer(http.HandlerFunc(a.odgovori))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *laziAdresar) odgovori(w http.ResponseWriter, r *http.Request) {
	tijelo, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	upit := ""
	if x := upitAdresaru.FindSubmatch(tijelo); x != nil {
		upit = html.UnescapeString(string(x[1]))
	}
	a.mu.Lock()
	a.upiti = append(a.upiti, upit)
	n := len(a.upiti)
	a.mu.Unlock()
	if a.odbijOd > 0 && n >= a.odbijOd {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	if poruka, ok := a.pokvaren[upit]; ok {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><faultcode>s:Server</faultcode><faultstring>%s</faultstring></s:Fault></s:Body></s:Envelope>`, html.EscapeString(poruka))
		return
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><m:ResolveNamesResponse xmlns:m="m" xmlns:t="t"><m:ResponseMessages>`)
	var nadjeni []posta.Kontakt
	for _, k := range a.kontakti {
		if strings.Contains(strings.ToLower(k.Ime+" "+k.Email), strings.ToLower(upit)) {
			nadjeni = append(nadjeni, k)
		}
	}
	if len(nadjeni) == 0 {
		b.WriteString(`<m:ResolveNamesResponseMessage ResponseClass="Error"><m:MessageText>No results were found.</m:MessageText><m:ResponseCode>ErrorNameResolutionNoResults</m:ResponseCode></m:ResolveNamesResponseMessage>`)
	} else {
		b.WriteString(`<m:ResolveNamesResponseMessage ResponseClass="Success"><m:ResponseCode>NoError</m:ResponseCode><m:ResolutionSet>`)
		for _, k := range nadjeni {
			fmt.Fprintf(&b, `<t:Resolution><t:Mailbox><t:Name>%s</t:Name><t:EmailAddress>%s</t:EmailAddress></t:Mailbox><t:Contact><t:DisplayName>%s</t:DisplayName><t:PhoneNumbers><t:Entry Key="BusinessPhone">%s</t:Entry><t:Entry Key="MobilePhone">%s</t:Entry></t:PhoneNumbers><t:JobTitle>%s</t:JobTitle></t:Contact></t:Resolution>`,
				html.EscapeString(k.Ime), html.EscapeString(k.Email), html.EscapeString(k.Ime), html.EscapeString(k.Telefon), html.EscapeString(k.Mobitel), html.EscapeString(k.Funkcija))
		}
		b.WriteString(`</m:ResolutionSet></m:ResolveNamesResponseMessage>`)
	}
	b.WriteString(`</m:ResponseMessages></m:ResolveNamesResponse></s:Body></s:Envelope>`)
	io.WriteString(w, b.String())
}

// Postavke usmjeravaju EWS na lažni adresar; Basic umjesto NTLM-a kao u probnom EWS-u
func (a *laziAdresar) Postavke() posta.Postavke {
	pool := x509.NewCertPool()
	pool.AddCert(a.srv.Certificate())
	return posta.Postavke{Nacin: posta.NacinEWS, Posluzitelj: a.srv.URL + "/EWS/Exchange.asmx",
		TLS: &tls.Config{RootCAs: pool}, DopustiBasic: true, Istek: 10 * time.Second}
}

func (a *laziAdresar) postavljeniUpiti() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Sorted(slices.Values(a.upiti))
}

// dojave bilježi pozive napretka; radnici ih javljaju usporedno
type dojave struct {
	mu     sync.Mutex
	imena  []string
	gotovo []int
	ukupno []int
}

func (d *dojave) javi(sto string, gotovo, ukupno int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.imena = append(d.imena, sto)
	d.gotovo = append(d.gotovo, gotovo)
	d.ukupno = append(d.ukupno, ukupno)
}

func (d *dojave) broj() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.imena)
}

// okolinaImenika: djelatnici sektora B (jedan bez imena, jedan bez
// adrese), jedan djelatnik sektora A, adresar tvrtke i spremljena lozinka
// sandučića prijavljenog administratora o.sef
type okolinaImenika struct {
	*okolinaOvlasti
	adresar    *laziAdresar
	djelatnici map[string]*models.User
}

// djelatnik otvara račun koji je već bio prijavljen (ListUsers "active")
// i daje mu dužnost u sektoru
func (o *okolinaImenika) djelatnik(t *testing.T, korisnik, ime, email, mobitel, telefon, sektor string) {
	t.Helper()
	o.racun(t, korisnik, "lozinka-"+korisnik, email, false, true)
	if _, err := o.db.Exec(`UPDATE users SET full_name = ?, mobile_phone = ?, phone = ?, last_login_at = datetime('now') WHERE username = ?`,
		ime, mobitel, telefon, korisnik); err != nil {
		t.Fatal(err)
	}
	u := o.ucitaj(t, korisnik)
	if err := o.users.AddDuty(o.admin, service.AddDutyRequest{UserID: u.ID, Role: models.RoleOperator, SectorID: sektorP(sektor)}); err != nil {
		t.Fatal(err)
	}
	o.djelatnici[korisnik] = o.ucitaj(t, korisnik)
}

func (o *okolinaImenika) ucitaj(t *testing.T, korisnik string) *models.User {
	t.Helper()
	u, err := o.repo.GetUserByUsername(korisnik)
	if err != nil || u == nil {
		t.Fatalf("račun %s: %v", korisnik, err)
	}
	return u
}

func novaOkolinaImenika(t *testing.T) *okolinaImenika {
	t.Helper()
	ctx := context.Background()
	o := &okolinaImenika{okolinaOvlasti: novaOkolinaOvlasti(t), djelatnici: map[string]*models.User{}}
	// prije nego što je poslužitelj zadan, lozinka se sprema bez provjere
	if _, err := o.akti.SpremiRacunPoste(ctx, o.sef, `voda.int\sef`, "Domenska-Lozinka-1"); err != nil {
		t.Fatal(err)
	}
	o.djelatnik(t, "bezimeni", " ", "bezimeni@voda.hr", "", "", "B")
	o.djelatnik(t, "ana", "Ana Anić", "ana.anic@voda.hr", "098 111 2222", "", "B")
	o.djelatnik(t, "ivo", "Ivo Ivić", "ivo@voda.hr", "091 000 0000", "031 100 100", "B")
	o.djelatnik(t, "marko", "Marko Markić", "", "", "", "B")
	o.djelatnik(t, "petra", "Petra Perić", "petra@voda.hr", "", "", "B")
	o.djelatnik(t, "zoran", "Zoran Zorić", "zoran@voda.hr", "", "", "B")
	o.djelatnik(t, "luka", "Luka Lukić", "luka@voda.hr", "099 777 8888", "", "A")
	o.adresar = pokreniLaziAdresar(t,
		posta.Kontakt{Ime: "Ana Anić", Email: "Ana.Anic@voda.hr", Mobitel: "+385 98 111 2222", Telefon: "031 200 300", Funkcija: "Vodočuvar"},
		posta.Kontakt{Ime: "Ivo Ivić", Email: "ivo.ivic@voda.hr", Mobitel: "092 333 4444"},
		posta.Kontakt{Ime: "Marko Markić", Email: "marko.markic@voda.hr", Telefon: "031 555 666"},
		posta.Kontakt{Ime: "Petra Perić", Email: "petra.peric@voda.hr"},
		posta.Kontakt{Ime: "Petra Perić", Email: "petra.peric2@voda.hr"},
		posta.Kontakt{Ime: "Luka Lukić", Email: "luka@voda.hr", Mobitel: "099 777 8888"},
	)
	o.akti.SetPosta(o.adresar.Postavke())
	return o
}

// Adresar se pita adresom pa imenom, a polja se uspoređuju s djelatnikom:
// ista adresa i broj u drugom zapisu su isto, prazno u goCOP-u je novo,
// prazno u adresaru je nema. Djelatnik bez imena se preskače, ali ulazi u
// ukupan broj koji javlja napredak.
func TestUsporediImenikSektora(t *testing.T) {
	o := novaOkolinaImenika(t)
	var d dojave
	rez, err := o.akti.UsporediImenik(context.Background(), o.admin, o.sef, "B", d.javi)
	if err != nil {
		t.Fatal(err)
	}
	ana := posta.Kontakt{Ime: "Ana Anić", Email: "ana.anic@voda.hr", Mobitel: "+385 98 111 2222", Telefon: "031 200 300", Funkcija: "Vodočuvar"}
	ivo := posta.Kontakt{Ime: "Ivo Ivić", Email: "ivo.ivic@voda.hr", Mobitel: "092 333 4444"}
	marko := posta.Kontakt{Ime: "Marko Markić", Email: "marko.markic@voda.hr", Telefon: "031 555 666"}
	polje := func(p, naziv, gocop, exch, stanje string) service.RazlikaKontakta {
		return service.RazlikaKontakta{Polje: p, Naziv: naziv, GoCOP: gocop, Exchange: exch, Stanje: stanje}
	}
	ocekivano := []struct {
		korisnik  string
		kontakt   *posta.Kontakt
		kandidati int
		polja     []service.RazlikaKontakta
		razlike   []service.RazlikaKontakta
	}{
		{"ana", &ana, 1, []service.RazlikaKontakta{
			polje("email", "E-pošta", "ana.anic@voda.hr", "ana.anic@voda.hr", "isto"),
			polje("mobile_phone", "Mobitel", "098 111 2222", "+385 98 111 2222", "isto"),
			polje("phone", "Fiksni telefon", "", "031 200 300", "novo"),
		}, []service.RazlikaKontakta{polje("phone", "Fiksni telefon", "", "031 200 300", "novo")}},
		{"ivo", &ivo, 1, []service.RazlikaKontakta{
			polje("email", "E-pošta", "ivo@voda.hr", "ivo.ivic@voda.hr", "drugacije"),
			polje("mobile_phone", "Mobitel", "091 000 0000", "092 333 4444", "drugacije"),
			polje("phone", "Fiksni telefon", "031 100 100", "", "nema"),
		}, []service.RazlikaKontakta{
			polje("email", "E-pošta", "ivo@voda.hr", "ivo.ivic@voda.hr", "drugacije"),
			polje("mobile_phone", "Mobitel", "091 000 0000", "092 333 4444", "drugacije"),
		}},
		{"marko", &marko, 1, []service.RazlikaKontakta{
			polje("email", "E-pošta", "", "marko.markic@voda.hr", "novo"),
			polje("mobile_phone", "Mobitel", "", "", "nema"),
			polje("phone", "Fiksni telefon", "", "031 555 666", "novo"),
		}, []service.RazlikaKontakta{
			polje("email", "E-pošta", "", "marko.markic@voda.hr", "novo"),
			polje("phone", "Fiksni telefon", "", "031 555 666", "novo"),
		}},
		// dvije osobe istog imena: ne pogađa se koja je
		{"petra", nil, 2, nil, nil},
		{"zoran", nil, 0, nil, nil},
	}
	if len(rez) != len(ocekivano) {
		t.Fatalf("usporedba: %+v", rez)
	}
	for i, w := range ocekivano {
		g := rez[i]
		if !reflect.DeepEqual(g.User, *o.djelatnici[w.korisnik]) {
			t.Errorf("%d: djelatnik %q, a čeka se %q", i, g.User.Username, w.korisnik)
		}
		if !reflect.DeepEqual(g.Kontakt, w.kontakt) || g.Kandidati != w.kandidati ||
			!reflect.DeepEqual(g.Polja, w.polja) || !reflect.DeepEqual(g.Razlike, w.razlike) {
			t.Errorf("%s: kontakt %+v, kandidata %d, polja %+v, razlike %+v", w.korisnik, g.Kontakt, g.Kandidati, g.Polja, g.Razlike)
		}
	}
	// adresa prva; ime tek kad za adresu nema nikoga ili adrese nema
	ocekivaniUpiti := []string{"Ivo Ivić", "Marko Markić", "Petra Perić", "Zoran Zorić", "ana.anic@voda.hr", "ivo@voda.hr", "petra@voda.hr", "zoran@voda.hr"}
	if u := o.adresar.postavljeniUpiti(); !slices.Equal(u, ocekivaniUpiti) {
		t.Errorf("upiti adresaru: %q", u)
	}
	if !slices.Equal(slices.Sorted(slices.Values(d.gotovo)), []int{1, 2, 3, 4, 5}) || !slices.Equal(d.ukupno, []int{6, 6, 6, 6, 6}) {
		t.Errorf("napredak: gotovo %v, ukupno %v", d.gotovo, d.ukupno)
	}
	if im := slices.Sorted(slices.Values(d.imena)); !slices.Equal(im, []string{"Ana Anić", "Ivo Ivić", "Marko Markić", "Petra Perić", "Zoran Zorić"}) {
		t.Errorf("napredak javlja: %q", im)
	}
}

// Sektorski administrator uspoređuje svoj sektor, a napredak smije izostati
func TestUsporediImenikSektorskiAdministrator(t *testing.T) {
	o := novaOkolinaImenika(t)
	perms := &models.UserPermissions{AdminSectors: map[string]bool{"A": true}}
	rez, err := o.akti.UsporediImenik(context.Background(), perms, o.sef, "A", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rez) != 1 || rez[0].User.Username != "luka" || rez[0].Kontakt == nil || len(rez[0].Razlike) != 0 {
		t.Fatalf("sektor A: %+v", rez)
	}
	if u := o.adresar.postavljeniUpiti(); !slices.Equal(u, []string{"luka@voda.hr"}) {
		t.Errorf("upiti adresaru: %q", u)
	}
}

// Ovlasti i sektor provjeravaju se prije prijave na poslužitelj, a
// lozinka sandučića prije čitanja djelatnika; ni jedno ne pita adresar
func TestUsporediImenikOdbija(t *testing.T) {
	o := novaOkolinaImenika(t)
	ctx := context.Background()
	bezSektora := "odaberite sektor: imenik se uspoređuje po sektoru, jer svi odjednom preopterete poslužitelj e-pošte"
	uprava := o.admin.User
	for _, s := range []struct {
		ime    string
		perms  *models.UserPermissions
		u      *models.User
		sektor string
		greska error
		tekst  string
	}{
		{"bez ovlasti", nil, o.sef, "B", service.ErrUnauthorized, ""},
		{"bez ovlasti i sektora", nil, o.sef, "", service.ErrUnauthorized, ""},
		{"nije administrator", &models.UserPermissions{AdminSectors: map[string]bool{}}, o.sef, "B", service.ErrUnauthorized, ""},
		{"bez sektora", o.admin, o.sef, "", nil, bezSektora},
		{"prazan sektor", o.admin, nil, "  ", nil, bezSektora},
		{"bez korisnika", o.admin, nil, "B", service.ErrUnauthorized, ""},
		{"bez lozinke sandučića", o.admin, &uprava, "B", service.ErrNemaLozinkePoste, ""},
	} {
		var d dojave
		rez, err := o.akti.UsporediImenik(ctx, s.perms, s.u, s.sektor, d.javi)
		switch {
		case rez != nil || err == nil:
			t.Errorf("%s: %+v, %v", s.ime, rez, err)
		case s.greska != nil && !errors.Is(err, s.greska):
			t.Errorf("%s: %v", s.ime, err)
		case s.tekst != "" && err.Error() != s.tekst:
			t.Errorf("%s: %q", s.ime, err.Error())
		case d.broj() != 0:
			t.Errorf("%s: napredak %v", s.ime, d.imena)
		}
	}
	if u := o.adresar.postavljeniUpiti(); len(u) != 0 {
		t.Errorf("upiti adresaru: %q", u)
	}
}

func TestUsporediImenikGreskaPopisa(t *testing.T) {
	o := novaOkolinaImenika(t)
	if _, err := o.db.Exec(`ALTER TABLE duties RENAME TO duties_skrivene`); err != nil {
		t.Fatal(err)
	}
	rez, err := o.akti.UsporediImenik(context.Background(), o.admin, o.sef, "B", nil)
	if rez != nil || err == nil || !strings.HasPrefix(err.Error(), "greška pri listanju korisnika: ") {
		t.Fatalf("%+v, %v", rez, err)
	}
}

// Greška adresara za jednog djelatnika ne prekida prolaz, ali poništi
// cijeli rezultat: ostali se ipak pitaju i javljaju. Kad upit adresom
// javi grešku, imenom se više ne pita.
func TestUsporediImenikGreskaAdresara(t *testing.T) {
	o := novaOkolinaImenika(t)
	o.adresar.pokvaren = map[string]string{"ivo@voda.hr": "Adresar nije dostupan"}
	var d dojave
	rez, err := o.akti.UsporediImenik(context.Background(), o.admin, o.sef, "B", d.javi)
	if rez != nil || err == nil || err.Error() != "Exchange: Adresar nije dostupan" {
		t.Fatalf("%+v, %v", rez, err)
	}
	ocekivaniUpiti := []string{"Marko Markić", "Petra Perić", "Zoran Zorić", "ana.anic@voda.hr", "ivo@voda.hr", "petra@voda.hr", "zoran@voda.hr"}
	if u := o.adresar.postavljeniUpiti(); !slices.Equal(u, ocekivaniUpiti) {
		t.Errorf("upiti adresaru: %q", u)
	}
	if !slices.Equal(slices.Sorted(slices.Values(d.gotovo)), []int{1, 2, 3, 4, 5}) {
		t.Errorf("napredak: %v", d.gotovo)
	}
}

// Od više grešaka adresara vraća se prva. Ana je prva na popisu, a Zoran
// zadnji, pa ga radnici uz takt pitaju barem pola sekunde poslije Ane.
func TestUsporediImenikPrvaGreska(t *testing.T) {
	o := novaOkolinaImenika(t)
	o.adresar.pokvaren = map[string]string{
		"ana.anic@voda.hr": "Adresar nije dostupan",
		"zoran@voda.hr":    "Adresar je preopterećen",
	}
	rez, err := o.akti.UsporediImenik(context.Background(), o.admin, o.sef, "B", nil)
	if rez != nil || err == nil || err.Error() != "Exchange: Adresar nije dostupan" {
		t.Fatalf("%+v, %v", rez, err)
	}
	if u := o.adresar.postavljeniUpiti(); !slices.Contains(u, "zoran@voda.hr") {
		t.Errorf("upiti adresaru: %q", u)
	}
}

// Odbijena prva prijava je kriva lozinka; prolaz staje i ostali se ne pitaju
func TestUsporediImenikKrivaLozinka(t *testing.T) {
	o := novaOkolinaImenika(t)
	o.adresar.odbijOd = 1
	var d dojave
	rez, err := o.akti.UsporediImenik(context.Background(), o.admin, o.sef, "B", d.javi)
	if rez != nil || !errors.Is(err, posta.ErrPrijava) {
		t.Fatalf("%+v, %v", rez, err)
	}
	// Drugi radnik čeka takt dok prvi pita; nakon prekida ne smije uzeti
	// svoj takt i javiti djelatnika kojeg nije ni pitao, jer prekinut upit
	// ne stigne do adresara. Spor prvi upit smije pustiti i drugi, ali i
	// taj tada ide adresaru.
	u := o.adresar.postavljeniUpiti()
	if n := d.broj(); n != len(u) || n < 1 || n > 2 {
		t.Errorf("napredak %q, upiti adresaru %q", d.imena, u)
	}
}

// Prijava odbijena nakon uspjelih upita: poslužitelj je stao primati
// prijave, pa poruka ne krivi lozinku. Dva radnika uzimaju Anu (jedan
// upit) i Ivu (dva upita) bilo kojim redom, pa treći upit pada uvijek
// nakon točno jednog obrađenog djelatnika.
func TestUsporediImenikPosluziteljStao(t *testing.T) {
	o := novaOkolinaImenika(t)
	o.adresar.odbijOd = 3
	rez, err := o.akti.UsporediImenik(context.Background(), o.admin, o.sef, "B", nil)
	tekst := "poslužitelj e-pošte prestao je primati upite nakon 1 djelatnika: previše prijava u kratkom vremenu. Lozinka je vjerojatno ispravna — pokušajte usporedbu po sektoru"
	if rez != nil || err == nil || err.Error() != tekst || errors.Is(err, posta.ErrPrijava) {
		t.Fatalf("%+v, %v", rez, err)
	}
}
