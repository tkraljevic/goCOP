package prognoza

// Odvojeni račun: Karašica, Vučica i Bednja (OdvojeniRacuni) i letve koje je
// stručnjak napravio u zapisu modela (model_letve). Računaju se poslije glavnog
// računa, u vlastitom Racunalu, a izvod se samo doda izdanju. Smiju čitati glavni
// lanac, a glavni lanac njih nikad: ne ulaze u SveInacice, TrebaniIzvori ni
// ZadnjiZajednicki, pa rječica koja stoji ne može povući sat izdanja ni srušiti
// izdanje.
//
// Računi iz koda u satu izdanja rade kao dok su bili u glavnom lancu: računata
// letva ima računat redak, a vrh zadnje mjerenje do ZaostatakVrha. Te letve u
// satnom izdanju gotovo nikad još nemaju mjerenje tog sata, a starija inačica na
// drugom računalu bez tog retka ima praznu karticu. Strože pravilo, da se u tom
// satu pokazuje samo mjerenje, vrijedi za letve iz retka modela.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"
)

// RokOdvojenih je koliko odvojeni račun smije trajati. Glavni račun je tada već
// gotov; kad rok prođe, izdanje ide bez novih letvi. Trajanje na računalu koje
// izdaje još nije izmjereno.
var RokOdvojenih = 20 * time.Second

// glavniRacun je ono što je glavni račun već imao u rukama.
type glavniRacun struct {
	racunalo *Racunalo
	inacice  map[string][][]Pojas
	pojasi   map[string][]Pojas // odabrane inačice
	nizovi   map[Izvor]Niz
	od       time.Time
	najdalje int
}

// ulazOdvojenih je sve što odvojeni račun treba. Glavni račun poslije toga više
// ništa od toga ne mijenja, pa račun smije ostati raditi i kad mu rok prođe.
type ulazOdvojenih struct {
	glavniRacun
	zapisi    map[string]ZapisLetve
	pokvareni map[string]error
	glavne    map[string]bool // letve koje pripadaju glavnom računu (letveGlavnog)
	sada      int64
}

// izvodOdvojenih je što je odvojeni račun dao.
type izvodOdvojenih struct {
	izdane     []Izdana
	izbor      map[string]Izbor
	bez        map[string]error
	upozorenja []Upozorenje
	greska     error              // račun nije završio: ne uzima se ništa
	citatelji  map[string][]Pojas // računi iz koda koji su u ovom izdanju računali
}

// dodatniKrug računa letve iz zapisa modela i dodaje izvod izdanju. Pad, panika
// ili prekoračen rok ostaju ovdje: glavni račun je već gotov i ne mijenja se.
func (o *Osvjezivac) dodatniKrug(ctx context.Context, ishod *Ishod, g glavniRacun) {
	zapisi, pokvareni, err := zapisiZaRacun(o.Baza, ishod.Promasaji)
	if err != nil {
		ishod.primiOdvojene(izvodOdvojenih{greska: err}, nil)
		return
	}
	if len(zapisi)+len(pokvareni) == 0 {
		return
	}
	pocetak := time.Now()
	rok, otkazi := context.WithTimeout(ctx, RokOdvojenih)
	defer otkazi()
	u := ulazOdvojenih{glavniRacun: g, zapisi: zapisi, pokvareni: pokvareni, glavne: letveGlavnog(ishod, g.inacice), sada: ishod.Sada}
	gotovo := make(chan izvodOdvojenih, 1)
	go func() { gotovo <- o.odvojeniRacun(rok, u) }()
	var izvod izvodOdvojenih
	select {
	case izvod = <-gotovo:
	case <-rok.Done():
	}
	// Račun koji je stigao baš kad je rok prošao možda je ostao bez dijela čitanja.
	if rok.Err() != nil {
		izvod = izvodOdvojenih{greska: fmt.Errorf("računanje traje dulje od %d s", int(RokOdvojenih.Seconds()))}
	}
	ishod.primiOdvojene(izvod, u.glavne)
	log.Printf("prognoza: odvojeni račun, %d zapisa, %.1f s", len(zapisi)+len(pokvareni), time.Since(pocetak).Seconds())
}

// letveGlavnog su letve koje je glavni račun dotaknuo i one koje računa ili čita.
// Odvojeni račun u njih ne smije dodati nijedan redak, pa ni u drugoj veličini.
func letveGlavnog(ishod *Ishod, inacice map[string][][]Pojas) map[string]bool {
	out := map[string]bool{}
	for l := range zauzeteLetve(inacice) {
		out[l] = true
	}
	for _, x := range ishod.Izdane {
		out[x.Letva] = true
	}
	for _, x := range ishod.Dnevne {
		out[x.Letva] = true
	}
	for l := range ishod.BezPrognoze {
		out[l] = true
	}
	for l := range ishod.BezDnevne {
		out[l] = true
	}
	return out
}

// odvojeniRacun radi cijeli račun u vlastitoj gorutini. Skokove i kvarove bilježi
// za sebe, jer je glavni račun svoje već pročitao.
func (o *Osvjezivac) odvojeniRacun(ctx context.Context, u ulazOdvojenih) (izvod izvodOdvojenih) {
	defer func() {
		if e := recover(); e != nil {
			log.Printf("prognoza: odvojeni račun: %v", e)
			izvod = izvodOdvojenih{greska: fmt.Errorf("greška u računu: %v", e)}
		}
	}()
	ctx = sBiljeznikomKvarova(sBiljeznikomSkokova(ctx))
	isk := ProvjeriZapise(u.zapisi, u.pokvareni, o.okvirModela(ctx, u))
	dobri := ostaliZapisi(u.zapisi, isk)
	nizovi := o.nizoviOdvojenih(ctx, dobri, u.glavniRacun)
	if err := ctx.Err(); err != nil {
		return izvodOdvojenih{greska: err}
	}
	odabrano, izbor, bez := odaberiOdvojene(dobri, u.pojasi, nizovi, u.sada)
	izvod = izvodOdvojenih{izbor: izbor, bez: bez}
	izvod.izdane = o.izdaneOdabranih(ctx, racunaloOdvojenih(odabrano, nizovi, u), dobri, odabrano, izbor, nizovi, bez, u)
	izvod.izdane = append(izvod.izdane, o.sidraOdvojenih(ctx, dobri, odabrano, izbor, nizovi, u)...)
	izvod.upozorenja = upozorenjaOdvojenih(ctx, izvod, dobri, odabrano, nizovi, isk, u)
	izvod.citatelji = racunatiIzKoda(dobri, odabrano, bez)
	maps.Copy(bez, isk)
	return izvod
}

// okvirModela je okvir provjere pri izdavanju. Ono što se ne da pročitati (baza
// bez tih tablica ili stupaca) ne isključuje letvu, nego ostaje neprovjereno.
func (o *Osvjezivac) okvirModela(ctx context.Context, u ulazOdvojenih) OkvirModela {
	return OkvirModela{Glavne: u.inacice, UIzdanju: u.glavne,
		ImaSektor:      func(letva string) bool { return LetvaImaSektor(ctx, o.Ocitanja, letva) },
		ImaSatniProtok: func(letva string) bool { return ImaSatniProtok(ctx, o.Arhiva, letva) }}
}

// LetvaImaSektor javlja ima li letva sektor u registru: preko dionice ili upisom
// u obrascu letve.
func LetvaImaSektor(ctx context.Context, registar *sql.DB, letva string) bool {
	if registar == nil {
		return true
	}
	var sektori, podrucja string
	var dionica int
	err := registar.QueryRowContext(ctx, `SELECT s.prate_sektori, s.prate_podrucja,
		EXISTS (SELECT 1 FROM section_stations ss WHERE ss.station_id = s.id)
		FROM stations s WHERE s.code = ?`, letva).Scan(&sektori, &podrucja, &dionica)
	if err == sql.ErrNoRows {
		return false
	}
	return err != nil || dionica != 0 || sektori != "" || podrucja != ""
}

// ImaSatniProtok javlja ima li letva satni protok u arhivi.
func ImaSatniProtok(ctx context.Context, arhiva *sql.DB, letva string) bool {
	if arhiva == nil {
		return true
	}
	var ima int
	err := arhiva.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM spoj
		WHERE letva = ? AND velicina = 'protok' AND korak = 'satni')`, letva).Scan(&ima)
	return err != nil || ima != 0
}

// nizoviOdvojenih su nizovi svih inačica: ono što je glavni račun već pročitao
// ostaje isto, a ostalo se čita samo kako je letva izmjerila, bez sati s druge
// obale i iz susjeda. Niz koji se ne da pročitati ostaje prazan, pa njegova letva
// nije svježa.
func (o *Osvjezivac) nizoviOdvojenih(ctx context.Context, dobri map[string]ZapisLetve, g glavniRacun) map[Izvor]Niz {
	sve := map[string][]Pojas{}
	for l, z := range dobri {
		sve[l] = slices.Concat(z.Brojevi.Inacice...)
	}
	trebani, _ := TrebaniIzvori(sve)
	nizovi := maps.Clone(g.nizovi)
	for iz := range trebani {
		if _, ima := nizovi[iz]; ima {
			continue
		}
		n, err := o.ucitajIzmjereni(ctx, iz, g.od)
		if err != nil {
			log.Printf("prognoza: odvojeni račun, niz %s (%s): %v", iz.Letva, iz.Velicina, err)
			n = Niz{}
		}
		nizovi[iz] = n
	}
	return nizovi
}

// odaberiOdvojene bira inačicu svake letve. Za razliku od OdaberiInacice, račun
// kojem ulazi nisu svježi, a ni letva nije, ne ostaje u računu: ispada on i sve
// nizvodno, a sat izdanja se ne mijenja. Svježina se mjeri prema satu izdanja, a
// ne prema najsvježijem očitanju, pa kad sat izdanja kasni, vrh koji mu je unutar
// ZaostatakVrha računa, iako je iza najsvježijih više od toga.
func odaberiOdvojene(dobri map[string]ZapisLetve, bazni map[string][]Pojas, nizovi map[Izvor]Niz, sada int64) (map[string][]Pojas, map[string]Izbor, map[string]error) {
	glavne := make(map[string][]Pojas, len(dobri))
	for l, z := range dobri {
		glavne[l] = z.Brojevi.Inacice[0]
	}
	odabrano, izbor, bez := map[string][]Pojas{}, map[string]Izbor{}, map[string]error{}
	for _, letva := range Redom(glavne) {
		z := dobri[letva]
		in := z.Brojevi.Inacice
		uzeta := slices.IndexFunc(in, func(ps []Pojas) bool { return ulaziSvjezi(ps, odabrano, bazni, nizovi, sada) })
		switch {
		case uzeta >= 0:
			odabrano[letva] = in[uzeta]
			if uzeta > 0 {
				izbor[letva] = Izbor{Inacica: uzeta, Opis: "rezerva: " + imenaSvihUlaza(in[uzeta]) + " umjesto " + imenaSvihUlaza(in[0])}
			}
		case svjezIzvor(Izvor{Letva: letva, Velicina: z.Velicina}, nizovi, sada):
			izbor[letva] = Izbor{Inacica: -1, Opis: "bez svježeg ulaza (" + imenaSvihUlaza(in[0]) + "): drži se zadnje mjerenje"}
		default:
			bez[letva] = fmt.Errorf("nema svježih očitanja ni svježeg ulaza (%s)", imenaSvihUlaza(in[0]))
		}
	}
	return odabrano, izbor, bez
}

// ulaziSvjezi javlja ima li svaki ulaz inačice svoj račun ili svježe mjerenje, u
// svim razinama: račun za sate poslije izdanja drži zadnje mjerenje ulaza koliko
// god staro bilo, pa ulaz koji stoji u višoj razini ne bi nitko primijetio.
func ulaziSvjezi(ps []Pojas, odabrano, bazni map[string][]Pojas, nizovi map[Izvor]Niz, sada int64) bool {
	for _, p := range ps {
		for _, u := range p.Ulazi {
			_, nasa := odabrano[u.Letva]
			_, bazna := bazni[u.Letva]
			if !nasa && !bazna && !svjezIzvor(Izvor{Letva: u.Letva, Velicina: u.Velicina}, nizovi, sada) {
				return false
			}
		}
	}
	return true
}

// imenaSvihUlaza su ulazi inačice u svim razinama, svaki jednom.
func imenaSvihUlaza(ps []Pojas) string {
	var s []string
	for _, p := range ps {
		for _, u := range p.Ulazi {
			if !slices.Contains(s, u.Letva) {
				s = append(s, u.Letva)
			}
		}
	}
	return strings.Join(s, " + ")
}

func svjezIzvor(iz Izvor, nizovi map[Izvor]Niz, sada int64) bool {
	z, ima := nizovi[iz].ZadnjiSatDo(sada)
	return ima && z+ZaostatakVrha >= sada
}

// racunaloOdvojenih je vlastito Racunalo: glavno se ne dira, pa mu ni ono što je
// već izračunalo ne može promijeniti. Budućnost vrhova ista je kao u glavnom,
// pa račun koji čita glavni lanac dobiva njegove vrijednosti.
func racunaloOdvojenih(odabrano map[string][]Pojas, nizovi map[Izvor]Niz, u ulazOdvojenih) *Racunalo {
	spojeno := make(map[string][]Pojas, len(u.pojasi)+len(odabrano))
	maps.Copy(spojeno, u.pojasi)
	maps.Copy(spojeno, odabrano)
	r := NovoRacunalo(spojeno, nizovi, u.sada)
	r.buducnost, r.dnevniCvor = u.racunalo.buducnost, u.racunalo.dnevniCvor
	return r
}

// izdaneOdabranih računa odabrane letve redom toka. Letva kojoj je ulaz ostao bez
// prognoze ne računa se: sirovi broj odbijene letve još stoji u Racunalu, pa bi
// iz njega izašla prognoza koja izgleda uvjerljivo.
func (o *Osvjezivac) izdaneOdabranih(ctx context.Context, r *Racunalo, dobri map[string]ZapisLetve, odabrano map[string][]Pojas,
	izbor map[string]Izbor, nizovi map[Izvor]Niz, bez map[string]error, u ulazOdvojenih) []Izdana {
	var izdane []Izdana
	for _, letva := range Redom(odabrano) {
		if pao, ima := ulazBezPrognoze(odabrano[letva], bez); ima {
			bez[letva] = fmt.Errorf("računa se iz letve %s, a ona je ostala bez prognoze", pao)
			continue
		}
		redci, err := o.izdaneLetve(ctx, r, dobri[letva], izbor[letva].Inacica == 0, nizovi[Izvor{Letva: letva, Velicina: dobri[letva].Velicina}], u)
		if err != nil {
			bez[letva] = err
			continue
		}
		izdane = append(izdane, redci...)
	}
	return izdane
}

// ulazBezPrognoze je prvi ulaz inačice koji je ostao bez prognoze.
func ulazBezPrognoze(ps []Pojas, bez map[string]error) (string, bool) {
	for _, p := range ps {
		for _, u := range p.Ulazi {
			if _, pao := bez[u.Letva]; pao {
				return u.Letva, true
			}
		}
	}
	return "", false
}

// izdaneLetve su redci jedne letve, u obje veličine.
func (o *Osvjezivac) izdaneLetve(ctx context.Context, r *Racunalo, z ZapisLetve, glavna bool, mjereno Niz, u ulazOdvojenih) ([]Izdana, error) {
	izdane, err := r.Prognoziraj(z.Letva, min(z.Racun.Doseg, u.najdalje), o.Model)
	if err != nil {
		return nil, err
	}
	if !z.Ugradjen {
		izdane = bezRacunatogSata(izdane, mjereno, u.sada)
	}
	if len(izdane) == 0 {
		return nil, bezRedaka(r.poVelicini[Izvor{Letva: z.Letva, Velicina: z.Velicina}])
	}
	if glavna {
		ispraviPromasajima(izdane, z.Brojevi.Promasaji, u.sada)
	}
	izdane = append(izdane, o.uDrugojVelicini(ctx, izdane)...)
	if !razumneIzdane(izdane) {
		return nil, fmt.Errorf("račun je dao nemoguće brojeve")
	}
	donjiRubProtoka(izdane)
	return izdane, nil
}

// bezRedaka kaže zašto letva nema nijedan redak. Ulazi su tu već svježi, pa letvi s
// isključenom razinom račun staje upravo na toj razini.
func bezRedaka(pojasi []Pojas) error {
	if imaNepovezanih(pojasi) {
		return errors.New("voda je u razini za koju je račun isključen, a letva u ovom satu nije izmjerena")
	}
	return errors.New("nema ulaza za nijedan sat unaprijed")
}

// bezRacunatogSata izbacuje redak u satu izdanja koji nije mjerenje. Račun bi za
// letvu koja kasni u tom satu dao svoju vrijednost, a stranica bi je pokazala kao
// „sada”. Procjena nikad ne smije izgledati kao mjerenje.
func bezRacunatogSata(izdane []Izdana, mjereno Niz, sada int64) []Izdana {
	if _, ima := tocnoIzmjereno(mjereno, sada); ima {
		return izdane
	}
	return slices.DeleteFunc(izdane, func(x Izdana) bool { return x.Ciljni == sada })
}

// tocnoIzmjereno je vrijednost samo ako je u tom satu stvarno izmjerena, bez
// premošćivanja rupe.
func tocnoIzmjereno(n Niz, sat int64) (float64, bool) {
	i, ima := slices.BinarySearch(n.Sati, sat)
	if !ima {
		return 0, false
	}
	return n.Iznosi[i], true
}

// ispraviPromasajima oduzima izmjereni promašaj i uzima njegov rasap, kao glavni
// račun. Mjereni su za glavnu inačicu, pa rezerva ostaje na rasapu namještanja.
func ispraviPromasajima(izdane []Izdana, promasaji map[int]Promasaj, sada int64) {
	for i := range izdane {
		p, ima := ispravakNaDosegu(promasaji, izdane[i].Ciljni-sada)
		if !ima {
			continue
		}
		izdane[i].Vrijednost -= p.Pomak
		izdane[i].Dolje, izdane[i].Gore = izdane[i].Vrijednost-p.Rasap, izdane[i].Vrijednost+p.Rasap
	}
}

// razumneIzdane je zadnja ograda: broj koji se ne da zapisati ili poslati srušio
// bi Zapisi ili Zamotaj za cijelo izdanje.
func razumneIzdane(izdane []Izdana) bool {
	return !slices.ContainsFunc(izdane, func(x Izdana) bool { return !razumnaIzdana(x) })
}

func razumnaIzdana(x Izdana) bool {
	return uGranicama(x.Vrijednost, najvecaIzdana) && uGranicama(x.Dolje, najvecaIzdana) && uGranicama(x.Gore, najvecaIzdana)
}

// sidraOdvojenih daje redak u satu izdanja vrhovima novih računa i letvama koje
// drže zadnje mjerenje, samo ako su u tom satu izmjerene. Vrhovi računa iz koda
// dobivaju zadnje mjerenje kao vrhovi glavnog lanca. Letve glavnog računa dobivaju
// svoje retke od njega.
func (o *Osvjezivac) sidraOdvojenih(ctx context.Context, dobri map[string]ZapisLetve, odabrano map[string][]Pojas,
	izbor map[string]Izbor, nizovi map[Izvor]Niz, u ulazOdvojenih) []Izdana {
	izvori := izvoriSidara(dobri, odabrano, izbor, zauzeteLetve(u.inacice))
	izKoda := izvoriIzKoda(dobri, odabrano, izbor)
	var mjereni, izvedeni []Izdana
	for _, iz := range izvori {
		v, ima := tocnoIzmjereno(nizovi[iz], u.sada)
		if izKoda[iz] {
			v, ima = sidroVrha(nizovi[iz], u.sada)
		}
		if !ima {
			continue
		}
		x := Izdana{Letva: iz.Letva, Velicina: iz.Velicina, Izdano: u.sada, Ciljni: u.sada, Vrijednost: v, Dolje: v, Gore: v, Model: o.Model}
		mjereni = append(mjereni, x)
		izvedeni = append(izvedeni, o.sidroDrugeVelicine(ctx, x, u.od, izKoda[iz])...)
	}
	return append(mjereni, izvedeni...)
}

// sidroDrugeVelicine je isti sat u veličini u kojoj se letva ne računa. Mjereno se
// ne provlači kroz krivulju, kao ni kod vrhova glavnog lanca (sidraDrugih), a račun
// iz koda uzima i premošćenu rupu, kao ondje.
func (o *Osvjezivac) sidroDrugeVelicine(ctx context.Context, x Izdana, od time.Time, izKoda bool) []Izdana {
	druga := Izvor{Letva: x.Letva, Velicina: "protok"}
	if x.Velicina == "protok" {
		druga.Velicina = "vodostaj"
	}
	if n, err := o.ucitajIzmjereni(ctx, druga, od); err == nil {
		v, ima := tocnoIzmjereno(n, x.Ciljni)
		if izKoda {
			v, ima = n.U(x.Ciljni)
		}
		if ima {
			return []Izdana{{Letva: druga.Letva, Velicina: druga.Velicina, Izdano: x.Izdano, Ciljni: x.Ciljni, Vrijednost: v, Dolje: v, Gore: v}}
		}
	}
	return o.uDrugojVelicini(ctx, []Izdana{x})
}

// izvoriSidara su vrhovi odabranih računa koji ne pripadaju glavnom računu i
// letve koje drže zadnje mjerenje.
func izvoriSidara(dobri map[string]ZapisLetve, odabrano map[string][]Pojas, izbor map[string]Izbor, zauzete map[string]string) []Izvor {
	_, vrhovi := TrebaniIzvori(odabrano)
	skup := map[Izvor]bool{}
	for iz := range vrhovi {
		if _, zauzeta := zauzete[iz.Letva]; !zauzeta && !jeElektranaIliBrana(iz.Letva) {
			skup[iz] = true
		}
	}
	for l, iz := range izbor {
		if iz.Inacica < 0 {
			skup[Izvor{Letva: l, Velicina: dobri[l].Velicina}] = true
		}
	}
	return slices.SortedFunc(maps.Keys(skup), func(a, b Izvor) int {
		return strings.Compare(a.Letva+" "+a.Velicina, b.Letva+" "+b.Velicina)
	})
}

// izvoriIzKoda su letve računa iz koda s ulazima i letve iz koda koje drže zadnje
// mjerenje.
func izvoriIzKoda(dobri map[string]ZapisLetve, odabrano map[string][]Pojas, izbor map[string]Izbor) map[Izvor]bool {
	racuni := map[string][]Pojas{}
	for l, ps := range odabrano {
		if dobri[l].Ugradjen {
			racuni[l] = ps
		}
	}
	out, _ := TrebaniIzvori(racuni)
	for l, iz := range izbor {
		if iz.Inacica < 0 && dobri[l].Ugradjen {
			out[Izvor{Letva: l, Velicina: dobri[l].Velicina}] = true
		}
	}
	return out
}

// upozorenjaOdvojenih su napomene o novim letvama. Glavni račun je svoja
// upozorenja već složio, pa ove idu pod vlastitom vrstom.
func upozorenjaOdvojenih(ctx context.Context, izvod izvodOdvojenih, dobri map[string]ZapisLetve, odabrano map[string][]Pojas,
	nizovi map[Izvor]Niz, isk map[string]error, u ulazOdvojenih) []Upozorenje {
	out := upozorenjaIskljucenih(isk, u.glavne)
	for _, l := range slices.Sorted(maps.Keys(izvod.bez)) {
		out = append(out, Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaNapomena, Letva: l, Opis: "nema prognoze: " + izvod.bez[l].Error()})
	}
	out = append(out, upozorenjaIzboraOdvojenih(izvod, odabrano, u.sada)...)
	out = append(out, upozorenjaBezMjerenja(dobri, odabrano, nizovi, izvod.bez, u.sada)...)
	out = append(out, upozorenjaKrivuljeOdvojenih(izvod.izdane, u.sada)...)
	jeNova := func(l string) bool { return !u.glavne[l] }
	skokovi := slices.DeleteFunc(skokoviIzdanja(ctx), func(s IzbaceniSkok) bool { return !jeNova(s.Letva) })
	kvarovi := slices.DeleteFunc(kvaroviIzdanja(ctx), func(k KvarMjerila) bool { return !jeNova(k.Letva) })
	out = append(out, upozorenjaSkokova(skokovi, u.sada)...)
	return append(out, upozorenjaKvarova(kvarovi, u.sada)...)
}

// upozorenjaIskljucenih kažu koji se zapis modela ne računa. Letva glavnog računa
// računa dalje s istim brojevima, pa takav redak ne stoji na njoj (inače ulazi na
// njezin list izvoza i čita se kao da joj prognoza ne radi): samo napomena.
func upozorenjaIskljucenih(isk map[string]error, glavne map[string]bool) []Upozorenje {
	out := make([]Upozorenje, 0, len(isk))
	for _, l := range slices.Sorted(maps.Keys(isk)) {
		if glavne[l] {
			out = append(out, Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaNapomena, Opis: "zapis modela za letvu " + l + " se ne koristi: " + isk[l].Error()})
			continue
		}
		out = append(out, Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaUpozorenje, Letva: l, Opis: "model letve je isključen: " + isk[l].Error()})
	}
	return out
}

// upozorenjaKrivuljeOdvojenih kažu da je broj nove letve računat preko ruba
// krivulje. Upozorenje glavnog računa o krivulji je već složeno i ostaje kakvo je.
func upozorenjaKrivuljeOdvojenih(izdane []Izdana, sada int64) []Upozorenje {
	var out []Upozorenje
	for _, k := range upozorenjeKrivulje(izdane, sada) {
		for _, q := range k.Upitno {
			opis := "izvan krivulje protoka"
			if q.OdSata > 0 {
				opis += fmt.Sprintf(" od +%d h", q.OdSata)
			}
			out = append(out, Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaNapomena, Letva: q.Letva,
				Opis: opis + " (krivulja je produžena preko ruba)"})
		}
	}
	return out
}

// upozorenjaIzboraOdvojenih kažu da letva računa iz rezerve ili drži zadnje
// mjerenje, i što je zbog toga upitno nizvodno.
func upozorenjaIzboraOdvojenih(izvod izvodOdvojenih, odabrano map[string][]Pojas, sada int64) []Upozorenje {
	ishod := &Ishod{Sada: sada, BezPrognoze: izvod.bez}
	var out []Upozorenje
	for _, l := range slices.Sorted(maps.Keys(izvod.izbor)) {
		iz := izvod.izbor[l]
		s := skupUpitnog{}
		if iz.Inacica < 0 {
			s.satna(satnaNizVrha(ishod, odabrano, l))
		} else {
			s.satna(satnaNizvodno(ishod, odabrano, l, sada+1))
		}
		out = append(out, Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaNapomena, Letva: l, Opis: iz.Opis, Upitno: s.popis()})
	}
	return out
}

// upozorenjaBezMjerenja kažu da letva dugo nema mjerenja, pa joj se prognoza
// računa bez oslonca na vlastito očitanje. Karika računa iz koda u satu izdanja
// ima računat broj, pa je upitna ona i letve ispod nje, kao u glavnom lancu. Krajnje
// letve tamo ne ulaze, pa ni ovdje.
func upozorenjaBezMjerenja(dobri map[string]ZapisLetve, odabrano map[string][]Pojas, nizovi map[Izvor]Niz, bez map[string]error, sada int64) []Upozorenje {
	ishod := &Ishod{Sada: sada, BezPrognoze: bez}
	karike := karikeBezMjerenja(odabrano, nizovi, sada)
	var out []Upozorenje
	for _, l := range slices.Sorted(maps.Keys(odabrano)) {
		z, ima := nizovi[Izvor{Letva: l, Velicina: dobri[l].Velicina}].ZadnjiSatDo(sada)
		if ima && sada-z <= ZaostatakKarike {
			continue
		}
		u := Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaNapomena, Letva: l, Opis: "nema mjerenja, a prognoza se računa iz uzvodnih letvi"}
		if ima {
			u.Zadnje = z
			u.Opis = "nema mjerenja od " + satDnevnika(z) + ", a prognoza se računa iz uzvodnih letvi"
		}
		if _, karika := karike[l]; karika && dobri[l].Ugradjen {
			s := skupUpitnog{}
			s.satna(satnaNizvodno(ishod, odabrano, l, z+1))
			u.Upitno = s.popis()
		}
		out = append(out, u)
	}
	return out
}

// primiOdvojene dodaje izvod izdanju. Letve glavnog računa ostaju netaknute, pa i
// kad je pokvaren redak nosio njihovu šifru.
func (i *Ishod) primiOdvojene(izvod izvodOdvojenih, glavne map[string]bool) {
	if izvod.greska != nil {
		i.Upozorenja = append(i.Upozorenja, Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaUpozorenje,
			Opis: "Karašica, Vučica, Bednja i letve s novim modelom nisu računate u ovom izdanju: " + izvod.greska.Error()})
		poredajUpozorenja(i.Upozorenja)
		return
	}
	dopuniUpitno(i.Upozorenja, izvod.citatelji, i.Sada)
	odbaceni := i.primiRetke(izvod.izdane, glavne)
	maps.Copy(i.Izbor, izvod.izbor)
	for l, err := range izvod.bez {
		if !glavne[l] {
			i.BezPrognoze[l] = err
		}
	}
	i.Upozorenja = append(append(i.Upozorenja, izvod.upozorenja...), odbaceni...)
	poredajUpozorenja(i.Upozorenja)
}

// racunatiIzKoda su računi iz koda koji su u ovom izdanju dali prognozu.
func racunatiIzKoda(dobri map[string]ZapisLetve, odabrano map[string][]Pojas, bez map[string]error) map[string][]Pojas {
	out := map[string][]Pojas{}
	for l, ps := range odabrano {
		if _, pala := bez[l]; dobri[l].Ugradjen && !pala {
			out[l] = ps
		}
	}
	return out
}

// dopuniUpitno vraća račune iz koda na popise upitnih letvi. Jelengrad čita Osijek
// i Belišće, pa mu je prognoza upitna kad je i njihova, a glavni račun o njemu više
// ne zna. Letve iz zapisa modela ovdje ne ulaze: glavna upozorenja ostaju kakva su.
func dopuniUpitno(upozorenja []Upozorenje, citatelji map[string][]Pojas, sada int64) {
	for i := range upozorenja {
		prvi := prviUpitniSati(upozorenja[i], sada)
		if len(prvi) == 0 {
			continue
		}
		a := Nizvodno(citatelji, prvi, sada)
		for _, l := range slices.Sorted(maps.Keys(citatelji)) {
			_, vec := prvi[l]
			if t, ima := a[l]; ima && !vec {
				upozorenja[i].Upitno = append(upozorenja[i].Upitno, Upitno{Letva: l, OdSata: int(t - sada)})
				poredajUpitno(upozorenja[i].Upitno)
			}
		}
	}
}

// prviUpitniSati su sati od kojih su letve upozorenja satno upitne; prazno kad
// upozorenje ne ide niz lanac. Doseg 0 znači da je nevolja počela prije izdanja,
// pa se letva nizvodno računa kao upitna od prvog sata.
func prviUpitniSati(u Upozorenje, sada int64) map[string]int64 {
	if !slices.Contains([]string{VrstaUlaz, VrstaRezerva, VrstaDrzi, VrstaVrh, VrstaRekonstrukcija}, u.Vrsta) {
		return nil
	}
	prvi := map[string]int64{}
	if u.Vrsta == VrstaDrzi || u.Vrsta == VrstaVrh {
		prvi[u.Letva] = sada + 1
	}
	for _, q := range u.Upitno {
		switch {
		case q.OdSata == 0:
			prvi[q.Letva] = 0
		case q.OdSata > 0:
			prvi[q.Letva] = sada + int64(q.OdSata)
		}
	}
	return prvi
}

// primiRetke dodaje retke novih letvi. Ovo je zadnja ograda za svaki put kojim
// redak stiže, i za izmjereni broj i njegovu pretvorbu kroz krivulju: broj koji se
// ne da zapisati ili poslati srušio bi Zapisi ili Zamotaj za cijelo izdanje.
func (i *Ishod) primiRetke(izdane []Izdana, glavne map[string]bool) []Upozorenje {
	vidjeno := map[[2]any]bool{}
	nerazumne := map[string]bool{}
	for _, x := range izdane {
		k := [2]any{Izvor{Letva: x.Letva, Velicina: x.Velicina}, x.Ciljni}
		if glavne[x.Letva] || vidjeno[k] {
			continue
		}
		vidjeno[k] = true
		if !razumnaIzdana(x) {
			nerazumne[x.Letva] = true
			continue
		}
		i.Izdane = append(i.Izdane, x)
	}
	var out []Upozorenje
	for _, l := range slices.Sorted(maps.Keys(nerazumne)) {
		out = append(out, Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaNapomena, Letva: l, Opis: "nemoguć broj nije ušao u izdanje; provjeri očitanje i krivulju"})
	}
	return out
}
