package peers

import (
	"encoding/json"
	"unicode/utf8"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// Ograde onoga što razgovor prima od druge strane i pamti. Ispravne poruke
// svih izdanja od 0.0.25 su daleko ispod njih: granica mreže danas ima
// desetke ključeva, delta je kod pošiljatelja ograđena na 32 MiB
// (najviseBajtovaDelte), sadržaj na 48 MiB, a pojedini sadržaj na 64 MiB
// (najveciSadrzajUPoruci), što je kao base64 oko 86 MiB.

// ogradePoruka je najveća primljena poruka po vrsti (razmjena.Conn.PrimiDo).
// Odbijenica stiže umjesto poruke koju razgovor čeka, pa dobiva njezinu
// ogradu. Vrsta koje nema u popisu prima se do razmjena.NajvecaPoruka.
var ogradePoruka = map[string]int64{
	kindFrontier:         8 << 20,
	kindGranicaOdbijenih: 8 << 20,
	kindRupe:             8 << 20,
	kindDelta:            64 << 20, // dvostruka ograda pošiljatelja
	kindZelje:            1 << 20,
	kindSadrzaj:          128 << 20,
	kindDone:             64 << 10,
}

// ogradaPoruke je najveća poruka vrste kind koju razgovor prima
func ogradaPoruke(kind string) int64 {
	if n, ok := ogradePoruka[kind]; ok {
		return n
	}
	return razmjena.NajvecaPoruka
}

const (
	// najduljiRazlog je koliko se bajtova razloga odbijenice druge strane
	// pamti i prikazuje
	najduljiRazlog = 512
	// najduljiProgram je koliko se bajtova izdanja i oznake gradnje druge
	// strane pamti (npr. 0.0.47-alfa, 0.0.47-alfa+606a83d5)
	najduljiProgram = 128
	// najduljaGreska je koliko se bajtova greške razgovora pamti i
	// prikazuje
	najduljaGreska = 1024
	// najviseKljucevaGranice: granica druge strane s više ključeva ne
	// pamti se uz stanje razmjene
	najviseKljucevaGranice = 4096
	// najduljiKljucGranice i najduljaVrijednostGranice: najdulji ključ
	// granice (autor ili autor|kanal) i vrijednost koji se pamte; oznaka
	// verzije ima 36 znakova (UUIDv7)
	najduljiKljucGranice      = 256
	najduljaVrijednostGranice = 64
)

// skrati vraća najviše n bajtova teksta, rezano na granici znaka
func skrati(tekst string, n int) string {
	if len(tekst) <= n {
		return tekst
	}
	for n > 0 && !utf8.RuneStart(tekst[n]) {
		n--
	}
	return tekst[:n]
}

// skracenaGreska je greška razgovora čiji je tekst predug za stanje razmjene
// i dnevnik; errors.Is i errors.As i dalje vide izvornu grešku
type skracenaGreska struct{ err error }

func (e skracenaGreska) Error() string { return skrati(e.err.Error(), najduljaGreska) + "…" }

func (e skracenaGreska) Unwrap() error { return e.err }

// ograniciGresku ostavi grešku kakva jest kad joj je tekst kratak, a inače
// je omota da se tekst skrati
func ograniciGresku(err error) error {
	if err == nil || len(err.Error()) <= najduljaGreska {
		return err
	}
	return skracenaGreska{err}
}

// granicaZaPamcenje je granica druge strane kakva se pamti uz stanje
// razmjene (za „prima se još” i zaostatak na pločici): samo ključevi
// i vrijednosti ispravne duljine. Granica s više od najviseKljucevaGranice
// takvih ključeva ne pamti se (nil).
func granicaZaPamcenje(granica map[string]string) map[string]string {
	if granica == nil {
		return nil
	}
	out := make(map[string]string, min(len(granica), najviseKljucevaGranice))
	for k, v := range granica {
		if !kljucZaPamcenje(k, v) {
			continue
		}
		if len(out) == najviseKljucevaGranice {
			return nil
		}
		out[k] = v
	}
	return out
}

// kljucZaPamcenje: ni ključ (autor, ili autor|kanal) ni vrijednost nisu
// prazni ni predugi
func kljucZaPamcenje(k, v string) bool {
	return k != "" && v != "" && len(k) <= najduljiKljucGranice && len(v) <= najduljaVrijednostGranice
}

// najvecaVerzija je najveća verzija, zapisana kao JSON za slanje, koju ovaj
// čvor upisuje iz razmjene i šalje dalje: sama u delti, s omotnicom, stane
// u ogradu delte primatelja (ogradaPoruke). Pošiljatelj prvu verziju delte
// šalje uvijek, pa bi veća zaustavila svaku razmjenu s primateljem od
// 0.0.47. JSON za slanje zna biti i šest puta dulji od primljenog (znakovi
// <, > i & idu kao \u003c), pa se mjeri kako se šalje.
var najvecaVerzija = int(ogradePoruka[kindDelta]) - 1<<20

// prevelikaVerzija javlja da verzija zapisana za slanje prelazi
// najvecaVerzija. Verzija kojoj ni šest puta dulji tekst ne bi prešao
// ogradu ne zapisuje se.
func prevelikaVerzija(v ledger.Version) bool {
	sirovo := len(v.Payload) + len(v.Entity) + len(v.EntityID) + len(v.NodeID) + len(v.Channel) + len(v.Supersedes) + len(v.VersionID)
	if 6*sirovo+512 <= najvecaVerzija {
		return false
	}
	b, err := json.Marshal(v)
	return err == nil && len(b) > najvecaVerzija
}
