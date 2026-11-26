package main

// Kopija baze prije nadogradnje. Postava (internal/postava, Ugradi) i
// instalacija za Linux (build/linux/instaliraj.sh) kopiraju bazu prije nego
// zamijene program, ali čvor u slici za Docker, čvor koji netko pokrene ručno
// i čvor koji je nadograđen mimo Postave kopiju nisu imali. Zato je sad radi
// i sam program: kad bazu otvara drugo izdanje od onoga koje ju je zadnje
// otvorilo, prvo je kopira u mapu kopije uz bazu, istim imenom i istim
// brojem kopija kao Postava. Ako je Postava ili instalacija kopiju upravo
// napravila, druga se ne radi.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gocop/internal/trajno"
)

const (
	// izdanjeBazeDatoteka uz bazu pamti izdanje programa koje je bazu
	// zadnje otvorilo
	izdanjeBazeDatoteka = "izdanje-baze.txt"
	// mapaKopija je mapa uz bazu, ista kao kod Postave i instalacije
	mapaKopija = "kopije"
	// cuvajKopija: toliko najnovijih kopija ostaje, kao kod Postave
	cuvajKopija = 3
	// svjezaKopija: kopija mlađa od ovoga znači da je bazu upravo kopirala
	// Postava ili instalacija, dok čvor nije radio
	svjezaKopija = 10 * time.Minute
)

// kopijaUzPokretanje je kopija baze prije prvog pokretanja ovog izdanja,
// uz redak u dnevniku kad je kopija napravljena
func kopijaUzPokretanje(baza string) error {
	kopija, err := kopijaPrijeNadogradnje(baza, izdanjePrograma(), time.Now())
	if kopija != "" {
		log.Printf("Prije prvog pokretanja izdanja %s baza je kopirana u %s", izdanjePrograma(), kopija)
	}
	return err
}

// izdanjePrograma je izdanje bez oznake commita, isto kao u imenu kopije
// koje daje Postava
func izdanjePrograma() string {
	if version != "" {
		return version
	}
	return verzijaPrograma
}

// kopijaPrijeNadogradnje kopira bazu na putu baza prije nego je otvori
// izdanje izdanje, kad ju je zadnje otvorilo drugo izdanje (ili se ne zna
// koje). Za novu bazu samo zapamti izdanje. Greška znači da kopije nema, pa
// čvor ne smije krenuti: novo izdanje može promijeniti bazu tako da je
// prethodno više ne otvara.
func kopijaPrijeNadogradnje(baza, izdanje string, sada time.Time) (kopija string, err error) {
	oznaka := filepath.Join(filepath.Dir(baza), izdanjeBazeDatoteka)
	staro, err := procitajIzdanjeBaze(oznaka)
	if err != nil {
		return "", err
	}
	if staro == izdanje {
		return "", nil
	}
	if _, err := os.Stat(baza); err == nil {
		if kopija, err = kopirajBazu(baza, staro, sada); err != nil {
			return "", fmt.Errorf("kopija baze prije prvog pokretanja izdanja %s nije spremljena: %w", izdanje, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return kopija, trajno.Zapisi(oznaka, []byte(izdanje+"\n"), 0o600)
}

// procitajIzdanjeBaze je zapamćeno izdanje; "" kad ga još nema
func procitajIzdanjeBaze(oznaka string) (string, error) {
	b, err := os.ReadFile(oznaka)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return strings.TrimSpace(string(b)), err
}

// kopirajBazu sprema bazu (i njezin WAL) u mapu kopije uz nju i ostavlja
// tri najnovije; vraća put kopije, ili "" kad je svježa kopija već tu
func kopirajBazu(baza, staro string, sada time.Time) (string, error) {
	mapa := filepath.Join(filepath.Dir(baza), mapaKopija)
	if err := os.MkdirAll(mapa, 0o700); err != nil {
		return "", err
	}
	kopije, err := kopijeU(mapa)
	if err != nil {
		return "", err
	}
	if len(kopije) > 0 && sada.Sub(kopije[0].kad) < svjezaKopija {
		return "", nil
	}
	put := filepath.Join(mapa, fmt.Sprintf("gocop-%s-%s.db", sada.Format("20060102-150405"), imeIzdanja(staro)))
	for _, nastavak := range []string{"", "-wal"} {
		if _, err := os.Stat(baza + nastavak); err != nil {
			continue
		}
		if err := kopirajDatoteku(baza+nastavak, put+nastavak); err != nil {
			return "", err
		}
	}
	return put, ostaviNajnovije(mapa)
}

// ostaviNajnovije briše sve kopije u mapi osim tri najnovije
func ostaviNajnovije(mapa string) error {
	kopije, err := kopijeU(mapa)
	if err != nil {
		return err
	}
	for _, k := range kopije[min(cuvajKopija, len(kopije)):] {
		_ = os.Remove(k.put)          // stara kopija; ako ostane, briše je sljedeće čišćenje
		_ = os.Remove(k.put + "-wal") // isto, a WAL-a često ni nema
	}
	return nil
}

// imeIzdanja je izdanje za ime datoteke: samo slova, brojke, točka i
// crtica; nepoznato kad ga nema
func imeIzdanja(izdanje string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		}
		return -1
	}, izdanje)
	if out == "" {
		return "nepoznato"
	}
	return out
}

type kopijaBaze struct {
	put string
	kad time.Time
}

// kopijeU su kopije baze u mapi (gocop-*.db), najnovija prva
func kopijeU(mapa string) ([]kopijaBaze, error) {
	stavke, err := os.ReadDir(mapa)
	if err != nil {
		return nil, err
	}
	var out []kopijaBaze
	for _, s := range stavke {
		if !strings.HasPrefix(s.Name(), "gocop-") || !strings.HasSuffix(s.Name(), ".db") {
			continue
		}
		if info, err := s.Info(); err == nil {
			out = append(out, kopijaBaze{put: filepath.Join(mapa, s.Name()), kad: info.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].kad.After(out[j].kad) })
	return out, nil
}

// kopirajDatoteku kopira iz u na disk, pa tek onda daje ime: napola
// kopirana datoteka nikad ne nosi ime kopije
func kopirajDatoteku(iz, u string) error {
	src, err := os.Open(iz)
	if err != nil {
		return err
	}
	defer src.Close() //nolint:errcheck // datoteka se samo čita
	dst, err := os.OpenFile(u+".dio", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if errZ := trajno.SpustiIZatvori(dst); err == nil {
		err = errZ
	}
	if err != nil {
		_ = os.Remove(u + ".dio") // napola kopirana; važnija je greška kopije
		return err
	}
	return trajno.Preimenuj(u+".dio", u)
}
