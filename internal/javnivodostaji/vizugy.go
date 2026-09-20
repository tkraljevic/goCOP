// Mađarske letve čita vodoprivredna stranica vizugy.hu. Stranica postaje
// nosi satne vrijednosti zadnja dva tjedna, ali ne u tablici nego u poljima
// koja crta graf:
//
//	Vizallas = new Array(28, 30, 31, …);
//	Idopont  = new Array('2026.09.06. 01:00', '2026.09.06. 02:00', …);
//
// Vrijeme je mađarsko lokalno, s ljetnim pomakom kao i naše — provjereno na
// preklapanju s arhivskim nizom Budimpešte: sa satnim pomakom ljeti se 33 od
// 33 zajednička sata poklope u dlaku, bez njega tek dio.
//
// Postaja se u adresi imenuje svojom oznakom (AllomasVOA), npr. Budimpešta
// 16496059-97AB-11D4-BB62-00508BA24287.
package javnivodostaji

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gocop/internal/models"
)

// Njihov poslužitelj uz svoj certifikat šalje pogrešan međucertifikat: potpisao
// ga je „e-Szigno RSA OV TLS CA 2026", a poslužitelj prilaže „e-Szigno OV TLS
// CA 2026", drugo tijelo. Preglednici to ne primijete jer pravi međucertifikat
// dovuku sami, po adresi zapisanoj u certifikatu; Go to ne radi, pa bi veza
// pala na provjeri. Zato pravi međucertifikat stoji uz program i dodaje se
// popisu pouzdanih. Provjera time ostaje na snazi, samo joj je dopunjena karika
// koju poslužitelj ne šalje.
//
//go:embed vizugy-ca.pem
var vizugyCA []byte

// vizugyKlijent je HTTP klijent s dopunjenim popisom pouzdanih tijela
var vizugyKlijent = sync.OnceValue(func() *http.Client {
	bazen, err := x509.SystemCertPool()
	if err != nil || bazen == nil {
		bazen = x509.NewCertPool()
	}
	bazen.AppendCertsFromPEM(vizugyCA)
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: bazen}},
	}
})

// PodrijetloVizugy je ono što stoji uz očitanje kao izvor
const PodrijetloVizugy = "vizugy.hu"

// AdresaVizugyPostaje je stranica grafa jedne postaje; OrasIdosor je satni niz
const AdresaVizugyPostaje = "https://www.vizugy.hu/"

var (
	reVizugyPostaja = regexp.MustCompile(`(?i)AllomasVOA=([0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12})`)
	reVizugyPolje   = regexp.MustCompile(`(?s)\b%s\s*=\s*new Array\((.*?)\)`)
)

// Vizugy čita mađarsku stranicu
type Vizugy struct {
	Client *Client // zbog HTTP klijenta
	Base   string  // prazno znači prava stranica; test podmeće svoju
}

// Naziv je vizugy.hu
func (v Vizugy) Naziv() string { return PodrijetloVizugy }

// Prepoznaje adrese mađarske stranice
func (v Vizugy) Prepoznaje(adresa string) bool { return PostajaVizugyIzAdrese(adresa) != "" }

// PostajaVizugyIzAdrese vraća oznaku postaje iz adrese; prazno kad adresa
// nije njihova ili nema oznake
func PostajaVizugyIzAdrese(adresa string) string {
	if !strings.Contains(strings.ToLower(adresa), "vizugy.hu") {
		return ""
	}
	m := reVizugyPostaja.FindStringSubmatch(adresa)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[1])
}

// AdresaVizugy je adresa stranice zadane postaje
func AdresaVizugy(oznaka string) string {
	return AdresaVizugyPostaje + "?mapModule=OpGrafikon&AllomasVOA=" +
		strings.ToUpper(oznaka) + "&mapData=OrasIdosor"
}

// Ocitanja čita satne vrijednosti s adrese, najstarije prvo
func (v Vizugy) Ocitanja(ctx context.Context, adresa string) ([]Redak, error) {
	oznaka := PostajaVizugyIzAdrese(adresa)
	if oznaka == "" {
		return nil, fmt.Errorf("adresa nema oznaku postaje (AllomasVOA=…)")
	}
	if v.Base != "" {
		adresa = v.Base + "?mapModule=OpGrafikon&AllomasVOA=" + oznaka + "&mapData=OrasIdosor"
	}
	b, err := v.dohvati(ctx, adresa)
	if err != nil {
		return nil, err
	}
	return CitajVizugy(string(b))
}

// dohvati čita stranicu klijentom koji zna za njihov međucertifikat
func (v Vizugy) dohvati(ctx context.Context, adresa string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adresa, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "goCOP (preuzimanje javnih vodostaja)")
	resp, err := vizugyKlijent().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", adresa, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// CitajVizugy čita satne vrijednosti iz polja koja stranica crta.
func CitajVizugy(html string) ([]Redak, error) {
	vrijednosti := poljeNiza(html, "Vizallas")
	vremena := poljeNiza(html, "Idopont")
	if len(vrijednosti) == 0 || len(vremena) == 0 {
		return nil, fmt.Errorf("na stranici nema polja s vodostajima — je li se stranica promijenila?")
	}
	n := len(vrijednosti)
	if len(vremena) < n {
		n = len(vremena)
	}
	// Na kraju polja stoje nule za sate koje postaja još nije javila. Nula je
	// inače moguć vodostaj, pa se odbacuje samo taj završni niz nula.
	for n > 0 && strings.TrimSpace(vrijednosti[n-1]) == "0" {
		n--
	}
	var out []Redak
	for i := 0; i < n; i++ {
		v := strings.TrimSpace(vrijednosti[i])
		if v == "" {
			continue
		}
		cm, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		kad, err := vrijemeVizugy(vremena[i])
		if err != nil {
			continue
		}
		out = append(out, Redak{Kad: kad, Cm: cm})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("polja su prazna — postaja možda ne javlja vodostaj")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kad.Before(out[j].Kad) })
	return out, nil
}

// vrijemeVizugy čita trenutak oblika '2026.09.06. 01:00'. Mađarska drži isto
// vrijeme kao i mi, pa se čita u našoj zoni i sprema kao UTC.
func vrijemeVizugy(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "'\""))
	t, err := time.ParseInLocation("2006.01.02. 15:04", s, models.Zagreb)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// poljeNiza vadi sadržaj jednog polja i dijeli ga po zarezima
func poljeNiza(html, ime string) []string {
	re := regexp.MustCompile(fmt.Sprintf(reVizugyPolje.String(), regexp.QuoteMeta(ime)))
	m := re.FindStringSubmatch(html)
	if m == nil {
		return nil
	}
	return strings.Split(m[1], ",")
}
