package prognoza

// Hidrografska služba Donje Austrije (Amt der NÖ Landesregierung) objavljuje
// na noel.gv.at za dunavske letve Kienstock, Korneuburg i Wildungsmauer
// prognozu vodostaja 48 sati unaprijed, po četvrt sata, s rasponom
// pouzdanosti, više puta dnevno. Wildungsmauer je vrh našeg dunavskog lanca:
// odande se voda prati do Nagybajcsa i Komároma, pa nizvodno, i tako lanac
// više ne ovisi o mađarskoj prognozi Komároma. Ista stranica daje i Angern
// na Moravi (48 h) — kad se za nj skupi satna povijest, ući će kao pritok.
//
// Datoteke su CSV s točkom-zarezom, a vrijeme u njima je srednjoeuropsko bez
// ljetnog pomaka (MEZ, UTC+1), kako austrijska hidrografija vodi sve nizove.

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Korijenski certifikat HARICA (Hellenic Academic and Research Institutions
// CA, grčka akademska mreža) kojim je preko GÉANT-a potpisan noel.gv.at.
// Go na macOS-u ga ne prihvaća iako stoji u sustavskom spremištu, pa se
// dodaje u bazen povjerenja uz sustavske korijene; vrijedi do 2045. Izvezen
// iz Appleova spremišta korijena, otisak SHA-256 D9:5D:0E:8E:DA:79:52:5B:….
//
//go:embed harica_tls_rsa_root_2021.pem
var korijenHARICA []byte

// klijentNOEL je HTTP klijent koji uz sustavske korijene vjeruje i HARICA-i.
func klijentNOEL() *http.Client {
	bazen, err := x509.SystemCertPool()
	if err != nil || bazen == nil {
		bazen = x509.NewCertPool()
	}
	bazen.AppendCertsFromPEM(korijenHARICA)
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: bazen}}}
}

// PodrijetloNOEL je ono što stoji uz austrijsku prognozu kao izvor.
const PodrijetloNOEL = "noel.gv.at"

// AdresaNOEL je mapa s nizovima po postaji; datoteka prognoze vodostaja je
// <broj>_WasserstandPrognose_48Stunden.csv.
const AdresaNOEL = "https://www.noel.gv.at/wasserstand/kidata/stationdata/"

// LetveNOEL su njihovi brojevi postaja za naše šifre.
var LetveNOEL = map[string]string{
	"207373": "wildungsmauer",
	"207357": "kienstock",
	"207241": "korneuburg",
}

// naziviNOEL preslikava njihove nazive iz zaglavlja datoteke u naše šifre.
var naziviNOEL = map[string]string{
	"Wildungsmauer": "wildungsmauer",
	"Kienstock":     "kienstock",
	"Korneuburg":    "korneuburg",
}

// SifraNOEL vraća našu šifru za njihov naziv; prazno kad letvu ne vodimo.
func SifraNOEL(naziv string) string {
	return naziviNOEL[strings.TrimSpace(naziv)]
}

// zonaNOEL je MEZ: austrijski hidrografski nizovi ne prelaze na ljetno vrijeme.
var zonaNOEL = time.FixedZone("MEZ", 3600)

// CitajNOEL razlaže jednu datoteku prognoze. Zaglavlje nosi naziv postaje i
// početak prognoze („von”), a redci „Datum;Mittel;Min;Max” vrijednosti po
// četvrt sata. Uzimaju se puni sati; raspon je polovina širine pouzdanosti.
func CitajNOEL(datoteka string) (Letva, error) {
	var l Letva
	l.Rijeka = "Donau"
	s := bufio.NewScanner(strings.NewReader(datoteka))
	for s.Scan() {
		polja := strings.Split(strings.TrimSpace(s.Text()), ";")
		if len(polja) < 2 {
			continue
		}
		switch polja[0] {
		case "Stationsname":
			l.Naziv = strings.TrimSpace(polja[1])
			continue
		case "von":
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(polja[1]), zonaNOEL); err == nil {
				l.Izdano = t
			}
			continue
		}
		t, err := time.ParseInLocation("2006-01-02 15:04:05", polja[0], zonaNOEL)
		if err != nil || t.Minute() != 0 {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(polja[1]), 64)
		if err != nil {
			continue
		}
		d := Dan{Kad: t.UTC(), Cm: int(math.Round(v))}
		if len(polja) >= 4 {
			lo, err1 := strconv.ParseFloat(strings.TrimSpace(polja[2]), 64)
			hi, err2 := strconv.ParseFloat(strings.TrimSpace(polja[3]), 64)
			if err1 == nil && err2 == nil && hi >= lo {
				d.PlusMin = int(math.Round((hi - lo) / 2))
			}
		}
		l.Dani = append(l.Dani, d)
	}
	if l.Naziv == "" || len(l.Dani) < 2 {
		return l, fmt.Errorf("u datoteci nema prognoze — je li se oblik promijenio?")
	}
	if l.Izdano.IsZero() {
		l.Izdano = l.Dani[0].Kad
	}
	return l, nil
}

// DohvatiNOEL čita prognoze svih letvi koje vodimo. Vraća one koje je
// dobio; pogreška je tek kad nije dobio nijednu.
func DohvatiNOEL(ctx context.Context, klijent *http.Client) ([]Letva, error) {
	return dohvatiNOEL(ctx, klijent, AdresaNOEL)
}

func dohvatiNOEL(ctx context.Context, klijent *http.Client, adresa string) ([]Letva, error) {
	if klijent == nil {
		klijent = klijentNOEL()
	}
	brojevi := make([]string, 0, len(LetveNOEL))
	for b := range LetveNOEL {
		brojevi = append(brojevi, b)
	}
	sort.Strings(brojevi)
	var out []Letva
	var greske []error
	for _, b := range brojevi {
		u := adresa + b + "_WasserstandPrognose_48Stunden.csv"
		l, err := func() (Letva, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return Letva{}, err
			}
			req.Header.Set("User-Agent", "goCOP (prognoza vodostaja)")
			resp, err := klijent.Do(req)
			if err != nil {
				return Letva{}, err
			}
			defer resp.Body.Close()
			tijelo, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if err != nil {
				return Letva{}, err
			}
			if resp.StatusCode != http.StatusOK {
				return Letva{}, fmt.Errorf("%s", resp.Status)
			}
			return CitajNOEL(string(tijelo))
		}()
		if err != nil {
			greske = append(greske, fmt.Errorf("%s: %w", LetveNOEL[b], err))
			continue
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, errors.Join(greske...)
	}
	return out, nil
}
