package models

import (
	"regexp"
	"strings"
	"time"
)

// Dezurstvo je jedan razmak rada jedne osobe u obrani: od–do, što je radila
// i gdje. Plan dežurstava je popis takvih razmaka unaprijed; kad prođu,
// isti zapisi su ulaz za obračun sati (obrazac IORS): osoba, dan, od, do,
// opis, ured ili teren. Zato se ne vode dvije evidencije nego jedna.
//
// Veže se na dnevnik COP-a, jer traje koliko i obrana: kad se dnevnik
// zaključi, zaključen je i plan. Nije samo dežurstvo u centru — isti zapis
// nosi i rukovođenje na terenu, obilazak objekata, administrativni rad —
// sve što obrazac razlikuje.
type Dezurstvo struct {
	ID        string `json:"id"`
	JournalID string `json:"journal_id"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"` // ime u trenutku upisa, da plan ostane čitljiv i bez imenika

	Od time.Time `json:"od"`
	Do time.Time `json:"do"`

	// Podrucje je za koga je osoba radila: branjeno područje, ili prazno za
	// cijeli sektor. To je podatak dežurstva, ne osobe — sektorski čovjek
	// jedan dan radi za jedno područje, a čovjek s područja zna biti poslan
	// drugamo. Obračun se po tome i slaže, kao i dosad po listovima BP-a.
	Podrucje *int `json:"podrucje,omitempty"`

	// Opis je jedan od OpisiRada; Mjesto se iz njega izvodi pri upisu i
	// pohranjuje, da obračun ostane isti i ako se popis opisa promijeni.
	// Zapis kojemu mjesto nije ni ured ni teren obračun čita kroz MjestoRada.
	Opis     string `json:"opis"`
	Mjesto   string `json:"mjesto"` // MjestoUred ili MjestoTeren
	Napomena string `json:"napomena"`

	// Svatko upisuje sebe, od vodočuvara do glavnog rukovoditelja; uprava
	// centra to poslije provjeri i potvrdi. U obračun ulazi samo potvrđeno.
	// Što uprava sama upiše potvrđeno je odmah — ona je ta koja provjerava.
	Potvrdio    string     `json:"potvrdio,omitempty"`
	PotvrdenoAt *time.Time `json:"potvrdeno_at,omitempty"`

	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// RazlogIspravka je razlog ispravka plana nakon ovjere dnevnika COP-a,
	// kako ga uprava centra upiše u obrazac. Ne pohranjuje se i ne ide
	// razmjenom: program ga upisuje u napomenu kao bilješku
	// (TragIspravkaNakonOvjere).
	RazlogIspravka string `json:"-"`
}

// Mjesto rada, kako ga obrazac razlikuje
const (
	MjestoUred  = "URED"
	MjestoTeren = "TEREN"
)

// OpisRada je vrsta posla u obrani s mjestom rada; popis je iz obrasca IORS
type OpisRada struct {
	Opis   string
	Mjesto string
}

// OpisiRada su opisi redom kojim ih obrazac nudi
var OpisiRada = []OpisRada{
	{"Dežurstvo u COP-u", MjestoUred},
	{"Rukovođenje provedbom obrane od poplava sektora u uredu", MjestoUred},
	{"Rukovođenje provedbom obrane od poplava sektora na terenu", MjestoTeren},
	{"Rukovođenje provedbom obrane od poplava branjenog područja u uredu", MjestoUred},
	{"Rukovođenje provedbom obrane od poplava branjenog područja na terenu", MjestoTeren},
	{"Rukovođenje provedbom obrane od poplava dionice", MjestoTeren},
	{"Obilazak i pregled obrambenih objekata", MjestoTeren},
	{"Ostali terenski poslovi pri obrani od poplava", MjestoTeren},
	{"Administrativni rad pri provedbi obrane od poplava", MjestoUred},
	{"Ostali uredski poslovi pri obrani od poplava", MjestoUred},
}

// MjestoZaOpis vraća mjesto rada za opis; prazno kad opis nije s popisa
func MjestoZaOpis(opis string) string {
	for _, o := range OpisiRada {
		if o.Opis == opis {
			return o.Mjesto
		}
	}
	return ""
}

// MjestoRada je mjesto rada za obračun: pohranjeno, kad je ured ili teren;
// inače izvedeno iz opisa, kad je opis s popisa. Prazno kad ni opis nije s
// popisa (zapis s drugog čvora ili stariji zapis bez mjesta): tada se ne zna
// je li ured ili teren, pa se ništa ne pretpostavlja.
func (d Dezurstvo) MjestoRada() string {
	if d.Mjesto == MjestoUred || d.Mjesto == MjestoTeren {
		return d.Mjesto
	}
	return MjestoZaOpis(d.Opis)
}

// ZaProvjeru javlja da se mjesto rada ne zna: takvo dežurstvo ne ulazi u
// obračun i ne potvrđuje se dok uprava centra ne izabere opis s popisa
func (d Dezurstvo) ZaProvjeru() bool { return d.MjestoRada() == "" }

// reTragIspravka prepoznaje bilješku koju u napomeni ostavlja ispravak
// opisa rada: „[opis ispravio Ana Anić 8.10.2026. 14:20; prije: „…”]”
var reTragIspravka = regexp.MustCompile(`\[opis ispravio .*?; prije: „.*?”\]`)

// TragoviIspravkaOpisa su bilješke o ispravku opisa rada iz napomene, redom
func TragoviIspravkaOpisa(napomena string) []string {
	return reTragIspravka.FindAllString(napomena, -1)
}

// reTragNakonOvjere prepoznaje bilješku koju u napomeni ostavlja ispravak
// plana nakon ovjere dnevnika COP-a: „[ispravak nakon ovjere: Ana Anić
// 8.10.2026. 14:20; razlog: „…”]”
var reTragNakonOvjere = regexp.MustCompile(`\[ispravak nakon ovjere: .*?; razlog: „.*?”\]`)

// TragoviIspravkaNakonOvjere su bilješke o ispravku plana nakon ovjere
// dnevnika COP-a iz napomene, redom
func TragoviIspravkaNakonOvjere(napomena string) []string {
	return reTragNakonOvjere.FindAllString(napomena, -1)
}

// TragIspravkaNakonOvjere je bilješka o ispravku plana nakon ovjere: tko,
// kada i zašto (Odluke (30. 10. 2026.), 5). Iz razloga se izbacuju zagrade i
// navodnici kojima bilješka počinje i završava, da je program uvijek
// prepozna cijelu.
func TragIspravkaNakonOvjere(tko string, kad time.Time, razlog string) string {
	razlog = strings.Join(strings.Fields(strings.NewReplacer("[", "(", "]", ")", "„", "\"", "”", "\"").Replace(razlog)), " ")
	return "[ispravak nakon ovjere: " + tko + " " + kad.Format("2.1.2006. 15:04") + "; razlog: „" + razlog + "”]"
}

// OpisBiraUprava javlja da opis rada ovog dežurstva bira uprava centra:
// kad se ne zna mjesto rada, i kad je opis već ispravila (u napomeni je
// trag ispravka). Osoba čije je dežurstvo takav opis ne mijenja i zapis ne
// miče — inače bi napomena upravi pripisala opis koji nije birala, a zapis
// bi se dao maknuti i upisati ponovno bez traga (Odluke (8. 10. 2026.),
// pitanje 10).
func (d Dezurstvo) OpisBiraUprava() bool {
	return d.ZaProvjeru() || reTragIspravka.MatchString(d.Napomena)
}

// OznakaZaProvjeru stoji umjesto mjesta rada kad se mjesto ne zna
const OznakaZaProvjeru = "mjesto nepoznato — za provjeru"

// NazivMjesta je mjesto rada kako ga plan, obračun i IORS ispisuju
func NazivMjesta(mjesto string) string {
	switch mjesto {
	case MjestoTeren:
		return "teren"
	case MjestoUred:
		return "ured"
	}
	return OznakaZaProvjeru
}

// MjestoTekst je mjesto rada dežurstva za ispis
func (d Dezurstvo) MjestoTekst() string { return NazivMjesta(d.MjestoRada()) }

// ZaPodrucje javlja je li dežurstvo vezano na jedno branjeno područje
func (d Dezurstvo) ZaPodrucje() bool { return d.Podrucje != nil && *d.Podrucje > 0 }

// PodrucjeID je broj područja, 0 za cijeli sektor — za usporedbu u predlošku
func (d Dezurstvo) PodrucjeID() int {
	if d.ZaPodrucje() {
		return *d.Podrucje
	}
	return 0
}

// Potvrdeno javlja je li dežurstvo provjerila uprava centra
func (d Dezurstvo) Potvrdeno() bool { return d.PotvrdenoAt != nil }

// Trajanje razmaka
func (d Dezurstvo) Trajanje() time.Duration { return d.Do.Sub(d.Od) }

// NajduljeBezStanke: dežurstvo dulje od ovoga bez stanke je sumnjivo (čovjek
// treba i spavati), pa ga uprava mora pogledati prije potvrde
const NajduljeBezStanke = 24 * time.Hour

// BezStanke javlja traje li dežurstvo dulje od NajduljeBezStanke
func (d Dezurstvo) BezStanke() bool { return d.Trajanje() > NajduljeBezStanke }

// DateKey je dan početka u obliku 2006-01-02, po zidnom satu
func (d Dezurstvo) DateKey() string { return d.Od.In(Zagreb).Format("2006-01-02") }

// PlanOsobe je jedan plan u kojem osoba ima dežurstva — za profil, gdje
// osoba vidi svoje planove i izvješća o satima bez traženja po dnevnicima
type PlanOsobe struct {
	JournalID    string
	Naslov       string
	Centar       string
	Sektor       string
	Zakljucen    bool
	Dezurstava   int
	CekaPotvrdu  int // nepotvrđena dežurstva s poznatim mjestom rada
	ZaProvjeru   int // dežurstva kojima se ne zna mjesto rada, potvrđena ili ne
	Sati         time.Duration
	Prvo, Zadnje time.Time
}

// Dodaj pribraja dežurstvo planu: broj, sate te prvi i zadnji dan. Što nije
// u obračunu broji se posebno, kao u obračunu i IORS-u: dežurstvo kojemu se
// ne zna mjesto rada je „za provjeru” i kad je potvrđeno, a nepotvrđeno
// takvo ne čeka potvrdu, jer se ne potvrđuje dok uprava centra ne izabere
// opis (Odluke (8. 10. 2026.), pitanje 10).
func (p *PlanOsobe) Dodaj(d Dezurstvo) {
	if p.Dezurstava == 0 || d.Od.Before(p.Prvo) {
		p.Prvo = d.Od
	}
	if p.Dezurstava == 0 || d.Do.After(p.Zadnje) {
		p.Zadnje = d.Do
	}
	p.Dezurstava++
	p.Sati += d.Trajanje()
	switch {
	case d.ZaProvjeru():
		p.ZaProvjeru++
	case !d.Potvrdeno():
		p.CekaPotvrdu++
	}
}
