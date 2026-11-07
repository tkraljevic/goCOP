package prognoza

// Zapis modela po letvi: tablica model_letve, jedan redak po letvi. Letva bez
// retka radi kao dosad. Letva koja nema račun u glavnom lancu računa se iz
// retka, odvojeno od glavnog računa (dodatniKrug), a brojevi joj stoje u retku,
// ne u tablicama pojasi i ulazi. Pripremi model te tablice briše cijele
// (Spremi), pa bi račun u njima nestao, a jedan zaboravljen filtar vratio bi vrh
// bez očitanja u ZadnjiZajednicki.
//
// Karašica, Vučica i Bednja računaju se istim putem, a nemaju redak: račun im je
// u kodu (OdvojeniRacuni), a brojevi u istim tablicama kao i dosad. Redak vrijedi
// umjesto koda.
//
// Isti se zapis provjerava pri spremanju i pri svakom čitanju: pokvaren redak
// isključuje samo svoju letvu i ono što iz nje računa, a glavni račun ide dalje.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// OblikModelaLetve je najnoviji oblik zapisa koji ovaj program zna pročitati.
const OblikModelaLetve = 1

// MetodaLanac je jedina metoda računa koju zapis zasad smije izabrati.
const MetodaLanac = "lanac"

// Granice brojeva u zapisu. Račun se bez njih ruši na brojevima koje JSON i
// SQLite prime: nagib 1e308 daje beskonačnost pri upisu izdanja, a rasap 1e200
// pri slanju razmjenom. Granice su široke, da ne stanu na put pravom modelu.
const (
	najveciDosegZapisa  = 96    // sati; toliko seže i izdanje
	najveceKasnjenje    = 240   // sati; dublje ne seže ni račun (Najranije)
	najveciNagib        = 1_000 // cm po m³/s i obrnuto
	najvecaVrijednost   = 1e6   // cm ili m³/s: granice razina, odsječak, rasap, pomak
	najvecaIzdana       = 1e7   // najveći broj koji račun smije izdati
	najviseRazinaZapisa = 20
	najviseInacica      = 10
	najviseUlaza        = 10
)

// ZapisLetve je jedan redak tablice model_letve.
type ZapisLetve struct {
	Oblik    int             `json:"oblik"`
	Letva    string          `json:"letva"`
	Metoda   string          `json:"metoda"`
	Velicina string          `json:"velicina"`
	Racun    RacunLetve      `json:"racun"`
	Dnevni   json.RawMessage `json:"dnevni"`
	Brojevi  BrojeviLetve    `json:"brojevi"`
	Promjene []PromjenaLetve `json:"promjene"`
	// Prethodni je jedna prethodna inačica zapisa, kako je stajala.
	Prethodni json.RawMessage `json:"prethodni"`
	// Ugradjen je račun iz koda (OdvojeniRacuni), bez retka u tablici. Računao je
	// i prije ovih provjera, pa mu sektor u registru nije uvjet, a u satu izdanja
	// radi kao u glavnom lancu (izdaneLetve, sidraOdvojenih).
	Ugradjen bool `json:"-"`
}

// RacunLetve je definicija računa: ono što stručnjak složi.
type RacunLetve struct {
	Ulazi   []string    `json:"ulazi"`
	Rezerve [][]string  `json:"rezerve"`
	Doseg   int         `json:"doseg"` // dokle se računa, u satima
	Razine  RazineLetve `json:"razine"`
	Rucno   RucnoLetve  `json:"rucno"`
}

// RazineLetve kažu po kojim se razinama vode računa: postoci ili granice.
type RazineLetve struct {
	Postoci    []float64 `json:"postoci"`
	Granice    []float64 `json:"granice"`
	Iskljucene []int     `json:"iskljucene"`
}

// RucnoLetve su vrijednosti koje je stručnjak sam upisao umjesto namještenih.
type RucnoLetve struct {
	Kasnjenje []RucnoKasnjenje `json:"kasnjenje"`
	Nagib     *float64         `json:"nagib"`
}

// RucnoKasnjenje je upisano vrijeme propagacije jedne razine.
type RucnoKasnjenje struct {
	Razina int `json:"razina"`
	Sati   int `json:"sati"`
}

// BrojeviLetve su brojevi računa: namješteni pojasi po inačici (0 glavna, dalje
// rezerve), promašaji po dosegu i razdoblje na kojem su mjereni.
type BrojeviLetve struct {
	Inacice   [][]Pojas        `json:"inacice"`
	Promasaji map[int]Promasaj `json:"promasaji"`
	Razdoblje []string         `json:"razdoblje"`
}

// PromjenaLetve je jedna izmjena zapisa: tko, kad i zašto.
type PromjenaLetve struct {
	Kad    string `json:"kad"`
	Tko    string `json:"tko"`
	Razlog string `json:"razlog"`
}

// OkvirModela je ono što provjera zapisa mora znati o ostatku programa.
type OkvirModela struct {
	// Glavne su inačice računa glavnog lanca iz baze prognoza.
	Glavne map[string][][]Pojas
	// UIzdanju su letve koje u izdanju već imaju retke glavnog računa.
	UIzdanju map[string]bool
	// ImaSektor i ImaSatniProtok čitaju registar i arhivu; nil se ne provjerava.
	ImaSektor      func(letva string) bool
	ImaSatniProtok func(letva string) bool
}

// ZapisiLetvi čita sve retke modela. Redak koji se ne da pročitati vraća se
// među pokvarene, s razlogom, a ostali ostaju. Baza nastala prije tablice je
// prazan zapis: stranica čita bazu samo za čitanje, pa je shema ne dograđuje.
func ZapisiLetvi(db *sql.DB) (map[string]ZapisLetve, map[string]error, error) {
	zapisi, pokvareni := map[string]ZapisLetve{}, map[string]error{}
	var ima int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'model_letve'`).Scan(&ima); err != nil || ima == 0 {
		return zapisi, pokvareni, err
	}
	r, err := db.Query(`SELECT letva, sadrzaj FROM model_letve`)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	for r.Next() {
		var letva, sadrzaj string
		if err := r.Scan(&letva, &sadrzaj); err != nil {
			return nil, nil, err
		}
		if z, err := procitajZapis(letva, sadrzaj); err != nil {
			pokvareni[letva] = err
		} else {
			zapisi[letva] = z
		}
	}
	return zapisi, pokvareni, r.Err()
}

// zapisiZaRacun su zapisi koje odvojeni račun računa: redci tablice i računi iz
// koda. Redak vrijedi umjesto koda, i kad je pokvaren.
func zapisiZaRacun(db *sql.DB, promasaji map[Izvor]map[int]Promasaj) (map[string]ZapisLetve, map[string]error, error) {
	zapisi, pokvareni, err := ZapisiLetvi(db)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range OdvojeniRacuni {
		_, imaRedak := zapisi[r.Letva]
		_, pokvaren := pokvareni[r.Letva]
		if imaRedak || pokvaren {
			continue
		}
		z, namjesten, err := ugradjeniZapis(db, r, promasaji)
		if err != nil {
			return nil, nil, err
		}
		if namjesten {
			zapisi[r.Letva] = z
		}
	}
	return zapisi, pokvareni, nil
}

// ugradjeniZapis složi zapis računa iz koda od brojeva u bazi. Račun koji se nije
// dao namjestiti ne računa se, kao ni dosad. Promašaj preko dosega izdanja, koji
// provjera unatrag smije zapisati, ne koristi se, pa zbog njega letva ne ispada.
func ugradjeniZapis(db *sql.DB, r Racun, promasaji map[Izvor]map[int]Promasaj) (ZapisLetve, bool, error) {
	in, err := InaciceLetve(db, r.Letva)
	if err != nil || len(in) == 0 {
		return ZapisLetve{}, false, err
	}
	vel := in[0][0].Velicina
	return ZapisLetve{Oblik: OblikModelaLetve, Letva: r.Letva, Metoda: MetodaLanac, Velicina: vel, Ugradjen: true,
		Racun:   RacunLetve{Ulazi: r.Ulazi, Rezerve: Rezerve[r.Letva], Doseg: najveciDosegZapisa},
		Brojevi: BrojeviLetve{Inacice: in, Promasaji: doDosegaZapisa(promasaji[Izvor{Letva: r.Letva, Velicina: vel}])}}, true, nil
}

// doDosegaZapisa su promašaji do dosega izdanja.
func doDosegaZapisa(p map[int]Promasaj) map[int]Promasaj {
	out := maps.Clone(p)
	maps.DeleteFunc(out, func(doseg int, _ Promasaj) bool { return doseg > najveciDosegZapisa })
	return out
}

// procitajZapis pretvara sadržaj retka u zapis.
func procitajZapis(letva, sadrzaj string) (ZapisLetve, error) {
	var z ZapisLetve
	if err := json.Unmarshal([]byte(sadrzaj), &z); err != nil {
		return z, errors.New("zapis modela se ne da pročitati")
	}
	if z.Letva != letva {
		return z, fmt.Errorf("zapis modela nosi drugu letvu (%s)", z.Letva)
	}
	return z, nil
}

// SpremiZapisLetve upisuje zapis letve, ako prođe provjere i ako zbog njega ne
// ispada nijedna letva koja je dosad računala.
func SpremiZapisLetve(db *sql.DB, z ZapisLetve, proba string, o OkvirModela) error {
	zapisi, pokvareni, err := zapisiZaRacun(db, nil)
	if err != nil {
		return err
	}
	if err := provjeriUpis(zapisi, pokvareni, z, o); err != nil {
		return err
	}
	sadrzaj, err := json.Marshal(z)
	if err != nil {
		return fmt.Errorf("%s: zapis modela: %w", z.Letva, err)
	}
	_, err = db.Exec(`INSERT INTO model_letve (letva, proba, sadrzaj) VALUES (?, ?, ?)
		ON CONFLICT(letva) DO UPDATE SET proba = excluded.proba, sadrzaj = excluded.sadrzaj`, z.Letva, proba, string(sadrzaj))
	return err
}

// provjeriUpis javlja bi li upis zapisa isključio njegovu letvu ili neku koja je
// dotad računala. Zapisi su i računi iz koda (zapisiZaRacun), jer i kroz njih ide
// krug. Zadane popise ne mijenja.
func provjeriUpis(zapisi map[string]ZapisLetve, pokvareni map[string]error, z ZapisLetve, o OkvirModela) error {
	prije := ProvjeriZapise(zapisi, pokvareni, o)
	sa := maps.Clone(zapisi)
	sa[z.Letva] = z
	bez := maps.Clone(pokvareni)
	delete(bez, z.Letva)
	return noveIskljucene(ProvjeriZapise(sa, bez, o), prije, z.Letva)
}

// noveIskljucene javlja je li spremanje isključilo spremanu letvu ili neku koja
// je dotad računala.
func noveIskljucene(sad, prije map[string]error, letva string) error {
	if err, ima := sad[letva]; ima {
		return err
	}
	for _, l := range slices.Sorted(maps.Keys(sad)) {
		if _, bila := prije[l]; !bila {
			return fmt.Errorf("letva %s tada ne bi mogla računati: %w", l, sad[l])
		}
	}
	return nil
}

// ProvjeriZapise vraća letve koje se isključuju, s razlogom: pokvarene, one koje
// ne prolaze Provjeri, one u krugu ili s krivom veličinom veze i sve koje iz
// isključene računaju.
func ProvjeriZapise(zapisi map[string]ZapisLetve, pokvareni map[string]error, o OkvirModela) map[string]error {
	isk := make(map[string]error, len(pokvareni))
	maps.Copy(isk, pokvareni)
	for l, z := range zapisi {
		if err := z.Provjeri(o); err != nil {
			isk[l] = err
		}
	}
	dobri := ostaliZapisi(zapisi, isk)
	for _, l := range kruzniRacuni(dobri) {
		isk[l] = errors.New("račun se vrti u krug kroz ulaze ili rezerve")
	}
	provjeriVeze(ostaliZapisi(zapisi, isk), o.Glavne, isk)
	iskljuciNizvodne(zapisi, isk, o)
	return isk
}

// ostaliZapisi su zapisi bez isključenih letvi.
func ostaliZapisi(zapisi map[string]ZapisLetve, isk map[string]error) map[string]ZapisLetve {
	out := make(map[string]ZapisLetve, len(zapisi))
	for l, z := range zapisi {
		if _, ispala := isk[l]; !ispala {
			out[l] = z
		}
	}
	return out
}

// kruzniRacuni su letve koje kroz ijednu inačicu računaju same iz sebe. Račun
// to ne hvata: s kašnjenjem od sat vremena svaki je sat novi korak, pa krug
// stane tek na dnu niza, s prognozom koja izgleda valjano.
func kruzniRacuni(zapisi map[string]ZapisLetve) []string {
	k := obilazakKruga{zapisi: zapisi, stanje: map[string]int{}, uKrugu: map[string]bool{}}
	for _, l := range slices.Sorted(maps.Keys(zapisi)) {
		k.obidi(l)
	}
	return slices.Sorted(maps.Keys(k.uKrugu))
}

type obilazakKruga struct {
	zapisi map[string]ZapisLetve
	stanje map[string]int // 1 na putu, 2 gotova
	put    []string
	uKrugu map[string]bool
}

func (k *obilazakKruga) obidi(letva string) {
	switch k.stanje[letva] {
	case 1:
		for _, l := range k.put[slices.Index(k.put, letva):] {
			k.uKrugu[l] = true
		}
		return
	case 2:
		return
	}
	k.stanje[letva] = 1
	k.put = append(k.put, letva)
	for _, u := range k.zapisi[letva].letveUlaza() {
		k.obidi(u)
	}
	k.put = k.put[:len(k.put)-1]
	k.stanje[letva] = 2
}

// letveUlaza su sve letve iz kojih zapis računa, u svim inačicama.
func (z ZapisLetve) letveUlaza() []string {
	out := slices.Clone(z.Racun.Ulazi)
	for _, r := range z.Racun.Rezerve {
		out = append(out, r...)
	}
	for _, ps := range z.Brojevi.Inacice {
		for _, p := range ps {
			for _, u := range p.Ulazi {
				out = append(out, u.Letva)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// provjeriVeze isključuje letve koje ulaz traže u veličini u kojoj se on ne
// računa. Račun bi takav ulaz tiho uzeo kao vrh lanca i držao mu zadnje
// mjerenje (Racunalo.U).
func provjeriVeze(zapisi map[string]ZapisLetve, glavne map[string][][]Pojas, isk map[string]error) {
	for l, z := range zapisi {
		for _, ps := range z.Brojevi.Inacice {
			for _, p := range ps {
				if err := provjeriVezePojasa(p, zapisi, glavne); err != nil {
					isk[l] = err
				}
			}
		}
	}
}

func provjeriVezePojasa(p Pojas, zapisi map[string]ZapisLetve, glavne map[string][][]Pojas) error {
	for _, u := range p.Ulazi {
		if v, ima := velicinaRacuna(u.Letva, zapisi, glavne); ima && v != u.Velicina {
			return fmt.Errorf("ulaz %s računa se u veličini %s, a ovdje se traži u veličini %s", u.Letva, v, u.Velicina)
		}
	}
	return nil
}

// velicinaRacuna je veličina u kojoj se letva računa, ako ima račun.
func velicinaRacuna(letva string, zapisi map[string]ZapisLetve, glavne map[string][][]Pojas) (string, bool) {
	if z, ima := zapisi[letva]; ima {
		return z.Velicina, true
	}
	if in := glavne[letva]; len(in) > 0 && len(in[0]) > 0 {
		return in[0][0].Velicina, true
	}
	return "", false
}

// iskljuciNizvodne isključuje sve što računa iz isključene letve, u bilo kojoj
// inačici: letva bez računa inače bi postala vrh lanca, pa bi prognoza nizvodno
// tiho držala zadnje mjerenje. Letva koju računa glavni lanac tu se ne broji: ona
// računa dalje s istim brojevima, koliko god njezin redak bio pokvaren.
func iskljuciNizvodne(zapisi map[string]ZapisLetve, isk map[string]error, o OkvirModela) {
	zauzete := zauzeteLetve(o.Glavne)
	nema := func(u string) bool {
		_, ispao := isk[u]
		_, glavna := zauzete[u]
		return ispao && !glavna && !o.UIzdanju[u]
	}
	for promjena := true; promjena; {
		promjena = false
		for l, z := range zapisi {
			if _, ispala := isk[l]; ispala {
				continue
			}
			if i := slices.IndexFunc(z.letveUlaza(), nema); i >= 0 {
				isk[l] = fmt.Errorf("računa se iz letve %s, a njezin model je isključen", z.letveUlaza()[i])
				promjena = true
			}
		}
	}
}

// Provjeri odbija zapis koji se ne smije računati. Krug i veličinu veze gleda
// ProvjeriZapise, jer traže sve retke.
func (z ZapisLetve) Provjeri(o OkvirModela) error {
	for _, korak := range []func() error{
		z.provjeriOblik, z.provjeriRacun, z.provjeriBrojeve,
		func() error { return z.provjeriMjesto(o) },
		func() error { return z.provjeriRegistar(o) },
	} {
		if err := korak(); err != nil {
			return err
		}
	}
	return nil
}

// provjeriOblik gleda ono bez čega se zapis ne smije ni tumačiti.
func (z ZapisLetve) provjeriOblik() error {
	switch {
	case z.Oblik > OblikModelaLetve:
		return errors.New("zapis modela je iz novije inačice programa")
	case z.Oblik < 1:
		return errors.New("zapis modela nema oblik")
	case z.Metoda != MetodaLanac:
		return fmt.Errorf("ovaj program ne zna metodu %q", z.Metoda)
	case !valjanaVelicina(z.Velicina):
		return fmt.Errorf("nepoznata veličina %q", z.Velicina)
	case z.Letva == "":
		return errors.New("zapis modela nema letvu")
	}
	return nil
}

func valjanaVelicina(v string) bool { return v == "vodostaj" || v == "protok" }

// provjeriRacun gleda definiciju: ulaze, doseg, razine i ručne vrijednosti.
func (z ZapisLetve) provjeriRacun() error {
	r := z.Racun
	switch {
	case len(r.Ulazi) == 0:
		return errors.New("račun nema nijedan ulaz")
	case r.Doseg < 1 || r.Doseg > najveciDosegZapisa:
		return fmt.Errorf("doseg %d nije između 1 i %d sati", r.Doseg, najveciDosegZapisa)
	case len(r.Rezerve)+1 > najviseInacica:
		return fmt.Errorf("najviše je %d rezervi", najviseInacica-1)
	case slices.ContainsFunc(r.Rezerve, func(u []string) bool { return len(u) == 0 }):
		return errors.New("rezerva nema nijedan ulaz")
	}
	if err := r.Razine.provjeri(); err != nil {
		return err
	}
	return r.Rucno.provjeri()
}

func (r RazineLetve) provjeri() error {
	if !postociRazina(r.Postoci) {
		return errors.New("postoci razina moraju rasti od 0 do 100")
	}
	if !rastuci(r.Granice, najvecaVrijednost) {
		return errors.New("granice razina moraju rasti")
	}
	if len(r.Postoci) > 0 && len(r.Granice) > 0 {
		return errors.New("razine se zadaju postocima ili granicama, ne jednim i drugim")
	}
	if slices.ContainsFunc(r.Iskljucene, func(i int) bool { return i < 0 || i >= najviseRazinaZapisa }) {
		return errors.New("isključena razina ne postoji")
	}
	return nil
}

// postociRazina traži prazan popis ili rast od točno 0 do točno 100: voda izvan
// postotaka inače ne bi imala nijednu razinu.
func postociRazina(p []float64) bool {
	if len(p) == 0 {
		return true
	}
	return len(p) >= 2 && p[0] == 0 && p[len(p)-1] == 100 && rastuci(p, 100)
}

func (r RucnoLetve) provjeri() error {
	for _, k := range r.Kasnjenje {
		if !k.razumno() {
			return fmt.Errorf("upisano kašnjenje %d h nije između 0 i %d", k.Sati, najveceKasnjenje)
		}
	}
	if r.Nagib != nil && !uGranicama(*r.Nagib, najveciNagib) {
		return errors.New("upisani nagib nije razuman broj")
	}
	return nil
}

func (k RucnoKasnjenje) razumno() bool {
	return k.Razina >= 0 && k.Razina < najviseRazinaZapisa && k.Sati >= 0 && k.Sati <= najveceKasnjenje
}

// uGranicama javlja je li broj konačan i ne veći od granice po iznosu.
func uGranicama(v, granica float64) bool { return math.Abs(v) <= granica }

// rastuci javlja da svaki broj raste i da je u granicama.
func rastuci(v []float64, granica float64) bool {
	for i, x := range v {
		if !uGranicama(x, granica) || (i > 0 && x <= v[i-1]) {
			return false
		}
	}
	return true
}

// provjeriBrojeve gleda namještene brojeve po inačicama i promašaje.
func (z ZapisLetve) provjeriBrojeve() error {
	b := z.Brojevi
	if err := z.provjeriGlavnuInacicu(); err != nil {
		return err
	}
	for _, ps := range b.Inacice {
		if err := z.provjeriInacicu(ps); err != nil {
			return err
		}
	}
	return provjeriPromasaje(b.Promasaji)
}

// provjeriGlavnuInacicu traži brojeve glavne inačice iz glavnog ulaza računa.
func (z ZapisLetve) provjeriGlavnuInacicu() error {
	in := z.Brojevi.Inacice
	if len(in) == 0 || len(in[0]) == 0 {
		return errors.New("račun nema namještenih brojeva")
	}
	if len(in) > najviseInacica {
		return fmt.Errorf("najviše je %d inačica računa", najviseInacica)
	}
	if u := in[0][0].Ulazi; len(u) == 0 || u[0].Letva != z.Racun.Ulazi[0] {
		return errors.New("namješteni brojevi ne odgovaraju glavnom ulazu računa")
	}
	return nil
}

func (z ZapisLetve) provjeriInacicu(ps []Pojas) error {
	if len(ps) == 0 || len(ps) > najviseRazinaZapisa {
		return fmt.Errorf("inačica računa treba 1 do %d razina", najviseRazinaZapisa)
	}
	for i, p := range ps {
		if i > 0 && p.Od <= ps[i-1].Od {
			return errors.New("granice razina moraju rasti")
		}
		if err := z.provjeriPojas(p); err != nil {
			return err
		}
	}
	return nil
}

func (z ZapisLetve) provjeriPojas(p Pojas) error {
	if p.Letva != z.Letva || p.Velicina != z.Velicina {
		return errors.New("namješteni brojevi nose drugu letvu ili veličinu")
	}
	if !p.razuman() {
		return errors.New("granice, odsječak ili rasap razine nisu razumni brojevi")
	}
	return z.provjeriUlaze(p.Ulazi)
}

func (p Pojas) razuman() bool {
	return rastuciIliJednaki(p.Od, p.Do) && uGranicama(p.Odsjecak, najvecaVrijednost) && uGranicama(p.Rasap, najvecaVrijednost) &&
		p.Rasap >= 0 && uGranicama(p.R, 1.001) && p.Sati >= 0
}

// rastuciIliJednaki traži dva razumna broja, prvi ne veći od drugog.
func rastuciIliJednaki(od, do float64) bool {
	return uGranicama(od, najvecaVrijednost) && uGranicama(do, najvecaVrijednost) && od <= do
}

func (z ZapisLetve) provjeriUlaze(ulazi []Ulaz) error {
	if len(ulazi) == 0 || len(ulazi) > najviseUlaza {
		return fmt.Errorf("razina treba 1 do %d ulaza", najviseUlaza)
	}
	dozvoljeni := z.Racun.Ulazi
	for _, r := range z.Racun.Rezerve {
		dozvoljeni = slices.Concat(dozvoljeni, r)
	}
	for _, u := range ulazi {
		if !slices.Contains(dozvoljeni, u.Letva) {
			return fmt.Errorf("namješteni ulaz %s nije među ulazima računa", u.Letva)
		}
		if err := provjeriUlaz(u); err != nil {
			return err
		}
	}
	return nil
}

func provjeriUlaz(u Ulaz) error {
	if !valjanaVelicina(u.Velicina) {
		return fmt.Errorf("ulaz %s: nepoznata veličina %q", u.Letva, u.Velicina)
	}
	if u.PomakH < 0 || u.Sirina < 1 || u.PomakH+u.Sirina > najveceKasnjenje {
		return fmt.Errorf("ulaz %s: kašnjenje i prozor nisu između 0 i %d sati", u.Letva, najveceKasnjenje)
	}
	if !uGranicama(u.Nagib, najveciNagib) {
		return fmt.Errorf("ulaz %s: nagib nije razuman broj", u.Letva)
	}
	return nil
}

func provjeriPromasaje(promasaji map[int]Promasaj) error {
	for d, p := range promasaji {
		if d < 0 || d > najveciDosegZapisa {
			return fmt.Errorf("promašaj za doseg %d nije u izdanju", d)
		}
		if !p.razuman() {
			return fmt.Errorf("promašaj za doseg %d nije razuman broj", d)
		}
	}
	return nil
}

func (p Promasaj) razuman() bool {
	return uGranicama(p.Pomak, najvecaVrijednost) && uGranicama(p.Rasap, najvecaVrijednost) && p.Rasap >= 0 && uGranicama(p.Postojanost, najvecaVrijednost)
}

// provjeriMjesto odbija letve koje ne smiju imati odvojen račun: one koje
// glavni račun već računa ili čita, i elektrane i brane.
func (z ZapisLetve) provjeriMjesto(o OkvirModela) error {
	if jeElektranaIliBrana(z.Letva) {
		return errors.New("elektrana ili brana ne računa se ovim modelom")
	}
	if razlog, ima := zauzeteLetve(o.Glavne)[z.Letva]; ima {
		return errors.New(razlog)
	}
	if o.UIzdanju[z.Letva] {
		return errors.New("letva već ima retke glavnog računa u izdanju")
	}
	return nil
}

// jeElektranaIliBrana prepoznaje šifre elektrana i brana.
func jeElektranaIliBrana(letva string) bool {
	return slices.ContainsFunc([]string{"he-", "gvb-", "brana-"}, func(p string) bool { return strings.HasPrefix(letva, p) })
}

// provjeriRegistar traži sektor letve i satni protok u arhivi za svaku letvu
// koju račun vodi u protoku.
func (z ZapisLetve) provjeriRegistar(o OkvirModela) error {
	if z.Ugradjen {
		return nil
	}
	if o.ImaSektor != nil && !o.ImaSektor(z.Letva) {
		return errors.New("letva nema sektor; upiši ga u obrascu letve")
	}
	if o.ImaSatniProtok == nil {
		return nil
	}
	for _, l := range z.letveUProtoku() {
		if !o.ImaSatniProtok(l) {
			return fmt.Errorf("letva %s nema satni protok u arhivi; računaj u vodostaju", l)
		}
	}
	return nil
}

// letveUProtoku su letve koje račun vodi u protoku: cilj i ulazi.
func (z ZapisLetve) letveUProtoku() []string {
	var out []string
	if z.Velicina == "protok" {
		out = append(out, z.Letva)
	}
	for _, ps := range z.Brojevi.Inacice {
		for _, p := range ps {
			for _, u := range p.Ulazi {
				if u.Velicina == "protok" {
					out = append(out, u.Letva)
				}
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// zauzeteLetve su letve koje glavni račun računa ili čita, s razlogom: iz koda
// i iz baze, jer se račun iz koda ne mora dati namjestiti, a i tada letva pripada
// glavnom lancu.
func zauzeteLetve(glavne map[string][][]Pojas) map[string]string {
	z := zauzece{}
	z.izLanca()
	z.izBaze(glavne)
	for _, c := range DnevniCiljevi {
		z.dodaj("letva je cilj dnevnog modela", c.Letva)
	}
	z.dodaj("letva je ulaz dnevnog modela", DnevniUlazi()...)
	for _, p := range PreneseneLetve {
		z.dodaj("letva ima prenesenu prognozu", p.Letva)
	}
	z.dodaj("letva stoji na pregledu kao mjerenje", PregledneLetve...)
	return z
}

type zauzece map[string]string

// dodaj upisuje letve; zapis u kodu smije nositi i veličinu („letva:protok”).
func (z zauzece) dodaj(razlog string, letve ...string) {
	for _, l := range letve {
		sifra, _, _ := strings.Cut(l, ":")
		sifra = strings.TrimSpace(sifra)
		if _, bilo := z[sifra]; !bilo {
			z[sifra] = razlog
		}
	}
}

const (
	razlogGlavna    = "letva se računa u glavnom lancu"
	razlogUlazLanca = "letva je ulaz glavnom lancu"
)

func (z zauzece) izLanca() {
	for _, t := range Tokovi {
		for _, r := range t.Racuni {
			z.dodaj(razlogGlavna, r.Letva)
		}
	}
	for _, t := range Tokovi {
		for _, r := range t.Racuni {
			z.dodaj(razlogUlazLanca, r.Ulazi...)
		}
	}
	for l, rez := range Rezerve {
		z.dodaj(razlogGlavna, l)
		z.dodaj(razlogUlazLanca, slices.Concat(rez...)...)
	}
}

func (z zauzece) izBaze(glavne map[string][][]Pojas) {
	for l := range glavne {
		z.dodaj(razlogGlavna, l)
	}
	for _, in := range glavne {
		for _, ps := range in {
			for _, p := range ps {
				for _, u := range p.Ulazi {
					z.dodaj(razlogUlazLanca, u.Letva)
				}
			}
		}
	}
}

// PrikazOdvojenih je ono što stranica prognoza treba znati o letvama iz zapisa.
type PrikazOdvojenih struct {
	Pojasi    map[string][]Pojas // glavne inačice
	Promasaji map[Izvor]map[int]Promasaj
	Vrhovi    map[string]bool // ulazi koji pripadaju samo novom računu, bez svog računa
}

// OdvojeniZaPrikaz slaže PrikazOdvojenih. Zapis koji se ne bi računao ovdje ne
// stoji, pa letva glavnog lanca s pogrešnim retkom ostaje kakva je i bila.
func OdvojeniZaPrikaz(db *sql.DB) PrikazOdvojenih {
	p := PrikazOdvojenih{Pojasi: map[string][]Pojas{}, Promasaji: map[Izvor]map[int]Promasaj{}, Vrhovi: map[string]bool{}}
	zapisi, pokvareni, err := ZapisiLetvi(db)
	if err != nil || len(zapisi) == 0 {
		return p
	}
	glavne, err := SveInacice(db)
	if err != nil {
		return p
	}
	zauzete := zauzeteLetve(glavne)
	dobri := ostaliZapisi(zapisi, ProvjeriZapise(zapisi, pokvareni, OkvirModela{Glavne: glavne}))
	for l, z := range dobri {
		p.Pojasi[l] = z.Brojevi.Inacice[0]
		p.Promasaji[Izvor{Letva: l, Velicina: z.Velicina}] = z.Brojevi.Promasaji
		dodajVrhove(p.Vrhovi, z, zauzete)
	}
	for l := range dobri {
		delete(p.Vrhovi, l)
	}
	return p
}

func dodajVrhove(vrhovi map[string]bool, z ZapisLetve, zauzete map[string]string) {
	for _, u := range z.letveUlaza() {
		if _, glavna := zauzete[u]; !glavna {
			vrhovi[u] = true
		}
	}
}
