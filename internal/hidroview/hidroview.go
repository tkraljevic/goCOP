// Paket hidroview čita telemetriju iz Geolux HydroViewa — sustava na kojem
// Hrvatske vode drže svoje radarske i tlačne mjerne postaje (hdv.voda.hr).
// Tikveš je ondje postaja 9000/900000, a vrijednost koju vodimo kao vodostaj
// zove se „Average Water Level“ i stiže svakih petnaest minuta.
//
// Za razliku od javnih stranica, ovaj sustav traži prijavu. Prijava ide u dva
// koraka, kako ih radi i njihovo sučelje: poslužitelj daje javni RSA ključ,
// klijent njime šifrira MD5 lozinke ispisan velikim slovima, a zauzvrat
// dobiva token koji se dalje šalje kao „Authorization: Bearer“.
//
// Lozinka se ne zapisuje nigdje u ovom paketu i ne ulazi u zapisnik; poziv
// je prima, upotrijebi i pusti.
package hidroview

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ZadanaAdresa je sustav Hrvatskih voda
const ZadanaAdresa = "https://hdv.voda.hr"

// Podrijetlo je ono što stoji uz očitanje kao izvor
const Podrijetlo = "hdv.voda.hr"

// Klijent drži adresu sustava i token dobiven prijavom.
type Klijent struct {
	Adresa string // prazno znači ZadanaAdresa
	HTTP   *http.Client

	token string
}

func (k *Klijent) osnova() string {
	a := strings.TrimRight(strings.TrimSpace(k.Adresa), "/")
	if a == "" {
		a = ZadanaAdresa
	}
	return a + "/api/v1/"
}

func (k *Klijent) klijent() *http.Client {
	if k.HTTP != nil {
		return k.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Prijavljen javlja ima li klijent token.
func (k *Klijent) Prijavljen() bool { return k.token != "" }

// Prijava dobiva token. Lozinka se šalje šifrirana javnim ključem
// poslužitelja, kako to radi i njihovo sučelje: prvo MD5 lozinke velikim
// slovima, pa RSA (PKCS#1 v1.5), pa heksadekadski ispis.
func (k *Klijent) Prijava(ctx context.Context, korisnik, lozinka string) error {
	var kljuc struct {
		RSAKljuc string `json:"rsa_key"`
	}
	if err := k.citaj(ctx, "login/get_rsa_key", &kljuc); err != nil {
		return fmt.Errorf("javni ključ: %w", err)
	}
	javni, err := javniKljuc(kljuc.RSAKljuc)
	if err != nil {
		return err
	}
	zbroj := md5.Sum([]byte(lozinka))
	tajna, err := rsa.EncryptPKCS1v15(rand.Reader, javni,
		[]byte(strings.ToUpper(hex.EncodeToString(zbroj[:]))))
	if err != nil {
		return fmt.Errorf("šifriranje lozinke: %w", err)
	}
	put := "login/get_token?username=" + url.QueryEscape(korisnik) +
		"&password=" + url.QueryEscape(hex.EncodeToString(tajna))
	var odgovor struct {
		Status          string `json:"status"`
		Token           string `json:"token"`
		RacunBlokiran   bool   `json:"account_blocked"`
		RazlogBlokade   string `json:"account_blocked_reason"`
		PromjenaLozinke bool   `json:"change_password_on_login"`
	}
	if err := k.citaj(ctx, put, &odgovor); err != nil {
		return err
	}
	if odgovor.RacunBlokiran {
		return fmt.Errorf("račun je blokiran: %s", odgovor.RazlogBlokade)
	}
	if odgovor.Status != "ok" || odgovor.Token == "" {
		// Poruka ne smije nositi ni korisničko ime ni lozinku.
		return fmt.Errorf("prijava odbijena (%s)", odgovor.Status)
	}
	k.token = odgovor.Token
	return nil
}

func javniKljuc(pemZapis string) (*rsa.PublicKey, error) {
	blok, _ := pem.Decode([]byte(pemZapis))
	if blok == nil {
		return nil, fmt.Errorf("poslužitelj nije dao javni ključ u PEM obliku")
	}
	k, err := x509.ParsePKIXPublicKey(blok.Bytes)
	if err != nil {
		return nil, fmt.Errorf("javni ključ: %w", err)
	}
	rsaKljuc, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("javni ključ nije RSA nego %T", k)
	}
	return rsaKljuc, nil
}

// Postaja je jedna mjerna postaja sustava.
type Postaja struct {
	SiteID   string `json:"site_id"`
	LoggerID string `json:"logger_id"`
	Zadnje   int64  `json:"last_timestamp"`
	Site     struct {
		Naziv         string  `json:"name"`
		Opis          string  `json:"description"`
		Sirina        float64 `json:"latitude"`
		Duzina        float64 `json:"longitude"`
		Sken          int     `json:"scan_interval_min"`
		SifraPostaje  string  `json:"station_id"`
		SifraProjekta string  `json:"project_id"`
	} `json:"site"`
	Grupa string `json:"-"`
}

// Mjerenje je jedna veličina koju postaja javlja. ID se predaje dohvatu
// podataka kao id0.
type Mjerenje struct {
	ID       string // msr_id
	Velicina string // qty_id, npr. #hydro-$7
	Odakle   string // zapisivač, naziv instrumenta ili naknadna obrada
}

// Alarm je prag upisan u sustav. Kod Tikveša su to stupnjevi obrane od
// poplava, izraženi u metrima.
type Alarm struct {
	MjerenjeID string  `json:"msr_id"`
	Opis       string  `json:"description"`
	Odnos      string  `json:"relational_operator"`
	Prag       float64 `json:"threshold_value"`
}

// Vrijednost je jedno očitanje. Vrijednosti stižu u osnovnim jedinicama —
// vodostaj u metrima, temperatura u Celzijevim stupnjevima — a sučelje ih
// tek prikazuje u centimetrima.
type Vrijednost struct {
	Kad        time.Time
	Vrijednost float64
}

// Šifre veličina koje nas zanimaju. Vodostaj letve je srednji vodostaj, jer
// na njemu stoje i pragovi obrane; trenutačni „Water Level“ je sirovo
// očitanje instrumenta i ne mora biti svedeno na nulu letve.
const (
	VelicinaSrednjiVodostaj = "#hydro-$7"
	VelicinaVodostaj        = "#hydro-$6"
	VelicinaTempVode        = "#hydro-$18"
	VelicinaProtok          = "#hydro--$$$$25"
	VelicinaOborina         = "#meteo-$$$$2"
	VelicinaBrzina          = "#hydro-$1"
)

// Postaje vraća postaje koje prijavljeni račun vidi.
func (k *Klijent) Postaje(ctx context.Context) ([]Postaja, error) {
	var odgovor struct {
		Status   string `json:"status"`
		SitesGet struct {
			Grupe []struct {
				Grupa struct {
					Naziv string `json:"name"`
				} `json:"group"`
				Postaje []Postaja `json:"sites"`
			} `json:"groups_sites"`
		} `json:"sites_get"`
	}
	if err := k.citaj(ctx, "sites/get_verbose", &odgovor); err != nil {
		return nil, err
	}
	if odgovor.Status != "ok" {
		return nil, fmt.Errorf("popis postaja: %s", odgovor.Status)
	}
	var out []Postaja
	for _, g := range odgovor.SitesGet.Grupe {
		for _, p := range g.Postaje {
			p.Grupa = g.Grupa.Naziv
			out = append(out, p)
		}
	}
	return out, nil
}

// Oprema vraća mjerenja postaje i pragove upisane uz njih. Mjerenja stoje na
// tri mjesta: na zapisivaču (napon, vlaga), na instrumentima (bubbler,
// termometar) i u naknadnoj obradi, gdje nastaje vodostaj letve.
func (k *Klijent) Oprema(ctx context.Context, siteID string) ([]Mjerenje, []Alarm, error) {
	var odgovor struct {
		Status string `json:"status"`
		Oprema struct {
			Komunikator struct {
				Mjerenja []struct {
					MsrID string `json:"msr_id"`
					QtyID string `json:"qty_id"`
				} `json:"measurements"`
			} `json:"communicator"`
			Instrumenti []struct {
				Naziv    string `json:"name"`
				Mjerenja []struct {
					MsrID string `json:"msr_id"`
					QtyID string `json:"qty_id"`
				} `json:"measurements"`
			} `json:"instruments"`
			Obrada []struct {
				MsrID string `json:"msr_id"`
				QtyID string `json:"qty_id"`
				Opis  string `json:"description"`
			} `json:"postprocessing"`
			Alarmi []Alarm `json:"alarms"`
		} `json:"installed_equipment"`
	}
	if err := k.citaj(ctx, "installed_equipment/get?site_id="+url.QueryEscape(siteID), &odgovor); err != nil {
		return nil, nil, err
	}
	if odgovor.Status != "ok" {
		return nil, nil, fmt.Errorf("oprema postaje: %s", odgovor.Status)
	}
	o := odgovor.Oprema
	var mjerenja []Mjerenje
	for _, m := range o.Komunikator.Mjerenja {
		mjerenja = append(mjerenja, Mjerenje{ID: m.MsrID, Velicina: m.QtyID, Odakle: "zapisivač"})
	}
	for _, i := range o.Instrumenti {
		for _, m := range i.Mjerenja {
			mjerenja = append(mjerenja, Mjerenje{ID: m.MsrID, Velicina: m.QtyID, Odakle: i.Naziv})
		}
	}
	for _, p := range o.Obrada {
		mjerenja = append(mjerenja, Mjerenje{ID: p.MsrID, Velicina: p.QtyID, Odakle: "obrada: " + p.Opis})
	}
	return mjerenja, o.Alarmi, nil
}

// Vrijednosti čita jedno mjerenje u zadanom razdoblju. Odgovor nije JSON
// nego CSV s dva stupca: vrijeme u sekundama od 1970. i vrijednost.
func (k *Klijent) Vrijednosti(ctx context.Context, mjerenjeID string, od, do time.Time) ([]Vrijednost, error) {
	put := fmt.Sprintf("data/get?id0=%s&start=%d&end=%d",
		url.QueryEscape(mjerenjeID), od.Unix(), do.Unix())
	b, err := k.dohvati(ctx, put)
	if err != nil {
		return nil, err
	}
	return CitajCSV(b)
}

// CitajCSV razlaže odgovor dohvata podataka.
func CitajCSV(b []byte) ([]Vrijednost, error) {
	redci := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(redci) == 0 || !strings.HasPrefix(redci[0], "timestamp") {
		// Kad nešto pođe po zlu, poslužitelj odgovori JSON-om sa statusom.
		var greska struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(b, &greska) == nil && greska.Status != "" {
			return nil, fmt.Errorf("dohvat podataka: %s", greska.Status)
		}
		return nil, fmt.Errorf("odgovor nije očekivani CSV s vremenom i vrijednošću")
	}
	var out []Vrijednost
	for _, r := range redci[1:] {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		polja := strings.Split(r, ",")
		if len(polja) < 2 {
			continue
		}
		sek, err := strconv.ParseInt(strings.TrimSpace(polja[0]), 10, 64)
		if err != nil {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(polja[1]), 64)
		if err != nil {
			continue
		}
		out = append(out, Vrijednost{Kad: time.Unix(sek, 0).UTC(), Vrijednost: v})
	}
	if len(out) == 0 {
		return nil, nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}

func (k *Klijent) citaj(ctx context.Context, put string, u any) error {
	b, err := k.dohvati(ctx, put)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, u); err != nil {
		return fmt.Errorf("%s: odgovor nije očekivani JSON: %w", put, err)
	}
	return nil
}

func (k *Klijent) dohvati(ctx context.Context, put string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.osnova()+put, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "goCOP (preuzimanje telemetrije)")
	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}
	resp, err := k.klijent().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", put, resp.Status)
	}
	return b, nil
}
