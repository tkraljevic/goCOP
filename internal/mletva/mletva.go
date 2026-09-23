// Package mletva čita zatvorenu mobilnu stranicu vodostaja Hrvatskih voda,
// mletva.voda.hr. Izgleda kao javna mvodostaji.voda.hr, ali traži prijavu i
// nosi i postaje koje javnost ne vidi: istjecanje i preljev hidroelektrana na
// Dravi, kote gornje vode i repa akumulacija. Za prognozu Drave to je jedini
// izvor istjecanja HE Dubrava uživo — a na tom je nizu Botovo i namješteno.
//
// Prijava je obična forma, s računom domene Hrvatskih voda. Kolačić prijave
// traje dok ga poslužitelj ne odbaci; tada stranica preusmjeri na prijavu, a
// klijent se prijavi iznova i ponovi zahtjev.
package mletva

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ZadanaAdresa je adresa sustava
const ZadanaAdresa = "https://mletva.voda.hr"

// Podrijetlo je ono što stoji uz očitanje kao izvor
const Podrijetlo = "mletva.voda.hr"

// putPrijave je stranica prijave; na nju sustav preusmjeri kad kolačić istekne
const putPrijave = "/Account/LogOn"

// Klijent drži prijavu na sustav. Siguran je za istodobnu upotrebu.
type Klijent struct {
	Adresa string       // prazno znači ZadanaAdresa
	HTTP   *http.Client // test podmeće svoj; inače se sastavlja pri prvoj upotrebi

	mu                sync.Mutex
	korisnik, lozinka string
	prijavljen        bool
}

func (k *Klijent) osnova() string {
	if k.Adresa != "" {
		return strings.TrimRight(k.Adresa, "/")
	}
	return ZadanaAdresa
}

func (k *Klijent) klijent() *http.Client {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.HTTP == nil {
		jar, _ := cookiejar.New(nil)
		k.HTTP = &http.Client{
			Timeout:   30 * time.Second,
			Jar:       jar,
			Transport: &http.Transport{TLSClientConfig: tlsBezIsteka()},
		}
	}
	// Preusmjeravanje se ne slijedi: po njemu se vidi da je prijava istekla.
	k.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return k.HTTP
}

// tlsBezIsteka provjerava certifikat u svemu osim u datumu. Certifikat
// *.voda.hr istekao je 21. 6. 2025. i nitko ga nije obnovio, a preglednik
// ondje samo upozori pa pusti dalje. Ovdje se lanac i naziv poslužitelja
// provjere kao i inače, samo na trenutak prije isteka: tuđi ili samopotpisan
// certifikat i dalje se odbija, pa lozinka ne ode nikome drugome.
//
// Poslužitelj (IIS 7.5) nakon rukovanja traži ponovno pregovaranje; bez
// dopuštenja za jedno veza pukne prije prvog odgovora.
func tlsBezIsteka() *tls.Config {
	return &tls.Config{
		Renegotiation:      tls.RenegotiateOnceAsClient,
		InsecureSkipVerify: true, // provjeru radi VerifyConnection, bez datuma
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("poslužitelj nije poslao certifikat")
			}
			list := cs.PeerCertificates[0]
			posrednici := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				posrednici.AddCert(c)
			}
			kad := time.Now()
			if kad.After(list.NotAfter) {
				kad = list.NotAfter.Add(-time.Minute)
			}
			_, err := list.Verify(x509.VerifyOptions{
				DNSName:       cs.ServerName,
				Intermediates: posrednici,
				CurrentTime:   kad,
			})
			return err
		},
	}
}

// Prijava se prijavljuje zadanim računom i pamti ga, da se istekla prijava
// može obnoviti bez pitanja.
func (k *Klijent) Prijava(ctx context.Context, korisnik, lozinka string) error {
	c := k.klijent()
	forma := url.Values{
		"UserName":   {korisnik},
		"Password":   {lozinka},
		"RememberMe": {"false"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		k.osnova()+putPrijave+"?ReturnUrl=%2f", strings.NewReader(forma.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "goCOP")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	// Uspjela prijava preusmjeri na početnu stranicu; odbijena vrati formu.
	kamo := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || strings.Contains(kamo, putPrijave) {
		return fmt.Errorf("%s je odbio prijavu (%s) — provjeri korisničko ime i lozinku", Podrijetlo, resp.Status)
	}
	k.mu.Lock()
	k.korisnik, k.lozinka, k.prijavljen = korisnik, lozinka, true
	k.mu.Unlock()
	return nil
}

// Prijavljen javlja je li klijent prijavljen ovim korisnikom
func (k *Klijent) Prijavljen(korisnik string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.prijavljen && k.korisnik == korisnik
}

// Stranica čita stranicu sustava, npr. "/Home/PregledProtokaPostaje?…".
// Kad prijava istekne, obnovi ju jednom i ponovi zahtjev.
func (k *Klijent) Stranica(ctx context.Context, put string) ([]byte, error) {
	for pokusaj := 0; ; pokusaj++ {
		b, istekla, err := k.dohvati(ctx, put)
		if err != nil || !istekla {
			return b, err
		}
		k.mu.Lock()
		korisnik, lozinka := k.korisnik, k.lozinka
		k.prijavljen = false
		k.mu.Unlock()
		if pokusaj > 0 || korisnik == "" {
			return nil, fmt.Errorf("%s traži prijavu", Podrijetlo)
		}
		if err := k.Prijava(ctx, korisnik, lozinka); err != nil {
			return nil, err
		}
	}
}

func (k *Klijent) dohvati(ctx context.Context, put string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.osnova()+put, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", "goCOP")
	resp, err := k.klijent().Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		if strings.Contains(resp.Header.Get("Location"), putPrijave) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("%s preusmjerava na %s", put, resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("%s%s: %s", Podrijetlo, put, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return b, false, err
}
