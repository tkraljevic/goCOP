package sadrzaj

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// datoteke drže bajtove sadržaja izvan baze. Svaki sadržaj je jedna
// datoteka u mapi uz bazu, složena po vrsti i mjesecu primitka:
// sadrzaj/pdf/2026-11/<otisak>.pdf. Mjesto datoteke (relativno, s „/”)
// piše u popisu, u stupcu datoteka, pa ga promjena rasporeda ne izgubi.
// Datoteka se nikad ne mijenja, samo nastaje i nestaje. Memorijsko
// spremište (proba) drži bajtove u mapi u memoriji.
type datoteke struct {
	mapa string // prazno: u memoriji

	mu  sync.Mutex // samo za memoriju
	mem map[string][]byte
}

// MapaUz je mapa s datotekama sadržaja za spremište na putu put
func MapaUz(put string) string {
	return filepath.Join(filepath.Dir(put), "sadrzaj")
}

// ispravanOtisak: otisak stiže i s drugih čvorova, a od njega nastaje ime
// datoteke, pa smije biti samo SHA-256 heksom malim slovima
func ispravanOtisak(otisak string) bool {
	if len(otisak) != 64 || strings.ToLower(otisak) != otisak {
		return false
	}
	_, err := hex.DecodeString(otisak)
	return err == nil
}

// mapeVrsta su mapa i nastavak datoteke po vrsti sadržaja
var mapeVrsta = map[string][2]string{
	"application/pdf":              {"pdf", ".pdf"},
	"image/jpeg":                   {"slike", ".jpg"},
	"image/png":                    {"slike", ".png"},
	"application/vnd.gocop.arhiva": {"paketi-arhive", ".cop"},
}

// noviPut je mjesto nove datoteke: mapa vrste, mjesec primitka, otisak s
// nastavkom. Vrsta koju program ne zna ide u ostalo, bez nastavka.
func noviPut(vrsta, otisak string, primljeno time.Time) (string, error) {
	if !ispravanOtisak(otisak) {
		return "", fmt.Errorf("neispravan otisak sadržaja %q", otisak)
	}
	m, ok := mapeVrsta[vrsta]
	if !ok {
		m = [2]string{"ostalo", ""}
	}
	return path.Join(m[0], primljeno.UTC().Format("2006-01"), otisak+m[1]), nil
}

// otisakIzImena vraća otisak iz imena datoteke sadržaja; prazno kad ime
// nije otisak s nastavkom
func otisakIzImena(ime string) string {
	o := strings.TrimSuffix(ime, path.Ext(ime))
	if !ispravanOtisak(o) {
		return ""
	}
	return o
}

// put je puna putanja datoteke na mjestu rel; mjesto mora ostati unutar
// mape sadržaja
func (d *datoteke) put(rel string) (string, error) {
	lokalno := filepath.FromSlash(rel)
	if rel == "" || !filepath.IsLocal(lokalno) {
		return "", fmt.Errorf("neispravno mjesto datoteke sadržaja %q", rel)
	}
	return filepath.Join(d.mapa, lokalno), nil
}

// zapisi sprema bajtove na mjesto rel. Datoteka koja već postoji ostaje:
// isto mjesto nosi otisak, a isti otisak su isti bajtovi. Nova se piše pod
// privremenim imenom, ide na disk i tek onda dobije pravo ime, pa nakon
// nestanka struje nema napola zapisane datoteke pod pravim imenom.
func (d *datoteke) zapisi(rel string, b []byte) error {
	put, err := d.put(rel)
	if err != nil {
		return err
	}
	if d.mapa == "" {
		d.uMemoriju(rel, b)
		return nil
	}
	if fi, err := os.Stat(put); err == nil && fi.Size() == int64(len(b)) {
		return nil
	}
	mapa := filepath.Dir(put)
	if err := os.MkdirAll(mapa, 0o755); err != nil {
		return err
	}
	if err := zapisiPaPreimenuj(mapa, put, b); err != nil {
		return fmt.Errorf("datoteka sadržaja: %w", err)
	}
	syncMapu(mapa)
	return nil
}

// uMemoriju sprema kopiju bajtova memorijskog spremišta
func (d *datoteke) uMemoriju(rel string, b []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mem == nil {
		d.mem = map[string][]byte{}
	}
	d.mem[rel] = append([]byte(nil), b...)
}

// zapisiPaPreimenuj piše bajtove u privremenu datoteku u mapi, šalje ih na
// disk i tek onda datoteku preimenuje u put; kod greške privremene nema
func zapisiPaPreimenuj(mapa, put string, b []byte) error {
	f, err := os.CreateTemp(mapa, ".novo-*")
	if err != nil {
		return err
	}
	privremena := f.Name()
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if errZ := f.Close(); err == nil {
		err = errZ
	}
	if err == nil {
		err = os.Rename(privremena, put)
	}
	if err != nil {
		_ = os.Remove(privremena) // već se javlja greška upisa
	}
	return err
}

// syncMapu zapisuje mapu na disk, da preimenovanje preživi nestanak struje.
// Windows mapu ne da otvoriti za to, a tamo je preimenovanje ionako trajno.
func syncMapu(mapa string) {
	if f, err := os.Open(mapa); err == nil {
		_ = f.Sync() // na Windowsu ne ide; datoteka je već na disku
		f.Close()    //nolint:errcheck // mapa je samo čitana
	}
}

// citaj vraća bajtove s mjesta rel; ErrNema kad datoteke nema
func (d *datoteke) citaj(rel string) ([]byte, error) {
	put, err := d.put(rel)
	if err != nil {
		return nil, ErrNema // zapis bez mjesta: datoteke nema
	}
	if d.mapa == "" {
		d.mu.Lock()
		defer d.mu.Unlock()
		b, ok := d.mem[rel]
		if !ok {
			return nil, ErrNema
		}
		return b, nil
	}
	b, err := os.ReadFile(put)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNema
	}
	return b, err
}

// ima javlja postoji li datoteka na mjestu rel
func (d *datoteke) ima(rel string) bool {
	put, err := d.put(rel)
	if err != nil {
		return false
	}
	if d.mapa == "" {
		d.mu.Lock()
		defer d.mu.Unlock()
		_, ok := d.mem[rel]
		return ok
	}
	_, err = os.Stat(put)
	return err == nil
}

// obrisi miče datoteku. Ako ne ide (na Windowsu je netko još čita), ostaje
// višak koji pospremanje kasnije makne.
func (d *datoteke) obrisi(rel string) {
	put, err := d.put(rel)
	if err != nil {
		return
	}
	if d.mapa == "" {
		d.mu.Lock()
		defer d.mu.Unlock()
		delete(d.mem, rel)
		return
	}
	_ = os.Remove(put) // višak makne PocistiDatoteke
}

// mjesto je mjesto datoteke sadržaja koji se upisuje: zapisano mjesto ako
// ga redak ima, a inače novo, po vrsti i vremenu primitka retka ako redak
// postoji, odnosno po zadanoj vrsti i vremenu
func (s *Spremiste) mjesto(ctx context.Context, otisak, vrsta string, sad time.Time) (string, error) {
	var rel, vrstaRetka string
	var primljeno time.Time
	err := s.db.QueryRowContext(ctx, `SELECT datoteka, vrsta, primljeno FROM sadrzaj WHERE otisak = ?`, otisak).Scan(&rel, &vrstaRetka, &primljeno)
	switch {
	case err == sql.ErrNoRows:
		return noviPut(vrsta, otisak, sad)
	case err != nil:
		return "", err
	case rel != "":
		return rel, nil
	}
	return noviPut(vrstaRetka, otisak, primljeno)
}

// ukloniDatoteke miče datoteke na mjestima koja popis više ne navodi. Zove
// se nakon što je brisanje u bazi potvrđeno, pod istom bravom kao upis, pa
// istodobni upis istog sadržaja ne ostane bez datoteke.
func (s *Spremiste) ukloniDatoteke(ctx context.Context, mjesta []string) {
	if len(mjesta) == 0 {
		return
	}
	s.upis.Lock()
	defer s.upis.Unlock()
	for _, rel := range mjesta {
		if rel != "" && !s.navedeno(ctx, rel) {
			s.dat.obrisi(rel)
		}
	}
}

// navedeno javlja navodi li popis datoteku na mjestu rel
func (s *Spremiste) navedeno(ctx context.Context, rel string) bool {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sadrzaj WHERE datoteka = ?`, rel).Scan(&n); err != nil {
		return true // ne zna se: datoteka ostaje
	}
	return n > 0
}

// visakStariji je koliko datoteka koju popis ne navodi mora biti stara da
// je pospremanje makne: novija je možda upravo zapisana, a redak još ne
const visakStariji = time.Hour

// PocistiDatoteke miče datoteke sadržaja koje popis ne navodi i ostatke
// prekinutih upisa, starije od sata. Vraća koliko ih je maknuto.
func (s *Spremiste) PocistiDatoteke(ctx context.Context) (int, error) {
	if s.dat.mapa == "" {
		return 0, nil
	}
	visak, err := s.visakDatoteka(ctx, time.Now().Add(-visakStariji))
	if err != nil {
		return 0, err
	}
	n := 0
	s.upis.Lock()
	defer s.upis.Unlock()
	for _, rel := range visak {
		if s.navedeno(ctx, rel) {
			continue // u međuvremenu ponovno upisan
		}
		if put, err := s.dat.put(rel); err == nil && os.Remove(put) == nil {
			n++
		}
	}
	return n, nil
}

// visakDatoteka su mjesta datoteka u mapi sadržaja starijih od prije koje
// popis ne navodi i ostataka prekinutih upisa. Mapa koje nema nema ni viška.
func (s *Spremiste) visakDatoteka(ctx context.Context, prije time.Time) ([]string, error) {
	var visak []string
	err := filepath.WalkDir(s.dat.mapa, func(put string, e fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if rel, da := s.jeVisak(ctx, put, e, prije); da {
			visak = append(visak, rel)
		}
		return nil
	})
	return visak, err
}

// jeVisak javlja je li stavka mape datoteka koju treba maknuti, i njezino
// mjesto. Dira samo datoteke imena otiska i ostatke prekinutih upisa.
func (s *Spremiste) jeVisak(ctx context.Context, put string, e fs.DirEntry, prije time.Time) (string, bool) {
	if e.IsDir() {
		return "", false
	}
	fi, err := e.Info()
	if err != nil || !fi.ModTime().Before(prije) {
		return "", false // nestala u međuvremenu ili još svježa
	}
	rel, err := filepath.Rel(s.dat.mapa, put)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(e.Name(), ".novo-") {
		return rel, true
	}
	return rel, otisakIzImena(e.Name()) != "" && !s.navedeno(ctx, rel)
}

// PreseliIzBaze prebacuje bajtove iz stupca podaci starog spremišta u
// datoteke, pa stupac briše i smanjuje bazu. Vraća koliko je sadržaja
// preseljeno. Radi u pozadini nakon pokretanja: svaka datoteka ide na disk
// posebno, što na Windowsu s antivirusom traje, a nadogradnja čeka da se
// program javi. Dok traje, ono što još nije preseljeno čita se iz baze.
// Prekinuto seljenje nastavlja se pri sljedećem pokretanju.
func (s *Spremiste) PreseliIzBaze(ctx context.Context) (int, error) {
	if !s.stari.Load() {
		return 0, nil
	}
	otisci, err := stupacUpita(ctx, s.db, `SELECT otisak FROM sadrzaj WHERE datoteka = ''`)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range otisci {
		if !ispravanOtisak(o) {
			continue // ne može biti ime datoteke; takav sadržaj nitko nije mogao upisati
		}
		preseljen, err := s.preseliJedan(ctx, o)
		if err != nil {
			return n, fmt.Errorf("seljenje sadržaja %s: %w", o, err)
		}
		if preseljen {
			n++
		}
	}
	return n, s.makniStupac(ctx)
}

// makniStupac briše stari stupac kad su sve datoteke na disku i smanjuje
// bazu
func (s *Spremiste) makniStupac(ctx context.Context) error {
	s.upis.Lock()
	defer s.upis.Unlock()
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE sadrzaj DROP COLUMN podaci`); err != nil {
		return fmt.Errorf("seljenje sadržaja, brisanje stupca: %w", err)
	}
	s.stari.Store(false)
	// u WAL-u se baza smanji tek kad se dnevnik prepiše u nju
	for _, q := range []string{`VACUUM`, `PRAGMA wal_checkpoint(TRUNCATE)`} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("seljenje sadržaja, sažimanje baze: %w", err)
		}
	}
	return nil
}

// preseliJedan zapisuje jedan sadržaj iz stupca u datoteku i upisuje joj
// mjesto, pod bravom upisa, da ga istodobno brisanje ne ostavi bez retka s
// datotekom ili obratno. Sadržaj koji je u međuvremenu obrisan ili već ima
// datoteku preskače se.
func (s *Spremiste) preseliJedan(ctx context.Context, otisak string) (bool, error) {
	s.upis.Lock()
	defer s.upis.Unlock()
	var b []byte
	var vrsta, rel string
	var primljeno time.Time
	err := s.db.QueryRowContext(ctx, `SELECT podaci, vrsta, primljeno, datoteka FROM sadrzaj WHERE otisak = ?`, otisak).Scan(&b, &vrsta, &primljeno, &rel)
	if err == sql.ErrNoRows || (err == nil && rel != "") {
		return false, nil // u međuvremenu obrisan ili upisan nanovo, s datotekom
	}
	if err != nil {
		return false, err
	}
	if rel, err = noviPut(vrsta, otisak, primljeno); err != nil {
		return false, err
	}
	if err := s.dat.zapisi(rel, b); err != nil {
		return false, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE sadrzaj SET datoteka = ? WHERE otisak = ?`, rel, otisak)
	return err == nil, err
}

// izStupca čita bajtove iz starog stupca dok seljenje traje; nil kad
// stupca više nema ili je sadržaj prazan
func (s *Spremiste) izStupca(ctx context.Context, otisak string) []byte {
	if !s.stari.Load() {
		return nil
	}
	var b []byte
	_ = s.db.QueryRowContext(ctx, `SELECT podaci FROM sadrzaj WHERE otisak = ?`, otisak).Scan(&b) // stupac je upravo nestao: nema ga
	if len(b) == 0 {
		return nil
	}
	return b
}

// uStupcu javlja čeka li sadržaj još u starom stupcu na seljenje
func (s *Spremiste) uStupcu(ctx context.Context, otisak string) bool {
	if !s.stari.Load() {
		return false
	}
	var ima bool
	_ = s.db.QueryRowContext(ctx, `SELECT length(podaci) > 0 FROM sadrzaj WHERE otisak = ?`, otisak).Scan(&ima) // stupac je upravo nestao: nema ga
	return ima
}
