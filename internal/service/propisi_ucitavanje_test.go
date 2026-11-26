package service

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/propisi"
	"gocop/internal/repository"
)

// Testovi učitavanja rade na izmišljenim stranicama i tekstovima u obliku
// Narodnih novina; pravi propisi se ne dohvaćaju (Odluke (25. 11. 2026.)).

// lazniDohvat vraća izmišljene stranice akata umjesto Narodnih novina
type lazniDohvat struct {
	stranice map[models.IzdanjeNN]string
	greska   error
	pitano   int
}

func (l *lazniDohvat) Akt(_ context.Context, i models.IzdanjeNN) ([]byte, error) {
	l.pitano++
	if l.greska != nil {
		return nil, l.greska
	}
	s, ok := l.stranice[i]
	if !ok {
		return nil, propisi.ErrNemaAkta
	}
	return []byte(s), nil
}

var (
	adminPropisa = &models.UserPermissions{IsGlobalAdmin: true}
	upisivac     = &models.User{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000a1"), Username: "pperic", FullName: "Pero Perić"}
)

// stranicaZakona je izmišljena stranica zakona u obliku Narodnih novina, s
// ELI vezom na izmjenu koja nije učitana
const stranicaZakona = `<!-- izmišljen tekst, nije propis -->
<html><head>
<meta about="https://narodne-novine.nn.hr/eli/sluzbeni/2026/12/201" property="http://data.europa.eu/eli/ontology#date_publication" content="2026-03-02"/>
<meta about="https://narodne-novine.nn.hr/eli/sluzbeni/2026/12/201" property="http://data.europa.eu/eli/ontology#amended_by" resource="https://narodne-novine.nn.hr/eli/sluzbeni/2026/47/808"/>
</head><body>
<div id="sticky" class="title"><h2>Zakon o čuvanju nasipa</h2></div>
<div class="article-column">
<p class="T-9-8">Na temelju izmišljenog ovlaštenja donosi se</p>
<p class="Clanak">Članak 1.</p>
<p class="T-9-8">Zakonom se uređuje čuvanje nasipa.</p>
<p class="Clanak">Članak 2.</p>
<p class="T-9-8">Nasip čuva vodočuvar.</p>
<p class="Clanak">Članak 3.</p>
<p class="T-9-8">Ovaj Zakon stupa na snagu osmoga dana od dana objave u »Narodnim novinama«.</p>
</div>
<table class="detailsTable">
<tr><td><span class='key'>Vrsta dokumenta:</span> Zakon</td></tr>
<tr><td><span class='key'>Donositelj:</span>Hrvatski sabor</td></tr>
</table>
</body></html>`

// stranicaIzmjene je izmišljena izmjena tog zakona: NN je vežu uz zakon
const stranicaIzmjene = `<!-- izmišljen tekst, nije propis -->
<html><head>
<meta about="https://narodne-novine.nn.hr/eli/sluzbeni/2026/47/808" property="http://data.europa.eu/eli/ontology#date_publication" content="2026-06-01"/>
<meta about="https://narodne-novine.nn.hr/eli/sluzbeni/2026/47/808" property="http://data.europa.eu/eli/ontology#amends" resource="https://narodne-novine.nn.hr/eli/sluzbeni/2026/12/201"/>
</head><body>
<div id="sticky" class="title"><h2>Zakon o izmjeni Zakona o čuvanju nasipa</h2></div>
<div class="article-column">
<p class="Clanak">Članak 1.</p>
<p class="T-9-8">U Zakonu o čuvanju nasipa (»Narodne novine«, broj 12/26) u članku 2. riječ »vodočuvar« zamjenjuje se riječju »čuvar«.</p>
<p class="Clanak">Članak 2.</p>
<p class="T-9-8">Ovaj Zakon stupa na snagu osmoga dana od dana objave u »Narodnim novinama«.</p>
</div>
</body></html>`

func servisSDohvatom(t *testing.T, d DohvatAkta) (*PropisiService, *repository.PropisiRepository) {
	t.Helper()
	_, repo, _ := servisPropisa(t)
	return NewPropisiService(repo, d), repo
}

// ulazZakona je broj NN i akta izmišljenog zakona
var ulazZakona = UlazPropisa{BrojNN: "12/2026", BrojAkta: "201", Skupina: "O obrani od poplava"}

// Upis, dopuna i uklanjanje propisa smije samo globalni administrator, i u
// servisu, ne samo na ruti: bez prava ništa se ne pita ni ne sprema
// (Odluke (25. 11. 2026.))
func TestUcitavanjeSamoGlobalniAdministrator(t *testing.T) {
	dohvat := &lazniDohvat{stranice: map[models.IzdanjeNN]string{nnZakona: stranicaZakona}}
	svc, repo := servisSDohvatom(t, dohvat)
	zakon := zakonNasipa()
	spremiPropise(t, repo, zakon)
	ctx := context.Background()
	for _, perms := range []*models.UserPermissions{nil, {}} {
		greske := map[string]error{}
		_, greske["procitaj"] = svc.Procitaj(ctx, perms, ulazZakona)
		_, greske["spremi"] = svc.Spremi(ctx, perms, upisivac, &PregledPropisa{Propis: *zakon}, OdabirPropisa{})
		_, greske["procitaj izmjenu"] = svc.ProcitajIzmjenu(ctx, perms, zakon.Adresa, UlazPropisa{BrojNN: "47/2026", BrojAkta: "808"})
		_, greske["spremi izmjenu"] = svc.SpremiIzmjenu(ctx, perms, upisivac, &PregledIzmjene{Propis: zakon, Izmjena: *izmjenaNasipa(zakon)}, nil)
		_, greske["ukloni"] = svc.Ukloni(ctx, perms, upisivac, zakon.Adresa, "krivo")
		_, greske["ukloni izmjenu"] = svc.UkloniIzmjenu(ctx, perms, upisivac, zakon.Adresa, nnIzmjene.ID(), "krivo")
		for ime, err := range greske {
			var g greskaOvlasti
			if !errors.As(err, &g) {
				t.Errorf("%s bez prava (%v): %v", ime, perms, err)
			}
		}
	}
	if dohvat.pitano != 0 {
		t.Errorf("bez prava se pitalo Narodne novine %d puta", dohvat.pitano)
	}
	if p, err := repo.Get(ctx, zakon.ID); err != nil || p == nil {
		t.Errorf("propis je nestao: %v", err)
	}
}

// Akt iz Narodnih novina dolazi kroz dohvat, a ne s adrese koju je netko
// upisao: iz stranice se čitaju naziv, donositelj, datum objave, veze i
// tekst, a pregled kaže koliko je teksta i koje izmjene NN navode
func TestProcitajIzNNKrozLazniDohvat(t *testing.T) {
	dohvat := &lazniDohvat{stranice: map[models.IzdanjeNN]string{nnZakona: stranicaZakona, nnIzmjene: stranicaIzmjene}}
	svc, repo := servisSDohvatom(t, dohvat)
	ctx := context.Background()
	pr, err := svc.Procitaj(ctx, adminPropisa, UlazPropisa{Poveznica: "https://narodne-novine.nn.hr/clanci/sluzbeni/2026_03_12_201.html", Skupina: " "})
	if err != nil {
		t.Fatal(err)
	}
	p := pr.Propis
	if p.ID != nnZakona.ID() || p.Naziv != "Zakon o čuvanju nasipa" || p.Donositelj != "Hrvatski sabor" || p.DatumObjave != "2026-03-02" ||
		p.StupaNaSnagu != "2026-03-10" || p.Izvor != models.IzvorPropisaNN || p.Citac != propisi.InacicaCitaca || p.Adresa != "zakon-o-cuvanju-nasipa" || p.Skupina != "Ostali" {
		t.Errorf("propis: %+v", p)
	}
	if pr.Opseg.Clanci != 3 || pr.Raspon != "1.–3." || pr.Postoji != nil || pr.Objava() != "2. 3. 2026." || pr.Stupa() != "10. 3. 2026." || len(pr.Tekst()) != len(p.Dijelovi) {
		t.Errorf("pregled: %+v", pr)
	}
	if len(pr.Neucitane) != 1 || pr.Neucitane[0] != nnIzmjene || !strings.HasPrefix(pr.OpisTeksta(), "oko ") {
		t.Errorf("neučitane %v, tekst %q", pr.Neucitane, pr.OpisTeksta())
	}
	if _, err := svc.Spremi(ctx, adminPropisa, upisivac, pr, OdabirPropisa{Skupina: "O obrani od poplava"}); err != nil {
		t.Fatal(err)
	}
	spremljen, err := repo.Get(ctx, p.ID)
	if err != nil || spremljen == nil || spremljen.Ucitao != "Pero Perić" || spremljen.UcitaoID != upisivac.ID.String() || spremljen.Skupina != "O obrani od poplava" {
		t.Fatalf("spremljen: %+v %v", spremljen, err)
	}

	iz, err := svc.ProcitajIzmjenu(ctx, adminPropisa, "zakon-o-cuvanju-nasipa", UlazPropisa{BrojNN: "47/26", BrojAkta: "808"})
	if err != nil {
		t.Fatal(err)
	}
	if iz.NeVezeSe || iz.Izmjena.ID != p.ID+"-izmjena-"+nnIzmjene.ID() || iz.Izmjena.StupaNaSnagu != "2026-06-09" || iz.Stupa != "9. 6. 2026." ||
		iz.Objava() != "1. 6. 2026." || len(iz.Tekst()) != 2 || iz.Odredbi != 1 || iz.Prepoznato != 1 || iz.Nepovezano != 0 {
		t.Errorf("izmjena: %+v", iz)
	}
	if len(iz.Retci) != 2 || len(iz.Retci[0].Ciljevi) != 1 || iz.Retci[0].Ciljevi[0].Naziv != "Članak 2." || len(iz.Clanci) != 3 {
		t.Errorf("odredbe: %+v, izbor %v", iz.Retci, iz.Clanci)
	}
	if dohvat.pitano != 2 {
		t.Errorf("Narodne novine pitane %d puta", dohvat.pitano)
	}
}

// Zalijepljen tekst traži broj NN i akta (isti akt na dva računala je isti
// zapis) i naziv; donositelj i datum nisu obavezni (Odluke (25. 11. 2026.),
// O9)
func TestProcitajIzTekstaTraziBrojeveINaziv(t *testing.T) {
	dohvat := &lazniDohvat{}
	svc, _ := servisSDohvatom(t, dohvat)
	ctx := context.Background()
	tekst := "\ufeffČlanak 1.\r\nZakonom se uređuje čuvanje nasipa.\r\nČlanak 2.\r\nOvaj Zakon stupa na snagu 1. siječnja 2027."
	for _, x := range []struct {
		ulaz UlazPropisa
		sto  GreskaPropisa
	}{
		{UlazPropisa{IzTeksta: true, Tekst: tekst, Naziv: "Zakon"}, porukaKriviBroj},
		{UlazPropisa{IzTeksta: true, BrojNN: "12/2026", BrojAkta: "201", Naziv: "Zakon"}, porukaBezTeksta},
		{UlazPropisa{IzTeksta: true, BrojNN: "12/2026", BrojAkta: "201", Tekst: tekst, Naziv: "  "}, porukaBezNaziva},
	} {
		if _, err := svc.Procitaj(ctx, adminPropisa, x.ulaz); !errors.Is(err, x.sto) {
			t.Errorf("%+v: %v", x.ulaz, err)
		}
	}
	pr, err := svc.Procitaj(ctx, adminPropisa, UlazPropisa{IzTeksta: true, BrojNN: "NN 12/26", BrojAkta: "akt 201", Tekst: tekst,
		Naziv: " Zakon o čuvanju nasipa ", Donositelj: "Hrvatski sabor", Datum: "2026-03-02", Skupina: strings.Repeat("š", 70)})
	if err != nil {
		t.Fatal(err)
	}
	p := pr.Propis
	if p.ID != nnZakona.ID() || p.Naziv != "Zakon o čuvanju nasipa" || p.Izvor != models.IzvorPropisaTekst || p.DatumObjave != "2026-03-02" ||
		p.StupaNaSnagu != "2027-01-01" || pr.Opseg.Clanci != 2 || len([]rune(p.Skupina)) != 60 || p.ELI != "" {
		t.Errorf("iz teksta: %+v", p)
	}
	pr, err = svc.Procitaj(ctx, adminPropisa, UlazPropisa{IzTeksta: true, BrojNN: "12/2026", BrojAkta: "201", Tekst: "Članak 1.\nTekst.", Naziv: "Zakon", Datum: "2. 3. 2026."})
	if err != nil || pr.Propis.DatumObjave != "" || pr.Stupa() != "" {
		t.Errorf("datum koji nije ISO se ne pamti: %+v %v", pr, err)
	}
	if dohvat.pitano != 0 {
		t.Error("zalijepljen tekst je pitao Narodne novine")
	}
}

// presretanje je greška veze kad mreža podmeće svoj certifikat
func presretanje() error {
	return fmt.Errorf("Get: %w", x509.UnknownAuthorityError{})
}

type isteklo struct{}

func (isteklo) Error() string   { return "i/o timeout" }
func (isteklo) Timeout() bool   { return true }
func (isteklo) Temporary() bool { return true }

var _ net.Error = isteklo{}

// Svaka greška dohvata daje rečenicu iz plana: što se nije dogodilo i što
// sad učiniti; uzrok ostaje u grešci za dnevnik
func TestGreskeDohvataDajuRecenice(t *testing.T) {
	ctx := context.Background()
	for _, x := range []struct {
		greska error
		sto    string
	}{
		{propisi.ErrNemaAkta, "Narodne novine nemaju akt 201 u NN 12/2026. Provjerite brojeve ili zalijepite poveznicu."},
		{propisi.ErrNemaTijelaAkta, string(porukaNemaTijela)},
		{propisi.ErrNijeHTML, string(porukaNemaTijela)},
		{propisi.ErrPrevelikaStranica, string(porukaPrevelikaStr)},
		{context.DeadlineExceeded, string(porukaRok)},
		{fmt.Errorf("Get: %w", isteklo{}), string(porukaRok)},
		{presretanje(), string(porukaPresretanje)},
		{errors.New("dial tcp: connection refused"), string(porukaNemaVeze)},
	} {
		svc, _ := servisSDohvatom(t, &lazniDohvat{greska: x.greska})
		_, err := svc.Procitaj(ctx, adminPropisa, ulazZakona)
		var g GreskaPropisa
		if !errors.As(err, &g) || string(g) != x.sto || !strings.Contains(err.Error(), x.greska.Error()) {
			t.Errorf("%v: %v", x.greska, err)
		}
	}
	for _, x := range []struct {
		ulaz UlazPropisa
		sto  GreskaPropisa
	}{
		{UlazPropisa{BrojNN: "84-2010", BrojAkta: "1"}, porukaKriviBroj},
		{UlazPropisa{Poveznica: "https://narodne-novine.nn.hr.primjer.hr/clanci/sluzbeni/2026_03_12_201.html"}, porukaNijeNN},
		{UlazPropisa{BrojNN: "12/2026", BrojAkta: "202", Poveznica: "https://narodne-novine.nn.hr/eli/sluzbeni/2026/12/201"}, porukaRazliciti},
	} {
		svc, _ := servisSDohvatom(t, &lazniDohvat{})
		if _, err := svc.Procitaj(ctx, adminPropisa, x.ulaz); !errors.Is(err, x.sto) {
			t.Errorf("%+v: %v", x.ulaz, err)
		}
	}
	// stranica bez stupca s tekstom akta i računalo bez dohvata
	svc, _ := servisSDohvatom(t, &lazniDohvat{stranice: map[models.IzdanjeNN]string{nnZakona: "<html><body><p>Naslovnica</p></body></html>"}})
	if _, err := svc.Procitaj(ctx, adminPropisa, ulazZakona); !errors.Is(err, porukaNemaTijela) {
		t.Errorf("bez tijela akta: %v", err)
	}
	svc, _ = servisSDohvatom(t, nil)
	if _, err := svc.Procitaj(ctx, adminPropisa, ulazZakona); !errors.Is(err, porukaNemaVeze) {
		t.Errorf("bez dohvata: %v", err)
	}
}

// Adresa iz naziva koju već ima drugi propis dobije godinu izdanja, a naziv
// bez slova daje oznaku zapisa; „ucitaj” je riječ programa
func TestAdresaZauzetaDobijeGodinu(t *testing.T) {
	svc, repo := servisSDohvatom(t, nil)
	ctx := context.Background()
	stari := zakonNasipa()
	stari.ID, stari.NN = "nn-2001-1-1", models.IzdanjeNN{Godina: 2001, Broj: 1, Akt: 1}
	spremiPropise(t, repo, stari)
	tekst := "Članak 1.\nZakonom se uređuje čuvanje nasipa."
	for naziv, adresa := range map[string]string{"Zakon o čuvanju nasipa": "zakon-o-cuvanju-nasipa-2026", "»«": nnZakona.ID(), "Učitaj": "ucitaj-2026"} {
		pr, err := svc.Procitaj(ctx, adminPropisa, UlazPropisa{IzTeksta: true, BrojNN: "12/2026", BrojAkta: "201", Tekst: tekst, Naziv: naziv})
		if err != nil || pr.Propis.Adresa != adresa {
			t.Errorf("%q: %+v %v", naziv, pr, err)
		}
	}
}

// pregledTeksta je pročitan izmišljen zakon iz zalijepljenog teksta
func pregledTeksta(t *testing.T, svc *PropisiService, nn, akt, naziv, tekst string) *PregledPropisa {
	t.Helper()
	pr, err := svc.Procitaj(context.Background(), adminPropisa, UlazPropisa{IzTeksta: true, BrojNN: nn, BrojAkta: akt, Tekst: tekst, Naziv: naziv})
	if err != nil {
		t.Fatal(err)
	}
	return pr
}

// Spremanje odbija adresu koju koristi program, krivog oblika ili onu koju
// već ima drugi propis; ništa se tada ne sprema
func TestSpremiOdbijaAdresu(t *testing.T) {
	svc, repo := servisSDohvatom(t, nil)
	ctx := context.Background()
	spremiPropise(t, repo, zakonNasipa())
	pr := pregledTeksta(t, svc, "5/2026", "101", "Plan obrane Primjerova", "Članak 1.\nPlanom se uređuje obrana.")
	for adresa, sto := range map[string]string{
		"ucitaj":                 "Adresu „ucitaj” koristi program. Upišite drugu.",
		"Plan Obrane":            string(porukaKrivaAdresa),
		"zakon-o-cuvanju-nasipa": "Adresu „zakon-o-cuvanju-nasipa” već ima propis „Zakon o čuvanju nasipa”. Upišite drugu.",
		nnZakona.ID():            "Adresu „" + nnZakona.ID() + "” već ima propis „Zakon o čuvanju nasipa”. Upišite drugu.",
	} {
		if _, err := svc.Spremi(ctx, adminPropisa, upisivac, pr, OdabirPropisa{Adresa: adresa}); err == nil || err.Error() != sto {
			t.Errorf("%q: %v", adresa, err)
		}
	}
	if p, err := repo.Get(ctx, pr.Propis.ID); err != nil || p != nil {
		t.Fatalf("odbijen propis je spremljen: %+v %v", p, err)
	}
	p, err := svc.Spremi(ctx, adminPropisa, upisivac, pr, OdabirPropisa{Adresa: " plan-primjerova "})
	if err != nil || p.Adresa != "plan-primjerova" || p.Skupina != "Ostali" {
		t.Fatalf("%+v %v", p, err)
	}
}

// Tekst bez ijednog članka, točke ili priloga nije propis, pa se ne sprema
func TestSpremiBezIjednogClanka(t *testing.T) {
	svc, repo := servisSDohvatom(t, nil)
	pr := pregledTeksta(t, svc, "5/2026", "101", "Nešto", "Ovo nije propis nego obavijest.")
	if _, err := svc.Spremi(context.Background(), adminPropisa, upisivac, pr, OdabirPropisa{}); !errors.Is(err, porukaBezClanka) {
		t.Fatalf("%v", err)
	}
	if p, err := repo.Get(context.Background(), pr.Propis.ID); err != nil || p != nil {
		t.Errorf("spremljeno: %+v %v", p, err)
	}
}

// Isti akt učitan ponovno zadrži adresu, datum prvog učitavanja i izmjene,
// a provjera kaže da je već učitan
func TestPonovnoUcitavanjeZadrzavaAdresu(t *testing.T) {
	svc, repo := servisSDohvatom(t, nil)
	ctx := context.Background()
	zakon := zakonNasipa()
	zakon.CreatedAt = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	spremiPropise(t, repo, zakon)
	spremiIzmjene(t, repo, izmjenaNasipa(zakon))
	pr := pregledTeksta(t, svc, "12/2026", "201", "Zakon o čuvanju nasipa (pročišćen)", "Članak 1.\nNovi tekst.")
	if pr.Postoji == nil || pr.Propis.Adresa != zakon.Adresa {
		t.Fatalf("pregled: %+v", pr)
	}
	p, err := svc.Spremi(ctx, adminPropisa, upisivac, pr, OdabirPropisa{Adresa: "nova-adresa"})
	if err != nil || p.Adresa != zakon.Adresa || !p.CreatedAt.Equal(zakon.CreatedAt) {
		t.Fatalf("%+v %v", p, err)
	}
	if izmjene, err := repo.Izmjene(ctx, zakon.ID); err != nil || len(izmjene) != 1 {
		t.Errorf("izmjene: %d %v", len(izmjene), err)
	}
}

// Administrator odredbu poveže s člankom sam: kriv članak se odbija, „-”
// kaže da odredba ne mijenja članke, a izbor ispravlja i prepoznat cilj.
// Pregled na čekanju ostaje kakav je bio.
func TestRucniCiljIzmjene(t *testing.T) {
	svc, repo := servisSDohvatom(t, nil)
	ctx := context.Background()
	zakon := zakonNasipa()
	spremiPropise(t, repo, zakon)
	pr := &PregledIzmjene{Propis: zakon, Izmjena: *izmjenaNasipa(zakon)}
	if _, err := svc.SpremiIzmjenu(ctx, adminPropisa, upisivac, pr, map[int]string{7: "clanak-999"}); !errors.Is(err, porukaCiljNijeUPropisu) {
		t.Fatalf("kriv članak: %v", err)
	}
	iz, err := svc.SpremiIzmjenu(ctx, adminPropisa, upisivac, pr, map[int]string{7: " clanak-119 ", 1: NeMijenjaClanke, 2: "", 9: "clanak-4", 10: "clanak-1"})
	if err != nil {
		t.Fatal(err)
	}
	// odredba same izmjene i stupanje vezani uz članak mijenjaju taj članak
	for _, u := range iz.Upute[8:10] {
		if !u.MijenjaPropis() || u.Vrsta != models.UputaNeprepoznata {
			t.Errorf("vlastita ili stupanje uz članak: %+v", u)
		}
	}
	if pr.Izmjena.Upute[8].Vrsta != models.UputaVlastita {
		t.Error("izbor je promijenio vrstu u pregledu na čekanju")
	}
	if u := iz.Upute[6]; !u.Rucno || !reflect.DeepEqual(u.Cilj.Clanci, []string{"clanak-119"}) {
		t.Errorf("ručno povezana: %+v", u)
	}
	if u := iz.Upute[0]; !u.Rucno || u.Cilj.Clanci != nil {
		t.Errorf("ne mijenja članke: %+v", u)
	}
	if iz.Upute[1].Rucno || pr.Izmjena.Upute[6].Rucno || len(pr.Izmjena.Upute[0].Cilj.Clanci) != 1 {
		t.Error("izbor je promijenio pregled na čekanju")
	}
	st, err := svc.Propis(ctx, zakon.Adresa)
	if err != nil {
		t.Fatal(err)
	}
	if st.Nepovezane != 0 || len(st.Oznake["clanak-119"]) != 1 || st.Oznake["clanak-119"][0].Natpis != "mijenjan NN 47/26" {
		t.Errorf("oznake: %d nepovezanih, %+v", st.Nepovezane, st.Oznake["clanak-119"])
	}
	for clanak, tekst := range map[string]string{"clanak-4": iz.Upute[8].Tekst, "clanak-1": iz.Upute[9].Tekst} {
		if !imaOdredbu(st.Oznake[clanak], tekst) {
			t.Errorf("uz %s nema odredbe %q: %+v", clanak, tekst, st.Oznake[clanak])
		}
	}
	if iz.Ucitao != "Pero Perić" {
		t.Errorf("učitao %q", iz.Ucitao)
	}
	// ponovno spremanje iste izmjene zadrži datum prvog dodavanja
	drugi, err := svc.SpremiIzmjenu(ctx, adminPropisa, upisivac, pr, nil)
	if err != nil || !drugi.CreatedAt.Equal(iz.CreatedAt) {
		t.Errorf("%v %v", drugi, err)
	}
}

// imaOdredbu javlja stoji li odredba s tim tekstom u kojoj oznaci dijela
func imaOdredbu(oznake []OznakaIzmjene, tekst string) bool {
	for _, o := range oznake {
		for _, od := range o.Odredbe {
			if od.Tekst == tekst {
				return true
			}
		}
	}
	return false
}

// Neprepoznata odredba ne priječi spremanje (O10): izmjena se sprema, uz
// članak nema oznake, a naslovnica broji odredbu koju treba povezati
func TestNeprepoznataSeSpremaBezOznake(t *testing.T) {
	svc, repo := servisSDohvatom(t, nil)
	ctx := context.Background()
	zakon := zakonNasipa()
	spremiPropise(t, repo, zakon)
	izmjena := izmjenaNasipa(zakon)
	pr := &PregledIzmjene{Propis: zakon, Izmjena: *izmjena}
	pr.osvjezi()
	if pr.Odredbi != 9 || pr.Nepovezano != 1 || pr.Prepoznato != 8 {
		t.Errorf("zbroj: %d od %d, %d nepovezanih", pr.Prepoznato, pr.Odredbi, pr.Nepovezano)
	}
	if _, err := svc.SpremiIzmjenu(ctx, adminPropisa, upisivac, pr, nil); err != nil {
		t.Fatal(err)
	}
	st, err := svc.Propis(ctx, zakon.Adresa)
	if err != nil {
		t.Fatal(err)
	}
	if st.Nepovezane != 1 {
		t.Errorf("nepovezanih %d", st.Nepovezane)
	}
	// propis uklonjen dok je pregled čekao
	if _, err := svc.Ukloni(ctx, adminPropisa, upisivac, zakon.Adresa, "krivo učitan"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SpremiIzmjenu(ctx, adminPropisa, upisivac, pr, nil); !errors.Is(err, ErrNemaPropisa) {
		t.Errorf("izmjena uklonjenog propisa: %v", err)
	}
}

// Izmjena koja ne spominje propis, a ni Narodne novine je ne vežu uz njega,
// dobije upozorenje; ona koja ga spominje u uvodu ne dobije
func TestIzmjenaKojaSeNeVeze(t *testing.T) {
	svc, repo := servisSDohvatom(t, nil)
	ctx := context.Background()
	spremiPropise(t, repo, zakonNasipa())
	ulaz := func(tekst string) UlazPropisa {
		return UlazPropisa{IzTeksta: true, BrojNN: "47/2026", BrojAkta: "808", Naziv: "Zakon o izmjeni", Tekst: tekst}
	}
	tudja, err := svc.ProcitajIzmjenu(ctx, adminPropisa, "zakon-o-cuvanju-nasipa", ulaz("Članak 1.\nU Zakonu o čuvanju šuma (»Narodne novine«, broj 1/26) u članku 2. riječ »šuma« zamjenjuje se riječju »gaj«."))
	if err != nil || !tudja.NeVezeSe {
		t.Errorf("tuđa izmjena: %+v %v", tudja, err)
	}
	nasa, err := svc.ProcitajIzmjenu(ctx, adminPropisa, nnZakona.ID(), ulaz("Članak 1.\nU Zakonu o čuvanju nasipa (»Narodne novine«, broj 12/26) u članku 2. riječ »nasip« zamjenjuje se riječju »bedem«.\nČlanak 2.\nU članku 300. riječ »x« briše se."))
	if err != nil || nasa.NeVezeSe || nasa.Postoji != nil {
		t.Fatalf("naša izmjena: %+v %v", nasa, err)
	}
	if len(nasa.Upozorenja) == 0 || !strings.Contains(nasa.Upozorenja[len(nasa.Upozorenja)-1].Sto, "300.") {
		t.Errorf("upozorenja: %+v", nasa.Upozorenja)
	}
	if _, err := svc.ProcitajIzmjenu(ctx, adminPropisa, "nema-ga", ulaz("Članak 1.")); !errors.Is(err, ErrNemaPropisa) {
		t.Errorf("izmjena propisa kojeg nema: %v", err)
	}
	if _, err := svc.ProcitajIzmjenu(ctx, adminPropisa, "zakon-o-cuvanju-nasipa", UlazPropisa{IzTeksta: true, BrojNN: "47/2026", BrojAkta: "808"}); !errors.Is(err, porukaBezTeksta) {
		t.Errorf("bez teksta: %v", err)
	}
}

// Uklanjanje traži razlog; propis i izmjena nestaju, a razlog, osoba i
// vrijeme ostaju u zapisu
func TestUklanjanjeTraziRazlog(t *testing.T) {
	_, repo, baza := servisPropisa(t)
	svc := NewPropisiService(repo, nil)
	sat := time.Date(2026, 11, 25, 9, 30, 0, 0, time.FixedZone("CET", 3600))
	svc.sada = func() time.Time { return sat }
	ctx := context.Background()
	zakon := zakonNasipa()
	spremiPropise(t, repo, zakon)
	spremiIzmjene(t, repo, izmjenaNasipa(zakon), drugaIzmjena(zakon))
	if _, err := svc.Ukloni(ctx, adminPropisa, upisivac, zakon.Adresa, "  "); !errors.Is(err, ErrBezRazloga) {
		t.Errorf("propis bez razloga: %v", err)
	}
	if _, err := svc.UkloniIzmjenu(ctx, adminPropisa, upisivac, zakon.Adresa, nnDruge.ID(), ""); !errors.Is(err, ErrBezRazloga) {
		t.Errorf("izmjena bez razloga: %v", err)
	}
	if _, err := svc.UkloniIzmjenu(ctx, adminPropisa, upisivac, zakon.Adresa, "nn-1-1-1", "krivo"); !errors.Is(err, ErrNemaIzmjene) {
		t.Errorf("izmjena koje nema: %v", err)
	}
	if _, err := svc.UkloniIzmjenu(ctx, adminPropisa, upisivac, "nema-ga", nnDruge.ID(), "krivo"); !errors.Is(err, ErrNemaPropisa) {
		t.Errorf("izmjena propisa kojeg nema: %v", err)
	}
	iz, err := svc.UkloniIzmjenu(ctx, adminPropisa, upisivac, zakon.Adresa, nnDruge.ID(), "dvaput učitana")
	if err != nil || iz.NN != nnDruge {
		t.Fatalf("%+v %v", iz, err)
	}
	if ostale, err := repo.Izmjene(ctx, zakon.ID); err != nil || len(ostale) != 1 {
		t.Errorf("ostale izmjene: %d %v", len(ostale), err)
	}
	if _, err := svc.Ukloni(ctx, adminPropisa, upisivac, "nema-ga", "krivo"); !errors.Is(err, ErrNemaPropisa) {
		t.Errorf("propis kojeg nema: %v", err)
	}
	p, err := svc.Ukloni(ctx, adminPropisa, upisivac, zakon.Adresa, " krivo učitan ")
	if err != nil || p.Naziv != zakon.Naziv {
		t.Fatalf("%+v %v", p, err)
	}
	if ostao, err := repo.Get(ctx, zakon.ID); err != nil || ostao != nil {
		t.Errorf("propis ostao: %+v %v", ostao, err)
	}
	knjiga := ledger.New(baza, "ured")
	for _, z := range []struct{ entitet, id, razlog string }{
		{repository.EntityPropisiIzmjene, iz.ID, "dvaput učitana"},
		{repository.EntityPropisi, zakon.ID, "krivo učitan"},
	} {
		u := uklanjanjeIzKnjige(t, knjiga, z.entitet, z.id)
		if u == nil || u.Razlog != z.razlog || u.Tko != upisivac.FullName || !u.Kad.Equal(sat) || u.Kad.Location() != time.UTC {
			t.Errorf("zapis o uklanjanju %s: %+v", z.entitet, u)
		}
	}
}

// uklanjanjeIzKnjige čita zapis o uklanjanju iz zadnje verzije u knjizi
func uklanjanjeIzKnjige(t *testing.T, knjiga *ledger.Recorder, entitet, id string) *models.Uklanjanje {
	t.Helper()
	v, err := knjiga.Latest(context.Background(), entitet, id)
	if err != nil {
		t.Fatal(err)
	}
	var x struct {
		Uklonjeno *models.Uklanjanje `json:"uklonjeno"`
	}
	if err := json.Unmarshal(v.Payload, &x); err != nil || !v.Archived {
		t.Fatalf("%s/%s: arhivirano %v, %v", entitet, id, v.Archived, err)
	}
	return x.Uklonjeno
}

// Kvar baze u učitavanju je greška, a ne prazan pregled ili tiho spremanje
func TestUcitavanjeKvarBaze(t *testing.T) {
	svc, repo, baza := servisPropisa(t)
	ctx := context.Background()
	zakon := zakonNasipa()
	spremiPropise(t, repo, zakon)
	spremiIzmjene(t, repo, izmjenaNasipa(zakon))
	pr := pregledTeksta(t, svc, "12/2026", "201", "Zakon o čuvanju nasipa", "Članak 1.\nTekst.")
	novi := pregledTeksta(t, svc, "5/2026", "101", "Plan obrane Primjerova", "Članak 1.\nTekst.")
	izmjena := &PregledIzmjene{Propis: zakon, Izmjena: *izmjenaNasipa(zakon)}
	ulazIzmjene := UlazPropisa{IzTeksta: true, BrojNN: "47/2026", BrojAkta: "808", Naziv: "Izmjena", Tekst: "Članak 1.\nU članku 2. riječ »a« briše se."}
	baza.Close()
	greske := map[string]error{}
	_, greske["procitaj"] = svc.Procitaj(ctx, adminPropisa, UlazPropisa{IzTeksta: true, BrojNN: "5/2026", BrojAkta: "101", Tekst: "Članak 1.", Naziv: "Plan"})
	_, greske["spremi postojeći"] = svc.Spremi(ctx, adminPropisa, upisivac, pr, OdabirPropisa{})
	_, greske["spremi novi"] = svc.Spremi(ctx, adminPropisa, upisivac, novi, OdabirPropisa{})
	_, greske["procitaj izmjenu"] = svc.ProcitajIzmjenu(ctx, adminPropisa, zakon.Adresa, ulazIzmjene)
	_, greske["spremi izmjenu"] = svc.SpremiIzmjenu(ctx, adminPropisa, upisivac, izmjena, nil)
	_, greske["ukloni izmjenu"] = svc.UkloniIzmjenu(ctx, adminPropisa, upisivac, zakon.Adresa, nnIzmjene.ID(), "krivo")
	_, greske["ukloni"] = svc.Ukloni(ctx, adminPropisa, upisivac, zakon.Adresa, "krivo")
	for ime, err := range greske {
		var g GreskaPropisa
		if err == nil || errors.As(err, &g) {
			t.Errorf("%s uz zatvorenu bazu: %v", ime, err)
		}
	}
	if _, err := svc.adresaZauzeta(ctx, "zakon", "x"); err == nil {
		t.Error("provjera adrese uz zatvorenu bazu")
	}
	if _, err := svc.slobodnaAdresa(ctx, models.Propis{Naziv: "Zakon"}); err == nil {
		t.Error("slobodna adresa uz zatvorenu bazu")
	}
}

// Kvar baze nakon čitanja: postojeći propis se pročita, a izmjene ne; i
// prevelik propis dobije rečenicu
func TestUcitavanjeKvarIzmjenaIPrevelikPropis(t *testing.T) {
	svc, repo, baza := servisPropisa(t)
	ctx := context.Background()
	zakon := zakonNasipa()
	spremiPropise(t, repo, zakon)
	if _, err := baza.Exec(`ALTER TABLE propisi_izmjene RENAME TO propisi_izmjene_stare`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Procitaj(ctx, adminPropisa, UlazPropisa{IzTeksta: true, BrojNN: "12/2026", BrojAkta: "201", Tekst: "Članak 1.", Naziv: "Zakon"}); err == nil {
		t.Error("izmjene se nisu mogle pročitati, a pregled je složen")
	}
	if _, err := svc.SpremiIzmjenu(ctx, adminPropisa, upisivac, &PregledIzmjene{Propis: zakon, Izmjena: *izmjenaNasipa(zakon)}, nil); err == nil {
		t.Error("izmjena spremljena bez tablice")
	}
	if _, err := svc.ProcitajIzmjenu(ctx, adminPropisa, zakon.Adresa, UlazPropisa{IzTeksta: true, BrojNN: "47/2026", BrojAkta: "808", Naziv: "I", Tekst: "Članak 1."}); err == nil {
		t.Error("izmjena pročitana bez tablice")
	}
	velik := pregledTeksta(t, svc, "5/2026", "101", "Plan", "Članak 1.\n"+strings.Repeat("Tekst ", 10))
	velik.Propis.Dijelovi[0].Stavci[0].Tekst = strings.Repeat("<>&", repository.NajveciPropis/12) // u JSON-u šest puta dulje
	if _, err := svc.Spremi(ctx, adminPropisa, upisivac, velik, OdabirPropisa{}); !errors.Is(err, porukaPrevelikPropis) {
		t.Errorf("prevelik propis: %v", err)
	}
	velikaIzmjena := *izmjenaNasipa(zakon)
	velikaIzmjena.Dijelovi[0].Stavci[0].Tekst = velik.Propis.Dijelovi[0].Stavci[0].Tekst
	if _, err := baza.Exec(`ALTER TABLE propisi_izmjene_stare RENAME TO propisi_izmjene`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SpremiIzmjenu(ctx, adminPropisa, upisivac, &PregledIzmjene{Propis: zakon, Izmjena: velikaIzmjena}, nil); !errors.Is(err, porukaPrevelikPropis) {
		t.Errorf("prevelika izmjena: %v", err)
	}
	mali := pregledTeksta(t, svc, "7/2026", "3", "Plan malen", "Članak 1.\nTekst.")
	if _, err := baza.Exec(`CREATE TRIGGER bez_propisa BEFORE INSERT ON propisi BEGIN SELECT RAISE(FAIL, 'kvar'); END`); err != nil {
		t.Fatal(err)
	}
	var g GreskaPropisa
	if _, err := svc.Spremi(ctx, adminPropisa, upisivac, mali, OdabirPropisa{}); err == nil || errors.As(err, &g) {
		t.Errorf("kvar upisa: %v", err)
	}
}

// Opis duljine teksta i raspon brojeva za karticu provjere
func TestOpisTekstaIRaspon(t *testing.T) {
	for n, opis := range map[int]string{1: "oko 1 znak", 3: "oko 3 znaka", 999: "oko 999 znakova", 44321: "oko 44.000 znakova", 1234567: "oko 1.235.000 znakova"} {
		if got := (&PregledPropisa{Znakova: n}).OpisTeksta(); got != opis {
			t.Errorf("%d: %q", n, got)
		}
	}
	jedan := []models.DioPropisa{{Vrsta: models.DioUvod}, {Vrsta: models.DioTocka, Broj: "I."}}
	if got := rasponBrojeva(jedan); got != "I." {
		t.Errorf("jedna točka: %q", got)
	}
	pr := &PregledPropisa{Podjela: propisi.Podjela{Upozorenja: []propisi.Upozorenje{{Vrsta: propisi.UpozorenjeNeprepoznato}, {Vrsta: propisi.UpozorenjePismo}}}}
	if pr.Neprepoznatih() != 1 {
		t.Errorf("neprepoznatih %d", pr.Neprepoznatih())
	}
	if got := NaziviSkupina(); len(got) != 4 || got[3] != "Ostali" {
		t.Errorf("skupine %v", got)
	}
}
