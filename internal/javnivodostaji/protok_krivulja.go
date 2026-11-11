package javnivodostaji

import (
	"context"
	"sync/atomic"

	"gocop/internal/models"
)

// Protok iz naše krivulje.
//
// Hrvatske vode su 6. 11. 2026. postaje prebacile na nove brojeve. Na novom
// broju dio postaja ima vodostaj, ali nema protok (Botovo, Novo Virje,
// Tuhovec…), jer je ondje protok računala letva iz krivulje upisane na starom
// zapisu. Krivulje istih letvi imamo u arhivi, pa protok izračunamo sami kad
// ga izvor ne da. Očitanje to i kaže: način je krivulja, a bilješka govori
// čija. Isto vrijedi kad izvor umjesto protoka da 0 tamo gdje rijeka teče.

// KrivuljeLetve daje krivulje protoka letve po šifri (botovo, tuhovec…),
// najnovija prva. Prazno kad letva krivulje nema.
type KrivuljeLetve func(ctx context.Context, sifra string) ([]models.HQKrivulja, error)

// BiljeskaNaseKrivulje stoji uz protok koji je izračunao goCOP
const BiljeskaNaseKrivulje = "izračunat iz vodostaja krivuljom iz arhive goCOP-a, izvor nije dao protok"

// greskaKrivuljaJavljena: greška čitanja krivulja javlja se jednom, a ne za
// svaku letvu svaki sat (čvor bez arhive inače bi zatrpao dnevnik).
var greskaKrivuljaJavljena atomic.Bool

// protokIzKrivulje dopuni retke bez protoka protokom iz naše krivulje
func (u *Uvoznik) protokIzKrivulje(ctx context.Context, st *models.Station, skupine ...[]Redak) {
	if u.Krivulje == nil || !trebaProtok(skupine...) {
		return
	}
	krivulje, err := u.Krivulje(ctx, st.Code)
	if err != nil {
		if !greskaKrivuljaJavljena.Swap(true) {
			u.Zapisnik("javni vodostaji: protok iz krivulje se ne računa, krivulje se ne mogu pročitati: %v", err)
		}
		return
	}
	for _, redci := range skupine {
		odbaciNulu(redci, krivulje)
		dopuniKrivuljom(redci, krivulje)
	}
}

// trebaProtok javlja ima li redak s vodostajem, a bez protoka ili s
// protokom 0 (vidi odbaciNulu)
func trebaProtok(skupine ...[]Redak) bool {
	for _, redci := range skupine {
		for _, r := range redci {
			if r.LevelCm != nil && (r.FlowM3s == nil || *r.FlowM3s <= 0) {
				return true
			}
		}
	}
	return false
}

// odbaciNulu briše protok 0 (ili manji) koji je izvor dao uz vodostaj na
// kojem naša krivulja kaže da rijeka teče (models.HQKrivulja.NulaNijeProtok).
// Takav redak poslije dopuni naša krivulja, ako je vodostaj u njezinu
// rasponu; inače ostaje bez protoka, a prognoza ga računa iz vodostaja.
// Vraća koliko je nula odbačeno.
func odbaciNulu(redci []Redak, krivulje []models.HQKrivulja) int {
	n := 0
	for i := range redci {
		r := &redci[i]
		if r.FlowM3s == nil || *r.FlowM3s > 0 || r.LevelCm == nil {
			continue
		}
		k := models.KrivuljaUTrenutku(krivulje, r.Kad)
		if k == nil || !k.NulaNijeProtok(*r.LevelCm) {
			continue
		}
		r.FlowM3s, r.FlowMetoda, r.FlowBiljeska = nil, "", ""
		n++
	}
	return n
}

// dopuniKrivuljom upiše protok retcima koji imaju vodostaj, a nemaju protok,
// krivuljom koja vrijedi u tom trenutku. Izvan raspona krivulje protok
// ostaje prazan: ondje ga nitko nije mjerio. Vraća koliko je redaka dopunjeno.
func dopuniKrivuljom(redci []Redak, krivulje []models.HQKrivulja) int {
	n := 0
	for i := range redci {
		r := &redci[i]
		if r.FlowM3s != nil || r.LevelCm == nil {
			continue
		}
		k := models.KrivuljaUTrenutku(krivulje, r.Kad)
		if k == nil {
			continue
		}
		q, ok := k.Protok(*r.LevelCm)
		if !ok {
			continue
		}
		r.FlowM3s = floatPtr(q)
		r.FlowMetoda = models.FlowMethodKrivulja
		r.FlowBiljeska = BiljeskaNaseKrivulje
		n++
	}
	return n
}
