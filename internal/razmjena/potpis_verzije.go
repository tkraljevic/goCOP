package razmjena

// Potpis autora verzije. Računalo koje zapiše verziju u knjigu potpiše je
// svojim ključem, a tko je primi, provjeri da ju je napisao baš taj ključ i
// da nitko usput nije promijenio ni jedan njezin podatak.
//
// Potpisuje se oznaka "<protokol>-verzija-v1|" i json.Marshal stalne
// strukture (potpisanaVerzija), polja ovim redom:
//
//	version_id, entity, entity_id, node_id, supersedes, archived, channel,
//	schema_version, created_at (Unix sekunde), kljuc_autora (base64),
//	sadrzaj (heks SHA-256)
//
// Sadržaj se ne potpisuje kakav jest, nego njegov sažetak nad oblikom koji
// Go šalje žicom: json.Marshal(json.RawMessage(sadrzaj)), dakle zbijeno, a
// <, >, &, U+2028 i U+2029 izbjegnuti. Tako isti zapis daje isti sažetak
// bez obzira na to je li ga računalo spremilo s razmacima ili bez njih.
// created_at se potpisuje u sekundama, jer dio ispod sekunde ne preživi
// svaki put (baza, razmjena, .cop, .db).
//
// Ovaj oblik se ne mijenja: verzije koje su njime već potpisane prestale bi
// vrijediti. Paket ne ovisi o knjizi, pa verziju prima kao obične
// vrijednosti (PodaciVerzije).

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// PodaciVerzije su podaci verzije iz knjige koje potpis pokriva
type PodaciVerzije struct {
	VersionID     string
	Entity        string
	EntityID      string
	NodeID        string
	Supersedes    string
	Archived      bool
	Channel       string
	SchemaVersion int
	CreatedAt     time.Time
	Payload       []byte
}

// potpisanaVerzija je kanonski oblik verzije koji se potpisuje
type potpisanaVerzija struct {
	VersionID     string `json:"version_id"`
	Entity        string `json:"entity"`
	EntityID      string `json:"entity_id"`
	NodeID        string `json:"node_id"`
	Supersedes    string `json:"supersedes"`
	Archived      bool   `json:"archived"`
	Channel       string `json:"channel"`
	SchemaVersion int    `json:"schema_version"`
	CreatedAt     int64  `json:"created_at"`
	KljucAutora   string `json:"kljuc_autora"`
	Sadrzaj       string `json:"sadrzaj"`
}

// Greške potpisa verzije
var (
	ErrOblikVerzije  = errors.New("verzija se ne da potpisati: sadržaj nije JSON")
	ErrKljucAutora   = errors.New("ključ autora verzije nije ispravan")
	ErrPotpisVerzije = errors.New("potpis verzije ne vrijedi")
)

// prefiksVerzije je oznaka ispred potpisanih bajtova verzije; potpis iz
// mreže s drugim imenom protokola ovdje ne vrijedi
func prefiksVerzije(protokol string) string { return protokol + "-verzija-v1|" }

// SazetakSadrzaja je heks SHA-256 sadržaja u obliku koji Go šalje žicom
func SazetakSadrzaja(sadrzaj []byte) (string, error) {
	b, err := json.Marshal(json.RawMessage(sadrzaj))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrOblikVerzije, err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// BajtoviVerzije su bajtovi koje autor potpisuje: oznaka s imenom
// protokola i kanonski oblik verzije s ključem autora
func BajtoviVerzije(protokol string, v PodaciVerzije, kljucAutora string) ([]byte, error) {
	sazetak, err := SazetakSadrzaja(v.Payload)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(potpisanaVerzija{ // nizovi, brojevi i istina/laž uvijek se daju zapisati
		VersionID: v.VersionID, Entity: v.Entity, EntityID: v.EntityID, NodeID: v.NodeID,
		Supersedes: v.Supersedes, Archived: v.Archived, Channel: v.Channel,
		SchemaVersion: v.SchemaVersion, CreatedAt: v.CreatedAt.Unix(),
		KljucAutora: kljucAutora, Sadrzaj: sazetak,
	})
	return append([]byte(prefiksVerzije(protokol)), b...), nil
}

// PotpisiVerziju potpisuje verziju ključem autora; vraća potpis i javni
// ključ, oba base64, kako se spremaju uz verziju i šalju žicom
func PotpisiVerziju(autor ed25519.PrivateKey, protokol string, v PodaciVerzije) (potpis, kljucAutora string, err error) {
	kljucAutora = PublicKeyString(autor.Public().(ed25519.PublicKey))
	b, err := BajtoviVerzije(protokol, v, kljucAutora)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(autor, b)), kljucAutora, nil
}

// Nepotpisana: verzija nema ni potpisa ni ključa autora. Takvu primatelj
// ne provjerava kao potpisanu. Čim je jedno od dvoga zapisano, verzija je
// potpisana i potpis mora vrijediti.
func Nepotpisana(potpis, kljucAutora string) bool {
	return potpis == "" && kljucAutora == ""
}

// ProvjeriPotpisVerzije javlja vrijedi li potpis verzije pod ključem
// autora koji uz nju stoji. Kaže samo da je verziju potpisao taj ključ; je
// li ključ smio pisati pod imenom node_id, provjerava onaj tko verziju prima.
func ProvjeriPotpisVerzije(protokol string, v PodaciVerzije, potpis, kljucAutora string) error {
	kljuc, err := ParsePublicKey(kljucAutora)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKljucAutora, err)
	}
	b, err := BajtoviVerzije(protokol, v, kljucAutora)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(potpis)
	if err != nil || !ed25519.Verify(kljuc, b, sig) {
		return ErrPotpisVerzije
	}
	return nil
}
