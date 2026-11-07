package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gocop/internal/models"
)

// lazniPostar glumi slanje: pamti poruke, a test mu zadaje grešku, kočnicu i kvar
type lazniPostar struct {
	mu         sync.Mutex
	primatelji []PrimateljObavijesti
	gresPrim   error
	greska     func(PrimateljObavijesti) error
	kocnica    chan struct{} // dok se ne zatvori, slanje stoji
	ispad      bool
	poruke     []poslanaPoruka
}

type poslanaPoruka struct {
	za, predmet, tekst string
}

func (l *lazniPostar) PrimateljiObavijesti(context.Context) ([]PrimateljObavijesti, error) {
	l.mu.Lock()
	ispad := l.ispad
	l.mu.Unlock()
	if ispad {
		panic("kvar u slanju")
	}
	return l.primatelji, l.gresPrim
}

func (l *lazniPostar) PosaljiPorukuOPrijavi(_ context.Context, p PrimateljObavijesti, predmet, tekst string) error {
	if l.kocnica != nil {
		<-l.kocnica
	}
	if l.greska != nil {
		if err := l.greska(p); err != nil {
			return err
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.poruke = append(l.poruke, poslanaPoruka{p.Adresa, predmet, tekst})
	return nil
}

func (l *lazniPostar) poslano() []poslanaPoruka {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]poslanaPoruka(nil), l.poruke...)
}

func dvaAdministratora() []PrimateljObavijesti {
	return []PrimateljObavijesti{{ID: "admin-1", Ime: "Iva Ivić", Adresa: "iva@primjer.hr"}, {ID: "admin-2", Ime: "Pero Perić", Adresa: "pero@primjer.hr"}}
}

// cekaj čeka uvjet u slanju koje ide u pozadini
func cekaj(t *testing.T, uvjet func() bool) {
	t.Helper()
	for range 500 {
		if uvjet() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("uvjet nije ispunjen na vrijeme")
}

// zapisana javlja je li ovo računalo zapisalo poruku o prijavi
func zapisana(t *testing.T, s *PrijavaGreskeService, id string) bool {
	t.Helper()
	p, err := s.Get(context.Background(), id)
	if err != nil || p == nil {
		t.Fatalf("%v %v", p, err)
	}
	return p.PorukaPoslana != nil
}

// Nova prijava ide svim administratorima, jednom, s poveznicom i bez slike, a
// ponovljeno slanje iste prijave ne šalje novu poruku
func TestPorukaONovojPrijavi(t *testing.T) {
	saSpremistem(t)
	s, u := servisPrijavaGreske(t)
	l := &lazniPostar{primatelji: dvaAdministratora()}
	defer s.PokreniPoruke(context.Background(), l, func() string { return "https://cop.primjer.hr/" })()
	ctx := context.Background()
	slika := slikaPNG(t, 8, 8)
	p, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "Graf se ne crta.", Stranica: "/karta", NazivStranice: "Karta", Slika: slika, Uloga: "vodočuvar", Izdanje: "0.0.54-alfa"})
	if err != nil {
		t.Fatal(err)
	}
	cekaj(t, func() bool { return zapisana(t, s, p.ID) })
	poruke := l.poslano()
	if len(poruke) != 2 || poruke[0].za != "iva@primjer.hr" || poruke[1].za != "pero@primjer.hr" {
		t.Fatalf("poruke: %+v", poruke)
	}
	m := poruke[0]
	if m.predmet != "goCOP: prijava greške, šalje Mara Marić" {
		t.Errorf("predmet: %q", m.predmet)
	}
	for _, treba := range []string{"Šalje: Mara Marić", "Uloga: vodočuvar", "Stranica: /karta", "Izdanje programa: 0.0.54-alfa", "> Graf se ne crta.", "Uz prijavu je slika zaslona",
		"https://cop.primjer.hr/administracija/prijave-gresaka/" + p.ID} {
		if !strings.Contains(m.tekst, treba) {
			t.Errorf("u poruci nema %q:\n%s", treba, m.tekst)
		}
	}
	if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "Graf se ne crta.", Stranica: "/karta", NazivStranice: "Karta", Slika: slika}); err != nil {
		t.Fatal(err)
	}
	drugi, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "Druga prijava.", Vrsta: models.VrstaPrijavePrijedlog})
	if err != nil {
		t.Fatal(err)
	}
	cekaj(t, func() bool { return zapisana(t, s, drugi.ID) })
	if n := len(l.poslano()); n != 4 {
		t.Errorf("ponovljeno slanje je poslalo poruku: %d", n)
	}
}

// Čvor samo prima prijavu razmjenom, bez poziva Posalji: prijava stigla s drugog
// računala ne prolazi ovuda, pa poruku šalje samo računalo na kojem je nastala.
// Spremanje prijave izravno (kao primjena) ne stavlja ništa u red.
func TestPorukaSamoIzPosalji(t *testing.T) {
	s, _ := servisPrijavaGreske(t)
	l := &lazniPostar{primatelji: dvaAdministratora()}
	defer s.PokreniPoruke(context.Background(), l, func() string { return "" })()
	if err := s.repo.Save(context.Background(), &models.PrijavaGreske{UserID: "a", Vrsta: models.VrstaPrijaveGreska, Opis: "x", Stanje: models.StanjePrijaveNova}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(l.poslano()); n != 0 {
		t.Errorf("poslano: %d", n)
	}
}

// Slanje koje stoji ne usporava slanje prijave, a kad red naraste, višak samo
// ostane bez poruke
func TestPorukaNeKoci(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	l := &lazniPostar{primatelji: dvaAdministratora(), kocnica: make(chan struct{})}
	defer s.PokreniPoruke(context.Background(), l, func() string { return "" })()
	defer close(l.kocnica)
	ctx := context.Background()
	pocetak := time.Now()
	for i := range mjestaURedu + 3 {
		if _, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "prijava " + strings.Repeat("x", i+1)}); err != nil {
			t.Fatal(err)
		}
	}
	if t2 := time.Since(pocetak); t2 > 5*time.Second {
		t.Errorf("slanje prijava je trajalo %s", t2)
	}
	if n := len(l.poslano()); n != 0 {
		t.Errorf("kočnica ne radi: %d", n)
	}
}

// Kvar u slanju ostaje u slanju: prijava je spremljena, a sljedeća se opet šalje
func TestPorukaKvarNeRusi(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	l := &lazniPostar{primatelji: dvaAdministratora(), ispad: true}
	defer s.PokreniPoruke(context.Background(), l, func() string { return "" })()
	ctx := context.Background()
	p, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "prva"})
	if err != nil || p.ID == "" {
		t.Fatalf("%+v %v", p, err)
	}
	cekaj(t, func() bool { return len(s.red) == 0 })
	time.Sleep(20 * time.Millisecond)
	l.mu.Lock()
	l.ispad = false
	l.mu.Unlock()
	druga, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "druga"})
	if err != nil {
		t.Fatal(err)
	}
	cekaj(t, func() bool { return zapisana(t, s, druga.ID) })
	if zapisana(t, s, p.ID) {
		t.Error("prva poruka nije poslana, a zapisana je")
	}
}

// Računalo bez podešene pošte ne šalje i ne bilježi, a na prvoj takvoj greški
// ostale ne pokušava
func TestPorukaBezPoste(t *testing.T) {
	for _, greska := range []error{ErrNemaPosiljatelja, ErrNemaKljucaPrijave, ErrPosiljateljNeispravan, ErrSlanjeZastalo} {
		s, u := servisPrijavaGreske(t)
		var pokusaja int
		l := &lazniPostar{primatelji: dvaAdministratora(), greska: func(PrimateljObavijesti) error { pokusaja++; return greska }}
		zaustavi := s.PokreniPoruke(context.Background(), l, func() string { return "" })
		p, err := s.Posalji(context.Background(), u, UlazPrijaveGreske{Opis: "x"})
		if err != nil {
			t.Fatal(err)
		}
		cekaj(t, func() bool { return len(s.red) == 0 })
		time.Sleep(20 * time.Millisecond)
		zaustavi()
		if pokusaja != 1 || zapisana(t, s, p.ID) {
			t.Errorf("%v: pokušaja %d", greska, pokusaja)
		}
	}
}

// Jedan primatelj koji ne prima ne zadržava drugoga; poruka je zapisana jer je
// netko dobio poruku
func TestPorukaJedanPrimateljNeUspije(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	l := &lazniPostar{primatelji: dvaAdministratora(), greska: func(p PrimateljObavijesti) error {
		if p.ID == "admin-1" {
			return errors.New("poslužitelj nije primio poruku")
		}
		return nil
	}}
	defer s.PokreniPoruke(context.Background(), l, func() string { return "" })()
	p, err := s.Posalji(context.Background(), u, UlazPrijaveGreske{Opis: "x"})
	if err != nil {
		t.Fatal(err)
	}
	cekaj(t, func() bool { return zapisana(t, s, p.ID) })
	if poruke := l.poslano(); len(poruke) != 1 || poruke[0].za != "pero@primjer.hr" {
		t.Errorf("%+v", poruke)
	}
}

// Administrator koji sam šalje prijavu ne dobije poruku o njoj
func TestPorukaNeIdeTkoSalje(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	l := &lazniPostar{primatelji: []PrimateljObavijesti{{ID: u.ID.String(), Adresa: "ja@primjer.hr"}, {ID: "admin-2", Adresa: "pero@primjer.hr"}}}
	defer s.PokreniPoruke(context.Background(), l, func() string { return "" })()
	p, err := s.Posalji(context.Background(), u, UlazPrijaveGreske{Opis: "x"})
	if err != nil {
		t.Fatal(err)
	}
	cekaj(t, func() bool { return zapisana(t, s, p.ID) })
	if poruke := l.poslano(); len(poruke) != 1 || poruke[0].za != "pero@primjer.hr" {
		t.Errorf("%+v", poruke)
	}
}

// Primatelji koji se ne daju pročitati: bez poruke i bez zapisa
func TestPorukaPrimateljiSeNeDajuProcitati(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	l := &lazniPostar{gresPrim: errors.New("kvar baze")}
	zaustavi := s.PokreniPoruke(context.Background(), l, func() string { return "" })
	p, err := s.Posalji(context.Background(), u, UlazPrijaveGreske{Opis: "x"})
	if err != nil {
		t.Fatal(err)
	}
	cekaj(t, func() bool { return len(s.red) == 0 })
	zaustavi()
	if zapisana(t, s, p.ID) {
		t.Error("zapisano")
	}
}

// Granica se računa po poslanim porukama u zadnjih sat vremena, i nakon
// ponovnog pokretanja, jer je zapis u bazi
func TestGranicaPorukaUSatu(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	ctx := context.Background()
	for i := range najvisePorukaOPrijavamaUSatu {
		if err := s.repo.ZabiljeziPoruku(ctx, "stara-"+strings.Repeat("x", i), time.Now().Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	l := &lazniPostar{primatelji: dvaAdministratora()}
	zaustavi := s.PokreniPoruke(ctx, l, func() string { return "" })
	p, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "preko granice"})
	if err != nil || p.ID == "" {
		t.Fatalf("prijava mora proći i preko granice poruka: %v", err)
	}
	cekaj(t, func() bool { return len(s.red) == 0 })
	time.Sleep(20 * time.Millisecond)
	zaustavi()
	if n := len(l.poslano()); n != 0 {
		t.Errorf("poslano preko granice: %d", n)
	}

	// sat kasnije opet ide
	s.sat = func() time.Time { return time.Now().Add(2 * time.Hour) }
	defer s.PokreniPoruke(ctx, l, func() string { return "" })()
	druga, err := s.Posalji(ctx, u, UlazPrijaveGreske{Opis: "kasnije"})
	if err != nil {
		t.Fatal(err)
	}
	cekaj(t, func() bool { return zapisana(t, s, druga.ID) })
}

// Bez pokrenutog slanja (npr. u testovima, naredbama) prijava samo ostaje spremljena
func TestPrijavaBezPokrenutogSlanja(t *testing.T) {
	s, u := servisPrijavaGreske(t)
	if _, err := s.Posalji(context.Background(), u, UlazPrijaveGreske{Opis: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestTekstPoruke(t *testing.T) {
	p := &models.PrijavaGreske{ID: "abc", Ime: "Mara Marić", Vrsta: models.VrstaPrijavePrijedlog, Stranica: "/karta", Opis: strings.Repeat("ž", najviseOpisaUPoruci+5)}
	predmet, tekst := tekstPorukeOPrijavi(p, "")
	if predmet != "goCOP: prijedlog, šalje Mara Marić" {
		t.Errorf("predmet: %q", predmet)
	}
	if !strings.Contains(tekst, "pod Administracija, Prijave grešaka i prijedlozi") || strings.Contains(tekst, "http") || strings.Contains(tekst, "slika zaslona") {
		t.Errorf("bez adrese i slike:\n%s", tekst)
	}
	if strings.Count(tekst, "ž") != najviseOpisaUPoruci || !strings.Contains(tekst, "…") {
		t.Errorf("opis nije skraćen: %d", strings.Count(tekst, "ž"))
	}
	if _, tekst := tekstPorukeOPrijavi(p, " https://cop.primjer.hr/ "); !strings.Contains(tekst, "Otvori prijavu: https://cop.primjer.hr/administracija/prijave-gresaka/abc\n") {
		t.Errorf("poveznica:\n%s", tekst)
	}
}

// Kvar baze pri brojanju poruka znači da poruke nema, a slanje ne pada
func TestPorukaKvarBaze(t *testing.T) {
	s, u, baza := servisPrijavaGreskeSBazom(t)
	l := &lazniPostar{primatelji: dvaAdministratora()}
	p := &models.PrijavaGreske{ID: "abc", UserID: u.ID.String(), Vrsta: models.VrstaPrijaveGreska, Opis: "x"}
	if err := baza.Close(); err != nil {
		t.Fatal(err)
	}
	s.javiPrijavu(context.Background(), l, func() string { return "" }, p)
	if n := len(l.poslano()); n != 0 {
		t.Errorf("poslano: %d", n)
	}
}

// Poruka je poslana, a zapis ne prolazi: to samo ide u dnevnik
func TestPorukaZapisNeProlazi(t *testing.T) {
	s, u, baza := servisPrijavaGreskeSBazom(t)
	if _, err := baza.Exec(`CREATE TRIGGER kvar BEFORE INSERT ON prijave_gresaka_posta BEGIN SELECT RAISE(ABORT, 'kvar'); END`); err != nil {
		t.Fatal(err)
	}
	l := &lazniPostar{primatelji: dvaAdministratora()}
	s.javiPrijavu(context.Background(), l, func() string { return "" }, &models.PrijavaGreske{ID: "abc", UserID: u.ID.String(), Vrsta: models.VrstaPrijaveGreska, Opis: "x"})
	if n := len(l.poslano()); n != 2 {
		t.Errorf("poslano: %d", n)
	}
}

// Otkazano slanje više nikoga ne zove
func TestPorukaOtkazana(t *testing.T) {
	s, u, _ := servisPrijavaGreskeSBazom(t)
	l := &lazniPostar{primatelji: dvaAdministratora()}
	ctx, otkazi := context.WithCancel(context.Background())
	otkazi()
	s.javiPrijavu(ctx, l, func() string { return "" }, &models.PrijavaGreske{ID: "abc", UserID: u.ID.String(), Vrsta: models.VrstaPrijaveGreska, Opis: "x"})
	if n := len(l.poslano()); n != 0 {
		t.Errorf("poslano: %d", n)
	}
}

// Tekst i naziv stranice piše osoba, a poruka ide računom programa: prava poveznica
// i podaci programa stoje iznad, a svaki redak teksta je označen kao tuđi
func TestTekstPorukeOdvajaTekstOsobe(t *testing.T) {
	lazna := "Otvori prijavu: https://lazno.primjer.hr/prijava"
	p := &models.PrijavaGreske{ID: "abc", Ime: "Mara\r\nBcc: x@primjer.hr", Vrsta: models.VrstaPrijaveGreska, Stranica: "/karta?sektor=B",
		NazivStranice: "Hitno: upiši lozinku na https://lazno.primjer.hr",
		Opis:          lazna + strings.Repeat("\n", 200) + "drugi redak\u2028treći redak"}
	predmet, tekst := tekstPorukeOPrijavi(p, "https://cop.primjer.hr")
	if predmet != "goCOP: prijava greške, šalje Mara Bcc: x@primjer.hr" || strings.Contains(predmet, "lazno") {
		t.Errorf("predmet: %q", predmet)
	}
	if !strings.HasPrefix(tekst, "Otvori prijavu: https://cop.primjer.hr/administracija/prijave-gresaka/abc\n") {
		t.Errorf("poveznica mora biti prva:\n%s", tekst)
	}
	if !strings.Contains(tekst, "Šalje: Mara Bcc: x@primjer.hr\n") || !strings.Contains(tekst, "Stranica: /karta\n") {
		t.Errorf("podaci programa:\n%s", tekst)
	}
	_, osoba, ok := strings.Cut(tekst, "\nTekst koji je napisala osoba:\n")
	if !ok {
		t.Fatalf("nema oznake:\n%s", tekst)
	}
	if want := "> " + lazna + "\n>\n> drugi redak\n> treći redak\n"; osoba != want {
		t.Errorf("tekst osobe: %q", osoba)
	}
	if strings.Contains(strings.ReplaceAll(tekst, "> "+lazna, ""), "lazno") {
		t.Errorf("lažna adresa izvan citata:\n%s", tekst)
	}
	p.Stranica = "/https://lazno.primjer.hr/prijava"
	if _, tekst := tekstPorukeOPrijavi(p, ""); strings.Contains(tekst, "Stranica:") {
		t.Errorf("put koji izgleda kao adresa:\n%s", tekst)
	}
}

func TestNazivSlanja(t *testing.T) {
	for brojilo, naziv := range map[string]string{brojiloPIN: "prijava izvana: slanje PIN-a", brojiloObavijesti: "obavijest administratorima: slanje", brojiloPrijava: "prijava greške: slanje poruke"} {
		if got := nazivSlanja(brojilo); got != naziv {
			t.Errorf("%s: %q", brojilo, got)
		}
	}
}
