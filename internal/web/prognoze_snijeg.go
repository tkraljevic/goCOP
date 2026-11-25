package web

// Kartica „Model snijega” na stranici Postavke prognoze: je li model snijega
// uključen, brzina i temperatura topljenja. Mijenja se kao model letve:
// spremi kao nacrt, isprobaj na povijesti, primijeni s razlogom. Vide je svi
// koji vide stranicu, a mijenja je globalni administrator na računalu koje
// izdaje prognozu (smijeUreditiModel).

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
)

// natragSnijeg je kartica snijega na stranici postavki.
const natragSnijeg = "/prognoze/postavke#snijeg"

// UrednikSnijega sprema, proba i primjenjuje postavke snijega. Daje ga isti
// urednik koji slaže model letve.
type UrednikSnijega interface {
	SpremiSnijeg(p prognoza.PostavkeSnijega, tko string) error
	ObrisiNacrtSnijega() error
	// ProbajSnijeg proba nacrt na povijesti; traje, pa ide kao posao.
	ProbajSnijeg(p *poslovi.Posao) error
	PrimijeniSnijeg(tko, razlog string) error
}

// urednikSnijega je urednik postavki snijega, kad ga ovo računalo ima.
func (h *PrognozeHandler) urednikSnijega() (UrednikSnijega, error) {
	u := h.urednik
	if p, omotan := u.(urednikPosluzitelja); omotan {
		u = p.s.urednikModela
	}
	if s, zna := u.(UrednikSnijega); zna {
		return s, nil
	}
	return nil, errBezUrednika
}

// KarticaSnijega je kartica „Model snijega” s rezultatom probe.
type KarticaSnijega struct {
	// Stanje je rečenica o primijenjenim postavkama, a Nacrt o nacrtu.
	Stanje, Nacrt string
	// Ukljucen, Topljenje i Prag su u obrascu: iz nacrta, a bez njega
	// primijenjeni.
	Ukljucen        bool
	Topljenje, Prag string
	ImaNacrt        bool
	SmijeUrediti    bool
	ZastoNe         string
	Proba           *ProbaSnijegaStranice
	// Granice polja, za obrazac.
	NajmanjeTopljenje, NajviseTopljenje, NajnizaTemperatura, NajvisaTemperatura string
}

// ProbaSnijegaStranice je rezultat probe kako ga kartica pokazuje.
type ProbaSnijegaStranice struct {
	Kad, Razdoblje string
	Letve          []RedProbeSnijega
	Natpisi        []string
	Losijih        int
	// Zastarjela je proba napravljena prema postavkama koje više nisu
	// primijenjene; takva se ne primjenjuje.
	Zastarjela bool
}

// RedProbeSnijega je jedna letva u rezultatu probe.
type RedProbeSnijega struct {
	Ime    string
	Dani   []DanProbeSnijega
	Ocjena StanjeModela
	Greska string
}

// karticaSnijega slaže karticu iz baze prognoza; nil kad baze nema.
func (h *PrognozeHandler) karticaSnijega(r *http.Request) *KarticaSnijega {
	if h.citac == nil {
		return nil
	}
	c := h.citac()
	if c == nil || c.db == nil {
		return nil
	}
	z, ima, err := prognoza.PrimijenjeneSnijega(c.db)
	k := &KarticaSnijega{Stanje: stanjeSnijega(z, ima, err),
		NajmanjeTopljenje: brojPolja(prognoza.NajmanjeTopljenje), NajviseTopljenje: brojPolja(prognoza.NajviseTopljenje),
		NajnizaTemperatura: brojPolja(prognoza.NajnizaTemperatura), NajvisaTemperatura: brojPolja(prognoza.NajvisaTemperatura)}
	k.SmijeUrediti, k.ZastoNe = h.smijeUreditiModel(r)
	p := z.Postavke
	n, imaNacrt, err := prognoza.NacrtSnijegaIz(c.db)
	if imaNacrt {
		p, k.ImaNacrt, k.Nacrt = n.Postavke, true, opisNacrtaSnijega(n)
		k.Proba = probaSnijegaStranice(n.Proba, z.Postavke, h.postaje(r.Context()))
	} else if err != nil {
		k.Nacrt = "Nacrt se ne da pročitati (" + err.Error() + ")."
	}
	k.Ukljucen, k.Topljenje, k.Prag = p.Ukljucen, brojPolja(p.Topljenje), brojPolja(p.Prag)
	return k
}

// opisNacrtaSnijega kaže tko je i kad spremio nacrt i da on još ne vrijedi.
func opisNacrtaSnijega(n prognoza.NacrtSnijega) string {
	out := strings.TrimSpace("Nacrt je spremio " + n.Tko + " " + n.Kad)
	if n.Tko == "" && n.Kad == "" {
		out = "Nacrt je spremljen"
	}
	return out + ". Dok se ne primijeni, prognoza računa s primijenjenim postavkama."
}

// stanjeSnijega je rečenica o tome s kojim postavkama prognoza sada računa.
func stanjeSnijega(z prognoza.ZapisSnijega, ima bool, err error) string {
	opis := opisPostavkiSnijega(z.Postavke)
	switch {
	case err != nil:
		return "Primijenjene postavke se ne daju pročitati (" + err.Error() + "), pa vrijede postavke iz programa: " + opis + "."
	case !ima:
		return "Vrijede postavke iz programa: " + opis + "."
	}
	zadnja := z.Zadnja()
	dijelovi := []string{"Primijenjeno " + zadnja.Kad}
	if zadnja.Tko != "" {
		dijelovi = append(dijelovi, zadnja.Tko)
	}
	dijelovi = append(dijelovi, opis, "„"+zadnja.Razlog+"”")
	return strings.Join(dijelovi, " · ")
}

// opisPostavkiSnijega opisuje postavke jednom rečenicom.
func opisPostavkiSnijega(p prognoza.PostavkeSnijega) string {
	if !p.Ukljucen {
		return "model snijega isključen, prognoza dobiva oborinu kakva je pala"
	}
	return "topi se " + decimalaHR(p.Topljenje) + " mm vode po °C na dan iznad " + strings.Replace(decimalaHR(p.Prag), "-", "−", 1) + " °C"
}

// brojPolja piše broj za polje obrasca: s točkom, jer ga tako čita polje za
// broj.
func brojPolja(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// probaSnijegaStranice slaže rezultat probe za karticu; nil kad proba nema.
func probaSnijegaStranice(p *prognoza.ProbaSnijega, primijenjene prognoza.PostavkeSnijega, postaje map[string]models.Station) *ProbaSnijegaStranice {
	if p == nil {
		return nil
	}
	out := &ProbaSnijegaStranice{Kad: p.Kad, Natpisi: p.Natpisi, Zastarjela: p.Primijenjene != primijenjene}
	if len(p.Razdoblje) == 2 {
		out.Razdoblje = datumHR(p.Razdoblje[0]) + " do " + datumHR(p.Razdoblje[1])
	}
	for _, l := range p.Letve {
		red := RedProbeSnijega{Ime: imeLetve(postaje, l.Letva), Ocjena: oznakaOcjeneSnijega(l.Ocjena), Greska: l.Greska}
		for i := range min(len(l.Sada), len(l.Nacrt)) {
			red.Dani = append(red.Dani, DanProbeSnijega{Dan: l.Sada[i].Dan, Tekst: celijaProbeSnijega(l.Sada[i], l.Nacrt[i])})
		}
		if l.Ocjena == prognoza.SnijegLosije {
			out.Losijih++
		}
		out.Letve = append(out.Letve, red)
	}
	return out
}

// DanProbeSnijega je „sada → nacrt” za jedan dan unaprijed.
type DanProbeSnijega struct {
	Dan   int
	Tekst string
}

// celijaProbeSnijega je „sada → nacrt” za jedan dan; dan bez slučajeva nema broj.
func celijaProbeSnijega(sada, nacrt prognoza.MjeraDana) string {
	if sada.Slucaja == 0 || nacrt.Slucaja == 0 {
		return "—"
	}
	return decimalaHR(sada.Promasaj) + " → " + decimalaHR(nacrt.Promasaj)
}

// oznakaOcjeneSnijega je čip ocjene probe snijega: bolje je dobro, lošije
// traži pozor.
func oznakaOcjeneSnijega(s prognoza.StanjeMjere) StanjeModela {
	switch s {
	case prognoza.SnijegBolje:
		return StanjeModela{string(s), "dobro"}
	case prognoza.SnijegLosije:
		return StanjeModela{string(s), "pozor"}
	}
	return StanjeModela{string(s), ""}
}

// postavkeSnijegaIzObrasca čita postavke iz obrasca; decimalni zarez vrijedi
// kao i točka.
func postavkeSnijegaIzObrasca(f url.Values) (prognoza.PostavkeSnijega, error) {
	broj := func(polje, sto string) (float64, error) {
		v, err := strconv.ParseFloat(strings.Replace(strings.TrimSpace(f.Get(polje)), ",", ".", 1), 64)
		if err != nil {
			return 0, errors.New(sto + " mora biti broj")
		}
		return v, nil
	}
	topljenje, err := broj("topljenje", "brzina topljenja")
	if err != nil {
		return prognoza.PostavkeSnijega{}, err
	}
	prag, err := broj("prag", "temperatura topljenja")
	if err != nil {
		return prognoza.PostavkeSnijega{}, err
	}
	p := prognoza.PostavkeSnijega{Ukljucen: f.Get("ukljucen") == "1", Topljenje: topljenje, Prag: prag}
	return p, p.Provjeri()
}

// SpremiSnijeg sprema, proba, primjenjuje ili odbacuje nacrt postavki snijega
// (POST /prognoze/postavke/snijeg).
func (h *PrognozeHandler) SpremiSnijeg(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "obrazac se ne da pročitati", http.StatusBadRequest)
		return
	}
	if smije, zasto := h.smijeUreditiModel(r); !smije {
		redirectWith(w, r, natragSnijeg, "error", zasto)
		return
	}
	u, err := h.urednikSnijega()
	if err != nil {
		redirectWith(w, r, natragSnijeg, "error", velikoPrvo(err.Error())+".")
		return
	}
	poruka, err := h.izvrsiRadnjuSnijega(r, u)
	if err != nil {
		redirectWith(w, r, natragSnijeg, "error", velikoPrvo(err.Error())+".")
		return
	}
	if r.PostForm.Get("radnja") == "probaj" && h.poslovi != nil && h.tmplPosao != nil {
		h.pokreniProbuSnijega(w, r, u)
		return
	}
	redirectWith(w, r, natragSnijeg, "success", poruka)
}

// izvrsiRadnjuSnijega izvrši radnju obrasca i vrati poruku o uspjehu. Proba
// prvo spremi nacrt iz obrasca; samu probu pokreće SpremiSnijeg kao posao.
func (h *PrognozeHandler) izvrsiRadnjuSnijega(r *http.Request, u UrednikSnijega) (string, error) {
	tko := ""
	if korisnik, _ := r.Context().Value(contextKeyUser).(*models.User); korisnik != nil {
		tko = korisnik.FullName
	}
	switch r.PostForm.Get("radnja") {
	case "odbaci":
		return "Nacrt postavki snijega je odbačen.", u.ObrisiNacrtSnijega()
	case "primijeni":
		return "Postavke snijega su primijenjene. Dnevni model uči se s njima od sljedećeg računa prognoze, a postavke putuju i na ostala računala.",
			u.PrimijeniSnijeg(tko, strings.TrimSpace(r.PostForm.Get("razlog")))
	}
	p, err := postavkeSnijegaIzObrasca(r.PostForm)
	if err != nil {
		return "", err
	}
	return "Nacrt postavki snijega je spremljen. Prognoza se ne mijenja dok se ne isproba i primijeni.", u.SpremiSnijeg(p, tko)
}

// pokreniProbuSnijega pokreće probu spremljenog nacrta kao posao i pokazuje
// stranicu koja ga prati; kad završi, vraća na karticu snijega.
func (h *PrognozeHandler) pokreniProbuSnijega(w http.ResponseWriter, r *http.Request, u UrednikSnijega) {
	korisnik, _ := r.Context().Value(contextKeyUser).(*models.User)
	perms, _ := r.Context().Value(contextKeyPerms).(*models.UserPermissions)
	id := ""
	if korisnik != nil {
		id = korisnik.ID.String()
	}
	p := h.poslovi.Pokreni("Proba postavki snijega", id, natragSnijeg+"-proba", u.ProbajSnijeg)
	d := PosaoData{CurrentUser: korisnik, Permissions: perms, ActiveNav: "prognoze", ViewAsBanner: viewBanner(r),
		PosaoID: p.ID, PosaoNaziv: p.Naziv, Natrag: natragSnijeg + "-proba"}
	if err := h.tmplPosao.ExecuteTemplate(w, "posao.html", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
