package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Popis postavki čita se iz strukture: svako polje Config je jedna postavka,
// s odjeljkom, objašnjenjem i vrstom; vrijednost se čita i upisuje po njemu.
func TestKljuceviIzStrukture(t *testing.T) {
	kljucevi := Kljucevi()
	broj := 0
	tc := reflect.TypeFor[Config]()
	for i := range tc.NumField() {
		if f := tc.Field(i); f.Type.Kind() == reflect.Struct {
			broj += f.Type.NumField()
		} else {
			broj++
		}
	}
	if len(kljucevi) != broj {
		t.Fatalf("%d postavki, a polja je %d", len(kljucevi), broj)
	}
	k, ok := NadjiKljuc("sync.auto_sync")
	if !ok || k.Oznaka() != "[sync] auto_sync" || k.Tip.Kind() != reflect.String || !strings.Contains(k.Opis, "sinkronizira") {
		t.Errorf("sync.auto_sync: %+v %v", k, ok)
	}
	addr, _ := NadjiKljuc("addr")
	if addr.Oznaka() != "addr" || addr.Odjeljak != "" {
		t.Errorf("addr: %+v", addr)
	}
	centar, _ := NadjiKljuc("kontakt.centar")
	if !strings.Contains(centar.OpisOdjeljka, "Centar obrane") {
		t.Errorf("opis odjeljka [kontakt]: %q", centar.OpisOdjeljka)
	}
	if _, ok := NadjiKljuc("nema.takve"); ok {
		t.Error("nepostojeća postavka nađena")
	}
	c := Default()
	c.Postavi(k, "1h")
	if c.Sync.AutoSync != "1h" || c.Vrijednost(k) != "1h" {
		t.Errorf("upis: %q", c.Sync.AutoSync)
	}
	// polje bez oznake toml zove se kao polje, a skriveno se ne čita
	type probna struct {
		Vidljivo string
		skriveno string //nolint:unused // provjerava se da ga popis preskače
		Izbaceno string `toml:"-"`
	}
	if got := kljuceviIz(reflect.TypeFor[probna](), "", "", nil); len(got) != 1 || got[0].Ime != "Vidljivo" {
		t.Errorf("polja bez oznake: %+v", got)
	}
	// odjeljak u odjeljku: [a.b]
	type ugnijezdjena struct {
		A struct {
			B struct {
				C int `toml:"c"`
			} `toml:"b"`
		} `toml:"a"`
	}
	if got := kljuceviIz(reflect.TypeFor[ugnijezdjena](), "", "", nil); len(got) != 1 || got[0].Puni() != "a.b.c" {
		t.Errorf("ugniježđeni odjeljak: %+v", got)
	}
	if imeKljuca("ime s razmakom") != `"ime s razmakom"` || imeKljuca("auto_sync") != "auto_sync" {
		t.Error("ime ključa u navodnicima")
	}
}

// Ručni komentari, nepoznati ključevi i komentar u retku ostaju; mijenja se
// samo vrijednost.
func TestIzmjenaNaMjestu(t *testing.T) {
	stari := "# moj komentar o čvoru\naddr = ':80'\nnepoznato = 'iz novijeg izdanja'\n\n[sync]\n" +
		"# razmak koji sam izabrao\nauto_sync = '5m'   # dogovor s uredom\nbootstrap = [\n  'cop-osijek.com',\n]\n\n[buducnost]\nnesto = 1\n"
	novi, cfg, err := Izmijeni([]byte(stari), map[string]any{
		"sync.auto_sync": "1h",
		"sync.bootstrap": []string{"cop-osijek.com", "cop.voda.hr"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(novi)
	for _, mora := range []string{"# moj komentar o čvoru", "nepoznato = 'iz novijeg izdanja'", "# razmak koji sam izabrao",
		"auto_sync = '1h'   # dogovor s uredom", "bootstrap = ['cop-osijek.com', 'cop.voda.hr']", "[buducnost]\nnesto = 1"} {
		if !strings.Contains(s, mora) {
			t.Errorf("nema %q:\n%s", mora, s)
		}
	}
	if cfg.Sync.AutoSync != "1h" || len(cfg.Sync.Bootstrap) != 2 || cfg.Addr != ":80" {
		t.Errorf("pročitano: %+v", cfg.Sync)
	}
}

// Postavka koje u datoteci nema dodaje se u svoj odjeljak s objašnjenjem;
// odjeljak kojeg nema dodaje se na kraj, s vlastitim objašnjenjem.
func TestIzmjenaDodajeKljuc(t *testing.T) {
	stari := "addr = ':8080'\n\n[sync]\nsve = false\n"
	novi, cfg, err := Izmijeni([]byte(stari), map[string]any{
		"sync.auto_sync":         "15m",
		"kontakt.centar_telefon": "031 000 111",
		"javna_adresa":           "https://cop.primjer.hr",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(novi)
	if !strings.Contains(s, "[sync]\nsve = false\n# Koliko se često čvor sam sinkronizira") || !strings.Contains(s, "auto_sync = '15m'") {
		t.Errorf("ključ u postojećem odjeljku:\n%s", s)
	}
	if !strings.Contains(s, "# Centar obrane od poplava ovog čvora") || !strings.Contains(s, "[kontakt]\n# Telefon na koji") {
		t.Errorf("novi odjeljak s objašnjenjem:\n%s", s)
	}
	if strings.Index(s, "javna_adresa") > strings.Index(s, "[sync]") {
		t.Errorf("ključ s vrha datoteke upisan je u odjeljak:\n%s", s)
	}
	if cfg.Support.CenterPhone != "031 000 111" || cfg.JavnaAdresa != "https://cop.primjer.hr" || cfg.Sync.AutoSync != "15m" {
		t.Errorf("pročitano: %+v %q", cfg.Support, cfg.JavnaAdresa)
	}
}

// Ključ s vrha u datoteci koja na vrhu nema nijednog ide ispred prvog
// odjeljka i komentara iznad njega; datoteka bez odjeljaka dobije ga na kraj.
func TestIzmjenaVrhDatoteke(t *testing.T) {
	novi, _, err := Izmijeni([]byte("# uvod\n\n# o mreži\n[sync]\nsve = true\n"), map[string]any{"javna_adresa": "https://a.primjer.hr"})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(novi); !strings.HasPrefix(s, "# uvod\n\n# Adresa na kojoj") || !strings.Contains(s, "'https://a.primjer.hr'\n\n# o mreži\n[sync]") {
		t.Errorf("vrh datoteke:\n%s", s)
	}
	novi, cfg, err := Izmijeni([]byte("# samo komentar"), map[string]any{"addr": ":9090"})
	if err != nil || cfg.Addr != ":9090" || !strings.HasPrefix(string(novi), "# samo komentar\n") {
		t.Errorf("datoteka bez odjeljaka i bez kraja retka: %v\n%s", err, novi)
	}
	novi, cfg, err = Izmijeni([]byte("[sync]\nsve = true"), map[string]any{"sync.auto_sync": "1m"})
	if err != nil || cfg.Sync.AutoSync != "1m" || !cfg.Sync.All || !strings.HasPrefix(string(novi), "[sync]\nsve = true\n# ") {
		t.Errorf("zadnji ključ bez kraja retka: %v\n%s", err, novi)
	}
}

// Datoteka s Windowsa (CRLF) dobiva nove retke istim krajem retka.
func TestIzmjenaCRLF(t *testing.T) {
	novi, cfg, err := Izmijeni([]byte("[sync]\r\nsve = false\r\n"), map[string]any{"sync.auto_sync": "2m", "sync.sve": true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(string(novi), "\r\n", ""), "\n") || cfg.Sync.AutoSync != "2m" || !cfg.Sync.All {
		t.Errorf("CRLF: %q", novi)
	}
}

// Ključ zapisan s točkom mijenja se na mjestu; ono što program ne zna
// sigurno izmijeniti (tablica u retku) javlja grešku, a ne kvari datoteku.
func TestIzmjenaPosebniOblici(t *testing.T) {
	novi, cfg, err := Izmijeni([]byte("sync.auto_sync = '5m'\n"), map[string]any{"sync.auto_sync": "30m"})
	if err != nil || cfg.Sync.AutoSync != "30m" || string(novi) != "sync.auto_sync = '30m'\n" {
		t.Errorf("ključ s točkom: %v %q", err, novi)
	}
	if _, _, err := Izmijeni([]byte("kontakt = { centar = 'COP Primjer' }\n"), map[string]any{"kontakt.centar_telefon": "1"}); err == nil {
		t.Error("tablica u retku: izmjena je prošla, a ne da se sigurno napraviti")
	}
	if _, _, err := Izmijeni([]byte("addr = :80 nije toml"), map[string]any{"addr": ":81"}); err == nil {
		t.Error("neispravna stara datoteka: nema greške")
	}
	if _, _, err := Izmijeni([]byte(""), map[string]any{"sync.exchange_port": "4710"}); err == nil {
		t.Error("vrijednost krive vrste: nema greške")
	}
	novi, cfg, err = Izmijeni([]byte("[[niz]]\nx = 1\n"), map[string]any{"node.name": "Laptop Ivana Ivića"})
	if err != nil || cfg.Node.Name != "Laptop Ivana Ivića" || !strings.Contains(string(novi), "[node]") {
		t.Errorf("uz niz tablica: %v\n%s", err, novi)
	}
}

// Primjer i izmjena primjera daju isto što piše prvo pokretanje, uz
// promijenjenu vrijednost.
func TestIzmjenaPrimjera(t *testing.T) {
	c := Default()
	c.Node.ID = "ivic-laptop"
	primjer, err := Primjer(c)
	if err != nil {
		t.Fatal(err)
	}
	novi, cfg, err := Izmijeni(primjer, map[string]any{"vodostaji.povijest_mjeseci": 12})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Node.ID != "ivic-laptop" || cfg.Readings.HistoryMonths != 12 || !strings.Contains(string(novi), "povijest_mjeseci = 12\n") {
		t.Errorf("izmjena primjera: %+v", cfg.Readings)
	}
	if strings.Replace(string(novi), "\npovijest_mjeseci = 12\n", "\npovijest_mjeseci = 0\n", 1) != string(primjer) {
		t.Error("izmjena primjera promijenila je i nešto drugo")
	}
}

func TestIstaVrijednost(t *testing.T) {
	if !IstaVrijednost([]string(nil), []string{}) || IstaVrijednost("a", "b") || !IstaVrijednost(17, 17) {
		t.Error("usporedba vrijednosti")
	}
	if IstaVrijednost(func() {}, func() {}) {
		t.Error("vrijednost koja se ne da zapisati ne smije biti ista")
	}
}

func TestProvjeriPostavke(t *testing.T) {
	if g := Provjeri(Default()); len(g) != 0 {
		t.Fatalf("zadane postavke imaju grešaka: %v", g)
	}
	c := Default()
	c.JavnaAdresa = "cop.primjer.hr"
	c.Sync.AutoSync = "pet minuta"
	c.Sync.Bootstrap = []string{"a.primjer.hr", "dva čvora"}
	c.Sync.ExchangePort = 70000
	c.Readings.HistoryMonths = -1
	c.Karta.Plocice = "https://plocice.primjer.hr/karta.png"
	c.Karta.NajviseZ = 25
	g := Provjeri(c)
	for _, k := range []string{"javna_adresa", "sync.auto_sync", "sync.bootstrap", "sync.exchange_port", "vodostaji.povijest_mjeseci", "karta.plocice", "karta.najvise_z"} {
		if g[k] == "" {
			t.Errorf("%s: nema poruke (%v)", k, g)
		}
	}
	c = Default()
	c.JavnaAdresa, c.Sync.AutoSync, c.Karta.Plocice = "https://cop.primjer.hr", "0", ""
	if g := Provjeri(c); len(g) != 0 {
		t.Errorf("ispravno odbijeno: %v", g)
	}
}

// Spremanje: stara datoteka ide u kopiju, nova je na mjestu s istim
// dopuštenjima, privremenih datoteka nema, a čuva se zadnjih pet kopija.
func TestZapisiSKopijom(t *testing.T) {
	dir := t.TempDir()
	put := filepath.Join(dir, FileName)
	if err := os.WriteFile(put, []byte("addr = ':80'\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, PrimjerFileName), []byte("# primjer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pocetak := time.Date(2026, 10, 12, 15, 30, 0, 0, time.Local)
	for i := range 7 {
		kopija, err := Zapisi(put, []byte("addr = ':81'\n"), pocetak.Add(time.Duration(i)*time.Minute))
		if err != nil || kopija == "" {
			t.Fatalf("%d. spremanje: %q %v", i, kopija, err)
		}
	}
	kopije := Kopije(put)
	if len(kopije) != ZadrziKopija || kopije[0].Ime != "gocop.toml.20261012-153600" || !kopije[0].Vrijeme.Equal(pocetak.Add(6*time.Minute)) {
		t.Fatalf("kopije: %+v", kopije)
	}
	if b, _ := os.ReadFile(put); string(b) != "addr = ':81'\n" {
		t.Errorf("nova datoteka: %q", b)
	}
	if fi, _ := os.Stat(put); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o640 {
		t.Errorf("dopuštenja: %v", fi.Mode().Perm())
	}
	unosi, _ := os.ReadDir(dir)
	for _, u := range unosi {
		if strings.HasPrefix(u.Name(), ".") {
			t.Errorf("ostala je privremena datoteka %s", u.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, PrimjerFileName)); err != nil {
		t.Error("primjer postavki obrisan kao kopija")
	}
	// dva spremanja u istoj sekundi: druga kopija dobije nastavak
	isto := pocetak.Add(time.Hour)
	k1, _ := Zapisi(put, []byte("addr = ':82'\n"), isto)
	k2, _ := Zapisi(put, []byte("addr = ':83'\n"), isto)
	if k1 == k2 || !strings.HasSuffix(k2, "-2") {
		t.Errorf("kopije iz iste sekunde: %q %q", k1, k2)
	}
}

// Kad se stara datoteka ne da spremiti u kopiju, nova se ne zapisuje.
func TestZapisiBezKopijeNista(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("mapa samo za čitanje ne brani pisanje")
	}
	dir := t.TempDir()
	put := filepath.Join(dir, FileName)
	if err := os.WriteFile(put, []byte("addr = ':80'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := Zapisi(put, []byte("addr = ':81'\n"), time.Now()); err == nil || !strings.Contains(err.Error(), "kopija") {
		t.Errorf("bez kopije: %v", err)
	}
	if b, _ := os.ReadFile(put); string(b) != "addr = ':80'\n" {
		t.Errorf("datoteka je promijenjena: %q", b)
	}
	if _, err := Zapisi(filepath.Join(dir, "nova", FileName), []byte("x"), time.Now()); err == nil {
		t.Error("nova mapa u mapi samo za čitanje bez greške")
	}
}

// Kad datoteke nema, nastaje nova bez kopije; kad se ne da zapisati, javlja
// se greška i ništa ne ostaje.
func TestZapisiNovuIGreske(t *testing.T) {
	dir := t.TempDir()
	put := filepath.Join(dir, "podmapa", FileName)
	if kopija, err := Zapisi(put, []byte("addr = ':80'\n"), time.Now()); err != nil || kopija != "" {
		t.Fatalf("nova datoteka: %q %v", kopija, err)
	}
	razmakPreimenovanja = 0
	t.Cleanup(func() { razmakPreimenovanja = 50 * time.Millisecond })
	// na mjestu datoteke stoji mapa s nečim u sebi: preimenovanje ne uspije
	mapa := filepath.Join(dir, "mapa")
	if err := os.MkdirAll(filepath.Join(mapa, "unutra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zapisiAtomski(mapa, []byte("x"), 0o644); err == nil {
		t.Error("zapis preko mape bez greške")
	}
	unosi, _ := os.ReadDir(dir)
	for _, u := range unosi {
		if strings.HasPrefix(u.Name(), ".") {
			t.Errorf("ostala je privremena datoteka %s", u.Name())
		}
	}
	if _, err := Zapisi(mapa, []byte("x"), time.Now()); err == nil {
		t.Error("Zapisi preko mape bez greške")
	}
	if Kopije(filepath.Join(dir, "nema", FileName)) != nil {
		t.Error("kopije iz nepostojeće mape")
	}
	zapreka := filepath.Join(dir, "datoteka")
	if err := os.WriteFile(zapreka, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Zapisi(filepath.Join(zapreka, FileName), []byte("x"), time.Now()); err == nil {
		t.Error("zapis u mapu koja se ne da napraviti bez greške")
	}
}

// Primjer pri prvom pokretanju koji se ne da zapisati javlja grešku
func TestPrimjerSeNeDaZapisati(t *testing.T) {
	dir := t.TempDir()
	zapreka := filepath.Join(dir, "datoteka")
	if err := os.WriteFile(zapreka, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if zapisan, err := WriteExample(filepath.Join(zapreka, "podmapa", FileName), Default()); err == nil || zapisan {
		t.Errorf("mapa se ne da napraviti: %v %v", zapisan, err)
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	samoCitanje := filepath.Join(dir, "samo-citanje")
	if err := os.MkdirAll(samoCitanje, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(samoCitanje, 0o755) })
	if zapisan, err := WriteExample(filepath.Join(samoCitanje, FileName), Default()); err == nil || zapisan {
		t.Errorf("mapa samo za čitanje: %v %v", zapisan, err)
	}
}
