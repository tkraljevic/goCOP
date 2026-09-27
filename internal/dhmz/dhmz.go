// Package dhmz čita otvorene podatke Državnog hidrometeorološkog zavoda
// (meteo.hr, "XML za korisnike"): trenutno vrijeme na glavnim postajama,
// upozorenja (CAP), prognozu po regijama i hidrološki bilten. Podaci su pod
// Otvorenom dozvolom Republike Hrvatske, uz obavezno navođenje DHMZ-a.
//
// Svaka datoteka se čuva petnaest minuta; kad DHMZ nije dostupan, vraća se
// zadnje što je pročitano, s vremenom kad je pročitano.
package dhmz

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Adrese otvorenih datoteka
const (
	AdresaVrijeme         = "https://vrijeme.hr/hrvatska_n.xml"
	AdresaUpozorenjaDanas = "https://meteo.hr/upozorenja/cap_hr_today.xml"
	AdresaUpozorenjaSutra = "https://meteo.hr/upozorenja/cap_hr_tomorrow.xml"
	AdresaRegije          = "https://prognoza.hr/regije_danas.xml"
	AdresaBilten          = "https://hidro.hr/hidro_bilten.xml"
)

// Trajanje je koliko se pročitana datoteka smatra svježom
const Trajanje = 15 * time.Minute

// Zagreb je zona u kojoj DHMZ piše datume
var Zagreb = func() *time.Location {
	l, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		return time.FixedZone("CET", 3600)
	}
	return l
}()

// Klijent čita i čuva datoteke. Siguran je za istodobnu upotrebu.
type Klijent struct {
	HTTP   *http.Client
	Adrese map[string]string // test podmeće svoje; ključ je zadana adresa

	mu     sync.Mutex
	zapisi map[string]zapis
}

type zapis struct {
	tijelo []byte
	kad    time.Time
}

func (k *Klijent) dohvati(ctx context.Context, adresa string) ([]byte, time.Time, error) {
	k.mu.Lock()
	z, ima := k.zapisi[adresa]
	k.mu.Unlock()
	if ima && time.Since(z.kad) < Trajanje {
		return z.tijelo, z.kad, nil
	}
	stvarna := adresa
	if a, ok := k.Adrese[adresa]; ok {
		stvarna = a
	}
	c := k.HTTP
	if c == nil {
		c = &http.Client{Timeout: 8 * time.Second}
	}
	tijelo, err := func() ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, stvarna, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "goCOP")
		res, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: %s", stvarna, res.Status)
		}
		return io.ReadAll(io.LimitReader(res.Body, 4<<20))
	}()
	if err != nil {
		if ima {
			return z.tijelo, z.kad, nil // zadnje pročitano, uz vrijeme kad je pročitano
		}
		return nil, time.Time{}, err
	}
	k.mu.Lock()
	if k.zapisi == nil {
		k.zapisi = map[string]zapis{}
	}
	k.zapisi[adresa] = zapis{tijelo: tijelo, kad: time.Now()}
	k.mu.Unlock()
	return tijelo, time.Now(), nil
}

// --- trenutno vrijeme -----------------------------------------------------

// Postaja je jedna glavna postaja s trenutnim vremenom.
type Postaja struct {
	Ime          string
	Lat, Lon     float64
	Temp         *float64
	Vlaga        *float64
	Tlak         *float64
	VjetarSmjer  string
	VjetarBrzina *float64
	Opis         string
}

// Vrijeme je stanje u jednom terminu.
type Vrijeme struct {
	Termin    time.Time
	Postaje   []Postaja
	Procitano time.Time
}

type xmlVrijeme struct {
	Datum   string `xml:"DatumTermin>Datum"`
	Termin  string `xml:"DatumTermin>Termin"`
	Gradovi []struct {
		Ime     string `xml:"GradIme"`
		Lat     string `xml:"Lat"`
		Lon     string `xml:"Lon"`
		Podatci struct {
			Temp, Vlaga, Tlak, VjetarSmjer, VjetarBrzina, Vrijeme string
		} `xml:"Podatci"`
	} `xml:"Grad"`
}

// broj čita broj; prazno i crtica nisu broj
func broj(s string) *float64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// CitajVrijeme čita hrvatska_n.xml.
func CitajVrijeme(b []byte) (*Vrijeme, error) {
	var x xmlVrijeme
	if err := xml.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	v := &Vrijeme{}
	if d, err := time.ParseInLocation("02.01.2006", strings.TrimSpace(x.Datum), Zagreb); err == nil {
		h, _ := strconv.Atoi(strings.TrimSpace(x.Termin))
		v.Termin = d.Add(time.Duration(h) * time.Hour)
	}
	for _, g := range x.Gradovi {
		lat, lon := broj(g.Lat), broj(g.Lon)
		if lat == nil || lon == nil {
			continue
		}
		v.Postaje = append(v.Postaje, Postaja{Ime: strings.TrimSpace(g.Ime), Lat: *lat, Lon: *lon,
			Temp: broj(g.Podatci.Temp), Vlaga: broj(g.Podatci.Vlaga), Tlak: broj(g.Podatci.Tlak),
			VjetarSmjer: strings.TrimSpace(g.Podatci.VjetarSmjer), VjetarBrzina: broj(g.Podatci.VjetarBrzina),
			Opis: strings.TrimSpace(g.Podatci.Vrijeme)})
	}
	return v, nil
}

// Vrijeme vraća trenutno vrijeme na glavnim postajama.
func (k *Klijent) Vrijeme(ctx context.Context) (*Vrijeme, error) {
	b, kad, err := k.dohvati(ctx, AdresaVrijeme)
	if err != nil {
		return nil, err
	}
	v, err := CitajVrijeme(b)
	if err != nil {
		return nil, err
	}
	v.Procitano = kad
	return v, nil
}

// Najbliza vraća postaju najbližu točki i udaljenost u km.
func (v *Vrijeme) Najbliza(lat, lon float64) (*Postaja, float64) {
	var naj *Postaja
	d := math.Inf(1)
	for i := range v.Postaje {
		if x := Udaljenost(lat, lon, v.Postaje[i].Lat, v.Postaje[i].Lon); x < d {
			naj, d = &v.Postaje[i], x
		}
	}
	return naj, d
}

// Udaljenost je udaljenost dviju točaka u km (dovoljno točna za Hrvatsku).
func Udaljenost(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	f1, f2 := lat1*math.Pi/180, lat2*math.Pi/180
	df, dl := (lat2-lat1)*math.Pi/180, (lon2-lon1)*math.Pi/180
	a := math.Sin(df/2)*math.Sin(df/2) + math.Cos(f1)*math.Cos(f2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// --- upozorenja -----------------------------------------------------------

// Upozorenje je jedno upozorenje za jedno područje.
type Upozorenje struct {
	Dogadjaj string // npr. "Žuto upozorenje za vjetar"
	Razina   int    // 1 zeleno, 2 žuto, 3 narančasto, 4 crveno
	Boja     string // yellow, orange, red
	Opis     string
	Uputa    string
	Od, Do   time.Time
	Podrucje string // županija ili pomorsko područje
}

type xmlCAP struct {
	Info []struct {
		Jezik     string `xml:"language"`
		Dogadjaj  string `xml:"event"`
		Onset     string `xml:"onset"`
		Expires   string `xml:"expires"`
		Opis      string `xml:"description"`
		Uputa     string `xml:"instruction"`
		Parametri []struct {
			Ime        string `xml:"valueName"`
			Vrijednost string `xml:"value"`
		} `xml:"parameter"`
		Podrucja []struct {
			Opis string `xml:"areaDesc"`
		} `xml:"area"`
	} `xml:"info"`
}

func capVrijeme(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, f := range []string{"2006-01-02 15:04:05-07:00", "2006-01-02T15:04:05-07:00", time.RFC3339} {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// CitajUpozorenja čita CAP datoteku; uzima samo hrvatski jezik.
func CitajUpozorenja(b []byte) ([]Upozorenje, error) {
	var x xmlCAP
	if err := xml.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	var out []Upozorenje
	for _, i := range x.Info {
		if i.Jezik != "" && !strings.HasPrefix(i.Jezik, "hr") {
			continue
		}
		u := Upozorenje{Dogadjaj: strings.TrimSpace(i.Dogadjaj), Opis: strings.TrimSpace(i.Opis),
			Uputa: strings.Join(strings.Fields(i.Uputa), " "), Od: capVrijeme(i.Onset), Do: capVrijeme(i.Expires)}
		for _, p := range i.Parametri {
			if p.Ime == "awareness_level" {
				dijelovi := strings.Split(p.Vrijednost, ";")
				u.Razina, _ = strconv.Atoi(strings.TrimSpace(dijelovi[0]))
				if len(dijelovi) > 1 {
					u.Boja = strings.TrimSpace(dijelovi[1])
				}
			}
		}
		for _, a := range i.Podrucja {
			k := u
			k.Podrucje = strings.TrimSpace(a.Opis)
			out = append(out, k)
		}
	}
	return out, nil
}

// Upozorenja vraća upozorenja za danas i sutra koja još nisu istekla, od
// najviše razine prema nižoj.
func (k *Klijent) Upozorenja(ctx context.Context) ([]Upozorenje, time.Time, error) {
	var out []Upozorenje
	var procitano time.Time
	var zadnja error
	for _, a := range []string{AdresaUpozorenjaDanas, AdresaUpozorenjaSutra} {
		b, kad, err := k.dohvati(ctx, a)
		if err != nil {
			zadnja = err
			continue
		}
		u, err := CitajUpozorenja(b)
		if err != nil {
			zadnja = err
			continue
		}
		out = append(out, u...)
		if procitano.IsZero() || kad.Before(procitano) {
			procitano = kad
		}
	}
	if len(out) == 0 && zadnja != nil {
		return nil, time.Time{}, zadnja
	}
	sada := time.Now()
	var aktivna []Upozorenje
	for _, u := range out {
		if !u.Do.IsZero() && u.Do.Before(sada) {
			continue
		}
		aktivna = append(aktivna, u)
	}
	sort.SliceStable(aktivna, func(a, b int) bool {
		if aktivna[a].Razina != aktivna[b].Razina {
			return aktivna[a].Razina > aktivna[b].Razina
		}
		return aktivna[a].Od.Before(aktivna[b].Od)
	})
	return aktivna, procitano, nil
}

// --- prognoza po regijama i hidrološki bilten -----------------------------

// Tekstovi su naslovi po ključu (regija ili rijeka) uz datum.
type Tekstovi struct {
	Datum     string
	Razdoblje string
	Tekst     map[string]string
	Procitano time.Time
}

// citajTekstove čita jednostavan XML: korijen s djecom koja nose tekst
func citajTekstove(b []byte) (*Tekstovi, error) {
	t := &Tekstovi{Tekst: map[string]string{}}
	d := xml.NewDecoder(strings.NewReader(string(b)))
	dubina, ime := 0, ""
	var sb strings.Builder
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch x := tok.(type) {
		case xml.StartElement:
			dubina++
			if dubina == 2 {
				ime = x.Name.Local
				sb.Reset()
			}
		case xml.CharData:
			if dubina == 2 {
				sb.Write(x)
			}
		case xml.EndElement:
			if dubina == 2 {
				v := strings.Join(strings.Fields(sb.String()), " ")
				switch ime {
				case "datum", "datum_upisa":
					t.Datum = v
				case "period_prognoze":
					t.Razdoblje = v
				case "vrijeme_upisa":
					t.Datum = strings.TrimSpace(t.Datum + " " + v)
				default:
					if v != "" {
						t.Tekst[ime] = v
					}
				}
			}
			dubina--
		}
	}
	return t, nil
}

// Regije vraća današnju prognozu po regijama (istocna, sredisnja, gorska,
// sjjadran, istra, dalmacija).
func (k *Klijent) Regije(ctx context.Context) (*Tekstovi, error) {
	b, kad, err := k.dohvati(ctx, AdresaRegije)
	if err != nil {
		return nil, err
	}
	t, err := citajTekstove(b)
	if t != nil {
		t.Procitano = kad
	}
	return t, err
}

// Bilten vraća hidrološki bilten po rijekama (sava, kupa, dunav, mura, drava).
func (k *Klijent) Bilten(ctx context.Context) (*Tekstovi, error) {
	b, kad, err := k.dohvati(ctx, AdresaBilten)
	if err != nil {
		return nil, err
	}
	t, err := citajTekstove(b)
	if t != nil {
		t.Procitano = kad
	}
	return t, err
}
