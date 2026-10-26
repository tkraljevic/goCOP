package plato

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"log"
	"maps"
	"slices"
	"sync"
	"time"

	"gocop/internal/models"
	"gocop/internal/skok"
)

// Stanje kvara računa se na čvoru iz očitanja koja ima (Odluke
// (25. 10. 2026.)): ne sprema se i ne putuje razmjenom, jer ga svaki čvor iz
// istih očitanja izračuna jednako.
const (
	// ProzorStanja: stranica letve gleda automatska očitanja zadnja dva
	// tjedna. Kvar koji je počeo prije toga traži se unatrag (Kvarovi).
	ProzorStanja = 14 * 24 * time.Hour
	// NajdaljeUnatrag: početak kvara traži se najviše toliko prije zadnjeg
	// očitanja, po ProzorStanja odjednom. Siga je u rujnu i listopadu 2026.
	// stajala 30 dana; kvar koji je počeo još ranije opisan je „barem od”.
	NajdaljeUnatrag = 60 * 24 * time.Hour
	// StanjeVrijedi: izračunato stanje letve vrijedi pet minuta, da popis
	// letvi pri svakom otvaranju ne čita očitanja svih susjeda.
	StanjeVrijedi = 5 * time.Minute
	// RegistarVrijedi: letve iz registra čitaju se iznova svakih deset
	// minuta (nova letva, promijenjena stacionaža).
	RegistarVrijedi = 10 * time.Minute
)

// Procjenitelj prepoznaje kvar mjerila iz očitanja u bazi čvora, uz
// svojstva naučena iz arhive.
type Procjenitelj struct {
	ocitanja *sql.DB // baza čvora: readings, stations, structures
	arhiva   *sql.DB // arhiva vodostaja; nil kad je nema
	pragovi  *skok.Pragovi
	// dodatni su susjedi koje registar ne zna (druga obala, rekonstrukcija
	// prognoze), po šifri letve.
	dodatni map[string][]string
	sada    func() time.Time

	// uciMu: iz arhive uči samo jedan u isto vrijeme (priprema pri
	// pokretanju, stranica, prognoza), a tko čeka, nauči se iz pamćenja.
	uciMu sync.Mutex

	mu       sync.Mutex
	svojstva map[string]Svojstva
	vlastita map[string]Svojstva
	stanice  []Stanica
	citano   time.Time
	stanja   map[string]zapamceno
	uci      map[string]bool // letve čija se svojstva uče u pozadini
}

type zapamceno struct {
	kvar *Kvar
	kad  time.Time
}

// Novi daje procjenitelja nad bazom čvora i arhivom (smije biti nil).
func Novi(ocitanja, arhiva *sql.DB, pragovi *skok.Pragovi, dodatni map[string][]string) *Procjenitelj {
	return &Procjenitelj{ocitanja: ocitanja, arhiva: arhiva, pragovi: pragovi, dodatni: dodatni, sada: time.Now,
		svojstva: map[string]Svojstva{}, vlastita: map[string]Svojstva{}, stanja: map[string]zapamceno{},
		uci: map[string]bool{}}
}

// registar su letve iz registra, pročitane najviše RegistarVrijedi prije.
func (p *Procjenitelj) registar(ctx context.Context) ([]Stanica, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stanice != nil && p.sada().Sub(p.citano) < RegistarVrijedi {
		return p.stanice, nil
	}
	s, err := citajRegistar(ctx, p.ocitanja)
	if err != nil {
		return nil, err
	}
	p.stanice, p.citano = s, p.sada()
	return s, nil
}

// citajRegistar čita letve: šifru, ime, vodu, stacionažu, oznaku kote i je
// li vodomjer uz ustavu ili crpnu stanicu.
func citajRegistar(ctx context.Context, baza *sql.DB) ([]Stanica, error) {
	rows, err := baza.QueryContext(ctx, `SELECT s.id, s.code, s.name, s.watercourse_code, s.stationing, s.kota_nm,
		EXISTS(SELECT 1 FROM structures o WHERE o.station_id = s.id AND o.kind IN ('USTAVA', 'CRPNA_STANICA'))
		FROM stations s`)
	if err != nil {
		return nil, fmt.Errorf("registar letvi: %w", err)
	}
	defer rows.Close()
	out := []Stanica{}
	for rows.Next() {
		var s Stanica
		if err := rows.Scan(&s.ID, &s.Sifra, &s.Ime, &s.Voda, &s.Stacionaza, &s.KotaNM, &s.UzObjekt); err != nil {
			return nil, fmt.Errorf("registar letvi: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// stanica je letva iz registra; letva koje u registru nema ima samo šifru.
func (p *Procjenitelj) stanica(ctx context.Context, sifra string) (Stanica, error) {
	stanice, err := p.registar(ctx)
	if err != nil {
		return Stanica{}, err
	}
	if i := slices.IndexFunc(stanice, func(s Stanica) bool { return s.Sifra == sifra }); i >= 0 {
		return stanice[i], nil
	}
	return Stanica{Sifra: sifra}, nil
}

// Ime je naziv letve iz registra, a šifra kad je nema.
func (p *Procjenitelj) Ime(ctx context.Context, sifra string) string {
	if s, err := p.stanica(ctx, sifra); err == nil && s.Ime != "" {
		return s.Ime
	}
	return sifra
}

// Sifra je šifra letve s ID-om postaje; prazno kad je nema.
func (p *Procjenitelj) Sifra(ctx context.Context, id string) string {
	stanice, err := p.registar(ctx)
	if err != nil {
		return ""
	}
	if i := slices.IndexFunc(stanice, func(s Stanica) bool { return s.ID == id }); i >= 0 {
		return stanice[i].Sifra
	}
	return ""
}

// susjediLetve su šifre susjeda: iz registra, pa dodatni, bez izuzetih i
// bez ponavljanja.
func (p *Procjenitelj) susjediLetve(sifra string, stanice []Stanica) []string {
	out := SusjediIzRegistra(sifra, stanice)
	for _, d := range p.dodatni[sifra] {
		i := slices.IndexFunc(stanice, func(s Stanica) bool { return s.Sifra == d })
		if !slices.Contains(out, d) && (i < 0 || !stanice[i].Izuzeta()) {
			out = append(out, d)
		}
	}
	return out
}

// Svojstva letve: prag skoka, a iz arhive trajanje, raspon i pragovi
// susjeda. Naučeno se pamti do gašenja programa (kao prag skoka); kad se
// arhiva ne da pročitati, vrijede zadane vrijednosti i ne pamte se, pa se
// idući put pokuša opet.
func (p *Procjenitelj) Svojstva(ctx context.Context, sifra string) (Svojstva, error) {
	if s, ima := p.zapamcena(p.svojstva, sifra); ima {
		return s, nil
	}
	p.uciMu.Lock()
	defer p.uciMu.Unlock()
	if s, ima := p.zapamcena(p.svojstva, sifra); ima {
		return s, nil
	}
	stanice, err := p.registar(ctx)
	if err != nil {
		return Svojstva{}, err
	}
	s, pamti := p.nauci(ctx, sifra, p.susjediLetve(sifra, stanice))
	if pamti {
		p.zapamti(p.svojstva, sifra, s)
	}
	return s, nil
}

// Vlastita su svojstva letve bez susjeda: trajanje, raspon i prag skoka.
// Po njima se iz očitanja susjeda izbaci njegov vlastiti kvar, da susjed u
// kvaru ne potvrdi kvar letve (radar Orešca koji skače između 48 i −372 cm
// „pomaknuo” bi se za 420 cm uz potok koji miruje).
func (p *Procjenitelj) Vlastita(ctx context.Context, sifra string) Svojstva {
	if s, ima := p.zapamcena(p.vlastita, sifra); ima {
		return s
	}
	p.uciMu.Lock()
	defer p.uciMu.Unlock()
	if s, ima := p.zapamcena(p.vlastita, sifra); ima {
		return s
	}
	niz, err := p.izArhive(ctx, sifra)
	s := vlastitaIzNiza(niz, p.pragovi.Za(sifra))
	if err == nil {
		p.zapamti(p.vlastita, sifra, s)
	}
	return s
}

// vlastitaIzNiza su svojstva letve bez susjeda iz njezina niza u arhivi.
func vlastitaIzNiza(niz []Tocka, prag float64) Svojstva {
	return Svojstva{Trajanje: TrajanjeIzNiza(niz), Raspon: RasponIzNiza(niz), Prag: prag}
}

func (p *Procjenitelj) zapamcena(m map[string]Svojstva, sifra string) (Svojstva, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ima := m[sifra]
	return s, ima
}

func (p *Procjenitelj) zapamti(m map[string]Svojstva, sifra string, s Svojstva) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m[sifra] = s
}

// nauci računa svojstva iz arhive; pamti je false kad se arhiva nije dala
// pročitati.
func (p *Procjenitelj) nauci(ctx context.Context, sifra string, susjedi []string) (Svojstva, bool) {
	niz, err := p.izArhive(ctx, sifra)
	s := vlastitaIzNiza(niz, p.pragovi.Za(sifra))
	if err != nil {
		s.Susjedi = zadaniSusjedi(susjedi)
		return s, false
	}
	for _, n := range susjedi {
		nn, err := p.izArhive(ctx, n)
		if err != nil {
			s.Susjedi = zadaniSusjedi(susjedi)
			return s, false
		}
		vn := vlastitaIzNiza(nn, p.pragovi.Za(n))
		p.zapamti(p.vlastita, n, vn)
		s.Susjedi = append(s.Susjedi, Susjed{Letva: n, Prag: PragSusjeda(niz, nn), Raspon: vn.Raspon})
	}
	return s, true
}

// izArhive je satni niz letve iz arhive; bez arhive prazan.
func (p *Procjenitelj) izArhive(ctx context.Context, sifra string) ([]Tocka, error) {
	if p.arhiva == nil {
		return nil, nil
	}
	return NizIzArhive(ctx, p.arhiva, sifra)
}

// zadaniSusjedi su susjedi bez naučenog praga (BezPraga) i bez raspona.
func zadaniSusjedi(sifre []string) []Susjed {
	out := make([]Susjed, len(sifre))
	for i, s := range sifre {
		out[i] = Susjed{Letva: s, Prag: BezPraga}
	}
	return out
}

// Ocitanja su automatska očitanja vodostaja letve od od do do (ručno
// upisana ne: čovjek ne zaglavi), svedena na pune sate i bez lažnih
// skokova.
func (p *Procjenitelj) Ocitanja(ctx context.Context, sifra string, od, do time.Time) ([]Tocka, error) {
	rows, err := p.ocitanja.QueryContext(ctx, `SELECT o.measured_at, o.level_cm
		FROM readings o JOIN stations s ON s.id = o.station_id
		WHERE s.code = ? AND o.measured_at >= ? AND o.measured_at <= ? AND o.level_cm IS NOT NULL AND o.source <> ?
		ORDER BY o.measured_at`,
		sifra, od.UTC(), do.UTC(), models.ReadingSourceManual)
	if err != nil {
		return nil, fmt.Errorf("očitanja %s: %w", sifra, err)
	}
	defer rows.Close()
	var sirovo []Tocka
	for rows.Next() {
		var t Tocka
		var cm int64
		if err := rows.Scan(&t.Kad, &cm); err != nil {
			return nil, fmt.Errorf("očitanja %s: %w", sifra, err)
		}
		if t.Cm = float64(cm); models.VodostajURasponu(t.Cm, false) {
			sirovo = append(sirovo, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("očitanja %s: %w", sifra, err)
	}
	return BezSkokova(PoSatu(sirovo), p.pragovi.Za(sifra)), nil
}

// Kvarovi su kvarovi mjerila u automatskim očitanjima letve (satnima, po
// vremenu, bez lažnih skokova), uz očitanja susjeda iz baze čvora. Kad
// točke počinju usred mogućeg kvara, njegov početak traži se u starijim
// očitanjima (sPocetkom), pa kvar smije početi prije prve točke. Izuzeta
// letva (ustava, elektrana, akumulacija) nema kvarova.
func (p *Procjenitelj) Kvarovi(ctx context.Context, sifra string, tocke []Tocka) ([]Kvar, error) {
	st, err := p.stanica(ctx, sifra)
	if err != nil || len(tocke) == 0 || st.Izuzeta() {
		return nil, err
	}
	s, err := p.Svojstva(ctx, sifra)
	if err != nil {
		return nil, err
	}
	tocke, nedovrseno, err := p.sPocetkom(ctx, sifra, tocke, s)
	if err != nil {
		return nil, err
	}
	nizovi, err := p.nizoviSusjeda(ctx, s.Susjedi, tocke[0].Kad.Add(-PrijeKvara), tocke[len(tocke)-1].Kad)
	if err != nil {
		return nil, err
	}
	kvarovi := Prepoznaj(tocke, s, nizovi)
	for i := range kvarovi {
		kvarovi[i].BaremOd = nedovrseno && kvarovi[i].Od.Equal(tocke[0].Kad)
	}
	return kvarovi, nil
}

// nizoviSusjeda su očitanja susjeda od od do do, s njihovim vlastitim
// kvarovima označenima kao nepoznata (BezKvarova).
func (p *Procjenitelj) nizoviSusjeda(ctx context.Context, susjedi []Susjed, od, do time.Time) (map[string][]Tocka, error) {
	nizovi := map[string][]Tocka{}
	for _, n := range susjedi {
		niz, err := p.Ocitanja(ctx, n.Letva, od, do)
		if err != nil {
			return nil, err
		}
		nizovi[n.Letva] = BezKvarova(niz, p.Vlastita(ctx, n.Letva))
	}
	return nizovi, nil
}

// sPocetkom su točke s dodanim starijim očitanjima letve, po ProzorStanja
// unatrag, dok prva točka leži u mogućem kvaru (nejasanPocetak), a najviše
// NajdaljeUnatrag prije zadnje. Bez toga kvar dulji od prozora (Siga 30
// dana) počinje prvom točkom, trajanje mu stane na dva tjedna, a kad se
// susjed u tom prozoru pomakne manje od praga, okvir nestane dok uređaj i
// dalje stoji. nedovrseno je true kad se stalo na NajdaljeUnatrag, a prva
// točka je još u mogućem kvaru.
func (p *Procjenitelj) sPocetkom(ctx context.Context, sifra string, tocke []Tocka, s Svojstva) ([]Tocka, bool, error) {
	granica := tocke[len(tocke)-1].Kad.Add(-NajdaljeUnatrag)
	for nejasanPocetak(tocke, s) {
		prva := tocke[0].Kad
		if !prva.After(granica) {
			return tocke, true, nil
		}
		od := prva.Add(-ProzorStanja)
		if od.Before(granica) {
			od = granica
		}
		ranije, err := p.Ocitanja(ctx, sifra, od, prva.Add(-time.Second))
		if err != nil {
			return nil, false, err
		}
		// Očitanje do pola sata prije prve točke svede se na njezin sat.
		ranije = slices.DeleteFunc(ranije, func(x Tocka) bool { return !x.Kad.Before(prva) })
		if len(ranije) == 0 {
			return tocke, false, nil
		}
		tocke = slices.Concat(ranije, tocke)
		if od.Equal(granica) {
			return tocke, nejasanPocetak(tocke, s), nil
		}
	}
	return tocke, false, nil
}

// nejasanPocetak javlja leži li prva točka u mogućem kvaru kojem se početak
// ne vidi: u nizu iste vrijednosti od barem NajkraceTrajanje ili daleko
// izvan raspona.
func nejasanPocetak(t []Tocka, s Svojstva) bool {
	return t[krajNiza(t, 0)].Kad.Sub(t[0].Kad) >= NajkraceTrajanje || s.Raspon.Daleko(t[0].Cm)
}

// Stanje je kvar mjerila koji traje na letvi (zadnje automatsko očitanje u
// zadnja dva tjedna je u kvaru), ili nil. Izračunato se pamti StanjeVrijedi.
// Dok svojstva letve nisu naučena (učenje iz arhive traje oko sekunde po
// letvi), stranica ne čeka: stanje je nil, a učenje ide u pozadini.
func (p *Procjenitelj) Stanje(ctx context.Context, sifra string) (*Kvar, error) {
	if _, ima := p.zapamcena(p.svojstva, sifra); !ima {
		p.uciUPozadini(sifra)
		return nil, nil
	}
	sada := p.sada()
	p.mu.Lock()
	z, ima := p.stanja[sifra]
	p.mu.Unlock()
	if ima && sada.Sub(z.kad) < StanjeVrijedi {
		return z.kvar, nil
	}
	tocke, err := p.Ocitanja(ctx, sifra, sada.Add(-ProzorStanja), sada)
	if err != nil {
		return nil, err
	}
	kvarovi, err := p.Kvarovi(ctx, sifra, tocke)
	if err != nil {
		return nil, err
	}
	k := kvarKojiTraje(kvarovi)
	p.mu.Lock()
	p.stanja[sifra] = zapamceno{kvar: k, kad: sada}
	p.mu.Unlock()
	return k, nil
}

// uciUPozadini uči svojstva letve u pozadini, jednom u isto vrijeme.
func (p *Procjenitelj) uciUPozadini(sifra string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.uci[sifra] {
		return
	}
	p.uci[sifra] = true
	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.uci, sifra)
			p.mu.Unlock()
		}()
		if _, err := p.Svojstva(context.Background(), sifra); err != nil {
			log.Printf("kvar mjerila %s: svojstva se ne daju naučiti: %v", sifra, err)
		}
	}()
}

// Pripremi uči svojstva svih letvi s automatskim očitanjima, jednu po
// jednu, da stranica i prognoza ne čekaju arhivu. Zove se u pozadini pri
// pokretanju.
func (p *Procjenitelj) Pripremi(ctx context.Context) error {
	sifre, err := automatskeLetve(ctx, p.ocitanja)
	if err != nil {
		return fmt.Errorf("letve s automatskim očitanjima: %w", err)
	}
	for _, s := range sifre {
		if _, err := p.Svojstva(ctx, s); err != nil || ctx.Err() != nil {
			return cmp.Or(err, ctx.Err())
		}
	}
	return nil
}

// automatskeLetve su šifre letvi koje imaju automatska očitanja vodostaja.
func automatskeLetve(ctx context.Context, baza *sql.DB) ([]string, error) {
	rows, err := baza.QueryContext(ctx, `SELECT DISTINCT s.code FROM readings o JOIN stations s ON s.id = o.station_id
		WHERE o.source <> ? AND o.level_cm IS NOT NULL`, models.ReadingSourceManual)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// kvarKojiTraje je zadnji kvar, kad traje.
func kvarKojiTraje(kvarovi []Kvar) *Kvar {
	for i := len(kvarovi) - 1; i >= 0; i-- {
		if kvarovi[i].Traje {
			return &kvarovi[i]
		}
	}
	return nil
}

// PoSatu svodi očitanja (po vremenu) na pune sate kao prognoza: svaki puni
// sat uzima očitanje najbliže sebi, najviše pola sata prije ili poslije, a
// od dva jednako udaljena ranije.
func PoSatu(t []Tocka) []Tocka {
	po := map[int64]Tocka{}
	for _, x := range t {
		sat := x.Kad.Add(30 * time.Minute).Truncate(time.Hour)
		if prije, ima := po[sat.Unix()]; ima && udaljenost(prije.Kad, sat) <= udaljenost(x.Kad, sat) {
			continue
		}
		po[sat.Unix()] = x
	}
	sati := slices.Sorted(maps.Keys(po))
	out := make([]Tocka, len(sati))
	for i, s := range sati {
		out[i] = Tocka{Kad: time.Unix(s, 0).UTC(), Cm: po[s].Cm}
	}
	return out
}

func udaljenost(a, b time.Time) time.Duration { return max(a.Sub(b), b.Sub(a)) }

// BezSkokova su točke bez lažnih skokova i onih koje još čekaju potvrdu
// (paket skok).
func BezSkokova(t []Tocka, prag float64) []Tocka {
	st := make([]skok.Tocka, len(t))
	for i, x := range t {
		st[i] = skok.Tocka{Kad: x.Kad, Cm: x.Cm}
	}
	out := make([]Tocka, 0, len(t))
	for i, o := range skok.Procijeni(st, prag) {
		if o == skok.Prihvaceno {
			out = append(out, t[i])
		}
	}
	return out
}

// Izuzeta javlja je li letva takva da njezin kvar ne prepoznajemo
// (Stanica.Izuzeta); kad se registar ne da pročitati, nije.
func (p *Procjenitelj) Izuzeta(ctx context.Context, sifra string) bool {
	st, err := p.stanica(ctx, sifra)
	return err == nil && st.Izuzeta()
}
