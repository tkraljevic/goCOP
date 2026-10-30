package postava

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"gocop/internal/izdanje"
)

// Izdanja čita s GitHuba. Pitanje ne nosi ništa o čvoru, korisniku ni
// podacima: samo popis javnih izdanja repozitorija.
type Izdanja struct {
	API     string // https://api.github.com, u testu lažni poslužitelj
	Repo    string // tkraljevic/goCOP
	Klijent *http.Client
	Agent   string // User-Agent, GitHub ga traži
}

type ghDatoteka struct {
	Ime      string `json:"name"`
	URL      string `json:"browser_download_url"`
	Velicina int64  `json:"size"`
}

type ghIzdanje struct {
	Oznaka   string       `json:"tag_name"`
	Naslov   string       `json:"name"`
	Stranica string       `json:"html_url"`
	Nacrt    bool         `json:"draft"`
	Datoteke []ghDatoteka `json:"assets"`
}

// Izdanje goCOP-a spremno za Postavu: ima program za ovaj sustav,
// SHA256SUMS i potpis
type Izdanje struct {
	Oznaka   string
	Verzija  izdanje.Verzija
	Naslov   string
	Stranica string
	Program  ghDatoteka
	Zbrojevi ghDatoteka
	Potpis   ghDatoteka
	Datoteke []ghDatoteka // sve datoteke izdanja (uz uslugu i skripta, jedinica, Postava za Linux)
}

// datoteka traži datoteku izdanja po imenu
func (iz Izdanje) datoteka(ime string) (ghDatoteka, bool) {
	for _, d := range iz.Datoteke {
		if d.Ime == ime {
			return d, true
		}
	}
	return ghDatoteka{}, false
}

// Ponuda je ono što provjera nađe
type Ponuda struct {
	GoCOP           *Izdanje // najnovije izdanje goCOP-a za ovaj sustav
	Postava         string   // novija Postava (oznaka), prazno ako nema
	PostavaStranica string
}

func (iz Izdanja) popis(ctx context.Context) ([]ghIzdanje, error) {
	var out []ghIzdanje
	if err := iz.dohvati(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=40", iz.API, iz.Repo), "popis izdanja", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// dohvati čita JSON s GitHubova API-ja u out; sto ide uz grešku čitanja
func (iz Izdanja) dohvati(ctx context.Context, url, sto string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", iz.Agent)
	odg, err := iz.Klijent.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub nije dostupan: %w", err)
	}
	defer odg.Body.Close()
	if odg.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub odgovara %s", odg.Status)
	}
	if err := json.NewDecoder(io.LimitReader(odg.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s: %w", sto, err)
	}
	return nil
}

var oznakaPostave = regexp.MustCompile(`^postava-v(\d+)\.(\d+)\.(\d+)$`)

// Provjeri traži najnovije izdanje goCOP-a za sustav i noviju Postavu od
// zadane. Nacrti se ne gledaju: izdanje postaje vidljivo tek kad se potpiše
// i objavi.
func (iz Izdanja) Provjeri(ctx context.Context, goos, goarch, mojaPostava string) (Ponuda, error) {
	lista, err := iz.popis(ctx)
	if err != nil {
		return Ponuda{}, err
	}
	var p Ponuda
	ime := izdanje.ImeDatoteke(goos, goarch)
	najPostava := brojeviPostave(mojaPostava)
	for _, g := range lista {
		if g.Nacrt {
			continue
		}
		if m := oznakaPostave.FindStringSubmatch(g.Oznaka); m != nil {
			if b := brojeviPostave(g.Oznaka); usporediBrojeve(b, najPostava) > 0 {
				najPostava = b
				p.Postava, p.PostavaStranica = g.Oznaka, g.Stranica
			}
			continue
		}
		kand, ok := zaSustav(g, ime)
		if ok && (p.GoCOP == nil || kand.Verzija.Usporedi(p.GoCOP.Verzija) > 0) {
			p.GoCOP = &kand
		}
	}
	return p, nil
}

// zaSustav je izdanje goCOP-a s programom ime, uz SHA256SUMS i potpis.
// Oznaka koja nije izdanje goCOP-a, nepotpisano izdanje i izdanje bez
// programa za ovaj sustav ne vrijede.
func zaSustav(g ghIzdanje, ime string) (Izdanje, bool) {
	v, ok := izdanje.ParsirajOznaku(g.Oznaka)
	if !ok {
		return Izdanje{}, false
	}
	kand := Izdanje{Oznaka: g.Oznaka, Verzija: v, Naslov: g.Naslov, Stranica: g.Stranica, Datoteke: g.Datoteke}
	for _, d := range g.Datoteke {
		switch d.Ime {
		case ime:
			kand.Program = d
		case izdanje.ImeZbrojeva:
			kand.Zbrojevi = d
		case izdanje.ImePotpisa:
			kand.Potpis = d
		}
	}
	return kand, kand.Program.URL != "" && kand.Zbrojevi.URL != "" && kand.Potpis.URL != ""
}

// PoOznaci traži izdanje goCOP-a po oznaci (v0.0.45-alfa), za vraćanje na
// starije izdanje. Kao i u Provjeri, nacrt ne vrijedi, a izdanje mora imati
// program za ovaj sustav, SHA256SUMS i potpis.
func (iz Izdanja) PoOznaci(ctx context.Context, goos, goarch, oznaka string) (Izdanje, error) {
	v, ok := izdanje.ParsirajOznaku(oznaka)
	if !ok {
		return Izdanje{}, fmt.Errorf("%q nije oznaka izdanja goCOP-a", oznaka)
	}
	var g ghIzdanje
	if err := iz.dohvati(ctx, fmt.Sprintf("%s/repos/%s/releases/tags/v%s", iz.API, iz.Repo, v), "izdanje", &g); err != nil {
		return Izdanje{}, fmt.Errorf("izdanje %s ne može se preuzeti: %w", v, err)
	}
	kand, ok := zaSustav(g, izdanje.ImeDatoteke(goos, goarch))
	if !ok || g.Nacrt || kand.Verzija != v {
		return Izdanje{}, fmt.Errorf("na GitHubu nema potpisanog izdanja %s za ovaj sustav", v)
	}
	return kand, nil
}

func brojeviPostave(s string) [3]int {
	var out [3]int
	m := oznakaPostave.FindStringSubmatch(s)
	if m == nil {
		m = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(s)
		if m == nil {
			return out
		}
		m = append([]string{""}, m[1:]...)
	}
	for i := 0; i < 3; i++ {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out
}

func usporediBrojeve(a, b [3]int) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}

// preuzmi čita datoteku do granice
func (iz Izdanja) preuzmi(ctx context.Context, url string, granica int64, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", iz.Agent)
	odg, err := iz.Klijent.Do(req)
	if err != nil {
		return fmt.Errorf("preuzimanje: %w", err)
	}
	defer odg.Body.Close()
	if odg.StatusCode != http.StatusOK {
		return fmt.Errorf("preuzimanje: %s", odg.Status)
	}
	n, err := io.Copy(w, io.LimitReader(odg.Body, granica+1))
	if err != nil {
		return fmt.Errorf("preuzimanje: %w", err)
	}
	if n > granica {
		return errors.New("preuzeta datoteka veća je od dopuštenog")
	}
	return nil
}
