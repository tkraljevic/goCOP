package main

// Kopija baze prije nadogradnje. Postava (internal/postava, Ugradi) i
// instalacija za Linux (build/linux/instaliraj.sh) kopiraju bazu prije nego
// zamijene program, ali čvor u slici za Docker, čvor koji netko pokrene ručno
// i čvor koji je nadograđen mimo Postave kopiju nisu imali. Zato je sad radi
// i sam program: kad bazu otvara drugo izdanje od onoga koje ju je zadnje
// otvorilo, prvo je kopira u mapu kopije uz bazu, istim imenom i istim
// brojem kopija kao Postava. Ako je Postava ili instalacija kopiju upravo
// napravila, druga se ne radi. Uz glavnu bazu kopira se i baza sadržaja
// (sadrzaj.db): izdanje koje je bajtove PDF-ova i slika preselilo u datoteke
// ostavlja je takvu da prethodno izdanje u njoj više ne nalazi ništa.

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
	// imeKopijeBaze i imeKopijeSadrzaja su početak imena kopije glavne baze
	// (kao kod Postave) i kopije baze sadržaja
	imeKopijeBaze     = "gocop"
	imeKopijeSadrzaja = "sadrzaj"
	// cuvajKopija: toliko najnovijih kopija ostaje, kao kod Postave
	cuvajKopija = 3
	// svjezaKopija: kopija mlađa od ovoga znači da je bazu upravo kopirala
	// Postava ili instalacija, dok čvor nije radio
	svjezaKopija = 10 * time.Minute
)

// kopijaUzPokretanje je kopija baze prije prvog pokretanja ovog izdanja,
// uz redak u dnevniku kad je kopija napravljena
func kopijaUzPokretanje(baza string) error {
	kopije, err := kopijaPrijeNadogradnje(baza, izdanjePrograma(), time.Now())
	for _, kopija := range kopije {
		log.Printf("Prije prvog pokretanja izdanja %s baza je kopirana u %s", izdanjePrograma(), kopija)
	}
	return err
}

// putSadrzaja je baza sadržaja (popis PDF-ova i slika) uz glavnu bazu
func putSadrzaja(baza string) string {
	return filepath.Join(filepath.Dir(baza), "sadrzaj.db")
}

// izdanjePrograma je izdanje bez oznake commita, isto kao u imenu kopije
// koje daje Postava. Slika za Docker zadaje version s oznakom commita
// ("0.0.70-alfa (abc1234)"), pa se oznaka ovdje reže.
func izdanjePrograma() string {
	if version == "" {
		return verzijaPrograma
	}
	izdanje, _, _ := strings.Cut(version, " (")
	return izdanje
}

// kopijaPrijeNadogradnje kopira bazu na putu baza i bazu sadržaja uz nju
// prije nego ih otvori izdanje izdanje, kad ih je zadnje otvorilo drugo
// izdanje (ili se ne zna koje). Za novu bazu samo zapamti izdanje. Vraća
// putove napravljenih kopija. Greška znači da kopije nema, pa čvor ne smije
// krenuti: novo izdanje može promijeniti bazu tako da je prethodno više ne
// otvara.
func kopijaPrijeNadogradnje(baza, izdanje string, sada time.Time) (kopije []string, err error) {
	oznaka := filepath.Join(filepath.Dir(baza), izdanjeBazeDatoteka)
	staro, err := procitajIzdanjeBaze(oznaka)
	if err != nil {
		return nil, err
	}
	if staro == izdanje {
		return nil, nil
	}
	za := []struct{ put, ime string }{{baza, imeKopijeBaze}, {putSadrzaja(baza), imeKopijeSadrzaja}}
	for _, b := range za {
		kopija, err := kopirajAkoPostoji(b.put, b.ime, staro, sada)
		if err != nil {
			return nil, fmt.Errorf("kopija baze prije prvog pokretanja izdanja %s nije spremljena: %w", izdanje, err)
		}
		if kopija != "" {
			kopije = append(kopije, kopija)
		}
	}
	return kopije, trajno.Zapisi(oznaka, []byte(izdanje+"\n"), 0o600)
}

// kopirajAkoPostoji kopira bazu koja postoji; za bazu koje još nema nema
// što kopirati
func kopirajAkoPostoji(baza, ime, staro string, sada time.Time) (string, error) {
	if _, err := os.Stat(baza); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return kopirajBazu(baza, ime, staro, sada)
}

// procitajIzdanjeBaze je zapamćeno izdanje; "" kad ga još nema
func procitajIzdanjeBaze(oznaka string) (string, error) {
	b, err := os.ReadFile(oznaka)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return strings.TrimSpace(string(b)), err
}

// kopirajBazu sprema bazu (i njezin WAL) u mapu kopije uz nju, pod imenom
// koje počinje s ime, i ostavlja tri najnovije takve; vraća put kopije, ili
// "" kad je svježa kopija već tu
func kopirajBazu(baza, ime, staro string, sada time.Time) (string, error) {
	mapa := filepath.Join(filepath.Dir(baza), mapaKopija)
	if err := os.MkdirAll(mapa, 0o700); err != nil {
		return "", err
	}
	kopije, err := kopijeU(mapa, ime)
	if err != nil {
		return "", err
	}
	if len(kopije) > 0 && sada.Sub(kopije[0].kad) < svjezaKopija {
		return "", nil
	}
	put := filepath.Join(mapa, fmt.Sprintf("%s-%s-%s.db", ime, sada.Format("20060102-150405"), imeIzdanja(staro)))
	for _, nastavak := range []string{"", "-wal"} {
		if _, err := os.Stat(baza + nastavak); err != nil {
			continue
		}
		if err := kopirajDatoteku(baza+nastavak, put+nastavak); err != nil {
			return "", err
		}
	}
	return put, ostaviNajnovije(mapa, ime)
}

// ostaviNajnovije briše sve kopije tog imena u mapi osim tri najnovije
func ostaviNajnovije(mapa, ime string) error {
	kopije, err := kopijeU(mapa, ime)
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

// kopijeU su kopije baze u mapi (<ime>-*.db), najnovija prva
func kopijeU(mapa, ime string) ([]kopijaBaze, error) {
	stavke, err := os.ReadDir(mapa)
	if err != nil {
		return nil, err
	}
	var out []kopijaBaze
	for _, s := range stavke {
		if !strings.HasPrefix(s.Name(), ime+"-") || !strings.HasSuffix(s.Name(), ".db") {
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
