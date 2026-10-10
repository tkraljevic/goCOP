package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Izvoz ranije prognoze. Baza prognoza čuva svako izdanje, pa se izdanje
// zadanog dana i sata (po mjesnom vremenu) izvozi kao i zadnje: isti listovi,
// a brojke, upozorenja, dnevna prognoza, kiša i tuđe prognoze iz tog
// izdanja. Kad u tom satu prognoza nije izdana, uzima se najbliže ranije
// izdanje i to se kaže. Mjerenja na grafovima sežu do sata izdanja, kao u
// izvozu koji bi se tada napravio. Uz najstarija izdanja nije zapisano sve
// (prognoza.Pocetci): izvoz nema ono čega nema i to kaže.

// RanijeIzdanje je izdanje nađeno za traženi sat.
type RanijeIzdanje struct {
	Trazeno time.Time // traženi sat
	Izdano  time.Time // sat izdanja koje se izvozi: traženi ili najbliži raniji
	// Nastalo je kad je izdavač izračunao zadnju verziju sata; nula kad
	// zapisa o izdanju nema. Putuje s izdanjem, pa je na svim čvorovima
	// isto, a broj verzija nije: primatelj nadoknadom dobije samo zadnju.
	Nastalo time.Time
	// Što je uz izdanje zapisano. Najstarija izdanja nemaju sve (vidi
	// prognoza.Pocetci); izvoz nema ono čega nema i to kaže.
	Zapis, Dnevna, Upozorenja, Kisa bool
}

// Tocno javlja je li u traženom satu prognoza izdana.
func (iz RanijeIzdanje) Tocno() bool { return iz.Izdano.Equal(iz.Trazeno) }

// kljucRanijeg nosi ranije izdanje u kontekstu zahtjeva izvoza.
type kljucRanijeg struct{}

// IzdanjeZahtjeva javlja sat (od epohe) ranijeg izdanja koje zahtjev izvozi.
// Čvor po njemu daje kišu po međuslivovima kakvu je to izdanje imalo.
func IzdanjeZahtjeva(ctx context.Context) (int64, bool) {
	iz, ok := ctx.Value(kljucRanijeg{}).(RanijeIzdanje)
	return iz.Izdano.Unix() / 3600, ok
}

// UzRanijeIzdanje stavlja ranije izdanje u kontekst zahtjeva izvoza.
func UzRanijeIzdanje(ctx context.Context, iz RanijeIzdanje) context.Context {
	return context.WithValue(ctx, kljucRanijeg{}, iz)
}

// ranijeIz vraća ranije izdanje iz konteksta zahtjeva.
func ranijeIz(ctx context.Context) (RanijeIzdanje, bool) {
	iz, ok := ctx.Value(kljucRanijeg{}).(RanijeIzdanje)
	return iz, ok
}

// citacZahtjeva je čitač prognoza za zahtjev: prikovan na ranije izdanje kad
// ga zahtjev izvozi, inače onaj koji služi zadnje.
func (h *PrognozeHandler) citacZahtjeva(r *http.Request) *CitacPrognoza {
	c := h.citacBaze()
	if iz, ok := ranijeIz(r.Context()); ok {
		return c.Na(iz.Izdano.Unix() / 3600)
	}
	return c
}

// sadZahtjeva je trenutak prema kojem se izdanje gleda: uz ranije izdanje
// njegov sat, inače sada. Ranije izdanje tako nije zastarjelo: kad traženi
// sat nije imao izdanja, poruka kaže koliko je izvezeno ranije, a
// zastarjelost pod „Upozorenja uz izdanje” izdanje nije imalo.
func sadZahtjeva(r *http.Request) time.Time {
	if iz, ok := ranijeIz(r.Context()); ok {
		return iz.Izdano
	}
	return time.Now()
}

// granicaSata je najkasniji sat koji se smije tražiti: sada ili, kad je
// zadnje izdanje označeno satom koji još nije došao, taj sat. Izdanje je
// označeno satom najnovijeg mjerenja, pa zna nastati i prije punog sata
// kojim je označeno, a Prognoze ga tada već pokazuju.
func granicaSata(c *CitacPrognoza, sada time.Time) time.Time {
	if zadnje, ima := c.ZadnjeIzdano(); ima && zadnje.After(sada) {
		return zadnje
	}
	return sada
}

// trazeniSat čita dan i sat iz upita (dan=2026-10-05&sat=9), po mjesnom
// vremenu. Sat kasniji od granice (granicaSata) nema izdanja: zadnje bi se
// tada izvezlo kao „najbliže ranije” i izgledalo zastarjelo.
// Proljetni sat koji zbog ljetnog vremena ne postoji ne pretvara se tiho u
// sljedeći. Jesenski sat koji dođe dvaput zadano je kasniji, po zimskom
// vremenu, jer najbliže ranije tada pokriva oba; ljetno=1 bira raniji.
func trazeniSat(q url.Values, granica time.Time) (time.Time, error) {
	dan, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(q.Get("dan")), models.Zagreb)
	if err != nil {
		return time.Time{}, fmt.Errorf("dan nije ispravan datum")
	}
	sat, err := strconv.Atoi(strings.TrimSpace(q.Get("sat")))
	if err != nil || sat < 0 || sat > 23 {
		return time.Time{}, fmt.Errorf("sat mora biti od 0 do 23")
	}
	t := time.Date(dan.Year(), dan.Month(), dan.Day(), sat, 0, 0, 0, models.Zagreb)
	if t.Hour() != sat {
		return time.Time{}, fmt.Errorf("%s u %02d:00 ne postoji — toga se dana sat pomiče unaprijed na ljetno vrijeme",
			dan.Format("2.1.2006."), sat)
	}
	if prvi, drugi, dvaput := dvaPuta(t); dvaput {
		t = drugi
		if q.Get("ljetno") == "1" {
			t = prvi
		}
	}
	if t.After(granica) {
		return time.Time{}, fmt.Errorf("sat %s još nije došao — zadnja prognoza izvozi se na stranici Prognoze", satRanijeg(t))
	}
	return t, nil
}

// nadjiRanije traži izdanje za traženi sat; ok je false kad do njega nema
// nijednog.
func nadjiRanije(c *CitacPrognoza, trazeno time.Time) (RanijeIzdanje, bool) {
	izdano, ok := c.IzdanjeDo(trazeno)
	if !ok {
		return RanijeIzdanje{}, false
	}
	iz := RanijeIzdanje{Trazeno: trazeno, Izdano: izdano}
	if o, ima := c.OpisIzdanja(izdano); ima {
		iz.Nastalo, iz.Zapis = o.Nastalo, true
	}
	_, iz.Upozorenja = c.Upozorenja(izdano)
	dnevne, _ := c.DnevnoZa(izdano)
	iz.Dnevna, iz.Kisa = len(dnevne) > 0, c.KisaZapisana(izdano)
	return iz, true
}

// dvaPuta javlja je li mjesni sat trenutka t dvoznačan — na jesenski
// prelazak na zimsko vrijeme 02:00 dođe dvaput — i vraća oba trenutka:
// raniji po ljetnom, kasniji po zimskom vremenu.
func dvaPuta(t time.Time) (prvi, drugi time.Time, dvaput bool) {
	for _, p := range []time.Time{t.Add(-time.Hour), t} {
		d := p.Add(time.Hour)
		if p.In(models.Zagreb).Hour() == d.In(models.Zagreb).Hour() {
			return p, d, true
		}
	}
	return t, t, false
}

// satRanijeg je sat po mjesnom vremenu, npr. „5.10.2026. u 09:00”; uz sat
// koji je toga dana bio dvaput kaže i po kojem vremenu.
func satRanijeg(t time.Time) string {
	s := t.In(models.Zagreb).Format("2.1.2006. u 15:04")
	if _, _, dvaput := dvaPuta(t); !dvaput {
		return s
	}
	if t.In(models.Zagreb).IsDST() {
		return s + " (ljetno vrijeme)"
	}
	return s + " (zimsko vrijeme)"
}

// upitSata je upit za traženi sat; raniji od dva jesenska sata nosi ljetno=1.
func upitSata(t time.Time) url.Values {
	l := t.In(models.Zagreb)
	q := url.Values{"dan": {l.Format("2006-01-02")}, "sat": {strconv.Itoa(l.Hour())}}
	if prvi, _, dvaput := dvaPuta(t); dvaput && t.Equal(prvi) {
		q.Set("ljetno", "1")
	}
	return q
}

// DrugiSat je poveznica na drugi od dva jesenska sata istog imena.
type DrugiSat struct {
	Tekst, Poveznica, URL string
}

// drugiSat vodi s jednog jesenskog sata koji dođe dvaput na drugi; prazan
// za sve ostale sate.
func drugiSat(t time.Time) DrugiSat {
	prvi, drugi, dvaput := dvaPuta(t)
	if !dvaput {
		return DrugiSat{}
	}
	o, vrijeme := prvi, "ljetnom"
	if t.Equal(prvi) {
		o, vrijeme = drugi, "zimskom"
	}
	sat := t.In(models.Zagreb).Format("15:04")
	return DrugiSat{Tekst: "Toga je dana " + sat + " bilo dvaput (prelazak na zimsko vrijeme).",
		Poveznica: "Izdanje u " + sat + " po " + vrijeme + " vremenu",
		URL:       "/prognoze/postavke?" + upitSata(o).Encode() + "#ranije"}
}

// Poruka kaže koje se izdanje izvozi, zašto baš to i što uz njega nije
// zapisano.
func (iz RanijeIzdanje) Poruka() string {
	if iz.Tocno() {
		return "Prognoza izdana " + satRanijeg(iz.Izdano) + iz.izracunato("izračunata") + "." + iz.nezapisano()
	}
	return "Za " + satRanijeg(iz.Trazeno) + " prognoza nije izdana; najbliže ranije izdanje je " +
		satRanijeg(iz.Izdano) + fmt.Sprintf(" (%d h ranije)", int(iz.Trazeno.Sub(iz.Izdano).Hours())) +
		iz.izracunato("izračunato") + "." + iz.nezapisano()
}

// nezapisano je rečenica o dijelovima izdanja koji uz njega nisu zapisani;
// prazno kad je zapisano sve.
func (iz RanijeIzdanje) nezapisano() string {
	d := iz.dijelovi(false)
	if len(d) == 0 {
		return ""
	}
	return " Izvoz nema ono što uz to izdanje nije zapisano: " + strings.Join(d, "; ") + "."
}

// dijelovi su dijelovi izdanja koji uz njega jesu ili nisu zapisani; uz
// nezapisane piše i što to znači.
func (iz RanijeIzdanje) dijelovi(zapisani bool) []string {
	svi := []struct {
		ima        bool
		jest, nije string
	}{
		{true, "brojke", ""},
		{iz.Dnevna, "dnevna prognoza", "dnevna prognoza"},
		{iz.Upozorenja, "upozorenja", "upozorenja (inačica programa koja je izdala prognozu nije ih bilježila, " +
			"pa se ne vidi je li pri izdavanju sve radilo)"},
		{iz.Kisa, "kiša po međuslivovima", "kiša po međuslivovima"},
		{iz.Zapis, "tuđe prognoze koje je izdavač tada imao",
			"koje je tuđe prognoze izdavač tada imao (uzete su one izdane do sata izdanja)"},
	}
	var out []string
	for _, d := range svi {
		switch {
		case d.ima && zapisani:
			out = append(out, d.jest)
		case !d.ima && !zapisani:
			out = append(out, d.nije)
		}
	}
	return out
}

// izracunato kaže kad je izdavač izračunao izvezenu verziju, npr.
// „, izračunata u 10:41”; s danom kad to nije dan izdanja, a prazno kad
// zapisa nema.
func (iz RanijeIzdanje) izracunato(rijec string) string {
	if iz.Nastalo.IsZero() {
		return ""
	}
	n := iz.Nastalo.In(models.Zagreb)
	if n.Format("2.1.2006.") == iz.Izdano.In(models.Zagreb).Format("2.1.2006.") {
		return ", " + rijec + " u " + n.Format("15:04")
	}
	return ", " + rijec + " " + satRanijeg(n)
}

// Napomena je odlomak na prvom listu izvoza: koje je izdanje i što je u
// izvozu iz njega, a što prema današnjem stanju.
func (iz RanijeIzdanje) Napomena(izvezeno time.Time) string {
	return "Ranije izdanje, izvezeno " + satRanijeg(izvezeno) + ". " + iz.Poruka() +
		" Iz tog izdanja (iz zadnjeg, kad je sat izdan više puta) su " + nabroji(iz.dijelovi(true)) +
		"; mjerenja na grafovima sežu do sata izdanja, a godišnji vodostaji, zabilježene krajnosti i list" +
		" „O prognozi” su prema današnjoj arhivi i modelu."
}

// zahtjevRanijeg pronalazi izdanje koje upit traži i stavlja ga u kontekst
// zahtjeva; kad ga nema, odgovara sam i vraća false.
func (h *PrognozeHandler) zahtjevRanijeg(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	c := h.citacBaze()
	trazeno, err := trazeniSat(r.URL.Query(), granicaSata(c, time.Now()))
	if err != nil {
		http.Error(w, "Ranije izdanje: "+err.Error()+".", http.StatusBadRequest)
		return r, false
	}
	iz, ok := nadjiRanije(c, trazeno)
	if !ok {
		http.Error(w, bezRanijeg(c, trazeno), http.StatusNotFound)
		return r, false
	}
	return r.WithContext(UzRanijeIzdanje(r.Context(), iz)), true
}

// bezRanijeg kaže zašto za traženi sat izdanja nema.
func bezRanijeg(c *CitacPrognoza, trazeno time.Time) string {
	if prvo, ima := c.PrvoIzdanje(); ima {
		return fmt.Sprintf("Za %s ni ranije nema sačuvane prognoze; najstarije izdanje u bazi je %s.",
			satRanijeg(trazeno), satRanijeg(prvo))
	}
	return "Nijedna prognoza još nije izdana."
}

// oznaciRanije u podacima za izvoz označava ranije izdanje: uz vrijeme
// izdanja na svakom listu i napomenom na prvom.
func oznaciRanije(data *PrognozePageData, r *http.Request) {
	iz, ok := ranijeIz(r.Context())
	if !ok {
		return
	}
	data.Izdano += " (ranije izdanje)"
	data.Ranije.Napomena = iz.Napomena(time.Now())
}

// imeIzvoza je ime datoteke: za zadnje izdanje vrijeme izvoza, kao dosad, a
// za ranije sat izdanja.
func imeIzvoza(r *http.Request) string {
	if iz, ok := ranijeIz(r.Context()); ok {
		return "prognoza_izdanje_" + iz.Izdano.In(models.Zagreb).Format("2006-01-02_15h") + ".xlsx"
	}
	return "prognoza_" + time.Now().In(models.Zagreb).Format("2006-01-02_15h") + ".xlsx"
}

// RanijiIzvoz je odjeljak „Ranija prognoza” na postavkama i napomena na
// prvom listu izvoza ranijeg izdanja.
type RanijiIzvoz struct {
	Od           string   // najstarije izdanje u bazi; prazno dok izdanja nema
	Ogranicenje  string   // od kad baza ima koji dio izdanja; prazno kad uz sva sve
	OdDan, DoDan string   // granice birača dana, 2006-01-02
	Dan          string   // traženi dan, 2006-01-02
	Sat          int      // traženi sat
	Trazeno      bool     // korisnik je tražio izdanje
	Poruka       string   // što je nađeno ili zašto nije
	URL          string   // preuzimanje izvoza; prazno kad izdanja nema
	Drugi        DrugiSat // drugi jesenski sat istog imena; prazno za ostale
	Napomena     string   // odlomak na prvom listu izvoza ranijeg izdanja
}

// Sati su sati dana za birač.
func (RanijiIzvoz) Sati() []int {
	s := make([]int, 24)
	for i := range s {
		s[i] = i
	}
	return s
}

// citacBaze je čitač zadnjeg izdanja; nil kad baze prognoza nema.
func (h *PrognozeHandler) citacBaze() *CitacPrognoza {
	if h.citac == nil {
		return nil
	}
	return h.citac()
}

// ranijiIzvoz slaže odjeljak „Ranija prognoza”: od kad baza čuva izdanja i,
// kad je korisnik tražio dan i sat, koje se izdanje izvozi. Zadani dan i sat
// su sadašnji, a kad je zadnje izdanje označeno satom koji još nije došao,
// njegov: tako se zadano nalazi izdanje koje Prognoze pokazuju.
func (h *PrognozeHandler) ranijiIzvoz(r *http.Request) RanijiIzvoz {
	c := h.citacBaze()
	sada := granicaSata(c, time.Now()).In(models.Zagreb)
	out := RanijiIzvoz{Dan: sada.Format("2006-01-02"), Sat: sada.Hour(), DoDan: sada.Format("2006-01-02")}
	p, ima := c.PocetciZapisa()
	if !ima {
		return out
	}
	prvo := satOdEpohe(p.Izdane)
	out.Od, out.OdDan = satRanijeg(prvo), prvo.In(models.Zagreb).Format("2006-01-02")
	out.Ogranicenje = ogranicenjeZapisa(p)
	q := r.URL.Query()
	if !q.Has("dan") {
		return out
	}
	out.Trazeno = true
	trazeno, err := trazeniSat(q, sada)
	if err != nil {
		out.Poruka = "Ranije izdanje: " + err.Error() + "."
		return out
	}
	out.Dan, out.Sat, out.Drugi = trazeno.Format("2006-01-02"), trazeno.Hour(), drugiSat(trazeno)
	iz, ok := nadjiRanije(c, trazeno)
	if !ok {
		out.Poruka = bezRanijeg(c, trazeno)
		return out
	}
	out.Poruka = iz.Poruka()
	out.URL = "/prognoze.xlsx?" + upitSata(trazeno).Encode()
	return out
}

// ogranicenjeZapisa kaže od kad baza ima koji dio izdanja, za dijelove koji
// ne sežu do prvog izdanja; prazno kad je uz sva izdanja zapisano sve.
func ogranicenjeZapisa(p prognoza.Pocetci) string {
	var d []string
	for _, x := range []struct {
		od  int64
		sto string
	}{
		{p.Dnevne, "dnevna prognoza"},
		{p.Zapis, "kiša po međuslivovima i tuđe prognoze koje je izdavač imao"},
		{p.Upozorenja, "upozorenja"},
	} {
		if x.od != p.Izdane {
			d = append(d, x.sto+" "+odSataZapisa(x.od))
		}
	}
	if len(d) == 0 {
		return ""
	}
	return "Uz starija izdanja nije zapisano sve: " + strings.Join(d, "; ") + "."
}

// odSataZapisa je „od 26.9.2026. u 15:00”, a za dio koji nema nijedno
// izdanje „još ni uz jedno izdanje”.
func odSataZapisa(sat int64) string {
	if sat == 0 {
		return "još ni uz jedno izdanje"
	}
	return "od " + satRanijeg(satOdEpohe(sat))
}
