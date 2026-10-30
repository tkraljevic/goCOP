package arhiva

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"gocop/internal/trajno"
)

// Ulaz u arhivu: gdje datoteka stoji i kako se zove. Naziv je ugovor —
// letva_izvor_velicina_vrsta_razdoblje.csv — jer gradnja iz njega čita sve što
// o nizu treba znati. Program datoteku smije samo staviti na pravo mjesto pod
// pravim imenom; čitanje ostaje na jednom mjestu, u gradnji.

// Velicine i Vrste su ono što gradnja razumije.
var (
	// Uz veličine s letve tu su i one s kvazi-kišomjera (registar slivova):
	// oborina i snijeg u mm vodenog ekvivalenta, visina snijega u cm i
	// temperatura zraka — odvojena od temperature vode, jer im rasponi i
	// smisao nisu isti.
	Velicine = []string{"vodostaj", "protok", "temperatura", "koncentracija", "pronos", "oborina", "snijeg", "visina-snijega", "temperatura-zraka"}
	Vrste    = []string{"satni", "srednjak", "jutarnji", "dnevni", "dvokratni"}
	Zone     = []string{"Europe/Zagreb", "UTC"}
)

// MogucaVrijednost javlja bi li gradnja tu vrijednost uopće primila. Vrata
// moraju odbijati po istom pravilu po kojem gradnja odbija, inače bi čovjek
// upisao nešto što se poslije tiho izgubi.
func MogucaVrijednost(velicina string, v float64) bool { return mogucaVrijednost(velicina, v) }

// Redak je jedno očitanje spremno za upis.
type Redak struct {
	Vrijeme time.Time
	// PoDanu javlja da izvor daje samo datum, bez sata. Zaglavlje se tada piše
	// kao "datum", jer dan je dan bez obzira na zonu.
	PoDanu     bool
	Vrijednost float64
	// izDatotekeDana javlja da je dopuna redak pročitala iz zatečene datoteke
	// sa zaglavljem „datum”. Takav niz ostaje po danu i kad je u retku sat.
	izDatotekeDana bool
}

// Letve vraća letve koje stablo već ima, sa slivom u kojem stoje. Nova letva
// nije zabranjena, ali mora doći sa slivom — inače ne bi imala gdje stati.
func Letve(koren string) (map[string]string, error) {
	out := map[string]string{}
	if koren == "" {
		return out, nil
	}
	slivovi, err := os.ReadDir(koren)
	if err != nil {
		return nil, err
	}
	for _, s := range slivovi {
		if !s.IsDir() || strings.HasPrefix(s.Name(), "PRISTUP") {
			continue
		}
		letve, err := os.ReadDir(filepath.Join(koren, s.Name()))
		if err != nil {
			return nil, err
		}
		for _, l := range letve {
			if l.IsDir() {
				out[l.Name()] = s.Name()
			}
		}
	}
	return out, nil
}

// LetveStabla je Letve za uvoz: stablo kojeg još nema nije greška, jer ga
// novi čvor počinje prvim uvozom. Svaka druga greška čitanja jest, jer se
// bez nje ne zna u kojem slivu letva već stoji.
func LetveStabla(koren string) (map[string]string, error) {
	letve, err := Letve(koren)
	if _, e := os.Stat(koren); errors.Is(e, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	return letve, err
}

// Slivovi vraća slivove koje stablo ima, poredane.
func Slivovi(koren string) ([]string, error) {
	letve, err := Letve(koren)
	if err != nil {
		return nil, err
	}
	vidjeno := map[string]bool{}
	for _, s := range letve {
		vidjeno[s] = true
	}
	var out []string
	for s := range vidjeno {
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// KanonskoIme slaže naziv po ugovoru. Razdoblje je godina, ili raspon godina
// kad ih niz obuhvaća više.
func KanonskoIme(letva, izvor, velicina, vrsta string, od, do time.Time) string {
	razdoblje := od.Format("2006")
	if g := do.Format("2006"); g != razdoblje {
		razdoblje += "-" + g
	}
	return strings.Join([]string{letva, izvor, velicina, vrsta, razdoblje}, "_") + ".csv"
}

// ProvjeriDjelove javlja je li ono što je čovjek upisao uopće upotrebljivo kao
// dio naziva. Dio naziva smije imati samo slova, brojke, crticu i točku
// (dopusteniNaziv): donja crta bi razlomila ugovor, kosa crta bi izašla iz
// mape, a znakovi traženja (* ? [) promijenili bi koje se datoteke smatraju
// starim nizom istog imena.
func ProvjeriDjelove(letva, izvor, velicina, vrsta string) error {
	for ime, v := range map[string]string{"letva": letva, "izvor": izvor, "veličina": velicina, "vrsta": vrsta} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s nije zadana", ime)
		}
		if !dopusteniNaziv(v) {
			return fmt.Errorf("%s %q ne smije imati ništa osim slova, brojki, crtice i točke, ni točku na početku ili kraju", ime, v)
		}
	}
	if !sadrziNiz(Velicine, velicina) {
		return fmt.Errorf("veličina %q nije jedna od: %s", velicina, strings.Join(Velicine, ", "))
	}
	if !sadrziNiz(Vrste, vrsta) {
		return fmt.Errorf("vrsta %q nije jedna od: %s", vrsta, strings.Join(Vrste, ", "))
	}
	return nil
}

// ProvjeriSliv javlja je li sliv upotrebljiv kao naziv mape u stablu. Sliv je
// jedna mapa ispod korijena s dopuštenim nazivom (dopusteniNaziv). Ne smije
// počinjati s „PRISTUP”, jer takve mape stablo preskače (Letve), pa letva u
// njoj ne bi bila vidljiva.
func ProvjeriSliv(sliv string) error {
	if !dopusteniNaziv(sliv) || strings.HasPrefix(sliv, "PRISTUP") {
		return fmt.Errorf("sliv %q nije upotrebljiv kao naziv mape", sliv)
	}
	return nil
}

// dopusteniNaziv: samo slova, brojke, crtica i točka, a točka ni na početku
// ni na kraju. Tako ne prolaze „.” i „..”, skrivene mape, kose i donje crte,
// razmaci, kontrolni znakovi, znakovi traženja (* ? [) ni znakovi koje
// Windows ne prima u nazivu (: < > | ").
func dopusteniNaziv(s string) bool {
	return s != "" && strings.Trim(s, ".") == s && strings.IndexFunc(s, nedopustenZnak) < 0
}

func nedopustenZnak(z rune) bool {
	return !unicode.IsLetter(z) && !unicode.IsDigit(z) && z != '-' && z != '.'
}

// MapaLetve vraća mapu letve u stablu, <koren>/<sliv>/<letva>. Sliv i letva
// moraju biti obični nazivi mapa, a gotova putanja mora ostati ispod korijena.
func MapaLetve(koren, sliv, letva string) (string, error) {
	if err := ProvjeriSliv(sliv); err != nil {
		return "", err
	}
	if !dopusteniNaziv(letva) {
		return "", fmt.Errorf("letva %q nije upotrebljiva kao naziv mape", letva)
	}
	mapa := filepath.Join(koren, sliv, letva)
	return mapa, uStablu(koren, mapa, filepath.Join(sliv, letva))
}

// uStablu potvrđuje da je mapa točno rel ispod korijena. Uz provjerene
// nazive to uvijek vrijedi; provjera ostaje za slučaj da se pravilo naziva
// jednom olabavi.
func uStablu(koren, mapa, rel string) error {
	if r, err := filepath.Rel(koren, mapa); err != nil || r != rel {
		return fmt.Errorf("mapa %s ne stoji u stablu s podacima", filepath.ToSlash(rel))
	}
	return nil
}

func sadrziNiz(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// Upisi zapisuje niz u stablo pod kanonskim imenom i vraća putanju. Postojeća
// datoteka istog niza zamjenjuje se, jer je ovo cjelovita izjava o razdoblju
// koje pokriva — ali tek nakon što je nova pročitana natrag i provjerena.
// Stari niz pod drugim razdobljem koji se ne da maknuti vraća se kao
// StariNizOstao uz putanju novoga, koji je zapisan; to nije neuspjeh upisa
// (JaviOstatak).
// Relativno stablo koje je vjerojatno drugdje ne počinje iznova (NemaMape).
func Upisi(koren, sliv, letva, izvor, velicina, vrsta string, redci []Redak) (string, error) {
	if err := ProvjeriKoren(koren); err != nil {
		return "", err
	}
	if err := ProvjeriDjelove(letva, izvor, velicina, vrsta); err != nil {
		return "", err
	}
	mapa, err := MapaLetve(koren, sliv, letva)
	if err != nil {
		return "", err
	}
	if len(redci) == 0 {
		return "", fmt.Errorf("nema nijednog retka za upis")
	}
	sort.Slice(redci, func(a, b int) bool { return redci[a].Vrijeme.Before(redci[b].Vrijeme) })

	if err := os.MkdirAll(mapa, 0o755); err != nil {
		return "", err
	}
	put := filepath.Join(mapa, KanonskoIme(letva, izvor, velicina, vrsta,
		redci[0].Vrijeme, redci[len(redci)-1].Vrijeme))

	// Vrijeme se piše u zoni koju gradnja za taj izvor očekuje. Hrvatski izvori
	// drže LOKALNI sat u stupcu nazvanom vrijeme_utc — naziv je zatečen i
	// pogrešan, ali gradnja tako čita, pa bi pravi UTC ovdje značio da se svaka
	// upisana vrijednost pri sljedećoj gradnji pomakne za sat ili dva.
	zona := zonaIzvora(izvor)
	privremeno := put + ".nova"
	if err := zapisiCSV(privremeno, velicina, zona, redci); err != nil {
		os.Remove(privremeno)
		return "", err
	}
	if err := provjeriZapisano(privremeno, zona, redci); err != nil {
		os.Remove(privremeno)
		return "", err
	}
	// Isti niz zna prije stajati pod drugim razdobljem u nazivu; ono što bi
	// ostalo iza njega gradnja bi pročitala kao dodatne godine istog niza.
	stare, _ := filepath.Glob(filepath.Join(mapa,
		strings.Join([]string{letva, izvor, velicina, vrsta}, "_")+"_*.csv"))
	if err := os.Rename(privremeno, put); err != nil {
		return "", err
	}
	// Novo ime mora biti na disku prije nego što stari niz nestane, inače
	// nestanak struje može ostaviti mapu bez ijednog.
	trajno.SpustiMapu(mapa)
	var ostali error
	for _, s := range stare {
		if s != put {
			ostali = errors.Join(ostali, makniStari(s, put))
		}
	}
	trajno.SpustiMapu(mapa) // i brisanje starih mora na disk
	return put, ostali
}

// makniStari miče niz koji je ostao pod starim razdobljem u nazivu. Ako se
// ne da maknuti, to se javlja (StariNizOstao). Niz koji je već nestao nije
// greška.
func makniStari(stari, novi string) error {
	err := os.Remove(stari)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return &StariNizOstao{Novi: filepath.Base(novi), Stari: filepath.Base(stari), Err: err}
}

// StariNizOstao je upozorenje uz upis koji je uspio: novi niz je zapisan, ali
// stari pod drugim razdobljem u nazivu nije se dao maknuti, pa bi ga gradnja
// pročitala uz novi kao dodatne godine istog niza. Posao zato ide dalje, a
// stari niz treba maknuti ručno. Poruka kaže samo imena datoteka; razlog koji
// je dao sustav ostaje u Err, za dnevnik.
type StariNizOstao struct {
	Novi, Stari string
	Err         error
}

func (e *StariNizOstao) Error() string {
	return fmt.Sprintf("novi niz %s je zapisan, ali stari %s nije se dao maknuti iz mape, pa bi ga gradnja pročitala uz novi. "+
		"Maknite ga ručno; ako je otvoren u drugom programu, npr. u Excelu, prvo ga zatvorite", e.Novi, e.Stari)
}

func (e *StariNizOstao) Unwrap() error { return e.Err }

// JaviOstatak odvaja stari niz koji je ostao od prave greške upisa. Kad je
// upis vratio samo to, a Upisi i Dopuni ga vraćaju samo kad je sve ostalo
// uspjelo, ispiše upozorenje u w i vrati nil, da posao ide na sljedeći niz.
// Svaku drugu grešku vrati kakva jest. Bez w upozorenje se ne ispisuje.
func JaviOstatak(err error, w io.Writer) error {
	var o *StariNizOstao
	if !errors.As(err, &o) {
		return err
	}
	if w != nil {
		_, _ = fmt.Fprintf(w, "upozorenje: %v\n", err) // dnevnik posla; posao ne staje zbog retka koji se nije ispisao
	}
	return nil
}

// SkupiOstatak je JaviOstatak za posao bez dnevnika: stari niz koji je ostao
// doda u ostaci, koje posao na kraju vrati uz sve što je napravio.
func SkupiOstatak(err error, ostaci *error) error {
	var o *StariNizOstao
	if !errors.As(err, &o) {
		return err
	}
	*ostaci = errors.Join(*ostaci, err)
	return nil
}

// Dopuni spaja nove retke s onim što niz već ima u stablu i zapisuje sve.
//
// Ulaganje očitanja mora dopunjavati, ne zamjenjivati: izvor `cop` na Vukovaru
// već drži 8.154 jutarnja očitanja od 2004., a ulaže se jedna godina. Upisi bi
// stariji dio maknuo jer nosi isto ime niza.
//
// Kad se isti trenutak pojavi u oboje, novi redak pobjeđuje — ulaže se ono što
// je čovjek upisao i ispravio, a to je novije od onoga što je ondje stajalo.
func Dopuni(koren, sliv, letva, izvor, velicina, vrsta string, redci []Redak) (string, error) {
	if err := ProvjeriDjelove(letva, izvor, velicina, vrsta); err != nil {
		return "", err
	}
	if err := ProvjeriSliv(sliv); err != nil {
		return "", err
	}
	stare, err := PostojeciRedci(koren, sliv, letva, izvor, velicina, vrsta)
	if err != nil {
		return "", fmt.Errorf("niz nije dopunjen, jer se zatečeni niz u stablu ne da pročitati do kraja: %w", err)
	}
	po := map[int64]Redak{}
	for _, r := range stare {
		po[r.Vrijeme.Unix()] = r
	}
	for _, r := range redci {
		po[r.Vrijeme.Unix()] = r
	}
	spojeno := make([]Redak, 0, len(po))
	for _, r := range po {
		spojeno = append(spojeno, r)
	}
	return Upisi(koren, sliv, letva, izvor, velicina, vrsta, spojeno)
}

// PostojeciRedci čita ono što niz već ima u stablu. Niza može i ne biti — tada
// se ulaže na prazno i to nije greška. Ni sliv ili letva koji nisu naziv mape
// u stablu nemaju niza.
//
// Čita se kao u gradnji, istim čitačem i istim pravilom za redak. Prije se
// redak dijelio na prvoj točki-zarezu, pa su redak s trećim stupcem, polje u
// navodnicima i vrijeme bez sekundi tiho ispadali, a Dopuni je datoteku
// prepisala bez njih. Redak koji se ne da pročitati sad je greška, pa dopuna
// odbije upis i datoteka ostane kakva je bila.
func PostojeciRedci(koren, sliv, letva, izvor, velicina, vrsta string) ([]Redak, error) {
	mapa, err := MapaLetve(koren, sliv, letva)
	if err != nil || ProvjeriDjelove(letva, izvor, velicina, vrsta) != nil {
		// Niza nema ondje gdje mapa ne smije ni stajati ni pod imenom koje se
		// ne smije upisati; upis takav sliv ili dio naziva odbije.
		return nil, nil
	}
	uzorak := filepath.Join(mapa,
		strings.Join([]string{letva, izvor, velicina, vrsta}, "_")+"_*.csv")
	puts, err := filepath.Glob(uzorak)
	if err != nil || len(puts) == 0 {
		return nil, nil
	}
	sort.Strings(puts)
	var out []Redak
	for _, p := range puts {
		redci, err := redciDatoteke(p, zonaIzvora(izvor))
		if err != nil {
			return nil, err
		}
		out = append(out, redci...)
	}
	return out, nil
}

// redciDatoteke čita jednu datoteku niza za dopunu. Prazna datoteka nema
// redaka; gradnja je ionako ne čita, pa dopuna nema što izgubiti.
func redciDatoteke(put string, zona *time.Location) ([]Redak, error) {
	f, err := os.Open(put)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cr := csvNiza(f)
	glava, err := cr.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s redak 1: %w", filepath.Base(put), err)
	}
	return redciIspodZaglavlja(cr, filepath.Base(put), zona, jeZaglavljeDana(glava))
}

// redciIspodZaglavlja čita retke datoteke niza. Redak bez vrijednosti gradnja
// preskače, pa i dopuna.
func redciIspodZaglavlja(cr *csv.Reader, ime string, zona *time.Location, poDanu bool) ([]Redak, error) {
	var out []Redak
	for i := 2; ; i++ {
		r, err := cr.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s redak %d: %w", ime, i, err)
		}
		if err := bezStupcaViska(r, ime, i); err != nil {
			return nil, err
		}
		rn, ima, err := procitajRedak(r, zona, ime, i)
		if err != nil {
			return nil, err
		}
		if ima {
			out = append(out, rn.uRedak(poDanu))
		}
	}
}

// uRedak vraća pročitani redak u obliku za upis. Sat u nizu po danu ostaje
// sat: zapisan kao sam datum, gradnja bi ga poslije pročitala u ponoć.
func (rn redakNiza) uRedak(poDanu bool) Redak {
	return Redak{Vrijeme: rn.t.UTC(), PoDanu: poDanu && !rn.imaSat, Vrijednost: rn.v, izDatotekeDana: poDanu}
}

// bezStupcaViska javlja grešku kad iza vrijednosti nešto piše. Gradnja taj
// stupac ne čita, ali dopuna datoteku piše iznova, samo s vremenom i
// vrijednošću, pa bi se ono što u njemu piše izgubilo.
func bezStupcaViska(r []string, ime string, i int) error {
	for _, s := range r[min(len(r), 2):] {
		if strings.TrimSpace(s) != "" {
			return fmt.Errorf("%s redak %d: iza vrijednosti piše još %q, a dopuna zapisuje samo vrijeme i vrijednost, pa bi se to izgubilo", ime, i, s)
		}
	}
	return nil
}

// zaglavlje bira prvi stupac po tome ima li niz sat. Gradnja po tome razlikuje
// vrijednost vezanu uz trenutak od one vezane uz dan.
//
// Dopuna zadrži zaglavlje „datum” zatečene datoteke. Dok se gledao samo prvi
// redak, niz po danu kojem je najraniji redak sa satom prepisan je kao satni,
// pa ga je gradnja poslije drukčije spajala.
func zaglavlje(velicina string, redci []Redak) string {
	prvi := "vrijeme_utc"
	zatecenPoDanu := func(r Redak) bool { return r.izDatotekeDana }
	if (len(redci) > 0 && redci[0].PoDanu) || slices.ContainsFunc(redci, zatecenPoDanu) {
		prvi = "datum"
	}
	drugi := map[string]string{
		"vodostaj": "vodostaj_cm", "protok": "protok_m3s", "temperatura": "temperatura_c",
		"koncentracija": "koncentracija_gl", "pronos": "pronos_kgs",
		"oborina": "oborina_mm", "snijeg": "snijeg_mm", "visina-snijega": "visina_snijega_cm", "temperatura-zraka": "temperatura_zraka_c",
	}[velicina]
	if drugi == "" {
		drugi = velicina
	}
	return prvi + ";" + drugi
}

func zapisiCSV(put, velicina string, zona *time.Location, redci []Redak) error {
	f, err := os.Create(put)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	if _, err := fmt.Fprintln(w, zaglavlje(velicina, redci)); err != nil {
		f.Close()
		return err
	}
	for _, r := range redci {
		if _, err := fmt.Fprintf(w, "%s;%s\n", vrijemeUCSV(r, zona), brojUCSV(r.Vrijednost)); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return trajno.SpustiIZatvori(f)
}

// vrijemeUCSV piše sat u zoni izvora, a dan bez zone — dan je dan bez obzira
// odakle se gleda, i gradnja ga tako i čita.
func vrijemeUCSV(r Redak, zona *time.Location) string {
	if r.PoDanu {
		return r.Vrijeme.UTC().Format("2006-01-02")
	}
	return r.Vrijeme.In(zona).Format("2006-01-02 15:04:05")
}

// brojUCSV piše bez suvišnih nula: 776 ostaje 776, a 8,66 ostaje 8.66.
func brojUCSV(v float64) string {
	s := fmt.Sprintf("%.6f", v)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// provjeriZapisano čita datoteku natrag i uspoređuje s onim što je u nju
// otišlo. Mjerenje se ne može ponoviti, pa se provjerava prije nego se stara
// datoteka makne.
func provjeriZapisano(put string, zona *time.Location, redci []Redak) error {
	f, err := os.Open(put)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<20)
	if !s.Scan() {
		return fmt.Errorf("provjera: datoteka je prazna")
	}
	n := 0
	for s.Scan() {
		redak := strings.TrimSpace(s.Text())
		if redak == "" {
			continue
		}
		if n >= len(redci) {
			return fmt.Errorf("provjera: zapisano je više redaka nego što ih je bilo")
		}
		ocekivano := vrijemeUCSV(redci[n], zona) + ";" + brojUCSV(redci[n].Vrijednost)
		if redak != ocekivano {
			return fmt.Errorf("provjera: redak %d je %q, a upisano je bilo %q", n+1, redak, ocekivano)
		}
		n++
	}
	if err := s.Err(); err != nil {
		return err
	}
	if n != len(redci) {
		return fmt.Errorf("provjera: zapisano %d redaka, pročitano %d", len(redci), n)
	}
	return nil
}
