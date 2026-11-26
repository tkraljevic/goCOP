package propisi

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"golang.org/x/net/html/charset"

	"gocop/internal/models"
)

// NajvecaStranica je najveća stranica akta koju program čita (8 MiB).
// Zakon o vodama ima oko 420 KB.
const NajvecaStranica = 8 << 20

var (
	// ErrNemaAkta: Narodne novine nemaju taj akt (preusmjere na stranicu
	// „nema stranice” ili na drugi akt).
	ErrNemaAkta = errors.New("narodne novine nemaju taj akt")
	// ErrPrevelikaStranica: stranica akta veća je od 8 MiB.
	ErrPrevelikaStranica = errors.New("stranica akta veća je od 8 MB")
	// ErrNijeHTML: odgovor nije HTML stranica.
	ErrNijeHTML = errors.New("narodne novine nisu vratile stranicu akta")
)

// korisnikDohvata je User-Agent upita Narodnim novinama.
const korisnikDohvata = "goCOP (propisi iz Narodnih novina)"

// uzorakStraniceAkta je put stranice akta nakon preusmjeravanja s ELI
// adrese: /clanci/sluzbeni/2010_07_84_2389.html.
var uzorakStraniceAkta = regexp.MustCompile(`^/clanci/sluzbeni/(?:full/)?(\d{4})_\d{2}_(\d{1,3})_(\d{1,5})\.html$`)

// Dohvat dohvaća stranice akata s narodne-novine.nn.hr. Adresu slaže sam iz
// izdanja, pa nikad ne dohvaća adresu koju je netko upisao.
type Dohvat struct {
	osnova  *url.URL
	klijent *http.Client
}

// NoviDohvat priprema dohvat s kopijom klijenta: bez kolačića, a Transport
// drži razmak između upita. Jedan dohvat služi cijelom programu, pa razmak
// vrijedi za sve upite zajedno.
func NoviDohvat(k *http.Client) *Dohvat {
	kopija := *k
	kopija.Jar = nil
	dalje := kopija.Transport
	if dalje == nil {
		dalje = http.DefaultTransport
	}
	kopija.Transport = noviRazmak(dalje)
	return &Dohvat{osnova: &url.URL{Scheme: "https", Host: PosluziteljNN}, klijent: &kopija}
}

// Akt dohvaća stranicu akta preko ELI adrese i vraća je u UTF-8. Konačna
// adresa mora biti stranica istog akta, inače Narodne novine akt nemaju
// (ErrNemaAkta). Prihvaća se samo HTML do 8 MiB s tekstom akta.
func (d *Dohvat) Akt(ctx context.Context, i models.IzdanjeNN) ([]byte, error) {
	adresa := d.osnova.JoinPath(i.PutELI())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adresa.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", korisnikDohvata)
	req.Header.Set("Accept", "text/html")
	resp, err := d.klijent.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := provjeriOdgovor(resp, i); err != nil {
		return nil, err
	}
	tijelo, err := procitajTijelo(resp)
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(tijelo, []byte("article-column")) {
		return nil, ErrNemaTijelaAkta
	}
	return tijelo, nil
}

// provjeriOdgovor provjerava kamo je dohvat stigao, status i vrstu odgovora.
func provjeriOdgovor(resp *http.Response, i models.IzdanjeNN) error {
	if !stranicaAkta(resp.Request.URL.Path, i) || resp.StatusCode == http.StatusNotFound {
		return ErrNemaAkta
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("narodne novine su odgovorile kodom %d", resp.StatusCode)
	}
	if vrsta, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || vrsta != "text/html" {
		return ErrNijeHTML
	}
	return nil
}

// stranicaAkta javlja je li put stranica istog akta: ELI put ili stranica
// /clanci/sluzbeni/ s istim godinom, brojem i aktom.
func stranicaAkta(put string, i models.IzdanjeNN) bool {
	if put == i.PutELI() {
		return true
	}
	g := uzorakStraniceAkta.FindStringSubmatch(put)
	return g != nil && g[1] == strconv.Itoa(i.Godina) && g[2] == strconv.Itoa(i.Broj) && g[3] == strconv.Itoa(i.Akt)
}

// procitajTijelo čita tijelo u UTF-8 po kodnoj stranici iz zaglavlja ili
// same stranice, najviše 8 MiB.
func procitajTijelo(resp *http.Response) ([]byte, error) {
	r, err := charset.NewReader(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	tijelo, err := io.ReadAll(io.LimitReader(r, NajvecaStranica+1))
	if err != nil {
		return nil, err
	}
	if len(tijelo) > NajvecaStranica {
		return nil, ErrPrevelikaStranica
	}
	return tijelo, nil
}

// JePresretanje javlja da veza nije uspjela jer certifikat poslužitelja
// potpisuje nepoznat potpisnik: tako izgleda mreža koja presreće promet.
func JePresretanje(err error) bool {
	var nepoznat x509.UnknownAuthorityError
	return errors.As(err, &nepoznat)
}
