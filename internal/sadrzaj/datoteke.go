package sadrzaj

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// datoteke drže bajtove sadržaja izvan baze: svaki sadržaj je jedna
// datoteka imena svog otiska u mapi uz bazu (sadrzaj/ab/abcd…). Datoteka se
// nikad ne mijenja, samo nastaje i nestaje. Memorijsko spremište (proba)
// drži bajtove u mapi u memoriji.
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

func (d *datoteke) put(otisak string) (string, error) {
	if !ispravanOtisak(otisak) {
		return "", fmt.Errorf("neispravan otisak sadržaja %q", otisak)
	}
	return filepath.Join(d.mapa, otisak[:2], otisak), nil
}

// zapisi sprema bajtove pod otiskom. Datoteka koja već postoji ostaje: isti
// otisak su isti bajtovi. Nova se piše pod privremenim imenom, ide na disk i
// tek onda dobije pravo ime, pa nakon nestanka struje nema napola zapisane
// datoteke pod pravim imenom.
func (d *datoteke) zapisi(otisak string, b []byte) error {
	if d.mapa == "" {
		d.uMemoriju(otisak, b)
		return nil
	}
	put, err := d.put(otisak)
	if err != nil {
		return err
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
func (d *datoteke) uMemoriju(otisak string, b []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mem == nil {
		d.mem = map[string][]byte{}
	}
	d.mem[otisak] = append([]byte(nil), b...)
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
		f.Close() //nolint:errcheck // mapa je samo čitana
	}
}

// citaj vraća bajtove; ErrNema kad datoteke nema
func (d *datoteke) citaj(otisak string) ([]byte, error) {
	if d.mapa == "" {
		d.mu.Lock()
		defer d.mu.Unlock()
		b, ok := d.mem[otisak]
		if !ok {
			return nil, ErrNema
		}
		return b, nil
	}
	put, err := d.put(otisak)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(put)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNema
	}
	return b, err
}

// ima javlja postoji li datoteka
func (d *datoteke) ima(otisak string) bool {
	if d.mapa == "" {
		d.mu.Lock()
		defer d.mu.Unlock()
		_, ok := d.mem[otisak]
		return ok
	}
	put, err := d.put(otisak)
	if err != nil {
		return false
	}
	_, err = os.Stat(put)
	return err == nil
}

// obrisi miče datoteku. Ako ne ide (na Windowsu je netko još čita), ostaje
// višak koji pospremanje kasnije makne.
func (d *datoteke) obrisi(otisak string) {
	if d.mapa == "" {
		d.mu.Lock()
		defer d.mu.Unlock()
		delete(d.mem, otisak)
		return
	}
	if put, err := d.put(otisak); err == nil {
		_ = os.Remove(put) // višak makne PocistiDatoteke
	}
}

// ukloniDatoteke miče datoteke sadržaja kojih više nema u bazi. Zove se
// nakon što je brisanje u bazi potvrđeno, pod istom bravom kao upis, pa
// istodobni upis istog sadržaja ne ostane bez datoteke.
func (s *Spremiste) ukloniDatoteke(ctx context.Context, otisci []string) {
	if len(otisci) == 0 {
		return
	}
	s.upis.Lock()
	defer s.upis.Unlock()
	for _, o := range otisci {
		if !s.uBazi(ctx, o) {
			s.dat.obrisi(o)
		}
	}
}

// uBazi javlja ima li sadržaj redak u bazi
func (s *Spremiste) uBazi(ctx context.Context, otisak string) bool {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sadrzaj WHERE otisak = ?`, otisak).Scan(&n); err != nil {
		return true // ne zna se: datoteka ostaje
	}
	return n > 0
}

// visakStariji je koliko datoteka bez retka u bazi mora biti stara da je
// pospremanje makne: novija je možda upravo zapisana, a redak još ne
const visakStariji = time.Hour

// PocistiDatoteke miče datoteke sadržaja kojih nema u bazi i ostatke
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
	for _, put := range visak {
		if ime := filepath.Base(put); ispravanOtisak(ime) && s.uBazi(ctx, ime) {
			continue // u međuvremenu ponovno upisan
		}
		if os.Remove(put) == nil {
			n++
		}
	}
	return n, nil
}

// visakDatoteka su datoteke u mapi sadržaja starije od prije koje baza ne
// navodi i ostaci prekinutih upisa. Mapa koje nema nema ni viška.
func (s *Spremiste) visakDatoteka(ctx context.Context, prije time.Time) ([]string, error) {
	var visak []string
	err := filepath.WalkDir(s.dat.mapa, func(put string, e fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if s.jeVisak(ctx, e, prije) {
			visak = append(visak, put)
		}
		return nil
	})
	return visak, err
}

// jeVisak javlja je li stavka mape datoteka koju treba maknuti
func (s *Spremiste) jeVisak(ctx context.Context, e fs.DirEntry, prije time.Time) bool {
	if e.IsDir() {
		return false
	}
	fi, err := e.Info()
	if err != nil || !fi.ModTime().Before(prije) {
		return false // nestala u međuvremenu ili još svježa
	}
	ime := e.Name()
	return strings.HasPrefix(ime, ".novo-") || (ispravanOtisak(ime) && !s.uBazi(ctx, ime))
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
	otisci, err := otisciUpita(ctx, s.db, `SELECT otisak FROM sadrzaj WHERE length(podaci) > 0`)
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

// preseliJedan zapisuje jedan sadržaj iz stupca u datoteku, pod bravom upisa,
// da ga istodobno brisanje ne ostavi bez retka s datotekom ili obratno.
// Sadržaj koji je u međuvremenu obrisan preskače se.
func (s *Spremiste) preseliJedan(ctx context.Context, otisak string) (bool, error) {
	s.upis.Lock()
	defer s.upis.Unlock()
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT podaci FROM sadrzaj WHERE otisak = ?`, otisak).Scan(&b)
	if err == sql.ErrNoRows {
		return false, nil // u međuvremenu obrisan
	}
	if err != nil {
		return false, err
	}
	if len(b) == 0 {
		return false, nil // u međuvremenu upisan nanovo: bajtovi su već u datoteci
	}
	return true, s.dat.zapisi(otisak, b)
}

// izStupca čita bajtove iz starog stupca dok seljenje traje; nil kad
// stupca više nema ili je sadržaj već preseljen
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
