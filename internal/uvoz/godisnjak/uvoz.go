package godisnjak

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/arhiva"
)

// Zadatak je jedan uvoz: koji godišnjaci, koje postaje i kamo.
type Zadatak struct {
	Godisnjaci []string          // putanje do PDF-ova
	Postaje    map[string]string // šifra postaje u godišnjaku → naša letva
	Koren      string            // korijen stabla vodostaji/
	Sliv       string            // sliv nove letve
	SlivZa     map[string]string // letva → sliv u kojem već stoji u stablu
	Probno     bool
	Popis      bool // samo ispiši postaje koje godišnjaci sadrže
	Dnevnik    io.Writer
	Ctx        context.Context // prekida čitanje PDF-a; nil znači bez prekida (samo rok)
}

func (z Zadatak) kontekst() context.Context {
	if z.Ctx == nil {
		return context.Background()
	}
	return z.Ctx
}

// Ishod kaže što je zapisano.
type Ishod struct {
	Letve    []string
	Zapisano []string
}

// Dan2 je dan s godinom, jer niz ide kroz više godišnjaka.
type Dan2 struct{ Godina, Mjesec, Dan int }

// Prije kaže je li dan a prije dana b.
func (a Dan2) Prije(b Dan2) bool {
	if a.Godina != b.Godina {
		return a.Godina < b.Godina
	}
	if a.Mjesec != b.Mjesec {
		return a.Mjesec < b.Mjesec
	}
	return a.Dan < b.Dan
}

// Uvezi čita godišnjake i zapiše dnevni niz vodostaja svake tražene postaje.
func Uvezi(z Zadatak) (Ishod, error) {
	ctx := z.kontekst()
	pisi := func(format string, a ...any) {
		if z.Dnevnik != nil {
			fmt.Fprintf(z.Dnevnik, format, a...)
		}
	}
	var ishod Ishod
	if len(z.Godisnjaci) == 0 {
		return ishod, fmt.Errorf("nema nijednog godišnjaka")
	}
	// letva → dan → vrijednost
	nizovi := map[string]map[Dan2]int{}
	var upozorenja []string

	for _, put := range z.Godisnjaci {
		tekst, err := TekstPDF(ctx, put)
		if err != nil {
			return ishod, fmt.Errorf("%s: %w", filepath.Base(put), err)
		}
		if z.Popis {
			for _, t := range Citaj(tekst) {
				pisi("%-8s %-22s %d.  dana %3d\n", t.Sifra, t.Naziv, t.Godina, len(t.Dani))
			}
			continue
		}
		nadjeno := 0
		for _, t := range Citaj(tekst) {
			letva, hocemo := z.Postaje[t.Sifra]
			if !hocemo {
				continue
			}
			provjereno, greske := t.Provjeri()
			for _, g := range greske {
				upozorenja = append(upozorenja, fmt.Sprintf("%s, %s %d.: %s", letva, filepath.Base(put), t.Godina, g))
			}
			if nizovi[letva] == nil {
				nizovi[letva] = map[Dan2]int{}
			}
			for d, v := range t.Dani {
				nizovi[letva][Dan2{t.Godina, d.Mjesec, d.Dan}] = v
			}
			nadjeno++
			pisi("%-10s %d.  dana %3d, provjerenih mjeseci %2d, s oznakom leda ili uspora %3d%s\n",
				letva, t.Godina, len(t.Dani), provjereno, t.Oznacenih, akoGreske(greske))
		}
		if nadjeno == 0 {
			upozorenja = append(upozorenja, filepath.Base(put)+": nijedna tražena postaja nije nađena")
		}
	}
	if z.Popis {
		return ishod, nil
	}
	for _, r := range upozorenja {
		pisi("  upozorenje: %s\n", r)
	}

	letve := make([]string, 0, len(nizovi))
	for l := range nizovi {
		letve = append(letve, l)
	}
	sort.Strings(letve)
	for _, letva := range letve {
		dani := nizovi[letva]
		if len(dani) == 0 {
			continue
		}
		popis := poredaniDani(dani)
		od, do := popis[0].Godina, popis[len(popis)-1].Godina
		ime := fmt.Sprintf("%s_hidmet_vodostaj_srednjak_%d-%d.csv", letva, od, do)
		mapa, err := z.mapaLetve(letva)
		if err != nil {
			return ishod, err
		}
		put := filepath.Join(mapa, ime)
		pisi("\n%-10s %6d dana, %d.-%d.\n              → %s\n", letva, len(dani), od, do, ime)
		ishod.Letve = append(ishod.Letve, letva)
		if z.Probno {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
			return ishod, err
		}
		if err := zapisiNiz(put, popis, dani); err != nil {
			return ishod, err
		}
		ishod.Zapisano = append(ishod.Zapisano, put)
	}
	if z.Probno {
		pisi("\nproba — ništa nije zapisano\n")
		return ishod, nil
	}
	pisi("\nzapisano %d nizova\n", len(ishod.Zapisano))
	return ishod, nil
}

// poredaniDani su dani niza od najstarijeg.
func poredaniDani(dani map[Dan2]int) []Dan2 {
	popis := make([]Dan2, 0, len(dani))
	for d := range dani {
		popis = append(popis, d)
	}
	sort.Slice(popis, func(i, j int) bool { return popis[i].Prije(popis[j]) })
	return popis
}

// mapaLetve je mapa letve u stablu: u slivu u kojem letva već stoji, a nova
// u zadanom slivu.
func (z Zadatak) mapaLetve(letva string) (string, error) {
	sliv := z.Sliv
	if s, ima := z.SlivZa[letva]; ima {
		sliv = s
	}
	return arhiva.MapaLetve(z.Koren, sliv, letva)
}

// zapisiNiz zapisuje dnevni niz letve u CSV.
func zapisiNiz(put string, popis []Dan2, dani map[Dan2]int) error {
	f, err := os.Create(put)
	if err != nil {
		return err
	}
	return upisiNiz(f, popis, dani)
}

// upisiNiz piše zaglavlje i dane u otvorenu datoteku i zatvara je. Greška
// pisanja se gleda: os.File je ne pamti, pa bi pun disk ostavio niz bez
// dijela dana, a uvoz bi ga javio kao zapisan (bufio.Writer prvu grešku
// pamti i vraća iz Flush). Nepotpun niz miče se iz stabla, jer bi ga
// gradnja pročitala kao cijeli.
func upisiNiz(f *os.File, popis []Dan2, dani map[Dan2]int) error {
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "datum;vodostaj_cm")
	for _, d := range popis {
		fmt.Fprintf(w, "%04d-%02d-%02d;%d\n", d.Godina, d.Mjesec, d.Dan, dani[d])
	}
	err := errors.Join(w.Flush(), f.Close())
	if err != nil {
		return errors.Join(fmt.Errorf("niz %s nije zapisan do kraja: %w", filepath.Base(f.Name()), err), os.Remove(f.Name()))
	}
	return nil
}

func akoGreske(g []string) string {
	if len(g) == 0 {
		return ""
	}
	return fmt.Sprintf("  — %d mjeseci se ne slaže sa sažetkom", len(g))
}

// ImaPDFAlat kaže može li ovo računalo čitati godišnjake.
func ImaPDFAlat() bool {
	_, err := exec.LookPath("pdftotext")
	return err == nil
}

// Granice čitanja godišnjaka. Godišnjak ima oko 300 stranica i pdftotext ga
// pročita za nekoliko sekundi; ništa veće od ovoga nije godišnjak. Najviše
// dva čitanja idu u isto vrijeme, ostala čekaju red.
var (
	rokPDF          = 2 * time.Minute
	najviseStranica = 1000
	najviseTeksta   = 64 << 20
	citaciPDF       = make(chan struct{}, 2)
)

// TekstPDF vadi tekst iz godišnjaka, s poravnanjem stupaca kakvo tablica i
// ima; bez -layout se stupci sliju i tablica se ne da pročitati. Tekst vadi
// pdftotext (paket poppler); skenirani godišnjak bez sloja teksta daje prazno.
// Čitanje se prekida kad istekne ctx ili rokPDF, a pdftotext se tada gasi.
func TekstPDF(ctx context.Context, put string) (string, error) {
	if !ImaPDFAlat() {
		return "", fmt.Errorf("na ovom računalu nema programa pdftotext (paket poppler), a bez njega se godišnjak ne može pročitati")
	}
	select {
	case citaciPDF <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-citaciPDF }()
	rok, prekini := context.WithTimeout(ctx, rokPDF)
	defer prekini()
	cmd := exec.CommandContext(rok, "pdftotext", "-layout", "-l", strconv.Itoa(najviseStranica), put, "-")
	cmd.WaitDelay = 5 * time.Second
	izlaz := &ograniceniIzlaz{ostalo: najviseTeksta}
	cmd.Stdout = izlaz
	if err := greskaPDF(ctx, rok, izlaz, pokreniPDF(cmd)); err != nil {
		return "", err
	}
	tekst := izlaz.b.String()
	if strings.Count(tekst, "\f") >= najviseStranica {
		return "", fmt.Errorf("PDF ima %d ili više stranica, a godišnjak ih ima oko 300; ovo nije godišnjak", najviseStranica)
	}
	return tekst, nil
}

// greskaPDF kaže zašto čitanje nije uspjelo: prevelik tekst, istekao rok,
// prekinut zahtjev ili greška samog pdftotexta.
func greskaPDF(ctx, rok context.Context, izlaz *ograniceniIzlaz, err error) error {
	switch {
	case izlaz.preko:
		return fmt.Errorf("tekst PDF-a je veći od %d MB; ovo nije godišnjak", najviseTeksta>>20)
	case ctx.Err() != nil:
		return ctx.Err()
	case rok.Err() != nil:
		return fmt.Errorf("čitanje PDF-a traje dulje od %v, pa je prekinuto", rokPDF)
	case err != nil:
		return fmt.Errorf("pdftotext: %w", err)
	}
	return nil
}

// ograniceniIzlaz skuplja izlaz pdftotexta do granice. Preko granice javlja
// grešku, pa pdftotext ne može više pisati i završi.
type ograniceniIzlaz struct {
	b      strings.Builder
	ostalo int
	preko  bool
}

func (o *ograniceniIzlaz) Write(p []byte) (int, error) {
	if len(p) > o.ostalo {
		o.preko = true
		return 0, errors.New("izlaz je prevelik")
	}
	o.ostalo -= len(p)
	return o.b.Write(p)
}
