package peers

// .cop paket karte: vektorska karta bez interneta (PMTiles) s manifestom i
// potpisom izdavača, u istom ZIP obliku kao paket kanala. Ne ide u knjigu
// verzija: razmjenom putuje samo opis karte (models.VektorskaKarta u
// zasebnoj postavci karta_bez_interneta), a datoteka se na svaki čvor
// učitava ovim paketom. Opis nosi i otiske dijelova karte; oni se ne
// potpisuju, nego se pri spremanju računaju iz bajtova.
//
// Unutra je ZIP:
//   manifest.json   vrsta "karta", opis karte, izdavač i potpis
//   karta.pmtiles   karta, bez sažimanja (PMTiles je već sažet)

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gocop/internal/models"
)

// VrstaPaketaKarte je oznaka paketa karte u manifestu
const VrstaPaketaKarte = "karta"

// KartaInacica je oblik paketa karte koji ovaj program čita
const KartaInacica = 1

// KartaManifest je ono što se o paketu karte zna prije raspakiravanja
type KartaManifest struct {
	Inacica int                   `json:"inacica"`
	Vrsta   string                `json:"vrsta"`
	Karta   models.VektorskaKarta `json:"karta"`
	Izdao   string                `json:"izdao"`
	Nastalo time.Time             `json:"nastalo"`
	Potpis  *CopPotpis            `json:"potpis,omitempty"`
}

// kanonskaKarta je ono što se potpisuje, uvijek istim redom
func kanonskaKarta(m KartaManifest) []byte {
	var b strings.Builder
	red := func(k, v string) { b.WriteString(k + "\t" + v + "\n") }
	k := m.Karta
	red("inacica", strconv.Itoa(m.Inacica))
	red("vrsta", m.Vrsta)
	red("naziv", k.Naziv)
	red("izvor", k.Izvor)
	red("otisak", k.Otisak)
	red("bajtova", strconv.FormatInt(k.Bajtova, 10))
	red("obuhvat", fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", k.Obuhvat[0], k.Obuhvat[1], k.Obuhvat[2], k.Obuhvat[3]))
	red("najvise_z", strconv.Itoa(k.NajviseZ))
	red("izdao", m.Izdao)
	return []byte(b.String())
}

// ImePaketaKarte je naziv datoteke paketa: gocop-karta-sektor-b.cop
func ImePaketaKarte(k models.VektorskaKarta) string {
	ime := strings.TrimPrefix(models.ImeIkoneIzNaziva(k.Naziv), "v-")
	if ime == "" {
		ime = k.Otisak[:12]
	}
	return "gocop-karta-" + ime + ".cop"
}

// IzveziKartu piše potpisani paket karte iz datoteke na disku
func (s *Service) IzveziKartu(k models.VektorskaKarta, put string, w io.Writer) (KartaManifest, error) {
	m := KartaManifest{Inacica: KartaInacica, Vrsta: VrstaPaketaKarte, Karta: k, Izdao: s.node.ID, Nastalo: time.Now().UTC()}
	f, err := os.Open(put)
	if err != nil {
		return m, errors.New("karte nema na ovom računalu")
	}
	defer f.Close() //nolint:errcheck // samo čitanje
	if len(s.node.key) > 0 {
		sig := ed25519.Sign(s.node.key, kanonskaKarta(m))
		m.Potpis = &CopPotpis{Kljuc: base64.StdEncoding.EncodeToString(s.node.key.Public().(ed25519.PublicKey)), Potpis: base64.StdEncoding.EncodeToString(sig)}
	}
	z := zip.NewWriter(w)
	manifest, _ := json.MarshalIndent(m, "", " ") // samo tekst, brojevi i vrijeme
	mw, err := z.Create("manifest.json")
	if err == nil {
		_, err = mw.Write(manifest)
	}
	var kw io.Writer
	if err == nil {
		kw, err = z.CreateHeader(&zip.FileHeader{Name: "karta.pmtiles", Method: zip.Store, Modified: m.Nastalo})
	}
	if err == nil {
		_, err = io.Copy(kw, f)
	}
	if err == nil {
		err = z.Close()
	}
	return m, err
}

// ProvjeriKartaPotpis javlja je li potpis valjan i vraća ključ izdavača
func ProvjeriKartaPotpis(m KartaManifest) (ed25519.PublicKey, error) {
	if m.Potpis == nil || m.Potpis.Potpis == "" {
		return nil, errors.New("paket karte nije potpisan")
	}
	javni, err := base64.StdEncoding.DecodeString(m.Potpis.Kljuc)
	if err != nil || len(javni) != ed25519.PublicKeySize {
		return nil, errors.New("ključ izdavača nije upotrebljiv")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Potpis.Potpis)
	if err != nil || !ed25519.Verify(javni, kanonskaKarta(m), sig) {
		return nil, errors.New("potpis ne odgovara paketu karte")
	}
	return javni, nil
}

// UveziKartu provjerava paket karte (potpis člana mreže, otisak i veličinu
// karte) i sprema kartu u mapu kao <otisak>.pmtiles. Paket koji je
// potpisao ovaj čvor prolazi i prije nego što čvor ima mrežu.
func (s *Service) UveziKartu(ctx context.Context, put, mapa string) (models.VektorskaKarta, error) {
	z, err := zip.OpenReader(put)
	if err != nil {
		return models.VektorskaKarta{}, errors.New("datoteka nije .cop paket")
	}
	defer func() { _ = z.Close() }() // samo čitanje
	m, kf, err := citajPaketKarte(&z.Reader)
	if err != nil {
		return m.Karta, err
	}
	if err := s.provjeriIzdavacaKarte(ctx, m); err != nil {
		return m.Karta, err
	}
	return raspakirajKartu(kf, m.Karta, mapa)
}

// raspakirajKartu sprema kartu iz paketa i vraća opis spremljene karte, s
// dijelovima izračunatim iz bajtova; karta koja ne odgovara opisu iz
// potpisanog manifesta ne ostaje na disku
func raspakirajKartu(kf *zip.File, opis models.VektorskaKarta, mapa string) (models.VektorskaKarta, error) {
	rc, err := kf.Open()
	if err != nil {
		return opis, errors.New("karta u paketu se ne da otvoriti")
	}
	defer func() { _ = rc.Close() }() // samo čitanje
	k, err := SpremiKartu(rc, mapa, opis.Naziv, opis.Izvor)
	if err == nil && (k.Otisak != opis.Otisak || k.Bajtova != opis.Bajtova) {
		_ = os.Remove(PutKarte(mapa, k.Otisak)) // mogla je biti i prije: tada je ista karta, pa ponovno učitavanje vraća
		err = errors.New("karta u paketu ne odgovara otisku iz manifesta")
	}
	if err != nil {
		return opis, err
	}
	return k, nil
}

func citajPaketKarte(z *zip.Reader) (KartaManifest, *zip.File, error) {
	mf, kf := datotekaPaketa(z, "manifest.json"), datotekaPaketa(z, "karta.pmtiles")
	m, err := manifestKarte(mf)
	switch {
	case err != nil:
		return m, nil, err
	case m.Inacica > KartaInacica:
		return m, nil, fmt.Errorf("paket karte je izdao noviji program (inačica %d) — nadogradite goCOP", m.Inacica)
	case kf == nil:
		return m, nil, errors.New("u paketu nema karte")
	}
	return m, kf, nil
}

func datotekaPaketa(z *zip.Reader, ime string) *zip.File {
	for _, f := range z.File {
		if f.Name == ime {
			return f
		}
	}
	return nil
}

// manifestKarte čita opis iz paketa; paket bez opisa karte nije paket karte
func manifestKarte(mf *zip.File) (KartaManifest, error) {
	var m KartaManifest
	if mf == nil || mf.UncompressedSize64 > 1<<20 {
		return m, errors.New("u paketu nema opisa karte")
	}
	rc, err := mf.Open()
	if err != nil {
		return m, errors.New("u paketu nema opisa karte")
	}
	b, _ := io.ReadAll(rc) // prekinuto čitanje daje krnji JSON, koji se odbije niže
	_ = rc.Close()         // samo čitanje
	if json.Unmarshal(b, &m) != nil || m.Vrsta != VrstaPaketaKarte {
		return m, errors.New("ovo nije paket karte; paket podataka učitava se u Održavanju baze")
	}
	return m, nil
}

func (s *Service) provjeriIzdavacaKarte(ctx context.Context, m KartaManifest) error {
	javni, err := ProvjeriKartaPotpis(m)
	if err != nil {
		return err
	}
	if len(s.node.key) > 0 && bytes.Equal(javni, s.node.key.Public().(ed25519.PublicKey)) {
		return nil
	}
	clan, err := s.izdavacClan(ctx, m.Izdao, javni)
	if err != nil {
		return err
	}
	if !clan {
		return errors.New("ovo računalo još nije u mreži, pa ne može provjeriti tko je izdao paket karte")
	}
	return nil
}

// PrivremenaDatoteka sprema tok, najviše toliko bajtova, u novu datoteku u
// mapi (koju po potrebi napravi) i vraća njezinu putanju; datoteku briše
// pozivatelj, i kad je greška
func PrivremenaDatoteka(r io.Reader, mapa, uzorak string, najvise int64) (string, error) {
	if err := os.MkdirAll(mapa, 0o750); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(mapa, uzorak)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, io.LimitReader(r, najvise+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > najvise {
		err = errors.New("datoteka je prevelika")
	}
	if err != nil {
		return f.Name(), errors.New("datoteka nije do kraja stigla")
	}
	return f.Name(), nil
}

// PutKarte je datoteka karte s tim otiskom u mapi karata
func PutKarte(mapa, otisak string) string {
	return filepath.Join(mapa, otisak+".pmtiles")
}

// NajvecaKarta ograđuje kartu koja se učitava (varijabla samo zato da je
// test smanji)
var NajvecaKarta int64 = 2 << 30

// VelicinaDijelaKarte je veličina dijela karte čiji se otisak upisuje u
// opis (varijabla samo zato da je test smanji; od 1 do
// models.NajveciDioKarte, inače SpremiKartu kartu odbije prije upisa)
var VelicinaDijelaKarte int64 = 4 << 20

// SpremiKartu sprema učitanu PMTiles kartu u mapu pod njezinim otiskom i iz
// zaglavlja čita područje i najveće povećanje, a usput računa otiske
// dijelova. Prima samo vektorsku kartu.
func SpremiKartu(r io.Reader, mapa, naziv, izvor string) (models.VektorskaKarta, error) {
	k := models.VektorskaKarta{Naziv: strings.TrimSpace(naziv), Izvor: strings.TrimSpace(izvor)}
	if err := os.MkdirAll(mapa, 0o750); err != nil {
		return k, err
	}
	u, err := upisiPrivremeno(r, mapa)
	defer os.Remove(u.put) //nolint:errcheck // nakon preimenovanja je nema
	if err != nil {
		return k, err
	}
	if err := zaglavljeKarte(u.glava, &k); err != nil {
		return k, err
	}
	k.Otisak, k.Bajtova, k.Dio, k.Dijelovi = u.otisak, u.n, VelicinaDijelaKarte, u.dijelovi
	if _, err := k.Provjereno(); err != nil {
		return k, err
	}
	return k, os.Rename(u.put, PutKarte(mapa, k.Otisak))
}

// upisanaKarta je privremena datoteka karte s onim što se o njoj saznalo
// dok je stizala
type upisanaKarta struct {
	put      string
	glava    []byte // zaglavlje PMTiles
	otisak   string
	n        int64
	dijelovi []string
}

// upisiPrivremeno sprema kartu kako stiže u privremenu datoteku u mapi
// karata, usput joj računa otisak i otiske dijelova i pamti zaglavlje
func upisiPrivremeno(r io.Reader, mapa string) (upisanaKarta, error) {
	if !dioKarteIspravan(VelicinaDijelaKarte) {
		return upisanaKarta{}, errors.New("veličina dijela karte nije ispravna")
	}
	tmp, err := os.CreateTemp(mapa, "ucitavanje-*.pmtiles")
	if err != nil {
		return upisanaKarta{}, err
	}
	h, g, d := sha256.New(), &pocetak{}, &dijeloviPisac{velicina: VelicinaDijelaKarte, h: sha256.New()}
	n, err := io.Copy(io.MultiWriter(tmp, h, g, d), io.LimitReader(r, NajvecaKarta+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		err = errors.New("karta nije do kraja stigla")
	case n > NajvecaKarta:
		err = errors.New("karta je prevelika (najviše 2 GB)")
	}
	return upisanaKarta{put: tmp.Name(), glava: g.b, otisak: hex.EncodeToString(h.Sum(nil)), n: n, dijelovi: d.zatvori()}, err
}

// dioKarteIspravan: dio od 1 do models.NajveciDioKarte bajtova; s nulom bi
// dijeloviPisac stao u beskonačnoj petlji
func dioKarteIspravan(velicina int64) bool {
	return velicina >= 1 && velicina <= models.NajveciDioKarte
}

// dijeloviPisac računa SHA-256 svakog dijela od velicina bajtova onoga što
// kroz njega prođe
type dijeloviPisac struct {
	velicina int64
	h        hash.Hash
	n        int64 // bajtova u dijelu koji je u tijeku
	otisci   []string
}

func (d *dijeloviPisac) Write(b []byte) (int, error) {
	ukupno := len(b)
	for len(b) > 0 {
		m := min(d.velicina-d.n, int64(len(b)))
		_, _ = d.h.Write(b[:m]) // hash.Hash.Write nikad ne vraća grešku
		d.n += m
		b = b[m:]
		if d.n == d.velicina {
			d.dovrsi()
		}
	}
	return ukupno, nil
}

func (d *dijeloviPisac) dovrsi() {
	d.otisci = append(d.otisci, hex.EncodeToString(d.h.Sum(nil)))
	d.h.Reset()
	d.n = 0
}

// zatvori dovrši zadnji, kraći dio i vraća otiske svih dijelova
func (d *dijeloviPisac) zatvori() []string {
	if d.n > 0 {
		d.dovrsi()
	}
	return d.otisci
}

// pocetak pamti prvih 127 bajtova (zaglavlje PMTiles) onoga što kroz njega prođe
type pocetak struct{ b []byte }

func (p *pocetak) Write(b []byte) (int, error) {
	if ostalo := 127 - len(p.b); ostalo > 0 {
		p.b = append(p.b, b[:min(ostalo, len(b))]...)
	}
	return len(b), nil
}

// zaglavljeKarte čita zaglavlje PMTiles v3 (127 bajtova): vrsta pločica,
// najveće povećanje i područje u desetmilijuntim dijelovima stupnja
func zaglavljeKarte(z []byte, k *models.VektorskaKarta) error {
	if len(z) < 127 || string(z[:7]) != "PMTiles" || z[7] != 3 {
		return errors.New("datoteka nije karta u obliku PMTiles (inačica 3)")
	}
	if z[99] != 1 {
		return errors.New("karta nije vektorska (PMTiles s pločicama MVT)")
	}
	stupanj := func(i int) float64 {
		return float64(int32(uint32(z[i])|uint32(z[i+1])<<8|uint32(z[i+2])<<16|uint32(z[i+3])<<24)) / 1e7
	}
	k.NajviseZ = int(z[101])
	k.Obuhvat = [4]float64{stupanj(102), stupanj(106), stupanj(110), stupanj(114)}
	return nil
}
