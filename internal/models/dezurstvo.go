package models

import "time"

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

// DateKey je dan početka u obliku 2006-01-02, po zidnom satu
func (d Dezurstvo) DateKey() string { return d.Od.In(Zagreb).Format("2006-01-02") }
