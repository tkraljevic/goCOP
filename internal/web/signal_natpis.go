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
	"net/netip"
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
	// Posluzitelj je domena sastajališta za tekst, npr. cop.primjer.hr. IP
	// adresa ne ide na pločicu, pa se tada piše „sastajališta”.
	Posluzitelj string
	Prijavljen  bool
	Od          time.Time // otkad traje ovo stanje: prijava ili kvar
	Kod         razmjena.KodSignala
	PonovnoU    time.Time // kad klijent pokušava ponovno
	// Odbijen: uz KodSignalaKopija ovo je računalo koje sastajalište nije
	// pustilo; ono koje je ostalo prijavljeno dobije samo poruku da kopija
	// postoji
	Odbijen        bool
	OdstupanjeSata time.Duration // sat ovog računala prema satu sastajališta
	// na sastajalištu
	Sastajaliste bool
	Prijavljenih int
	PozivaSat    int // poziva u zadnjem satu
	Prekida24h   int
	Kopije       []string // računala čiju je kopiju sastajalište nedavno odbilo
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

// bezImenaSastajalista stoji u rečenici kad sastajalište nema domenu
const bezImenaSastajalista = "sastajališta"

// imeSastajalista je domena sastajališta za tekst. Prazno, IP adresa i
// adresa s portom ne idu na pločicu, jer je vidi svaki prijavljeni
// korisnik; sve rečenice zato ime imaju u genitivu („kod sastajališta”).
func (st SignalStanje) imeSastajalista() string {
	h := st.Posluzitelj
	if h == "" || strings.ContainsAny(h, ":[]") {
		return bezImenaSastajalista
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return bezImenaSastajalista
	}
	return h
}

// odKad je početak prijave: sat, a kad nije od danas, i dan
func odKad(t, sad time.Time) string {
	t = t.In(models.Zagreb)
	if t.Format("2006-01-02") == sad.In(models.Zagreb).Format("2006-01-02") {
		return t.Format("15:04")
	}
	return t.Format("2. 1. u 15:04")
}

// stanjeRecenica je redak o WEB syncu za pločicu i Sinkronizaciju; prazno
// kad nije prijavljen i nije sastajalište
func (st SignalStanje) stanjeRecenica(sad time.Time) string {
	switch {
	case st.Sastajaliste:
		return fmt.Sprintf("Sastajalište za WEB sync: %d %s, %d %s u zadnjem satu, %d %s u 24 h.",
			st.Prijavljenih, models.UzBrojHR(st.Prijavljenih, "prijavljeno računalo", "prijavljena računala", "prijavljenih računala"),
			st.PozivaSat, models.UzBrojHR(st.PozivaSat, "poziv", "poziva", "poziva"),
			st.Prekida24h, models.UzBrojHR(st.Prekida24h, "prekid", "prekida", "prekida"))
	case st.Prijavljen:
		r := fmt.Sprintf("WEB sync: prijavljen kod %s od %s.", st.imeSastajalista(), odKad(st.Od, sad))
		if !st.WebSync {
			r += " Podaci još idu kao prije."
		}
		return r
	}
	return ""
}

// recenicaSignala su rečenice za kod signala, s %s na mjestu imena
// sastajališta u genitivu kad ga trebaju
var recenicaSignala = map[razmjena.KodSignala]string{
	razmjena.KodSignalaSpajaSe:    "WEB sync se prijavljuje kod %s. Podaci idu kao prije.",
	razmjena.KodSignalaNedostupno: "Od %s nema odgovora, pa WEB sync čeka. Podaci idu kao prije.",
	razmjena.KodSignalaNemaRute: "WEB sync se ne može prijaviti kod %s, jer ondje sastajalište ne radi. Na stalnom čvoru uključite " +
		"posluzi_sastajaliste i prijavu za WEB sync i nadogradite goCOP, ili ispravite sastajaliste u gocop.toml.",
	razmjena.KodSignalaKriviKljuc: "Na adresi %s javlja se drugo računalo, a ne čvor sastajališta iz postavki. Provjerite sastajaliste " +
		"i sastajaliste_cvor u gocop.toml.",
	razmjena.KodSignalaNijeClan: "WEB sync se ne može prijaviti kod %s, jer ovo računalo nije član mreže ili mu je članstvo opozvano. " +
		"Administrator ga treba ponovno primiti u mrežu.",
	razmjena.KodSignalaBezPostavke: "Prijava za WEB sync je uključena, ali sastajalište nije upisano (sastajaliste i sastajaliste_cvor u gocop.toml).",
}

// upozorenja su rečenice o kvaru WEB synca prema sastajalištu, s odgodom
// za prolazno stanje
func (st SignalStanje) upozorenja(sad time.Time) []string {
	var out []string
	if r := st.kvarRijecima(sad); r != "" {
		out = append(out, r)
	}
	for _, ime := range st.Kopije {
		out = append(out, "Na još jednom računalu radi kopija goCOP-a s računala "+ime+"; sastajalište ju je odbilo. Ugasite kopiju.")
	}
	if odst := st.OdstupanjeSata.Abs(); odst > najveceOdstupanjeSata {
		out = append(out, fmt.Sprintf("Sat ovog računala odstupa %d min od poslužitelja. Uključite automatsko podešavanje sata.",
			int(odst.Round(time.Minute).Minutes())))
	}
	return out
}

// kvarRijecima je rečenica za kvar prijave; prazno kad je prijavljen, kad
// je sastajalište ili kad prolazno stanje traje kraće od odgode. Kopija se
// javlja i računalu koje je ostalo prijavljeno.
func (st SignalStanje) kvarRijecima(sad time.Time) string {
	switch {
	case st.Kod == razmjena.KodSignalaKopija:
		return kopijaRijecima(st, sad)
	case st.Prijavljen || st.Sastajaliste || st.Kod == "" || !st.Kod.Trajan() && sad.Sub(st.Od) < odgodaProlaznog:
		return ""
	}
	return strings.Replace(recenicaSignala[st.Kod], "%s", st.imeSastajalista(), 1)
}

// kopijaRijecima: odbijeno računalo zna kad pokušava ponovno, a ono koje je
// ostalo prijavljeno samo da kopija postoji
func kopijaRijecima(st SignalStanje, sad time.Time) string {
	r := "Na još jednom računalu radi kopija ovog goCOP-a. Ugasite kopiju."
	if st.Odbijen && !st.PonovnoU.IsZero() {
		r = fmt.Sprintf("Na još jednom računalu radi kopija ovog goCOP-a. Ugasite kopiju; WEB sync ovdje pokušava ponovno za %d min.",
			max(1, int(st.PonovnoU.Sub(sad).Round(time.Minute).Minutes())))
	}
	return r
}

// redakSignala je redak o WEB syncu za pločicu i Sinkronizaciju
func (s *Server) redakSignala() string { return s.povezivost.signalStanje().stanjeRecenica(time.Now()) }
