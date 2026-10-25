package web

import (
	"context"
	"log"
	"time"

	"gocop/internal/models"
	"gocop/internal/plato"
)

// Upozorenja o kvaru mjerila (Odluke (25. 10. 2026.)): na vrhu stranice
// letve okvir dok uređaj stoji zaglavljen ili se drži daleko od prave
// razine (paket plato), i okvir kad mjerilo često odskoči i vrati se
// (odbačeni lažni skokovi); u popisu na stranici Operativni vodostaji znak
// uz letvu. Na naslovnoj dežurnog i e-poštom zasad ne. Stanje se računa na
// ovom čvoru iz očitanja koja ima. Letve kojima razinu vodi čovjek ili
// elektrana (ustava, crpna stanica, akumulacija) nemaju ni jedno ni drugo.

// KvaroviMjerila daje stanje kvara mjerila letve (plato.Procjenitelj).
type KvaroviMjerila interface {
	Stanje(ctx context.Context, sifra string) (*plato.Kvar, error)
	Ime(ctx context.Context, sifra string) string
	Sifra(ctx context.Context, id string) string
	Izuzeta(ctx context.Context, sifra string) bool
}

// SetKvaroviMjerila daje rukovatelju prepoznavanje kvara mjerila; bez
// njega stranica letve i popis o kvaru ne govore.
func (h *ReadingsHandler) SetKvaroviMjerila(f func() KvaroviMjerila) { h.kvarovi = f }

func (h *ReadingsHandler) mjerila() KvaroviMjerila {
	if h.kvarovi == nil {
		return nil
	}
	return h.kvarovi()
}

// KvarLetve je ono što stranica letve kaže o kvaru mjerila koji traje.
type KvarLetve struct {
	Tekst   string // npr. „Mjerenje stoji na 412 cm od 25. 10. u 7 h, već 30 h, a …”
	Izbacen bool   // prognoza ova očitanja ne uzima (potvrđuje susjedna letva)
	NaDnu   bool   // vjerojatno nije kvar, nego je voda ispod dosega mjerila; prognoza ih uzima
}

// NajviseUPopisu: okvir čestih skokova nabraja najviše toliko zadnjih.
const NajviseUPopisu = 20

// CestiSkokovi su lažni skokovi letve odbačeni u zadnjih
// models.CestiSkokoviDana dana, kad ih je barem models.CestiSkokovi. Popis
// ima najviše NajviseUPopisu zadnjih, a Broj sve.
type CestiSkokovi struct {
	Broj  int
	Dana  int
	Popis []SkokLetve
}

// Skraceno javlja da popis nema sve skokove.
func (c CestiSkokovi) Skraceno() bool { return len(c.Popis) < c.Broj }

// SkokLetve je jedan odbačeni skok za popis.
type SkokLetve struct {
	Kad   time.Time
	Opis  string // npr. „skok na 830 cm”
	Izvor string
}

// ZnakLetve je znak uz letvu u popisu.
type ZnakLetve struct {
	Kvar    string // rečenica o kvaru koji traje; prazno kad ga nema
	NaDnu   bool   // to nije kvar, nego je voda vjerojatno ispod dosega mjerila
	Skokova int    // odbačenih skokova u zadnjih CestiSkokoviDana dana, kad ih je barem CestiSkokovi
}

// upozorenjaMjerila priprema okvire o kvaru mjerila i čestim skokovima za
// stranicu letve.
func (h *ReadingsHandler) upozorenjaMjerila(ctx context.Context, data *ReadingHistoryData, st *models.Station) {
	if st == nil {
		return
	}
	m := h.mjerila()
	if m != nil && m.Izuzeta(ctx, st.Code) {
		return
	}
	data.KvarMjerila = h.kvarLetve(ctx, m, st.Code)
	data.CestiSkokovi = h.cestiSkokovi(ctx, data.GaugeKey, time.Now())
}

// kvarLetve je kvar mjerila koji traje na letvi; nil kad ga nema ili se ne
// da provjeriti.
func (h *ReadingsHandler) kvarLetve(ctx context.Context, m KvaroviMjerila, sifra string) *KvarLetve {
	if m == nil || sifra == "" {
		return nil
	}
	k, err := m.Stanje(ctx, sifra)
	if err != nil {
		log.Printf("kvar mjerila %s se ne da provjeriti: %v", sifra, err)
		return nil
	}
	if k == nil {
		return nil
	}
	return &KvarLetve{Tekst: plato.Recenica(*k, func(s string) string { return m.Ime(ctx, s) }), Izbacen: k.BezPrognoze(), NaDnu: k.NaDnu}
}

// cestiSkokovi su odbačeni skokovi letve u zadnjih CestiSkokoviDana dana od
// sada, kad ih je barem CestiSkokovi; inače nil.
func (h *ReadingsHandler) cestiSkokovi(ctx context.Context, kljuc string, sada time.Time) *CestiSkokovi {
	skokovi, err := h.readingService.NedavniSkokovi(ctx, kljuc, sada.AddDate(0, 0, -models.CestiSkokoviDana))
	if err != nil {
		log.Printf("skokovi letve %s se ne daju pročitati: %v", kljuc, err)
		return nil
	}
	if len(skokovi) < models.CestiSkokovi {
		return nil
	}
	c := &CestiSkokovi{Broj: len(skokovi), Dana: models.CestiSkokoviDana}
	for _, s := range skokovi[:min(len(skokovi), NajviseUPopisu)] {
		c.Popis = append(c.Popis, SkokLetve{Kad: s.Kad, Opis: s.Opis, Izvor: models.Reading{Origin: s.Izvor}.OriginLabel()})
	}
	return c
}

// znakoviMjerila su znakovi uz letve u popisu, po ključu letve; letva bez
// kvara i bez čestih skokova nije u mapi.
func (h *ReadingsHandler) znakoviMjerila(ctx context.Context, letve []models.GaugeSummary, sada time.Time) map[string]ZnakLetve {
	broj, err := h.readingService.BrojNedavnihSkokova(ctx, sada.AddDate(0, 0, -models.CestiSkokoviDana))
	if err != nil {
		log.Printf("skokovi letvi se ne daju pročitati: %v", err)
	}
	m := h.mjerila()
	out := map[string]ZnakLetve{}
	for _, g := range letve {
		if z := h.znakLetve(ctx, m, g, broj[g.Key]); z != (ZnakLetve{}) {
			out[g.Key] = z
		}
	}
	return out
}

// znakLetve je znak uz jednu letvu: kvar koji traje (samo postaja, ne
// objekt) i broj nedavnih skokova kad ih je dosta. Izuzeta letva i vodomjer
// ustave ili crpne stanice nemaju znaka.
func (h *ReadingsHandler) znakLetve(ctx context.Context, m KvaroviMjerila, g models.GaugeSummary, skokova int) ZnakLetve {
	var z ZnakLetve
	if g.Kind == models.StructureKindSluice || g.Kind == models.StructureKindPumpingStation {
		return z
	}
	sifra := ""
	if m != nil && g.StationID != "" {
		if sifra = m.Sifra(ctx, g.StationID); m.Izuzeta(ctx, sifra) {
			return z
		}
	}
	if skokova >= models.CestiSkokovi {
		z.Skokova = skokova
	}
	if k := h.kvarLetve(ctx, m, sifra); k != nil {
		z.Kvar, z.NaDnu = k.Tekst, k.NaDnu
	}
	return z
}
