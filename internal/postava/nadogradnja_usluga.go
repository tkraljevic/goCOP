package postava

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gocop/internal/izdanje"
)

// Nadogradnja usluge sustava (Linux). Program usluge je rootov
// (/usr/local/bin/gocop), pa ga Postava ne mijenja sama: preuzme i provjeri
// izdanje u svoju mapu, a zamjenu radi instaliraj.sh tog izdanja, pokrenut
// kao root preko pkexec-a. Skripta datoteke još jednom provjeri (potpis i
// SHA-256), zaustavi uslugu, kopira bazu, zamijeni program, pokrene uslugu i
// pričeka /zdravlje (docs/linux.md, poglavlje 9).

// Datoteke izdanja koje traži instaliraj.sh, uz program i SHA256SUMS
const (
	imeSkripte  = "instaliraj.sh"
	imeJedinice = "gocop.service"
)

// imePostave je Postava za Linux uz izdanje goCOP-a (gradi je
// .github/workflows/izdanje.yml, postavlja instaliraj.sh)
func imePostave(goos, goarch string) string { return "gocop-postava-" + goos + "-" + goarch }

// uputaRucno ide uz svaku grešku zbog koje se usluga ne može nadograditi iz
// ikone
const uputaRucno = "goCOP se tada nadograđuje ručno, kako piše u docs/linux.md, poglavlje 9."

// PripremiZaUslugu preuzima u novu mapu (u mapi Postave) program,
// instaliraj.sh, gocop.service, SHA256SUMS i potpis te, ako je u izdanju,
// Postavu za Linux. Potpis i SHA-256 svake datoteke provjeravaju se prije
// nego išta pokrene; kad nešto ne valja, mape nema.
func (u Ugradnja) PripremiZaUslugu(ctx context.Context, iz Izdanje) (string, error) {
	zbrojevi, potpis, err := u.potpisaniZbrojevi(ctx, iz)
	if err != nil {
		return "", err
	}
	mapa, err := os.MkdirTemp(u.M.Podaci, "nadogradnja-")
	if err != nil {
		return "", err
	}
	if err := u.preuzmiZaUslugu(ctx, iz, zbrojevi, potpis, mapa); err != nil {
		_ = os.RemoveAll(mapa)
		return "", err
	}
	return mapa, nil
}

func (u Ugradnja) preuzmiZaUslugu(ctx context.Context, iz Izdanje, zbrojevi, potpis []byte, mapa string) error {
	for ime, b := range map[string][]byte{izdanje.ImeZbrojeva: zbrojevi, izdanje.ImePotpisa: potpis} {
		if err := os.WriteFile(filepath.Join(mapa, ime), b, 0o644); err != nil {
			return err
		}
	}
	for _, ime := range u.datotekeZaUslugu(zbrojevi) {
		if err := u.preuzmiIzIzdanja(ctx, iz, zbrojevi, mapa, ime); err != nil {
			return err
		}
	}
	return nil
}

// datotekeZaUslugu: program, skripta i jedinica uvijek, a Postava kad je
// ima u SHA256SUMS
func (u Ugradnja) datotekeZaUslugu(zbrojevi []byte) []string {
	imena := []string{izdanje.ImeDatoteke(u.GOOS, u.GOARCH), imeSkripte, imeJedinice}
	if _, err := izdanje.Zbroj(zbrojevi, imePostave(u.GOOS, u.GOARCH)); err == nil {
		imena = append(imena, imePostave(u.GOOS, u.GOARCH))
	}
	return imena
}

func (u Ugradnja) preuzmiIzIzdanja(ctx context.Context, iz Izdanje, zbrojevi []byte, mapa, ime string) error {
	d, ok := iz.datoteka(ime)
	if !ok {
		return fmt.Errorf("uz izdanje %s nema datoteke %s. %s", iz.Oznaka, ime, uputaRucno)
	}
	zbroj, err := izdanje.Zbroj(zbrojevi, ime)
	if err != nil {
		return err
	}
	return odbijeno(iz, ime, u.preuzmiProvjereno(ctx, d.URL, filepath.Join(mapa, ime), zbroj))
}

// Ugradi pokreće instaliraj.sh iz pripremljene mape kao root: pkexec
// <bash> <mapa>/instaliraj.sh <mapa>/<program>. Sustav jednom pita lozinku
// administratora. Ispis skripte ide u dnevnik Postave.
func (u *Usluga) Ugradi(ctx context.Context, mapa, program string, pisi func(string, ...any)) error {
	pkexec, err := u.Trazi("pkexec")
	if err != nil {
		return errors.New("na ovom računalu nema programa pkexec, pa Postava ne može tražiti lozinku administratora. " + uputaRucno)
	}
	bash, err := u.Trazi("bash")
	if err != nil {
		return fmt.Errorf("nema programa bash (%v). %s", err, uputaRucno)
	}
	// Skripta se ne prekida, ni kad se Postava gasi: prekinuta usred
	// zamjene ostavila bi uslugu zaustavljenu. Nakon lozinke ionako radi
	// kao root, pa je korisnikov proces ne bi ni mogao ugasiti.
	out, err := u.Naredba(context.WithoutCancel(ctx), pkexec, bash, filepath.Join(mapa, imeSkripte), filepath.Join(mapa, program))
	u.zaboravi()
	pisi("instaliraj.sh (%v):\n%s", err, out)
	return greskaUgradnje(out, err)
}

// greskaUgradnje: pkexec izlazi s 126 kad korisnik odustane od lozinke, a
// sa 127 kad je ne može tražiti; sve ostalo je greška skripte
func greskaUgradnje(out []byte, err error) error {
	if err == nil {
		return nil
	}
	var izlaz interface{ ExitCode() int }
	if !errors.As(err, &izlaz) {
		return fmt.Errorf("pkexec nije pokrenut (%v). %s", err, uputaRucno)
	}
	switch izlaz.ExitCode() {
	case 126:
		return errors.New("goCOP nije nadograđen jer lozinka administratora nije upisana. " + uputaRucno)
	case 127:
		return errors.New("goCOP nije nadograđen: sustav nije mogao otvoriti prozor za lozinku administratora. " + uputaRucno)
	}
	return fmt.Errorf("instalacijska skripta javlja grešku: %s\nCijeli ispis je u dnevniku Postave (postava.log).", ispisIliGreska(out, err))
}

// nadogradiUslugu: preuzmi i provjeri, pokreni instaliraj.sh kao root i
// potvrdi da usluga odgovara s novim izdanjem
func (p *Postava) nadogradiUslugu(ctx context.Context, iz Izdanje) (string, error) {
	mapa, err := p.Ugradnja.PripremiZaUslugu(ctx, iz)
	if err != nil {
		p.Pisi("Nadogradnja odbijena: %v", err)
		return "", err
	}
	defer func() { _ = os.RemoveAll(mapa) }()
	novo := iz.Verzija.String()
	if err := p.usluga.Ugradi(ctx, mapa, izdanje.ImeDatoteke(p.Ugradnja.GOOS, p.Ugradnja.GOARCH), p.Pisi); err != nil {
		p.Pisi("Nadogradnja: %v", err)
		return "", err
	}
	if !p.Ugradnja.cekajIzdanje(ctx, novo) {
		return "", fmt.Errorf("skripta je završila, ali usluga ne javlja izdanje %s. Pogledajte Dnevnik čvora", novo)
	}
	p.Pisi("Nadogradnja na %s gotova", novo)
	return novo, nil
}
