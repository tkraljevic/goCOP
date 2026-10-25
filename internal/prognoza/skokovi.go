package prognoza

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/skok"
)

// Lažni skok u očitanjima za prognozu (Odluke (25. 10. 2026.)): ista
// zaštita kao pri preuzimanju (paket skok), za sve letve i sva očitanja osim
// ručno upisanih, kao druga ograda — očitanje može stići razmjenom s čvora
// sa starijim programom ili iz uvoza. Vrijednost koja od zadnje prihvaćene
// odskoči više od praga letve, a iduće očitanje se vrati, ne ulazi u
// prognozu; zadnja vrijednost koja je odskočila ne ulazi dok je iduće ne
// potvrdi. 24. 10. 2026. jedan takav sat (Donji Miholjac 830 cm umjesto
// −122) dizao je srednjak 24 sata dnevnog modela četrdesetak cm, 18 izdanja
// zaredom.
//
// bezSkoka (uzvodno.go) je drugo: uzvodnom ispravku zamjenjuje lažnu
// vrijednost satom prije, s pragom iz kojeg je ispravak naučen, i prepoznaje
// i skok od dva sata. Ovaj filtar ide prije njega i samo izbacuje.

// SkokNapomenaSati je koliko sati unatrag od sata izdanja izbačeni skok
// stoji u napomeni izdanja: dok ga dnevni model gleda (četiri dana
// srednjaka, PrognozirajDnevno).
const SkokNapomenaSati = 4 * 24

// IzbaceniSkok je sat letve koji filtar lažnog skoka nije pustio u prognozu.
type IzbaceniSkok struct {
	Letva string
	Sat   int64   // sat od epohe (UTC)
	Cm    float64 // izbačena vrijednost
	Prije float64 // zadnja prihvaćena vrijednost prije nje
	// Poslije je iduće očitanje, kojim se vrijednost vratila; Ceka kad
	// idućeg još nema, pa vrijednost čeka potvrdu.
	Poslije float64
	Ceka    bool
	// NakonRupe: vrijednost nema blisku razinu prije sebe (prva u nizu ili
	// nakon rupe, skok.PoRupi), pa je mjerena prema iduća dva očitanja,
	// Poslije i Poslije2; Prije tada ne znači ništa.
	NakonRupe bool
	Poslije2  float64
}

// skokoviKruga skuplja izbačene skokove jednog izdanja: ista letva čita
// se više puta (za vodostaj, protok, susjede), a u napomeni stoji jednom.
type skokoviKruga struct {
	mu sync.Mutex
	po map[satLetve]IzbaceniSkok
}

// satLetve je sat jedne letve.
type satLetve struct {
	letva string
	sat   int64
}

type kljucSkokova struct{}

// sBiljeznikomSkokova daje kontekst izdanja koji pamti izbačene skokove.
func sBiljeznikomSkokova(ctx context.Context) context.Context {
	return context.WithValue(ctx, kljucSkokova{}, &skokoviKruga{po: map[satLetve]IzbaceniSkok{}})
}

// zabiljeziSkok pamti izbačeni skok, kad kontekst ima bilježnik.
func zabiljeziSkok(ctx context.Context, s IzbaceniSkok) {
	k, ima := ctx.Value(kljucSkokova{}).(*skokoviKruga)
	if !ima {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.po[satLetve{s.Letva, s.Sat}] = s
}

// skokoviIzdanja su izbačeni skokovi izdanja, po letvi i satu.
func skokoviIzdanja(ctx context.Context) []IzbaceniSkok {
	k, ima := ctx.Value(kljucSkokova{}).(*skokoviKruga)
	if !ima {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	out := slices.Collect(maps.Values(k.po))
	slices.SortFunc(out, func(a, b IzbaceniSkok) int { return cmp.Or(cmp.Compare(a.Letva, b.Letva), cmp.Compare(a.Sat, b.Sat)) })
	return out
}

// bezSkokova izbaci iz sati letve lažne skokove i one koji čekaju potvrdu;
// ručno upisana očitanja ostaju i služe kao razina od koje se mjeri.
// Vrijednost nakon rupe kojoj iduće ni nakon skok.NajduljeCekanje nije
// stiglo ulazi, kao i pri preuzimanju (izvor koji javlja rijetko).
func (o *Osvjezivac) bezSkokova(ctx context.Context, letva string, sati map[int64]sirovoOcitanje) {
	redom, tocke := tockeSirovih(sati, models.JeLetvaAkumulacijeHEP(letva))
	prag := o.Pragovi.Za(letva)
	z := -1
	for i, odl := range skok.Procijeni(tocke, prag) {
		if odl == skok.Prihvaceno || (odl == skok.CekaIduca && time.Since(tocke[i].Kad) > skok.NajduljeCekanje) {
			z = i
			continue
		}
		delete(sati, redom[i])
		zabiljeziSkok(ctx, izbaceni(letva, redom, tocke, z, i, prag))
	}
}

// izbaceni opisuje izbačenu točku i uz zadnju prihvaćenu z (−1 kad je nema).
func izbaceni(letva string, redom []int64, tocke []skok.Tocka, z, i int, prag float64) IzbaceniSkok {
	s := IzbaceniSkok{Letva: letva, Sat: redom[i], Cm: tocke[i].Cm, NakonRupe: z < 0}
	if z >= 0 {
		s.Prije = tocke[z].Cm
		s.NakonRupe = skok.PoRupi(&tocke[z], tocke[i], prag)
	}
	if n := len(tocke) - i - 1; n > 0 {
		s.Poslije = tocke[i+1].Cm
		s.Ceka = s.NakonRupe && n < 2
		if !s.Ceka && s.NakonRupe {
			s.Poslije2 = tocke[i+2].Cm
		}
	} else {
		s.Ceka = true
	}
	return s
}

// tockeSirovih su sati s vodostajem, redom, i njihove točke za pravilo
// skoka. Sat bez vodostaja (samo protok) se ne mjeri, a ni vodostaj izvan
// raspona letve (kota za akumulaciju): takav upisan prije provjere raspona
// (8. 10. 2026.) nije razina od koje bi se mjerilo — inače bi ispravna
// vrijednost između dva zaglavljena −1102 cm ispala kao skok.
func tockeSirovih(sati map[int64]sirovoOcitanje, kota bool) ([]int64, []skok.Tocka) {
	var redom []int64
	for sat, s := range sati {
		if s.cm.Valid && models.VodostajURasponu(float64(s.cm.Int64), kota) {
			redom = append(redom, sat)
		}
	}
	slices.Sort(redom)
	tocke := make([]skok.Tocka, len(redom))
	for i, sat := range redom {
		s := sati[sat]
		tocke[i] = skok.Tocka{Kad: time.Unix(sat*3600, 0).UTC(), Cm: float64(s.cm.Int64), Stalna: s.rucno}
	}
	return redom, tocke
}

// upozorenjaSkokova su napomene o izbačenim skokovima u zadnja
// SkokNapomenaSati sati izdanja, jedna po letvi.
func upozorenjaSkokova(skokovi []IzbaceniSkok, sada int64) []Upozorenje {
	po := map[string][]IzbaceniSkok{}
	var letve []string
	for _, s := range skokovi {
		if s.Sat <= sada-SkokNapomenaSati {
			continue
		}
		if _, bilo := po[s.Letva]; !bilo {
			letve = append(letve, s.Letva)
		}
		po[s.Letva] = append(po[s.Letva], s)
	}
	out := make([]Upozorenje, 0, len(letve))
	for _, l := range letve {
		out = append(out, Upozorenje{Vrsta: VrstaSkok, Tezina: TezinaNapomena, Letva: l, Opis: opisSkokova(po[l])})
	}
	return out
}

// opisSkokova je napomena o izbačenim skokovima jedne letve, npr.
// „prognoza ne uzima 830 cm iz 24. 10. u 21 h: odskočilo je od −123 cm, a
// iduće očitanje vratilo se na −121 cm”.
func opisSkokova(s []IzbaceniSkok) string {
	dijelovi := make([]string, len(s))
	for i, x := range s {
		dijelovi[i] = opisSkoka(x)
	}
	return "prognoza ne uzima " + strings.Join(dijelovi, "; ")
}

func opisSkoka(s IzbaceniSkok) string {
	o := fmt.Sprintf("%s cm iz %s: ", cmHR(s.Cm), satOpisa(s.Sat))
	switch {
	case s.NakonRupe && s.Ceka:
		return o + "stiglo je nakon rupe u očitanjima, a idućih očitanja još nema, pa čeka potvrdu"
	case s.NakonRupe:
		return o + "stiglo je nakon rupe u očitanjima, a iduća dva očitanja su " + cmHR(s.Poslije) + " i " + cmHR(s.Poslije2) + " cm"
	case s.Ceka:
		return o + "odskočilo je od " + cmHR(s.Prije) + " cm, a idućeg očitanja još nema, pa čeka potvrdu"
	}
	return o + "odskočilo je od " + cmHR(s.Prije) + " cm, a iduće očitanje vratilo se na " + cmHR(s.Poslije) + " cm"
}

// cmHR piše cijele centimetre s pravim znakom minus, kako se piše u tekstu.
func cmHR(v float64) string {
	return strings.Replace(strconv.Itoa(int(math.Round(v))), "-", "−", 1)
}
