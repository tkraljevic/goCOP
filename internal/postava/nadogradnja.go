package postava

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gocop/internal/izdanje"
)

const (
	najveciProgram  = 300 << 20
	najveciZbrojevi = 1 << 20
	najveciPotpis   = 4 << 10
	rokZdravlja     = 90 * time.Second
	rokGasenja      = 30 * time.Second
	cuvajKopija     = 3
)

// Ugradnja preuzima, provjerava i mijenja program čvora
type Ugradnja struct {
	M        Mjesta
	Izdanja  Izdanja
	Cvor     *Cvor
	Kljucevi []ed25519.PublicKey // zadano: ugrađeni ključevi izdanja
	GOOS     string
	GOARCH   string
	Pisi     func(string, ...any)
}

func (u Ugradnja) pisi(f string, a ...any) {
	if u.Pisi != nil {
		u.Pisi(f, a...)
	}
}

func (u Ugradnja) kljucevi() []ed25519.PublicKey {
	if len(u.Kljucevi) > 0 {
		return u.Kljucevi
	}
	return izdanje.Kljucevi(izdanje.JavniKljucevi)
}

// Pripremi preuzima izdanje u mapu novo i provjerava ga: potpis
// SHA256SUMS ključem izdanja, SHA-256 programa i redak koji ispiše
// -version. Kad išta ne valja, ništa postojeće nije dirnuto.
func (u Ugradnja) Pripremi(ctx context.Context, iz Izdanje) (string, error) {
	var zbrojevi, potpis bytes.Buffer
	if err := u.Izdanja.preuzmi(ctx, iz.Zbrojevi.URL, najveciZbrojevi, &zbrojevi); err != nil {
		return "", err
	}
	if err := u.Izdanja.preuzmi(ctx, iz.Potpis.URL, najveciPotpis, &potpis); err != nil {
		return "", err
	}
	if err := izdanje.ProvjeriPotpis(zbrojevi.Bytes(), potpis.Bytes(), u.kljucevi()); err != nil {
		return "", fmt.Errorf("izdanje %s odbijeno: %w", iz.Oznaka, err)
	}
	ime := izdanje.ImeDatoteke(u.GOOS, u.GOARCH)
	zbroj, err := izdanje.Zbroj(zbrojevi.Bytes(), ime)
	if err != nil {
		return "", err
	}
	cilj := u.M.Novi()
	if err := os.MkdirAll(filepath.Dir(cilj), 0o755); err != nil {
		return "", err
	}
	dio := cilj + ".dio"
	f, err := os.OpenFile(dio, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	err = u.Izdanja.preuzmi(ctx, iz.Program.URL, najveciProgram, io.MultiWriter(f, h))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dio)
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != zbroj {
		os.Remove(dio)
		return "", fmt.Errorf("izdanje %s odbijeno: preuzeti program ne odgovara potpisanom SHA-256", iz.Oznaka)
	}
	if err := zamijeni(dio, cilj); err != nil {
		return "", err
	}
	return cilj, u.provjeriIzdanje(ctx, cilj, iz.Verzija.String())
}

// PripremiIzMape uzima izdanje bez interneta: mapa s programom za ovaj
// sustav, SHA256SUMS i SHA256SUMS.sig (npr. s USB-a). Provjera je ista.
func (u Ugradnja) PripremiIzMape(ctx context.Context, mapa string) (string, izdanje.Verzija, error) {
	zbrojevi, err := os.ReadFile(filepath.Join(mapa, izdanje.ImeZbrojeva))
	if err != nil {
		return "", izdanje.Verzija{}, err
	}
	potpis, err := os.ReadFile(filepath.Join(mapa, izdanje.ImePotpisa))
	if err != nil {
		return "", izdanje.Verzija{}, err
	}
	if err := izdanje.ProvjeriPotpis(zbrojevi, potpis, u.kljucevi()); err != nil {
		return "", izdanje.Verzija{}, fmt.Errorf("izdanje u %s odbijeno: %w", mapa, err)
	}
	ime := izdanje.ImeDatoteke(u.GOOS, u.GOARCH)
	zbroj, err := izdanje.Zbroj(zbrojevi, ime)
	if err != nil {
		return "", izdanje.Verzija{}, err
	}
	izvor, err := os.Open(filepath.Join(mapa, ime))
	if err != nil {
		return "", izdanje.Verzija{}, err
	}
	defer izvor.Close()
	cilj := u.M.Novi()
	if err := os.MkdirAll(filepath.Dir(cilj), 0o755); err != nil {
		return "", izdanje.Verzija{}, err
	}
	dio := cilj + ".dio"
	f, err := os.OpenFile(dio, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", izdanje.Verzija{}, err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(izvor, najveciProgram))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != zbroj {
		err = fmt.Errorf("%s ne odgovara potpisanom SHA-256", ime)
	}
	if err != nil {
		os.Remove(dio)
		return "", izdanje.Verzija{}, err
	}
	if err := zamijeni(dio, cilj); err != nil {
		return "", izdanje.Verzija{}, err
	}
	izd, err := IzdanjeDatoteke(ctx, cilj)
	if err != nil {
		return "", izdanje.Verzija{}, err
	}
	v, ok := izdanje.ParsirajOznaku(izd)
	if !ok {
		return "", izdanje.Verzija{}, fmt.Errorf("program javlja nepoznato izdanje %q", izd)
	}
	return cilj, v, nil
}

func (u Ugradnja) provjeriIzdanje(ctx context.Context, put, ocekivano string) error {
	izd, err := IzdanjeDatoteke(ctx, put)
	if err != nil {
		os.Remove(put)
		return err
	}
	if izd != ocekivano {
		os.Remove(put)
		return fmt.Errorf("preuzeti program javlja izdanje %s, a izdanje je %s", izd, ocekivano)
	}
	return nil
}

// Postavi stavlja pripremljeni program na mjesto čvora, kad čvora još nema
// (prva instalacija)
func (u Ugradnja) Postavi(novi string) error {
	if err := os.MkdirAll(u.M.Program, 0o755); err != nil {
		return err
	}
	return zamijeni(novi, u.M.Gocop())
}

// Ugradi mijenja program koji radi pripremljenim novim: zaustavi čvor,
// kopira bazu, zamijeni datoteku, pokrene i čeka da novi odgovori s novim
// izdanjem. Ne odgovori li za 90 s, vraća prethodni program i javlja grešku.
// Baza se ne vraća sama: novo izdanje ju je možda već promijenilo, pa to
// odlučuje administrator (kopija je u data/kopije).
func (u Ugradnja) Ugradi(ctx context.Context, novi, staroIzdanje, novoIzdanje string) error {
	radio := u.Cvor.Proces().Zeljeno
	if err := u.Cvor.Zaustavi(rokGasenja); err != nil {
		return err
	}
	if err := KopirajBazu(u.M, staroIzdanje, time.Now()); err != nil {
		u.pisi("Kopija baze prije nadogradnje nije uspjela: %v", err)
		if radio {
			_ = u.Cvor.Pokreni()
		}
		return fmt.Errorf("kopija baze prije nadogradnje: %w", err)
	}
	_ = os.Remove(u.M.Prethodni())
	if err := zamijeni(u.M.Gocop(), u.M.Prethodni()); err != nil {
		if radio {
			_ = u.Cvor.Pokreni()
		}
		return err
	}
	if err := zamijeni(novi, u.M.Gocop()); err != nil {
		_ = zamijeni(u.M.Prethodni(), u.M.Gocop())
		if radio {
			_ = u.Cvor.Pokreni()
		}
		return err
	}
	u.pisi("Program zamijenjen: %s → %s", staroIzdanje, novoIzdanje)
	if err := u.Cvor.Pokreni(); err == nil && u.cekajIzdanje(ctx, novoIzdanje) {
		u.pisi("Nadogradnja na %s gotova", novoIzdanje)
		return nil
	}

	u.pisi("Novo izdanje %s nije odgovorilo za %s; vraćam %s", novoIzdanje, rokZdravlja, staroIzdanje)
	_ = u.Cvor.Zaustavi(rokGasenja)
	_ = os.Remove(u.M.Neuspjeli())
	_ = zamijeni(u.M.Gocop(), u.M.Neuspjeli())
	if err := zamijeni(u.M.Prethodni(), u.M.Gocop()); err != nil {
		return fmt.Errorf("novo izdanje nije krenulo, a prethodni program se ne da vratiti: %w", err)
	}
	if err := u.Cvor.Pokreni(); err != nil {
		return fmt.Errorf("novo izdanje nije krenulo; prethodni vraćen, ali ni on ne kreće: %w", err)
	}
	return fmt.Errorf("izdanje %s nije krenulo pa je vraćeno %s; dnevnik: %s", novoIzdanje, staroIzdanje, u.M.DnevnikCvora())
}

func (u Ugradnja) cekajIzdanje(ctx context.Context, ocekivano string) bool {
	rok := time.Now().Add(rokZdravlja)
	for time.Now().Before(rok) {
		if izd, _ := u.Cvor.Zdravlje(ctx); izd == ocekivano {
			return true
		}
		if p := u.Cvor.Proces(); !p.Radi && p.Pao {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return false
}

// zamijeni premješta datoteku, uz do pet pokušaja: antivirus zna kratko
// držati tek preuzet ili tek pokrenut program
func zamijeni(iz, u string) error {
	var err error
	for i := 0; i < 5; i++ {
		if err = os.Rename(iz, u); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("premještanje %s → %s: %w", filepath.Base(iz), filepath.Base(u), err)
}

// KopirajBazu sprema bazu čvora (i njezin WAL) u data/kopije prije
// nadogradnje; čvor je tada zaustavljen pa je kopija dosljedna. Čuva tri
// najnovije.
func KopirajBazu(m Mjesta, izdanjeBaze string, sad time.Time) error {
	if _, err := os.Stat(m.BazaCvora()); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := os.MkdirAll(m.Kopije(), 0o755); err != nil {
		return err
	}
	if izdanjeBaze == "" {
		izdanjeBaze = "nepoznato"
	}
	ime := fmt.Sprintf("gocop-%s-%s.db", sad.Format("20060102-150405"), izdanjeBaze)
	for _, nastavak := range []string{"", "-wal"} {
		iz := m.BazaCvora() + nastavak
		if _, err := os.Stat(iz); err != nil {
			continue
		}
		if err := kopiraj(iz, filepath.Join(m.Kopije(), ime+nastavak)); err != nil {
			return err
		}
	}
	return pocisti(m.Kopije(), cuvajKopija)
}

func kopiraj(iz, u string) error {
	src, err := os.Open(iz)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(u+".dio", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(u + ".dio")
		return err
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	return os.Rename(u+".dio", u)
}

// pocisti ostavlja n najnovijih kopija (s pripadnim -wal)
func pocisti(dir string, n int) error {
	stavke, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var baze []string
	for _, s := range stavke {
		if strings.HasPrefix(s.Name(), "gocop-") && strings.HasSuffix(s.Name(), ".db") {
			baze = append(baze, s.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(baze))) // ime počinje datumom
	for i, b := range baze {
		if i < n {
			continue
		}
		_ = os.Remove(filepath.Join(dir, b))
		_ = os.Remove(filepath.Join(dir, b+"-wal"))
	}
	return nil
}
