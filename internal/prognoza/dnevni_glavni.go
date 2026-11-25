package prognoza

// Dnevni model Drave i Dunava iz zapisa. Letva koja je cilj dnevnog modela u
// programu (DnevniCiljevi) smije u svom retku modela nositi blok „dnevni”: iz
// čega se uči njezin dnevni model i od kojeg dana on daje vrijednost. Letva
// bez takvog bloka radi iz programa, kao i dosad.
//
// Blokovi se iz baze prognoza čitaju u postavu koju svi čitaju odjednom: račun
// je čita pri svakom izdavanju, a stranice pri prikazu. Program je tako isti na
// računalu koje izdaje i na onome koje prognozu samo prima.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"math"
	"slices"
	"sync/atomic"
)

// dnevnaPostava je ono što zapisi mijenjaju u dnevnom modelu iz programa.
type dnevnaPostava struct {
	ciljevi     map[string]DnevniCilj
	odDana      map[string]int
	odDanaNiska map[string]int
	visoka      map[string]float64
	otisak      string
}

var dnevnaIzZapisa atomic.Pointer[dnevnaPostava]

// postavaDnevnog je trenutna postava iz zapisa; prazna kad zapisa nema.
func postavaDnevnog() *dnevnaPostava {
	if p := dnevnaIzZapisa.Load(); p != nil {
		return p
	}
	return &dnevnaPostava{}
}

// CiljeviDnevnog su ciljevi dnevnog modela, redom kako voda teče: oni iz
// programa, a letvi koja u zapisu ima dnevni blok ulazi i slivovi iz zapisa.
func CiljeviDnevnog() []DnevniCilj {
	p := postavaDnevnog()
	if len(p.ciljevi) == 0 {
		return DnevniCiljevi
	}
	out := slices.Clone(DnevniCiljevi)
	for i, c := range out {
		if z, ima := p.ciljevi[c.Letva]; ima {
			out[i] = z
		}
	}
	return out
}

// ciljIzPrograma je cilj dnevnog modela letve kako stoji u programu.
func ciljIzPrograma(letva string) (DnevniCilj, bool) {
	i := slices.IndexFunc(DnevniCiljevi, func(c DnevniCilj) bool { return c.Letva == letva })
	if i < 0 {
		return DnevniCilj{}, false
	}
	return DnevniCiljevi[i], true
}

// GlavniDnevniCilj javlja je li letva cilj dnevnog modela u programu.
func GlavniDnevniCilj(letva string) bool {
	_, ima := ciljIzPrograma(letva)
	return ima
}

// DnevniRacunIzZapisa javlja uči li se dnevni model letve iz drugih ulaza ili
// slivova nego u programu. Takvoj letvi ne vrijedi uzvodni ispravak, jer je
// naučen na greškama modela iz programa.
func DnevniRacunIzZapisa(letva string) bool {
	_, ima := postavaDnevnog().ciljevi[letva]
	return ima
}

// DnevnaOdDanaLetve je dan od kojeg dnevni model daje vrijednost pri visokoj
// vodi: iz zapisa letve, inače iz programa.
func DnevnaOdDanaLetve(letva string) (int, bool) {
	if d, ima := postavaDnevnog().odDana[letva]; ima {
		return d, true
	}
	d, ima := DnevnaOdDana[letva]
	return d, ima
}

// OdDanaNiskeLetve je isto za nisku i srednju vodu.
func OdDanaNiskeLetve(letva string) (int, bool) {
	if d, ima := postavaDnevnog().odDanaNiska[letva]; ima {
		return d, true
	}
	d, ima := DnevnaOdDanaNiska[letva]
	return d, ima
}

// VisokaVodaLetve je vodostaj iznad kojeg vrijedi dan prijelaza visoke vode.
func VisokaVodaLetve(letva string) (float64, bool) {
	if g, ima := postavaDnevnog().visoka[letva]; ima {
		return g, true
	}
	g, ima := VisokaVoda[letva]
	return g, ima
}

// provjeriGlavniDnevni odbija dnevni blok letve glavnog dnevnog modela koji se
// ne smije računati.
func (d DnevniLetve) provjeriGlavniDnevni(letva string) error {
	if !GlavniDnevniCilj(letva) {
		return errors.New("letva nije cilj dnevnog modela Drave i Dunava")
	}
	if err := d.provjeri(letva); err != nil {
		return err
	}
	if slices.ContainsFunc(d.Slivovi, func(s string) bool { _, kisomjer := KisomjerIzSliva(s); return kisomjer }) {
		return errors.New("dnevni model Drave i Dunava računa s kišom međuslivova; kiša jednog kišomjera ovdje se ne bira")
	}
	return d.provjeriPrijelaz()
}

// provjeriPrijelaz gleda dan od kojeg dnevni model daje vrijednost i granicu
// visoke vode.
func (d DnevniLetve) provjeriPrijelaz() error {
	if !valjaDanPrijelaza(d.OdDana) || !valjaDanPrijelaza(d.OdDanaNiska) {
		return fmt.Errorf("dan od kojeg vrijedi dnevni model mora biti između 1 i %d", DnevniDosezi)
	}
	if !valjaGranicaVode(d.VisokaVoda) {
		return errors.New("granica visoke vode nije razuman broj")
	}
	return nil
}

// valjaDanPrijelaza javlja je li dan prijelaza prazan ili unutar dosega dnevnog modela.
func valjaDanPrijelaza(dan *int) bool {
	return dan == nil || (*dan >= 1 && *dan <= DnevniDosezi)
}

// valjaGranicaVode javlja je li granica visoke vode prazna ili razuman broj.
func valjaGranicaVode(g *float64) bool {
	return g == nil || (!math.IsNaN(*g) && uGranicama(*g, najvecaVrijednost))
}

// istiRacun javlja uči li se cilj iz istih ulaza i slivova, bez obzira na redoslijed.
func istiRacun(a, b DnevniCilj) bool {
	isti := func(x, y []string) bool {
		return slices.Equal(slices.Sorted(slices.Values(x)), slices.Sorted(slices.Values(y)))
	}
	return isti(a.Ulazi, b.Ulazi) && isti(a.Slivovi, b.Slivovi)
}

// dodaj upisuje u postavu dnevni blok jedne letve.
func (p *dnevnaPostava) dodaj(letva string, d DnevniLetve) {
	iz, _ := ciljIzPrograma(letva)
	cilj := DnevniCilj{Letva: letva, Ulazi: d.Ulazi, Slivovi: d.Slivovi, NajmanjeDana: iz.NajmanjeDana}
	if !istiRacun(cilj, iz) {
		p.ciljevi[letva] = cilj
	}
	if d.OdDana != nil {
		p.odDana[letva] = *d.OdDana
	}
	if d.OdDanaNiska != nil {
		p.odDanaNiska[letva] = *d.OdDanaNiska
	}
	if d.VisokaVoda != nil {
		p.visoka[letva] = *d.VisokaVoda
	}
}

// postavaIzZapisa slaže postavu iz redaka modela: uzima dnevne blokove letvi
// glavnog dnevnog modela koji prolaze provjeru. Ostale vraća s razlogom.
func postavaIzZapisa(zapisi map[string]ZapisLetve) (*dnevnaPostava, map[string]error) {
	p := &dnevnaPostava{ciljevi: map[string]DnevniCilj{}, odDana: map[string]int{}, odDanaNiska: map[string]int{}, visoka: map[string]float64{}}
	odbijeni := map[string]error{}
	uzeti := map[string]DnevniLetve{}
	for _, letva := range slices.Sorted(maps.Keys(zapisi)) {
		d, err := zapisi[letva].DnevniBlok()
		if err != nil || !GlavniDnevniCilj(letva) {
			continue // letva bez dnevnog bloka ili izvan glavnog dnevnog modela
		}
		if err := d.provjeriGlavniDnevni(letva); err != nil {
			odbijeni[letva] = err
			continue
		}
		p.dodaj(letva, d)
		uzeti[letva] = d
	}
	otisak, _ := json.Marshal(uzeti) // popis šifri i brojeva uvijek se da zapisati
	p.otisak = string(otisak)
	return p, odbijeni
}

// UcitajDnevneIzZapisa čita dnevne blokove iz baze prognoza u postavu koju
// čitaju račun i stranice. Kad se postava promijeni, naučeni dnevni modeli se
// bacaju, pa ih sljedeći račun uči iznova. Vraća letve čiji je blok odbijen.
func UcitajDnevneIzZapisa(baza *sql.DB) (map[string]error, error) {
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil {
		return nil, err
	}
	nova, odbijeni := postavaIzZapisa(zapisi)
	if stara := postavaDnevnog(); stara.otisak != nova.otisak && (stara.otisak != "" || nova.otisak != "{}") {
		ZaboraviDnevneModele()
	}
	dnevnaIzZapisa.Store(nova)
	return odbijeni, nil
}

// ucitajDnevneIzZapisa pročita dnevne blokove prije računa. Blok koji ne prolazi
// provjeru ide u dnevnik, a letva radi iz programa; ni baza koja se ne da
// pročitati ne zaustavlja račun.
func (o *Osvjezivac) ucitajDnevneIzZapisa() {
	OsvjeziDnevnuPostavu(o.Baza)
}

// OsvjeziDnevnuPostavu čita dnevne blokove i postavke snijega iz baze i u
// dnevnik piše što nije uzeto. Zove se pri pokretanju, prije svakog računa i
// kad model stigne razmjenom.
func OsvjeziDnevnuPostavu(baza *sql.DB) {
	osvjeziPostavkeSnijega(baza)
	odbijeni, err := UcitajDnevneIzZapisa(baza)
	if err != nil {
		log.Printf("prognoza: dnevni model iz zapisa: %v", err)
		return
	}
	for _, letva := range slices.Sorted(maps.Keys(odbijeni)) {
		log.Printf("prognoza: dnevni model letve %s iz zapisa se ne koristi: %v", letva, odbijeni[letva])
	}
}
