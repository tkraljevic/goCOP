package prognoza

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
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

// Lanac ne čita nijednu tuđu prognozu (Odluke (22. 10. 2026.)). Vrh lanca
// budućnost dobiva iz našeg dnevnog modela (VrhoviIzDnevnog) ili iz modela
// ispuštanja elektrane, a inače drži zadnje mjerenje. Tuđe prognoze
// (mađarska, austrijska, srpska) preuzimaju se i stoje uz našu samo radi
// usporedbe.
//
// Dotad su vrhove vodile tuđe prognoze:
//   - Wildungsmauer, vrh Dunava, od 25. 9. 2026. slijedio je 48-satnu
//     prognozu Donje Austrije (noel.gv.at). Dok je mađarska prognoza
//     Komároma svježa, austrijska je mijenjala samo Nagybajcs, a ni točna
//     austrijska ne popravlja Batinu i Ilok unutar 96 sati.
//   - Komárom je od 23. 9. 2026. slijedio mađarsku prognozu (hydroinfo.hu)
//     dok je svježa, a od 25. 9. bez nje se računao iz Nagybajcsa i
//     Wildungsmauera. Na 27 mađarskih izdanja 2024.–2026. (uglavnom valovi)
//     taj lanac Komárom promaši 18, 38, 61, 90, 113 i 131 cm za 1.–6. dan
//     prema njihovih 12, 20, 28, 38, 47 i 59, ali pogrešku satne prognoze
//     naših letvi na 96 sati mađarska mijenja za 0–3 cm, a tablicu dana
//     nimalo (dnevni model tuđe prognoze ne čita). Pomaže samo mađarskom
//     Dunavu u valu (pogreška Budimpešte na 96 h 34 cm s njom, 78 bez nje),
//     a pri maloj vodi naš je lanac ondje bolji (11,1 prema 13,2 cm).
//   - Letenye je mađarsku prognozu uzimao samo kad mu budućnost ne da naš
//     dnevni model; u 589 izdanja od 26. 9. 2026. to se nije dogodilo.

// PregledneLetve se na pregledu prognoza pokazuju s mjerenjem, iako u model ne
// ulaze: Bratislava i Komárno stoje u svakoj uredskoj tablici za Dunav, a
// srpske letve nasuprot našima nose srpsku prognozu. Mursko Središće tu više
// ne stoji, jer je ulaz dnevne prognoze Drave.
var PregledneLetve = []string{"bratislava", "komarno",
	"bezdan", "apatin", "bogojevo", "backa-palanka",
	// razine akumulacija HEP-ovih brana na Dravi: kota nad morem, na
	// uzdužnom profilu točka uz branu
	"gvb-he-varazdin", "gvb-he-cakovec", "gvb-he-dubrava",
	// inundacija Dunava uz Tikveš: vanjske letve na dunavcima od Draža do Kopačeva
	"ustava-draz-nizvodno", "dunav-zmajevac", "zlatna-greda",
	"ustava-kopacevo-nizvodno"}

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
	// Oborine daju dnevnom modelu kišu po međuslivovima; nil znači da se
	// dnevni model uči i izdaje bez oborine.
	Oborine *OborinskiIzvor
	// Cvor je naziv čvora koji izdaje; ide uz zapis o izdanju i putuje s njim.
	Cvor string
}

// Ishod je što je jedno osvježavanje dalo.
type Ishod struct {
	Sada           int64                      // sat za koji je prognoza izdana
	Vrhovi         map[Izvor]int64            // zadnji izmjereni sat svakog vrha lanca
	Izdane         []Izdana                   // sve izračunate vrijednosti
	Promasaji      map[Izvor]map[int]Promasaj // po letvi i veličini
	Preskoceno     bool                       // isti sat već je izdan, ništa se nije mijenjalo
	BezPrognoze    map[string]error           // letve koje nisu dale nijedan sat
	Dnevne         []DnevnaIzdana             // dnevna prognoza za 1.–6. dan
	OperaterVrhovi []string                   // vrhovi kojima je budućnost dao model ispuštanja elektrane
	NasiVrhovi     []string                   // vrhovi kojima je budućnost dao naš dnevni model
	BezDnevne      map[string]error           // letve s dnevnim modelom koje ga nisu dale
	Izbor          map[string]Izbor           // letve koje su računate iz rezerve, i iz koje
	// Kisa je oborina po međuslivovima koju je dnevni model imao: dan 0 je
	// 24 sata do izdanja, negativni dani pala kiša, pozitivni prognoza.
	Kisa map[string]DnevniNiz
	// Verzija je redni broj zapisa ovog sata izdanja, kad je zapisan.
	Verzija int
	// Ceka su vrhovi lanca koji kasne više od ZaostatakVrha i zato drže sat
	// izdanja iza najsvježijih očitanja: letva → zadnji izmjereni sat. Bez
	// ovoga je prognoza satima tiho stajala („za ovaj sat već je izdana”), a
	// nije se vidjelo da čeka letvu kojoj je izvor zapeo.
	Ceka map[string]int64
	// Upozorenja kažu što pri izdavanju nije radilo i koji su rezultati
	// zbog toga upitni (Upozori); zapisuju se s izdanjem i putuju s njim.
	Upozorenja []Upozorenje
	// Rekonstruirano su rupe letvi koje su susjedi popunili, a sežu u
	// zadnja četiri dana prije izdanja: letva → rupe redom.
	Rekonstruirano map[string][]RekonstruiranaRupa
}

// KoCeka opisuje vrhove koji drže sat izdanja, npr. „kotoriba (zadnje 29.9. u
// 04:00)”; prazno kad nitko ne kasni.
func (i *Ishod) KoCeka() string {
	var s []string
	for letva, z := range i.Ceka {
		s = append(s, fmt.Sprintf("%s (zadnje %s)", letva, time.Unix(z*3600, 0).In(models.Zagreb).Format("2.1. u 15:04")))
	}
	sort.Strings(s)
	return strings.Join(s, ", ")
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
	ctx = uKrugu(ctx)
	inacice, err := SveInacice(o.Baza)
	if err != nil {
		return nil, err
	}
	if len(inacice) == 0 {
		return nil, fmt.Errorf("baza prognoza je prazna; prvo pokreni namjesti-prognozu")
	}
	promasaji, err := Promasaji(o.Baza)
	if err != nil {
		return nil, err
	}

	// Nizovi se čitaju za ulaze svih inačica, glavnih i rezervnih: tek po
	// njihovoj svježini bira se kojim putem prognoza ide.
	svePojase := map[string][]Pojas{}
	for letva, in := range inacice {
		for _, ps := range in {
			svePojase[letva] = append(svePojase[letva], ps...)
		}
	}
	trebani, _ := TrebaniIzvori(svePojase)
	// Modeli ispuštanja elektrana trebaju i nizove koji u lanac ne ulaze
	// (dotok uzvodnih elektrana); bez modela nema ni tih nizova.
	operateri, err := UcitajOperatere(o.Baza)
	if err != nil {
		return nil, fmt.Errorf("modeli ispuštanja: %w", err)
	}
	for _, iz := range OperaterIzvori(operateri) {
		trebani[iz] = true
	}
	od := time.Now().UTC().Add(-Unatrag)
	nizovi := map[Izvor]Niz{}
	for iz := range trebani {
		n, err := o.ucitajNiz(ctx, iz, od)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", iz.Letva, iz.Velicina, err)
		}
		nizovi[iz] = n
	}
	pojasi, izbor := OdaberiInacice(inacice, nizovi, 0)
	// Kartica letve kojoj je kraj niza došao s druge obale to kaže.
	for iz := range nizovi {
		if _, bilo := izbor["zamjena:"+iz.Letva]; bilo {
			continue
		}
		if z, ima := o.zamjenaNaKraju(ctx, iz.Letva, od); ima {
			izbor["zamjena:"+iz.Letva] = Izbor{Opis: z.Opis()}
		}
	}
	_, vrhovi := TrebaniIzvori(pojasi)
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
	ishod := &Ishod{Sada: sada, Promasaji: promasaji, Izbor: izbor,
		Vrhovi: map[Izvor]int64{}, BezPrognoze: map[string]error{}}
	var najsvjeziji int64
	for iz := range vrhovi {
		if z, ima := nizovi[iz].Zadnji(); ima {
			ishod.Vrhovi[iz] = z
			najsvjeziji = max(najsvjeziji, z)
		}
	}
	if sada < najsvjeziji {
		for iz, z := range ishod.Vrhovi {
			if z+ZaostatakVrha <= sada {
				if ishod.Ceka == nil {
					ishod.Ceka = map[string]int64{}
				}
				ishod.Ceka[iz.Letva] = z
			}
		}
	}
	if zadnje, ima, err := ZadnjeIzdanje(o.Baza); err == nil && ima && zadnje == sada && !o.Iznova {
		ishod.Preskoceno = true
		return ishod, nil
	}

	r := NovoRacunalo(pojasi, nizovi, sada)
	o.napomeneRekonstrukcije(ctx, nizovi, ishod, od)
	nasi := o.vrhoviIzDnevnog(ctx, sada, od, vrhovi)
	for iz, n := range nasi {
		r.PostaviBuducnostVrha(iz, n)
		ishod.NasiVrhovi = append(ishod.NasiVrhovi, iz.Letva)
		ishod.Izbor["vrh:"+iz.Letva] = Izbor{Inacica: 1, Opis: "budućnost vrha lanca iz našeg dnevnog modela s kišom"}
	}
	sort.Strings(ishod.NasiVrhovi)
	for iz, n := range o.cvoroviIzDnevnog(ctx, sada, od) {
		r.PostaviDnevniCvora(iz, n)
		ishod.Izbor["cvor:"+iz.Letva] = Izbor{Inacica: 1, Opis: "satni lanac ugođen našim dnevnim modelom s kišom"}
	}
	// Elektrana na vrhu lanca: budućnost iz modela ispuštanja, gdje ga ima i
	// gdje je u provjeri bio bolji od zadnjeg mjerenja.
	zauzeto := map[string]bool{}
	for iz := range nasi {
		zauzeto[iz.Letva] = true
	}
	for iz, n := range BuducnostOperatera(operateri, nizovi, sada) {
		if !vrhovi[iz] || zauzeto[iz.Letva] {
			continue
		}
		r.PostaviBuducnostVrha(iz, n)
		ishod.OperaterVrhovi = append(ishod.OperaterVrhovi, iz.Letva)
		q0, _ := nizovi[iz].ZadnjiDo(sada)
		ishod.Izbor["vrh:"+iz.Letva] = Izbor{Inacica: 3, Opis: OpisOperatera(operateri[iz.Letva], q0)}
	}
	sort.Strings(ishod.OperaterVrhovi)
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
		// Mjereni su za glavni račun, u veličini u kojoj on računa; rezervi
		// ostaje rasap namještanja. Sat izdavanja je mjerenje, pa se ne ispravlja.
		for i := range izdane {
			if izbor[letva].Inacica != 0 {
				break
			}
			iz := Izvor{Letva: letva, Velicina: izdane[i].Velicina}
			p, ima := ispravakNaDosegu(promasaji[iz], izdane[i].Ciljni-sada)
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
	var izborDnevni map[string]Izbor
	ishod.Dnevne, sidra, ishod.BezDnevne, izborDnevni, ishod.Kisa = o.dnevno(ctx, sada, od)
	for k, v := range izborDnevni {
		ishod.Izbor[k] = v
	}
	// Srpske letve nasuprot i između naših: prognoza prenesena s naše.
	pi, pd, pizb := o.prenesene(ctx, ishod, od)
	ishod.Izdane = append(ishod.Izdane, pi...)
	ishod.Dnevne = append(ishod.Dnevne, pd...)
	for k, v := range pizb {
		ishod.Izbor[k] = v
	}
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
			ima[i.Letva] = true
		}
	}
	// Pregledne letve u model ne ulaze, ali ih dežurni navikao gledati uz
	// ostale; stoji njihovo mjerenje u satu izdavanja.
	for _, l := range PregledneLetve {
		if ima[l] {
			continue
		}
		n, err := o.ucitajIzmjereni(ctx, Izvor{Letva: l, Velicina: "vodostaj"}, od)
		if err != nil {
			continue
		}
		if z, imaZ := n.ZadnjiSatDo(sada); imaZ && sada-z <= ZaostatakVrha {
			v, _ := n.U(z)
			ishod.Izdane = append(ishod.Izdane, Izdana{Letva: l, Velicina: "vodostaj", Izdano: sada,
				Ciljni: sada, Vrijednost: v, Dolje: v, Gore: v, Model: o.Model})
		}
	}
	donjiRubProtoka(ishod.Izdane)
	ishod.Upozorenja = Upozori(ishod, pojasi, nizovi, vrhovi)
	return ishod, nil
}

// dnevniModeliZaDanas vraća naučene dnevne modele i letve kojima se nijedna
// inačica nije dala naučiti. Uče se iz arhive i vrijede do zagrebačke ponoći
// ili do promjene arhive; neuspjelo se ponavlja idući sat (zalihaDnevnih).
// U izdanju (kontekst iz uKrugu) zaliha se pita jednom, pa vrhovi lanca i
// dnevna prognoza računaju istim modelima.
func (o *Osvjezivac) dnevniModeliZaDanas(ctx context.Context) (map[string][]*DnevniModel, map[string]error) {
	k, ima := ctx.Value(kljucKruga{}).(*krugDnevnih)
	if !ima {
		return o.dnevniIzZalihe()
	}
	k.Do(func() { k.modeli, k.greske = o.dnevniIzZalihe() })
	return k.modeli, k.greske
}

// dnevniIzZalihe osvježi zalihu naučenih modela i vraća ih.
func (o *Osvjezivac) dnevniIzZalihe() (map[string][]*DnevniModel, map[string]error) {
	dnevniModeli.Lock()
	defer dnevniModeli.Unlock()
	dnevniModeli.osvjezi(o.Arhiva, o.Oborine, sadaUcenja())
	return dnevniModeli.modeli, dnevniModeli.greske
}

// oborinskiDosezi su koliko dana kiše unatrag i unaprijed dnevni model traži.
func oborinskiDosezi() (unatrag, unaprijed int) {
	for _, d := range OborinskiDani {
		unatrag = max(unatrag, d)
	}
	for _, d := range OborinskiDaniUnaprijed {
		unaprijed = max(unaprijed, d)
	}
	return unatrag, unaprijed
}

// vrhoviIzDnevnog računa dnevnu prognozu vrhova lanca iz VrhoviIzDnevnog i
// slaže je u satni niz za lanac, u veličini u kojoj lanac vrh vodi (vidi
// dnevniNizovi).
func (o *Osvjezivac) vrhoviIzDnevnog(ctx context.Context, sada int64, od time.Time, vrhovi map[Izvor]bool) map[Izvor]Niz {
	velicina := map[string]string{}
	for iz := range vrhovi {
		if VrhoviIzDnevnog[iz.Letva] {
			velicina[iz.Letva] = iz.Velicina
		}
	}
	return o.dnevniNizovi(ctx, sada, od, velicina, false)
}

// cvoroviIzDnevnog računa dnevnu prognozu karika s ugađanjem u lancu
// (UgadjanjeCvorova), samo modelom s kišom: bez kiše karika ostaje satni
// lanac, jer je ugađanje mjereno s kišom.
func (o *Osvjezivac) cvoroviIzDnevnog(ctx context.Context, sada int64, od time.Time) map[Izvor]Niz {
	velicina := map[string]string{}
	for letva := range UgadjanjeCvorova {
		if v, ima := Velicine[letva]; ima {
			velicina[letva] = v
		}
	}
	return o.dnevniNizovi(ctx, sada, od, velicina, true)
}

// dnevniNizovi slaže dnevnu prognozu zadanih letvi u satni niz za lanac, u
// zadanoj veličini: zadnje mjerenje letve, pa srednjaci dana smješteni u
// sredinu svojih 24 sata, pravocrtno između njih. Letvi u protoku dnevna
// prognoza vodostaja ide kroz krivulju protoka; bez krivulje takva letva niz
// ne dobiva. Uzima se prva inačica modela koja da niz.
func (o *Osvjezivac) dnevniNizovi(ctx context.Context, sada int64, od time.Time, velicina map[string]string, samoSKisom bool) map[Izvor]Niz {
	out := map[Izvor]Niz{}
	if o.Arhiva == nil || len(velicina) == 0 {
		return out
	}
	modeli, _ := o.dnevniModeliZaDanas(ctx)
	oborine := o.oborineZaDnevni(sada)
	for _, c := range DnevniCiljevi {
		if vel, treba := velicina[c.Letva]; treba {
			if n, ok := o.prviNizCilja(ctx, c, modeli[c.Letva], oborine, sada, od, vel, samoSKisom); ok {
				out[Izvor{Letva: c.Letva, Velicina: vel}] = n
			}
		}
	}
	return out
}

// oborineZaDnevni je kiša međuslivova oko sata izdavanja; kiša koja se ne da
// pročitati ne ruši račun, nego modeli s kišom ne računaju.
func (o *Osvjezivac) oborineZaDnevni(sada int64) map[string]DnevniNiz {
	unatrag, unaprijed := oborinskiDosezi()
	oborine, err := OborineOkoSada(o.Oborine, sada, unatrag, unaprijed)
	if err != nil {
		return nil
	}
	return oborine
}

// prviNizCilja je niz prve inačice dnevnog modela cilja koja ga da; s
// samoSKisom samo inačice s kišom.
func (o *Osvjezivac) prviNizCilja(ctx context.Context, c DnevniCilj, modeli []*DnevniModel, oborine map[string]DnevniNiz,
	sada int64, od time.Time, vel string, samoSKisom bool) (Niz, bool) {
	satni := o.satniCilja(ctx, c, od)
	for _, m := range modeli {
		if m == nil || (samoSKisom && len(m.Cilj.Slivovi) == 0) {
			continue
		}
		if n, ok := o.nizIzDnevnog(ctx, m, modeli, satni, oborine, sada, od, vel); ok {
			return n, true
		}
	}
	return Niz{}, false
}

// satniCilja čita satne vodostaje svih letvi koje dnevni cilj i njegove
// inačice trebaju.
func (o *Osvjezivac) satniCilja(ctx context.Context, c DnevniCilj, od time.Time) map[string]Niz {
	satni := map[string]Niz{}
	for _, in := range c.Inacice() {
		for _, l := range in.letve() {
			if _, ima := satni[l]; ima {
				continue
			}
			if n, err := o.ucitajNiz(ctx, Izvor{Letva: l, Velicina: "vodostaj"}, od); err == nil {
				satni[l] = n
			}
		}
	}
	return satni
}

// nizIzDnevnog je satni niz jedne inačice dnevnog modela (vidi dnevniNizovi);
// ok je false kad model ne računa ili ne da barem dvije točke (npr.
// pretvorba u protok ispadne iz krivulje), pa put ostaje rezervi.
func (o *Osvjezivac) nizIzDnevnog(ctx context.Context, m *DnevniModel, modeli []*DnevniModel, satni map[string]Niz,
	oborine map[string]DnevniNiz, sada int64, od time.Time, vel string) (Niz, bool) {
	d, err := PrognozirajDnevno(m, satni, oborine, sada)
	if err != nil {
		return Niz{}, false
	}
	d = SastaviBezKise(d, m, modeli, satni, oborine, sada)
	sidro, krivulje := o.sidroDnevnog(ctx, m.Cilj.Letva, satni, od, vel)
	tocke := map[int64]float64{}
	if z, ima := sidro.ZadnjiSatDo(sada); ima {
		v, _ := sidro.U(z)
		tocke[z] = v
	}
	for _, x := range d {
		if v, ok := uVeliciniDnevne(x, vel, krivulje); ok && x.Dan > 0 {
			tocke[x.Ciljni-12] = v
		}
	}
	if len(tocke) < 2 {
		return Niz{}, false
	}
	return NizIzTocaka(tocke), true
}

// sidroDnevnog je izmjereni niz letve u veličini lanca, od čijeg zadnjeg sata
// kreće niz dnevnog modela, i krivulje protoka kad lanac letvu ne vodi u
// vodostaju.
func (o *Osvjezivac) sidroDnevnog(ctx context.Context, letva string, satni map[string]Niz, od time.Time, vel string) (Niz, []models.HQKrivulja) {
	if vel == "vodostaj" {
		return satni[letva], nil
	}
	sidro := Niz{}
	if n, err := o.ucitajNiz(ctx, Izvor{Letva: letva, Velicina: vel}, od); err == nil {
		sidro = n
	}
	krivulje, _ := o.ucitajKrivulje(ctx, letva) // bez krivulje dani se ne pretvore, pa letva niz ne dobiva
	return sidro, krivulje
}

// uVeliciniDnevne je dnevna vrijednost vodostaja u veličini lanca; u protoku
// kroz krivulju koja tada vrijedi.
func uVeliciniDnevne(x DnevnaIzdana, vel string, krivulje []models.HQKrivulja) (float64, bool) {
	if vel == "vodostaj" {
		return x.Vrijednost, true
	}
	k := KrivuljaZa(krivulje, time.Unix(x.Ciljni*3600, 0).UTC())
	if k == nil {
		return 0, false
	}
	q, _, ok := pretvori(k, vel, x.Vrijednost)
	return q, ok
}

// dnevno izdaje dnevnu prognozu iz satnih očitanja do sata izdavanja.
//
// Kad glavni ulaz nema zadnja četiri dana (Borl I zna stati danima), ide
// prva rezerva koja ih ima; izbor se zapiše pod ključem „dnevni:letva”.
func (o *Osvjezivac) dnevno(ctx context.Context, sada int64, od time.Time) ([]DnevnaIzdana, []Izdana, map[string]error, map[string]Izbor, map[string]DnevniNiz) {
	bez := map[string]error{}
	izbor := map[string]Izbor{}
	if o.Arhiva == nil {
		return nil, nil, bez, izbor, nil
	}
	modeli, greske := o.dnevniModeliZaDanas(ctx)
	for l, err := range greske {
		bez[l] = err
	}
	satni := map[string]Niz{}
	unatrag, unaprijed := oborinskiDosezi()
	oborine, err := OborineOkoSada(o.Oborine, sada, unatrag, unaprijed)
	if err != nil {
		log.Printf("dnevni model: oborina uživo: %v", err)
		oborine = nil
	}
	razlogBezKise := ""
	switch {
	case o.Oborine == nil || len(o.Oborine.Tocke) == 0:
		razlogBezKise = "oborine nisu uključene (registar slivova bez kišomjera)"
	case err != nil:
		razlogBezKise = "oborine se nisu dale pročitati: " + err.Error()
	default:
		razlogBezKise = fmt.Sprintf("oborine nisu preuzete za svih %d dana unatrag i %d unaprijed", unatrag, unaprijed)
	}
	sKisom, bezKise := 0, 0
	var out []DnevnaIzdana
	for _, c := range DnevniCiljevi {
		in := modeli[c.Letva]
		if len(in) == 0 {
			continue
		}
		var prvi *DnevniModel
		for _, m := range in {
			if m != nil {
				prvi = m
				break
			}
		}
		if prvi == nil {
			continue
		}
		for _, cilj := range c.Inacice() {
			for _, l := range cilj.letve() {
				if _, ima := satni[l]; ima {
					continue
				}
				n, err := o.ucitajNiz(ctx, Izvor{Letva: l, Velicina: "vodostaj"}, od)
				if err != nil {
					continue
				}
				satni[l] = n
			}
		}
		var d []DnevnaIzdana
		var err error
		for i, m := range in {
			if m == nil {
				continue
			}
			if d, err = prognozirajInacicu(m, satni, oborine, sada, err); err == nil {
				d = SastaviBezKise(d, m, in, satni, oborine, sada)
				if len(c.Slivovi) > 0 {
					if len(m.Cilj.Slivovi) > 0 {
						sKisom++
					} else {
						bezKise++
					}
				}
				if i > 0 {
					opis := "dnevni model: " + strings.Join(m.Cilj.Ulazi, " + ")
					if m.Cilj.BezOborine() {
						opis += " bez oborine"
					}
					izbor["dnevni:"+c.Letva] = Izbor{Inacica: i, Opis: opis + " umjesto " + strings.Join(c.Ulazi, " + ") +
						map[bool]string{true: " s oborinom", false: ""}[len(c.Slivovi) > 0]}
				}
				break
			}
		}
		if err != nil {
			bez[c.Letva] = err
			continue
		}
		if d == nil {
			continue
		}
		// Protok kroz krivulju letve, kao i satna prognoza u drugoj veličini:
		// granice idu kroz krivulju, jer je ona rastuća. Granica koja iz
		// krivulje ispadne ne briše protok (rasponKrozKrivulju).
		if krivulje, err := o.ucitajKrivulje(ctx, c.Letva); err == nil && len(krivulje) > 0 {
			for i := range d {
				k := KrivuljaZa(krivulje, time.Unix(d[i].Ciljni*3600, 0).UTC())
				if k == nil {
					continue
				}
				q, _, ok := pretvori(k, "protok", d[i].Vrijednost)
				if !ok {
					continue
				}
				qd, qg, _ := rasponKrozKrivulju(k, "protok", q, d[i].Dolje, d[i].Gore)
				d[i].Q, d[i].QDolje, d[i].QGore, d[i].ImaQ = q, qd, qg, true
			}
		}
		out = append(out, d...)
	}
	// Je li dnevni model računao s kišom, mora pisati uz svako izdanje — i
	// kad je sve u redu, ne samo kad je pao na inačicu bez nje.
	switch {
	case sKisom > 0 && bezKise == 0:
		izbor["oborina"] = Izbor{Inacica: 1, Opis: fmt.Sprintf("dnevni model računa s kišom: pala kiša %d dana unatrag i prognoza %d dana unaprijed (Open-Meteo, %d %s)",
			unatrag, unaprijed, len(o.Oborine.Tocke), models.UzBrojHR(len(o.Oborine.Tocke), "kišomjer", "kišomjera", "kišomjera"))}
	case sKisom > 0:
		izbor["oborina"] = Izbor{Inacica: 1, Opis: fmt.Sprintf("dnevni model računa s kišom na %d %s, bez kiše na %d — %s",
			sKisom, models.UzBrojHR(sKisom, "letvi", "letve", "letvi"), bezKise, razlogBezKise)}
	case bezKise > 0:
		izbor["oborina"] = Izbor{Inacica: 2, Opis: "dnevni model računa bez kiše: " + razlogBezKise}
	}
	// Sidra: ulazi dnevnog modela i njegovi ciljevi koji nisu u lancu
	// (Kotoriba) dobivaju mjerenje u satu izdavanja, da na pregledu stoje.
	var sidra []Izdana
	sidrene := DnevniUlazi()
	for _, c := range DnevniCiljevi {
		sidrene = append(sidrene, c.Letva)
	}
	for _, l := range sidrene {
		n, ucitan := satni[l]
		if !ucitan {
			continue
		}
		if v, ima := n.ZadnjiDo(sada); ima {
			sidra = append(sidra, Izdana{Letva: l, Velicina: "vodostaj", Izdano: sada, Ciljni: sada,
				Vrijednost: v, Dolje: v, Gore: v, Model: ModelDnevni})
		}
	}
	return out, sidra, bez, izbor, oborine
}

// Zapisi sprema izračunato.
func (o *Osvjezivac) Zapisi(ishod *Ishod) error {
	if ishod == nil || ishod.Preskoceno || len(ishod.Izdane) == 0 {
		return nil
	}
	if o.Iznova {
		// Ponovno izdavanje gazi staro cijelo, inače bi letva koja se više
		// ne računa zadržala staru prognozu uz novo izdanje.
		if err := ObrisiIzdanje(o.Baza, ishod.Sada); err != nil {
			return err
		}
	}
	if err := SpremiIzdane(o.Baza, ishod.Izdane); err != nil {
		return err
	}
	if err := SpremiIzbor(o.Baza, ishod.Sada, ishod.Izbor); err != nil {
		return err
	}
	if err := SpremiDnevne(o.Baza, ishod.Dnevne); err != nil {
		return err
	}
	// Uz izdanje ide zapis za kalibraciju: kojim je modelom izdano, koje su
	// tuđe prognoze tada bile u rukama, kakvu je kišu dnevni model imao i
	// što pri izdavanju nije radilo.
	v, err := SpremiZapisIzdanja(o.Baza, ZapisIzdanja{Izdano: ishod.Sada, Izbor: ishod.Izbor, Kisa: ishod.Kisa, Cvor: o.Cvor,
		Upozorenja: ishod.Upozorenja})
	if err != nil {
		return fmt.Errorf("zapis o izdanju: %w", err)
	}
	ishod.Verzija = v
	return nil
}

// kljucIznova označava krug pokrenut rukom (gumb Generiraj).
type kljucIznova struct{}

// SIznova označava da se prognoza u ovom krugu računa i kad je za taj sat već
// izdana: tko pritisne Generiraj, hoće novi račun — npr. nakon popravka koda,
// kad su očitanja ista, a prognoza nije.
func SIznova(ctx context.Context) context.Context {
	return context.WithValue(ctx, kljucIznova{}, true)
}

// TraziIznova javlja je li krug pokrenut sa SIznova.
func TraziIznova(ctx context.Context) bool {
	v, _ := ctx.Value(kljucIznova{}).(bool)
	return v
}

// OdaberiInacice bira za svaku letvu kojim putem se računa u satu sada
// (nula znači: u satu najsvježijeg očitanja). Lanac se obilazi od uzvodnih
// prema nizvodnima; za kariku se uzme prva inačica čiji je svaki ulaz ili
// svjež — očitan najviše ZaostatakVrha sati prije sada — ili već sam ima svoj
// račun, pa dolazi izračunat. Kad nijedna ne prolazi, a letva sama ima
// svježe mjerenje, ona za to izdanje postaje vrh lanca: drži se zadnjeg
// mjerenja, kao svaki vrh, umjesto da nizvodne karike ostanu bez ičega.
// Kad ni toga nema, ostaje glavna, pa prognoza ide dokle ulaz seže.
func OdaberiInacice(inacice map[string][][]Pojas, nizovi map[Izvor]Niz, sada int64) (map[string][]Pojas, map[string]Izbor) {
	glavne := map[string][]Pojas{}
	for letva, in := range inacice {
		if len(in) > 0 {
			glavne[letva] = in[0]
		}
	}
	if sada == 0 {
		for _, n := range nizovi {
			if z, ima := n.Zadnji(); ima && z > sada {
				sada = z
			}
		}
	}
	svjez := func(iz Izvor) bool {
		z, ima := nizovi[iz].ZadnjiSatDo(sada)
		return ima && z+ZaostatakVrha >= sada
	}
	odabrano := map[string][]Pojas{}
	izbor := map[string]Izbor{}
	for _, letva := range Redom(glavne) {
		in := inacice[letva]
		uzeta := -1
		for i, ps := range in {
			if len(ps) == 0 || len(ps[0].Ulazi) == 0 {
				continue
			}
			prolazi := true
			for _, u := range ps[0].Ulazi {
				_, racunata := odabrano[u.Letva]
				if !racunata && !svjez(Izvor{Letva: u.Letva, Velicina: u.Velicina}) {
					prolazi = false
					break
				}
			}
			if prolazi {
				uzeta = i
				break
			}
		}
		switch {
		case uzeta > 0:
			odabrano[letva] = in[uzeta]
			izbor[letva] = Izbor{Inacica: uzeta, Opis: "rezerva: " + imenaUlaza(in[uzeta]) + " umjesto " + imenaUlaza(in[0])}
		case uzeta == 0:
			odabrano[letva] = in[0]
		case svjez(Izvor{Letva: letva, Velicina: in[0][0].Velicina}):
			// Nijedan ulaz nije svjež, a letva jest: vrh lanca za ovo izdanje.
			izbor[letva] = Izbor{Inacica: -1, Opis: "bez svježeg ulaza (" + imenaUlaza(in[0]) + "): drži se zadnje mjerenje"}
		default:
			odabrano[letva] = in[0]
		}
	}
	return odabrano, izbor
}

func imenaUlaza(ps []Pojas) string {
	if len(ps) == 0 {
		return ""
	}
	var s []string
	for _, u := range ps[0].Ulazi {
		s = append(s, u.Letva)
	}
	return strings.Join(s, " + ")
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
// model mu traži kubike. Sate koji letvi nedostaju popuni letva s druge obale
// (Zamjenske), a dulje rupe susjedi (Rekonstrukcije), prije preračuna u protok.
func (o *Osvjezivac) ucitajNiz(ctx context.Context, iz Izvor, od time.Time) (Niz, error) {
	return o.ucitajNizZ(ctx, iz, od, true)
}

// ucitajIzmjereni čita samo ono što je letva sama izmjerila, bez sati s druge
// obale — za mjerenje koje se pokazuje kao mjerenje.
func (o *Osvjezivac) ucitajIzmjereni(ctx context.Context, iz Izvor, od time.Time) (Niz, error) {
	return o.ucitajNizZ(ctx, iz, od, false)
}

func (o *Osvjezivac) ucitajNizZ(ctx context.Context, iz Izvor, od time.Time, zamjena bool) (Niz, error) {
	var krivulje []models.HQKrivulja
	if iz.Velicina == "protok" {
		var err error
		if krivulje, err = o.ucitajKrivulje(ctx, iz.Letva); err != nil {
			return Niz{}, err
		}
	}
	sirovo, err := o.ucitajSirovo(ctx, iz.Letva, od)
	if err != nil {
		return Niz{}, err
	}
	if zamjena {
		o.popuniRupe(ctx, iz.Letva, sirovo, od)
	}
	vrijednosti := map[int64]float64{}
	for sat, s := range sirovo {
		if v, ima := uVelicini(iz.Velicina, s.cm, s.q, krivulje, time.Unix(sat*3600, 0).UTC()); ima {
			vrijednosti[sat] = v
		}
	}
	return NoviNiz(vrijednosti), nil
}

// sirovoOcitanje je očitanje letve svedeno na puni sat, kako je u evidenciji.
type sirovoOcitanje struct {
	cm sql.NullInt64
	q  sql.NullFloat64
}

// ucitajSirovo čita očitanja letve po satima.
func (o *Osvjezivac) ucitajSirovo(ctx context.Context, letva string, od time.Time) (map[int64]sirovoOcitanje, error) {
	r, err := o.Ocitanja.QueryContext(ctx, `SELECT o.measured_at, o.level_cm, o.flow_m3s
		FROM readings o JOIN stations s ON s.id = o.station_id
		WHERE s.code = ? AND o.measured_at >= ?`, letva, od)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	sati := noviPoSatu[sirovoOcitanje]()
	for r.Next() {
		// Stupac je DATETIME, pa ga upravljač sam pretvara u vrijeme; čitan
		// kao tekst dolazi u drugom zapisu nego što u bazi stoji.
		var t time.Time
		var s sirovoOcitanje
		if err := r.Scan(&t, &s.cm, &s.q); err != nil {
			return nil, err
		}
		if !s.cm.Valid && !s.q.Valid {
			continue
		}
		// Na pune sate isto kao namještanje iz arhive (poSatu).
		sati.dodaj(t.Unix(), s)
	}
	return sati.vrijednosti, r.Err()
}

func uVelicini(velicina string, cm sql.NullInt64, q sql.NullFloat64,
	krivulje []models.HQKrivulja, kad time.Time) (float64, bool) {
	if velicina == "vodostaj" || velicina == "kota" {
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
//
// Dan valjanosti krivulje je zagrebački dan, kao i na stranicama: prognoza je
// dosad dan računala po UTC-u, pa je u prvom satu dana kad počinje nova
// krivulja (ljeti u prva dva sata) uzimala prethodnu. Na Donjem Miholjcu od
// 2017. to su bile tri satne vrijednosti (mjereno 13. 10. 2026.).
func KrivuljaZa(krivulje []models.HQKrivulja, kad time.Time) *models.HQKrivulja {
	d := kad.In(models.Zagreb).Format("2006-01-02")
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
		// Vrh smije kasniti do ZaostatakVrha; tada stoji njegovo zadnje
		// mjerenje, inače bi s pregleda nestao baš vrh koji vodi lanac.
		v, ima := nizovi[iz].U(sada)
		if !ima {
			if z, imaZ := nizovi[iz].ZadnjiSatDo(sada); imaZ && sada-z <= ZaostatakVrha {
				v, ima = nizovi[iz].U(z)
			}
		}
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
// Granicu koja iz krivulje ispadne određuje rasponKrozKrivulju.
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
		d, g, izvanR := rasponKrozKrivulju(k, ciljna, v, i.Dolje, i.Gore)
		n := i
		n.Velicina, n.Vrijednost, n.Dolje, n.Gore = ciljna, v, d, g
		n.Racunata = true
		n.Izvan = izvanV || izvanR
		out = append(out, n)
	}
	return out
}

// rasponKrozKrivulju vodi granice raspona kroz krivulju, oko vrijednosti v
// koja je kroz nju već prošla.
//
// Granica koja padne preko produljenja krivulje ne smije stisnuti raspon na
// vrijednost: raspon bi tada tvrdio da je prognoza sigurna upravo ondje gdje
// se krivulja nagađa. Zato granica dobiva istu udaljenost od vrijednosti kao
// ona druga, koja je prošla, a nikad nije bliže od kraja krivulje, jer je
// prava granica još dalje od njega. Pri maloj vodi svaki centimetar niže
// nosi manje vode nego onaj iznad, pa je ovako zrcaljen donji rub protoka
// niži od pravoga: raspon ispadne širi, a ne uži.
//
// Ne prođe li nijedna granica, raspona nema: obje stoje na vrijednosti.
// Protok ne ide ispod nule. Treće što se vraća javlja da je granica izašla
// iz umjerenog dijela krivulje ili iz nje cijele.
func rasponKrozKrivulju(k *models.HQKrivulja, ciljna string, v, dolje, gore float64) (float64, float64, bool) {
	krajD, krajG, _ := k.RasponProduljenja()
	d, izvanD, pravaD := granicaKrozKrivulju(k, ciljna, dolje, krajD)
	g, izvanG, pravaG := granicaKrozKrivulju(k, ciljna, gore, krajG)
	switch {
	case !pravaD && !pravaG:
		d, g = v, v
	case !pravaD:
		d = math.Min(d, v-(g-v))
	case !pravaG:
		g = math.Max(g, v+(v-d))
	}
	if ciljna == "protok" {
		d = math.Max(d, 0)
	}
	return d, g, izvanD || izvanG
}

// granicaKrozKrivulju vodi jednu granicu raspona kroz krivulju. Vraća
// granicu, je li izvan umjerenog dijela krivulje i je li stvarno prošla. Kad
// nije, vraća ono što krivulja daje na kraju produljenja s te strane (kraj,
// u cm), uz oznaku izvan.
func granicaKrozKrivulju(k *models.HQKrivulja, ciljna string, granica float64, kraj int) (float64, bool, bool) {
	if ciljna == "vodostaj" {
		// Obrat ne pada preko kraja krivulje nego na njemu stane. Je li
		// stao, vidi se po tome ide li krivulja od njega još centimetar dalje
		// na obje strane; kraj se tako ne pogađa brojem nego čita iz nje.
		cm, izvan, ok := k.Vodostaj(granica)
		if !ok {
			return float64(kraj), true, false
		}
		_, _, nize := k.ProtokProsiren(cm - 1)
		_, _, vise := k.ProtokProsiren(cm + 1)
		return float64(cm), izvan, nize && vise
	}
	if q, izvan, ok := k.ProtokProsiren(int(math.Round(granica))); ok {
		return q, izvan, true
	}
	if q, _, ok := k.ProtokProsiren(kraj); ok {
		return q, true, false
	}
	return 0, true, false
}

// donjiRubProtoka podiže donji rub raspona protoka na nulu. Raspon je iste
// širine pri maloj vodi kao i pri većoj, pa oko male vode zna pasti ispod
// nule, a protoka ondje nema — ne daje ga ni produljena krivulja
// (ProtokProsiren). Sama vrijednost se ne dira.
func donjiRubProtoka(izdane []Izdana) {
	for i := range izdane {
		if izdane[i].Velicina == "protok" && izdane[i].Dolje < 0 && izdane[i].Vrijednost >= 0 {
			izdane[i].Dolje = 0
		}
	}
}

// Pretvori vodi jednu vrijednost kroz krivulju u traženu veličinu (vodostaj
// ili protok); izvan javlja da je krivulja produljena preko mjerenog.
func Pretvori(k *models.HQKrivulja, ciljna string, v float64) (vrijednost float64, izvan, ok bool) {
	return pretvori(k, ciljna, v)
}

// pretvori vodi jednu vrijednost kroz krivulju u traženu veličinu.
// UProtok preračuna vodostaj i granice raspona u protok krivuljom, istim
// pravilom kao satni lanac; ok je false kad vrijednost ne prođe krivulju.
func UProtok(k *models.HQKrivulja, v, dolje, gore float64) (q, qd, qg float64, ok bool) {
	q, _, ok = pretvori(k, "protok", v)
	if !ok {
		return 0, 0, 0, false
	}
	qd, qg, _ = rasponKrozKrivulju(k, "protok", q, dolje, gore)
	return q, qd, qg, true
}

func pretvori(k *models.HQKrivulja, ciljna string, v float64) (float64, bool, bool) {
	if ciljna == "vodostaj" {
		cm, izvan, ok := k.Vodostaj(v)
		return float64(cm), izvan, ok
	}
	q, izvan, ok := k.ProtokProsiren(int(math.Round(v)))
	return q, izvan, ok
}

// ispravakNaDosegu vraća zapisani promašaj za doseg. Sat izdavanja (doseg 0)
// je mjerenje i nema ispravka, pa prognoza kreće točno od izmjerenog.
func ispravakNaDosegu(promasaji map[int]Promasaj, doseg int64) (Promasaj, bool) {
	if doseg == 0 {
		return Promasaj{}, false
	}
	p, ima := promasaji[int(doseg)]
	return p, ima
}
