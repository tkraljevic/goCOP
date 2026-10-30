package razmjena

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Opoziv je zapis da potvrda (članstvo ili ovlast) jednog ključa, izdana u
// trenutku IssuedAt ili ranije, više ne vrijedi. Putuje kao i svaki drugi
// zapis, pa sam po sebi ne opoziva ništa: tko ga smije potpisati, provjerava
// onaj tko ga koristi (Potpisao).
//
// Oznaka opoziva izvedena je iz vrste, ključa, trenutka izdavanja i
// sažetka potpisa (Oznaka). Zapis s istom oznakom, a drugim sadržajem, zato
// nije ispravan sam za sebe (Ispravan).
type Opoziv struct {
	ID         string    `json:"id"`
	Vrsta      string    `json:"vrsta"`
	NodeID     string    `json:"nodeId"`
	PublicKey  string    `json:"publicKey"`
	IssuedAt   time.Time `json:"issuedAt"` // opozvana je potvrda izdana tada i sve starije istog ključa
	OpozvanoAt time.Time `json:"opozvanoAt"`
	Opozvao    string    `json:"opozvao"`
	Potpisnik  string    `json:"potpisnik"` // javni ključ koji je potpisao opoziv
	Potpis     string    `json:"potpis"`
}

// oznakaOblikaOpoziva je oznaka kanonskog oblika opoziva. Ne mijenja se:
// opozivi koji su već potpisani prestali bi vrijediti.
const oznakaOblikaOpoziva = "opoziv/1"

// opozivZaPotpis je kanonski oblik opoziva koji se potpisuje
type opozivZaPotpis struct {
	V          string `json:"v"`
	Vrsta      string `json:"vrsta"`
	NodeID     string `json:"nodeId"`
	PublicKey  string `json:"publicKey"`
	IssuedAt   int64  `json:"issuedAt"`
	OpozvanoAt int64  `json:"opozvanoAt"`
	Opozvao    string `json:"opozvao"`
	Potpisnik  string `json:"potpisnik"`
}

func (op Opoziv) signedBytes() []byte {
	b, _ := json.Marshal(opozivZaPotpis{oznakaOblikaOpoziva, op.Vrsta, op.NodeID, op.PublicKey, op.IssuedAt.Unix(), op.OpozvanoAt.Unix(), op.Opozvao, op.Potpisnik}) // nizovi i brojevi uvijek se daju zapisati
	return b
}

// Potpisi vraća opoziv potpisan ključem priv, s potpisnikom i oznakom
func (op Opoziv) Potpisi(priv ed25519.PrivateKey) Opoziv {
	op.Potpisnik = PublicKeyString(priv.Public().(ed25519.PublicKey))
	op.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, op.signedBytes()))
	op.ID = op.Oznaka()
	return op
}

// Oznaka je oznaka koju opoziv mora imati: vrsta, ključ, trenutak izdavanja
// i prvih osam bajtova sažetka potpisa
func (op Opoziv) Oznaka() string {
	potpis, _ := base64.StdEncoding.DecodeString(op.Potpis) // pokvaren potpis ne prolazi Potpisao, pa oznaka tada nije važna
	sazetak := sha256.Sum256(potpis)
	return fmt.Sprintf("%s:%s:%d:%s", op.Vrsta, op.PublicKey, op.IssuedAt.Unix(), hex.EncodeToString(sazetak[:8]))
}

// Potpisao javlja je li opoziv potpisao jedan od zadanih ključeva
func (op Opoziv) Potpisao(kljucevi ...ed25519.PublicKey) bool {
	potpis, err := base64.StdEncoding.DecodeString(op.Potpis)
	if err != nil {
		return false
	}
	for _, k := range kljucevi {
		if op.Potpisnik == PublicKeyString(k) && ed25519.Verify(k, op.signedBytes(), potpis) {
			return true
		}
	}
	return false
}

// Ispravan javlja vrijedi li opoziv sam za sebe: oznaka mu je izvedena iz
// sadržaja i potpisa, a potpis vrijedi za ključ naveden kao potpisnik.
// Smije li taj ključ opozivati, ovdje se ne gleda.
func (op Opoziv) Ispravan() bool {
	k, err := ParsePublicKey(op.Potpisnik)
	return err == nil && op.ID == op.Oznaka() && op.Potpisao(k)
}
