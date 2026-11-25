package web

import (
	"fmt"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// StanjeTjedne je ono što program zna o tjednoj pripremi lanca: zadnji
// pokušaji (najnoviji prvi), kad je sljedeća, izdaje li ovo računalo
// prognozu i nazivi letvi po šifri.
type StanjeTjedne struct {
	Pripreme []prognoza.TjednaPriprema
	Sljedeca time.Time
	Izdaje   bool
	Nazivi   map[string]string
}

// TjednaPripremaPrikaz je blok „Tjedna priprema” u Postavkama prognoze
// (crteži tjedna_*).
type TjednaPripremaPrikaz struct {
	Izdaje      bool
	Oznaka      string // čip uz naslov; prazno bez njega
	OznakaPozor bool   // čip je žut
	Upozorenje  string // žuti okvir; prazno bez njega
	Zadnja      string
	Promasaj    string // redak promašaja; prazno bez njega
	Bolje       bool   // uz promašaj stoji „bolje”
	Losije      string // „lošije: Botovo +3 cm, …”; prazno bez njega
	Sljedeca    string
}

// SetTjednaPriprema daje stranicama stanje tjedne pripreme lanca.
func (s *Server) SetTjednaPriprema(f func() StanjeTjedne) { s.tjedna = f }

// redakModela je redak Za riješiti s upozorenjem tjedne pripreme; prazno
// bez upozorenja.
func redakModela(upozorenje string) []RedakKartice {
	if upozorenje == "" {
		return nil
	}
	return []RedakKartice{{Ikona: "triangle-alert", Glavno: upozorenje, Sporedno: "Postavke prognoze", URL: "/prognoze/postavke#priprema"}}
}

// upozorenjeModela je upozorenje tjedne pripreme za Za riješiti; prazno
// bez njega.
func upozorenjeModela(tjedna func() *StanjeTjedne) string {
	if tjedna == nil {
		return ""
	}
	if st := tjedna(); st != nil {
		return prognoza.UpozorenjeTjedne(st.Pripreme)
	}
	return ""
}

// tjedniPrikaz je blok „Tjedna priprema” za Postavke prognoze; nil kad ga
// osoba ne vidi ili program ne daje stanje.
func tjedniPrikaz(vidi bool, tjedna func() *StanjeTjedne) *TjednaPripremaPrikaz {
	if !vidi || tjedna == nil {
		return nil
	}
	st := tjedna()
	if st == nil {
		return nil
	}
	p := PrikazTjedne(*st)
	return &p
}

// stanjeTjedne je stanje tjedne pripreme; nil dok ga program ne postavi.
func (s *Server) stanjeTjedne() *StanjeTjedne {
	if s.tjedna == nil {
		return nil
	}
	st := s.tjedna()
	return &st
}

// termin je „subota 22. 11. 2026. u 2:00”.
func terminTjedne(t time.Time) string {
	l := t.In(models.Zagreb)
	return fmt.Sprintf("subota %s u %d:%02d", l.Format("2. 1. 2006."), l.Hour(), l.Minute())
}

// cmTjedne je promašaj s jednom decimalom i decimalnim zarezom.
func cmTjedne(v float64) string { return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1) }

// PrikazTjedne slaže blok iz stanja.
func PrikazTjedne(s StanjeTjedne) TjednaPripremaPrikaz {
	p := TjednaPripremaPrikaz{Izdaje: s.Izdaje}
	if !s.Izdaje {
		return p
	}
	p.Sljedeca = "Sljedeća: " + terminTjedne(s.Sljedeca)
	if len(s.Pripreme) == 0 {
		p.Zadnja = "Zadnja: još nije bilo"
		return p
	}
	z := s.Pripreme[0]
	zadnjaTjedna[z.Ishod](&p, z, s.Nazivi)
	upozorenjeTjedne(&p, s.Pripreme)
	return p
}

// zadnjaTjedna slaže retke zadnje pripreme po njezinu ishodu.
var zadnjaTjedna = map[string]func(*TjednaPripremaPrikaz, prognoza.TjednaPriprema, map[string]string){
	prognoza.TjednaPreskocena: func(p *TjednaPripremaPrikaz, z prognoza.TjednaPriprema, _ map[string]string) {
		p.Oznaka, p.Zadnja = "preskočena", "Preskočena: "+z.Razlog
	},
	prognoza.TjednaGreska: func(p *TjednaPripremaPrikaz, z prognoza.TjednaPriprema, _ map[string]string) {
		p.Oznaka, p.OznakaPozor = "nije uspjela", true
		p.Zadnja = "Zadnja: " + terminTjedne(z.Kad) + " · nije uspjela: " + z.Razlog
	},
	prognoza.TjednaNijePrimijenjena: func(p *TjednaPripremaPrikaz, z prognoza.TjednaPriprema, nazivi map[string]string) {
		p.Oznaka, p.OznakaPozor = "nije primijenjena", true
		p.Zadnja = fmt.Sprintf("Zadnja: %s · nije primijenjena: novi lanac bio je lošiji (%s → %s cm)%s",
			terminTjedne(z.Kad), cmTjedne(z.Prije), cmTjedne(z.Poslije), ostajeLanac(z.LanacOd))
		p.Losije = losijeLetve(z.Losije, nazivi)
	},
	prognoza.TjednaPrimijenjena: func(p *TjednaPripremaPrikaz, z prognoza.TjednaPriprema, _ map[string]string) {
		p.Zadnja = "Zadnja: " + terminTjedne(z.Kad) + " · primijenjena"
		if cmTjedne(z.Prije) == cmTjedne(z.Poslije) {
			p.Zadnja += fmt.Sprintf(", ali bez pomaka (%s → %s cm)", cmTjedne(z.Prije), cmTjedne(z.Poslije))
			return
		}
		p.Promasaj = fmt.Sprintf("promašaj 1.–4. dan: %s → %s cm", cmTjedne(z.Prije), cmTjedne(z.Poslije))
		p.Bolje = z.Poslije < z.Prije
	},
}

// ostajeLanac je „, ostaje lanac od 15. 11.”; prazno kad se ne zna.
func ostajeLanac(od time.Time) string {
	if od.IsZero() {
		return ""
	}
	return ", ostaje lanac od " + od.In(models.Zagreb).Format("2. 1.")
}

// upozorenjeTjedne doda čip i žuti okvir kad lanac slabi ili tri pripreme
// zaredom nisu prošle probu.
func upozorenjeTjedne(p *TjednaPripremaPrikaz, pripreme []prognoza.TjednaPriprema) {
	switch prognoza.UpozorenjeTjedne(pripreme) {
	case prognoza.UpozorenjeSlabi:
		m := prognoza.MjerenePripreme(pripreme)[0]
		p.Oznaka, p.OznakaPozor = "potreban pregled", true
		p.Upozorenje = fmt.Sprintf("Prognoza je lošija nego kad je lanac primijenjen (promašaj 1.–4. dan %s → %s cm), "+
			"a novi lanac ne pomaže. Potreban je pregled modela.", cmTjedne(m.Osnovica), cmTjedne(m.Prije))
	case prognoza.UpozorenjeZaredom:
		p.Oznaka, p.OznakaPozor = fmt.Sprintf("%d puta zaredom", prognoza.TjednaZaredom), true
		p.Upozorenje = fmt.Sprintf("Tjedna priprema %d puta zaredom nije prošla probu. Provjerite arhivu (moguće krivi "+
			"podaci na letvi) ili pokrenite pripremu ručno.", prognoza.TjednaZaredom)
	}
}

// losijeLetve je „lošije: Botovo +3 cm, Terezino Polje +2 cm”.
func losijeLetve(l []prognoza.LosijaLetva, nazivi map[string]string) string {
	if len(l) == 0 {
		return ""
	}
	var dijelovi []string
	for _, x := range l {
		ime := nazivi[x.Letva]
		if ime == "" {
			ime = x.Letva
		}
		jed := "cm"
		if x.Velicina == "protok" {
			jed = "m³/s"
		}
		dijelovi = append(dijelovi, fmt.Sprintf("%s +%.0f %s", ime, x.Razlika, jed))
	}
	return "lošije: " + strings.Join(dijelovi, ", ")
}
