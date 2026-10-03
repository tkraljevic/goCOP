package web

import (
	"strings"
	"testing"
)

// Pomoć mora reći tko zadužuje račun bez dužnosti, da se piše po dosegu
// dužnosti (i u dnevnike svog područja, i po dionicama akta), da lozinku
// e-pošte nakon promjene ili poništenja lozinke treba upisati ponovno i kad
// se briše, da se zauzeta adresa ne upisuje, da se ključ uklanja uz lozinku
// i da certifikat nosi korisničko ime, bez zagrada iz punog imena.
func TestPomocOpisujeOvlastiPoDosegu(t *testing.T) {
	h := pomocHTML(t)
	for _, want := range []string{
		"Račun bez aktivne dužnosti", "zadužuje samo globalni administrator", "kao ispomoć, pod nazivom uloge",
		"Pravo pisanja ide po dosegu dužnosti", "ne daje pisanje po sektoru",
		"prestaje vrijediti i briše se", "pri prvoj sljedećoj uporabi ili pokretanju čvora", "brišu na ovom čvoru i spremljenu",
		"u dnevnicima svog područja i COP-a", "Akt vodomjera piše tko piše na bilo", "upisuje samo tko",
		"Terenska uloga traži dionice ili", "pri izmjeni naziv i", "zagrade iz punog imena se",
		"Adresu e-pošte koju već ima drugi aktivni račun program ne upisuje nikome", "ni preimenovanjem",
		"uklanjanju ključa (tuđim očima ni jedno ni drugo)", "„Ime Prezime (korisnicko)”",
		"pri prvoj prijavi mora zamijeniti svojom",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("pomoć ne opisuje pravilo %q", want)
		}
	}
}
