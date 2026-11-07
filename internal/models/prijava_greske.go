package models

import (
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Vrste prijave koju korisnik šalje ikonom kukca
const (
	VrstaPrijaveGreska    = "greska"
	VrstaPrijavePrijedlog = "prijedlog"
)

// Stanja prijave, u redoslijedu kojim prijava obično ide
const (
	StanjePrijaveNova     = "nova"
	StanjePrijaveURadu    = "u_radu"
	StanjePrijaveRijeseno = "rijeseno"
)

// Ograde teksta prijave. Prijava putuje na sva računala u mreži, pa ostaje mala.
const (
	NajviseOpisaPrijave    = 4000
	NajviseOdgovoraPrijave = 4000
	najviseStranice        = 1000
	najviseNazivaStranice  = 200
	najviseZaslona         = 32
	najviseGresakaStranice = 5
	najviseJedneGreske     = 300

	najviseVrijednostiUpita = 64
)

// NajvecaSlikaPrijave je najveća slika zaslona uz prijavu. Slika putuje na sva
// računala u mreži, pa je granica manja nego za fotografije s terena.
const NajvecaSlikaPrijave = 2 << 20

// RokSlikePrijave je koliko slika ostaje nakon što je prijava riješena. Prijava
// i tekst ostaju i poslije.
const RokSlikePrijave = 30 * 24 * time.Hour

// Vrste slike zaslona koje prijava smije nositi
const (
	SlikaPrijavePNG  = "image/png"
	SlikaPrijaveJPEG = "image/jpeg"
	SlikaPrijaveWebP = "image/webp"
)

// PrijavaGreske je greška ili prijedlog koji je korisnik poslao s neke
// stranice. Tekst napiše korisnik, a stranicu, izdanje, računalo i preglednik
// zabilježi program. Stanje upisuje administrator kao novu verziju istog zapisa,
// a svaki odgovor je zaseban zapis, da ga promjena stanja na drugom računalu
// ne može pregaziti.
type PrijavaGreske struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	Ime           string    `json:"ime"`
	Uloga         string    `json:"uloga"`
	Vrsta         string    `json:"vrsta"`
	Opis          string    `json:"opis"`
	Stranica      string    `json:"stranica"` // put i upit, bez osjetljivih parametara
	NazivStranice string    `json:"naziv_stranice"`
	Izdanje       string    `json:"izdanje"`
	Cvor          string    `json:"cvor"`
	Sustav        string    `json:"sustav"`
	Preglednik    string    `json:"preglednik"`
	Zaslon        string    `json:"zaslon"`
	Greske        []string  `json:"greske,omitempty"`
	Stanje        string    `json:"stanje"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Slika zaslona po želji. Bajtovi su u spremištu sadržaja i putuju po otisku.
	SlikaOtisak  string `json:"slika_otisak,omitempty"`
	SlikaBajtova int    `json:"slika_bajtova,omitempty"`
	SlikaVrsta   string `json:"slika_vrsta,omitempty"`
	// SlikaIstekla: prijava je vraćena iz „riješeno” kad je rok slike već prošao.
	// Slike tada više nema, pa je nijedno računalo ne smije tražiti.
	SlikaIstekla bool `json:"slika_istekla,omitempty"`

	// Odgovori se ne spremaju uz prijavu; puni ih čitanje, redom kojim su nastali
	Odgovori []OdgovorNaPrijavu `json:"-"`
	// PorukaPoslana je kad je ovo računalo javilo prijavu e-poštom; ne putuje
	PorukaPoslana *time.Time `json:"-"`
	// SlikaTu javlja je li slika već na ovom računalu; puni je MojePrijave, ne putuje
	SlikaTu bool `json:"-"`
}

// OdgovorNaPrijavu je jedan odgovor administratora. Ne mijenja se: novi odgovor
// je novi zapis, pa dva administratora ne mogu izbrisati jedan drugome tekst.
type OdgovorNaPrijavu struct {
	ID        string    `json:"id"`
	PrijavaID string    `json:"prijava_id"`
	Tekst     string    `json:"tekst"`
	Odgovorio string    `json:"odgovorio"`
	CreatedAt time.Time `json:"created_at"`
}

// Kad je dan odgovora riječima
func (o *OdgovorNaPrijavu) Kad() string {
	return o.CreatedAt.In(Zagreb).Format("2. 1. 2006.")
}

// VrstaNaziv je vrsta prijave riječima
func (p *PrijavaGreske) VrstaNaziv() string {
	if p.Vrsta == VrstaPrijavePrijedlog {
		return "Prijedlog"
	}
	return "Greška"
}

// StanjeNaziv je stanje prijave riječima
func (p *PrijavaGreske) StanjeNaziv() string { return NazivStanjaPrijave(p.Stanje) }

// StanjeZaPrikaz je stanje kako ga ovaj program prikazuje i broji: nepoznato
// stanje s novijeg računala vodi se kao nova prijava, da je netko pogleda.
func StanjeZaPrikaz(stanje string) string {
	if JeStanjePrijave(stanje) {
		return stanje
	}
	return StanjePrijaveNova
}

// NazivStanjaPrijave je stanje riječima
func NazivStanjaPrijave(stanje string) string {
	switch stanje {
	case StanjePrijaveURadu:
		return "U radu"
	case StanjePrijaveRijeseno:
		return "Riješeno"
	}
	return "Nova"
}

// StanjeRazred je oznaka stanja za izgled; nepoznato stanje izgleda kao nova
func (p *PrijavaGreske) StanjeRazred() string {
	switch p.Stanje {
	case StanjePrijaveURadu:
		return "pg-stanje-rad"
	case StanjePrijaveRijeseno:
		return "pg-stanje-rijeseno"
	}
	return "pg-stanje-nova"
}

// Kad je vrijeme slanja riječima, npr. 6. 11. 2026. u 17:42
func (p *PrijavaGreske) Kad() string {
	return p.CreatedAt.In(Zagreb).Format("2. 1. 2006. u 15:04")
}

// Racunalo je sustav i ime računala kroz koje je prijava poslana
func (p *PrijavaGreske) Racunalo() string {
	switch {
	case p.Sustav == "":
		return p.Cvor
	case p.Cvor == "":
		return p.Sustav
	}
	return p.Sustav + " · " + p.Cvor
}

// PorukaKad je vrijeme e-pošte administratorima riječima; prazno kad je ovo
// računalo nije slalo
func (p *PrijavaGreske) PorukaKad() string {
	if p.PorukaPoslana == nil {
		return ""
	}
	return p.PorukaPoslana.In(Zagreb).Format("2. 1. 2006. u 15:04")
}

// SlikaUklonjenaU javlja je li slika u tom trenutku za brisanje. Svako računalo
// to računa samo, iz stanja i vremena koja je prijava donijela razmjenom. Prijava
// se mijenja samo uz promjenu stanja (odgovor je zaseban zapis), pa je UpdatedAt
// riješene prijave trenutak kad je riješena, a vraćena prijava izlazi iz roka.
// Zato brisanje ne treba novu verziju i ne putuje među računalima.
func (p *PrijavaGreske) SlikaUklonjenaU(sad time.Time) bool {
	if p.SlikaOtisak == "" {
		return false
	}
	return p.SlikaIstekla || (p.Stanje == StanjePrijaveRijeseno && !sad.Before(p.UpdatedAt.Add(RokSlikePrijave)))
}

// SlikaUklonjena je SlikaUklonjenaU za ovaj trenutak, za stranice
func (p *PrijavaGreske) SlikaUklonjena() bool { return p.SlikaUklonjenaU(time.Now()) }

// JeVrstaSlikePrijave javlja je li to vrsta slike zaslona koju prijava smije nositi
func JeVrstaSlikePrijave(vrsta string) bool {
	return vrsta == SlikaPrijavePNG || vrsta == SlikaPrijaveJPEG || vrsta == SlikaPrijaveWebP
}

// OcistiOpisSlike briše opis slike koji program ne bi sam napisao: drugo računalo
// ne smije naložiti dohvat nečega što nije slika zaslona prihvatljive veličine.
func (p *PrijavaGreske) OcistiOpisSlike() {
	if !jeOtisakSlike(p.SlikaOtisak) || !JeVrstaSlikePrijave(p.SlikaVrsta) || p.SlikaBajtova <= 0 || p.SlikaBajtova > NajvecaSlikaPrijave {
		p.SlikaOtisak, p.SlikaVrsta, p.SlikaBajtova = "", "", 0
	}
}

// jeOtisakSlike: SHA-256 u heksadecimalnom zapisu malim slovima
func jeOtisakSlike(o string) bool {
	if len(o) != 64 {
		return false
	}
	return !strings.ContainsFunc(o, func(r rune) bool {
		return (r < '0' || r > '9') && (r < 'a' || r > 'f')
	})
}

// JeStanjePrijave javlja je li vrijednost jedno od stanja prijave
func JeStanjePrijave(stanje string) bool {
	return stanje == StanjePrijaveNova || stanje == StanjePrijaveURadu || stanje == StanjePrijaveRijeseno
}

// ImeStranice je naziv stranice kakav je korisnik vidio, a bez njega put
func (p *PrijavaGreske) ImeStranice() string {
	if p.NazivStranice != "" {
		return p.NazivStranice
	}
	return p.Stranica
}

// Naslov je naslov otvorene prijave
func (p *PrijavaGreske) Naslov() string {
	if p.Vrsta == VrstaPrijavePrijedlog {
		return "Prijedlog za stranicu " + p.ImeStranice()
	}
	return "Greška na stranici " + p.ImeStranice()
}

// Poveznica je adresa za „Otvori tu stranicu”, a prazna kad zapis, možda s
// drugog računala, ne vodi na stranicu ovog programa
func (p *PrijavaGreske) Poveznica() string {
	s := p.Stranica
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, `/\`) || strings.ContainsFunc(s, unicode.IsControl) {
		return ""
	}
	return s
}

// dopusteniParametri su izbori kojima se osoba kreće po stranicama: sektor,
// dan, pogled, kartica. Sve ostalo iz upita ne ide u prijavu, jer tamo znaju
// stajati tekst pretrage, nacrt pisma, ime osobe ili kod za prijavu. Parametar
// kojeg nema na popisu gubi samo dio adrese.
var dopusteniParametri = map[string]bool{
	"akt": true, "all": true, "area": true, "category": true, "ceka": true, "centar": true, "cijeli": true, "country": true, "county_id": true,
	"dan": true, "datum": true, "days": true, "dio": true, "dionica": true, "do": true, "god": true, "godina": true, "id": true, "izdanje": true,
	"kind": true, "kisa": true, "korak": true, "lat": true, "letva": true, "ljetno": true, "lon": true, "mapa": true, "mj": true, "mjesec": true,
	"mlat": true, "mlon": true, "modul": true, "moje": true, "nacin": true, "niz": true, "oblik": true, "obrana": true, "ocitanje_id": true, "od": true,
	"odabrana": true, "okno": true, "page": true, "podrucje": true, "pogled": true, "posao": true, "prekida_akt_id": true, "prije": true, "prognoza": true,
	"radnja": true, "razina": true, "red": true, "regija": true, "review": true, "rezultat": true, "role": true, "sat": true, "sector": true, "sektor": true,
	"skladiste": true, "sliv": true, "sredstvo": true, "stanje": true, "station": true, "station_id": true, "status": true, "stranica": true, "structure": true,
	"stupanj": true, "sve": true, "tab": true, "tendencija": true, "type": true, "uredi": true, "used": true, "v": true, "view": true, "voda": true,
	"vodocuvar": true, "vodotok": true, "vrijedi": true, "vrsta": true, "watercourse": true, "year": true, "zemlja": true,
}

// dopustenUpit ostavlja samo dopuštene izbore. Izbor je kratak: duga vrijednost
// nije izbor, čak ni pod dopuštenim imenom.
func dopustenUpit(upit url.Values) url.Values {
	cist := url.Values{}
	for ime, vrijednosti := range upit {
		if !dopusteniParametri[ime] {
			continue
		}
		for _, v := range vrijednosti {
			if len(v) <= najviseVrijednostiUpita {
				cist.Add(ime, v)
			}
		}
	}
	return cist
}

// OcistiStranicu ostavlja put stranice s ovog programa i izbore iz upita, bez
// onoga iza #. Što nije put na ovom programu daje prazno.
func OcistiStranicu(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(u.Path, "/") {
		return ""
	}
	p := PrijavaGreske{Stranica: u.Path}
	if p.Poveznica() == "" {
		return ""
	}
	put := u.EscapedPath()
	upit, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ogranici(put, najviseStranice)
	}
	cijelo := put
	if q := dopustenUpit(upit).Encode(); q != "" {
		cijelo += "?" + q
	}
	if len(cijelo) > najviseStranice {
		return ogranici(put, najviseStranice)
	}
	return cijelo
}

// jedanRedak skuplja razmake i miče upravljačke znakove
func jedanRedak(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// ogranici reže tekst na najviše n bajtova, na granici znaka
func ogranici(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// OcistiNazivStranice sprema naziv stranice u jedan kratak redak
func OcistiNazivStranice(s string) string {
	return ogranici(jedanRedak(s), najviseNazivaStranice)
}

// OcistiZaslon sprema veličinu zaslona; drugo od toga ne smije stići
func OcistiZaslon(s string) string {
	return ogranici(jedanRedak(s), najviseZaslona)
}

var (
	nositeljUGresci   = regexp.MustCompile(`(?i)\bBearer\s+[^\s"']+`)
	osjetljivoUGresci = regexp.MustCompile(`(?i)([\w-]*(?:token|lozink|passw|pwd|pass|secret|tajn|session|sesij|csrf|cookie|kolacic|key|kljuc|ključ|auth|\bpin|\bkod|\bcode|otp|jwt)[\w-]*)\s*[=:]\s*[^&\s"']+`)
	upitUAdresi       = regexp.MustCompile(`\?[^\s"')]*`)
)

// OcistiGreske sprema zadnje greške stranice: najviše pet, svaku u jednom
// kratkom retku i bez upita iz adresa, jer upit može nositi ključ ili token.
func OcistiGreske(greske []string) []string {
	if len(greske) > najviseGresakaStranice {
		greske = greske[len(greske)-najviseGresakaStranice:]
	}
	var ciste []string
	for _, g := range greske {
		g = nositeljUGresci.ReplaceAllString(g, "Bearer …")
		g = osjetljivoUGresci.ReplaceAllString(g, "$1=…")
		g = upitUAdresi.ReplaceAllString(g, "")
		g = ogranici(jedanRedak(g), najviseJedneGreske)
		if g != "" {
			ciste = append(ciste, g)
		}
	}
	return ciste
}

// OcistiTekst miče upravljačke znakove osim novog retka i razmake s rubova
func OcistiTekst(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
