package web

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Upozorenja uz izdanje prognoze: što izdavaču pri izdavanju nije radilo i
// koji su rezultati zbog toga upitni. Izdavač ih zapisuje šiframa uz izdanje
// i ona putuju s njim, pa svaki čvor pokazuje isto; ovdje se samo pišu
// imenima. Jedino zastarjelost izdanja svaki čvor računa sam, po svom satu.

// PrognozaZastarijeva je koliko star sat izdanja smije biti prije nego što
// stranica upozori da novijeg nema: isto kao pločica razmjene na naslovnoj.
const PrognozaZastarijeva = 3 * time.Hour

// UpozorenjeStranice je jedno upozorenje kako ga stranica piše.
type UpozorenjeStranice struct {
	Sto    string // što ne radi
	Upitno string // što je zbog toga upitno; prazno kad ništa posebno
}

// pisacUpozorenja piše upozorenja imenima letvi i danima pregleda.
type pisacUpozorenja struct {
	postaje map[string]models.Station
	letve   []LetvaPrognoze
	dani    []string
}

// upozorenjaStranice slaže upozorenja izdanja za pregled: otvorena,
// sklopljene napomene, zastarjelost i redak na kartici svake letve kojoj je
// prognoza upitna.
func upozorenjaStranice(data *PrognozePageData, c *CitacPrognoza, izdano, sad time.Time, postaje map[string]models.Station) {
	u, zapisano := c.Upozorenja(izdano)
	p := pisacUpozorenja{postaje: postaje, letve: data.Letve, dani: data.Dani}
	data.Upozorenja, data.Napomene = p.slozi(u)
	data.Napomene = append(data.Napomene, bezZapisa(zapisano, data.IzdaoCvor)...)
	data.Zastarjelo = zastarjelo(izdano, sad)
	p.oznaciLetve(u)
}

// bezZapisa je napomena uz izdanje drugog čvora koje upozorenja ne nosi.
func bezZapisa(zapisano bool, izdavac string) []UpozorenjeStranice {
	if zapisano || izdavac == "" {
		return nil
	}
	return []UpozorenjeStranice{{Sto: "Izdavač uz izdanje ne zapisuje upozorenja (starija inačica programa), " +
		"pa se ovdje ne vidi radi li mu sve."}}
}

// zastarjelo upozorava kad je sat izdanja stariji od PrognozaZastarijeva.
func zastarjelo(izdano, sad time.Time) string {
	starost := sad.Sub(izdano)
	if starost <= PrognozaZastarijeva {
		return ""
	}
	return fmt.Sprintf("Zadnja prognoza izdana je za %s — prije %d h; novije izdanje nije stiglo ni izračunato. "+
		"Upitno: sve vrijednosti.", satUpozorenja(izdano), int(starost.Hours()))
}

// satUpozorenja je sat po mjesnom vremenu, npr. „7. 10. u 11 h”.
func satUpozorenja(t time.Time) string {
	t = t.In(models.Zagreb)
	return fmt.Sprintf("%d. %d. u %d h", t.Day(), int(t.Month()), t.Hour())
}

// satOdEpohe pretvara sat od epohe u vrijeme.
func satOdEpohe(sat int64) time.Time { return time.Unix(sat*3600, 0) }

// slozi piše upozorenja: otvorena i sklopljena.
func (p pisacUpozorenja) slozi(u []prognoza.Upozorenje) (otvorena, napomene []UpozorenjeStranice) {
	for _, x := range u {
		s := UpozorenjeStranice{Sto: p.sto(x), Upitno: p.upitno(x)}
		if x.Tezina >= prognoza.TezinaUpozorenje {
			otvorena = append(otvorena, s)
		} else {
			napomene = append(napomene, s)
		}
	}
	return otvorena, napomene
}

// ime je naziv letve bez države; šifra kad je registar ne zna.
func (p pisacUpozorenja) ime(kod string) string {
	st, ima := p.postaje[kod]
	if !ima || st.Name == "" {
		return kod
	}
	ime, _ := imeIDrzava(st.Name)
	return ime
}

// imeSIzvorom je naziv letve s izvorom mjerenja, npr. „Mohács (vizugy.hu)”.
func (p pisacUpozorenja) imeSIzvorom(kod string) string {
	if h := izvorMjerenja(p.postaje[kod].JavniURL); h != "" {
		return p.ime(kod) + " (" + h + ")"
	}
	return p.ime(kod)
}

// izvorMjerenja je poslužitelj javne stranice letve, bez „www.”.
func izvorMjerenja(adresa string) string {
	a, err := url.Parse(strings.TrimSpace(adresa))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(a.Hostname(), "www.")
}

// imena zamijeni šifre letvi u tekstu nazivima; šifra smije stajati u
// zagradi ili uz interpunkciju („(batina):”).
func (p pisacUpozorenja) imena(tekst string) string {
	rijeci := strings.Fields(tekst)
	for i, r := range rijeci {
		kod := strings.Trim(r, "()[]:;,.")
		if _, ima := p.postaje[kod]; ima && kod != "" {
			rijeci[i] = strings.Replace(r, kod, p.ime(kod), 1)
		}
	}
	return strings.Join(rijeci, " ")
}

// opisVrste kaže kako se opis izdavača piše na stranici: što mu se skida s
// početka i čime se uvodi.
var opisVrste = map[string]struct{ prefiks, uvod string }{
	prognoza.VrstaRezerva:       {"rezerva: ", "računa se iz rezerve: "},
	prognoza.VrstaDnevnaRezerva: {"dnevni model: ", "dnevna prognoza iz rezerve: "},
	prognoza.VrstaZamjena:       {"zamjena: ", ""},
}

// uLetvi piše tekst uz letvu („Batina: …”), ili sam kad letve nema.
func (p pisacUpozorenja) uLetvi(letva, tekst string) string {
	if letva == "" {
		return velikoSlovo(tekst) + "."
	}
	return p.ime(letva) + ": " + tekst + "."
}

// sto piše što ne radi.
func (p pisacUpozorenja) sto(u prognoza.Upozorenje) string {
	switch u.Vrsta {
	case prognoza.VrstaUlaz:
		return p.stoUlaza(u)
	case prognoza.VrstaDnevni:
		return "Dnevni model nije računao za " + nabroji(p.imenaDnevnih(u.Upitno, true)) + ": " + p.imena(u.Opis) + "."
	case prognoza.VrstaVrh:
		return p.uLetvi(u.Letva, "vrh lanca, "+p.imena(u.Opis))
	case prognoza.VrstaBezPrognoze:
		return p.uLetvi(u.Letva, "nema prognoze ("+p.imena(u.Opis)+")")
	case prognoza.VrstaCeka:
		return p.uLetvi(u.Letva, "zadnje mjerenje "+satUpozorenja(satOdEpohe(u.Zadnje))+
			"; čeka se, pa sat izdanja stoji iza najsvježijih očitanja ostalih letvi")
	case prognoza.VrstaKrivulja:
		return "Izvan krivulje protoka (krivulja produljena preko svog ruba): " + p.krivulja(u.Upitno) + "."
	}
	// rezerva, zadržano mjerenje, dnevna rezerva, druga obala, kiša i vrste
	// koje ova inačica još ne zna: opis izdavača imenima letvi
	o := opisVrste[u.Vrsta]
	return p.uLetvi(u.Letva, o.uvod+p.imena(strings.TrimPrefix(u.Opis, o.prefiks)))
}

// stoUlaza piše letvu bez mjerenja: od kad do kad, i što je zbog nje stalo.
func (p pisacUpozorenja) stoUlaza(u prognoza.Upozorenje) string {
	s := p.imeSIzvorom(u.Letva) + ": nema mjerenja"
	switch {
	case u.Zadnje > 0 && u.Sljedece > 0:
		s += " od zadnjeg očitanja " + satUpozorenja(satOdEpohe(u.Zadnje)) + " do " + satUpozorenja(satOdEpohe(u.Sljedece))
	case u.Zadnje > 0:
		s += " od " + satUpozorenja(satOdEpohe(u.Zadnje))
	}
	if d := p.imenaDnevnih(u.Upitno, true); len(d) > 0 {
		s += ", pa dnevni model nije računao za " + nabroji(d)
	}
	if slices.ContainsFunc(u.Upitno, func(x prognoza.Upitno) bool { return x.Letva == u.Letva && x.OdSata == 0 }) {
		s += "; vrijednost u satu izdanja izračunata je iz uzvodnih letvi"
	}
	return s + "."
}

// imenaDnevnih su upitne letve s dnevnom prognozom: ciljevi dnevnog modela
// (cilj true) ili srpske letve s prenesenom (cilj false).
func (p pisacUpozorenja) imenaDnevnih(upitno []prognoza.Upitno, cilj bool) []string {
	var out []string
	for _, x := range upitno {
		if x.Dnevno && jeDnevniCilj(x.Letva) == cilj {
			out = append(out, p.ime(x.Letva))
		}
	}
	return out
}

// upitno piše što je upitno: satna po dosezima, dnevna s danima.
func (p pisacUpozorenja) upitno(u prognoza.Upozorenje) string {
	if u.Vrsta == prognoza.VrstaKrivulja {
		return ""
	}
	dijelovi := []string{}
	if s := p.satnaUpitna(u.Upitno); s != "" {
		dijelovi = append(dijelovi, "satna prognoza "+s)
	}
	if d := p.dnevnaUpitna(u); d != "" {
		dijelovi = append(dijelovi, d)
	}
	if len(dijelovi) == 0 {
		return ""
	}
	return "Upitno: " + strings.Join(dijelovi, "; ") + "."
}

// satnaUpitna grupira letve po dosegu od kojeg im je satna upitna, npr.
// „Mohács od izdanja; Batina, Bezdan i Apatin najranije od +1 h”. Doseg je
// najraniji mogući: namješteno kašnjenje vala, bez prozora prosjeka i
// ispravka po zadnjem mjerenju, koji ga samo odgađaju.
func (p pisacUpozorenja) satnaUpitna(upitno []prognoza.Upitno) string {
	po := map[int][]string{}
	var dosezi []int
	for _, x := range upitno {
		if x.OdSata < 0 {
			continue
		}
		if _, bilo := po[x.OdSata]; !bilo {
			dosezi = append(dosezi, x.OdSata)
		}
		po[x.OdSata] = append(po[x.OdSata], p.ime(x.Letva))
	}
	slices.Sort(dosezi)
	grupe := make([]string, 0, len(dosezi))
	for _, d := range dosezi {
		grupe = append(grupe, nabroji(po[d])+" "+odDosega(d))
	}
	return strings.Join(grupe, "; ")
}

// odDosega: 0 je vrijednost u satu izdanja, inače najraniji doseg.
func odDosega(d int) string {
	if d == 0 {
		return "od izdanja"
	}
	return fmt.Sprintf("najranije od +%d h", d)
}

// dnevnaUpitna piše dnevnu prognozu koja nije izdana ili je upitna, i što
// tablica dana zato pokazuje.
func (p pisacUpozorenja) dnevnaUpitna(u prognoza.Upozorenje) string {
	var dijelovi []string
	if c := p.imenaDnevnih(u.Upitno, true); len(c) > 0 {
		dijelovi = append(dijelovi, "dnevna prognoza "+nabroji(c))
	}
	if pr := p.imenaDnevnih(u.Upitno, false); len(pr) > 0 {
		dijelovi = append(dijelovi, "prenesena dnevna "+nabroji(pr))
	}
	if len(dijelovi) == 0 {
		return ""
	}
	s := strings.Join(dijelovi, ", ")
	if u.Vrsta == prognoza.VrstaKisa {
		return s
	}
	return s + p.daniBezDnevne(u.Upitno)
}

// daniBezDnevne kaže koje dane tablica za upitne letve pokazuje iz satne
// prognoze, a koji su prazni.
func (p pisacUpozorenja) daniBezDnevne(upitno []prognoza.Upitno) string {
	izSatne, prazni := map[int]bool{}, map[int]bool{}
	for _, l := range p.letve {
		if !slices.ContainsFunc(upitno, func(x prognoza.Upitno) bool { return x.Dnevno && x.Letva == l.Kod }) {
			continue
		}
		for k, d := range l.Dani {
			izSatne[k] = izSatne[k] || d.IzSatne
			prazni[k] = prazni[k] || d.BezDnevne
		}
	}
	var dijelovi []string
	if s := p.naslovi(izSatne); len(s) > 0 {
		dijelovi = append(dijelovi, nabroji(s)+" stoji satna (oznaka s)")
	}
	if s := p.naslovi(prazni); len(s) > 0 {
		dijelovi = append(dijelovi, nabroji(s)+" bez vrijednosti")
	}
	if len(dijelovi) == 0 {
		return ""
	}
	return " — u tablici " + strings.Join(dijelovi, ", ")
}

// naslovi su naslovi dana označenih u skupu, redom.
func (p pisacUpozorenja) naslovi(dani map[int]bool) []string {
	var out []string
	for k, n := range p.dani {
		if dani[k] {
			out = append(out, n)
		}
	}
	return out
}

// krivulja nabraja letve izvan krivulje po veličini, s dosegom od kojeg.
func (p pisacUpozorenja) krivulja(upitno []prognoza.Upitno) string {
	po := map[string][]string{}
	for _, x := range upitno {
		ime := p.ime(x.Letva)
		if x.OdSata > 0 {
			ime += fmt.Sprintf(" od +%d h", x.OdSata)
		}
		po[x.Velicina] = append(po[x.Velicina], ime)
	}
	velicine := make([]string, 0, len(po))
	for v := range po {
		velicine = append(velicine, v)
	}
	slices.SortFunc(velicine, func(a, b string) int { return cmp.Compare(b, a) }) // vodostaj pa protok
	var dijelovi []string
	for _, v := range velicine {
		dijelovi = append(dijelovi, v+" "+nabroji(po[v]))
	}
	return strings.Join(dijelovi, "; ")
}

// oznaciLetve stavlja na karticu svake letve kojoj je prognoza upitna
// kratak redak s razlogom; samo za otvorena upozorenja.
func (p pisacUpozorenja) oznaciLetve(u []prognoza.Upozorenje) {
	po := map[string][]string{}
	for _, x := range u {
		if x.Tezina < prognoza.TezinaUpozorenje {
			continue
		}
		for _, q := range x.Upitno {
			po[q.Letva] = append(po[q.Letva], p.naKartici(x, q))
		}
	}
	for i := range p.letve {
		p.letve[i].Upozorenje = strings.Join(po[p.letve[i].Kod], " ")
	}
}

// naKartici je redak kartice: što je upitno i zašto.
func (p pisacUpozorenja) naKartici(u prognoza.Upozorenje, q prognoza.Upitno) string {
	var s []string
	if q.OdSata >= 0 {
		s = append(s, "satna upitna "+odDosega(q.OdSata))
	}
	switch {
	case q.Dnevno && u.Vrsta == prognoza.VrstaKisa:
		s = append(s, "dnevna upitna")
	case q.Dnevno:
		s = append(s, "dnevna nije izdana")
	}
	return velikoSlovo(strings.Join(s, ", ")) + " (" + p.uzrok(u) + ")."
}

// uzrok je kratko zašto, za karticu.
func (p pisacUpozorenja) uzrok(u prognoza.Upozorenje) string {
	switch u.Vrsta {
	case prognoza.VrstaUlaz:
		return p.ime(u.Letva) + " bez mjerenja"
	case prognoza.VrstaRezerva:
		return p.ime(u.Letva) + " iz rezerve"
	case prognoza.VrstaDrzi:
		return p.ime(u.Letva) + " bez svježeg ulaza"
	case prognoza.VrstaVrh:
		return p.ime(u.Letva) + " bez budućnosti vrha"
	case prognoza.VrstaKisa:
		return "dnevni model bez kiše"
	}
	return "dnevni model nije računao"
}

// nabroji spaja riječi zarezima, a zadnju s „i”.
func nabroji(s []string) string {
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " i " + s[len(s)-1]
}

// velikoSlovo piše prvo slovo velikim.
func velikoSlovo(s string) string {
	for i, r := range s {
		return strings.ToUpper(string(r)) + s[i+len(string(r)):]
	}
	return s
}
