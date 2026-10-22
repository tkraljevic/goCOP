package web

import (
	"context"
	"math"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
)

// Prikaz prognoze po satu (Odluke (22. 10. 2026.)): tablica, uzdužni profil,
// graf u Excelu i graf na stranici letve čitaju isti niz, pa pokazuju isto. U
// satima stupaca tablice niz je točno ono što stupac pokazuje. Između dvaju
// stupaca dana koje daju različiti modeli — jedan satni lanac, drugi dnevni
// model ili susjedi — crta postupno prelazi s jednoga na drugi, umjesto da
// skoči na kraju satnog lanca.

// SatiPrikaza je dokle niz ide: do kraja šestog dana dnevnog modela.
const SatiPrikaza = prognoza.DnevniDosezi * 24

// izvorDana kaže koji model daje vrijednost stupca dana.
type izvorDana int

const (
	bezIzvora izvorDana = iota
	izSatnog
	izDnevnog
	izSusjeda
)

// izvorStupca je pravilo stupca dana k (0 je prvi dan): dnevni model od dana
// odDana gdje ga ima, inače satni lanac dokle seže, a iza njega dnevni model.
func izvorStupca(k, odDana int, imaSatni, imaDnevni bool) izvorDana {
	switch {
	case imaDnevni && odDana > 0 && k+1 >= odDana:
		return izDnevnog
	case imaSatni:
		return izSatnog
	case imaDnevni:
		return izDnevnog
	}
	return bezIzvora
}

// tockaPrikaza je vrijednost niza u jednom satu. Dnevni je udio dnevnog
// modela u njoj (0 satni lanac, 1 dnevni model), a Susjedi udio vrijednosti
// iz susjeda; između stupaca s različitim modelom udio raste postupno.
type tockaPrikaza struct {
	PregledVrijednost
	Ima     bool
	Dnevni  float64
	Susjedi float64
}

// nizPrikaza je niz jedne letve po satu od izdanja (indeks je sat, 0 je sat
// izdanja i ostaje prazan) u vodostaju i protoku.
type nizPrikaza struct {
	Cm, Q   []tockaPrikaza
	Ugodjen bool // satni lanac i dnevni model zajedno (prognoza.UgadjanjeLetve)
}

// izvorSata daje vrijednost jednog modela u satu h od izdanja. drzi kaže da je
// to zadnja vrijednost satnog lanca zadržana iza njegova kraja: smije ući samo
// u prijelaz prema drugom modelu, sama se ne crta.
type izvorSata func(h int) (v tockaPrikaza, ok, drzi bool)

// satniIzvor je satni lanac; iza zadnjeg sata drži zadnju vrijednost.
func satniIzvor(satno map[int64]PregledVrijednost, izdanoH int64) izvorSata {
	kraj := 0
	for t := range satno {
		kraj = max(kraj, int(t-izdanoH))
	}
	return func(h int) (tockaPrikaza, bool, bool) {
		if v, ima := satno[izdanoH+int64(h)]; ima {
			return tockaPrikaza{PregledVrijednost: v, Ima: true}, true, false
		}
		if v, ima := satno[izdanoH+int64(kraj)]; ima && h > kraj {
			return tockaPrikaza{PregledVrijednost: v, Ima: true}, true, true
		}
		return tockaPrikaza{}, false, false
	}
}

// dnevniIzvor je dnevni model, vodostaj ili protok (u je dnevniU ili dnevniQU).
func dnevniIzvor(u func([]prognoza.DnevnaIzdana, int64) (PregledVrijednost, bool),
	dnevne []prognoza.DnevnaIzdana, izdanoH int64) izvorSata {
	return func(h int) (tockaPrikaza, bool, bool) {
		v, ima := u(dnevne, izdanoH+int64(h))
		return tockaPrikaza{PregledVrijednost: v, Ima: ima, Dnevni: 1}, ima, false
	}
}

// spojiStupce slaže niz od 1. do zadnjeg sata. sidra su sati stupaca dana od
// izdanja (sidra[0] = 0), izvori[k] model koji daje stupac k, a izvori[0]
// model od kojeg niz kreće. Između dvaju stupaca s istim modelom ide taj
// model; između različitih udio drugoga raste pravocrtno od 0 do 1, pa je u
// satu stupca niz točno ono što stupac pokazuje. Iza zadnjeg stupca ide
// njegov model.
func spojiStupce(sidra []int, izvori []izvorDana, fje map[izvorDana]izvorSata, doSata int) []tockaPrikaza {
	out := make([]tockaPrikaza, doSata+1)
	bez := func(int) (tockaPrikaza, bool, bool) { return tockaPrikaza{}, false, false }
	fja := func(i izvorDana) izvorSata {
		if f, ima := fje[i]; ima {
			return f
		}
		return bez
	}
	k := 1
	for h := 1; h <= doSata; h++ {
		for k < len(sidra) && h > sidra[k] {
			k++
		}
		if k >= len(sidra) {
			if v, ok, drzi := fja(izvori[len(izvori)-1])(h); ok && !drzi {
				out[h] = v
			}
			continue
		}
		out[h] = prijelaz(h, sidra[k-1], sidra[k], izvori[k-1], izvori[k], fja)
	}
	return out
}

// prijelaz je sat h između stupaca u satima s0 i s1 kojima vrijednost daju
// modeli ia i ib. Kad jedan model u tom satu nema vrijednosti, ide drugi; u
// satu stupca samo model stupca, jer je to broj iz tablice.
func prijelaz(h, s0, s1 int, ia, ib izvorDana, fja func(izvorDana) izvorSata) tockaPrikaza {
	a := uSatu(fja(ia), h)
	if ia == ib {
		return a.prava()
	}
	b := uSatu(fja(ib), h)
	switch {
	case h == s1:
		return b.prava()
	case a.ok && b.ok && !(a.drzi && b.drzi):
		return pomijesaj(a.v, b.v, float64(h-s0)/float64(s1-s0))
	case a.ok && !a.drzi:
		return a.v
	}
	return b.prava()
}

// vrijednostIzvora je ono što je model dao u jednom satu.
type vrijednostIzvora struct {
	v        tockaPrikaza
	ok, drzi bool
}

func uSatu(f izvorSata, h int) vrijednostIzvora {
	v, ok, drzi := f(h)
	return vrijednostIzvora{v: v, ok: ok, drzi: drzi}
}

// prava je vrijednost koja se smije crtati sama: postoji i nije zadržana iza
// kraja satnog lanca.
func (x vrijednostIzvora) prava() tockaPrikaza {
	if x.ok && !x.drzi {
		return x.v
	}
	return tockaPrikaza{}
}

// pomijesaj je udio u od b i 1-u od a, u vrijednosti i rasponu.
func pomijesaj(a, b tockaPrikaza, u float64) tockaPrikaza {
	ip := func(x, y float64) float64 { return x + u*(y-x) }
	return tockaPrikaza{
		PregledVrijednost: PregledVrijednost{Vrijednost: ip(a.Vrijednost, b.Vrijednost),
			Dolje: ip(a.Dolje, b.Dolje), Gore: ip(a.Gore, b.Gore),
			BoljaOdPostojanosti: a.BoljaOdPostojanosti || b.BoljaOdPostojanosti},
		Ima: true, Dnevni: ip(a.Dnevni, b.Dnevni), Susjedi: ip(a.Susjedi, b.Susjedi),
	}
}

// sidraDana su sati stupaca dana od izdanja, sa satom izdanja na početku.
func sidraDana(ciljevi []int64, izdanoH int64) []int {
	sidra := []int{0}
	for _, t := range ciljevi {
		sidra = append(sidra, int(t-izdanoH))
	}
	return sidra
}

// izvoriLetve su modeli stupaca dana letve po pravilu tablice, s modelom od
// kojeg niz kreće na početku: satni lanac gdje ga ima, inače dnevni model.
func izvoriLetve(odDana int, l PregledLetve, dnevne []prognoza.DnevnaIzdana, ciljevi []int64) []izvorDana {
	prvi := izDnevnog
	if len(l.Satno) > 0 {
		prvi = izSatnog
	}
	izvori := []izvorDana{prvi}
	for k, t := range ciljevi {
		_, imaSatni := l.Satno[t]
		_, imaDnevni := dnevniU(dnevne, t)
		izvori = append(izvori, izvorStupca(k, odDana, imaSatni, imaDnevni))
	}
	return izvori
}

// prikazLetve je niz letve iz njezinih modela, bez susjeda.
func prikazLetve(odDana int, l PregledLetve, dnevne []prognoza.DnevnaIzdana, ciljevi []int64, izdanoH int64) nizPrikaza {
	sidra, izvori := sidraDana(ciljevi, izdanoH), izvoriLetve(odDana, l, dnevne, ciljevi)
	cm := map[izvorDana]izvorSata{izSatnog: satniIzvor(l.Satno, izdanoH), izDnevnog: dnevniIzvor(dnevniU, dnevne, izdanoH)}
	q := map[izvorDana]izvorSata{izSatnog: satniIzvor(l.SatnoQ, izdanoH), izDnevnog: dnevniIzvor(dnevniQU, dnevne, izdanoH)}
	return nizPrikaza{Cm: spojiStupce(sidra, izvori, cm, SatiPrikaza), Q: spojiStupce(sidra, izvori, q, SatiPrikaza)}
}

// ugodjeniNiz je niz letve s ugađanjem: u svakom satu dnevni model i satni
// lanac s težinom prognoza.TezinaLanca, a gdje je samo jedan od njih, on.
func ugodjeniNiz(l PregledLetve, dnevne []prognoza.DnevnaIzdana, izdanoH int64) nizPrikaza {
	return nizPrikaza{
		Cm:      ugodi(satniIzvor(l.Satno, izdanoH), dnevniIzvor(dnevniU, dnevne, izdanoH)),
		Q:       ugodi(satniIzvor(l.SatnoQ, izdanoH), dnevniIzvor(dnevniQU, dnevne, izdanoH)),
		Ugodjen: true,
	}
}

func ugodi(satni, dnevni izvorSata) []tockaPrikaza {
	out := make([]tockaPrikaza, SatiPrikaza+1)
	for h := 1; h <= SatiPrikaza; h++ {
		c, d := uSatu(satni, h), uSatu(dnevni, h)
		switch {
		case c.ok && !c.drzi && d.ok:
			out[h] = pomijesaj(c.v, d.v, 1-prognoza.TezinaLanca(h))
		case c.ok && !c.drzi:
			out[h] = c.v
		default:
			out[h] = d.prava()
		}
	}
	return out
}

// ugodiCelije upisuje stupce dana letvama s ugađanjem iz njihova niza, s
// oznakom ds gdje su satni lanac i dnevni model zajedno. Gdje niz ima samo
// satni lanac (dnevne u izdanju nema), stupci ostaju kakve ih je složila
// tablica.
func ugodiCelije(letve []LetvaPrognoze, prikaz map[string]nizPrikaza, ciljevi []int64, izdanoH int64,
	postaje map[string]models.Station) {
	for i := range letve {
		x := &letve[i]
		n, ima := prikaz[x.Kod]
		if !ima || !n.Ugodjen {
			continue
		}
		for k := 0; k < len(x.Dani) && k < len(ciljevi); k++ {
			h := int(ciljevi[k] - izdanoH)
			if h > SatiPrikaza || !n.Cm[h].Ima || n.Cm[h].Dnevni == 0 {
				continue
			}
			upisiUgodjeno(&x.Dani[k], n.Cm[h], n.Q[h], postaje[x.Kod])
		}
	}
}

// upisiUgodjeno stavlja u dan vrijednost niza s ugađanjem.
func upisiUgodjeno(c *CelijaDana, cm, q tockaPrikaza, st models.Station) {
	c.Cm, c.CmV, c.Raspon = brojHRf(cm.Vrijednost, 0), ptr(cm.Vrijednost, true), rasponUz(cm.Vrijednost, cm.Dolje, cm.Gore)
	c.Q, c.QV, c.QRaspon = "", nil, ""
	if q.Ima {
		c.Q, c.QV, c.QRaspon = brojHRf(q.Vrijednost, 0), ptr(q.Vrijednost, true), rasponUz(q.Vrijednost, q.Dolje, q.Gore)
	}
	c.Dnevna, c.Kombinacija = cm.Dnevni == 1, cm.Dnevni < 1
	c.IzSatne, c.BezDnevne, c.Slabija = false, false, false
	c.Razina, c.Moguce = razinaObrane(st, cm.Vrijednost), ""
	if g := razinaObrane(st, cm.Gore); g != c.Razina {
		c.Moguce = g
	}
}

// oznakaUdjela je oznaka vrijednosti po udjelu dnevnog modela u njoj.
func oznakaUdjela(dnevni float64) OznakaModela {
	switch {
	case dnevni <= 0:
		return oznakaSatnog
	case dnevni >= 1:
		return oznakaDnevnog
	}
	return oznakaKombinacije
}

// nizLetve je niz letve iz njezinih modela: uz kombinaciju na letvi s
// ugađanjem oba zajedno, inače po pravilu tablice.
func (u ulazPrikaza) nizLetve(l PregledLetve) nizPrikaza {
	if u.nacin == models.PrognozaKombinacija && prognoza.Ugadja(l.Letva) {
		return ugodjeniNiz(l, u.dnevne[l.Letva], u.izdanoH)
	}
	return prikazLetve(odDanaZa(l.Letva, u.nacin, l), l, u.dnevne[l.Letva], u.ciljevi, u.izdanoH)
}

// ulazPrikaza je sve iz čega se slažu nizovi izdanja.
type ulazPrikaza struct {
	pregled  []PregledLetve
	dnevne   map[string][]prognoza.DnevnaIzdana
	postaje  map[string]models.Station
	ciljevi  []int64
	izdanoH  int64
	nacin    string
	susjedi  bool               // dani iz susjeda uključeni (Postavke prognoze)
	srednjak map[string]float64 // izmjereni srednjak 24 sata do izdanja, letvama iz susjeda
}

// prikazIzdanja slaže niz svake letve izdanja, s danima iz susjeda gdje su
// uključeni, istim pravilom kao tablica.
func prikazIzdanja(u ulazPrikaza) map[string]nizPrikaza {
	out := map[string]nizPrikaza{}
	po := map[string]PregledLetve{}
	for _, l := range u.pregled {
		po[l.Letva] = l
		out[l.Letva] = u.nizLetve(l)
	}
	if !u.susjedi {
		return out
	}
	for _, s := range LetveIzSusjeda {
		l, imaX := po[s.Letva]
		w, imaW := tezinaSusjeda(u.postaje, s)
		if !imaX || !imaW || l.Nepovezana {
			continue
		}
		gore, imaG := u.susjedPrikaza(po, out, s.Uzvodna)
		dolje, imaD := u.susjedPrikaza(po, out, s.Nizvodna)
		if !imaG || !imaD {
			continue
		}
		sada, imaSada := l.Sada["vodostaj"]
		srednjak, imaSrednjak := u.srednjak[s.Letva]
		p := polazisteLetve{srednjak: srednjak, sada: sada, imaSrednjak: imaSrednjak, imaSada: imaSada}
		out[s.Letva] = u.prikazIzSusjeda(l, out[s.Letva], w, p, gore, dolje)
	}
	return out
}

// susjedPrikaza je niz susjeda s onim što treba za promjenu od polazišta.
type susjedPrikaza struct {
	niz     []tockaPrikaza
	sada    float64
	imaSada bool
	nula    float64
	imaNulu bool
	dnevne  []prognoza.DnevnaIzdana
}

func (u ulazPrikaza) susjedPrikaza(po map[string]PregledLetve, nizovi map[string]nizPrikaza, kod string) (susjedPrikaza, bool) {
	l, ima := po[kod]
	if !ima {
		return susjedPrikaza{}, false
	}
	s := susjedPrikaza{niz: nizovi[kod].Cm, dnevne: u.dnevne[kod]}
	s.sada, s.imaSada = l.Sada["vodostaj"]
	s.nula, s.imaNulu = danNula(s.dnevne)
	return s, true
}

// vrijednostIz je vrijednost letve po jednom susjedu u satu h: polazište letve
// i promjena susjeda od njegova polazišta, oboje vagnuto udjelom dnevnog
// modela u nizu susjeda. U satu stupca udio je 0 ili 1, pa je to isto što
// tablica računa za dan (dopuniLetvu).
func (s susjedPrikaza) vrijednostIz(h int, izdanoH int64, p polazisteLetve) (float64, bool) {
	if h >= len(s.niz) || !s.niz[h].Ima {
		return 0, false
	}
	if _, ima := dnevniU(s.dnevne, izdanoH+int64(h)); !ima {
		return 0, false
	}
	d := s.niz[h].Dnevni
	var polaziste, nas float64
	if d < 1 {
		pl, imaP := p.za(false)
		if !s.imaSada || !imaP {
			return 0, false
		}
		polaziste, nas = (1-d)*s.sada, (1-d)*pl
	}
	if d > 0 {
		pl, imaP := p.za(true)
		if !s.imaNulu || !imaP {
			return 0, false
		}
		polaziste, nas = polaziste+d*s.nula, nas+d*pl
	}
	return nas + s.niz[h].Vrijednost - polaziste, true
}

// prikazIzSusjeda je niz letve kojoj od DanIzSusjeda dane daju susjedi: stupac
// koji tablica računa iz susjeda i ovdje dolazi iz njih, a između stupaca ide
// postupni prijelaz. Vrijednost iz susjeda nema raspona ni protoka.
func (u ulazPrikaza) prikazIzSusjeda(l PregledLetve, vlastiti nizPrikaza, w float64, p polazisteLetve,
	gore, dolje susjedPrikaza) nizPrikaza {
	susjedi := func(h int) (tockaPrikaza, bool, bool) {
		vg, okG := gore.vrijednostIz(h, u.izdanoH, p)
		vd, okD := dolje.vrijednostIz(h, u.izdanoH, p)
		if !okG || !okD {
			return tockaPrikaza{}, false, false
		}
		v := w*vg + (1-w)*vd
		return tockaPrikaza{PregledVrijednost: PregledVrijednost{Vrijednost: v, Dolje: v, Gore: v}, Ima: true, Susjedi: 1}, true, false
	}
	sidra := sidraDana(u.ciljevi, u.izdanoH)
	izvori := izvoriLetve(odDanaZa(l.Letva, u.nacin, l), l, u.dnevne[l.Letva], u.ciljevi)
	for k := DanIzSusjeda; k < len(sidra); k++ {
		if u.nacin == models.PrognozaSatni && vlastiti.Cm[min(sidra[k], SatiPrikaza)].Ima {
			continue // u satnom načinu susjedi daju samo dane do kojih satni lanac ne seže
		}
		if _, ok, _ := susjedi(sidra[k]); ok {
			izvori[k] = izSusjeda
		}
	}
	dnevne := u.dnevne[l.Letva]
	cm := map[izvorDana]izvorSata{izSatnog: satniIzvor(l.Satno, u.izdanoH),
		izDnevnog: dnevniIzvor(dnevniU, dnevne, u.izdanoH), izSusjeda: susjedi}
	q := map[izvorDana]izvorSata{izSatnog: satniIzvor(l.SatnoQ, u.izdanoH), izDnevnog: dnevniIzvor(dnevniQU, dnevne, u.izdanoH)}
	return nizPrikaza{Cm: spojiStupce(sidra, izvori, cm, SatiPrikaza), Q: spojiStupce(sidra, izvori, q, SatiPrikaza)}
}

// satniStupci upisuje stupce +6 h i +12 h iz niza, da tablica ondje pokazuje
// isto što i graf: kad dnevni model daje već prvi dan, crta prema njemu kreće
// od sata izdanja, pa i ti stupci nose dio dnevnog modela.
func satniStupci(letve []LetvaPrognoze, prikaz map[string]nizPrikaza) {
	for i := range letve {
		n, ima := prikaz[letve[i].Kod]
		if !ima {
			continue
		}
		for j := range letve[i].Vrijednosti {
			v := &letve[i].Vrijednosti[j]
			d := v.DosegH
			if d >= len(n.Cm) {
				continue
			}
			*v = VrijednostPrognoze{DosegH: d}
			if cm := n.Cm[d]; cm.Ima {
				v.Ima, v.Cm, v.CmRaspon, v.CmV = true, brojHRf(cm.Vrijednost, 0), granice(cm.PregledVrijednost), ptr(cm.Vrijednost, true)
				v.Slabija, v.Oznaka = !cm.BoljaOdPostojanosti, oznakaUdjela(cm.Dnevni)
			}
			if q := n.Q[d]; q.Ima {
				v.Q, v.QRaspon, v.QV = brojHRf(q.Vrijednost, 0), granice(q.PregledVrijednost), ptr(q.Vrijednost, true)
				if !v.Ima {
					v.Slabija, v.Oznaka = !q.BoljaOdPostojanosti, oznakaUdjela(q.Dnevni)
				}
				v.Ima = true
			}
		}
	}
}

// PrikazZaLetvu je prognoza letve za graf na stranici letve, iz najnovijeg
// izdanja: isti niz koji pokazuje tablica na stranici prognoza, od sata
// izdanja do kraja šestog dana.
func (h *PrognozeHandler) PrikazZaLetvu(ctx context.Context, letva, velicina string) *PrognozaNiza {
	c := h.citacBaze()
	if c == nil || letva == "" {
		return nil
	}
	izdano, letve, err := c.Pregled()
	if err != nil || len(letve) == 0 {
		return nil
	}
	dnevne, _ := c.DnevnoZa(izdano) // bez dnevne prognoze graf ide satnim lancem, kao i tablica
	postaje := h.postaje(ctx)
	_, ciljevi := daniPregleda(izdano)
	susjedi, srednjaci := h.susjediIzdanja(ctx, postaje, izdano)
	n, ima := prikazIzdanja(ulazPrikaza{pregled: letve, dnevne: dnevne, postaje: postaje, ciljevi: ciljevi,
		izdanoH: izdano.Unix() / 3600, nacin: h.nacini(ctx).PrognozaPrikaz, susjedi: susjedi, srednjak: srednjaci})[letva]
	if !ima {
		return nil
	}
	p := prognozaIzNiza(izdano, sadaLetve(letve, letva, velicina), n.uVelicini(velicina))
	if p == nil {
		return nil
	}
	u, _ := c.Upozorenja(izdano)
	p.Upitno = upitnoLetve(u, letva)
	return p
}

// uVelicini je niz u vodostaju ili protoku.
func (n nizPrikaza) uVelicini(velicina string) []tockaPrikaza {
	if velicina == "protok" {
		return n.Q
	}
	return n.Cm
}

// sadaLetve je vrijednost letve u satu izdanja, kad je ima.
func sadaLetve(letve []PregledLetve, letva, velicina string) *float64 {
	for _, l := range letve {
		if v, ima := l.Sada[velicina]; ima && l.Letva == letva {
			return &v
		}
	}
	return nil
}

// prognozaIzNiza je niz kakav ide na graf: od vrijednosti u satu izdanja
// dalje; manje od dvije točke nije crta.
func prognozaIzNiza(izdano time.Time, sada *float64, niz []tockaPrikaza) *PrognozaNiza {
	p := &PrognozaNiza{Izdano: izdano, Udio: int(math.Round(prognoza.UdioURasponu * 100))}
	if sada != nil {
		p.Tocke = append(p.Tocke, TockaPrognoze{Kad: izdano, Vrijednost: *sada, Dolje: *sada, Gore: *sada})
	}
	for sat := 1; sat < len(niz); sat++ {
		if t := niz[sat]; t.Ima {
			p.Tocke = append(p.Tocke, TockaPrognoze{Kad: izdano.Add(time.Duration(sat) * time.Hour),
				Vrijednost: t.Vrijednost, Dolje: t.Dolje, Gore: t.Gore})
		}
	}
	if len(p.Tocke) < 2 {
		return nil
	}
	return p
}
