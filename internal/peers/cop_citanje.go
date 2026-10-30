package peers

// Čitanje .cop paketa po dijelovima. Svaki dio ima svoju granicu, a svi
// pročitani bajtovi jednog čitanja zajedno najviše najveciCop. Manifest se čita
// prvi i sam, pa uvoz provjeri potpis i izdavača prije nego što raspakira
// ostatak. Sadržaji se ne drže u memoriji svi odjednom: provjera otiska ide
// kroz tok, a upis jedan po jedan.

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gocop/internal/ledger"
	"gocop/internal/raspakiraj"
)

// Granice dijelova paketa. Zapisi cijelog zajedničkog kanala danas su oko
// 200 MB; sadržaj je jedan PDF ili slika. Varijable su zbog testova.
var (
	najveciManifestCopa int64 = 1 << 20
	najveciPopisCopa    int64 = 64 << 20
	najveciZapisiCopa   int64 = 512 << 20
	najveciSadrzajCopa  int64 = 256 << 20
)

// najviseStavkiCopa je najviše stavki u ZIP-u paketa: tri dijela i
// uključeni sadržaji. Svi sadržaji zajedno smiju imati najveciCop, a slika ili
// PDF ima barem nekoliko kB. Varijabla je zbog testova.
var najviseStavkiCopa = 1 << 18

// najkraciZapisCopa: redak zapisa kraći od ovoga nije verzija. Verzija u
// paketu uvijek nosi sva polja (zaSlanje), pa i prazna ima 160 bajtova.
const najkraciZapisCopa = 120

const prefiksSadrzajaCopa = "sadrzaj/"

// copPaket je otvoren paket: dijelovi po imenu, ostatak ukupne granice i
// pročitan manifest.
type copPaket struct {
	dijelovi map[string]*zip.File
	g        *raspakiraj.Granica
	m        CopManifest
}

// otvoriCop otvara paket i čita samo manifest. Paket s dvostrukim ili
// nepoznatim dijelom ne čita se dalje. Vraća paket i kad javi grešku, da se
// vidi što je o manifestu poznato.
func otvoriCop(r io.ReaderAt, size int64) (*copPaket, error) {
	p := &copPaket{g: raspakiraj.Nova(najviseStavkiCopa, najveciCop)}
	z, err := otvoriZipCopa(r, size)
	if err != nil {
		return p, err
	}
	if p.dijelovi, err = dijeloviCopa(z); err != nil {
		return p, err
	}
	mb, err := p.citaj("manifest.json", najveciManifestCopa)
	if err == nil {
		err = citajManifestCopa(mb, &p.m)
	}
	if err != nil {
		return p, err
	}
	// Stariji oblik se čita (novi program zna stare pakete); noviji ne, jer
	// ovaj program ne zna što je u njemu dodano.
	if p.m.Inacica > CopInacica {
		return p, fmt.Errorf("paket je izdao noviji program (inačica %d, ovaj čita do %d) — nadogradite goCOP", p.m.Inacica, CopInacica)
	}
	return p, poznatiDijeloviCopa(p.dijelovi)
}

// otvoriZipCopa otvara ZIP paketa, ako nema više od najviseStavkiCopa
// stavki: zip.NewReader drži zapis za svaku stavku, pa se broj provjerava
// prije njega.
func otvoriZipCopa(r io.ReaderAt, size int64) (*zip.Reader, error) {
	if err := raspakiraj.ProvjeriBrojStavki(r, size, najviseStavkiCopa); err != nil {
		return nil, err
	}
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("datoteka nije .cop paket: %w", err)
	}
	return z, nil
}

// dijeloviCopa slaže dijelove po imenu; ime koje se ponavlja je greška, jer
// se ne zna koji je pravi.
func dijeloviCopa(z *zip.Reader) (map[string]*zip.File, error) {
	out := make(map[string]*zip.File, len(z.File))
	for _, f := range z.File {
		if _, ima := out[f.Name]; ima {
			return nil, fmt.Errorf("dio %s je u paketu dvaput", f.Name)
		}
		out[f.Name] = f
	}
	return out, nil
}

// poznatiDijeloviCopa traži da paket ima samo dijelove koje paket kanala
// ima: manifest, zapise, popis sadržaja i sadržaje po otisku. Zato novi dio
// paketa uvijek traži i novu CopInacica: stariji program inače takav paket
// odbije kao neispravan, a ne s porukom da treba nadogradnja.
func poznatiDijeloviCopa(dijelovi map[string]*zip.File) error {
	for ime := range dijelovi {
		if !poznatDioCopa(ime) {
			return fmt.Errorf("u paketu je dio %q kojeg paket kanala nema", ime)
		}
	}
	return nil
}

func poznatDioCopa(ime string) bool {
	switch ime {
	case "manifest.json", "zapisi.jsonl", "sadrzaji.json":
		return true
	}
	o, ok := strings.CutPrefix(ime, prefiksSadrzajaCopa)
	return ok && jeOtisak(o)
}

// jeOtisak: SHA-256 kao 64 mala heks znaka (sadrzaj.Otisak).
func jeOtisak(o string) bool {
	return len(o) == sha256.Size*2 && strings.Trim(o, "0123456789abcdef") == ""
}

// citaj čita cijeli dio, najviše najvise bajtova.
func (p *copPaket) citaj(ime string, najvise int64) ([]byte, error) {
	rc, err := p.otvori(ime, p.g, najvise)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("dio %s se ne da raspakirati: %w", ime, err)
	}
	return b, nil
}

// otvori otvara dio uz granicu dijela i granicu g. archive/zip ne pročita
// više od veličine iz zaglavlja dijela (javi ErrFormat), pa je zaglavlje
// dovoljno za granicu dijela; g broji stvarno pročitano.
func (p *copPaket) otvori(ime string, g *raspakiraj.Granica, najvise int64) (io.ReadCloser, error) {
	f, ima := p.dijelovi[ime]
	if !ima {
		return nil, fmt.Errorf("u paketu nema %s", ime)
	}
	if f.UncompressedSize64 > uint64(najvise) {
		return nil, fmt.Errorf("dio %s paketa je prevelik: smije imati najviše %d MB", ime, najvise>>20)
	}
	rc, err := g.Otvori(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ime, err)
	}
	return rc, nil
}

// zapisi čita zapise i popis sadržaja i provjerava ih otiskom iz manifesta.
// Oba dijela čitaju se kroz tok i staju čim imaju više stavki nego što
// manifest kaže, pa u memoriji nikad nije više od onoga što je potpisano.
func (p *copPaket) zapisi() ([]ledger.Version, []CopSadrzaj, error) {
	h := sha256.New()
	versions, err := p.citajZapise(h)
	if err != nil {
		return nil, nil, err
	}
	sadrzaji, err := p.citajPopis(len(versions))
	if err != nil {
		return nil, nil, err
	}
	if hex.EncodeToString(otisakPopisa(h, sadrzaji)) != p.m.Otisak {
		return nil, nil, fmt.Errorf("otisak paketa se ne slaže s manifestom: sadržaj je mijenjan ili oštećen")
	}
	if err := sadrzajiIzZapisa(sadrzaji, versions); err != nil {
		return nil, nil, err
	}
	return versions, sadrzaji, nil
}

// citajZapise čita zapise kroz tok i usput ih dodaje u otisak h.
func (p *copPaket) citajZapise(h io.Writer) ([]ledger.Version, error) {
	rc, err := p.otvori("zapisi.jsonl", p.g, najveciZapisiCopa)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	versions, err := razloziZapiseCopa(io.TeeReader(rc, h), p.m.Zapisa)
	if err == nil && len(versions) != p.m.Zapisa {
		err = fmt.Errorf("manifest kaže %d zapisa, u paketu ih je %d", p.m.Zapisa, len(versions))
	}
	return versions, err
}

// razloziZapiseCopa čita verzije, jednu po retku, najviše najvise; prazni
// reci se preskaču.
func razloziZapiseCopa(r io.Reader, najvise int) ([]ledger.Version, error) {
	var versions []ledger.Version
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		v, prazan, err := zapisCopa(sc.Bytes())
		if err != nil {
			return nil, err
		}
		if prazan {
			continue
		}
		if len(versions) >= najvise {
			return nil, fmt.Errorf("manifest kaže %d zapisa, a u paketu ih je više", najvise)
		}
		versions = append(versions, v)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("zapisi se ne daju pročitati: %w", err)
	}
	return versions, nil
}

// zapisCopa razlaže jedan redak zapisa; prazan redak nije greška.
func zapisCopa(redak []byte) (ledger.Version, bool, error) {
	var v ledger.Version
	if len(bytes.TrimSpace(redak)) == 0 {
		return v, true, nil
	}
	if len(redak) < najkraciZapisCopa {
		return v, false, fmt.Errorf("zapis od %d bajtova je prekratak da bi bio zapis paketa", len(redak))
	}
	if err := json.Unmarshal(redak, &v); err != nil {
		return v, false, fmt.Errorf("zapis nije čitljiv: %w", err)
	}
	return v, false, nil
}

// citajPopis čita popis sadržaja kroz tok. Popis ima točno onoliko stavki
// koliko manifest kaže, a ne više nego zapisa: svaki sadržaj navodi barem
// jedan zapis.
func (p *copPaket) citajPopis(zapisa int) ([]CopSadrzaj, error) {
	if p.m.Sadrzaja < 0 || p.m.Sadrzaja > zapisa {
		return nil, fmt.Errorf("manifest kaže %d sadržaja uz %d zapisa", p.m.Sadrzaja, zapisa)
	}
	rc, err := p.otvori("sadrzaji.json", p.g, najveciPopisCopa)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	sadrzaji, err := razloziPopisCopa(json.NewDecoder(rc), p.m.Sadrzaja)
	if err != nil {
		return nil, fmt.Errorf("popis sadržaja nije čitljiv: %w", err)
	}
	if len(sadrzaji) != p.m.Sadrzaja {
		return nil, fmt.Errorf("manifest kaže %d sadržaja, na popisu ih je %d", p.m.Sadrzaja, len(sadrzaji))
	}
	return sadrzaji, nil
}

// razloziPopisCopa čita niz stavki popisa, najviše najvise. Paket bez
// sadržaja ima null (IzveziCop piše prazan popis kao null).
func razloziPopisCopa(dec *json.Decoder, najvise int) ([]CopSadrzaj, error) {
	t, err := dec.Token()
	if err != nil || t == nil {
		return nil, krajPopisa(dec, err)
	}
	if t != json.Delim('[') {
		return nil, fmt.Errorf("popis nije niz")
	}
	out, err := stavkePopisa(dec, najvise)
	if err == nil {
		_, err = dec.Token() // zatvara niz
	}
	if err != nil {
		return nil, err
	}
	return out, krajPopisa(dec, nil)
}

// stavkePopisa čita stavke niza do kraja niza, najviše najvise.
func stavkePopisa(dec *json.Decoder, najvise int) ([]CopSadrzaj, error) {
	var out []CopSadrzaj
	vidjeno := map[string]bool{}
	for dec.More() {
		if len(out) >= najvise {
			return nil, fmt.Errorf("manifest kaže %d sadržaja, a na popisu ih je više", najvise)
		}
		sd, err := stavkaPopisa(dec, vidjeno)
		if err != nil {
			return nil, err
		}
		out = append(out, sd)
	}
	return out, nil
}

// stavkaPopisa čita jednu stavku: otisak mora biti otisak, i samo jednom.
func stavkaPopisa(dec *json.Decoder, vidjeno map[string]bool) (CopSadrzaj, error) {
	var sd CopSadrzaj
	if err := dec.Decode(&sd); err != nil {
		return sd, err
	}
	if !jeOtisak(sd.Otisak) {
		return sd, fmt.Errorf("%.20q nije otisak sadržaja", sd.Otisak)
	}
	if vidjeno[sd.Otisak] {
		return sd, fmt.Errorf("sadržaj %.12s je na popisu dvaput", sd.Otisak)
	}
	vidjeno[sd.Otisak] = true
	return sd, nil
}

// krajPopisa traži da iza popisa nema ničega osim razmaka.
func krajPopisa(dec *json.Decoder, err error) error {
	if err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("iza popisa ima još nešto")
	}
	return nil
}

// sadrzajiIzZapisa traži da svaki sadržaj s popisa navodi neki zapis paketa,
// kao kad paket slaže IzveziCop (sadrzajiPaketa).
func sadrzajiIzZapisa(sadrzaji []CopSadrzaj, versions []ledger.Version) error {
	if len(sadrzaji) == 0 {
		return nil
	}
	navedeni := sadrzajiPoOtisku(versions)
	for _, sd := range sadrzaji {
		if _, ima := navedeni[sd.Otisak]; !ima {
			return fmt.Errorf("sadržaj %.12s s popisa ne navodi nijedan zapis paketa", sd.Otisak)
		}
	}
	return nil
}

// provjeriSadrzaje provjerava otisak svakog uključenog sadržaja, kroz tok,
// bez držanja u memoriji. Sadržaj u paketu kojeg nema na popisu uključenih
// je greška.
func (p *copPaket) provjeriSadrzaje(sadrzaji []CopSadrzaj) error {
	ukljuceni := map[string]bool{}
	for _, sd := range sadrzaji {
		if !sd.Ukljucen {
			continue
		}
		ukljuceni[prefiksSadrzajaCopa+sd.Otisak] = true
		if err := p.provjeriSadrzaj(sd.Otisak); err != nil {
			return err
		}
	}
	for ime := range p.dijelovi {
		if strings.HasPrefix(ime, prefiksSadrzajaCopa) && !ukljuceni[ime] {
			return fmt.Errorf("dio %s nije na popisu uključenih sadržaja", ime)
		}
	}
	return nil
}

func (p *copPaket) provjeriSadrzaj(otisak string) error {
	rc, err := p.otvori(prefiksSadrzajaCopa+otisak, p.g, najveciSadrzajCopa)
	if err != nil {
		return err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return fmt.Errorf("dio %s%s se ne da raspakirati: %w", prefiksSadrzajaCopa, otisak, err)
	}
	if hex.EncodeToString(h.Sum(nil)) != otisak {
		return fmt.Errorf("sadržaj %.12s ne odgovara svom otisku", otisak)
	}
	return nil
}

// procitajSadrzaj čita jedan uključeni sadržaj za upis. Otisak je već
// provjeren (provjeriSadrzaje), pa ne troši ukupnu granicu još jednom.
func (p *copPaket) procitajSadrzaj(otisak string) ([]byte, error) {
	rc, err := p.otvori(prefiksSadrzajaCopa+otisak, raspakiraj.Nova(1, najveciSadrzajCopa), najveciSadrzajCopa)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
