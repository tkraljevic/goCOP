// Hidrografska služba Donje Austrije (noel.gv.at) objavljuje za svaku
// postaju zadnja tri dana vodostaja po četvrt sata kao CSV s točkom-zarezom:
// kidata/stationdata/<broj>_Wasserstand_3Tage.csv. Vrijeme je srednjoeuropsko
// bez ljetnog pomaka (MEZ, UTC+1), kako austrijska hidrografija vodi sve
// nizove. goCOP uzima očitanja na punom satu, kao i s PEGELONLINE-a.
package javnivodostaji

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PodrijetloNOEL je ono što stoji uz očitanje kao izvor
const PodrijetloNOEL = "noel.gv.at"

// noel.gv.at je potpisan preko GÉANT-a korijenom HARICA (grčka akademska
// mreža), koji Go na macOS-u ne prihvaća iako stoji u sustavskom spremištu.
// Korijen zato stoji uz program i dodaje se popisu pouzdanih; provjera ostaje.
//
//go:embed harica-ca.pem
var haricaCA []byte

var noelKlijent = sync.OnceValue(func() *http.Client {
	bazen, err := x509.SystemCertPool()
	if err != nil || bazen == nil {
		bazen = x509.NewCertPool()
	}
	bazen.AppendCertsFromPEM(haricaCA)
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: bazen}},
	}
})

var reNOEL = regexp.MustCompile(`(?i)/kidata/stationdata/(\d+)_Wasserstand_3Tage\.csv$`)

// zonaNOEL je MEZ: austrijski hidrografski nizovi ne prelaze na ljetno vrijeme
var zonaNOEL = time.FixedZone("MEZ", 3600)

// NOEL čita datoteku Donje Austrije
type NOEL struct {
	Client *Client
	Base   string // zamjenska adresa u testu
}

// Naziv je noel.gv.at
func (n NOEL) Naziv() string { return PodrijetloNOEL }

// Prepoznaje kaže je li adresa datoteka vodostaja s noel.gv.at
func (n NOEL) Prepoznaje(adresa string) bool { return PostajaNOELIzAdrese(adresa) != "" }

// PostajaNOELIzAdrese vraća njihov broj postaje iz adrese; prazno kad nije njihova
func PostajaNOELIzAdrese(adresa string) string {
	u, err := url.Parse(strings.TrimSpace(adresa))
	if err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), "noel.gv.at") {
		return ""
	}
	m := reNOEL.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return m[1]
}

// Ocitanja dohvaća datoteku i vraća očitanja na punom satu
func (n NOEL) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	if PostajaNOELIzAdrese(adresa) == "" {
		return nil, fmt.Errorf("adresa nije datoteka vodostaja s noel.gv.at")
	}
	if n.Base != "" {
		adresa = n.Base
	}
	klijent := noelKlijent()
	if n.Client != nil && n.Client.HTTP != nil {
		klijent = n.Client.HTTP
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adresa, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "goCOP (preuzimanje javnih vodostaja)")
	resp, err := klijent.Do(req)
	if err != nil {
		return nil, objasniTLS(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", adresa, resp.Status)
	}
	s := bufio.NewScanner(resp.Body)
	var redci []string
	for s.Scan() {
		redci = append(redci, s.Text())
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return CitajNOEL(strings.Join(redci, "\n"))
}

// CitajNOEL razlaže datoteku: iza zaglavlja slijede redci „Datum;Wert”.
// Uzimaju se samo puni sati; četvrtine se ne prosječe.
func CitajNOEL(datoteka string) ([]Redak, error) {
	poVremenu := map[int64]Redak{}
	for _, red := range strings.Split(datoteka, "\n") {
		polja := strings.Split(strings.TrimSpace(red), ";")
		if len(polja) < 2 {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02 15:04:05", polja[0], zonaNOEL)
		if err != nil || t.Minute() != 0 || t.Second() != 0 {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(polja[1]), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		poVremenu[t.Unix()] = Redak{Kad: t.UTC(), LevelCm: intPtr(int(math.Round(v)))}
	}
	if len(poVremenu) == 0 {
		return nil, fmt.Errorf("noel.gv.at nije vratio nijedno očitanje na punom satu — je li se oblik promijenio?")
	}
	out := make([]Redak, 0, len(poVremenu))
	for _, r := range poVremenu {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}
