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
	letve  []string
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
	return []UpozorenjeStranice{{Sto: "Računalo koje je izdalo prognozu ima stariji program i ne šalje upozorenja, " +
		"pa se ne vidi je li mu sve radilo."}}
}

// zastarjelo upozorava kad je sat izdanja stariji od PrognozaZastarijeva.
func zastarjelo(izdano, sad time.Time) string {
	starost := sad.Sub(izdano)
	if starost <= PrognozaZastarijeva {
		return ""
	}
	return fmt.Sprintf("Zadnja prognoza je za %s, prije %d h. Novija nije stigla ni izračunata, "+
		"pa su sve vrijednosti upitne.", satUpozorenja(izdano), int(starost.Hours()))
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
		s := UpozorenjeStranice{Sto: p.sto(x), Upitno: p.upitno(x), letve: letveUpozorenja(x)}
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
	prognoza.VrstaRezerva:        {"rezerva: ", "računa se iz rezerve: "},
	prognoza.VrstaDnevnaRezerva:  {"dnevni model: ", "dnevna prognoza iz rezerve: "},
	prognoza.VrstaZamjena:        {"zamjena: ", ""},
	prognoza.VrstaRekonstrukcija: {"rekonstrukcija: ", "rekonstruirano iz susjeda: "},
}

// uLetvi piše tekst uz letvu („Batina: …”), ili sam kad letve nema.
func (p pisacUpozorenja) uLetvi(letva, tekst string) string {
	if letva == "" {
		return recenica(velikoSlovo(tekst))
	}
	return recenica(p.ime(letva) + ": " + tekst)
}

// recenica završava tekst točkom; opis izdavača koji već završava datumom
// („do 29. 10.”) ne dobiva drugu.
func recenica(s string) string {
	if strings.HasSuffix(s, ".") {
		return s
	}
	return s + "."
}

// sto piše što ne radi.
func (p pisacUpozorenja) sto(u prognoza.Upozorenje) string {
	switch u.Vrsta {
	case prognoza.VrstaUlaz:
		return p.stoUlaza(u)
	case prognoza.VrstaDnevni:
		return recenica("Dnevni model nije računao za " + nabroji(p.imenaDnevnih(u.Upitno, true)) + ": " + p.imena(u.Opis))
	case prognoza.VrstaVrh:
		return p.uLetvi(u.Letva, "vrh lanca, "+p.imena(u.Opis))
	case prognoza.VrstaBezPrognoze:
		return p.uLetvi(u.Letva, "nema prognoze ("+p.imena(u.Opis)+")")
	case prognoza.VrstaCeka:
		return p.uLetvi(u.Letva, "zadnje mjerenje je od "+satUpozorenja(satOdEpohe(u.Zadnje))+
			". Prognoza ga čeka, pa je izdana za raniji sat nego što ostale letve već imaju")
	case prognoza.VrstaKrivulja:
		return "Izvan krivulje protoka (krivulja je produžena preko ruba): " + p.krivulja(u.Upitno) + "."
	case prognoza.VrstaUzvodno:
		return p.stoUzvodnog(u)
	}
	// rezerva, zadržano mjerenje, dnevna rezerva, druga obala, susjedi, kiša i vrste
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

// stoUzvodnog piše napomenu uzvodnog ispravka kako ju je izdavač napisao:
// uz letvu s izvorom mjerenja koje mjerenje fali i kako se zato računa
// dnevna prognoza Dunava, a bez letve da je ispravak digao prognozu.
func (p pisacUpozorenja) stoUzvodnog(u prognoza.Upozorenje) string {
	if u.Letva == "" {
		return recenica(velikoSlovo(p.imena(u.Opis)))
	}
	return recenica(p.imeSIzvorom(u.Letva) + ": " + p.imena(u.Opis))
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
	if dnevnaIzdana(u.Vrsta) {
		return s
	}
	return s + p.daniBezDnevne(u.Upitno)
}

// dnevnaIzdana: uz ovu vrstu dnevna prognoza jest izdana, samo je upitna —
// računata bez kiše ili iz rekonstruiranih sati izvan raspona provjere.
func dnevnaIzdana(vrsta string) bool {
	return vrsta == prognoza.VrstaKisa || vrsta == prognoza.VrstaRekonstrukcija
}

// daniBezDnevne kaže koje dane tablica za upitne letve pokazuje iz satne
// prognoze, a koji su prazni. Dane koje sve te letve ne pokazuju jednako
// piše uz letve koje ih tako pokazuju.
func (p pisacUpozorenja) daniBezDnevne(upitno []prognoza.Upitno) string {
	var letve []LetvaPrognoze
	for _, l := range p.letve {
		if slices.ContainsFunc(upitno, func(x prognoza.Upitno) bool { return x.Dnevno && x.Letva == l.Kod }) {
			letve = append(letve, l)
		}
	}
	dijelovi := slices.Concat(
		p.daniPoLetvama(letve, func(c CelijaDana) bool { return c.IzSatne }, "stoji satna (oznaka s)"),
		p.daniPoLetvama(letve, func(c CelijaDana) bool { return c.BezDnevne }, "bez vrijednosti"))
	if len(dijelovi) == 0 {
		return ""
	}
	return " — u tablici " + strings.Join(dijelovi, ", ")
}

// daniPoLetvama skuplja letve po danima kojima je ćelija označena, npr.
// „ned 11.10. i pon 12.10. stoji satna (oznaka s) za letvu Osijek”; kad
// iste dane imaju sve zadane letve, imena se ne pišu.
func (p pisacUpozorenja) daniPoLetvama(letve []LetvaPrognoze, oznacena func(CelijaDana) bool, sto string) []string {
	po := map[string][]string{} // dani → imena letvi
	var redom []string
	for _, l := range letve {
		dani := map[int]bool{}
		for k, d := range l.Dani {
			dani[k] = oznacena(d)
		}
		n := nabroji(p.naslovi(dani))
		if n == "" {
			continue
		}
		if _, bilo := po[n]; !bilo {
			redom = append(redom, n)
		}
		po[n] = append(po[n], p.ime(l.Kod))
	}
	out := make([]string, 0, len(redom))
	for _, n := range redom {
		out = append(out, n+" "+sto+zaLetve(po[n], len(letve)))
	}
	return out
}

// zaLetve imenuje letve uz dane, osim kad su to sve od njih ukupno.
func zaLetve(imena []string, ukupno int) string {
	switch {
	case len(imena) == ukupno:
		return ""
	case len(imena) == 1:
		return " za letvu " + imena[0]
	}
	return " za letve " + nabroji(imena)
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
	case q.Dnevno && dnevnaIzdana(u.Vrsta):
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
	case prognoza.VrstaRekonstrukcija:
		return p.ime(u.Letva) + " rekonstruirana izvan provjerenog raspona"
	case prognoza.VrstaDnevni:
		return "dnevni model nije računao"
	}
	// Vrsta koju ova inačica ne zna: s izdavača novije inačice, ili vrsta
	// koje više nema (npr. „bez-tude”, Komárom bez mađarske prognoze). Razlog
	// se ne nagađa: stoji ime letve, a opis izdavača je u okviru upozorenja.
	return cmp.Or(p.ime(u.Letva), "vidi upozorenja uz izdanje")
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

// letveUpozorenja su letve koje upozorenje imenuje: ona koja ne radi i one
// kojima je prognoza upitna.
func letveUpozorenja(u prognoza.Upozorenje) []string {
	out := []string{u.Letva}
	for _, q := range u.Upitno {
		out = append(out, q.Letva)
	}
	return out
}

// upozorenjaIzvoza su zastarjelost i otvorena upozorenja koja imenuju
// ijednu od zadanih letvi, za napomenu lista izvoza; svako u svom retku.
func upozorenjaIzvoza(data PrognozePageData, letve []LetvaPrognoze) string {
	var s []string
	if data.Zastarjelo != "" {
		s = append(s, data.Zastarjelo)
	}
	for _, u := range data.Upozorenja {
		if slices.ContainsFunc(letve, func(l LetvaPrognoze) bool { return slices.Contains(u.letve, l.Kod) }) {
			s = append(s, "Upozorenje: "+strings.TrimSpace(u.Sto+" "+u.Upitno))
		}
	}
	if len(s) == 0 {
		return ""
	}
	return strings.Join(s, "\n") + "\n"
}

// upozorenjaSazetka su zastarjelost i sva otvorena upozorenja izdanja, za
// odlomak na početku sažetka; prazno kad ih nema.
func upozorenjaSazetka(data PrognozePageData) string {
	var s []string
	if data.Zastarjelo != "" {
		s = append(s, data.Zastarjelo)
	}
	for _, u := range data.Upozorenja {
		s = append(s, strings.TrimSpace(u.Sto+" "+u.Upitno))
	}
	if len(s) == 0 {
		return ""
	}
	return "Upozorenja uz izdanje: " + strings.Join(s, " ")
}

// neprazno su zadani tekstovi bez praznih.
func neprazno(s ...string) []string {
	return slices.DeleteFunc(s, func(x string) bool { return x == "" })
}

// upitnoLetve je kratak redak bez imena za natpis grafa letve: od kojeg je
// dosega satna upitna i je li dnevna izdana; prazno kad je sve u redu.
func upitnoLetve(u []prognoza.Upozorenje, letva string) string {
	od, dnevna := -1, ""
	for _, x := range u {
		q, ima := upitnaLetva(x, letva)
		if !ima {
			continue
		}
		od = najranije(od, q.OdSata)
		dnevna = cmp.Or(dnevna, dnevnaNaGrafu(x.Vrsta, q))
	}
	var s []string
	if od >= 0 {
		s = append(s, "satna upitna "+odDosega(od))
	}
	return velikoSlovo(strings.Join(append(s, neprazno(dnevna)...), "; "))
}

// upitnaLetva nalazi letvu među upitnima otvorenog upozorenja.
func upitnaLetva(u prognoza.Upozorenje, letva string) (prognoza.Upitno, bool) {
	i := slices.IndexFunc(u.Upitno, func(q prognoza.Upitno) bool { return q.Letva == letva })
	if i < 0 || u.Tezina < prognoza.TezinaUpozorenje {
		return prognoza.Upitno{}, false
	}
	return u.Upitno[i], true
}

// najranije je manji od dva dosega; −1 znači da dosega nema.
func najranije(a, b int) int {
	if b >= 0 && (a < 0 || b < a) {
		return b
	}
	return a
}

// dnevnaNaGrafu: dnevna nije izdana, ili je izdana bez kiše.
func dnevnaNaGrafu(vrsta string, q prognoza.Upitno) string {
	switch {
	case !q.Dnevno:
		return ""
	case vrsta == prognoza.VrstaKisa:
		return "dnevna upitna"
	}
	return "dnevna nije izdana"
}

// natpisUpitnog dodaje natpisu grafa zašto je prognoza upitna.
func natpisUpitnog(upitno string) string {
	if upitno == "" {
		return ""
	}
	return " " + upitno + "."
}
