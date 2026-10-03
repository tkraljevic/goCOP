// Package imecvora: ime (identifikator) čvora u mreži.
//
// Mreža razlikuje čvorove po imenu: pod njim čvor upisuje svoje zapise u
// knjigu verzija, pod njim ga drugi čvorovi pamte uz javni ključ, i pod
// njim mu je izdana potvrda članstva. Dva čvora istog imena zato se ne smiju
// sresti: drugi bi tiho prepisao ključ prvoga i njihovi bi se zapisi
// miješali. Zato ime nastaje prije prvog pokretanja (instalacijski program,
// gocop.toml ili -node) ili ga čvor sam izabere jedinstveno, i ne mijenja se
// nakon toga: prvi zapisi već nose to ime.
//
// Oblik: mala slova, brojke i crtica, 3–40 znakova, bez crtice na početku i
// kraju (npr. pperic-thinkpad, cop-osijek-unraid).
package imecvora

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Stari je ime koje je čvor dobivao dok ga nitko ne bi upisao (do
// 0.0.28-alfa). Postojeći čvor s tim imenom ga zadržava; novi ga ne dobiva.
const Stari = "gocop-cvor"

const (
	najkrace = 3
	najdulje = 40
)

var oblik = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Provjeri javlja je li ime ispravnog oblika
func Provjeri(ime string) error {
	if n := len(ime); n < najkrace || n > najdulje {
		return fmt.Errorf("ime čvora ima %d–%d znakova", najkrace, najdulje)
	}
	if !oblik.MatchString(ime) || strings.Contains(ime, "--") {
		return errors.New("ime čvora smije imati mala slova bez kvačica, brojke i crticu (ne na početku ni kraju), npr. pperic-thinkpad")
	}
	return nil
}

var kvacice = strings.NewReplacer(
	"č", "c", "ć", "c", "đ", "d", "dž", "dz", "š", "s", "ž", "z",
	"Č", "c", "Ć", "c", "Đ", "d", "Dž", "dz", "DŽ", "dz", "Š", "s", "Ž", "z",
)

// Ocisti pretvara slobodan upis u ispravan oblik: mala slova, hrvatska
// slova bez kvačica, sve ostalo u crticu, bez ponovljenih crtica i s
// najviše 40 znakova. Može vratiti i prekratko ime; to javlja Provjeri.
func Ocisti(s string) string {
	s = strings.ToLower(kvacice.Replace(strings.TrimSpace(s)))
	var b strings.Builder
	crtica := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			crtica = false
			continue
		}
		if !crtica && b.Len() > 0 {
			b.WriteByte('-')
			crtica = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > najdulje {
		out = strings.TrimRight(out[:najdulje], "-")
	}
	return out
}

// Predlozi slaže ime iz korisnika i računala: pperic-thinkpad
func Predlozi(korisnik, racunalo string) string {
	return Ocisti(korisnik + "-" + racunalo)
}

// Nasumicno je ime za čvor kojem nitko nije upisao ime: ime računala i
// četiri nasumična znaka, npr. thinkcentre-5-k3f9
func Nasumicno(racunalo string) string {
	const znakovi = "abcdefghijkmnpqrstuvwxyz23456789" // bez l, o, 0, 1
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = znakovi[int(b[i])%len(znakovi)]
	}
	osnova := Ocisti(racunalo)
	if len(osnova) > najdulje-5 {
		osnova = strings.TrimRight(osnova[:najdulje-5], "-")
	}
	if osnova == "" {
		osnova = "cvor"
	}
	return osnova + "-" + string(b)
}
