package prognoza

// Provjera dnevnog modela jedne letve unutar programa (plan uređivanja modela,
// PR 10). Model se nauči na danima prije granice i mjeri na danima od granice
// do kraja, koje nije vidio. Po danu daje promašaj modela i promašaj bez modela,
// tj. kad se drži zadnje mjerenje, istom mjerom: korijenom srednjeg kvadrata.
// Isti račun dosad je radio samo alat izvan programa.
//
// U provjeri kiša za dane unaprijed je ona koja je stvarno pala, jer arhive
// prognoza kiše nema za cijelo razdoblje. Uživo je to prognoza kiše, pa je
// izmjereni promašaj donja granica onoga što model može.

import (
	"database/sql"
	"errors"
	"math"
)

// NatpisTocnaKisa stoji uz mjeru dnevnog modela s kišom.
const NatpisTocnaKisa = "Mjereno uz kišu koja je stvarno pala, a ne uz prognozu kiše"

// NatpisKratkoUcenje stoji uz mjeru modela koji je učio na manje dana nego što
// traži dnevni model letve iz programa.
const NatpisKratkoUcenje = "Model je učio na kratkoj arhivi, pa veliku vodu možda nije vidio"

// MjeraDana je promašaj dnevnog modela za jedan dan unaprijed.
type MjeraDana struct {
	Dan       int
	Promasaj  float64 // korijen srednjeg kvadrata promašaja modela
	BezModela float64 // ista mjera kad se drži zadnje mjerenje
	URasponu  float64 // udio dana u kojima je izmjereno ostalo u rasponu
	Slucaja   int
	Stanje    StanjeMjere
}

// MjeraDnevnog je ishod provjere dnevnog modela jedne letve.
type MjeraDnevnog struct {
	Dani       []MjeraDana
	DanaUcenja int
	Natpisi    []string
}

// zbrojDana skuplja promašaje jednog dana unaprijed.
type zbrojDana struct {
	kvModel, kvBez float64
	n, uRasponu    int
}

func (z *zbrojDana) dodaj(prognoza, raspon, stvarno float64) {
	z.kvModel += (prognoza - stvarno) * (prognoza - stvarno)
	z.kvBez += stvarno * stvarno
	z.n++
	if math.Abs(prognoza-stvarno) <= raspon {
		z.uRasponu++
	}
}

func (z zbrojDana) mjera(dan int) MjeraDana {
	m := MjeraDana{Dan: dan, Slucaja: z.n}
	if z.n > 0 {
		n := float64(z.n)
		m.Promasaj, m.BezModela, m.URasponu = math.Sqrt(z.kvModel/n), math.Sqrt(z.kvBez/n), float64(z.uRasponu)/n
	}
	m.Stanje = OcijeniMjeru(MjeraProvjere{Promasaj: m.Promasaj, BezModela: m.BezModela, Slucaja: z.n, SlucajaBez: z.n})
	return m
}

// ProvjeriDnevni nauči dnevni model cilja na danima prije granice i izmjeri ga
// na danima od granice do kraja (dani od epohe). Nizovi su dnevni nizovi letvi
// i oborina međuslivova cilja (DnevniNizoviCilja).
func ProvjeriDnevni(nizovi map[string]DnevniNiz, c DnevniCilj, granica, kraj int64) (MjeraDnevnog, error) {
	if granica >= kraj {
		return MjeraDnevnog{}, errors.New("granica učenja mora biti prije kraja provjere")
	}
	m, err := NamjestiDnevni(nizovi, c, granica)
	if err != nil {
		return MjeraDnevnog{}, err
	}
	zbroj := izmjeriDnevni(m, nizovi, c, granica, kraj)
	out := MjeraDnevnog{DanaUcenja: m.Uzoraka()}
	for k := 1; k <= DnevniDosezi; k++ {
		out.Dani = append(out.Dani, zbroj[k].mjera(k))
	}
	if len(c.Slivovi) > 0 {
		out.Natpisi = append(out.Natpisi, NatpisTocnaKisa)
	}
	if out.DanaUcenja < NajmanjeDanaDnevnog {
		out.Natpisi = append(out.Natpisi, NatpisKratkoUcenje)
	}
	return out, nil
}

// izmjeriDnevni pušta naučeni model po danima od granice do kraja i zbraja
// promašaje po danu unaprijed.
func izmjeriDnevni(m *DnevniModel, nizovi map[string]DnevniNiz, c DnevniCilj, granica, kraj int64) [DnevniDosezi + 1]zbrojDana {
	var zbroj [DnevniDosezi + 1]zbrojDana
	cilj := nizovi[c.Letva]
	for t := granica; t < kraj; t++ {
		x, ok := DnevneZnacajke(c, nizovi, t)
		if !ok {
			continue
		}
		p, r := m.Prognoziraj(x)
		for k := 1; k <= DnevniDosezi; k++ {
			if v, ima := cilj[t+int64(k)]; ima {
				zbroj[k].dodaj(p[k], r[k], v-cilj[t])
			}
		}
	}
	return zbroj
}

// DnevniNizoviCilja čita iz arhive dnevne nizove cilja i njegovih ulaza te
// oborinu njegovih međuslivova, iz kišomjera registra (OborinskeTocke). Kiša
// stvarnog kišomjera traži da je među točkama i on (KisomjeriKise).
func DnevniNizoviCilja(arhiva *sql.DB, tocke []OborinskaTocka, c DnevniCilj) (map[string]DnevniNiz, error) {
	nizovi := map[string]DnevniNiz{}
	for _, l := range c.letve() {
		n, err := DnevniIzArhive(arhiva, l)
		if err != nil {
			return nil, err
		}
		nizovi[l] = n
	}
	if len(c.Slivovi) == 0 {
		return nizovi, nil
	}
	tocke = tockeCilja(tocke, c)
	oborine, err := DnevneOborine(arhiva, tocke)
	if err != nil {
		return nil, err
	}
	for _, s := range c.Slivovi {
		if n, ima := oborine[OborinaKljuc(s)]; ima {
			nizovi[OborinaKljuc(s)] = n
		}
	}
	return nizovi, dodajKisuKisomjera(arhiva, nizovi, oborine, tocke, c)
}
