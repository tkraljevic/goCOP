package peers

// .cop inačica 3: potpisano izdanje jednog ili više kanala knjige verzija, s
// pripadajućim sadržajem po otisku. Isti oblik koji mreža prenosi među
// čvorovima, samo u datoteci: za USB, e-poštu ili računalo bez stalne veze.
//
// Unutra je ZIP:
//   manifest.json   što je unutra, tko je izdao, otisak i potpis
//   zapisi.jsonl    verzije kanala, jedna po retku, po version_id
//   sadrzaji.json   popis sadržaja koje zapisi navode (otisak, vrsta, veličina,
//                   kanal, je li uključen)
//   sadrzaj/<otisak> bajtovi uključenih sadržaja
//
// Otisak izdanja računa se preko zapisa i popisa sadržaja, ne preko toga jesu
// li bajtovi uključeni: paket s kazalom i paket sa svime isto su izdanje.
// Potpisuje se kanonski zapis manifesta ključem čvora; primatelj traži da je
// ključ član njegove mreže.

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// CopInacica je inačica oblika paketa kanala
const CopInacica = 3

// najveciCop ograđuje raspakirano: paket kanala nosi i sadržaj
const najveciCop = 2 << 30

// CopObuhvat kaže što je paket obuhvatio; prazno ili 0 znači bez ograde
type CopObuhvat struct {
	Vrsta  string `json:"vrsta,omitempty"` // ocitanja, dnevnici, prijave; prazno = sve
	Sektor string `json:"sektor,omitempty"`
	AreaID int    `json:"area_id,omitempty"`
	Od     int    `json:"od,omitempty"` // godina
	Do     int    `json:"do,omitempty"`
}

// Kljuc je oznaka izdanja u katalogu: isti obuhvat, isti niz brojeva
func (o CopObuhvat) Kljuc() string {
	vrsta := o.Vrsta
	if vrsta == "" {
		vrsta = "sve"
	}
	gdje := "sva"
	switch {
	case o.AreaID > 0:
		gdje = fmt.Sprintf("bp%d", o.AreaID)
	case o.Sektor != "":
		gdje = "sektor-" + strings.ToLower(o.Sektor)
	}
	kad := "sve"
	switch {
	case o.Od > 0 && o.Do > 0 && o.Od != o.Do:
		kad = fmt.Sprintf("%d-%d", o.Od, o.Do)
	case o.Od > 0:
		kad = fmt.Sprint(o.Od)
	case o.Do > 0:
		kad = fmt.Sprintf("do%d", o.Do)
	}
	return vrsta + "/" + gdje + "/" + kad
}

// CopSadrzaj je jedan sadržaj koji zapisi paketa navode
type CopSadrzaj struct {
	Otisak   string `json:"otisak"`
	Vrsta    string `json:"vrsta"`
	Bajtova  int    `json:"bajtova"`
	Kanal    string `json:"kanal,omitempty"`
	Ukljucen bool   `json:"ukljucen"`
}

// CopPotpis je potpis izdavača nad kanonskim zapisom manifesta
type CopPotpis struct {
	Kljuc  string `json:"kljuc"`
	Potpis string `json:"potpis"`
}

// CopManifest je ono što se o paketu zna prije raspakiravanja
type CopManifest struct {
	Inacica   int        `json:"inacica"`
	Obuhvat   CopObuhvat `json:"obuhvat"`
	Kanali    []string   `json:"kanali"`
	Izdanje   int        `json:"izdanje"`
	Prethodno string     `json:"prethodno,omitempty"` // otisak prethodnog izdanja istog obuhvata
	Izdao     string     `json:"izdao"`
	Nastalo   time.Time  `json:"nastalo"`
	Otisak    string     `json:"otisak"`
	Zapisa    int        `json:"zapisa"`
	Sadrzaja  int        `json:"sadrzaja"`
	Ukljuceno int        `json:"ukljuceno"` // koliko sadržaja nosi bajtove
	Potpis    *CopPotpis `json:"potpis,omitempty"`
}

// kanonskiCop je ono što se potpisuje: jedan redak po podatku, uvijek istim
// redom, da prepakiravanje JSON-a ne mijenja potpis
func kanonskiCop(m CopManifest) []byte {
	var b strings.Builder
	red := func(k, v string) { b.WriteString(k); b.WriteByte('\t'); b.WriteString(v); b.WriteByte('\n') }
	red("inacica", strconv.Itoa(m.Inacica))
	red("obuhvat", m.Obuhvat.Kljuc())
	kanali := append([]string(nil), m.Kanali...)
	sort.Strings(kanali)
	red("kanali", strings.Join(kanali, ","))
	red("izdanje", strconv.Itoa(m.Izdanje))
	red("prethodno", m.Prethodno)
	red("izdao", m.Izdao)
	red("otisak", m.Otisak)
	red("zapisa", strconv.Itoa(m.Zapisa))
	red("sadrzaja", strconv.Itoa(m.Sadrzaja))
	return []byte(b.String())
}

// otisakIzdanja veže zapise i popis sadržaja, ne i bajtove ni vrijeme
func otisakIzdanja(zapisi []byte, sadrzaji []CopSadrzaj) string {
	h := sha256.New()
	h.Write(zapisi)
	poredani := append([]CopSadrzaj(nil), sadrzaji...)
	sort.Slice(poredani, func(i, j int) bool { return poredani[i].Otisak < poredani[j].Otisak })
	for _, s := range poredani {
		fmt.Fprintf(h, "%s %s %d\n", s.Otisak, s.Vrsta, s.Bajtova)
	}
	return hex.EncodeToString(h.Sum(nil))
}

const shemaCop = `
CREATE TABLE IF NOT EXISTS cop_izdanja (
	kljuc TEXT PRIMARY KEY,
	izdanje INTEGER NOT NULL,
	otisak TEXT NOT NULL,
	nastalo TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS cop_primljena (
	kljuc TEXT NOT NULL,
	izdao TEXT NOT NULL,
	izdanje INTEGER NOT NULL,
	otisak TEXT NOT NULL,
	primljeno TEXT NOT NULL,
	PRIMARY KEY (kljuc, izdao)
);`

func (s *Service) shemaCopa(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, shemaCop)
	return err
}

// CopIzvjestaj kaže što je paket nosio i što je uvoz donio
type CopIzvjestaj struct {
	Manifest        CopManifest
	Verzija         int // u paketu
	Novih           int // novih u knjizi
	SadrzajaUpisano int
	SadrzajaZeljeno int
	PotpisValjan    bool
	IzdavacClan     bool
	Napomena        string
}

// IzveziCop sastavlja .cop izdanje zadanih kanala. sSadrzajem kaže nose li
// se i bajtovi sadržaja; bez njih paket je kazalo koje stane u kilobajte.
// Broj izdanja daje katalog: isti dok se otisak ne promijeni.
func (s *Service) IzveziCop(ctx context.Context, obuhvat CopObuhvat, channels []string, sSadrzajem bool, w io.Writer) (CopManifest, error) {
	m := CopManifest{Inacica: CopInacica, Obuhvat: obuhvat, Kanali: append([]string(nil), channels...), Izdao: s.node.ID, Nastalo: time.Now().UTC()}
	if err := s.shemaCopa(ctx); err != nil {
		return m, err
	}
	versions, err := s.rec.InChannels(ctx, channels)
	if err != nil {
		return m, err
	}
	if len(versions) == 0 {
		return m, fmt.Errorf("nema nijedne verzije u zadanim kanalima")
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionID < versions[j].VersionID })
	var zapisi bytes.Buffer
	enc := json.NewEncoder(&zapisi)
	for _, v := range versions {
		if err := enc.Encode(v); err != nil {
			return m, err
		}
	}
	// sadržaji koje zapisi navode: otisak iz zapisa izvornika
	sadrzaji := s.sadrzajiUZapisima(ctx, versions)
	m.Zapisa, m.Sadrzaja = len(versions), len(sadrzaji)
	m.Otisak = otisakIzdanja(zapisi.Bytes(), sadrzaji)

	// katalog: isti otisak, isto izdanje; drukčiji, sljedeće
	kljuc := obuhvat.Kljuc()
	var izdanje int
	var otisak, nastalo string
	err = s.db.QueryRowContext(ctx, `SELECT izdanje, otisak, nastalo FROM cop_izdanja WHERE kljuc = ?`, kljuc).Scan(&izdanje, &otisak, &nastalo)
	switch {
	case err == sql.ErrNoRows:
		m.Izdanje = 1
	case err != nil:
		return m, err
	case otisak == m.Otisak:
		m.Izdanje = izdanje
	default:
		m.Izdanje, m.Prethodno = izdanje+1, otisak
	}

	z := zip.NewWriter(w)
	dodaj := func(ime string, b []byte) error {
		f, err := z.Create(ime)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		return err
	}
	sp := s.spremisteSadrzaja()
	for i := range sadrzaji {
		if !sSadrzajem || sp == nil {
			continue
		}
		b, _, err := sp.Citaj(ctx, sadrzaji[i].Otisak)
		if err != nil {
			continue
		}
		if err := dodaj("sadrzaj/"+sadrzaji[i].Otisak, b); err != nil {
			return m, err
		}
		sadrzaji[i].Ukljucen = true
		m.Ukljuceno++
	}
	popis, _ := json.MarshalIndent(sadrzaji, "", " ")
	if err := dodaj("zapisi.jsonl", zapisi.Bytes()); err != nil {
		return m, err
	}
	if err := dodaj("sadrzaji.json", popis); err != nil {
		return m, err
	}
	if len(s.node.key) > 0 {
		sig := ed25519.Sign(s.node.key, kanonskiCop(m))
		m.Potpis = &CopPotpis{Kljuc: base64.StdEncoding.EncodeToString(s.node.key.Public().(ed25519.PublicKey)), Potpis: base64.StdEncoding.EncodeToString(sig)}
	}
	manifest, _ := json.MarshalIndent(m, "", " ")
	if err := dodaj("manifest.json", manifest); err != nil {
		return m, err
	}
	if err := z.Close(); err != nil {
		return m, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO cop_izdanja (kljuc, izdanje, otisak, nastalo) VALUES (?, ?, ?, ?)
		ON CONFLICT(kljuc) DO UPDATE SET izdanje = excluded.izdanje, otisak = excluded.otisak, nastalo = excluded.nastalo`,
		kljuc, m.Izdanje, m.Otisak, m.Nastalo.Format(time.RFC3339)); err != nil {
		return m, err
	}
	return m, nil
}

// sadrzajiUZapisima skuplja otiske sadržaja koje zapisi izvornika navode
func (s *Service) sadrzajiUZapisima(ctx context.Context, versions []ledger.Version) []CopSadrzaj {
	vidjeno := map[string]bool{}
	var out []CopSadrzaj
	for _, v := range versions {
		if !strings.HasSuffix(v.Entity, "_izvornici") || v.Archived {
			continue
		}
		var iz struct {
			Otisak  string `json:"otisak"`
			Bajtova int    `json:"bajtova"`
			Vrsta   string `json:"vrsta"`
		}
		if json.Unmarshal(v.Payload, &iz) != nil || iz.Otisak == "" || vidjeno[iz.Otisak] {
			continue
		}
		vidjeno[iz.Otisak] = true
		if iz.Vrsta == "" {
			iz.Vrsta = "application/pdf"
		}
		out = append(out, CopSadrzaj{Otisak: iz.Otisak, Vrsta: iz.Vrsta, Bajtova: iz.Bajtova, Kanal: v.Channel})
	}
	return out
}

// ProvjeriCopPotpis javlja je li potpis valjan i vraća ključ izdavača
func ProvjeriCopPotpis(m CopManifest) (ed25519.PublicKey, error) {
	if m.Potpis == nil || m.Potpis.Potpis == "" {
		return nil, fmt.Errorf("paket nije potpisan")
	}
	javni, err := base64.StdEncoding.DecodeString(m.Potpis.Kljuc)
	if err != nil || len(javni) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("ključ izdavača nije upotrebljiv")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Potpis.Potpis)
	if err != nil {
		return nil, fmt.Errorf("potpis nije upotrebljiv")
	}
	if !ed25519.Verify(javni, kanonskiCop(m), sig) {
		return nil, fmt.Errorf("potpis ne odgovara sadržaju paketa")
	}
	return javni, nil
}

// CitajCop otvara paket i provjerava ga bez ugradnje: manifest, otisak,
// potpis. Vraća manifest i pročitane dijelove.
func CitajCop(r io.ReaderAt, size int64) (CopManifest, []ledger.Version, []CopSadrzaj, map[string][]byte, error) {
	var m CopManifest
	z, err := zip.NewReader(r, size)
	if err != nil {
		return m, nil, nil, nil, fmt.Errorf("datoteka nije .cop paket: %w", err)
	}
	var ukupno int64
	citaj := func(ime string) ([]byte, error) {
		for _, f := range z.File {
			if f.Name != ime {
				continue
			}
			if ukupno += int64(f.UncompressedSize64); ukupno > najveciCop {
				return nil, fmt.Errorf("paket je prevelik")
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, najveciCop))
		}
		return nil, fmt.Errorf("u paketu nema %s", ime)
	}
	mb, err := citaj("manifest.json")
	if err != nil {
		return m, nil, nil, nil, err
	}
	if err := json.Unmarshal(mb, &m); err != nil {
		return m, nil, nil, nil, fmt.Errorf("manifest nije čitljiv: %w", err)
	}
	if m.Inacica != CopInacica {
		return m, nil, nil, nil, fmt.Errorf("paket je inačice %d, program čita inačicu %d", m.Inacica, CopInacica)
	}
	zb, err := citaj("zapisi.jsonl")
	if err != nil {
		return m, nil, nil, nil, err
	}
	sb, err := citaj("sadrzaji.json")
	if err != nil {
		return m, nil, nil, nil, err
	}
	var sadrzaji []CopSadrzaj
	if err := json.Unmarshal(sb, &sadrzaji); err != nil {
		return m, nil, nil, nil, fmt.Errorf("popis sadržaja nije čitljiv: %w", err)
	}
	if got := otisakIzdanja(zb, sadrzaji); got != m.Otisak {
		return m, nil, nil, nil, fmt.Errorf("otisak paketa se ne slaže s manifestom: sadržaj je mijenjan ili oštećen")
	}
	var versions []ledger.Version
	sc := bufio.NewScanner(bytes.NewReader(zb))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var v ledger.Version
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			return m, nil, nil, nil, fmt.Errorf("zapis nije čitljiv: %w", err)
		}
		versions = append(versions, v)
	}
	if len(versions) != m.Zapisa {
		return m, nil, nil, nil, fmt.Errorf("manifest kaže %d zapisa, u paketu ih je %d", m.Zapisa, len(versions))
	}
	bajtovi := map[string][]byte{}
	for _, sd := range sadrzaji {
		if !sd.Ukljucen {
			continue
		}
		b, err := citaj("sadrzaj/" + sd.Otisak)
		if err != nil {
			return m, nil, nil, nil, err
		}
		if sadrzaj.Otisak(b) != sd.Otisak {
			return m, nil, nil, nil, fmt.Errorf("sadržaj %.12s ne odgovara svom otisku", sd.Otisak)
		}
		bajtovi[sd.Otisak] = b
	}
	return m, versions, sadrzaji, bajtovi, nil
}

// UveziCop ugrađuje paket: provjera otiska i potpisa, izdavač mora biti
// član mreže (osim dok čvor još nema mrežu), izdanje ne smije biti starije
// od već primljenog istog izdavača; zatim verzije kao razmjenom, pa sadržaj.
func (s *Service) UveziCop(ctx context.Context, put string) (CopIzvjestaj, error) {
	var rep CopIzvjestaj
	f, err := os.Open(put)
	if err != nil {
		return rep, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return rep, err
	}
	m, versions, sadrzaji, bajtovi, err := CitajCop(f, st.Size())
	rep.Manifest, rep.Verzija = m, len(versions)
	if err != nil {
		return rep, err
	}
	javni, err := ProvjeriCopPotpis(m)
	if err != nil {
		return rep, err
	}
	rep.PotpisValjan = true
	rep.IzdavacClan = s.trusted(javni)
	if !rep.IzdavacClan {
		if s.NetworkInfo() != nil {
			return rep, fmt.Errorf("paket je potpisao čvor %s koji nije član naše mreže", m.Izdao)
		}
		rep.Napomena = "čvor još nije u mreži, pa se članstvo izdavača nije moglo provjeriti"
	}
	if err := s.shemaCopa(ctx); err != nil {
		return rep, err
	}
	kljuc := m.Obuhvat.Kljuc()
	var primljeno int
	var primljeniOtisak string
	err = s.db.QueryRowContext(ctx, `SELECT izdanje, otisak FROM cop_primljena WHERE kljuc = ? AND izdao = ?`, kljuc, m.Izdao).Scan(&primljeno, &primljeniOtisak)
	if err != nil && err != sql.ErrNoRows {
		return rep, err
	}
	if err == nil {
		if m.Izdanje < primljeno {
			return rep, fmt.Errorf("paket je izdanje %d, a od čvora %s već je primljeno izdanje %d; starije se ne ugrađuje", m.Izdanje, m.Izdao, primljeno)
		}
		if m.Izdanje == primljeno && m.Otisak != primljeniOtisak {
			return rep, fmt.Errorf("izdanje %d od čvora %s već je primljeno s drugim otiskom; paket se ne ugrađuje", m.Izdanje, m.Izdao)
		}
	}
	// zapise čvor uzima po svojoj pretplati i ogradi, kao razmjenom
	myWants, err := s.CurrentWants(ctx)
	if err != nil {
		return rep, err
	}
	filter := s.wantsFunc(ctx, &myWants)
	var wanted []ledger.Version
	for _, v := range versions {
		if filter != nil && !filter(v.Channel) {
			continue
		}
		if s.accept != nil && !s.accept(v) {
			continue
		}
		wanted = append(wanted, v)
	}
	for start := 0; start < len(wanted); start += 2000 {
		end := start + 2000
		if end > len(wanted) {
			end = len(wanted)
		}
		batch := wanted[start:end]
		applied, err := s.rec.Apply(ctx, batch)
		if err != nil {
			return rep, err
		}
		rep.Novih += applied
		if applied > 0 && s.onApplied != nil {
			if err := s.onApplied(ctx, batch); err != nil {
				return rep, fmt.Errorf("površina nije osvježena: %w", err)
			}
		}
	}
	// sadržaj: uključeni se upišu (paket je namjerno donesen), ostali na popis
	if sp := s.spremisteSadrzaja(); sp != nil {
		for _, sd := range sadrzaji {
			if filter != nil && sd.Kanal != "" && !filter(sd.Kanal) {
				continue
			}
			if b, ok := bajtovi[sd.Otisak]; ok {
				if err := sp.UpisiProvjereno(ctx, sd.Otisak, sd.Vrsta, b, "cop:"+m.Izdao, sadrzaj.Veza{Kanal: sd.Kanal}); err == nil {
					rep.SadrzajaUpisano++
				}
				continue
			}
			if !sp.Ima(ctx, sd.Otisak) {
				if err := sp.Zeli(ctx, sd.Otisak, sd.Vrsta, sd.Bajtova, "cop", sd.Kanal); err == nil {
					rep.SadrzajaZeljeno++
				}
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO cop_primljena (kljuc, izdao, izdanje, otisak, primljeno) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(kljuc, izdao) DO UPDATE SET izdanje = excluded.izdanje, otisak = excluded.otisak, primljeno = excluded.primljeno`,
		kljuc, m.Izdao, m.Izdanje, m.Otisak, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return rep, err
	}
	return rep, nil
}

// CopIme je naziv datoteke paketa: gocop-dnevnici-bp16-2026_v3.cop
func CopIme(m CopManifest) string {
	k := strings.NewReplacer("/", "-").Replace(m.Obuhvat.Kljuc())
	return fmt.Sprintf("gocop-%s_v%d.cop", k, m.Izdanje)
}
