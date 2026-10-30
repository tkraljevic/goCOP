// Package izvori uvozi nizove iz vanjskih izvora čiji oblik općenita vrata
// arhive ne prepoznaju: ARSO, eHYD, GKD, PEGELONLINE, Geolux SEBA, izvoz
// postaje iz HIS-2000 i godišnjake RHMZ Srbije. Svaki format zapiše u stablo
// vodostaji/ iste datoteke koje je zapisivala njegova naredba; aplikacija i
// pomoćni alat zovu isti kod.
package izvori

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gocop/internal/arhiva"
	"gocop/internal/importer/arso"
	"gocop/internal/importer/ehyd"
	"gocop/internal/importer/gkd"
	"gocop/internal/importer/pegelonline"
	"gocop/internal/importer/seba"
	"gocop/internal/raspakiraj"
	"gocop/internal/uvoz/godisnjak"
	"gocop/internal/uvoz/his2000"
)

// Datoteka je jedna ulazna datoteka.
type Datoteka struct {
	Ime     string
	Sadrzaj []byte
}

// Zadatak je jedan uvoz.
type Zadatak struct {
	Koren    string // korijen stabla vodostaji/
	Sliv     string
	Letva    string
	Izvor    string // prazno znači zadani izvor formata
	Velicina string // samo eHYD: vodostaj, protok ili temperatura
	Postaje  string // samo godišnjak: šifra=letva, odvojeno zarezom
	Probno   bool
	Zamijeni bool // samo HIS-2000: prepiši zatečeni niz i kad se vrijednosti razlikuju

	ctx context.Context // samo za ovaj poziv (UveziUz); ne sprema se uz zadatak
}

// Ishod kaže koje letve treba ponovno izgraditi i što je zapisano.
type Ishod struct {
	Letve    []string
	Zapisano []string
}

// Format je jedan vanjski izvor.
type Format struct {
	Kod         string
	Naziv       string
	Opis        string
	ZadaniIzvor string
	Nastavci    []string // prihvaćeni nastavci datoteka
	Vise        bool     // prima više datoteka odjednom
	Velicina    bool     // traži veličinu (eHYD)
	Postaje     bool     // traži popis postaja umjesto letve (godišnjak)
	ZadaniSliv  string

	uvezi func(dat []string, z Zadatak, w io.Writer) (Ishod, error)
}

// Formati su svi posebni izvori, redom kako ih stranica nudi.
var Formati = []Format{
	{Kod: "his2000", Naziv: "HIS-2000 (DHMZ), izvoz postaje", ZadaniIzvor: "his2000",
		Opis:     "Sve datoteke izvoza jedne postaje ili ZIP s njima: nizovi, krivulje protoka, snimke korita i vodomjerenja. Svaka se prepoznaje po zaglavlju.",
		Nastavci: []string{".csv", ".xls", ".txt", ".zip"}, Vise: true, uvezi: uveziHIS},
	{Kod: "arso", Naziv: "ARSO (Slovenija), dnevne vrijednosti", ZadaniIzvor: "arso",
		Opis:     "Izvoz dnevnih vrijednosti; iz jednog izvoza nastaju nizovi vodostaja, protoka i temperature. Dopunjuje zatečeni niz.",
		Nastavci: []string{".csv"}, Vise: true, uvezi: uveziARSO},
	{Kod: "ehyd", Naziv: "eHYD (Austrija), dnevni srednjaci", ZadaniIzvor: "ehyd",
		Opis:     "Jedna datoteka dnevnih srednjaka za zadanu veličinu. Mjesečni srednjaci se odbijaju.",
		Nastavci: []string{".csv"}, Velicina: true, uvezi: uveziEHYD},
	{Kod: "gkd", Naziv: "GKD (Bavarska), provjereni dnevni protoci", ZadaniIzvor: "gkd", ZadaniSliv: "dunav",
		Opis:     "ZIP s provjerenim dnevnim protocima. Neprovjerene vrijednosti se ne uzimaju.",
		Nastavci: []string{".zip"}, uvezi: uveziGKD},
	{Kod: "pegelonline", Naziv: "PEGELONLINE, 15-minutni JSON", ZadaniIzvor: "pegelonline", ZadaniSliv: "dunav",
		Opis:     "Uzimaju se samo očitanja punog sata, kao satni niz vodostaja.",
		Nastavci: []string{".json"}, uvezi: uveziPegel},
	{Kod: "seba", Naziv: "Geolux SmartObserver / SEBA izvoz", ZadaniIzvor: "geolux-seba",
		Opis:     "Izvoz zapisivača: satni niz vodostaja u cm i temperature vode. Zamjenjuje zatečeni niz istog izvora.",
		Nastavci: []string{".csv"}, Vise: true, uvezi: uveziSEBA},
	{Kod: "godisnjak", Naziv: "Hidrološki godišnjak RHMZ Srbije (PDF)",
		Opis:     "Dnevni vodostaji iz tablica godišnjaka, za zadane postaje (šifra=letva). Treba pdftotext; skenirani godišnjak bez teksta se ne da pročitati.",
		Nastavci: []string{".pdf"}, Vise: true, Postaje: true, ZadaniSliv: "dunav", uvezi: uveziGodisnjak},
}

// UveziUz je Uvezi koji se prekida s ctx (npr. kad korisnik zatvori
// stranicu pregleda): zasad se prekida čitanje godišnjaka.
func (f Format) UveziUz(ctx context.Context, datoteke []Datoteka, z Zadatak, w io.Writer) (Ishod, error) {
	z.ctx = ctx
	return f.Uvezi(datoteke, z, w)
}

// Nadji vraća format po kodu.
func Nadji(kod string) (Format, bool) {
	for _, f := range Formati {
		if f.Kod == kod {
			return f, true
		}
	}
	return Format{}, false
}

// Prihvaca kaže prima li format datoteku tog imena.
func (f Format) Prihvaca(ime string) bool {
	n := strings.ToLower(filepath.Ext(ime))
	for _, x := range f.Nastavci {
		if n == x {
			return true
		}
	}
	return false
}

// Uvezi provjeri zadatak i uveze datoteke. Datoteke se na trenutak spuštaju
// u privremenu mapu, jer čitači formata rade nad putanjama; mapa se poslije
// briše. Dnevnik prima isti ispis koji je davala naredba.
func (f Format) Uvezi(datoteke []Datoteka, z Zadatak, w io.Writer) (Ishod, error) {
	if w == nil {
		w = io.Discard
	}
	if len(datoteke) == 0 {
		return Ishod{}, fmt.Errorf("nije odabrana nijedna datoteka")
	}
	if !f.Vise && len(datoteke) > 1 {
		return Ishod{}, fmt.Errorf("%s prima jednu datoteku, a odabrano ih je %d", f.Naziv, len(datoteke))
	}
	for _, d := range datoteke {
		if !f.Prihvaca(d.Ime) {
			return Ishod{}, fmt.Errorf("%s: %s ne prima datoteke s nastavkom %s", d.Ime, f.Naziv, filepath.Ext(d.Ime))
		}
	}
	// Isto pravilo kao upis niza: i uvozi koji u stablo pišu sami (HIS-2000,
	// godišnjak) ne počinju novo stablo kad je pravo vjerojatno drugdje.
	if err := arhiva.ProvjeriKoren(z.Koren); err != nil {
		return Ishod{}, err
	}
	sliv, err := f.slivZadatka(z.Sliv)
	if err != nil {
		return Ishod{}, err
	}
	z.Sliv = sliv
	if f.Postaje {
		if strings.TrimSpace(z.Postaje) == "" {
			return Ishod{}, fmt.Errorf("trebaju postaje u obliku šifra=letva")
		}
	} else if z.Letva == "" {
		return Ishod{}, fmt.Errorf("treba letva")
	}
	if f.Velicina && z.Velicina == "" {
		return Ishod{}, fmt.Errorf("treba veličina: vodostaj, protok ili temperatura")
	}
	if z.Izvor == "" {
		z.Izvor = f.ZadaniIzvor
	}
	if !f.Postaje {
		if err := arhiva.ProvjeriDjelove(z.Letva, z.Izvor, "vodostaj", "satni"); err != nil {
			return Ishod{}, err
		}
	}

	mapa, err := os.MkdirTemp("", "gocop-uvoz-")
	if err != nil {
		return Ishod{}, err
	}
	defer os.RemoveAll(mapa)
	var puts []string
	for i, d := range datoteke {
		// Ime iz preglednika se ne uzima za putanju: samo osnova, i s
		// rednim brojem ispred, da dva ista imena ne pregaze jedno drugo.
		ime := filepath.Base(strings.ReplaceAll(d.Ime, "\\", "/"))
		if ime == "." || ime == "/" || ime == "" {
			ime = "datoteka"
		}
		put := filepath.Join(mapa, fmt.Sprintf("%03d_%s", i, ime))
		if f.Kod == "his2000" {
			// HIS razvrstava po zaglavlju, ali pamti ime izvornika krivulja;
			// zato ime ostaje kakvo je stiglo, a redni broj ide u mapu.
			put = filepath.Join(mapa, fmt.Sprintf("%03d", i), ime)
			if err := os.MkdirAll(filepath.Dir(put), 0o755); err != nil {
				return Ishod{}, err
			}
		}
		if err := os.WriteFile(put, d.Sadrzaj, 0o644); err != nil {
			return Ishod{}, err
		}
		puts = append(puts, put)
	}
	ishod, err := f.uvezi(puts, z, w)
	sort.Strings(ishod.Letve)
	return ishod, err
}

// slivZadatka je sliv iz zadatka, a bez njega zadani sliv formata. Mora biti
// obična mapa u stablu, za svaki format.
func (f Format) slivZadatka(sliv string) (string, error) {
	if sliv == "" {
		sliv = f.ZadaniSliv
	}
	if sliv == "" {
		return "", fmt.Errorf("treba sliv")
	}
	return sliv, arhiva.ProvjeriSliv(sliv)
}

func uveziARSO(puts []string, z Zadatak, w io.Writer) (Ishod, error) {
	r, err := arso.Citaj(puts)
	if err != nil {
		return Ishod{}, err
	}
	fmt.Fprintf(w, "redaka: %d; nečitljivih datuma: %d; duplikata: %d; sukoba: %d\n",
		r.Izvjestaj.Redaka, r.Izvjestaj.NeispravnihDatuma, r.Izvjestaj.Duplikata, r.Izvjestaj.Sukoba)
	fmt.Fprintf(w, "vodostaja: %d; praznih: %d; neispravnih: %d\n",
		len(r.Vodostaji), r.Izvjestaj.BezVodostaja, r.Izvjestaj.NeispravnihVodostaja)
	fmt.Fprintf(w, "protoka: %d; praznih: %d; neispravnih: %d\n",
		len(r.Protoci), r.Izvjestaj.BezProtoka, r.Izvjestaj.NeispravnihProtoka)
	fmt.Fprintf(w, "temperatura: %d; praznih: %d; neispravnih: %d\n",
		len(r.Temperature), r.Izvjestaj.BezTemperature, r.Izvjestaj.NeispravnihTemperatura)
	ishod := Ishod{Letve: []string{z.Letva}}
	if z.Probno {
		fmt.Fprintln(w, "probni prolaz — ništa nije zapisano")
		return ishod, nil
	}
	for _, n := range []struct {
		velicina string
		redci    []arhiva.Redak
	}{{"vodostaj", r.Vodostaji}, {"protok", r.Protoci}, {"temperatura", r.Temperature}} {
		if len(n.redci) == 0 {
			continue
		}
		put, err := arhiva.Dopuni(z.Koren, z.Sliv, z.Letva, z.Izvor, n.velicina, "srednjak", n.redci)
		if err = arhiva.JaviOstatak(err, w); err != nil {
			return ishod, err
		}
		fmt.Fprintln(w, put)
		ishod.Zapisano = append(ishod.Zapisano, put)
	}
	return ishod, nil
}

func uveziEHYD(puts []string, z Zadatak, w io.Writer) (Ishod, error) {
	r, err := ehyd.Citaj(puts[0], z.Velicina)
	if err != nil {
		return Ishod{}, err
	}
	fmt.Fprintf(w, "postaja: %s (HZB %s); interval: %s; jedinica: %s\n",
		r.Izvjestaj.Postaja, r.Izvjestaj.HZB, r.Izvjestaj.Interval, r.Izvjestaj.Jedinica)
	fmt.Fprintf(w, "redaka: %d; vrijednosti: %d; praznina: %d; neispravnih: %d\n",
		r.Izvjestaj.Redaka, len(r.Redci), r.Izvjestaj.Praznina, r.Izvjestaj.Neispravnih)
	if r.Izvjestaj.Interval != "T" {
		return Ishod{}, fmt.Errorf("niz nije dnevni; mjesečni niz ne smije se upisati kao dnevni srednjak")
	}
	ishod := Ishod{Letve: []string{z.Letva}}
	if z.Probno {
		fmt.Fprintln(w, "probni prolaz — ništa nije zapisano")
		return ishod, nil
	}
	put, err := arhiva.Upisi(z.Koren, z.Sliv, z.Letva, z.Izvor, z.Velicina, "srednjak", r.Redci)
	if err = arhiva.JaviOstatak(err, w); err != nil {
		return ishod, err
	}
	fmt.Fprintln(w, put)
	ishod.Zapisano = append(ishod.Zapisano, put)
	return ishod, nil
}

func uveziGKD(puts []string, z Zadatak, w io.Writer) (Ishod, error) {
	r, err := gkd.Citaj(puts[0])
	if err != nil {
		return Ishod{}, err
	}
	fmt.Fprintf(w, "postaja: %s (%s); ZIP datoteka: %d\n", r.Izvjestaj.Postaja, r.Izvjestaj.Broj, r.Izvjestaj.Datoteka)
	fmt.Fprintf(w, "redaka: %d; provjerenih protoka: %d; praznih datoteka: %d; neprovjerenih: %d; neispravnih: %d; duplikata: %d; sukoba: %d\n",
		r.Izvjestaj.Redaka, len(r.Protoci), r.Izvjestaj.BezPodataka, r.Izvjestaj.Neprovjerenih,
		r.Izvjestaj.Neispravnih, r.Izvjestaj.Duplikata, r.Izvjestaj.Sukoba)
	ishod := Ishod{Letve: []string{z.Letva}}
	if z.Probno {
		fmt.Fprintln(w, "probni prolaz — ništa nije zapisano")
		return ishod, nil
	}
	put, err := arhiva.Upisi(z.Koren, z.Sliv, z.Letva, z.Izvor, "protok", "srednjak", r.Protoci)
	if err = arhiva.JaviOstatak(err, w); err != nil {
		return ishod, err
	}
	fmt.Fprintln(w, put)
	ishod.Zapisano = append(ishod.Zapisano, put)
	return ishod, nil
}

func uveziPegel(puts []string, z Zadatak, w io.Writer) (Ishod, error) {
	r, err := pegelonline.Citaj(puts[0])
	if err != nil {
		return Ishod{}, err
	}
	fmt.Fprintf(w, "izvornih redaka: %d; punih sati: %d; ostalih četvrt-sati: %d; neispravnih: %d; duplikata: %d; sukoba: %d\n",
		r.Izvjestaj.Redaka, len(r.Vodostaji), r.Izvjestaj.IzvanPunogSata,
		r.Izvjestaj.Neispravnih, r.Izvjestaj.Duplikata, r.Izvjestaj.Sukoba)
	ishod := Ishod{Letve: []string{z.Letva}}
	if z.Probno {
		fmt.Fprintln(w, "probni prolaz — ništa nije zapisano")
		return ishod, nil
	}
	put, err := arhiva.Upisi(z.Koren, z.Sliv, z.Letva, z.Izvor, "vodostaj", "satni", r.Vodostaji)
	if err = arhiva.JaviOstatak(err, w); err != nil {
		return ishod, err
	}
	fmt.Fprintln(w, put)
	ishod.Zapisano = append(ishod.Zapisano, put)
	return ishod, nil
}

func uveziSEBA(puts []string, z Zadatak, w io.Writer) (Ishod, error) {
	r, err := seba.Citaj(puts)
	if err != nil {
		return Ishod{}, err
	}
	fmt.Fprintf(w, "izvornih redaka: %d; duplikata: %d; sukoba vrijednosti: %d\n",
		r.Izvjestaj.Redaka, r.Izvjestaj.Duplikata, r.Izvjestaj.Sukoba)
	fmt.Fprintf(w, "vodostaja: %d; praznih: %d; neispravnih: %d\n",
		len(r.Vodostaji), r.Izvjestaj.BezVodostaja, r.Izvjestaj.NeispravnihVodostaja)
	fmt.Fprintf(w, "temperatura: %d; praznih: %d; neispravnih: %d\n",
		len(r.Temperature), r.Izvjestaj.BezTemperature, r.Izvjestaj.NeispravnihTemperatura)
	ishod := Ishod{Letve: []string{z.Letva}}
	if z.Probno {
		fmt.Fprintln(w, "probni prolaz — ništa nije zapisano")
		return ishod, nil
	}
	v, err := arhiva.Upisi(z.Koren, z.Sliv, z.Letva, z.Izvor, "vodostaj", "satni", r.Vodostaji)
	if err = arhiva.JaviOstatak(err, w); err != nil {
		return ishod, err
	}
	t, err := arhiva.Upisi(z.Koren, z.Sliv, z.Letva, z.Izvor, "temperatura", "satni", r.Temperature)
	if err = arhiva.JaviOstatak(err, w); err != nil {
		return ishod, err
	}
	fmt.Fprintln(w, v)
	fmt.Fprintln(w, t)
	ishod.Zapisano = append(ishod.Zapisano, v, t)
	return ishod, nil
}

func uveziHIS(puts []string, z Zadatak, w io.Writer) (Ishod, error) {
	// Granica vrijedi za cijeli uvoz: sve ZIP-ove i sve datoteke zajedno.
	g := granicaUvoza()
	var dat []his2000.Datoteka
	for _, put := range puts {
		iz, err := datotekeHIS(put, g)
		if err != nil {
			return Ishod{}, err
		}
		dat = append(dat, iz...)
	}
	cilj, err := arhiva.MapaLetve(z.Koren, z.Sliv, z.Letva)
	if err != nil {
		return Ishod{}, err
	}
	p := &his2000.Posao{Cilj: cilj, Letva: z.Letva, Izvor: z.Izvor,
		Probno: z.Probno, Zamijeni: z.Zamijeni, Dnevnik: w}
	if _, err := p.Uvezi(dat); err != nil {
		return Ishod{}, err
	}
	return Ishod{Letve: []string{z.Letva}}, nil
}

// datotekeHIS čita jednu poslanu datoteku: ZIP se raspakira, a obična
// datoteka ide kakva jest. Obje troše istu granicu uvoza.
func datotekeHIS(put string, g *raspakiraj.Granica) ([]his2000.Datoteka, error) {
	b, err := os.ReadFile(put)
	if err != nil {
		return nil, err
	}
	ime := filepath.Base(put)
	if strings.EqualFold(filepath.Ext(put), ".zip") {
		b, err := izZIP(b, g)
		return b, omotaj(ime, err)
	}
	if err := g.Potrosi(int64(len(b))); err != nil {
		return nil, omotaj(ime, err)
	}
	return []his2000.Datoteka{{Ime: ime, Sadrzaj: b}}, nil
}

// omotaj dodaje ime datoteke grešci; bez greške vraća nil.
func omotaj(ime string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", ime, err)
}

// granicaUvoza je granica raspakiranja jednog uvoza HIS-2000.
var granicaUvoza = raspakiraj.Zadana

// izZIP vadi datoteke iz ZIP-a, bez mapa u imenu; HIS ih ionako prepoznaje
// po zaglavlju. Stavke i raspakirani bajtovi troše granicu cijelog uvoza, pa
// ZIP koji bi je prešao odbija se cijeli.
func izZIP(b []byte, g *raspakiraj.Granica) ([]his2000.Datoteka, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	if err := g.Stavke(len(zr.File)); err != nil {
		return nil, err
	}
	var out []his2000.Datoteka
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.HasPrefix(filepath.Base(f.Name), ".") {
			continue
		}
		s, err := g.Procitaj(f)
		if err != nil {
			return nil, err
		}
		out = append(out, his2000.Datoteka{Ime: filepath.Base(f.Name), Sadrzaj: s})
	}
	return out, nil
}

func uveziGodisnjak(puts []string, z Zadatak, w io.Writer) (Ishod, error) {
	zeljene, err := ParsirajPostaje(z.Postaje)
	if err != nil {
		return Ishod{}, err
	}
	// Letva koja u stablu već stoji ostaje u svom slivu; zadani sliv vrijedi
	// samo za nove.
	slivZa, err := arhiva.LetveStabla(z.Koren)
	if err != nil {
		return Ishod{}, err
	}
	ish, err := godisnjak.Uvezi(godisnjak.Zadatak{Godisnjaci: puts, Postaje: zeljene,
		Koren: z.Koren, Sliv: z.Sliv, SlivZa: slivZa, Probno: z.Probno, Dnevnik: w, Ctx: z.ctx})
	return Ishod{Letve: ish.Letve, Zapisano: ish.Zapisano}, err
}

// ParsirajPostaje čita „šifra=letva, šifra=letva“.
func ParsirajPostaje(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		d := strings.SplitN(p, "=", 2)
		if len(d) != 2 || strings.TrimSpace(d[0]) == "" || strings.TrimSpace(d[1]) == "" {
			return nil, fmt.Errorf("postaja %q nije u obliku šifra=letva", p)
		}
		out[strings.TrimSpace(d[0])] = strings.TrimSpace(d[1])
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("trebaju postaje u obliku šifra=letva")
	}
	return out, nil
}

// LetveZadatka su letve na koje zadatak piše, za provjeru prava prije uvoza.
func (f Format) LetveZadatka(z Zadatak) ([]string, error) {
	if !f.Postaje {
		return []string{z.Letva}, nil
	}
	m, err := ParsirajPostaje(z.Postaje)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range m {
		out = append(out, l)
	}
	sort.Strings(out)
	return out, nil
}
