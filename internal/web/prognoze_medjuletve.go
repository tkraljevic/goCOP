package web

import (
	"context"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// MedjuletveDunava su naše letve na Dunavu između letvi s ugađanjem, bez
// svojeg dnevnog modela (Odluke (22. 10. 2026.)). Satni lanac im ne vidi
// kišu, a susjedi uz satni lanac imaju i dnevni model, pa je uzdužni profil
// do 96. sata imao grbe: susjedi su rasli, a letve između nisu. Dnevni dio
// zato posuđuju od susjeda, po riječnom kilometru, i miješaju ga sa svojim
// satnim lancem istom težinom kao susjedi (prognoza.TezinaLanca); iza 96.
// sata stoji samo dnevni dio iz susjeda. Oznaka je p, kao na Dravi.
//
// Siga i Petreš leže između Batine i Aljmaša, iznad ušća Drave, a Aljmaš je
// ispod njega, pa njihov dio od Aljmaša nosi i nešto Drave; uspor uz ušće to
// djelomično opravdava. Nije posebno izmjereno. Srpsko Bogojevo (prognoza
// prenesena s Aljmaša i Dalja) leži između Aljmaša i Vukovara, pa ide isto
// kao Dalj; Bezdan, Apatin i Bačka Palanka imaju ugađanje izvora.
var MedjuletveDunava = []LetvaIzSusjeda{
	{"siga", "batina", "aljmas"},
	{"petres", "batina", "aljmas"},
	{"bogojevo", "aljmas", "vukovar"},
	{"dalj", "aljmas", "vukovar"},
	{"sotin", "vukovar", "ilok"},
	{"mohovo", "vukovar", "ilok"},
}

// letveSPosudbom su sve letve kojima vrijednost dolazi iz susjeda.
func letveSPosudbom() []LetvaIzSusjeda {
	return append(append([]LetvaIzSusjeda(nil), LetveIzSusjeda...), MedjuletveDunava...)
}

// dodajMedjuletve slaže nizove međuletava Dunava. Bez srednjaka letve ili bez
// dnevne prognoze susjeda niz ostaje satni lanac.
func (u ulazPrikaza) dodajMedjuletve(po map[string]PregledLetve, out map[string]nizPrikaza) {
	for _, s := range MedjuletveDunava {
		l, imaX := po[s.Letva]
		w, imaW := tezinaSusjeda(u.postaje, s)
		srednjak, imaS := u.srednjak[s.Letva]
		if !imaX || !imaW || !imaS || l.Nepovezana {
			continue
		}
		dp := dnevniIzSusjeda(w, srednjak, u.dnevne[s.Uzvodna], u.dnevne[s.Nizvodna], u.izdanoH)
		out[s.Letva] = u.nizMedjuletve(l, dp)
	}
}

// nizMedjuletve je niz međuletve: uz kombinaciju satni lanac i dnevni dio iz
// susjeda zajedno, kao ugađanje susjeda; inače po pravilu tablice, s dnevnim
// dijelom iz susjeda na mjestu dnevnog modela.
func (u ulazPrikaza) nizMedjuletve(l PregledLetve, dp izvorSata) nizPrikaza {
	satni := satniIzvor(l.Satno, u.izdanoH)
	q := spojiStupce([]int{0}, []izvorDana{izSatnog}, map[izvorDana]izvorSata{izSatnog: satniIzvor(l.SatnoQ, u.izdanoH)}, SatiPrikaza)
	if u.nacin == models.PrognozaKombinacija {
		return nizPrikaza{Cm: ugodi(satni, dp), Q: q}
	}
	odDana := 0
	if u.nacin == models.PrognozaDnevni {
		odDana = 1
	}
	ima := func(t int64) bool { _, ok, _ := dp(int(t - u.izdanoH)); return ok }
	izvori := izvoriPo(odDana, l.Satno, ima, u.ciljevi)
	fje := map[izvorDana]izvorSata{izSatnog: satni, izDnevnog: dp}
	return nizPrikaza{Cm: spojiStupce(sidraDana(u.ciljevi, u.izdanoH), izvori, fje, SatiPrikaza), Q: q}
}

// dnevniIzSusjeda je dnevni dio letve iz dnevnih modela susjeda: srednjak
// letve 24 sata do izdanja i promjena dnevne prognoze svakog susjeda od
// njegova dana 0, utegnuta po kilometru (w je udio uzvodnog). Granice raspona
// idu istim putem.
func dnevniIzSusjeda(w, srednjak float64, gore, dolje []prognoza.DnevnaIzdana, izdanoH int64) izvorSata {
	nulaG, imaG := danNula(gore)
	nulaD, imaD := danNula(dolje)
	return func(h int) (tockaPrikaza, bool, bool) {
		g, okG := dnevniU(gore, izdanoH+int64(h))
		d, okD := dnevniU(dolje, izdanoH+int64(h))
		if !imaG || !imaD || !okG || !okD {
			return tockaPrikaza{}, false, false
		}
		uz := func(a, b float64) float64 { return srednjak + w*(a-nulaG) + (1-w)*(b-nulaD) }
		v := PregledVrijednost{Vrijednost: uz(g.Vrijednost, d.Vrijednost), Dolje: uz(g.Dolje, d.Dolje),
			Gore: uz(g.Gore, d.Gore), BoljaOdPostojanosti: true}
		return tockaPrikaza{PregledVrijednost: v, Ima: true, Susjedi: 1}, true, false
	}
}

// protokIzKrivulja daje protok vrijednostima iz susjeda: krivuljom letve,
// s rasponom kroz nju, kao satni lanac. Letva bez krivulje ondje protoka
// nema.
func (u ulazPrikaza) protokIzKrivulja(out map[string]nizPrikaza) {
	for kod, n := range out {
		for h, t := range n.Cm {
			if t.Ima && t.Susjedi > 0 && h < len(n.Q) {
				n.Q[h] = qIzKrivulje(u.krivulje[kod], u.izdanoH+int64(h), t)
			}
		}
	}
}

// qIzKrivulje je protok vrijednosti iz krivulje koja vrijedi u satu sat.
func qIzKrivulje(krivulje []models.HQKrivulja, sat int64, cm tockaPrikaza) tockaPrikaza {
	k := prognoza.KrivuljaZa(krivulje, time.Unix(sat*3600, 0).UTC())
	if k == nil {
		return tockaPrikaza{}
	}
	q, qd, qg, ok := prognoza.UProtok(k, cm.Vrijednost, cm.Dolje, cm.Gore)
	if !ok {
		return tockaPrikaza{}
	}
	return tockaPrikaza{PregledVrijednost: PregledVrijednost{Vrijednost: q, Dolje: qd, Gore: qg, BoljaOdPostojanosti: true},
		Ima: true, Dnevni: cm.Dnevni, Susjedi: cm.Susjedi}
}

// krivuljeSusjeda čita krivulje protoka letvi kojima vrijednost dolazi iz
// susjeda; letva bez krivulje ili bez arhive ostaje bez protoka uz p.
func (h *PrognozeHandler) krivuljeSusjeda(ctx context.Context) map[string][]models.HQKrivulja {
	out := map[string][]models.HQKrivulja{}
	if h.arhiva == nil {
		return out
	}
	a := h.arhiva()
	if a == nil {
		return out
	}
	for _, s := range letveSPosudbom() {
		if k, err := a.Krivulje(ctx, s.Letva); err == nil && len(k) > 0 {
			out[s.Letva] = k
		}
	}
	return out
}
