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

// sviSektori je vrijednost parametra sektor kad pregled zbraja sve sektore
// (Odluke 16. 11. 2026., 4); prazan parametar znači sektor korisnika.
const sviSektori = "svi"

// CipoviSektora vode na istu karticu u drugom sektoru. Katalog nema sektora,
// a „Svi sektori” ima samo pregled: ostale kartice rade sa skladištima jednog sektora.
func (d MtsPageData) CipoviSektora() []CipPopisa {
	for _, k := range karticeSredstava {
		if k.id != d.Kartica || k.id == "katalog" {
			continue
		}
		cipovi := cipoviJednogSektora(k.staza, d.Sektori, d.Sektor)
		if k.id == "pregled" {
			svi := CipPopisa{Naziv: "Svi sektori", URL: k.staza + "?sektor=" + sviSektori, Odabran: d.SviSektori}
			cipovi = append([]CipPopisa{svi}, cipovi...)
		}
		return cipovi
	}
	return nil
}
