package arhiva

// Potpis paketa: otisak dokazuje cjelovitost, potpis dokazuje tko ga je izdao.
//
// SHA-256 otisak pokazuje da sadržaj odgovara manifestu i otkriva slučajno
// oštećenje. Ne dokazuje ništa o izdavaču: tko promijeni sadržaj, izračuna novi
// otisak i upiše proizvoljno ime u "izdao". Paket koji putuje USB-om ili
// preuzet s tuđe mreže bez potpisa je samo tvrdnja.
//
// Potpisuje se KANONSKI zapis, ne sama datoteka manifesta. JSON se da zapisati
// na više načina — drukčiji razmak, drugi redoslijed polja — pa bi potpis nad
// sirovim bajtovima pukao na svakom prepakiravanju iako se sadržaj nije
// promijenio. Kanonski zapis je jedan redak po podatku, uvijek istim redom.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DioPaketa je jedan dio s vlastitim otiskom.
//
// Zajednički otisak računa se preko spojenih dijelova, što veže njihov sadržaj
// ali ne i granicu među njima. Otisak po dijelu veže i to: dio se ne može
// preseliti u susjedni a da zbroj ostane isti.
type DioPaketa struct {
	Ime     string `json:"ime"`
	Otisak  string `json:"otisak"`
	Bajtova int    `json:"bajtova"`
}

// kanonski slaže ono što se potpisuje, uvijek istim redom i oblikom.
func kanonski(m Manifest, dijelovi []DioPaketa) []byte {
	var b strings.Builder
	red := func(kljuc, vrijednost string) {
		b.WriteString(kljuc)
		b.WriteByte('\t')
		b.WriteString(vrijednost)
		b.WriteByte('\n')
	}
	red("inacica", strconv.Itoa(m.Inacica))
	red("letva", m.Letva)
	red("izdanje", strconv.Itoa(m.Izdanje))
	red("izdao", m.Izdao)
	red("otisak", m.Otisak)
	red("nizova", strconv.Itoa(m.Nizova))
	red("zapisa", strconv.Itoa(m.Zapisa))
	red("od", m.Od)
	red("do", m.Do)

	// Dijelovi idu po imenu, da poredak u ZIP-u ne mijenja potpis.
	poredani := append([]DioPaketa(nil), dijelovi...)
	sort.Slice(poredani, func(i, j int) bool { return poredani[i].Ime < poredani[j].Ime })
	for _, d := range poredani {
		red("dio", d.Ime+" "+d.Otisak+" "+strconv.Itoa(d.Bajtova))
	}
	return []byte(b.String())
}

// Potpisi je ono što izdavač stavlja uz manifest.
type Potpis struct {
	Kljuc   string `json:"kljuc"`  // javni ključ izdavača
	Vaznost string `json:"potpis"` // potpis kanonskog zapisa
}

// potpisi slaže potpis kanonskog zapisa privatnim ključem čvora.
func potpisi(m Manifest, dijelovi []DioPaketa, kljuc ed25519.PrivateKey) *Potpis {
	if len(kljuc) == 0 {
		return nil
	}
	p := ed25519.Sign(kljuc, kanonski(m, dijelovi))
	return &Potpis{
		Kljuc:   base64.StdEncoding.EncodeToString(kljuc.Public().(ed25519.PublicKey)),
		Vaznost: base64.StdEncoding.EncodeToString(p),
	}
}

// ProvjeriPotpis javlja je li potpis valjan za zadani manifest i dijelove.
//
// Vraća javni ključ kojim je potpisano. Tko je taj ključ i vjeruje li mu se
// odlučuje se drugdje: ovdje se zna samo da je potpis matematički ispravan.
func ProvjeriPotpis(m Manifest, dijelovi []DioPaketa, p *Potpis) (ed25519.PublicKey, error) {
	if p == nil || p.Vaznost == "" {
		return nil, fmt.Errorf("paket nije potpisan")
	}
	javni, err := base64.StdEncoding.DecodeString(p.Kljuc)
	if err != nil || len(javni) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("ključ izdavača nije upotrebljiv")
	}
	sig, err := base64.StdEncoding.DecodeString(p.Vaznost)
	if err != nil {
		return nil, fmt.Errorf("potpis nije upotrebljiv")
	}
	if !ed25519.Verify(javni, kanonski(m, dijelovi), sig) {
		return nil, fmt.Errorf("potpis ne odgovara sadržaju paketa")
	}
	return javni, nil
}

// otisciDijelova računa otisak svakog dijela.
func otisciDijelova(sadrzaj map[string][]byte, imena []string) []DioPaketa {
	out := make([]DioPaketa, 0, len(imena))
	for _, ime := range imena {
		b := sadrzaj[ime]
		z := sha256.Sum256(b)
		out = append(out, DioPaketa{Ime: ime, Otisak: hex.EncodeToString(z[:]), Bajtova: len(b)})
	}
	return out
}
