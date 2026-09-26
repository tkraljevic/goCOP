package his2000

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Datoteka je jedna datoteka izvoza, kako je stigla: s diska ili kroz
// preglednik.
type Datoteka struct {
	Ime     string
	Sadrzaj []byte
}

// Posao zapisuje pročitano u stablo i broji što je učinio. Dnevnik prima isti
// ispis koji je naredba davala na zaslon, pa ga aplikacija pokaže uz posao.
type Posao struct {
	Cilj, Letva, Izvor string
	Probno, Zamijeni   bool
	Dnevnik            io.Writer

	Zapisano, Ostavljeno, Sporno int
}

func (p *Posao) pisi(format string, a ...any) {
	if p.Dnevnik != nil {
		fmt.Fprintf(p.Dnevnik, format, a...)
	}
}

// Uvezi razvrsta datoteke izvoza jedne postaje i zapiše ih: nizove,
// krivulje, snimke korita i vodomjerenja. HIS ne daje isti naziv datoteke
// dvaput — ime bira onaj tko izvozi — pa se svaka prepoznaje po zaglavlju.
// Vraća opise preskočenih datoteka; greška znači da zapis nije uspio.
func (p *Posao) Uvezi(datoteke []Datoteka) ([]string, error) {
	var nizovi []*Sadrzaj
	var krivulje *Sadrzaj
	var krivuljeIzvornik []byte
	var profili []*Sadrzaj
	var preskoceno []string
	var mjerenja []Mjerenje
	vidjeniProfili := map[time.Time]int{} // snimka istog dana zna doći dvaput; broj je mjesto u popisu

	dat := make([]Datoteka, 0, len(datoteke))
	for _, d := range datoteke {
		nastavak := strings.ToLower(filepath.Ext(d.Ime))
		if nastavak == ".csv" || nastavak == ".xls" || nastavak == ".txt" {
			dat = append(dat, d)
		}
	}
	sort.SliceStable(dat, func(i, j int) bool { return dat[i].Ime < dat[j].Ime })

	for _, d := range dat {
		ime, sirovo := d.Ime, d.Sadrzaj
		if JeSylk(sirovo) {
			m, err := ProcitajMjerenja(sirovo)
			if err != nil {
				preskoceno = append(preskoceno, fmt.Sprintf("%s — %v", ime, err))
				continue
			}
			mjerenja = append(mjerenja, m...)
			continue
		}
		s, err := Procitaj(ime, sirovo)
		if err != nil {
			preskoceno = append(preskoceno, fmt.Sprintf("%s — %v", ime, err))
			continue
		}
		switch {
		case s.Profil != nil:
			// Dvije snimke istog dana nisu nužno ista snimka: Botovo je
			// 15.03.2016. imalo jednu s 211 i jednu sa 153 točke. Zadržava se
			// bogatija, a ne prva po redu — inače ishod ovisi o tome kojim je
			// redoslijedom izvoz složen.
			if j, ima := vidjeniProfili[s.Profil.Datum]; ima {
				if len(s.Profil.Tocke) <= len(profili[j].Profil.Tocke) {
					preskoceno = append(preskoceno, fmt.Sprintf(
						"%s (%d točaka) — ista snimka korita kao %s, koja ih ima %d",
						ime, len(s.Profil.Tocke), profili[j].Ime, len(profili[j].Profil.Tocke)))
					continue
				}
				preskoceno = append(preskoceno, fmt.Sprintf(
					"%s (%d točaka) — ista snimka korita kao %s, koja ih ima %d",
					profili[j].Ime, len(profili[j].Profil.Tocke), ime, len(s.Profil.Tocke)))
				profili[j] = s
				continue
			}
			vidjeniProfili[s.Profil.Datum] = len(profili)
			profili = append(profili, s)
		case s.Krivulje != nil:
			if krivulje == nil || len(s.Krivulje) > len(krivulje.Krivulje) {
				krivulje, krivuljeIzvornik = s, sirovo
			}
		case len(s.Niz) > 0:
			nizovi = append(nizovi, s)
		default:
			raspon := ""
			if s.OdGodine > 0 {
				raspon = fmt.Sprintf(" za %d.-%d.", s.OdGodine, s.DoGodine)
			}
			preskoceno = append(preskoceno, fmt.Sprintf("%s — izvoz je prošao%s, ali mjerenja nema nijednog", ime, raspon))
		}
	}

	for _, s := range nizovi {
		if err := p.Niz(s); err != nil {
			return preskoceno, err
		}
	}
	if krivulje != nil {
		if err := p.Krivulje(krivulje, krivuljeIzvornik); err != nil {
			return preskoceno, err
		}
	}
	for _, s := range profili {
		if err := p.Profil(s); err != nil {
			return preskoceno, err
		}
	}
	if len(mjerenja) > 0 {
		if err := p.Mjerenja(mjerenja); err != nil {
			return preskoceno, err
		}
	}
	for _, r := range preskoceno {
		p.pisi("  preskočeno: %s\n", r)
	}
	p.pisi("\n%s\n", p.Sazetak())
	return preskoceno, nil
}

// Niz zapisuje jedan vremenski niz pod dogovorenim nazivom. Zatečeni niz se
// ne gazi: kad novi izvoz ima manje od onoga što već imamo, to se javi i
// datoteka ostaje; kad se vrijednosti razlikuju, ostaje dok nije Zamijeni.
func (p *Posao) Niz(s *Sadrzaj) error {
	od, do_ := s.Niz[0].Kad.Year(), s.Niz[len(s.Niz)-1].Kad.Year()
	ime := fmt.Sprintf("%s_%s_%s_%s_%d-%d.csv", p.Letva, p.Izvor, s.Vrsta.Velicina, s.Vrsta.Gustoca, od, do_)
	put := filepath.Join(p.Cilj, ime)
	opis := fmt.Sprintf("%-12s %-9s %7d  %s .. %s", s.Vrsta.Velicina, s.Vrsta.Gustoca,
		len(s.Niz), s.Niz[0].Kad.Format("2006-01-02"), s.Niz[len(s.Niz)-1].Kad.Format("2006-01-02"))
	if s.Preskoceno > 0 {
		opis += fmt.Sprintf("  (preskočeno %d sati kojih u lokalnom vremenu nema)", s.Preskoceno)
	}
	p.pisi("%s\n              → %s\n", opis, ime)

	if zat, err := zatecen(p.Cilj, p.Letva, p.Izvor, s.Vrsta); err == nil && zat != "" {
		stari, err := ucitaj(filepath.Join(p.Cilj, zat))
		if err != nil {
			return fmt.Errorf("%s: %w", zat, err)
		}
		suk, samoStari, nepostojeci := Usporedi(stari, s.Niz)
		if suk > 0 && !p.Zamijeni {
			p.pisi("              zatečeno %s: %d vrijednosti se razlikuje — ostavljam kako jest, -zamijeni ako treba drugačije\n", zat, suk)
			p.Sporno++
			return nil
		}
		if samoStari > 0 {
			p.pisi("              zatečeno %s ima %d vrijednosti kojih u novom izvozu nema — ostavljam kako jest\n", zat, samoStari)
			p.Ostavljeno++
			return nil
		}
		if nepostojeci > 0 {
			p.pisi("              zatečeno %s ima %d sati kojih u lokalnom vremenu nema — ne računaju se kao gubitak\n", zat, nepostojeci)
		}
		// naziv nosi godine, pa se pri proširenju niza datoteka zove drugačije
		// i staru treba maknuti, inače bi arhiva čitala obje
		if zat != filepath.Base(put) {
			p.pisi("              zatečeno %s zamjenjujem novim rasponom\n", zat)
			if !p.Probno {
				if err := os.Remove(filepath.Join(p.Cilj, zat)); err != nil {
					return err
				}
			}
		}
	}
	if p.Probno {
		p.Zapisano++
		return nil
	}
	if err := os.MkdirAll(p.Cilj, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	stupacVremena := "vrijeme_utc"
	if s.Niz[0].Dan {
		stupacVremena = "datum"
	}
	fmt.Fprintf(&b, "%s;%s\n", stupacVremena, s.Vrsta.Stupac)
	for _, v := range s.Niz {
		if v.Dan {
			fmt.Fprintf(&b, "%s;%s\n", v.Kad.Format("2006-01-02"), v.V)
		} else {
			fmt.Fprintf(&b, "%s;%s\n", v.Kad.Format("2006-01-02 15:04:05"), v.V)
		}
	}
	if err := os.WriteFile(put, []byte(b.String()), 0o644); err != nil {
		return err
	}
	p.Zapisano++
	return nil
}

// Krivulje zapisuje odsječke u dogovoreni oblik i čuva izvornik.
func (p *Posao) Krivulje(s *Sadrzaj, izvornik []byte) error {
	odsjecaka := 0
	for _, k := range s.Krivulje {
		odsjecaka += len(k.Odsjecci)
	}
	ime := fmt.Sprintf("hq/%s_hq_krivulje.csv", p.Letva)
	p.pisi("%-12s %-9s %7d  %s .. %s\n              → %s\n", "krivulje", "protok", odsjecaka,
		s.Krivulje[0].Od.Format("2006"), s.Krivulje[len(s.Krivulje)-1].Do.Format("2006"), ime)
	if s.Postaja.KotaNule != "" {
		p.pisi("              uz krivulje stoji kota nule %s m i koordinate %s / %s\n",
			s.Postaja.KotaNule, s.Postaja.Sirina, s.Postaja.Duzina)
	}
	if p.Probno {
		p.Zapisano++
		return nil
	}
	hq := filepath.Join(p.Cilj, "hq")
	if err := os.MkdirAll(filepath.Join(hq, "izvornik"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(p.Cilj, ime), KrivuljeCSV(s), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(hq, "izvornik", filepath.Base(s.Ime)), izvornik, 0o644); err != nil {
		return err
	}
	p.Zapisano++
	return nil
}

// Profil zapisuje jednu snimku korita pod datumom mjerenja.
func (p *Posao) Profil(s *Sadrzaj) error {
	pr := s.Profil
	ime := fmt.Sprintf("profil/%s_profil_%s.csv", p.Letva, pr.Datum.Format("2006-01-02"))
	p.pisi("%-12s %-9s %7d  %s, vodostaj %d cm\n              → %s\n",
		"profil", "korito", len(pr.Tocke), pr.Datum.Format("2006-01-02"), pr.Vodostaj, ime)
	if p.Probno {
		p.Zapisano++
		return nil
	}
	if err := os.MkdirAll(filepath.Join(p.Cilj, "profil"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(p.Cilj, ime), ProfilCSV(pr), 0o644); err != nil {
		return err
	}
	p.Zapisano++
	return nil
}

// Mjerenja zapisuje vodomjerenja uz krivulje, jer im ondje i služe.
func (p *Posao) Mjerenja(m []Mjerenje) error {
	sort.SliceStable(m, func(i, j int) bool { return m[i].Datum.Before(m[j].Datum) })
	ime := fmt.Sprintf("hq/%s_vodomjerenja_%d-%d.csv", p.Letva, m[0].Datum.Year(), m[len(m)-1].Datum.Year())
	p.pisi("%-12s %-9s %7d  %s .. %s\n              → %s\n", "vodomjerenja", "protok", len(m),
		m[0].Datum.Format("2006-01-02"), m[len(m)-1].Datum.Format("2006-01-02"), ime)
	if p.Probno {
		p.Zapisano++
		return nil
	}
	if err := os.MkdirAll(filepath.Join(p.Cilj, "hq"), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintln(&b, "datum;vodostaj_cm;srednja_brzina_ms;protok_m3s;metoda")
	for _, v := range m {
		fmt.Fprintf(&b, "%s;%s;%s;%s;%s\n", v.Datum.Format("2006-01-02"), v.Vodostaj, v.Brzina, v.Protok, v.Metoda)
	}
	if err := os.WriteFile(filepath.Join(p.Cilj, ime), []byte(b.String()), 0o644); err != nil {
		return err
	}
	p.Zapisano++
	return nil
}

// Sazetak je jedan redak o cijelom poslu.
func (p *Posao) Sazetak() string {
	rijec := "zapisano"
	if p.Probno {
		rijec = "zapisalo bi se"
	}
	s := fmt.Sprintf("%s %d datoteka", rijec, p.Zapisano)
	if p.Ostavljeno > 0 {
		s += fmt.Sprintf(", ostavljeno zatečeno %d", p.Ostavljeno)
	}
	if p.Sporno > 0 {
		s += fmt.Sprintf(", sporno %d", p.Sporno)
	}
	return s
}

// zatecen traži datoteku iste letve, izvora, veličine i gustoće, bez obzira
// na godine u nazivu.
func zatecen(cilj, letva, izvor string, v Vrsta) (string, error) {
	predmetak := fmt.Sprintf("%s_%s_%s_%s_", letva, izvor, v.Velicina, v.Gustoca)
	stavke, err := os.ReadDir(cilj)
	if err != nil {
		return "", err
	}
	for _, s := range stavke {
		if strings.HasPrefix(s.Name(), predmetak) {
			return s.Name(), nil
		}
	}
	return "", nil
}

func ucitaj(put string) (map[time.Time]string, error) {
	b, err := os.ReadFile(put)
	if err != nil {
		return nil, err
	}
	out := map[time.Time]string{}
	for i, r := range strings.Split(string(b), "\n") {
		r = strings.TrimSpace(r)
		if i == 0 || r == "" {
			continue
		}
		d := strings.SplitN(r, ";", 2)
		if len(d) != 2 {
			continue
		}
		kad, err := time.Parse("2006-01-02 15:04:05", d[0])
		if err != nil {
			if kad, err = time.Parse("2006-01-02", d[0]); err != nil {
				continue
			}
		}
		out[kad] = d[1]
	}
	return out, nil
}
