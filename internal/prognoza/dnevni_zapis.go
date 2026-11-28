package prognoza

// Dnevni model letve iz zapisa (plan uređivanja modela, PR 11). Rječicu koju diže
// kiša s vlastitog sliva satni račun iz uzvodne letve ne pogađa; njoj se u zapisu
// bira metoda „dnevni”: uzvodne letve i međuslivovi čija kiša ulazi u račun.
// Brojeva u zapisu nema: model se, kao i dnevni modeli iz programa, uči iz
// arhive jednom dnevno na računalu koje izdaje.
//
// U izdanju se takve letve računaju nakon svega ostaloga, u vlastitoj gorutini s
// rokom. Pad, panika ili spora arhiva ostaju ovdje: glavni račun je već gotov.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// MetodaDnevni je metoda zapisa koji letvu računa dnevnim modelom.
const MetodaDnevni = "dnevni"

// DnevniLetve je blok „dnevni” zapisa: iz čega se uči dnevni model letve.
type DnevniLetve struct {
	Ulazi   []string `json:"ulazi"`   // uzvodne letve i letve čiji uspor diže vodu
	Slivovi []string `json:"slivovi"` // međuslivovi čija kiša ulazi u račun
	// OdDana i OdDanaNiska su dan od kojeg na pregledu vrijednost daje dnevni
	// model, pri visokoj te pri niskoj i srednjoj vodi, a VisokaVoda vodostaj
	// (cm) koji ih dijeli. Vrijede samo letvama dnevnog modela Drave i Dunava;
	// prazno ostavlja ono iz programa.
	OdDana      *int     `json:"od_dana,omitempty"`
	OdDanaNiska *int     `json:"od_dana_niska,omitempty"`
	VisokaVoda  *float64 `json:"visoka_voda,omitempty"`
}

// DnevniBlok čita blok „dnevni” zapisa.
func (z ZapisLetve) DnevniBlok() (DnevniLetve, error) {
	var d DnevniLetve
	if len(z.Dnevni) == 0 || string(z.Dnevni) == "null" {
		return d, errors.New("zapis nema dnevni model")
	}
	if err := json.Unmarshal(z.Dnevni, &d); err != nil {
		return d, errors.New("dnevni model u zapisu se ne da pročitati")
	}
	return d, nil
}

// DnevniCiljZapisa je cilj dnevnog modela složen iz zapisa.
func (z ZapisLetve) DnevniCiljZapisa() (DnevniCilj, error) {
	d, err := z.DnevniBlok()
	return DnevniCilj{Letva: z.Letva, Ulazi: d.Ulazi, Slivovi: d.Slivovi, NajmanjeDana: NajmanjeDanaZapisa}, err
}

// provjeriDnevni odbija zapis dnevnog modela koji se ne smije računati.
func (z ZapisLetve) provjeriDnevni(o OkvirModela) error {
	if err := z.provjeriGlavuDnevnog(); err != nil {
		return err
	}
	d, err := z.DnevniBlok()
	if err != nil {
		return err
	}
	if err := d.provjeri(z.Letva); err != nil {
		return err
	}
	if err := z.provjeriMjesto(o); err != nil {
		return err
	}
	if o.ImaSektor != nil && !o.ImaSektor(z.Letva) {
		return errors.New("letva nema sektor; upiši ga u obrascu letve")
	}
	return nil
}

// provjeriGlavuDnevnog gleda ono bez čega se zapis ne smije ni tumačiti.
func (z ZapisLetve) provjeriGlavuDnevnog() error {
	switch {
	case z.Oblik > OblikModelaLetve:
		return errors.New("zapis modela je iz novije inačice programa")
	case z.Oblik < 1:
		return errors.New("zapis modela nema oblik")
	case z.Letva == "":
		return errors.New("zapis modela nema letvu")
	}
	return nil
}

func (d DnevniLetve) provjeri(letva string) error {
	svi := slices.Concat(d.Ulazi, d.Slivovi)
	switch {
	case len(svi) == 0:
		return errors.New("dnevni model nema ni ulaz ni sliv; izaberi barem jedno")
	case max(len(d.Ulazi), len(d.Slivovi)) > najviseUlaza:
		return fmt.Errorf("dnevni model ima najviše %d ulaza i %d slivova", najviseUlaza, najviseUlaza)
	case slices.Contains(svi, ""):
		return errors.New("dnevni model ima prazan ulaz ili sliv")
	case slices.Contains(d.Ulazi, letva):
		return errors.New("letva ne može biti sama sebi ulaz")
	case imaPonavljanja(d.Ulazi), imaPonavljanja(d.Slivovi):
		return errors.New("isti ulaz ili sliv naveden je dvaput; makni jedan")
	}
	return nil
}

// imaPonavljanja javlja stoji li ista šifra na popisu dvaput.
func imaPonavljanja(sifre []string) bool {
	return len(slices.Compact(slices.Sorted(slices.Values(sifre)))) != len(sifre)
}

// naucenZapis je naučeni dnevni model jedne letve iz zapisa i ono po čemu se
// zna vrijedi li još: dan, otisak arhive i definicija.
type naucenZapis struct {
	dan        string
	zig        zigArhive
	definicija string
	sat        int64 // sat zadnjeg pokušaja: neuspjelo se ponavlja tek idući sat
	model      *DnevniModel
	greska     error
}

// zalihaZapisa drži naučene dnevne modele letvi iz zapisa.
var zalihaZapisa = struct {
	sync.Mutex
	po map[string]naucenZapis
}{po: map[string]naucenZapis{}}

// ZaboraviDnevneZapisa baca naučene dnevne modele letvi iz zapisa.
func ZaboraviDnevneZapisa() {
	zalihaZapisa.Lock()
	defer zalihaZapisa.Unlock()
	zalihaZapisa.po = map[string]naucenZapis{}
}

// dnevniModelZapisa vraća naučeni model cilja. Uči se iz arhive kad ga nema, kad
// je počeo novi dan, kad se arhiva ili definicija promijenila; neuspjelo učenje
// ponavlja se najviše jednom na sat.
func (o *Osvjezivac) dnevniModelZapisa(c DnevniCilj, sada time.Time) (*DnevniModel, error) {
	def, _ := json.Marshal(c) // popis šifri se uvijek da zapisati
	dan, zig, sat := danUcenja(sada), otisakArhive(o.Arhiva), sada.Unix()/3600
	zalihaZapisa.Lock()
	n, ima := zalihaZapisa.po[c.Letva]
	zalihaZapisa.Unlock()
	svjez := ima && n.dan == dan && !n.zig.promijenjen(zig) && n.definicija == string(def)
	if svjez && (n.model != nil || n.sat == sat) {
		return n.model, n.greska
	}
	n = naucenZapis{dan: dan, zig: zig, definicija: string(def), sat: sat}
	n.model, n.greska = o.nauciDnevniZapisa(c)
	zalihaZapisa.Lock()
	zalihaZapisa.po[c.Letva] = n
	zalihaZapisa.Unlock()
	return n.model, n.greska
}

// nauciDnevniZapisa uči dnevni model cilja iz arhive, na svim danima.
func (o *Osvjezivac) nauciDnevniZapisa(c DnevniCilj) (*DnevniModel, error) {
	var tocke []OborinskaTocka
	if o.Oborine != nil {
		tocke = slices.Concat(o.Oborine.Tocke, o.Oborine.Kisomjeri)
	}
	if len(c.Slivovi) > 0 && len(tocke) == 0 {
		return nil, errors.New("oborine nisu uključene, a dnevni model letve računa s kišom")
	}
	nizovi, err := DnevniNizoviCilja(o.Arhiva, tocke, c)
	if err != nil {
		return nil, err
	}
	return NamjestiDnevni(nizovi, c, 0)
}

// izvodDnevnihZapisa je što je dnevni krug letvi iz zapisa dao.
type izvodDnevnihZapisa struct {
	dnevne []DnevnaIzdana
	sidra  []Izdana
	bez    map[string]error
	greska error
}

// dnevniKrugZapisa računa letve čiji zapis bira dnevni model i dodaje ih izdanju.
// Letvu koja u izdanju već ima dnevne vrijednosti ne dira.
func (o *Osvjezivac) dnevniKrugZapisa(ctx context.Context, ishod *Ishod, inacice map[string][][]Pojas, od time.Time) {
	if o.Arhiva == nil || o.Baza == nil {
		return
	}
	ciljevi, bez := dnevniCiljeviZapisa(o.Baza, OkvirModela{Glavne: inacice,
		ImaSektor: func(letva string) bool { return LetvaImaSektor(ctx, o.Ocitanja, letva) }})
	if len(ciljevi)+len(bez) == 0 {
		return
	}
	ciljevi = bezVecIzdanih(ciljevi, ishod.Dnevne)
	pocetak := time.Now()
	rok, otkazi := context.WithTimeout(ctx, RokOdvojenih)
	defer otkazi()
	gotovo := make(chan izvodDnevnihZapisa, 1)
	go func() { gotovo <- o.racunDnevnihZapisa(rok, ciljevi, ishod.Sada, od) }()
	ishod.primiDnevneZapisa(docekajDnevneZapisa(rok, gotovo), bez)
	log.Printf("prognoza: dnevni modeli iz zapisa, %d letvi, %.1f s", len(ciljevi), time.Since(pocetak).Seconds())
}

// bezVecIzdanih su ciljevi čija letva u izdanju još nema dnevne vrijednosti.
func bezVecIzdanih(ciljevi []DnevniCilj, dnevne []DnevnaIzdana) []DnevniCilj {
	ima := map[string]bool{}
	for _, x := range dnevne {
		ima[x.Letva] = true
	}
	return slices.DeleteFunc(ciljevi, func(c DnevniCilj) bool { return ima[c.Letva] })
}

// docekajDnevneZapisa čeka račun do roka. Račun koji je stigao baš kad je rok
// prošao možda je ostao bez dijela čitanja, pa se ni on ne uzima.
func docekajDnevneZapisa(rok context.Context, gotovo <-chan izvodDnevnihZapisa) izvodDnevnihZapisa {
	var izvod izvodDnevnihZapisa
	select {
	case izvod = <-gotovo:
	case <-rok.Done():
	}
	if rok.Err() != nil {
		return izvodDnevnihZapisa{greska: fmt.Errorf("računanje traje dulje od %d s", int(RokOdvojenih.Seconds()))}
	}
	return izvod
}

// dnevniCiljeviZapisa čita zapise s dnevnim modelom: ciljeve koji smiju računati,
// redom šifri, i letve čiji zapis ne prolazi provjeru, s razlogom.
func dnevniCiljeviZapisa(baza *sql.DB, o OkvirModela) ([]DnevniCilj, map[string]error) {
	bez := map[string]error{}
	zapisi, _, err := ZapisiLetvi(baza)
	if err != nil {
		return nil, bez
	}
	var letve []string
	for l, z := range zapisi {
		// letvu dnevnog modela Drave i Dunava računa glavni dnevni model
		if z.Metoda == MetodaDnevni && !GlavniDnevniCilj(l) {
			letve = append(letve, l)
		}
	}
	slices.Sort(letve)
	var out []DnevniCilj
	for _, letva := range letve {
		z := zapisi[letva]
		if err := z.Provjeri(o); err != nil {
			bez[letva] = err
			continue
		}
		c, _ := z.DnevniCiljZapisa() // Provjeri je blok već pročitao
		out = append(out, c)
	}
	return out, bez
}

// racunDnevnihZapisa uči (po potrebi) i izdaje dnevnu prognozu svakog cilja.
func (o *Osvjezivac) racunDnevnihZapisa(ctx context.Context, ciljevi []DnevniCilj, sada int64, od time.Time) (izvod izvodDnevnihZapisa) {
	defer func() {
		if e := recover(); e != nil {
			log.Printf("prognoza: dnevni modeli iz zapisa: %v", e)
			izvod = izvodDnevnihZapisa{greska: fmt.Errorf("greška u računu: %v", e)}
		}
	}()
	izvod.bez = map[string]error{}
	oborine := o.oborineZaDnevni(sada)
	satni := map[string]Niz{}
	for _, c := range ciljevi {
		if ctx.Err() != nil {
			return izvod
		}
		d, err := o.izdajDnevniZapisa(ctx, c, satni, oborine, sada, od)
		if err != nil {
			izvod.bez[c.Letva] = err
			continue
		}
		izvod.dnevne = append(izvod.dnevne, d...)
		if v, ima := satni[c.Letva].ZadnjiDo(sada); ima {
			izvod.sidra = append(izvod.sidra, Izdana{Letva: c.Letva, Velicina: "vodostaj", Izdano: sada, Ciljni: sada,
				Vrijednost: v, Dolje: v, Gore: v, Model: ModelDnevni})
			// i protok sada, kroz krivulju, da uz vodostaj stoji i m³/s
			sad := []DnevnaIzdana{{Letva: c.Letva, Ciljni: sada, Vrijednost: v, Dolje: v, Gore: v}}
			if o.protokKrozKrivulju(ctx, c.Letva, sad); sad[0].ImaQ {
				izvod.sidra = append(izvod.sidra, Izdana{Letva: c.Letva, Velicina: "protok", Izdano: sada, Ciljni: sada,
					Vrijednost: sad[0].Q, Dolje: sad[0].Q, Gore: sad[0].Q, Model: ModelDnevni})
			}
		}
	}
	return izvod
}

// izdajDnevniZapisa nauči model cilja ako treba, učita satna očitanja njegovih
// letvi u satni (jednom po letvi) i vrati dnevnu prognozu.
func (o *Osvjezivac) izdajDnevniZapisa(ctx context.Context, c DnevniCilj, satni map[string]Niz, oborine map[string]DnevniNiz, sada int64, od time.Time) ([]DnevnaIzdana, error) {
	m, err := o.dnevniModelZapisa(c, sadaUcenja())
	if err != nil {
		return nil, err
	}
	for _, l := range c.letve() {
		if _, ima := satni[l]; !ima {
			n, err := o.ucitajNiz(ctx, Izvor{Letva: l, Velicina: "vodostaj"}, od)
			_ = err // niz koji se ne da pročitati ostaje prazan, pa prognoza javi što fali
			satni[l] = n
		}
	}
	d, err := PrognozirajDnevno(m, satni, oborine, sada)
	if err == nil {
		o.protokKrozKrivulju(ctx, c.Letva, d) // Bračevci imaju krivulju, pa i protok
	}
	return d, err
}

// primiDnevneZapisa dodaje izdanju dnevne vrijednosti letvi iz zapisa, njihovo
// mjerenje u satu izdavanja i razloge zašto neka letva nije računala.
func (i *Ishod) primiDnevneZapisa(izvod izvodDnevnihZapisa, neispravni map[string]error) {
	if i.BezDnevne == nil {
		i.BezDnevne = map[string]error{}
	}
	maps.Copy(i.BezDnevne, neispravni)
	if izvod.greska != nil {
		i.Upozorenja = append(i.Upozorenja, Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaUpozorenje,
			Opis: "Letve s dnevnim modelom iz zapisa nisu računate u ovom izdanju: " + izvod.greska.Error()})
		poredajUpozorenja(i.Upozorenja)
		return
	}
	i.Dnevne = append(i.Dnevne, izvod.dnevne...)
	i.dodajMjerenja(izvod.sidra)
	maps.Copy(i.BezDnevne, izvod.bez)
	if n := upozorenjaDnevnihZapisa(neispravni, izvod.bez); len(n) > 0 {
		i.Upozorenja = append(i.Upozorenja, n...)
		poredajUpozorenja(i.Upozorenja)
	}
}

// upozorenjaDnevnihZapisa kažu za svaku letvu s dnevnim modelom iz zapisa
// zašto u izdanju nema dnevnu prognozu. Upozorenja glavnog dnevnog modela
// idu samo po njegovim ciljevima, pa bi ovakva letva s pregleda nestala bez
// riječi: Vuka 25. 11. 2026., kad je javna stranica postaje ugašena.
// Napomena, kao i ostalo o letvama iz zapisa: rječica ne zauzima traku na
// vrhu stranice.
func upozorenjaDnevnihZapisa(bez ...map[string]error) []Upozorenje {
	var out []Upozorenje
	for _, m := range bez {
		for _, l := range slices.Sorted(maps.Keys(m)) {
			out = append(out, Upozorenje{Vrsta: VrstaOdvojeni, Tezina: TezinaNapomena, Letva: l,
				Opis: "nema dnevne prognoze" + razlogDnevnogZapisa(l, m[l])})
		}
	}
	return out
}

// razlogDnevnogZapisa je nastavak rečenice „nema dnevne prognoze”: kojoj
// letvi fale mjerenja i od kada, a za ostale greške tekst greške. Šifre
// letvi stoje kao zasebne riječi, da ih stranica zamijeni imenima.
func razlogDnevnogZapisa(letva string, err error) string {
	var nd *NepotpuniDani
	if !errors.As(err, &nd) || len(nd.Fali) == 0 {
		return ": " + razlogBezDnevne(letva, err)
	}
	dijelovi := make([]string, 0, len(nd.Fali))
	for _, f := range nd.Fali {
		if sliv, kisa := strings.CutPrefix(f, "oborina "); kisa {
			dijelovi = append(dijelovi, "nema kiše sliva "+sliv)
			continue
		}
		r, _ := rupaLetve(nd.Rupe, f)
		dijelovi = append(dijelovi, cijaRupa(letva, f)+rupaRijecima(r))
	}
	return ", jer " + strings.Join(dijelovi, "; ")
}

// cijaRupa imenuje letvu kojoj fale mjerenja kad to nije sama letva modela.
func cijaRupa(letva, fali string) string {
	if fali == letva {
		return ""
	}
	return "letva " + fali + " "
}

// rupaRijecima kaže od kada mjerenja nema. Rupa koja je završila i dalje
// ruši prognozu dok ne izađe iz dana iz kojih model računa, pa to i piše.
func rupaRijecima(r RupaUlaza) string {
	switch {
	case r.Zadnje > 0 && r.Sljedece > 0:
		return "nema mjerenja od " + satDnevnika(r.Zadnje) + " do " + satDnevnika(r.Sljedece) +
			", a model traži potpune dane"
	case r.Zadnje > 0:
		return "nema mjerenja od " + satDnevnika(r.Zadnje)
	}
	return "nema dovoljno mjerenja u zadnja četiri dana"
}

// dodajMjerenja dodaje izdanju mjerenje u satu izdavanja za letve koje ga još
// nemaju, da stoje na pregledu.
func (i *Ishod) dodajMjerenja(sidra []Izdana) {
	ima := map[string]bool{}
	for _, x := range i.Izdane {
		ima[x.Letva] = true
	}
	for _, s := range sidra {
		if !ima[s.Letva] {
			i.Izdane = append(i.Izdane, s)
		}
	}
}
