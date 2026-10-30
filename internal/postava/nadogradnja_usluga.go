package postava

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"gocop/internal/izdanje"
)

// Nadogradnja usluge sustava (Linux). Program usluge je rootov
// (/usr/local/bin/gocop), pa ga Postava ne mijenja sama: preuzme i provjeri
// izdanje u svoju mapu, a zamjenu radi instaliraj.sh tog izdanja, pokrenut
// kao root preko pkexec-a iz kopije u rootovoj mapi. Skripta datoteke još
// jednom provjeri (potpis i SHA-256), zaustavi uslugu, kopira bazu,
// zamijeni program, pokrene uslugu i pričeka /zdravlje (docs/linux.md,
// poglavlje 9).

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
const uputaRucno = "Izdanje goCOP-a tada se mijenja ručno, kako piše u docs/linux.md, poglavlje 9."

// ZaUslugu je izdanje pripremljeno za instaliraj.sh: mapa u koju je
// preuzeto, imena datoteka i SHA-256 dviju datoteka koje root ponovno
// provjerava u svojoj kopiji. Oba zbroja Postava drži u memoriji, pa ih
// promjena datoteka u mapi ne mijenja.
type ZaUslugu struct {
	Mapa     string
	Program  string   // ime programa čvora u mapi
	Datoteke []string // sve datoteke u mapi koje idu skripti
	Zbrojevi string   // SHA-256 potpisanog SHA256SUMS
	Skripta  string   // SHA-256 instaliraj.sh iz SHA256SUMS
	Rok      int64    // do kad (Unix sekunde) se skripta smije pokrenuti; 0: bez roka
}

// PripremiZaUslugu preuzima u novu mapu (u mapi Postave) program,
// instaliraj.sh, gocop.service, SHA256SUMS i potpis te, ako je u izdanju,
// Postavu za Linux. Potpis i SHA-256 svake datoteke provjeravaju se prije
// nego išta pokrene, a preuzeti program mora na -version javiti baš izdanje
// koje se postavlja. Kad nešto ne valja, mape nema.
func (u Ugradnja) PripremiZaUslugu(ctx context.Context, iz Izdanje) (ZaUslugu, error) {
	return u.pripremiZaUslugu(ctx, iz, true)
}

// pripremiZaUslugu je PripremiZaUslugu; bez sPostavom Postava iz izdanja
// se ne preuzima, pa je instaliraj.sh ne dira (vraćanje na starije
// izdanje ne vraća i Postavu)
func (u Ugradnja) pripremiZaUslugu(ctx context.Context, iz Izdanje, sPostavom bool) (ZaUslugu, error) {
	zbrojevi, potpis, err := u.potpisaniZbrojevi(ctx, iz)
	if err != nil {
		return ZaUslugu{}, err
	}
	mapa, err := os.MkdirTemp(u.M.Podaci, "nadogradnja-")
	if err != nil {
		return ZaUslugu{}, err
	}
	imena := u.datotekeZaUslugu(zbrojevi)
	if !sPostavom {
		imena = slices.DeleteFunc(imena, func(ime string) bool { return ime == imePostave(u.GOOS, u.GOARCH) })
	}
	z := ZaUslugu{Mapa: mapa, Program: izdanje.ImeDatoteke(u.GOOS, u.GOARCH),
		Datoteke: append([]string{izdanje.ImeZbrojeva, izdanje.ImePotpisa}, imena...),
		Zbrojevi: fmt.Sprintf("%x", sha256.Sum256(zbrojevi))}
	err = u.preuzmiZaUslugu(ctx, iz, zbrojevi, potpis, mapa, imena)
	if err == nil {
		z.Skripta, err = izdanje.Zbroj(zbrojevi, imeSkripte)
	}
	if err == nil {
		err = u.provjeriZaUslugu(ctx, filepath.Join(mapa, z.Program), iz.Verzija.String())
	}
	if err != nil {
		_ = os.RemoveAll(mapa) // javlja se greška preuzimanja, ne brisanja
		return ZaUslugu{}, err
	}
	return z, nil
}

// provjeriZaUslugu: preuzeti program mora na -version javiti baš izdanje
// koje se postavlja. Kad se ne da ni pokrenuti (npr. mapa je na disku bez
// prava izvršavanja), uz grešku ide uputa za ručnu nadogradnju.
func (u Ugradnja) provjeriZaUslugu(ctx context.Context, put, ocekivano string) error {
	err := u.provjeriIzdanje(ctx, put, ocekivano)
	if err == nil || errors.As(err, new(drugoIzdanje)) {
		return err
	}
	return fmt.Errorf("%w. %s", err, uputaRucno)
}

func (u Ugradnja) preuzmiZaUslugu(ctx context.Context, iz Izdanje, zbrojevi, potpis []byte, mapa string, imena []string) error {
	for ime, b := range map[string][]byte{izdanje.ImeZbrojeva: zbrojevi, izdanje.ImePotpisa: potpis} {
		if err := os.WriteFile(filepath.Join(mapa, ime), b, 0o644); err != nil {
			return err
		}
	}
	for _, ime := range imena {
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

// prijenosKaoRoot je naredba koju bash izvršava kao root. Datoteke iz
// korisnikove mape najprije kopira u novu mapu koju piše samo root, ondje
// provjeri SHA-256 od SHA256SUMS i instaliraj.sh prema zbrojevima iz
// Postave, pa skriptu pokrene iz te kopije. Ostale datoteke skripta sama
// provjeri prema kopiji SHA256SUMS. Ništa se ne čita iz korisnikove mape
// nakon kopiranja. Kopira se samo obična datoteka (ne poveznica, cijev ni
// uređaj), i od nje najviše najveciProgram bajtova. Uz rok (vraćanje na
// starije izdanje) ništa se ne radi kad je rok prošao dok se upisivala
// lozinka.
//
//	$1 mapa, $2 SHA-256 od SHA256SUMS, $3 SHA-256 od instaliraj.sh,
//	$4 ime programa, $5 rok (Unix sekunde, 0 bez roka), dalje imena
//	datoteka koje se kopiraju
const prijenosKaoRoot = `# goCOP: nadogradnja usluge (instaliraj.sh iz provjerene kopije)
set -eu
mapa=$1 zbrojevi=$2 skripta=$3 program=$4 rok=$5
shift 5
if [ "$rok" != 0 ] && [ "$(date +%s)" -ge "$rok" ]; then
	echo "Greška: dopuštenje za starije izdanje isteklo je dok se upisivala lozinka, pa ništa nije promijenjeno. Globalni administrator ga može ponovno dopustiti." >&2
	exit 1
fi
cilj=$(mktemp -d /tmp/gocop-nadogradnja.XXXXXXXXXX)
trap 'rm -rf -- "$cilj"' EXIT
for ime in "$@"; do
	if [ ! -f "$mapa/$ime" ] || [ -h "$mapa/$ime" ]; then
		echo "Greška: $ime u mapi izdanja nije obična datoteka, pa nadogradnja nije pokrenuta. Pokrenite je ponovno." >&2
		exit 1
	fi
	head -c 314572800 -- "$mapa/$ime" >"$cilj/$ime"
done
printf '%s  %s\n' "$zbrojevi" SHA256SUMS "$skripta" instaliraj.sh >"$cilj/.provjera"
if ! (cd "$cilj" && sha256sum -c --strict --status .provjera); then
	echo "Greška: datoteke izdanja promijenile su se nakon provjere, pa nadogradnja nije pokrenuta. Pokrenite je ponovno." >&2
	exit 1
fi
"$BASH" "$cilj/instaliraj.sh" "$cilj/$program"`

// Ugradi pokreće instaliraj.sh pripremljenog izdanja kao root, preko
// pkexec-a i prijenosKaoRoot. Sustav jednom pita lozinku administratora.
// Ispis skripte ide u dnevnik Postave.
func (u *Usluga) Ugradi(ctx context.Context, z ZaUslugu, pisi func(string, ...any)) error {
	if !jeZbroj(z.Zbrojevi) || !jeZbroj(z.Skripta) {
		return errors.New("skripta nije pokrenuta: za izdanje nema SHA-256 kojim se provjerava kopija. " + uputaRucno)
	}
	pkexec, err := u.Trazi("pkexec")
	if err != nil {
		return errors.New("na ovom računalu nema programa pkexec, pa Postava ne može tražiti lozinku administratora. " + uputaRucno)
	}
	bash, err := u.Trazi("bash")
	if err != nil {
		return fmt.Errorf("nema programa bash (%v). %s", err, uputaRucno)
	}
	arg := append([]string{bash, "-c", prijenosKaoRoot, "gocop-nadogradnja", z.Mapa, z.Zbrojevi, z.Skripta, z.Program,
		strconv.FormatInt(z.Rok, 10)}, z.Datoteke...)
	// Skripta se ne prekida, ni kad se Postava gasi: prekinuta usred
	// zamjene ostavila bi uslugu zaustavljenu. Nakon lozinke ionako radi
	// kao root, pa je korisnikov proces ne bi ni mogao ugasiti.
	out, err := u.Naredba(context.WithoutCancel(ctx), pkexec, arg...)
	u.zaboravi()
	pisi("instaliraj.sh (%v):\n%s", err, out)
	return greskaUgradnje(out, err)
}

// jeZbroj: SHA-256 zapisan kao 64 heksadecimalna znaka
func jeZbroj(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
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
		return errors.New("izdanje goCOP-a nije promijenjeno jer lozinka administratora nije upisana. " + uputaRucno)
	case 127:
		return errors.New("izdanje goCOP-a nije promijenjeno: sustav nije mogao otvoriti prozor za lozinku administratora. " + uputaRucno)
	}
	return fmt.Errorf("instalacijska skripta javlja grešku: %s\nCijeli ispis je u dnevniku Postave (postava.log).", ispisIliGreska(out, err))
}

// nadogradiUslugu: preuzmi i provjeri, pokreni instaliraj.sh kao root i
// potvrdi da usluga odgovara s novim izdanjem. Smije li se izdanje
// postaviti, provjerio je pozivatelj; g.jos (kad nije nil) pita još jednom,
// prije lozinke, a rok koji javi provjeri skripta kao root, nakon lozinke.
func (p *Postava) nadogradiUslugu(ctx context.Context, iz Izdanje, g posaoUgradnje) (string, error) {
	z, err := p.Ugradnja.pripremiZaUslugu(ctx, iz, g.sPostavom)
	if err != nil {
		p.Pisi("%s: %v", g.odbijeno, err)
		return "", err
	}
	defer os.RemoveAll(z.Mapa) //nolint:errcheck // preuzeto izdanje više ne treba, ostatak ne smeta
	do, err := g.provjeri(ctx)
	if err != nil {
		return "", err
	}
	if !do.IsZero() {
		z.Rok = do.Unix()
	}
	novo := iz.Verzija.String()
	if err := p.usluga.Ugradi(ctx, z, p.Pisi); err != nil {
		p.Pisi("%s: %v", g.ime, err)
		return "", err
	}
	if !p.Ugradnja.cekajIzdanje(ctx, novo) {
		return "", fmt.Errorf("skripta je završila, ali usluga ne javlja izdanje %s. Pogledajte Dnevnik čvora", novo)
	}
	p.zapisiGotovo(g, novo)
	return novo, nil
}

// dopustenaNadogradnja vraća izdanje koje je sada postavljeno, ili grešku
// kad iz nije novije od njega. Bez postavljenog čvora (prva instalacija)
// nema s čim usporediti. Usluga je uvijek postavljena.
func (p *Postava) dopustenaNadogradnja(ctx context.Context, iz Izdanje, zdravlje string) (string, error) {
	if p.usluga == nil && !p.Cvor.Instaliran() {
		return "", nil
	}
	staro := p.postavljenoIzdanje(ctx, zdravlje)
	err := novijeOd(iz.Verzija, staro)
	if err != nil && p.usluga != nil {
		err = fmt.Errorf("%w. %s", err, uputaRucno)
	}
	if err != nil {
		p.Pisi("Nadogradnja odbijena: %v", err)
		return "", err
	}
	return staro, nil
}

// postavljenoIzdanje je veće od izdanja koje javlja čvor (/zdravlje) i
// onoga koje javlja program na disku. Uz uslugu je program rootov, a na
// /zdravlje može odgovoriti i drugi proces na istom portu.
func (p *Postava) postavljenoIzdanje(ctx context.Context, zdravlje string) string {
	datoteka, _ := IzdanjeDatoteke(ctx, p.M.Gocop()) // bez programa ostaje izdanje čvora
	return veceIzdanje(zdravlje, datoteka)
}

// veceIzdanje od dva zapisa izdanja; zapis koji nije izdanje ne pobjeđuje
func veceIzdanje(a, b string) string {
	va, okA := izdanje.ParsirajOznaku(a)
	vb, okB := izdanje.ParsirajOznaku(b)
	if !okA || (okB && vb.Usporedi(va) > 0) {
		return b
	}
	return a
}

// novijeOd: Postava čvor sama ne vraća na starije ni isto izdanje, a kad ne
// zna koje je postavljeno, ne nadograđuje. Jedina iznimka je dopustenoIzdanje.
func novijeOd(novo izdanje.Verzija, trenutno string) error {
	v, ok := izdanje.ParsirajOznaku(trenutno)
	if !ok {
		return fmt.Errorf("ne zna se koje je izdanje goCOP-a sada postavljeno, pa Postava ne postavlja %s", novo)
	}
	if novo.Usporedi(v) <= 0 {
		return fmt.Errorf("izdanje %s nije novije od postavljenog %s, pa ga Postava ne postavlja", novo, v)
	}
	return nil
}
