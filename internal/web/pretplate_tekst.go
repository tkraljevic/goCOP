package web

import (
	"fmt"
	"strings"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
)

// Rečenice stranice „Što ovo računalo prati” o brisanju s računala. Svaka
// je cijela rečenica za jedan slučaj (CODE_QUALITY.md, pravilo 8), a
// brojevi dolaze iz pregleda brisanja, koji računa ista funkcija koja briše.
// Naziv razine uz broj područja („BP 16”) je iz naziva ustroja (OrgTerms).

// KanalStranice je redak tablice „Što računalo drži”: kanal s nazivom vrste
// i, za kanal koji pretplata više ne pokriva, što bi brisanje odnijelo.
// PovijestMjeseci je koliko mjeseci tuđih očitanja čvor prima (0 = sva).
type KanalStranice struct {
	peers.ChannelStat
	Naziv           string
	Brisanje        *peers.Brisanje
	PovijestMjeseci int
}

// ImaZaBrisanje javlja ima li u kanalu išta što brisanje odnosi
func (k KanalStranice) ImaZaBrisanje() bool {
	return k.Brisanje != nil && k.Brisanje.Zapisa+k.Brisanje.Dokumenata+k.Brisanje.Verzija > 0
}

// BriseSe je rečenica o tome što bi brisanje odnijelo
func (k KanalStranice) BriseSe() string {
	if k.Brisanje == nil {
		return ""
	}
	return recenicaBriseSe(*k.Brisanje)
}

// Ostaje je rečenica o tome što brisanje ostavlja jer je nastalo ovdje;
// prazna kad takvoga nema
func (k KanalStranice) Ostaje() string {
	if k.Brisanje == nil {
		return ""
	}
	return recenicaOstaje(*k.Brisanje)
}

// Potvrda je pitanje prije brisanja, s istim brojevima kao u tablici
func (k KanalStranice) Potvrda() string {
	povratak := "Ono što se briše stiglo je s drugih računala i vratit će se razmjenom kad to opet počnete pratiti."
	if o := k.OgradaPovratka(); o != "" {
		povratak = o
	}
	return fmt.Sprintf("Brisanje s ovog računala: %s, %s %d, %d.\n\n%s %s\n\n%s Obrisati?",
		strings.ToLower(k.Naziv), models.Terms().Get("bp"), k.AreaID, k.Year, k.BriseSe(), k.Ostaje(), povratak)
}

// OgradaPovratka je rečenica za kanal očitanja na računalu koje prima samo
// zadnje mjesece tuđih očitanja: starija se razmjenom ne vraćaju, osim za
// letve koje računalo prati (repository.KeepReadingVersion). Prazna je
// kad toga nema.
func (k KanalStranice) OgradaPovratka() string {
	n := k.PovijestMjeseci
	if n <= 0 || k.Kind != ledger.ChannelReadings {
		return ""
	}
	return fmt.Sprintf(models.UzBrojHR(n,
		"Ono što se briše stiglo je s drugih računala. Kad to opet počnete pratiti, razmjena vrati očitanja iz zadnjeg %d mjeseca, a starija samo za letve koje pratite.",
		"Ono što se briše stiglo je s drugih računala. Kad to opet počnete pratiti, razmjena vrati očitanja iz zadnja %d mjeseca, a starija samo za letve koje pratite.",
		"Ono što se briše stiglo je s drugih računala. Kad to opet počnete pratiti, razmjena vrati očitanja iz zadnjih %d mjeseci, a starija samo za letve koje pratite."), n)
}

// nazivVrste je vrsta kanala riječima, za stupac tablice
func nazivVrste(vrsta string) string {
	switch vrsta {
	case ledger.ChannelReadings:
		return "Očitanja"
	case ledger.ChannelJournals:
		return "Dnevnici radova"
	case ledger.ChannelPrijave:
		return "Prijave s terena"
	case ledger.ChannelVodocuvar:
		return "Vodočuvarski dnevnik"
	}
	return vrsta
}

// vrstaIz je vrsta kanala u genitivu, za „obrisano je … iz …”
func vrstaIz(vrsta string) string {
	switch vrsta {
	case ledger.ChannelReadings:
		return "očitanja"
	case ledger.ChannelJournals:
		return "dnevnika radova"
	case ledger.ChannelPrijave:
		return "prijava s terena"
	case ledger.ChannelVodocuvar:
		return "vodočuvarskog dnevnika"
	}
	return vrsta
}

func zapisa(n int) string     { return models.UzBrojHR(n, "zapis", "zapisa", "zapisa") }
func dokumenata(n int) string { return models.UzBrojHR(n, "dokument", "dokumenta", "dokumenata") }

// recenicaBriseSe: „Obrisat će se” se ne mijenja s brojem, pa je rečenica
// ista za jedan, dva i pet zapisa. Brojevi su s točkom za tisućice
// (1.426 zapisa).
func recenicaBriseSe(b peers.Brisanje) string {
	z, d := b.Zapisa, b.Dokumenata
	switch {
	case z > 0 && d > 0:
		return fmt.Sprintf("Obrisat će se %s %s i %s %s.", brojHR(z), zapisa(z), brojHR(d), dokumenata(d))
	case z > 0:
		return fmt.Sprintf("Obrisat će se %s %s.", brojHR(z), zapisa(z))
	case d > 0:
		return fmt.Sprintf("Obrisat će se %s %s.", brojHR(d), dokumenata(d))
	case b.Verzija > 0:
		return "Obrisat će se samo povijest izmjena s drugih računala."
	}
	return "Ništa se neće obrisati."
}

// recenicaOstaje: brojevi stoje iza dvotočke, pa se glagol s njima ne slaže
func recenicaOstaje(b peers.Brisanje) string {
	return recenicaOstatka(b, "Ostaje ono što je nastalo na ovom računalu: %s %s i %s %s.", "Ostaje ono što je nastalo na ovom računalu: %s %s.")
}

// recenicaOstalo: kao recenicaOstaje, nakon brisanja
func recenicaOstalo(b peers.Brisanje) string {
	return recenicaOstatka(b, "Ostalo je ono što je nastalo na ovom računalu: %s %s i %s %s.", "Ostalo je ono što je nastalo na ovom računalu: %s %s.")
}

// recenicaOstatka bira cijelu rečenicu o onome što ostaje: s zapisima i
// dokumentima (oboje) ili samo s jednim od njih (jedno); prazna kad ne
// ostaje ništa
func recenicaOstatka(b peers.Brisanje, oboje, jedno string) string {
	z, d := b.OstajeZapisa, b.OstajeDokumenata
	switch {
	case z > 0 && d > 0:
		return fmt.Sprintf(oboje, brojHR(z), zapisa(z), brojHR(d), dokumenata(d))
	case z > 0:
		return fmt.Sprintf(jedno, brojHR(z), zapisa(z))
	case d > 0:
		return fmt.Sprintf(jedno, brojHR(d), dokumenata(d))
	}
	return ""
}

// porukaObrisano je poruka nakon brisanja: što je obrisano i, ako je
// išta ostalo, što je ostalo jer je nastalo ovdje
func porukaObrisano(b peers.Brisanje) string {
	poruka := recenicaObrisano(b)
	if ostalo := recenicaOstalo(b); ostalo != "" {
		poruka += " " + ostalo
	}
	return poruka
}

// recenicaObrisano: glagol se slaže s prvim brojem („obrisan je 1 zapis”,
// „obrisana su 2 zapisa”, „obrisano je 5 zapisa”)
func recenicaObrisano(b peers.Brisanje) string {
	z, d := b.Zapisa, b.Dokumenata
	vrsta, podrucje, godina := ledger.SplitChannel(b.Kanal)
	iz, bp := vrstaIz(vrsta), models.Terms().Get("bp")
	jedno := func(n int, rijec string) string {
		return fmt.Sprintf(models.UzBrojHR(n, "S ovog računala obrisan je %s %s iz %s, %s %d, %d.", "S ovog računala obrisana su %s %s iz %s, %s %d, %d.",
			"S ovog računala obrisano je %s %s iz %s, %s %d, %d."), brojHR(n), rijec, iz, bp, podrucje, godina)
	}
	switch {
	case z > 0 && d > 0:
		return fmt.Sprintf(models.UzBrojHR(z, "S ovog računala obrisan je %s %s i %s %s iz %s, %s %d, %d.", "S ovog računala obrisana su %s %s i %s %s iz %s, %s %d, %d.",
			"S ovog računala obrisano je %s %s i %s %s iz %s, %s %d, %d."), brojHR(z), zapisa(z), brojHR(d), dokumenata(d), iz, bp, podrucje, godina)
	case z > 0:
		return jedno(z, zapisa(z))
	case d > 0:
		return jedno(d, dokumenata(d))
	case b.Verzija > 0:
		return fmt.Sprintf("S ovog računala obrisana je samo povijest izmjena iz %s, %s %d, %d.", iz, bp, podrucje, godina)
	}
	return fmt.Sprintf("S ovog računala nije obrisano ništa iz %s, %s %d, %d.", iz, bp, podrucje, godina)
}
