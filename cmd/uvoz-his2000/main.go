// uvoz-his2000 pretvara izvoz jedne postaje iz HIS-2000 u datoteke kakve
// očekuje stablo vodostaji/: nizove po veličini i gustoći, krivulje protoka i
// snimke poprečnog profila korita.
//
// HIS ne daje isti naziv datoteke dvaput — ime bira onaj tko izvozi — pa se
// svaka datoteka prepoznaje po zaglavlju, a ne po imenu.
//
//	uvoz-his2000 -iz ~/Downloads/HIS2000-download/Dunav-Dalj -letva dalj -sliv dunav -probno
//
// Zatečeni niz se ne gazi: kad novi izvoz ima manje od onoga što već imamo,
// program to javi i datoteku ostavi na miru. Kad se vrijednosti na istim
// trenucima razlikuju, ne dira ništa dok mu se ne kaže -zamijeni.
package main

import (
	"flag"
	"fmt"
	"gocop/internal/uvoz/his2000"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	iz := flag.String("iz", "", "mapa s izvozom jedne postaje iz HIS-2000")
	u := flag.String("u", "vodostaji", "korijen stabla s datotekama")
	letva := flag.String("letva", "", "šifra letve, npr. dalj")
	sliv := flag.String("sliv", "", "sliv, npr. dunav")
	izvor := flag.String("izvor", "his2000", "oznaka izvora u nazivu datoteke")
	probno := flag.Bool("probno", false, "samo ispiši što bi se zapisalo")
	zamijeni := flag.Bool("zamijeni", false, "prepiši zatečeni niz i kad se vrijednosti razlikuju")
	flag.Parse()

	if *iz == "" || *letva == "" || *sliv == "" {
		log.Fatal("trebaju -iz, -letva i -sliv")
	}
	stavke, err := os.ReadDir(*iz)
	if err != nil {
		log.Fatal(err)
	}
	cilj := filepath.Join(*u, *sliv, *letva)

	var nizovi []*his2000.Sadrzaj
	var krivulje *his2000.Sadrzaj
	var profili []*his2000.Sadrzaj
	var preskoceno []string
	var mjerenja []his2000.Mjerenje
	vidjeniProfili := map[time.Time]string{} // snimka istog dana zna doći dvaput

	imena := make([]string, 0, len(stavke))
	for _, s := range stavke {
		nastavak := strings.ToLower(filepath.Ext(s.Name()))
		if !s.IsDir() && (nastavak == ".csv" || nastavak == ".xls" || nastavak == ".txt") {
			imena = append(imena, s.Name())
		}
	}
	sort.Strings(imena)

	for _, ime := range imena {
		sirovo, err := os.ReadFile(filepath.Join(*iz, ime))
		if err != nil {
			log.Fatal(err)
		}
		if his2000.JeSylk(sirovo) {
			m, err := his2000.ProcitajMjerenja(sirovo)
			if err != nil {
				preskoceno = append(preskoceno, fmt.Sprintf("%s — %v", ime, err))
				continue
			}
			mjerenja = append(mjerenja, m...)
			continue
		}
		s, err := his2000.Procitaj(ime, sirovo)
		if err != nil {
			preskoceno = append(preskoceno, fmt.Sprintf("%s — %v", ime, err))
			continue
		}
		switch {
		case s.Profil != nil:
			if prije, ima := vidjeniProfili[s.Profil.Datum]; ima {
				preskoceno = append(preskoceno, fmt.Sprintf("%s — ista snimka korita kao %s", ime, prije))
				continue
			}
			vidjeniProfili[s.Profil.Datum] = ime
			profili = append(profili, s)
		case s.Krivulje != nil:
			if krivulje == nil || len(s.Krivulje) > len(krivulje.Krivulje) {
				krivulje = s
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

	posao := &Posao{Cilj: cilj, Letva: *letva, Izvor: *izvor, Probno: *probno, Zamijeni: *zamijeni}
	for _, s := range nizovi {
		posao.Niz(s)
	}
	if krivulje != nil {
		posao.Krivulje(krivulje, filepath.Join(*iz, krivulje.Ime))
	}
	for _, s := range profili {
		posao.Profil(s)
	}
	if len(mjerenja) > 0 {
		posao.Mjerenja(mjerenja)
	}
	for _, r := range preskoceno {
		fmt.Printf("  preskočeno: %s\n", r)
	}
	fmt.Printf("\n%s\n", posao.Sazetak())
}

// Posao zapisuje pročitano u stablo i broji što je učinio.
type Posao struct {
	Cilj, Letva, Izvor string
	Probno, Zamijeni   bool

	Zapisano, Ostavljeno, Sporno int
}

// Niz zapisuje jedan vremenski niz pod dogovorenim nazivom.
func (p *Posao) Niz(s *his2000.Sadrzaj) {
	od, do_ := s.Niz[0].Kad.Year(), s.Niz[len(s.Niz)-1].Kad.Year()
	ime := fmt.Sprintf("%s_%s_%s_%s_%d-%d.csv", p.Letva, p.Izvor, s.Vrsta.Velicina, s.Vrsta.Gustoca, od, do_)
	put := filepath.Join(p.Cilj, ime)
	opis := fmt.Sprintf("%-12s %-9s %7d  %s .. %s", s.Vrsta.Velicina, s.Vrsta.Gustoca,
		len(s.Niz), s.Niz[0].Kad.Format("2006-01-02"), s.Niz[len(s.Niz)-1].Kad.Format("2006-01-02"))
	if s.Preskoceno > 0 {
		opis += fmt.Sprintf("  (preskočeno %d sati kojih u lokalnom vremenu nema)", s.Preskoceno)
	}
	fmt.Printf("%s\n              → %s\n", opis, ime)

	if zat, err := zatecen(p.Cilj, p.Letva, p.Izvor, s.Vrsta); err == nil && zat != "" {
		stari, err := ucitaj(filepath.Join(p.Cilj, zat))
		if err != nil {
			log.Fatalf("%s: %v", zat, err)
		}
		suk, samoStari, nepostojeci := his2000.Usporedi(stari, s.Niz)
		if suk > 0 && !p.Zamijeni {
			fmt.Printf("              zatečeno %s: %d vrijednosti se razlikuje — ostavljam kako jest, -zamijeni ako treba drugačije\n", zat, suk)
			p.Sporno++
			return
		}
		if samoStari > 0 {
			fmt.Printf("              zatečeno %s ima %d vrijednosti kojih u novom izvozu nema — ostavljam kako jest\n", zat, samoStari)
			p.Ostavljeno++
			return
		}
		if nepostojeci > 0 {
			fmt.Printf("              zatečeno %s ima %d sati kojih u lokalnom vremenu nema — ne računaju se kao gubitak\n", zat, nepostojeci)
		}
		// naziv nosi godine, pa se pri proširenju niza datoteka zove drugačije
		// i staru treba maknuti, inače bi arhiva čitala obje
		if zat != filepath.Base(put) {
			fmt.Printf("              zatečeno %s zamjenjujem novim rasponom\n", zat)
			if !p.Probno {
				if err := os.Remove(filepath.Join(p.Cilj, zat)); err != nil {
					log.Fatal(err)
				}
			}
		}
	}
	if p.Probno {
		p.Zapisano++
		return
	}
	if err := os.MkdirAll(p.Cilj, 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(put)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	stupacVremena := "vrijeme_utc"
	if s.Niz[0].Dan {
		stupacVremena = "datum"
	}
	fmt.Fprintf(f, "%s;%s\n", stupacVremena, s.Vrsta.Stupac)
	for _, v := range s.Niz {
		if v.Dan {
			fmt.Fprintf(f, "%s;%s\n", v.Kad.Format("2006-01-02"), v.V)
		} else {
			fmt.Fprintf(f, "%s;%s\n", v.Kad.Format("2006-01-02 15:04:05"), v.V)
		}
	}
	p.Zapisano++
}

// Krivulje zapisuje odsječke u dogovoreni oblik i čuva izvornik.
func (p *Posao) Krivulje(s *his2000.Sadrzaj, izvornik string) {
	odsjecaka := 0
	for _, k := range s.Krivulje {
		odsjecaka += len(k.Odsjecci)
	}
	ime := fmt.Sprintf("hq/%s_hq_krivulje.csv", p.Letva)
	fmt.Printf("%-12s %-9s %7d  %s .. %s\n              → %s\n", "krivulje", "protok", odsjecaka,
		s.Krivulje[0].Od.Format("2006"), s.Krivulje[len(s.Krivulje)-1].Do.Format("2006"), ime)
	if s.Postaja.KotaNule != "" {
		fmt.Printf("              uz krivulje stoji kota nule %s m i koordinate %s / %s\n",
			s.Postaja.KotaNule, s.Postaja.Sirina, s.Postaja.Duzina)
	}
	if p.Probno {
		p.Zapisano++
		return
	}
	hq := filepath.Join(p.Cilj, "hq")
	if err := os.MkdirAll(filepath.Join(hq, "izvornik"), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(filepath.Join(p.Cilj, ime))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintln(f, "vrijedi_od;vrijedi_do;od_cm;do_cm;oblik;p1;p2;p3;izvor;napomena")
	for i, k := range s.Krivulje {
		// Zadnjoj krivulji kraj ostaje otvoren: DHMZ ga upiše na kraj tekuće
		// godine, a krivulja vrijedi dok ne objave novu. Sa zapisanim krajem
		// protok bi na Silvestrovo prestao imati krivulju.
		do_ := k.Do.Format("2006-01-02")
		if i == len(s.Krivulje)-1 {
			do_ = ""
		}
		for _, o := range k.Odsjecci {
			fmt.Fprintf(f, "%s;%s;%d;%d;polinom;%s;%s;%s;DHMZ, HIS-2000;\n",
				k.Od.Format("2006-01-02"), do_, o.OdCm, o.DoCm, o.P1, o.P2, o.P3)
		}
	}
	if err := prepisi(izvornik, filepath.Join(hq, "izvornik", filepath.Base(izvornik))); err != nil {
		log.Fatal(err)
	}
	p.Zapisano++
}

// Profil zapisuje jednu snimku korita pod datumom mjerenja.
func (p *Posao) Profil(s *his2000.Sadrzaj) {
	pr := s.Profil
	ime := fmt.Sprintf("profil/%s_profil_%s.csv", p.Letva, pr.Datum.Format("2006-01-02"))
	fmt.Printf("%-12s %-9s %7d  %s, vodostaj %d cm\n              → %s\n",
		"profil", "korito", len(pr.Tocke), pr.Datum.Format("2006-01-02"), pr.Vodostaj, ime)
	if p.Probno {
		p.Zapisano++
		return
	}
	if err := os.MkdirAll(filepath.Join(p.Cilj, "profil"), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(filepath.Join(p.Cilj, ime))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "# poprečni profil korita, mjereno %s, vodostaj pri mjerenju %d cm, kota nule %s\n",
		pr.Datum.Format("2006-01-02"), pr.Vodostaj, pr.KotaNule)
	fmt.Fprintln(f, "stacionaza_m;visina_m")
	for _, t := range pr.Tocke {
		fmt.Fprintf(f, "%s;%s\n", t.Stacionaza, t.Visina)
	}
	p.Zapisano++
}

// Mjerenja zapisuje vodomjerenja uz krivulje, jer im ondje i služe.
func (p *Posao) Mjerenja(m []his2000.Mjerenje) {
	sort.SliceStable(m, func(i, j int) bool { return m[i].Datum.Before(m[j].Datum) })
	ime := fmt.Sprintf("hq/%s_vodomjerenja_%d-%d.csv", p.Letva, m[0].Datum.Year(), m[len(m)-1].Datum.Year())
	fmt.Printf("%-12s %-9s %7d  %s .. %s\n              → %s\n", "vodomjerenja", "protok", len(m),
		m[0].Datum.Format("2006-01-02"), m[len(m)-1].Datum.Format("2006-01-02"), ime)
	if p.Probno {
		p.Zapisano++
		return
	}
	if err := os.MkdirAll(filepath.Join(p.Cilj, "hq"), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(filepath.Join(p.Cilj, ime))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintln(f, "datum;vodostaj_cm;srednja_brzina_ms;protok_m3s;metoda")
	for _, v := range m {
		fmt.Fprintf(f, "%s;%s;%s;%s;%s\n", v.Datum.Format("2006-01-02"), v.Vodostaj, v.Brzina, v.Protok, v.Metoda)
	}
	p.Zapisano++
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
func zatecen(cilj, letva, izvor string, v his2000.Vrsta) (string, error) {
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

func prepisi(iz, u string) error {
	b, err := os.ReadFile(iz)
	if err != nil {
		return err
	}
	return os.WriteFile(u, b, 0o644)
}
