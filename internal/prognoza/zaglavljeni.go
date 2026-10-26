package prognoza

import (
	"cmp"
	"context"
	"database/sql"
	"log"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/plato"
)

// Kvar mjerila u očitanjima za prognozu (Odluke (25. 10. 2026.)): uređaj
// zaglavljen na jednoj vrijednosti ili mjerilo koje je odskočilo i ondje se
// drži (paket plato). Kad kvar potvrđuje susjedna letva, prognoza te sate ne
// uzima i radi kao da mjerenja nema: rupu premoste zamjena s druge obale,
// rekonstrukcija iz susjeda ili rezerva, kao i svaku drugu. Bez potvrde
// susjeda vrijednosti ostaju u računu. U oba slučaja uz izdanje stoji
// napomena. Očitanja se ne brišu. Iznimka je vrijednost na dnu mjerila
// (plato.Kvar.BezPrognoze): ostaje u računu, uz napomenu. Vrh lanca kojem
// kvar traje do kraja niza drži zadnju vrijednost prije kvara
// (drziVrhoveUKvaru), kao što vrh drži zadnje mjerenje za sate poslije
// izdanja.
//
// Filtar ide iza filtra lažnog skoka (bezSkokova) i gleda samo automatska
// očitanja: ručno upisano uvijek ulazi.

// PrepoznavacKvarova prepoznaje kvar mjerila u automatskim očitanjima
// letve (plato.Procjenitelj).
type PrepoznavacKvarova interface {
	Kvarovi(ctx context.Context, letva string, tocke []plato.Tocka) ([]plato.Kvar, error)
}

// KvarMjerila je kvar mjerila jedne letve u očitanjima izdanja.
type KvarMjerila struct {
	Letva string
	plato.Kvar
	// Drzi: letva je vrh lanca, pa prognoza kroz sate kvara drži zadnju
	// vrijednost prije njega (drziVrhoveUKvaru).
	Drzi bool
}

// Izbacen javlja je li prognoza sate kvara izbacila (potvrdila ga je
// susjedna letva, a nije dno mjerila: plato.Kvar.BezPrognoze).
func (k KvarMjerila) Izbacen() bool { return k.BezPrognoze() }

// kvaroviKruga skuplja kvarove jednog izdanja: ista letva čita se više puta,
// a u napomeni stoji jednom.
type kvaroviKruga struct {
	mu sync.Mutex
	po map[kvarLetve]KvarMjerila
}

type kvarLetve struct {
	letva string
	od    int64
}

type kljucKvarova struct{}

// sBiljeznikomKvarova daje kontekst izdanja koji pamti kvarove mjerila.
func sBiljeznikomKvarova(ctx context.Context) context.Context {
	return context.WithValue(ctx, kljucKvarova{}, &kvaroviKruga{po: map[kvarLetve]KvarMjerila{}})
}

// zabiljeziKvar pamti kvar, kad kontekst ima bilježnik. Kvar koji vrh već
// drži (Drzi) to ostaje i kad se letva pročita opet.
func zabiljeziKvar(ctx context.Context, k KvarMjerila) {
	b, ima := ctx.Value(kljucKvarova{}).(*kvaroviKruga)
	if !ima {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	kljuc := kvarLetve{k.Letva, k.Od.Unix()}
	k.Drzi = k.Drzi || b.po[kljuc].Drzi
	b.po[kljuc] = k
}

// kvaroviIzdanja su kvarovi izdanja, po letvi i početku.
func kvaroviIzdanja(ctx context.Context) []KvarMjerila {
	b, ima := ctx.Value(kljucKvarova{}).(*kvaroviKruga)
	if !ima {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := slices.Collect(maps.Values(b.po))
	slices.SortFunc(out, func(a, b KvarMjerila) int { return cmp.Or(cmp.Compare(a.Letva, b.Letva), a.Od.Compare(b.Od)) })
	return out
}

// bezZaglavljenih izbaci iz sati letve sate kvara mjerila koje potvrđuje
// susjedna letva, a svaki kvar zabilježi za napomenu. Kad se kvar ne da
// provjeriti (baza), sati ostaju kakvi jesu.
func (o *Osvjezivac) bezZaglavljenih(ctx context.Context, letva string, sati map[int64]sirovoOcitanje) {
	if o.Kvarovi == nil {
		return
	}
	redom, tocke := automatskiSati(sati)
	kvarovi, err := o.Kvarovi.Kvarovi(ctx, letva, tocke)
	if err != nil {
		log.Printf("prognoza: kvar mjerila %s se ne da provjeriti: %v", letva, err)
		return
	}
	for _, k := range kvarovi {
		zabiljeziKvar(ctx, KvarMjerila{Letva: letva, Kvar: k})
		if k.BezPrognoze() {
			izbaciSate(sati, redom, k)
		}
	}
}

// drziVrhoveUKvaru: vrh lanca kojem prognoza ne uzima sate kvara mjerila
// koji traje do kraja niza kroz te sate drži zadnju vrijednost prije kvara.
// Bez toga bi sat izdanja (ZadnjiZajednicki) pao na početak kvara i ondje
// stajao dok kvar traje: vrh prepoznat kao zaglavljen tek nakon naučenog
// trajanja vratio bi izdanje desetke sati unatrag. Vrh koji je izbor
// inačica (OdaberiInacice) već zaobišao rezervom ili nizvodnom letvom ovdje
// nije vrh. Sati koje je popunila druga obala ili rekonstrukcija ostaju.
func (o *Osvjezivac) drziVrhoveUKvaru(ctx context.Context, nizovi map[Izvor]Niz, vrhovi map[Izvor]bool) {
	for _, k := range kvaroviIzdanja(ctx) {
		if !k.Izbacen() || !k.Traje {
			continue
		}
		for iz := range vrhovi {
			if iz.Letva != k.Letva {
				continue
			}
			if v, ima := o.prijeKvara(ctx, iz, nizovi[iz], k.Kvar); ima {
				nizovi[iz] = drzi(nizovi[iz], k.Kvar, v)
				k.Drzi = true
				zabiljeziKvar(ctx, k)
			}
		}
	}
}

// prijeKvara je zadnja vrijednost niza prije kvara; kad je kvar počeo prije
// niza, razina prije kvara (u protoku iz krivulje).
func (o *Osvjezivac) prijeKvara(ctx context.Context, iz Izvor, n Niz, k plato.Kvar) (float64, bool) {
	if v, ima := n.ZadnjiDo(k.Od.Unix()/3600 - 1); ima {
		return v, true
	}
	if !k.ImaPrije {
		return 0, false
	}
	var krivulje []models.HQKrivulja
	if iz.Velicina == "protok" {
		var err error
		if krivulje, err = o.ucitajKrivulje(ctx, iz.Letva); err != nil {
			return 0, false
		}
	}
	return uVelicini(iz.Velicina, sql.NullInt64{Int64: int64(math.Round(k.Prije)), Valid: true}, sql.NullFloat64{}, krivulje, k.Od)
}

// drzi je niz s vrijednošću v u satima kvara koji nemaju vrijednost.
func drzi(n Niz, k plato.Kvar, v float64) Niz {
	vr := make(map[int64]float64, len(n.Sati))
	for i, s := range n.Sati {
		vr[s] = n.Iznosi[i]
	}
	for sat := k.Od.Unix() / 3600; sat <= k.Do.Unix()/3600; sat++ {
		if _, ima := vr[sat]; !ima {
			vr[sat] = v
		}
	}
	return NoviNiz(vr)
}

// automatskiSati su sati s automatskim vodostajem u rasponu, redom, i
// njihove točke.
func automatskiSati(sati map[int64]sirovoOcitanje) ([]int64, []plato.Tocka) {
	var redom []int64
	for sat, s := range sati {
		if s.cm.Valid && !s.rucno && models.VodostajURasponu(float64(s.cm.Int64), false) {
			redom = append(redom, sat)
		}
	}
	slices.Sort(redom)
	tocke := make([]plato.Tocka, len(redom))
	for i, sat := range redom {
		tocke[i] = plato.Tocka{Kad: time.Unix(sat*3600, 0).UTC(), Cm: float64(sati[sat].cm.Int64)}
	}
	return redom, tocke
}

// izbaciSate briše automatske sate unutar kvara.
func izbaciSate(sati map[int64]sirovoOcitanje, redom []int64, k plato.Kvar) {
	for _, sat := range redom {
		if k.U(time.Unix(sat*3600, 0)) {
			delete(sati, sat)
		}
	}
}

// upozorenjaKvarova su napomene o kvarovima mjerila koji su trajali u
// zadnjih SkokNapomenaSati sati izdanja (koliko unatrag gleda dnevni model),
// jedna po letvi.
func upozorenjaKvarova(kvarovi []KvarMjerila, sada int64) []Upozorenje {
	po := map[string][]KvarMjerila{}
	var letve []string
	for _, k := range kvarovi {
		if k.Do.Unix()/3600 <= sada-SkokNapomenaSati {
			continue
		}
		if _, bilo := po[k.Letva]; !bilo {
			letve = append(letve, k.Letva)
		}
		po[k.Letva] = append(po[k.Letva], k)
	}
	out := make([]Upozorenje, 0, len(letve))
	for _, l := range letve {
		out = append(out, Upozorenje{Vrsta: VrstaKvarMjerila, Tezina: TezinaNapomena, Letva: l, Opis: opisKvarova(po[l])})
	}
	return out
}

// opisKvarova je napomena o kvarovima jedne letve, npr. „mjerenje stoji na
// 412 cm od 25. 10. u 7 h, već 30 h, a susjedna letva gornji-brod se u tom
// vremenu pomaknula za 38 cm; prognoza ga ne uzima”. Šifra susjeda stoji
// kao zasebna riječ, da je stranica zamijeni imenom.
func opisKvarova(kvarovi []KvarMjerila) string {
	dijelovi := make([]string, len(kvarovi))
	for i, k := range kvarovi {
		dijelovi[i] = opisKvara(k)
	}
	return strings.Join(dijelovi, "; ")
}

func opisKvara(k KvarMjerila) string {
	o := plato.Opis(k.Kvar, func(s string) string { return s })
	switch {
	case k.Izbacen() && k.Drzi:
		return o + "; prognoza ga ne uzima, nego drži zadnju vrijednost prije kvara"
	case k.Izbacen():
		return o + "; prognoza ga ne uzima"
	case k.Susjed != nil:
		return o + "; prognoza ga ipak uzima, jer je voda ispod dosega mjernog uređaja"
	}
	return o + "; prognoza ga ipak uzima, jer to susjedna letva ne potvrđuje"
}
