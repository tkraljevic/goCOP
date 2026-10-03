// Package izdanje opisuje izdanja goCOP-a na GitHubu i njihov potpis.
//
// Dijele ga Postava, koja izdanje provjerava prije ugradnje, i alat za potpis
// izdanja na laptopu. Uz svako izdanje stoje:
//
//   - program za svaki sustav: gocop-windows-amd64.exe, gocop-linux-amd64,
//     gocop-darwin-arm64…;
//   - SHA256SUMS: SHA-256 svake datoteke, u obliku koji ispisuje sha256sum;
//   - SHA256SUMS.sig: ed25519 potpis datoteke SHA256SUMS ključem izdanja.
//
// Ključ izdanja nije ključ čvora ni mreže: drži ga samo onaj tko izdaje,
// na svom računalu, i nikad ne odlazi na GitHub. Zato i provala u GitHub
// račun ne može podmetnuti program Postavi: potpis se ne može napraviti bez
// ključa, a Postava uz javni ključ ugrađen u sebe ne prihvaća ništa drugo.
//
// Ovo je dio ugovora između Postave i goCOP-a (docs/plan-instalacija.md
// §3.1a): Postava se ne mijenja s goCOP-om, pa imena datoteka, oblik
// SHA256SUMS i potpis ostaju kakvi jesu.
package izdanje

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Repozitorij je GitHub repozitorij s izdanjima goCOP-a
const Repozitorij = "tkraljevic/goCOP"

// Imena datoteka uz svako izdanje
const (
	ImeZbrojeva = "SHA256SUMS"
	ImePotpisa  = "SHA256SUMS.sig"
)

// JavniKljucevi su ključevi izdanja kojima Postava vjeruje, base64. Više
// ključeva je zbog zamjene: novi se doda uz stari, izdanja se potpisuju
// novim, a stari se makne tek u sljedećoj Postavi.
var JavniKljucevi = []string{
	"qFBwYywODKstemglL46NTo1shJ8MeSzwkByRPoi+J8w=", // 3. 10. 2026., ~/.config/gocop/kljuc-izdanja na laptopu
}

// domenaPotpisa se potpisuje ispred SHA256SUMS, da potpis izdanja ne može
// vrijediti ni za što drugo potpisano istim ključem
const domenaPotpisa = "goCOP izdanje v1\n"

// ImeDatoteke je ime programa za sustav i procesor, npr. gocop-windows-amd64.exe
func ImeDatoteke(goos, goarch string) string {
	ime := "gocop-" + goos + "-" + goarch
	if goos == "windows" {
		ime += ".exe"
	}
	return ime
}

// Kljucevi pretvara base64 ključeve u ed25519; neispravni se preskaču
func Kljucevi(popis []string) []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, s := range popis {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil || len(b) != ed25519.PublicKeySize {
			continue
		}
		out = append(out, ed25519.PublicKey(b))
	}
	return out
}

// Potpisi vraća sadržaj datoteke SHA256SUMS.sig: jedan redak base64
func Potpisi(kljuc ed25519.PrivateKey, zbrojevi []byte) []byte {
	sig := ed25519.Sign(kljuc, poruka(zbrojevi))
	return []byte(base64.StdEncoding.EncodeToString(sig) + "\n")
}

// ProvjeriPotpis javlja je li SHA256SUMS potpisan nekim od ključeva
func ProvjeriPotpis(zbrojevi, potpis []byte, kljucevi []ed25519.PublicKey) error {
	if len(kljucevi) == 0 {
		return errors.New("u programu nema ključa izdanja")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(potpis)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("potpis izdanja nije ispravnog oblika")
	}
	p := poruka(zbrojevi)
	for _, k := range kljucevi {
		if ed25519.Verify(k, p, sig) {
			return nil
		}
	}
	return errors.New("potpis izdanja ne odgovara ključu izdanja")
}

func poruka(zbrojevi []byte) []byte {
	return append([]byte(domenaPotpisa), zbrojevi...)
}

// Zbroj vraća SHA-256 (hex) datoteke iz SHA256SUMS
func Zbroj(zbrojevi []byte, ime string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(zbrojevi))
	for sc.Scan() {
		polja := strings.Fields(sc.Text())
		if len(polja) != 2 {
			continue
		}
		if strings.TrimPrefix(polja[1], "*") == ime {
			z := strings.ToLower(polja[0])
			if _, err := hex.DecodeString(z); err != nil || len(z) != 64 {
				return "", fmt.Errorf("zbroj za %s nije ispravan", ime)
			}
			return z, nil
		}
	}
	return "", fmt.Errorf("u %s nema datoteke %s", ImeZbrojeva, ime)
}

// ProvjeriZbroj čita datoteku i uspoređuje SHA-256 sa zapisanim
func ProvjeriZbroj(r io.Reader, ocekivan string) error {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(ocekivan) {
		return fmt.Errorf("SHA-256 preuzete datoteke (%s…) ne odgovara izdanju (%s…)", got[:12], ocekivan[:min(12, len(ocekivan))])
	}
	return nil
}

// Verzija je izdanje goCOP-a: 0.0.28-alfa, 0.1.0-beta, 1.0.0
type Verzija struct {
	Glavna, Sporedna, Ispravak int
	Faza                       int // 0 alfa, 1 beta, 2 stabilno
}

var oblikOznake = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-(alfa|beta))?$`)

// ParsirajOznaku čita oznaku izdanja (v0.0.28-alfa ili 0.0.28-alfa).
// Oznake Postave (postava-v1.0.0) i probne gradnje nisu izdanja goCOP-a.
func ParsirajOznaku(s string) (Verzija, bool) {
	m := oblikOznake.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Verzija{}, false
	}
	var v Verzija
	v.Glavna, _ = strconv.Atoi(m[1])
	v.Sporedna, _ = strconv.Atoi(m[2])
	v.Ispravak, _ = strconv.Atoi(m[3])
	switch m[4] {
	case "alfa":
		v.Faza = 0
	case "beta":
		v.Faza = 1
	default:
		v.Faza = 2
	}
	return v, true
}

// String je oznaka bez "v": 0.0.28-alfa
func (v Verzija) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Glavna, v.Sporedna, v.Ispravak)
	switch v.Faza {
	case 0:
		s += "-alfa"
	case 1:
		s += "-beta"
	}
	return s
}

// Usporedi vraća -1, 0 ili 1
func (v Verzija) Usporedi(w Verzija) int {
	for _, p := range [][2]int{{v.Glavna, w.Glavna}, {v.Sporedna, w.Sporedna}, {v.Ispravak, w.Ispravak}, {v.Faza, w.Faza}} {
		switch {
		case p[0] < p[1]:
			return -1
		case p[0] > p[1]:
			return 1
		}
	}
	return 0
}
