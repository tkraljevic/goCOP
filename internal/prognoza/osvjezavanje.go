package prognoza

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"

	"gocop/internal/models"
)

// Osvježavanje: jedan prolaz koji iz zatečenih očitanja izračuna prognozu i
// zapiše je.
//
// Ovo stoji u paketu, a ne u naredbi, jer ga zovu dvoje: naredba kad se pokreće
// rukom, i poslužitelj čim preuzme nove vodostaje. Prognoza koja se osvježava
// jednom na dan zastarjela je devetnaest sati od dvadeset četiri; ova se
// obnavlja kako voda stiže.

// VrhoviSTudomPrognozom su vrhovi lanca kojima se za sate poslije izdavanja
// uzima hod mađarske prognoze umjesto zadnjeg mjerenja. Mjereno na 29 njihovih
// izdanja 2024.–2026. modelom naučenim prije njih: s njihovim Letenyeom naš
// lanac Botovu skida pogrešku 1.–4. dan s 30, 36, 48 i 60 cm na 22, 29, 41 i
// 49 — manje i od njihove vlastite prognoze Botova (24, 37, 42, 51) — a
// Donjem Miholjcu i Drávaszabolcsu 3.–4. dan za 4–7 cm. Njihov Komárom
// Aljmašu 5. dan skida 6 cm, a mađarskim letvama puno više. Starija od dva
// dana ne uzima se.
var VrhoviSTudomPrognozom = map[string]string{
	"letenye": Podrijetlo,
	"komarom": Podrijetlo,
}

// Unatrag je koliko se očitanja čita unatrag. Dva tjedna su dosta i najduljem
// lancu, a manje bi ostavilo rupe kod letvi koje se očitavaju rjeđe.
const Unatrag = 14 * 24 * time.Hour

// Osvjezivac drži veze i postavke jednog osvježavanja.
type Osvjezivac struct {
	Baza     *sql.DB // baza prognoza, otvorena za pisanje
	Ocitanja *sql.DB // operativna baza, samo za čitanje
	Arhiva   *sql.DB // arhiva, zbog krivulja protoka
	Najdalje int     // dokle se računa, u satima
	Model    string  // oznaka pod kojom se prognoza zapisuje
	// Iznova računa i kad je za taj sat prognoza već izdana. Treba nakon
	// namještanja: bez toga se učinak novih pojasa ne vidi do idućeg sata.
	Iznova bool
}

// Ishod je što je jedno osvježavanje dalo.
type Ishod struct {
	Sada        int64           // sat za koji je prognoza izdana
	Vrhovi      map[Izvor]int64 // zadnji izmjereni sat svakog vrha lanca
	Izdane      []Izdana        // sve izračunate vrijednosti
	Promasaji   map[string]map[int]Promasaj
	Preskoceno  bool             // isti sat već je izdan, ništa se nije mijenjalo
	BezPrognoze map[string]error // letve koje nisu dale nijedan sat
	Dnevne      []DnevnaIzdana   // dnevna prognoza za 1.–6. dan
	TudiVrhovi  []string         // vrhovi kojima je budućnost dala tuđa prognoza
	BezDnevne   map[string]error // letve s dnevnim modelom koje ga nisu dale
}

// Letvi je koliko ih je prognoza dotaknula.
func (i *Ishod) Letvi() int {
	vidjeno := map[string]bool{}
	for _, x := range i.Izdane {
		vidjeno[x.Letva] = true
	}
	return len(vidjeno)
}

// Zaostatak je koliko je izdanje starije od sadašnjeg sata. Izvori objavljuju
// sa zakašnjenjem, pa se izdaje za zadnji sat koji imaju sve ulazne letve —
// a koliko je to star podatak, dežurni mora vidjeti.
func (i *Ishod) Zaostatak(sada time.Time) time.Duration {
	return sada.UTC().Truncate(time.Hour).Sub(time.Unix(i.Sada*3600, 0).UTC())
}

// Osvjezi računa prognozu iz zatečenih očitanja i zapisuje je. Kad je za taj
// sat prognoza već izdana, ne računa ništa: ista očitanja daju istu prognozu,
// pa bi ponovni upis bio samo trošak.
func (o *Osvjezivac) Osvjezi(ctx context.Context) (*Ishod, error) {
	pojasi, err := SviPojasi(o.Baza)
	if err != nil {
		return nil, err
	}
	if len(pojasi) == 0 {
		return nil, fmt.Errorf("baza prognoza je prazna; prvo pokreni namjesti-prognozu")
	}
	promasaji, err := Promasaji(o.Baza)
	if err != nil {
		return nil, err
	}

	trebani, vrhovi := TrebaniIzvori(pojasi)
	od := time.Now().UTC().Add(-Unatrag)
	nizovi := map[Izvor]Niz{}
	for iz := range trebani {
		n, err := o.ucitajNiz(ctx, iz, od)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", iz.Letva, iz.Velicina, err)
		}
		nizovi[iz] = n
	}
	// Vrh lanca se vodi u jednoj veličini, ali letva ima obje. Donja Dubrava
	// nema krivulju, pa joj se vodostaj ne da izračunati — a mjeren jest, i na
	// uzdužnom profilu treba stajati.
	druge := map[Izvor]Niz{}
	for iz := range vrhovi {
		suprotna := Izvor{Letva: iz.Letva, Velicina: "vodostaj"}
		if iz.Velicina == "vodostaj" {
			suprotna.Velicina = "protok"
		}
		if n, err := o.ucitajNiz(ctx, suprotna, od); err == nil {
			if _, ima := n.Zadnji(); ima {
				druge[suprotna] = n
			}
		}
	}

	sada, ok := ZadnjiZajednicki(nizovi, vrhovi)
	if !ok {
		return nil, fmt.Errorf("nijedna ulazna letva nema svježa očitanja")
	}
	ishod := &Ishod{Sada: sada, Promasaji: promasaji,
		Vrhovi: map[Izvor]int64{}, BezPrognoze: map[string]error{}}
	for iz := range vrhovi {
		if z, ima := nizovi[iz].Zadnji(); ima {
			ishod.Vrhovi[iz] = z
		}
	}
	if zadnje, ima, err := ZadnjeIzdanje(o.Baza); err == nil && ima && zadnje == sada && !o.Iznova {
		ishod.Preskoceno = true
		return ishod, nil
	}

	r := NovoRacunalo(pojasi, nizovi, sada)
	for letva, izvor := range VrhoviSTudomPrognozom {
		iz := Izvor{Letva: letva, Velicina: "vodostaj"}
		if !vrhovi[iz] {
			continue
		}
		if n, ima, err := TudaPrognoza(o.Baza, izvor, letva, sada-48, sada+6); err == nil && ima {
			r.PostaviBuducnostVrha(iz, n)
			ishod.TudiVrhovi = append(ishod.TudiVrhovi, letva)
		}
	}
	najdalje := o.Najdalje
	if najdalje <= 0 {
		najdalje = 96
	}
	for _, letva := range Redom(pojasi) {
		izdane, err := r.Prognoziraj(letva, najdalje, o.Model)
		if err != nil {
			ishod.BezPrognoze[letva] = err
			continue
		}
		if len(izdane) == 0 {
			ishod.BezPrognoze[letva] = fmt.Errorf("nema ulaza za nijedan sat unaprijed")
			continue
		}
		// Promašaji su izmjereni puštanjem prognoze unatrag po arhivi. Ondje
		// gdje ih ima, oni kažu i koliko treba oduzeti i koliko se smije
		// obećati — bolje od rasapa namještanja, koji ne zna za ispravak.
		for i := range izdane {
			p, ima := promasaji[letva][int(izdane[i].Ciljni-sada)]
			if !ima {
				continue
			}
			izdane[i].Vrijednost -= p.Pomak
			izdane[i].Dolje = izdane[i].Vrijednost - p.Rasap
			izdane[i].Gore = izdane[i].Vrijednost + p.Rasap
		}
		ishod.Izdane = append(ishod.Izdane, izdane...)
		// Ista prognoza i u drugoj veličini, ondje gdje krivulja postoji.
		// Model radi u jednoj, a dežurni čita onu koju je navikao gledati.
		ishod.Izdane = append(ishod.Izdane, o.uDrugojVelicini(ctx, izdane)...)
	}

	// Vrhovi lanca nemaju svoju prognozu, ali imaju mjerenje. Bez njih bi
	// uzdužni profil počinjao od druge letve — Dunav bez Batine, Drava bez
	// Donje Dubrave — a upravo su to mjesta na kojima val ulazi u naš sliv.
	ishod.Izdane = append(ishod.Izdane, o.sidraVrhova(ctx, nizovi, vrhovi, sada)...)
	ishod.Izdane = append(ishod.Izdane, sidraDrugih(druge, sada)...)
	var sidra []Izdana
	ishod.Dnevne, sidra, ishod.BezDnevne = o.dnevno(ctx, sada, od)
	// Ulazi dnevnog modela koji nisu u lancu nemaju svoju prognozu, ali na
	// pregledu moraju stajati — iz njih se računa. Zapisuje se njihovo
	// mjerenje u satu izdavanja, kao i za vrhove lanca.
	ima := map[string]bool{}
	for _, i := range ishod.Izdane {
		ima[i.Letva] = true
	}
	for _, i := range sidra {
		if !ima[i.Letva] {
			ishod.Izdane = append(ishod.Izdane, i)
		}
	}
	return ishod, nil
}

// dnevniModeli drži naučene dnevne modele. Uče se iz arhive jednom dnevno:
// arhiva se mijenja rijetko, a učenje traje sekundu-dvije.
var dnevniModeli struct {
	sync.Mutex
	dan    string
	modeli map[string]*DnevniModel
	greske map[string]error
}

func (o *Osvjezivac) dnevniModeliZaDanas() (map[string]*DnevniModel, map[string]error) {
	danas := time.Now().Format("2006-01-02")
	dnevniModeli.Lock()
	defer dnevniModeli.Unlock()
	if dnevniModeli.dan == danas {
		return dnevniModeli.modeli, dnevniModeli.greske
	}
	modeli, greske := map[string]*DnevniModel{}, map[string]error{}
	nizovi := map[string]DnevniNiz{}
	for _, c := range DnevniCiljevi {
		for _, l := range c.letve() {
			if _, ima := nizovi[l]; ima {
				continue
			}
			n, err := DnevniIzArhive(o.Arhiva, l)
			if err != nil {
				greske[c.Letva] = err
				continue
			}
			nizovi[l] = n
		}
		m, err := NamjestiDnevni(nizovi, c, 0)
		if err != nil {
			greske[c.Letva] = err
			continue
		}
		modeli[c.Letva] = m
	}
	dnevniModeli.dan, dnevniModeli.modeli, dnevniModeli.greske = danas, modeli, greske
	return modeli, greske
}

// dnevno izdaje dnevnu prognozu iz satnih očitanja do sata izdavanja.
func (o *Osvjezivac) dnevno(ctx context.Context, sada int64, od time.Time) ([]DnevnaIzdana, []Izdana, map[string]error) {
	bez := map[string]error{}
	if o.Arhiva == nil {
		return nil, nil, bez
	}
	modeli, greske := o.dnevniModeliZaDanas()
	for l, err := range greske {
		bez[l] = err
	}
	satni := map[string]Niz{}
	var out []DnevnaIzdana
	for _, c := range DnevniCiljevi {
		m := modeli[c.Letva]
		if m == nil {
			continue
		}
		for _, l := range c.letve() {
			if _, ima := satni[l]; ima {
				continue
			}
			n, err := o.ucitajNiz(ctx, Izvor{Letva: l, Velicina: "vodostaj"}, od)
			if err != nil {
				bez[c.Letva] = err
				continue
			}
			satni[l] = n
		}
		d, err := PrognozirajDnevno(m, satni, sada)
		if err != nil {
			bez[c.Letva] = err
			continue
		}
		out = append(out, d...)
	}
	var sidra []Izdana
	for _, l := range DnevniUlazi() {
		n, ucitan := satni[l]
		if !ucitan {
			continue
		}
		if v, ima := n.ZadnjiDo(sada); ima {
			sidra = append(sidra, Izdana{Letva: l, Velicina: "vodostaj", Izdano: sada, Ciljni: sada,
				Vrijednost: v, Dolje: v, Gore: v, Model: ModelDnevni})
		}
	}
	return out, sidra, bez
}

// Zapisi sprema izračunato.
func (o *Osvjezivac) Zapisi(ishod *Ishod) error {
	if ishod == nil || ishod.Preskoceno || len(ishod.Izdane) == 0 {
		return nil
	}
	if err := SpremiIzdane(o.Baza, ishod.Izdane); err != nil {
		return err
	}
	return SpremiDnevne(o.Baza, ishod.Dnevne)
}

// TrebaniIzvori nabraja sve letve koje račun dira, i posebno one koje nemaju
// svoj račun — vrhove lanca, o kojima ovisi dokle prognoza seže.
func TrebaniIzvori(pojasi map[string][]Pojas) (svi, vrhovi map[Izvor]bool) {
	svi = map[Izvor]bool{}
	vrhovi = map[Izvor]bool{}
	for letva, ps := range pojasi {
		for _, p := range ps {
			svi[Izvor{Letva: letva, Velicina: p.Velicina}] = true
			for _, u := range p.Ulazi {
				iz := Izvor{Letva: u.Letva, Velicina: u.Velicina}
				svi[iz] = true
				if len(pojasi[u.Letva]) == 0 {
					vrhovi[iz] = true
				}
			}
		}
	}
	return svi, vrhovi
}

// ZaostatakVrha je koliko sati vrh lanca smije kasniti za ostalima, a da se
// prognoza ipak izda za sat najsvježijih. U satima koji mu nedostaju drži se
// njegova zadnja vrijednost, kao i za sate poslije izdavanja. Izdati prognozu
// ranije, za sat kad je i on javio, ne bi dalo ništa više od te iste
// vrijednosti — a izgubilo bi zadnja mjerenja svih ostalih letvi.
const ZaostatakVrha = 3

// ZadnjiZajednicki je sat za koji se prognoza izdaje: zadnji koji imaju sve
// ulazne letve, s tim da vrh smije kasniti do ZaostatakVrha. Uzeti kasniji
// značilo bi računati iz onoga čega još nema.
func ZadnjiZajednicki(nizovi map[Izvor]Niz, vrhovi map[Izvor]bool) (int64, bool) {
	var najkasniji, granica int64
	prvi := true
	for iz := range vrhovi {
		z, ima := nizovi[iz].Zadnji()
		if !ima {
			return 0, false
		}
		if prvi || z > najkasniji {
			najkasniji = z
		}
		if prvi || z+ZaostatakVrha < granica {
			granica = z + ZaostatakVrha
		}
		prvi = false
	}
	return min(najkasniji, granica), !prvi
}

// ucitajNiz čita satni niz iz očitanja. Protok se uzima kako je izmjeren, a
// gdje ga nema računa se iz krivulje — Terezino Polje šalje samo centimetre, a
// model mu traži kubike.
func (o *Osvjezivac) ucitajNiz(ctx context.Context, iz Izvor, od time.Time) (Niz, error) {
	var krivulje []models.HQKrivulja
	if iz.Velicina == "protok" {
		var err error
		if krivulje, err = o.ucitajKrivulje(ctx, iz.Letva); err != nil {
			return Niz{}, err
		}
	}
	r, err := o.Ocitanja.QueryContext(ctx, `SELECT o.measured_at, o.level_cm, o.flow_m3s
		FROM readings o JOIN stations s ON s.id = o.station_id
		WHERE s.code = ? AND o.measured_at >= ?`, iz.Letva, od)
	if err != nil {
		return Niz{}, err
	}
	defer r.Close()

	vrijednosti := map[int64]float64{}
	odmak := map[int64]time.Duration{}
	for r.Next() {
		// Stupac je DATETIME, pa ga upravljač sam pretvara u vrijeme; čitan
		// kao tekst dolazi u drugom zapisu nego što u bazi stoji.
		var t time.Time
		var cm sql.NullInt64
		var q sql.NullFloat64
		if err := r.Scan(&t, &cm, &q); err != nil {
			return Niz{}, err
		}
		t = t.UTC()
		v, ima := uVelicini(iz.Velicina, cm, q, krivulje, t)
		if !ima {
			continue
		}
		// Očitanje s pola sata pripada najbližem satu; kad ih na isti sat
		// padne više, ostaje ono bliže punoj uri.
		sat := int64(t.Add(30*time.Minute).Unix() / 3600)
		raz := t.Sub(time.Unix(sat*3600, 0))
		if raz < 0 {
			raz = -raz
		}
		if prije, bilo := odmak[sat]; !bilo || raz < prije {
			vrijednosti[sat] = v
			odmak[sat] = raz
		}
	}
	return NoviNiz(vrijednosti), r.Err()
}

func uVelicini(velicina string, cm sql.NullInt64, q sql.NullFloat64,
	krivulje []models.HQKrivulja, kad time.Time) (float64, bool) {
	if velicina == "vodostaj" {
		if !cm.Valid {
			return 0, false
		}
		return float64(cm.Int64), true
	}
	if q.Valid {
		return q.Float64, true
	}
	if !cm.Valid {
		return 0, false
	}
	k := KrivuljaZa(krivulje, kad)
	if k == nil {
		return 0, false
	}
	v, _, ok := k.ProtokProsiren(int(cm.Int64))
	return v, ok
}

// KrivuljaZa bira krivulju koja vrijedi u zadanom trenutku.
func KrivuljaZa(krivulje []models.HQKrivulja, kad time.Time) *models.HQKrivulja {
	d := kad.Format("2006-01-02")
	for i := range krivulje {
		k := &krivulje[i]
		if k.VrijediOd <= d && (k.VrijediDo == "" || d <= k.VrijediDo) {
			return k
		}
	}
	return nil
}

func (o *Osvjezivac) ucitajKrivulje(ctx context.Context, letva string) ([]models.HQKrivulja, error) {
	if o.Arhiva == nil {
		return nil, nil
	}
	return KrivuljeIzArhive(ctx, o.Arhiva, letva)
}

// KrivuljeIzArhive čita krivulje protoka letve iz arhive, najnovija prva.
func KrivuljeIzArhive(ctx context.Context, arhiva *sql.DB, letva string) ([]models.HQKrivulja, error) {
	r, err := arhiva.QueryContext(ctx, `SELECT id, vrijedi_od, vrijedi_do FROM hq_krivulje
		WHERE letva = ? ORDER BY vrijedi_od DESC`, letva)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []models.HQKrivulja
	for r.Next() {
		var k models.HQKrivulja
		if err := r.Scan(&k.ID, &k.VrijediOd, &k.VrijediDo); err != nil {
			return nil, err
		}
		k.Letva = letva
		out = append(out, k)
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		s, err := arhiva.QueryContext(ctx, `SELECT od_cm, do_cm, oblik, p1, p2, p3, p4
			FROM hq_odsjecci WHERE krivulja = ? ORDER BY od_cm`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for s.Next() {
			var x models.HQOdsjecak
			if err := s.Scan(&x.OdCm, &x.DoCm, &x.Oblik, &x.P1, &x.P2, &x.P3, &x.P4); err != nil {
				s.Close()
				return nil, err
			}
			out[i].Odsjecci = append(out[i].Odsjecci, x)
		}
		s.Close()
	}
	return out, nil
}

// sidraVrhova zapisuje zadnju izmjerenu vrijednost letvi koje nemaju svoj
// račun. To nije prognoza nego sidro: jedan jedini sat, onaj izdavanja, da se
// zna odakle val ulazi.
func (o *Osvjezivac) sidraVrhova(ctx context.Context, nizovi map[Izvor]Niz, vrhovi map[Izvor]bool, sada int64) []Izdana {
	var out []Izdana
	for iz := range vrhovi {
		v, ima := nizovi[iz].U(sada)
		if !ima {
			continue
		}
		i := Izdana{Letva: iz.Letva, Velicina: iz.Velicina, Izdano: sada, Ciljni: sada,
			Vrijednost: v, Dolje: v, Gore: v, Model: o.Model}
		out = append(out, i)
		out = append(out, o.uDrugojVelicini(ctx, []Izdana{i})...)
	}
	return out
}

// sidraDrugih zapisuje mjerenje vrha lanca u onoj veličini u kojoj se ne
// računa. Ne prolazi kroz krivulju — mjereno je, pa ga nema smisla računati.
func sidraDrugih(druge map[Izvor]Niz, sada int64) []Izdana {
	var out []Izdana
	for iz, n := range druge {
		v, ima := n.U(sada)
		if !ima {
			continue
		}
		out = append(out, Izdana{Letva: iz.Letva, Velicina: iz.Velicina,
			Izdano: sada, Ciljni: sada, Vrijednost: v, Dolje: v, Gore: v})
	}
	return out
}

// uDrugojVelicini pretvara prognozu krivuljom: protok u vodostaj i obrnuto.
// Granice raspona idu kroz krivulju jednako kao i sama vrijednost — krivulja
// je rastuća, pa granica od 68 % ostaje granica od 68 %. Množenje nagibom bi
// pri maloj vodi, gdje je krivulja najzakrivljenija, dalo krivu širinu.
func (o *Osvjezivac) uDrugojVelicini(ctx context.Context, izdane []Izdana) []Izdana {
	if len(izdane) == 0 {
		return nil
	}
	krivulje, err := o.ucitajKrivulje(ctx, izdane[0].Letva)
	if err != nil || len(krivulje) == 0 {
		return nil
	}
	ciljna := "vodostaj"
	if izdane[0].Velicina == "vodostaj" {
		ciljna = "protok"
	}
	out := make([]Izdana, 0, len(izdane))
	for _, i := range izdane {
		k := KrivuljaZa(krivulje, time.Unix(i.Ciljni*3600, 0).UTC())
		if k == nil {
			continue
		}
		v, izvanV, ok := pretvori(k, ciljna, i.Vrijednost)
		if !ok {
			continue
		}
		d, izvanD, okD := pretvori(k, ciljna, i.Dolje)
		g, izvanG, okG := pretvori(k, ciljna, i.Gore)
		if !okD || !okG {
			d, g = v, v
			izvanD, izvanG = izvanV, izvanV
		}
		n := i
		n.Velicina, n.Vrijednost, n.Dolje, n.Gore = ciljna, v, d, g
		n.Racunata = true
		n.Izvan = izvanV || izvanD || izvanG
		out = append(out, n)
	}
	return out
}

// Pretvori vodi jednu vrijednost kroz krivulju u traženu veličinu (vodostaj
// ili protok); izvan javlja da je krivulja produljena preko mjerenog.
func Pretvori(k *models.HQKrivulja, ciljna string, v float64) (vrijednost float64, izvan, ok bool) {
	return pretvori(k, ciljna, v)
}

// pretvori vodi jednu vrijednost kroz krivulju u traženu veličinu.
func pretvori(k *models.HQKrivulja, ciljna string, v float64) (float64, bool, bool) {
	if ciljna == "vodostaj" {
		cm, izvan, ok := k.Vodostaj(v)
		return float64(cm), izvan, ok
	}
	q, izvan, ok := k.ProtokProsiren(int(math.Round(v)))
	return q, izvan, ok
}
