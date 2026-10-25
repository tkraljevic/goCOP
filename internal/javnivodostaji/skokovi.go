package javnivodostaji

import (
	"cmp"
	"context"
	"slices"
	"time"

	"gocop/internal/models"
	"gocop/internal/skok"
)

// Lažni skok pri preuzimanju (Odluke (25. 10. 2026.)): vrijednost s izvora
// koja od zadnje prihvaćene vrijednosti letve odskoči više od praga letve
// (skok.Pragovi) ne upisuje se odmah, nego čeka iduće očitanje. Kad se iduće
// vrati blizu stare razine, skok se odbacuje i bilježi za stranicu letve
// (models.PoljeSkok); kad ostane na novoj razini ili ode dalje, upišu se
// oba. Ovo radi samo satno preuzimanje s javnih stranica i telemetrije;
// ručni upis, lijepljenje i uvoz evidencija pravilo ne dira.
const (
	// NajduljeCekanje je koliko sumnjiva vrijednost čeka iduće očitanje,
	// od kad je prvi put stigla (i kad je stigla kasno, npr. s rezervnog
	// izvora). Kad ga izvor ni tada ne javi, vrijednost koja je odskočila od
	// zadnje razine odbacuje se i bilježi kao skok: izvor koji nakon velikog
	// skoka zašuti vjerojatnije je u kvaru nego što je voda doista skočila.
	// Kad proradi, njegovo iduće očitanje mjeri se prema idućima
	// (skok.PoRupi), pa prava promjena prođe; a kad izvor poslije potvrdi i
	// odbačenu vrijednost, ona se upiše, a zapis o skoku ukloni (repository,
	// primljeno). Vrijednost nakon rupe kojoj iduće nije stiglo nakon
	// čekanja se upisuje (nakonCekanja).
	NajduljeCekanje = skok.NajduljeCekanje
	// okoSkoka je koliko se prije i poslije novih očitanja čitaju razine
	// koje letva već ima: zadnja prihvaćena prije njih i prvo poslije (rupa
	// koju izvor popunjava). Uz njih spremište daje i zadnju razinu prije
	// toga, kako god davno bila.
	okoSkoka = 24 * time.Hour
)

// pragZa je prag skoka letve
func (u *Uvoznik) pragZa(sifra string) float64 {
	if u.Prag == nil {
		return skok.ZadaniPrag
	}
	return u.Prag(sifra)
}

// bezSkokova vraća nova očitanja bez lažnih skokova: odbačene bilježi,
// a one koje čekaju iduće očitanje pamti za idući krug, kad se o njima
// odlučuje zajedno s novima. Očitanje bez vodostaja ili s vodostajem izvan
// raspona (njega odbaci upis) prolazi netaknuto i ne mjeri se.
func (u *Uvoznik) bezSkokova(ctx context.Context, st *models.Station, nova []models.Reading, sada time.Time, s *StanjeLetve) ([]models.Reading, error) {
	c := u.cekanje(st.ID.String())
	kandidati, ostala := razdvojiZaSkok(st, slices.Concat(nova, c.ocitanja))
	if len(kandidati) == 0 {
		u.zapamtiCekanje(st.ID.String(), c.poslije(nil, sada))
		return ostala, nil
	}
	od, do := kandidati[0].MeasuredAt, kandidati[len(kandidati)-1].MeasuredAt
	razine, err := u.Spremiste.Razine(ctx, st.ID.String(), od.Add(-okoSkoka), do.Add(okoSkoka))
	if err != nil {
		return nil, err
	}
	kandidati = bezUpisanih(kandidati, razine)
	upis, ceka, odbaceni := razvrstajSkokove(kandidati, uRasponu(razine, st.KotaNM), u.pragZa(st.Code), sada, c.prvo)
	if len(odbaceni) > 0 {
		if err := u.Spremiste.ZabiljeziOdbacene(ctx, odbaceni); err != nil {
			return nil, err
		}
	}
	u.zapamtiCekanje(st.ID.String(), c.poslije(ceka, sada))
	s.Skokova, s.Ceka = len(odbaceni), len(ceka)
	return append(ostala, upis...), nil
}

// razdvojiZaSkok dijeli očitanja na ona čiji se vodostaj mjeri (poredana po
// vremenu, svaki trenutak jednom, prvo pojavljivanje ostaje) i ostala
func razdvojiZaSkok(st *models.Station, ocitanja []models.Reading) (kandidati, ostala []models.Reading) {
	vidjeno := map[int64]bool{}
	for _, rd := range ocitanja {
		switch {
		case rd.LevelCm == nil || !models.VodostajURasponu(float64(*rd.LevelCm), st.KotaNM):
			ostala = append(ostala, rd)
		case !vidjeno[rd.MeasuredAt.Unix()]:
			vidjeno[rd.MeasuredAt.Unix()] = true
			kandidati = append(kandidati, rd)
		}
	}
	slices.SortStableFunc(kandidati, func(a, b models.Reading) int { return a.MeasuredAt.Compare(b.MeasuredAt) })
	return kandidati, ostala
}

// bezUpisanih izbacuje očitanja za trenutke u kojima letva već ima
// vodostaj: sumnjivu vrijednost koja je čekala netko je u međuvremenu upisao
// rukom, ili je stigla drugim putem
func bezUpisanih(kandidati []models.Reading, razine map[int64]int) []models.Reading {
	return slices.DeleteFunc(kandidati, func(rd models.Reading) bool {
		_, ima := razine[rd.MeasuredAt.Unix()]
		return ima
	})
}

// uRasponu su vodostaji letve bez onih izvan raspona: takav upisan prije
// provjere raspona (8. 10. 2026.) nije razina od koje bi se mjerio skok
func uRasponu(razine map[int64]int, kota bool) map[int64]int {
	out := make(map[int64]int, len(razine))
	for kad, cm := range razine {
		if models.VodostajURasponu(float64(cm), kota) {
			out[kad] = cm
		}
	}
	return out
}

// razvrstajSkokove odlučuje o kandidatima (poredanima po vremenu) uz
// vodostaje koje letva već ima: što se upisuje, što čeka iduće očitanje i
// što je odbačeno kao skok. Sumnjiva vrijednost koja čeka dulje od
// NajduljeCekanje od kad je prvi put stigla (prvo, po trenutku očitanja;
// koje nema, stiglo je sada) odbacuje se.
func razvrstajSkokove(kandidati []models.Reading, razine map[int64]int, prag float64, sada time.Time, prvo map[int64]time.Time) (upis, ceka []models.Reading, odbaceni []models.OdbacenaVrijednost) {
	tocke, kandidat := tockeSkoka(kandidati, razine)
	for i, o := range skok.Procijeni(tocke, prag) {
		if kandidat[i] < 0 {
			continue
		}
		rd := kandidati[kandidat[i]]
		switch nakonCekanja(o, sada.Sub(cmp.Or(prvo[rd.MeasuredAt.Unix()], sada))) {
		case skok.Prihvaceno:
			upis = append(upis, rd)
		case skok.Odbaceno:
			odbaceni = append(odbaceni, models.OdbacenSkok(rd))
		default:
			ceka = append(ceka, rd)
		}
	}
	return upis, ceka, odbaceni
}

// nakonCekanja je odluka o vrijednosti koja čeka već cekala. Nakon
// NajduljeCekanje vrijednost koja je odskočila od zadnje razine (Ceka)
// odbacuje se, a vrijednost nakon rupe kojoj iduće nije stiglo (CekaIduca)
// upisuje se: ništa ne govori da je skok (skok.NajduljeCekanje).
func nakonCekanja(o skok.Odluka, cekala time.Duration) skok.Odluka {
	switch {
	case o == skok.Prihvaceno || o == skok.Odbaceno || cekala <= NajduljeCekanje:
		return o
	case o == skok.CekaIduca:
		return skok.Prihvaceno
	}
	return skok.Odbaceno
}

// tockeSkoka slaže po vremenu vodostaje koje letva već ima (stalne točke) i
// kandidate; kandidat[i] je indeks kandidata točke i, −1 za upisano
func tockeSkoka(kandidati []models.Reading, razine map[int64]int) ([]skok.Tocka, []int) {
	type t struct {
		skok.Tocka
		k int
	}
	sve := make([]t, 0, len(razine)+len(kandidati))
	for kad, cm := range razine {
		sve = append(sve, t{skok.Tocka{Kad: time.Unix(kad, 0).UTC(), Cm: float64(cm), Stalna: true}, -1})
	}
	for i, rd := range kandidati {
		sve = append(sve, t{skok.Tocka{Kad: rd.MeasuredAt, Cm: float64(*rd.LevelCm)}, i})
	}
	slices.SortFunc(sve, func(a, b t) int { return a.Kad.Compare(b.Kad) })
	tocke, kandidat := make([]skok.Tocka, len(sve)), make([]int, len(sve))
	for i, x := range sve {
		tocke[i], kandidat[i] = x.Tocka, x.k
	}
	return tocke, kandidat
}

// cekanjeLetve su očitanja letve koja čekaju iduće i kad je koja sumnjiva
// vrijednost prvi put stigla, po trenutku očitanja (UTC, unix). Trenutak
// ostaje zapamćen i kad vrijednost više ne čeka (odbačena nakon
// NajduljeCekanje): kad je izvor opet vrati, ne čeka iznova.
type cekanjeLetve struct {
	ocitanja []models.Reading
	prvo     map[int64]time.Time
}

// poslije je čekanje letve nakon kruga u kojem čekaju očitanja ceka:
// njihovo prvo stizanje se pamti (kad ga nema, sada), a stizanja očitanja
// starijih od dva prozora razina (izvor ih više ne vraća) se zaboravljaju.
func (c cekanjeLetve) poslije(ceka []models.Reading, sada time.Time) cekanjeLetve {
	out := cekanjeLetve{ocitanja: ceka, prvo: map[int64]time.Time{}}
	for kad, t := range c.prvo {
		if kad >= sada.Add(-2*okoSkoka).Unix() {
			out.prvo[kad] = t
		}
	}
	for _, rd := range ceka {
		if _, ima := out.prvo[rd.MeasuredAt.Unix()]; !ima {
			out.prvo[rd.MeasuredAt.Unix()] = sada
		}
	}
	return out
}

// cekanje je čekanje letve
func (u *Uvoznik) cekanje(id string) cekanjeLetve {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.cekaju[id]
}

// zapamtiCekanje pamti čekanje letve; prazno briše
func (u *Uvoznik) zapamtiCekanje(id string, c cekanjeLetve) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(c.ocitanja) == 0 && len(c.prvo) == 0 {
		delete(u.cekaju, id)
		return
	}
	u.cekaju[id] = c
}
