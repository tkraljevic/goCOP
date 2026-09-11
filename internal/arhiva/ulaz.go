package arhiva

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Ulaz u arhivu: gdje datoteka stoji i kako se zove. Naziv je ugovor —
// letva_izvor_velicina_vrsta_razdoblje.csv — jer gradnja iz njega čita sve što
// o nizu treba znati. Program datoteku smije samo staviti na pravo mjesto pod
// pravim imenom; čitanje ostaje na jednom mjestu, u gradnji.

// Velicine i Vrste su ono što gradnja razumije.
var (
	Velicine = []string{"vodostaj", "protok", "temperatura", "koncentracija", "pronos"}
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
// dio naziva. Donja crta bi razlomila ugovor, a kosa crta bi izašla iz mape.
func ProvjeriDjelove(letva, izvor, velicina, vrsta string) error {
	for ime, v := range map[string]string{"letva": letva, "izvor": izvor, "veličina": velicina, "vrsta": vrsta} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s nije zadana", ime)
		}
		if strings.ContainsAny(v, "_/\\ ") {
			return fmt.Errorf("%s %q ne smije sadržavati donju crtu, kosu crtu ni razmak", ime, v)
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
func Upisi(koren, sliv, letva, izvor, velicina, vrsta string, redci []Redak) (string, error) {
	if koren == "" {
		return "", fmt.Errorf("nije zadano gdje stablo s podacima stoji")
	}
	if err := ProvjeriDjelove(letva, izvor, velicina, vrsta); err != nil {
		return "", err
	}
	if strings.TrimSpace(sliv) == "" || strings.ContainsAny(sliv, "_/\\ ") {
		return "", fmt.Errorf("sliv %q nije upotrebljiv kao naziv mape", sliv)
	}
	if len(redci) == 0 {
		return "", fmt.Errorf("nema nijednog retka za upis")
	}
	sort.Slice(redci, func(a, b int) bool { return redci[a].Vrijeme.Before(redci[b].Vrijeme) })

	mapa := filepath.Join(koren, sliv, letva)
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		return "", err
	}
	put := filepath.Join(mapa, KanonskoIme(letva, izvor, velicina, vrsta,
		redci[0].Vrijeme, redci[len(redci)-1].Vrijeme))

	privremeno := put + ".nova"
	if err := zapisiCSV(privremeno, velicina, redci); err != nil {
		os.Remove(privremeno)
		return "", err
	}
	if err := provjeriZapisano(privremeno, redci); err != nil {
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
	for _, s := range stare {
		if s != put {
			os.Remove(s)
		}
	}
	return put, nil
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
	stare, err := PostojeciRedci(koren, sliv, letva, izvor, velicina, vrsta)
	if err != nil {
		return "", err
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
// se ulaže na prazno i to nije greška.
func PostojeciRedci(koren, sliv, letva, izvor, velicina, vrsta string) ([]Redak, error) {
	uzorak := filepath.Join(koren, sliv, letva,
		strings.Join([]string{letva, izvor, velicina, vrsta}, "_")+"_*.csv")
	puts, err := filepath.Glob(uzorak)
	if err != nil || len(puts) == 0 {
		return nil, nil
	}
	sort.Strings(puts)
	var out []Redak
	for _, p := range puts {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		prvi := true
		poDanu := false
		for sc.Scan() {
			redak := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
			if prvi {
				poDanu = strings.HasPrefix(strings.ToLower(redak), "datum")
				prvi = false
				continue
			}
			if redak == "" {
				continue
			}
			dj := strings.SplitN(redak, ";", 2)
			if len(dj) != 2 {
				continue
			}
			t, err := vrijemeIzCSV(dj[0])
			if err != nil {
				continue
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(dj[1], ",", ".")), 64)
			if err != nil {
				continue
			}
			out = append(out, Redak{Vrijeme: t, PoDanu: poDanu, Vrijednost: v})
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func vrijemeIzCSV(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 19 {
		return time.Parse("2006-01-02 15:04:05", s[:19])
	}
	return time.Parse("2006-01-02", s)
}

// zaglavlje bira prvi stupac po tome ima li niz sat. Gradnja po tome razlikuje
// vrijednost vezanu uz trenutak od one vezane uz dan.
func zaglavlje(velicina string, redci []Redak) string {
	prvi := "vrijeme_utc"
	if len(redci) > 0 && redci[0].PoDanu {
		prvi = "datum"
	}
	drugi := map[string]string{
		"vodostaj": "vodostaj_cm", "protok": "protok_m3s", "temperatura": "temperatura_c",
		"koncentracija": "koncentracija_gl", "pronos": "pronos_kgs",
	}[velicina]
	if drugi == "" {
		drugi = velicina
	}
	return prvi + ";" + drugi
}

func zapisiCSV(put, velicina string, redci []Redak) error {
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
		if _, err := fmt.Fprintf(w, "%s;%s\n", vrijemeUCSV(r), brojUCSV(r.Vrijednost)); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func vrijemeUCSV(r Redak) string {
	if r.PoDanu {
		return r.Vrijeme.UTC().Format("2006-01-02")
	}
	return r.Vrijeme.UTC().Format("2006-01-02 15:04:05")
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
func provjeriZapisano(put string, redci []Redak) error {
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
		ocekivano := vrijemeUCSV(redci[n]) + ";" + brojUCSV(redci[n].Vrijednost)
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
