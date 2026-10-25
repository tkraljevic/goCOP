package skok

import (
	"context"
	"database/sql"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
)

// Prag skoka je po letvi, iz arhive: koliko se letva u satu zaista mijenja.
// Za svaku godinu arhive visok percentil satnih promjena (Percentil), od
// godišnjih srednja vrijednost (medijan), puta Rezerva, zaokruženo na 10 cm
// naviše, a najmanje NajmanjiPrag. Percentil, a ne najveća promjena, jer i
// arhiva ima lažnih skokova (Čačinci-Vojlovica 1554 cm, Donja Dubrava
// 1537 cm u satu). Medijan godina, a ne percentil cijele arhive, jer i
// percentil podigne godina u kojoj je mjerilo dugo bilo u kvaru: Donjem
// Miholjcu dva ljetna mjeseca 2026. (letva-hv skače između −115 i −75 cm)
// podignu percentil cijele arhive sa 7 na 29 cm, a prag s 50 na 90, pa bi
// prošli skokovi od 60 cm koji se vrate. Rezerva pokriva rijetke prave
// promjene iznad percentila. Letve ispod hidroelektrana tako same dobiju
// veći prag: Botovo 90, Varaždin 110, Donja Dubrava 250 cm. Pravilo ionako
// ne odbacuje veliku promjenu, nego samo onu koja se idući sat vrati.
//
// Ustave (šifra počinje s „ustava-”) zadržavaju percentil cijele arhive:
// manevar ustave diže i spušta vodu nizvodno za 70–90 cm u sat-dva
// (Ustava Kopačevo nizvodno, srpanj 2015.), a medijan godina dao bi im prag
// 50 cm.
const (
	// NajmanjiPrag je najmanji prag skoka (cm u satu). Mirne letve Dunava
	// mijenjaju se manje od 10 cm u satu; kvar je na Donjem Miholjcu bio
	// 950 cm.
	NajmanjiPrag = 50.0
	// ZadaniPrag je prag letve koja u arhivi nema dovoljno satnih promjena
	// (nova letva, letva samo s dnevnim vrijednostima, kota akumulacije).
	// Dvostruko je od najmanjeg jer takva letva može biti i ispod elektrane;
	// lažni skokovi koji prognozi smetaju ionako su stotine cm.
	ZadaniPrag = 100.0
	// Percentil satnih promjena iz kojeg se prag računa.
	Percentil = 0.999
	// Rezerva množi percentil.
	Rezerva = 3.0
	// NajmanjePromjena je koliko satnih promjena treba da percentil ima
	// smisla (oko tri mjeseca satnih očitanja).
	NajmanjePromjena = 2000
	// OdGodine je prva godina arhive iz koje se prag računa: od 2012.
	// satni nizovi postoje za gotovo sve letve, a starije satne vrijednosti
	// su često razvučene iz dvokratnih očitanja.
	OdGodine = 2012
)

// PragIzGodina je prag skoka iz satnih promjena razine po godinama (cm,
// bilo kojim redom): medijan godišnjih percentila, a za ustavu percentil
// svih promjena. Godina s manje od NajmanjePromjena promjena ne daje svoj
// percentil; kad nijedna nema dovoljno, vrijedi percentil svih, a kad je
// ni svih nema dovoljno, ZadaniPrag.
func PragIzGodina(poGodinama map[int][]float64, ustava bool) float64 {
	var sve, godisnji []float64
	for _, p := range poGodinama {
		sve = append(sve, p...)
		if len(p) >= NajmanjePromjena {
			godisnji = append(godisnji, percentil(p))
		}
	}
	if len(sve) < NajmanjePromjena {
		return ZadaniPrag
	}
	v := percentil(sve)
	if !ustava && len(godisnji) > 0 {
		v = medijan(godisnji)
	}
	return math.Max(NajmanjiPrag, math.Ceil(Rezerva*v/10)*10)
}

// percentil je Percentil apsolutnih vrijednosti promjena (ne praznih).
func percentil(promjene []float64) float64 {
	p := slices.Clone(promjene)
	for i := range p {
		p[i] = math.Abs(p[i])
	}
	slices.Sort(p)
	return p[int(math.Ceil(Percentil*float64(len(p))))-1]
}

// medijan je srednja vrijednost (ne praznih), uz paran broj prosjek dviju
// srednjih.
func medijan(v []float64) float64 {
	p := slices.Sorted(slices.Values(v))
	n := len(p)
	return (p[(n-1)/2] + p[n/2]) / 2
}

// jeUstava javlja je li letva vodomjer ustave (šifra „ustava-…”).
func jeUstava(letva string) bool { return strings.HasPrefix(letva, "ustava-") }

// Pragovi daju prag skoka po šifri letve. Prag letve računa se iz arhive
// kad ga prvi put zatraži preuzimanje ili prognoza, i drži se u memoriji do
// gašenja programa: medijan petnaest godina ne mijenja se kad arhivi doda
// koji tjedan, a program se pri svakoj nadogradnji ponovo pokreće. Bez
// arhive, ili kad se arhiva ne da pročitati, vrijedi ZadaniPrag (bez
// pamćenja, pa ga idući put pokuša opet).
type Pragovi struct {
	arhiva *sql.DB
	mu     sync.Mutex
	po     map[string]float64
}

// NoviPragovi daje pragove iz arhive; arhiva smije biti nil.
func NoviPragovi(arhiva *sql.DB) *Pragovi {
	return &Pragovi{arhiva: arhiva, po: map[string]float64{}}
}

// Za je prag skoka letve u cm u satu. Nil Pragovi daju ZadaniPrag.
func (p *Pragovi) Za(letva string) float64 {
	if p == nil || p.arhiva == nil {
		return ZadaniPrag
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ima := p.po[letva]; ima {
		return v
	}
	promjene, err := SatnePromjene(context.Background(), p.arhiva, letva)
	if err != nil {
		return ZadaniPrag
	}
	v := PragIzGodina(promjene, jeUstava(letva))
	p.po[letva] = v
	return v
}

// SatnePromjene su promjene spojenog satnog vodostaja letve u arhivi od
// OdGodine između dva uzastopna puna sata, po godini (UTC) kasnijeg sata.
// Sati između punih (desetminutni nizovi) ne ulaze: prag je promjena u
// satu.
func SatnePromjene(ctx context.Context, arhiva *sql.DB, letva string) (map[int][]float64, error) {
	od := time.Date(OdGodine, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	rows, err := arhiva.QueryContext(ctx, `SELECT vrijeme, vrijednost FROM spoj
		WHERE letva = ? AND velicina = 'vodostaj' AND korak = 'satni' AND vrijeme >= ? AND vrijeme % 3600 = 0
		ORDER BY vrijeme`, letva, od)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int][]float64{}
	var prije int64
	var vPrije float64
	for rows.Next() {
		var t int64
		var v float64
		if err := rows.Scan(&t, &v); err != nil {
			return nil, err
		}
		if t-prije == 3600 {
			g := time.Unix(t, 0).UTC().Year()
			out[g] = append(out[g], v-vPrije)
		}
		prije, vPrije = t, v
	}
	return out, rows.Err()
}
