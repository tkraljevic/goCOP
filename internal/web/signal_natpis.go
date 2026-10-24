package web

// Stanje WEB synca prema sastajalištu (plan WEB synca, F2a): je li ovo
// računalo prijavljeno kod sastajališta, a na samom sastajalištu koliko je
// računala prijavljeno. Puni ga signalni klijent iz F2b; dok njega nema,
// stanja nema i pločica o sastajalištu ne piše ništa. Ovdje su sve rečenice
// za F2 i F3, da F3 ne mora dirati web sučelje.
//
// Prolazno stanje (prijava u tijeku, sastajalište ne odgovara) pločica
// pokaže tek kad traje dulje od 10 s, a trajno (nema sastajališta na
// adresi, drugo računalo, nije član, kopija) odmah.

import (
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

// odgodaProlaznog je koliko prolazno stanje traje prije nego dođe na pločicu
const odgodaProlaznog = 10 * time.Second

// najveceOdstupanjeSata: veće odstupanje od sata sastajališta javlja se
const najveceOdstupanjeSata = 2 * time.Minute

// SignalStanje je stanje WEB synca prema sastajalištu
type SignalStanje struct {
	Posluzitelj string // ime sastajališta za tekst, npr. cop.primjer.hr
	Prijavljen  bool
	Od          time.Time // otkad traje ovo stanje: prijava ili kvar
	Kod         razmjena.KodSignala
	PonovnoU    time.Time // kad klijent pokušava ponovno
	// Odbijen: uz KodSignalaKopija ovo je računalo koje sastajalište nije
	// pustilo; ono drugo dobije samo poruku da kopija postoji
	Odbijen        bool
	OdstupanjeSata time.Duration // sat ovog računala prema satu sastajališta
	// na sastajalištu
	Sastajaliste bool
	Prijavljenih int
	PozivaSat    int // poziva u zadnjem satu
	Prekida24h   int
	// WebSync: razmjena ide i WEB syncom; dok ne ide, prijavljenom
	// računalu piše da podaci idu kao prije
	WebSync bool
}

// SetSignalStanje daje poslužitelju stanje sastajališta; zove se prije
// Start
func (s *Server) SetSignalStanje(stanje func() SignalStanje) { s.povezivost.signal = stanje }

// signalStanje je prazno dok ga program ne da; prazno stanje nema ni
// retka ni upozorenja
func (p povezivostCvora) signalStanje() SignalStanje {
	if p.signal == nil {
		return SignalStanje{}
	}
	return p.signal()
}

// stanjeRecenica je redak o WEB syncu za pločicu i Sinkronizaciju; prazno
// kad nije prijavljen i nije sastajalište
func (st SignalStanje) stanjeRecenica() string {
	switch {
	case st.Sastajaliste:
		return fmt.Sprintf("Sastajalište za WEB sync: %d %s, %d %s u zadnjem satu, %d %s u 24 h.",
			st.Prijavljenih, models.UzBrojHR(st.Prijavljenih, "prijavljeno računalo", "prijavljena računala", "prijavljenih računala"),
			st.PozivaSat, models.UzBrojHR(st.PozivaSat, "poziv", "poziva", "poziva"),
			st.Prekida24h, models.UzBrojHR(st.Prekida24h, "prekid", "prekida", "prekida"))
	case st.Prijavljen:
		r := fmt.Sprintf("WEB sync: prijavljen kod %s od %s.", st.Posluzitelj, st.Od.In(models.Zagreb).Format("15:04"))
		if !st.WebSync {
			r += " Podaci još idu kao prije."
		}
		return r
	}
	return ""
}

// recenicaSignala su rečenice za kod signala, s %s na mjestu imena
// sastajališta kad ga trebaju
var recenicaSignala = map[razmjena.KodSignala]string{
	razmjena.KodSignalaSpajaSe:     "WEB sync se prijavljuje kod %s. Podaci idu kao prije.",
	razmjena.KodSignalaNedostupno:  "Sastajalište %s se ne javlja, pa WEB sync čeka. Podaci idu kao prije.",
	razmjena.KodSignalaNemaRute:    "%s nije sastajalište za WEB sync. Na stalnom čvoru uključite posluzi_sastajaliste ili ispravite sastajaliste u gocop.toml.",
	razmjena.KodSignalaKriviKljuc:  "Na adresi %s odgovara drugo računalo, a ne čvor sastajališta iz postavki. Provjerite sastajaliste i sastajaliste_cvor u gocop.toml.",
	razmjena.KodSignalaNijeClan:    "Sastajalište %s ne prima ovo računalo: nije član mreže ili mu je članstvo opozvano.",
	razmjena.KodSignalaBezPostavke: "Prijava za WEB sync je uključena, ali sastajalište nije upisano (sastajaliste i sastajaliste_cvor u gocop.toml).",
}

// upozorenja su rečenice o kvaru WEB synca prema sastajalištu, s odgodom
// za prolazno stanje
func (st SignalStanje) upozorenja(sad time.Time) []string {
	var out []string
	if r := st.kvarRijecima(sad); r != "" {
		out = append(out, r)
	}
	if odst := st.OdstupanjeSata.Abs(); odst > najveceOdstupanjeSata {
		out = append(out, fmt.Sprintf("Sat ovog računala odstupa %d min od poslužitelja. Uključite automatsko podešavanje sata.",
			int(odst.Round(time.Minute).Minutes())))
	}
	return out
}

// kvarRijecima je rečenica za stanje bez prijave; prazno kad je prijavljen,
// kad je sastajalište ili kad prolazno stanje traje kraće od odgode
func (st SignalStanje) kvarRijecima(sad time.Time) string {
	if st.Prijavljen || st.Sastajaliste || st.Kod == "" || !st.Kod.Trajan() && sad.Sub(st.Od) < odgodaProlaznog {
		return ""
	}
	if st.Kod == razmjena.KodSignalaKopija {
		return kopijaRijecima(st, sad)
	}
	return strings.Replace(recenicaSignala[st.Kod], "%s", st.Posluzitelj, 1)
}

// kopijaRijecima: odbijeno računalo zna kad pokušava ponovno, a ono koje je
// ostalo prijavljeno samo da kopija postoji
func kopijaRijecima(st SignalStanje, sad time.Time) string {
	r := "Na još jednom računalu radi kopija ovog goCOP-a. Ugasite kopiju."
	if st.Odbijen {
		r = fmt.Sprintf("Na još jednom računalu radi kopija ovog goCOP-a. Ugasite kopiju; WEB sync ovdje pokušava ponovno za %d min.",
			max(1, int(st.PonovnoU.Sub(sad).Round(time.Minute).Minutes())))
	}
	return r
}

// redakSignala je redak o WEB syncu za pločicu i Sinkronizaciju
func (s *Server) redakSignala() string { return s.povezivost.signalStanje().stanjeRecenica() }
