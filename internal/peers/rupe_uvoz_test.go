package peers

// Rupe uz izvoz i uvoz (rupe_uvoz.go): što arhiva knjige i COP paket nose,
// što uvoznik iz toga zapiše, COP inačica 4 i potpis, te kako ono što nosi
// rupe čita čvor 0.0.38.

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gocop/internal/ledger"
)

// Uvoz uzima kanale koje čvor prati, a što ograda odbije, vraća zasebno
func TestZaUvoz(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	s.Accept(odbijaStaro)
	verzije := []ledger.Version{v("ured", "ocitanja/16/2024", "a"), v("ured", "ocitanja/3/2024", "b"), v("ured", "ocitanja/16/2024", "c")}
	verzije[2].EntityID = "staro-c"
	samo16 := func(kanal string) bool { return kanal == "ocitanja/16/2024" }
	wanted, bacio := s.zaUvoz(verzije, samo16)
	if len(wanted) != 1 || wanted[0].VersionID != "a" || bacio["ured|ocitanja/16/2024"] != (baceno{najmanja: "c", najveca: "c"}) || len(bacio) != 1 {
		t.Errorf("uz filtar: %v %+v", wanted, bacio)
	}
	s.Accept(nil)
	if wanted, bacio = s.zaUvoz(verzije, nil); len(wanted) != 3 || bacio != nil {
		t.Errorf("bez filtra: %v %+v", wanted, bacio)
	}
}

// Uvoz: po tuđem ključu odbijeno i rupe izvoznika iznad granice knjige;
// kad se granica ne da pročitati, prazna
func TestPromjenaUvoza(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	upisiStalno(t, s, verzijaRupe("301", "novo-1"))
	k := kljucRupe
	wanted := []ledger.Version{verzijaRupe("302", "x"), v("cop-osijek", "ocitanja/16/2024", "z")}
	bacio := map[string]baceno{k: {najmanja: "0190a000-0000-7000-8000-000000000303", najveca: "0190a000-0000-7000-8000-000000000304"}}
	rupe := map[string]raspon{k: r("", "0190a000-0000-7000-8000-000000000310")}
	p := s.promjenaUvoza(ctxRub, wanted, bacio, rupe)
	ocekivano := promjenaKljuca{fs: rupaNovo1.VersionID, nesigurno: rupe[k], odbijeno: r("0190a000-0000-7000-8000-000000000302", "0190a000-0000-7000-8000-000000000304")}
	if len(p.kljucevi) != 1 || fmt.Sprint(p.kljucevi[k]) != fmt.Sprint(ocekivano) || p.prije != nil || p.pravilo {
		t.Errorf("uvoz: %+v", p)
	}
	pokvariTablicu(t, s, "record_versions")
	if p := s.promjenaUvoza(ctxRub, wanted, nil, rupe); p.kljucevi[k].fs != "" {
		t.Errorf("bez granice: %+v", p)
	}
}

// Uvoz rupa i verzija: kad se rupe ne daju zapisati, ne upisuje se ništa;
// kad padne serija verzija, vraća se koliko je upisano do tada
func TestUveziSRupama(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	otkazan, otkazi := context.WithCancel(ctxRub)
	otkazi()
	// brava je zauzeta, pa uvoz čeka dok mu kontekst ne istekne
	if err := s.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	if err := s.rupePodBravom(otkazan, promjenaRupa{pravilo: true}, promjenaKljuca.prosiri); err == nil {
		t.Error("brava uz otkazan kontekst")
	}
	if n, err := s.uveziSRupama(otkazan, []ledger.Version{rupaNovo1}, promjenaRupa{}); n != 0 || err == nil {
		t.Errorf("otkazan uvoz: %d %v", n, err)
	}
	s.pustiBravu()
	verzije := make([]ledger.Version, 2001)
	for i := range verzije {
		verzije[i] = stalnaVerzija(fmt.Sprintf("0190a000-0000-7000-8000-%012d", i), "windows-ured", kanalRupe, fmt.Sprintf("z-%d", i), `{}`)
	}
	kvarUpisa(t, s, "INSERT", "record_versions", "NEW.entity_id = 'z-2000'")
	if n, err := s.uveziSRupama(ctxRub, verzije, promjenaRupa{}); n != 2000 || err == nil {
		t.Errorf("pala druga serija: %d %v", n, err)
	}
}

// arhivaSRupama je arhiva knjige s jednim ključem i zadanim zapisom rupa
// (bez retka kad je nil)
func arhivaSRupama(t *testing.T, rupe *string) *sql.DB {
	t.Helper()
	baza, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "arhiva.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if _, err := baza.Exec(`CREATE TABLE archive_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if rupe != nil {
		if _, err := baza.Exec(`INSERT INTO archive_meta (key, value) VALUES ('rupe', ?)`, *rupe); err != nil {
			t.Fatal(err)
		}
	}
	return baza
}

func TestRupeIzArhiveIPaketa(t *testing.T) {
	verzije := []ledger.Version{v("ured", "ocitanja/16/2024", "c"), v("ured", "ocitanja/16/2024", "e"), v("ured", "ocitanja/3/2024", "d")}
	ispravne := `{"ured|ocitanja/16/2024":{"od":"b","do":"z"},"ured|ocitanja/3/2024":{"od":"x","do":"x"},"drugi":{"od":"","do":"a"}}`
	ocisceno := map[string]raspon{"ured|ocitanja/16/2024": r("b", "e"), "ured|ocitanja/3/2024": r("", "d")}
	pokvarene, prazne := `nije json`, `{}`
	slucajevi := []struct {
		ime  string
		rupe *string
		out  map[string]raspon
	}{
		{"stara arhiva", nil, nil},
		{"ispravne", &ispravne, ocisceno},
		{"bez rupa", &prazne, map[string]raspon{}},
		{"pokvarene", &pokvarene, map[string]raspon{"ured|ocitanja/16/2024": r("", "e"), "ured|ocitanja/3/2024": r("", "d")}},
	}
	for _, sl := range slucajevi {
		if got := rupeIzArhive(ctxRub, arhivaSRupama(t, sl.rupe), verzije); fmt.Sprint(got) != fmt.Sprint(sl.out) || (sl.out == nil) != (got == nil) {
			t.Errorf("%s: %v", sl.ime, got)
		}
	}
	var m CopManifest
	if err := json.Unmarshal([]byte(`{"rupe":`+ispravne+`}`), &m); err != nil {
		t.Fatal(err)
	}
	if got := rupeIzPaketa(m, verzije); fmt.Sprint(got) != fmt.Sprint(ocisceno) {
		t.Errorf("paket: %v", got)
	}
	if got := rupeIzPaketa(CopManifest{}, verzije); len(got) != 0 {
		t.Errorf("paket bez rupa: %v", got)
	}
	if got := sveNesigurno(verzije); fmt.Sprint(got) != fmt.Sprint(slucajevi[3].out) {
		t.Errorf("sve nesigurno: %v", got)
	}
}

// kanonskiCop0038 je kanonski zapis manifesta kakav potpisuje i provjerava
// čvor 0.0.38 (v0.0.38-alfa, c0162ca9), prepisan doslovno i zamrznut
func kanonskiCop0038(m CopManifest) []byte {
	var b strings.Builder
	red := func(k, v string) { b.WriteString(k); b.WriteByte('\t'); b.WriteString(v); b.WriteByte('\n') }
	red("inacica", strconv.Itoa(m.Inacica))
	red("obuhvat", m.Obuhvat.Kljuc())
	kanali := append([]string(nil), m.Kanali...)
	sort.Strings(kanali)
	red("kanali", strings.Join(kanali, ","))
	red("izdanje", strconv.Itoa(m.Izdanje))
	red("prethodno", m.Prethodno)
	red("izdao", m.Izdao)
	red("otisak", m.Otisak)
	red("zapisa", strconv.Itoa(m.Zapisa))
	red("sadrzaja", strconv.Itoa(m.Sadrzaja))
	return []byte(b.String())
}

// manifest0038 je manifest kako ga čita čvor 0.0.38: bez rupa, a noviju
// inačicu od 3 odbija istom porukom kao njegov CitajCop
func manifest0038(mb []byte) error {
	var m struct {
		Inacica int `json:"inacica"`
	}
	if err := json.Unmarshal(mb, &m); err != nil {
		return err
	}
	if m.Inacica > 3 {
		return fmt.Errorf("paket je izdao noviji program (inačica %d, ovaj čita do %d) — ažurirajte goCOP", m.Inacica, 3)
	}
	return nil
}

// manifestPaketa pročita manifest.json iz paketa
func manifestPaketa(t *testing.T, paket []byte) []byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(paket), int64(len(paket)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			var b bytes.Buffer
			if _, err := b.ReadFrom(rc); err != nil {
				t.Fatal(err)
			}
			return b.Bytes()
		}
	}
	t.Fatal("paket bez manifesta")
	return nil
}

// COP inačica 4: paket s rupama potpisuje i rupe, pa ih nitko ne može
// maknuti ni dodati; čvor 0.0.38 ga odbija čitljivom porukom. Paket bez
// rupa je inačica 3 s istim kanonskim zapisom kao u 0.0.38, pa ga 0.0.38
// uveze.
func TestCop4(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
	a, d, c := cv[0], cv[1], cv[2]
	d.Accept(odbijaStaro)
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, d, c, true)
	izvezi := func(s *Service) (CopManifest, []byte) {
		var paket bytes.Buffer
		m, err := s.IzveziCop(ctxRub, CopObuhvat{Vrsta: "ocitanja", AreaID: 16, Od: 2024, Do: 2024}, []string{kanalRupe}, false, &paket)
		if err != nil {
			t.Fatal(err)
		}
		return m, paket.Bytes()
	}
	m4, paket4 := izvezi(d)
	if m4.Inacica != 4 || len(m4.Rupe) != 1 || m4.Rupe[kljucRupe] != r(rupaNovo1.VersionID, rupaStaroA.VersionID) {
		t.Fatalf("uski laptop: inačica %d, rupe %v", m4.Inacica, m4.Rupe)
	}
	if _, err := ProvjeriCopPotpis(m4); err != nil {
		t.Fatalf("potpis: %v", err)
	}
	bezRupa := m4
	bezRupa.Rupe = nil
	sDrugom := m4
	sDrugom.Rupe = map[string]raspon{kljucRupe: r(rupaNovo1.VersionID, rupaNovo1.VersionID+"1")}
	kao3 := bezRupa
	kao3.Inacica = 3
	for ime, m := range map[string]CopManifest{"maknute rupe": bezRupa, "druge rupe": sDrugom, "maknute rupe i inačica 3": kao3} {
		if _, err := ProvjeriCopPotpis(m); err == nil {
			t.Errorf("%s: potpis vrijedi", ime)
		}
	}
	if err := manifest0038(manifestPaketa(t, paket4)); err == nil || !strings.Contains(err.Error(), "inačica 4") || !strings.Contains(err.Error(), "ažurirajte goCOP") {
		t.Errorf("0.0.38 i inačica 4: %v", err)
	}
	m3, paket3 := izvezi(c)
	if m3.Inacica != 3 || len(m3.Rupe) != 0 || strings.Contains(string(manifestPaketa(t, paket3)), "rupe") {
		t.Errorf("ured: inačica %d, rupe %v", m3.Inacica, m3.Rupe)
	}
	if !bytes.Equal(kanonskiCop(m3), kanonskiCop0038(m3)) || manifest0038(manifestPaketa(t, paket3)) != nil {
		t.Error("paket inačice 3 nije isti kao u 0.0.38")
	}
	if redRupa(nil) != "" || redRupa(map[string]raspon{"b": r("", "x"), "a|k": r("c", "d")}) != "rupe\t\"a|k\"=\"c\",\"d\";\"b\"=\"\",\"x\";\n" {
		t.Errorf("redak rupa: %q", redRupa(map[string]raspon{"b": r("", "x"), "a|k": r("c", "d")}))
	}
	if inacicaCopa(nil) != 3 || inacicaCopa(map[string]raspon{"k": r("a", "b")}) != 4 || CopInacica != 4 {
		t.Error("inačica paketa")
	}
	put := filepath.Join(t.TempDir(), "paket.cop")
	if err := os.WriteFile(put, paket4, 0o600); err != nil {
		t.Fatal(err)
	}
	if rep, err := a.UveziCop(ctxRub, put); err != nil || rep.Novih != 2 || rupeCvora(t, a)[kljucRupe].trazi.prazan() {
		t.Errorf("uvoz inačice 4: %+v %v", rep, err)
	}
}

// delta0038 je delta kakvu šalje i čita čvor 0.0.38, prepisana doslovno
type delta0038 struct {
	Versions  []ledger.Version `json:"versions"`
	Skraceno  bool             `json:"skraceno,omitempty"`
	Tailscale []string         `json:"tailscale,omitempty"`
}

// Delta s Rupama i Popunom i manifest s rupama čvor 0.0.38 čita bez greške
// i s istim verzijama, a nova polja zanemari; delta bez njih je bajt po
// bajt ista
func TestRupe0038ZanemarujeNovaPolja(t *testing.T) {
	nova := deltaMsg{Versions: []ledger.Version{rupaNovo1}, Rupe: map[string]raspon{kljucRupe: r("", "a")}, Popuna: map[string]raspon{kljucRupe: r("", "b")}}
	b, err := json.Marshal(nova)
	if err != nil {
		t.Fatal(err)
	}
	var stara delta0038
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&stara); err != nil || len(stara.Versions) != 1 || stara.Versions[0].VersionID != rupaNovo1.VersionID {
		t.Errorf("0.0.38 čita deltu: %+v %v", stara, err)
	}
	bez, _ := json.Marshal(deltaMsg{Versions: []ledger.Version{rupaNovo1}})
	staraBez, _ := json.Marshal(delta0038{Versions: []ledger.Version{rupaNovo1}})
	if !bytes.Equal(bez, staraBez) {
		t.Errorf("delta bez rupa:\n%s\n%s", bez, staraBez)
	}
	var m struct {
		Inacica int    `json:"inacica"`
		Otisak  string `json:"otisak"`
	}
	mb, _ := json.Marshal(CopManifest{Inacica: 3, Otisak: "x", Rupe: map[string]raspon{kljucRupe: r("", "a")}})
	if err := json.Unmarshal(mb, &m); err != nil || m.Otisak != "x" {
		t.Errorf("0.0.38 čita manifest: %+v %v", m, err)
	}
}

// Arhiva koja se ne da otvoriti: izvoz i uvoz vrate grešku
func TestArhivaSeNeDaOtvoriti(t *testing.T) {
	s := rubniCvor(t, "cop-osijek")
	upisiStalno(t, s, rupaNovo1)
	prije := upravljacArhive
	upravljacArhive = "nepostojeci"
	t.Cleanup(func() { upravljacArhive = prije })
	put := filepath.Join(t.TempDir(), "arhiva.db")
	if _, err := s.ExportFile(ctxRub, put, []string{kanalRupe}); err == nil {
		t.Error("izvoz bez upravljačkog programa")
	}
	if _, err := s.ImportFile(ctxRub, put); err == nil {
		t.Error("uvoz bez upravljačkog programa")
	}
}

// COP paket se ne uvozi kad se katalog paketa ne da pripremiti
func TestUveziCopBezKataloga(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "windows-ured")
	a, c := cv[0], cv[1]
	upisiStalno(t, c, rupaNovo1)
	var paket bytes.Buffer
	if _, err := c.IzveziCop(ctxRub, CopObuhvat{Vrsta: "ocitanja", AreaID: 16, Od: 2024, Do: 2024}, []string{kanalRupe}, false, &paket); err != nil {
		t.Fatal(err)
	}
	put := filepath.Join(t.TempDir(), "paket.cop")
	if err := os.WriteFile(put, paket.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`CREATE INDEX cop_izdanja ON peers(name)`); err != nil {
		t.Fatal(err)
	}
	if rep, err := a.UveziCop(ctxRub, put); err == nil || rep.Novih != 0 {
		t.Errorf("uvoz bez kataloga: %+v %v", rep, err)
	}
}

// Uvoz uz pokvarenu tablicu rupa: rupe se ne daju zapisati, pa se ne
// upisuje ni jedna verzija
func TestRupeUvozBezZapisa(t *testing.T) {
	cv := mrezaRupe(t, nil, "cop-osijek", "laptop-drugi", "windows-ured")
	a, d, c := cv[0], cv[1], cv[2]
	d.Accept(odbijaStaro)
	upisiStalno(t, c, rupaNovo1, rupaStaroA, rupaNovo2)
	upisaoPrimatelj(t, d, c, true)
	pokvariTablicu(t, a, "rupe_knjige")
	for _, put := range putoviUvoza {
		if n, err := put.uvezi(a, d, kanalRupe, t.TempDir()); err == nil || n != 0 || imaVerziju(t, a, rupaNovo1.VersionID) {
			t.Errorf("%s uz pokvarenu tablicu: uvezeno %d, %v", put.ime, n, err)
		}
	}
}
