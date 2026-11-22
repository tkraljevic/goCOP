package web

// Dnevni model Drave i Dunava na stranici „Računi letvi”: uz letvu koja je cilj
// dnevnog modela stoji zaseban obrazac s ulazima, slivovima i danom od kojeg
// dnevni model daje vrijednost. Ide svojim putem, neovisno o satnom računu:
// probaj, pa primijeni.

import (
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"gocop/internal/models"
	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
)

// UrednikDnevnog sprema, proba i primjenjuje dnevne blokove letvi dnevnog
// modela Drave i Dunava. Daje ga isti urednik koji slaže satne račune.
type UrednikDnevnog interface {
	SpremiDnevni(letva string, blok prognoza.DnevniLetve, tko string) error
	ObrisiDnevniNacrt(letva string) error
	// ProbajDnevniGlavni mjeri dnevni blok iz nacrta; traje, pa ide kao posao.
	ProbajDnevniGlavni(letva string, p *poslovi.Posao) error
	PrimijeniDnevni(letva, tko, razlog string) error
	UkloniDnevni(letva, tko, razlog string) error
}

// urednikDnevnog je urednik dnevnih blokova, kad ga ovo računalo ima.
func (h *PrognozeHandler) urednikDnevnog() (UrednikDnevnog, error) {
	u := h.urednik
	if p, omotan := u.(urednikPosluzitelja); omotan {
		u = p.s.urednikModela
	}
	if d, zna := u.(UrednikDnevnog); zna {
		return d, nil
	}
	return nil, errBezUrednika
}

// KvacicaIzbora je jedna letva ili sliv u popisu s kvačicama.
type KvacicaIzbora struct {
	Kod, Ime string
	Odabran  bool
}

// ObrazacDnevnog je dnevni model odabrane letve, kako stoji u obrascu.
type ObrazacDnevnog struct {
	Kod, Ime string
	// IzZapisa kaže da model već ima dnevni blok ove letve; inače vrijedi program.
	IzZapisa bool
	// Tko i Kad su tko je i kad spremio nacrt; prazno kad nacrta nema.
	Tko, Kad                        string
	Ulazi, Slivovi                  []KvacicaIzbora
	OdDana, OdDanaNiska, VisokaVoda string
	Proba                           *ProbaRacuna
}

// redProbeBloka je redak probe dnevnog bloka: novi ulazi, sadašnji i zadnje mjerenje.
func redProbeBloka(d prognoza.MjeraDana, sada map[int]prognoza.MjeraDana) DosegProbe {
	r := DosegProbe{Dan: d.Dan, Slucaja: d.Slucaja, Ocjena: oznakaOcjene(d.Stanje), Racun: "—", BezModela: "—", Sada: "—"}
	if d.Stanje != prognoza.MjeraNijeProvjereno {
		r.Racun, r.BezModela = decimalaHR(d.Promasaj), decimalaHR(d.BezModela)
	}
	if s, ima := sada[d.Dan]; ima && s.Stanje != prognoza.MjeraNijeProvjereno {
		r.Sada = decimalaHR(s.Promasaj)
	}
	return r
}

// probaBloka slaže ishod probe dnevnog bloka za stranicu; nil kad nije proban.
func probaBloka(p *prognoza.ProbaDnevnogBloka) *ProbaRacuna {
	if p == nil {
		return nil
	}
	out := &ProbaRacuna{Kad: p.Kad, Jedinica: "cm", Natpisi: p.Natpisi, Dnevna: true, Lanac: true}
	if len(p.Razdoblje) == 2 {
		out.Razdoblje = datumHR(p.Razdoblje[0]) + " – " + datumHR(p.Razdoblje[1])
	}
	sada := map[int]prognoza.MjeraDana{}
	for _, d := range p.Prije {
		sada[d.Dan] = d
	}
	for _, d := range p.Dani {
		out.Dosezi = append(out.Dosezi, redProbeBloka(d, sada))
		if d.Stanje == prognoza.MjeraLosije {
			out.Losijih++
		}
	}
	return out
}

// obrazacDnevnog slaže obrazac dnevnog modela letve: nacrt kad ga ima, inače ono
// što sada vrijedi. nil kad letva nije cilj dnevnog modela Drave i Dunava.
func (h *PrognozeHandler) obrazacDnevnog(kod string, postaje map[string]models.Station, slivovi []IzborLetve) *ObrazacDnevnog {
	if !prognoza.GlavniDnevniCilj(kod) {
		return nil
	}
	o := &ObrazacDnevnog{Kod: kod, Ime: imeLetve(postaje, kod), IzZapisa: h.imaDnevniBlok(kod)}
	blok := sadasnjiBlok(kod)
	if n, ima := h.dnevniNacrt(kod); ima {
		blok, o.Tko, o.Kad, o.Proba = n.Blok, n.Tko, n.Kad, probaBloka(n.Proba)
	}
	for _, l := range kandidatiUlaza(kod, blok.Ulazi) {
		o.Ulazi = append(o.Ulazi, KvacicaIzbora{Kod: l, Ime: imeLetve(postaje, l), Odabran: slices.Contains(blok.Ulazi, l)})
	}
	for _, s := range slivovi {
		o.Slivovi = append(o.Slivovi, KvacicaIzbora{Kod: s.Kod, Ime: s.Ime, Odabran: slices.Contains(blok.Slivovi, s.Kod)})
	}
	o.OdDana, o.OdDanaNiska = brojIliPrazno(blok.OdDana), brojIliPrazno(blok.OdDanaNiska)
	if blok.VisokaVoda != nil {
		o.VisokaVoda = strings.Replace(strconv.FormatFloat(*blok.VisokaVoda, 'f', -1, 64), ".", ",", 1)
	}
	return o
}

func brojIliPrazno(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// sadasnjiBlok je dnevni model letve kakav sada vrijedi, u obliku bloka: ulazi i
// slivovi iz zapisa ili programa, s danima prijelaza koji vrijede.
func sadasnjiBlok(kod string) prognoza.DnevniLetve {
	var blok prognoza.DnevniLetve
	for _, c := range prognoza.CiljeviDnevnog() {
		if c.Letva == kod {
			blok.Ulazi, blok.Slivovi = c.Ulazi, c.Slivovi
		}
	}
	if d, ima := prognoza.DnevnaOdDanaLetve(kod); ima {
		blok.OdDana = &d
	}
	if d, ima := prognoza.OdDanaNiskeLetve(kod); ima {
		blok.OdDanaNiska = &d
	}
	if g, ima := prognoza.VisokaVodaLetve(kod); ima {
		blok.VisokaVoda = &g
	}
	return blok
}

// kandidatiUlaza su letve koje obrazac nudi kao ulaze dnevnog modela: odabrane,
// pa ostale koje dnevni model već čita ili računa, bez same letve.
func kandidatiUlaza(kod string, odabrane []string) []string {
	out := slices.Clone(odabrane)
	dodaj := func(l string) {
		if l != kod && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	for _, c := range prognoza.CiljeviDnevnog() {
		dodaj(c.Letva)
	}
	for _, l := range prognoza.DnevniUlazi() {
		dodaj(l)
	}
	return out
}

// imaDnevniBlok javlja ima li model redak ove letve s dnevnim blokom.
func (h *PrognozeHandler) imaDnevniBlok(kod string) bool {
	if h.citac == nil {
		return false
	}
	z, ima := h.citac().ZapisiModela()[kod]
	if !ima {
		return false
	}
	_, err := z.DnevniBlok()
	return err == nil
}

// dnevniNacrt je nacrt dnevnog bloka letve.
func (h *PrognozeHandler) dnevniNacrt(kod string) (prognoza.NacrtDnevnog, bool) {
	if h.citac == nil {
		return prognoza.NacrtDnevnog{}, false
	}
	n, ima := h.citac().DnevniNacrti()[kod]
	return n, ima
}

// dnevniBlokIzObrasca slaže dnevni blok iz polja obrasca: kvačice i dodatni
// ulazi, kvačice slivova te dani prijelaza i granica visoke vode.
func dnevniBlokIzObrasca(f url.Values) (prognoza.DnevniLetve, error) {
	blok := prognoza.DnevniLetve{Slivovi: odabraneLetve(f["d_sliv"])}
	for _, l := range odabraneLetve(slices.Concat(f["d_ulaz"], f["d_ulaz_jos"])) {
		if !slices.Contains(blok.Ulazi, l) {
			blok.Ulazi = append(blok.Ulazi, l)
		}
	}
	var err error
	if blok.OdDana, err = danPrijelazaIzObrasca(f.Get("d_od_dana")); err != nil {
		return blok, err
	}
	if blok.OdDanaNiska, err = danPrijelazaIzObrasca(f.Get("d_od_dana_niska")); err != nil {
		return blok, err
	}
	if v := strings.TrimSpace(f.Get("d_visoka_voda")); v != "" {
		g, err := strconv.ParseFloat(strings.Replace(v, ",", ".", 1), 64)
		if err != nil {
			return blok, errors.New("granica visoke vode mora biti broj, u centimetrima")
		}
		blok.VisokaVoda = &g
	}
	return blok, nil
}

// danPrijelazaIzObrasca čita dan prijelaza; prazno polje ostavlja ono iz programa.
func danPrijelazaIzObrasca(polje string) (*int, error) {
	polje = strings.TrimSpace(polje)
	if polje == "" {
		return nil, nil
	}
	d, err := strconv.Atoi(polje)
	if err != nil {
		return nil, errors.New("dan od kojeg vrijedi dnevni model mora biti cijeli broj")
	}
	return &d, nil
}

// izvrsiRadnjuDnevnog izvrši radnju obrasca dnevnog modela i vrati poruku o
// uspjehu. obradena je false kad radnja nije njegova.
func (h *PrognozeHandler) izvrsiRadnjuDnevnog(f url.Values, letva, ime, tko string) (poruka string, obradena bool, err error) {
	radnja := f.Get("radnja")
	if !strings.HasPrefix(radnja, "dnevni-") {
		return "", false, nil
	}
	u, err := h.urednikDnevnog()
	if err != nil {
		return "", true, err
	}
	razlog := strings.TrimSpace(f.Get("razlog"))
	switch radnja {
	case "dnevni-odbaci":
		return "Nacrt dnevnog modela letve " + ime + " je odbačen.", true, u.ObrisiDnevniNacrt(letva)
	case "dnevni-primijeni":
		return "Dnevni model letve " + ime + " je primijenjen. Vrijedi od sljedećeg računa prognoze.", true, u.PrimijeniDnevni(letva, tko, razlog)
	case "dnevni-ukloni":
		return "Dnevni model letve " + ime + " opet se uči iz programa, od sljedećeg računa prognoze.", true, u.UkloniDnevni(letva, tko, razlog)
	}
	blok, err := dnevniBlokIzObrasca(f)
	if err != nil {
		return "", true, err
	}
	return "Nacrt dnevnog modela letve " + ime + " je spremljen.", true, u.SpremiDnevni(letva, blok, tko)
}
