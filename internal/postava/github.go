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
}

// Ponuda je ono što provjera nađe
type Ponuda struct {
	GoCOP           *Izdanje // najnovije izdanje goCOP-a za ovaj sustav
	Postava         string   // novija Postava (oznaka), prazno ako nema
	PostavaStranica string
}

func (iz Izdanja) popis(ctx context.Context) ([]ghIzdanje, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=40", iz.API, iz.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", iz.Agent)
	odg, err := iz.Klijent.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub nije dostupan: %w", err)
	}
	defer odg.Body.Close()
	if odg.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub odgovara %s", odg.Status)
	}
	var out []ghIzdanje
	if err := json.NewDecoder(io.LimitReader(odg.Body, 8<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("popis izdanja: %w", err)
	}
	return out, nil
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
		v, ok := izdanje.ParsirajOznaku(g.Oznaka)
		if !ok {
			continue
		}
		kand := Izdanje{Oznaka: g.Oznaka, Verzija: v, Naslov: g.Naslov, Stranica: g.Stranica}
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
		if kand.Program.URL == "" || kand.Zbrojevi.URL == "" || kand.Potpis.URL == "" {
			continue // nepotpisano ili bez programa za ovaj sustav
		}
		if p.GoCOP == nil || v.Usporedi(p.GoCOP.Verzija) > 0 {
			k := kand
			p.GoCOP = &k
		}
	}
	return p, nil
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
