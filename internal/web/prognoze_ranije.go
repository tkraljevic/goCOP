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
)

// Izvoz ranije prognoze. Baza prognoza čuva svako izdanje, pa se izdanje
// zadanog dana i sata (po mjesnom vremenu) izvozi kao i zadnje: isti listovi,
// a brojke, upozorenja, dnevna prognoza, kiša i tuđe prognoze iz tog
// izdanja. Kad u tom satu prognoza nije izdana, uzima se najbliže ranije
// izdanje i to se kaže. Mjerenja na grafovima sežu do sata izdanja, kao u
// izvozu koji bi se tada napravio.

// RanijeIzdanje je izdanje nađeno za traženi sat.
type RanijeIzdanje struct {
	Trazeno time.Time // traženi sat
	Izdano  time.Time // sat izdanja koje se izvozi: traženi ili najbliži raniji
	// Nastalo je kad je izdavač izračunao zadnju verziju sata; nula kad
	// zapisa o izdanju nema. Putuje s izdanjem, pa je na svim čvorovima
	// isto, a broj verzija nije: primatelj nadoknadom dobije samo zadnju.
	Nastalo time.Time
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

// trazeniSat čita dan i sat iz upita (dan=2026-10-05&sat=9), po mjesnom
// vremenu. Sat koji do trenutka sada još nije došao nema izdanja: zadnje
// bi se tada izvezlo kao „najbliže ranije” i izgledalo zastarjelo.
func trazeniSat(q url.Values, sada time.Time) (time.Time, error) {
	dan, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(q.Get("dan")), models.Zagreb)
	if err != nil {
		return time.Time{}, fmt.Errorf("dan nije ispravan datum")
	}
	sat, err := strconv.Atoi(strings.TrimSpace(q.Get("sat")))
	if err != nil || sat < 0 || sat > 23 {
		return time.Time{}, fmt.Errorf("sat mora biti od 0 do 23")
	}
	t := time.Date(dan.Year(), dan.Month(), dan.Day(), sat, 0, 0, 0, models.Zagreb)
	if t.After(sada) {
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
		iz.Nastalo = o.Nastalo
	}
	return iz, true
}

// satRanijeg je sat po mjesnom vremenu, npr. „5.10.2026. u 09:00”.
func satRanijeg(t time.Time) string { return t.In(models.Zagreb).Format("2.1.2006. u 15:04") }

// Poruka kaže koje se izdanje izvozi i zašto baš to.
func (iz RanijeIzdanje) Poruka() string {
	if iz.Tocno() {
		return "Prognoza izdana " + satRanijeg(iz.Izdano) + iz.izracunato("izračunata") + "."
	}
	return "Za " + satRanijeg(iz.Trazeno) + " prognoza nije izdana; najbliže ranije izdanje je " +
		satRanijeg(iz.Izdano) + fmt.Sprintf(" (%d h ranije)", int(iz.Trazeno.Sub(iz.Izdano).Hours())) +
		iz.izracunato("izračunato") + "."
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
		" Brojke, upozorenja, dnevna prognoza, kiša i tuđe prognoze su iz tog izdanja (iz zadnjeg, kad je sat" +
		" izdan više puta), a mjerenja na grafovima sežu" +
		" do sata izdanja; godišnji vodostaji, zabilježene krajnosti i list „O prognozi” su prema današnjoj arhivi i modelu."
}

// zahtjevRanijeg pronalazi izdanje koje upit traži i stavlja ga u kontekst
// zahtjeva; kad ga nema, odgovara sam i vraća false.
func (h *PrognozeHandler) zahtjevRanijeg(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	trazeno, err := trazeniSat(r.URL.Query(), time.Now())
	if err != nil {
		http.Error(w, "Ranije izdanje: "+err.Error()+".", http.StatusBadRequest)
		return r, false
	}
	c := h.citacBaze()
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
	Od           string // najstarije izdanje u bazi; prazno dok izdanja nema
	OdDan, DoDan string // granice birača dana, 2006-01-02
	Dan          string // traženi dan, 2006-01-02
	Sat          int    // traženi sat
	Trazeno      bool   // korisnik je tražio izdanje
	Poruka       string // što je nađeno ili zašto nije
	URL          string // preuzimanje izvoza; prazno kad izdanja nema
	Napomena     string // odlomak na prvom listu izvoza ranijeg izdanja
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
// kad je korisnik tražio dan i sat, koje se izdanje izvozi.
func (h *PrognozeHandler) ranijiIzvoz(r *http.Request) RanijiIzvoz {
	c := h.citacBaze()
	sada := time.Now().In(models.Zagreb)
	out := RanijiIzvoz{Dan: sada.Format("2006-01-02"), Sat: sada.Hour(), DoDan: sada.Format("2006-01-02")}
	prvo, ima := c.PrvoIzdanje()
	if !ima {
		return out
	}
	out.Od, out.OdDan = satRanijeg(prvo), prvo.In(models.Zagreb).Format("2006-01-02")
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
	out.Dan, out.Sat = trazeno.Format("2006-01-02"), trazeno.Hour()
	iz, ok := nadjiRanije(c, trazeno)
	if !ok {
		out.Poruka = bezRanijeg(c, trazeno)
		return out
	}
	out.Poruka = iz.Poruka()
	out.URL = "/prognoze.xlsx?" + url.Values{"dan": {out.Dan}, "sat": {strconv.Itoa(out.Sat)}}.Encode()
	return out
}
