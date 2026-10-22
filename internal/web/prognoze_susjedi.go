package web

import (
	"context"
	"math"
	"time"

	"gocop/internal/models"
	"gocop/internal/prognoza"
	"gocop/internal/repository"
)

// Dani letvi bez dnevnog modela iz susjeda (Odluke (22. 10. 2026.)).
//
// Novo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs nemaju dnevni
// model s kišom. Dani su im dolazili samo iz satnog lanca, koji kišu ne vidi,
// pa su stajali ravno dok su susjedi rasli, a 5. i 6. dan bili su prazni. Od
// 2. dana sad ih daju susjedi uzvodno i nizvodno koji dnevni model imaju:
// promjena koju tablica pokazuje na svakom susjedu, utegnuta po riječnom
// kilometru, dodaje se vrijednosti letve. Promjena dana iz dnevnog modela
// mjeri se od njegova dana 0 (srednjak 24 sata do izdanja) i dodaje srednjaku
// letve istih sati; promjena dana iz satnog lanca mjeri se od vrijednosti u
// satu izdanja i dodaje njezinoj. Uzimaju se dani kako ih tablica susjeda
// pokazuje, pa vrijedi i njihov dan prijelaza na dnevni model.
//
// Provjereno na svim izdanjima 2024.–2025. s tada izdanim prognozama kiše:
// 2.–6. dan bolje od ravne crte na svih pet letvi (Novo Virje 2., 4. i 6. dan
// 16,5, 20,9 i 25,0 cm prema 22,0, 31,6 i 34,2), a na deset kišnih valova
// 2014.–2024. još više (Novo Virje 4. dan 29 cm prema 90). Rizik: letva
// slijedi susjede i kad porast koji im dnevni model najavi ne dođe (tada Novo
// Virje 4. dan 43 cm prema 27 ravno). Prvi dan ostaje satni lanac: u valu je
// bolji.

// LetvaIzSusjeda je letva bez dnevnog modela i njezini susjedi s dnevnim
// modelom na istoj rijeci, uzvodni i nizvodni.
type LetvaIzSusjeda struct{ Letva, Uzvodna, Nizvodna string }

// LetveIzSusjeda su letve kojima dane daju susjedi. Drávaszabolcs leži malo
// ispod Donjeg Miholjca, pa su mu susjedi Donji Miholjac i Belišće.
var LetveIzSusjeda = []LetvaIzSusjeda{
	{"novo-virje", "botovo", "terezino-polje"},
	{"szentborbas", "terezino-polje", "donji-miholjac"},
	{"vrbovka", "terezino-polje", "donji-miholjac"},
	{"moslavina", "terezino-polje", "donji-miholjac"},
	{"dravaszabolcs", "donji-miholjac", "belisce"},
}

// DanIzSusjeda je prvi dan tablice koji daju susjedi.
const DanIzSusjeda = 2

// satiSrednjaka je koliko sati od 24 treba za srednjak, kao i za dan 0
// dnevnog modela.
const satiSrednjaka = 18

// dopuniSusjede upisuje dane iz susjeda, osim kad ih je uprava isključila u
// Postavkama prognoze. U satnom načinu prikaza dopunjuje samo dane do kojih
// satni lanac ne seže, kao što ondje i dnevni model daje samo njih.
func (h *PrognozeHandler) dopuniSusjede(ctx context.Context, letve []LetvaPrognoze, pregled []PregledLetve,
	dnevne map[string][]prognoza.DnevnaIzdana, postaje map[string]models.Station, ciljevi []int64,
	izdano time.Time, nacin string) {
	if h.nacini(ctx).PrognozaBezSusjeda {
		return
	}
	dopuniIzSusjeda(tablicaSusjeda{letve: letve, pregled: pregled, dnevne: dnevne, postaje: postaje, ciljevi: ciljevi},
		h.srednjaciDana(ctx, postaje, izdano), nacin == models.PrognozaSatni)
}

// tablicaSusjeda je ono iz čega se dani računaju: retci tablice i pregleda
// istim redom (opisiLetve), dnevne prognoze izdanja i registar letvi.
type tablicaSusjeda struct {
	letve   []LetvaPrognoze
	pregled []PregledLetve
	dnevne  map[string][]prognoza.DnevnaIzdana
	postaje map[string]models.Station
	ciljevi []int64
}

// dopuniIzSusjeda upisuje letvama iz LetveIzSusjeda dane od DanIzSusjeda.
// Letva bez kilometra, bez nekog susjeda u izdanju ili koja nije povezana sa
// živom vodom ostaje kakva jest.
func dopuniIzSusjeda(t tablicaSusjeda, srednjaci map[string]float64, samoPrazne bool) {
	mjesto := map[string]int{}
	for i, l := range t.letve {
		mjesto[l.Kod] = i
	}
	for _, s := range LetveIzSusjeda {
		ix, imaX := mjesto[s.Letva]
		gore, imaU := t.susjed(mjesto, s.Uzvodna)
		dolje, imaD := t.susjed(mjesto, s.Nizvodna)
		w, imaW := tezinaSusjeda(t.postaje, s)
		if !imaX || !imaU || !imaD || !imaW || t.pregled[ix].Nepovezana {
			continue
		}
		sada, imaSada := t.pregled[ix].Sada["vodostaj"]
		srednjak, imaSrednjak := srednjaci[s.Letva]
		p := polazisteLetve{srednjak: srednjak, sada: sada, imaSrednjak: imaSrednjak, imaSada: imaSada}
		dopuniLetvu(&t.letve[ix], t.postaje[s.Letva], t.ciljevi, w, p, gore, dolje, samoPrazne)
	}
}

// susjed je redak tablice susjeda s onim iz čega je nastao.
type susjed struct {
	dani    []CelijaDana
	pregled PregledLetve
	dnevne  []prognoza.DnevnaIzdana
}

func (t tablicaSusjeda) susjed(mjesto map[string]int, kod string) (susjed, bool) {
	i, ima := mjesto[kod]
	if !ima {
		return susjed{}, false
	}
	return susjed{dani: t.letve[i].Dani, pregled: t.pregled[i], dnevne: t.dnevne[kod]}, true
}

// promjena je koliko dan k susjeda odstupa od polazne vrijednosti i je li
// dan iz dnevnog modela. Susjed bez dnevne prognoze za taj termin u ovom
// izdanju ne daje ništa, pa letva taj dan ostaje kakva jest.
func (s susjed) promjena(k int, t int64) (cm float64, dnevni, ok bool) {
	if _, ima := dnevniU(s.dnevne, t); !ima || k >= len(s.dani) || s.dani[k].CmV == nil {
		return 0, false, false
	}
	c := s.dani[k]
	if c.Dnevna {
		nula, ima := danNula(s.dnevne)
		return *c.CmV - nula, true, ima
	}
	sada, ima := s.pregled.Sada["vodostaj"]
	return *c.CmV - sada, false, ima
}

// danNula je dan 0 dnevne prognoze: izmjereni srednjak 24 sata do izdanja.
func danNula(dnevne []prognoza.DnevnaIzdana) (float64, bool) {
	for _, d := range dnevne {
		if d.Dan == 0 {
			return d.Vrijednost, true
		}
	}
	return 0, false
}

// polazisteLetve su vrijednosti letve kojima se dodaje promjena susjeda.
type polazisteLetve struct {
	srednjak, sada       float64
	imaSrednjak, imaSada bool
}

// za vraća polazište za promjenu dana iz dnevnog modela ili satnog lanca.
func (p polazisteLetve) za(dnevni bool) (float64, bool) {
	if dnevni {
		return p.srednjak, p.imaSrednjak
	}
	return p.sada, p.imaSada
}

// dopuniLetvu računa dane letve od DanIzSusjeda: w je udio uzvodnog susjeda.
func dopuniLetvu(x *LetvaPrognoze, st models.Station, ciljevi []int64, w float64, p polazisteLetve,
	gore, dolje susjed, samoPrazne bool) {
	for k := DanIzSusjeda - 1; k < len(ciljevi) && k < len(x.Dani); k++ {
		if samoPrazne && x.Dani[k].CmV != nil {
			continue
		}
		cmU, dnevniU, okU := gore.promjena(k, ciljevi[k])
		cmD, dnevniD, okD := dolje.promjena(k, ciljevi[k])
		pU, imaU := p.za(dnevniU)
		pD, imaD := p.za(dnevniD)
		if okU && okD && imaU && imaD {
			upisiIzSusjeda(&x.Dani[k], w*(pU+cmU)+(1-w)*(pD+cmD), st)
			x.ImaTermina = true
		}
	}
}

// upisiIzSusjeda stavlja vrijednost iz susjeda u dan. Raspona i protoka nema:
// nisu izmjereni za ovaj račun, pa se ne izmišljaju.
func upisiIzSusjeda(c *CelijaDana, v float64, st models.Station) {
	c.Cm, c.CmV, c.Raspon = brojHRf(v, 0), ptr(v, true), ""
	c.Q, c.QV, c.QRaspon = "", nil, ""
	c.Dnevna, c.IzSatne, c.BezDnevne, c.Slabija, c.IzSusjeda = false, false, false, false, true
	c.Razina, c.Moguce = razinaObrane(st, v), ""
}

// tezinaSusjeda je udio uzvodnog susjeda u vrijednosti letve: 1 na njemu, 0
// na nizvodnom, a između po riječnom kilometru. Bez kilometra u registru, ili
// kad letva ne leži između susjeda, težine nema.
func tezinaSusjeda(postaje map[string]models.Station, s LetvaIzSusjeda) (float64, bool) {
	x, imaX := rijecniKm(postaje[s.Letva].Stationing)
	u, imaU := rijecniKm(postaje[s.Uzvodna].Stationing)
	d, imaD := rijecniKm(postaje[s.Nizvodna].Stationing)
	if !imaX || !imaU || !imaD || u <= x || x <= d {
		return 0, false
	}
	return (x - d) / (u - d), true
}

// srednjaciDana čita izmjereni srednjak 24 sata do izdanja letvama kojima dane
// daju susjedi: njemu se dodaje promjena dana iz dnevnog modela susjeda.
func (h *PrognozeHandler) srednjaciDana(ctx context.Context, postaje map[string]models.Station, izdano time.Time) map[string]float64 {
	out := map[string]float64{}
	if h.readings == nil {
		return out
	}
	for _, s := range LetveIzSusjeda {
		st, ima := postaje[s.Letva]
		if !ima {
			continue
		}
		rs, err := h.readings.List(ctx, repository.ReadingFilter{StationID: st.ID.String(),
			From: izdano.Add(-23*time.Hour - 30*time.Minute), To: izdano})
		if err != nil {
			// Letva ostaje bez srednjaka: dani iz dnevnog modela susjeda
			// joj se ne računaju, a tablica pokazuje satni lanac kao i dosad.
			continue
		}
		if v, ima := srednjakDana(rs, izdano); ima {
			out[s.Letva] = v
		}
	}
	return out
}

// srednjakDana je srednjak sati od 23 sata prije izdanja do sata izdanja.
// Sat uzima očitanje najbliže punom satu, najviše pola sata daleko.
func srednjakDana(rs []models.Reading, izdano time.Time) (float64, bool) {
	sati := map[int]float64{}
	odmak := map[int]time.Duration{}
	for _, rd := range rs {
		razmak := rd.MeasuredAt.Sub(izdano)
		sat := int(math.Round(razmak.Hours()))
		o := (razmak - time.Duration(sat)*time.Hour).Abs()
		if prije, bio := odmak[sat]; rd.LevelCm == nil || sat > 0 || sat < -23 || bio && prije <= o {
			continue
		}
		sati[sat], odmak[sat] = float64(*rd.LevelCm), o
	}
	if len(sati) < satiSrednjaka {
		return 0, false
	}
	zbroj := 0.0
	for _, v := range sati {
		zbroj += v
	}
	return zbroj / float64(len(sati)), true
}

// napomenaSusjeda je rečenica za napomenu lista u Excelu kad neka letva na
// njemu ima dan iz susjeda; u Excelu oznake p nema, samo skriveni redak.
func napomenaSusjeda(letve []LetvaPrognoze) string {
	for _, x := range letve {
		for _, d := range x.Dani {
			if d.IzSusjeda {
				return "\nNovo Virje, Szentborbás, Vrbovka, Moslavina i Drávaszabolcs nemaju dnevni model, pa od " +
					"drugog dana (skriveni redak „model”: iz susjeda) slijede susjedne postaje koje ga imaju, " +
					"razmjerno udaljenosti po rijeci, bez raspona. Kad dnevni model susjedima najavi porast od " +
					"kiše koji ne dođe, i ondje je najava pogrešna."
			}
		}
	}
	return ""
}
