package web

import "net/url"

// KarticaSredstava je jedna kartica modula Sredstva (crtež 19a): stranica na
// kojoj se stoji nije poveznica.
type KarticaSredstava struct {
	Naziv, URL string
	Odabrana   bool
}

// karticeSredstava su stranice razine sektora redom kojim stoje u traci
var karticeSredstava = []struct{ id, naziv, staza string }{
	{"pregled", "Pregled", "/sredstva"},
	{"promet", "Knjiga prometa", "/sredstva/promet"},
	{"teren", "Na terenu", "/sredstva/na-terenu"},
	{"inventura", "Inventura", "/sredstva/popisi"},
	{"potrebe", "Potrebe za nabavom", "/sredstva/potrebe"},
	{"katalog", "Katalog vrsta", "/sredstva/katalog"},
}

// KarticeModula su kartice ispod sektora; svaka nosi odabrani sektor, pa se
// on ne gubi ni kad se prođe kroz Katalog, koji je isti za sve sektore.
func (d MtsPageData) KarticeModula() []KarticaSredstava {
	out := make([]KarticaSredstava, 0, len(karticeSredstava))
	for _, k := range karticeSredstava {
		adresa := k.staza
		if d.Sektor != "" {
			adresa += "?" + url.Values{"sektor": {d.Sektor}}.Encode()
		}
		out = append(out, KarticaSredstava{Naziv: k.naziv, URL: adresa, Odabrana: k.id == d.Kartica})
	}
	return out
}

// CipoviSektora vode na istu karticu u drugom sektoru. Katalog nema sektora.
func (d MtsPageData) CipoviSektora() []CipPopisa {
	for _, k := range karticeSredstava {
		if k.id == d.Kartica && k.id != "katalog" {
			return cipoviJednogSektora(k.staza, d.Sektori, d.Sektor)
		}
	}
	return nil
}
