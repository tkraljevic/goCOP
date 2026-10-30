package razmjena

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// PotvrdaPrava je odluka nositelja ključa mreže o jednoj verziji zapisa iz
// knjige: verzija koja mijenja prava vrijedi (potvrđeno) ili ne vrijedi
// (odbijeno). Potvrda putuje kao i svaki drugi zapis, a vrijedi samo kad je
// potpisana ključem mreže; to provjerava onaj tko je koristi (Vrijedi).
//
// Potpis pokriva verziju (oznaka verzije, entitet, oznaka zapisa i otisak
// sadržaja), vrijednosti polja prava kakve vrijede nakon odluke (Prava,
// kako ih zapisuje program), odluku, način, tko je i na kojem računalu
// odlučio, i kad. Prava se potpisuju kao vrijednosti, ne kao otisak: tko
// koristi potvrdu, uzima ih iz nje, pa vrijedi i kad potvrđene verzije u
// knjizi više nema (sažimanje) ili još nije stigla.
//
// Oznaka potvrde počinje entitetom i oznakom zapisa (Oznaka), pa se sve
// potvrde jednog zapisa čitaju iz knjige jednim rasponom oznaka.
type PotvrdaPrava struct {
	ID        string    `json:"id"`
	Odluka    string    `json:"odluka"` // OdlukaPotvrdeno ili OdlukaOdbijeno
	VersionID string    `json:"versionId"`
	Entitet   string    `json:"entitet"`
	Zapis     string    `json:"zapis"`
	Otisak    string    `json:"otisak"`   // sadržaja verzije (OtisakVerzije)
	Prava     string    `json:"prava"`    // vrijednosti polja prava kao JSON, kako ih zapisuje program
	Nacin     string    `json:"nacin"`    // NacinOdluka, NacinIzmjena ili NacinStanje
	Potvrdio  string    `json:"potvrdio"` // osoba; prazno za izmjenu i stanje
	Racunalo  string    `json:"racunalo"` // računalo s ključem mreže na kojem je odlučeno
	Kad       time.Time `json:"kad"`
	Mreza     string    `json:"mreza"` // javni ključ mreže koji je potpisao
	Potpis    string    `json:"potpis"`
}

// Odluke o verziji
const (
	OdlukaPotvrdeno = "potvrdeno"
	OdlukaOdbijeno  = "odbijeno"
)

// Načini odluke: osoba je odlučila o promjeni koja je čekala, izmjena je
// napravljena na računalu s ključem mreže, ili je potpisano stanje koje je
// to računalo već imalo (prije potvrda ključem mreže)
const (
	NacinOdluka  = "odluka"
	NacinIzmjena = "izmjena"
	NacinStanje  = "stanje"
)

// oznakaOblikaPotvrde je oznaka kanonskog oblika potvrde. Ne mijenja se:
// potvrde koje su već potpisane prestale bi vrijediti.
const oznakaOblikaPotvrde = "potvrda-prava/1"

// ErrBezKljucaMreze: ovo računalo ne drži ključ mreže
var ErrBezKljucaMreze = errors.New("ovo računalo ne drži ključ mreže")

// potvrdaZaPotpis je kanonski oblik potvrde koji se potpisuje
type potvrdaZaPotpis struct {
	V         string `json:"v"`
	Mreza     string `json:"mreza"`
	Odluka    string `json:"odluka"`
	VersionID string `json:"versionId"`
	Entitet   string `json:"entitet"`
	Zapis     string `json:"zapis"`
	Otisak    string `json:"otisak"`
	Prava     string `json:"prava"`
	Nacin     string `json:"nacin"`
	Potvrdio  string `json:"potvrdio"`
	Racunalo  string `json:"racunalo"`
	Kad       int64  `json:"kad"`
}

func (p PotvrdaPrava) signedBytes() []byte {
	oblik := potvrdaZaPotpis{oznakaOblikaPotvrde, p.Mreza, p.Odluka, p.VersionID, p.Entitet, p.Zapis,
		p.Otisak, p.Prava, p.Nacin, p.Potvrdio, p.Racunalo, p.Kad.Unix()}
	b, _ := json.Marshal(oblik) // nizovi i brojevi uvijek se daju zapisati
	return b
}

// PrefiksPotvrda je početak oznake svake potvrde zapisa entitet/zapis
func PrefiksPotvrda(entitet, zapis string) string { return entitet + ":" + zapis + ":" }

// Oznaka je oznaka koju potvrda mora imati: entitet, zapis, verzija i odluka
func (p PotvrdaPrava) Oznaka() string {
	return PrefiksPotvrda(p.Entitet, p.Zapis) + p.VersionID + ":" + p.Odluka
}

// PotpisiPotvrdu potpisuje odluku ključem mreže, s današnjim vremenom
func (n NetworkKey) PotpisiPotvrdu(p PotvrdaPrava) (PotvrdaPrava, error) {
	if !n.CanSign() {
		return p, ErrBezKljucaMreze
	}
	p.Mreza, p.Kad = PublicKeyString(n.Public), time.Now().UTC().Truncate(time.Second)
	p.ID = p.Oznaka()
	p.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(n.private, p.signedBytes()))
	return p, nil
}

// Vrijedi javlja je li potvrdu potpisao ključ mreže mreza: poznata odluka,
// oznaka izvedena iz sadržaja i potpis koji vrijedi
func (p PotvrdaPrava) Vrijedi(mreza ed25519.PublicKey) bool {
	if p.Mreza != PublicKeyString(mreza) || p.ID != p.Oznaka() {
		return false
	}
	if p.Odluka != OdlukaPotvrdeno && p.Odluka != OdlukaOdbijeno {
		return false
	}
	potpis, err := base64.StdEncoding.DecodeString(p.Potpis)
	return err == nil && ed25519.Verify(mreza, p.signedBytes(), potpis)
}

// OtisakVerzije je otisak sadržaja verzije i toga je li spomenik. JSON se
// prije sažimanja zbije, pa razmaci koje bi netko usput dodao ne mijenjaju
// otisak; sadržaj koji nije JSON sažima se kakav jest.
func OtisakVerzije(obrisano bool, sadrzaj []byte) string {
	var b bytes.Buffer
	if json.Compact(&b, sadrzaj) != nil {
		b.Reset()
		b.Write(sadrzaj)
	}
	h := sha256.New()
	if obrisano {
		h.Write([]byte("obrisano\n"))
	} else {
		h.Write([]byte("zivo\n"))
	}
	h.Write(b.Bytes())
	return hex.EncodeToString(h.Sum(nil))
}
