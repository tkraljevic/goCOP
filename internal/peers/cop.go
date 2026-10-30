package peers

// .cop inačica 3 i 4: potpisano izdanje jednog ili više kanala knjige
// verzija, s pripadajućim sadržajem po otisku. Isti oblik koji mreža
// prenosi među čvorovima, samo u datoteci: za USB, e-poštu ili računalo bez
// stalne veze. Inačica 4 nosi i rupe u nizu (rupe_uvoz.go), pa je stariji
// program ne uvozi.
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
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/sadrzaj"
)

// CopInacica je najveća inačica oblika paketa kanala koju ovaj program
// čita. Paket bez rupa izlazi kao inačica 3, a s rupama kao 4 (inacicaCopa).
const CopInacica = 4

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
	// Rupe su, po ključu, dio niza za koji izdavač ne jamči da ga ima
	// cijelog (rupe_uvoz.go); samo u inačici 4 i u potpisu (redRupa)
	Rupe map[string]raspon `json:"rupe,omitempty"`
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
	b.WriteString(redRupa(m.Rupe))
	return []byte(b.String())
}

// otisakIzdanja veže zapise i popis sadržaja, ne i bajtove ni vrijeme
func otisakIzdanja(zapisi []byte, sadrzaji []CopSadrzaj) string {
	h := sha256.New()
	h.Write(zapisi)
	return hex.EncodeToString(otisakPopisa(h, sadrzaji))
}

// otisakPopisa dodaje u h, koji već ima zapise, popis sadržaja po otisku i
// vraća otisak izdanja.
func otisakPopisa(h hash.Hash, sadrzaji []CopSadrzaj) []byte {
	poredani := append([]CopSadrzaj(nil), sadrzaji...)
	sort.Slice(poredani, func(i, j int) bool { return poredani[i].Otisak < poredani[j].Otisak })
	for _, s := range poredani {
		fmt.Fprintf(h, "%s %s %d\n", s.Otisak, s.Vrsta, s.Bajtova)
	}
	return h.Sum(nil)
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
	rupe := s.pocetakRupa(ctx)
	versions, err := s.rec.InChannels(ctx, channels)
	if err != nil {
		return m, err
	}
	if len(versions) == 0 {
		return m, fmt.Errorf("nema nijedne verzije u zadanim kanalima")
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionID < versions[j].VersionID })
	zapisi, versions, neispravne := zapisiZaCop(versions)
	s.zabiljeziNeispravne(neispravne)
	if len(versions) == 0 {
		return m, fmt.Errorf("Paket nije izdan: za taj odabir nijedan zapis na ovom računalu nije ispravan. Pojedinosti su u dnevniku programa.")
	}
	// sadržaji koje zapisi navode: otisak iz zapisa izvornika
	sadrzaji := sadrzajiPaketa(versions)
	m.Zapisa, m.Sadrzaja = len(versions), len(sadrzaji)
	m.Rupe = rupe.zaIzvoz(s.rupeMijenja.Load(), versions)
	m.Inacica = inacicaCopa(m.Rupe)
	m.Otisak = otisakIzdanja(zapisi, sadrzaji)

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
	if err := dodaj("zapisi.jsonl", zapisi); err != nil {
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

// zapisiZaCop slaže zapisi.jsonl, redak po verziji, od ispravnih verzija, i
// vraća ih s popisom neispravnih. Neispravna verzija (zaSlanje, pokvaren
// redak u knjizi) ne ide u paket, kao ni u razmjenu (ogradiDeltu): do 0.0.38
// jedna takva verzija rušila je cijeli izvoz. Redak je isti kao iz
// json.Encoder, pa ispravne verzije daju isti otisak izdanja kao dosad.
func zapisiZaCop(versions []ledger.Version) (zapisi []byte, ispravne, neispravne []ledger.Version) {
	var b bytes.Buffer
	for _, v := range versions {
		redak, err := zaSlanje(v)
		if err != nil {
			neispravne = append(neispravne, v)
			continue
		}
		b.Write(redak)
		b.WriteByte('\n')
		ispravne = append(ispravne, v)
	}
	return b.Bytes(), ispravne, neispravne
}

// sadrzajiPaketa skuplja otiske sadržaja koje zapisi izvornika navode,
// svaki jednom, s vrstom, veličinom i kanalom iz zapisa
func sadrzajiPaketa(versions []ledger.Version) []CopSadrzaj {
	vidjeno := map[string]bool{}
	var out []CopSadrzaj
	for _, v := range versions {
		sd, ok := sadrzajVerzije(v)
		if !ok || vidjeno[sd.Otisak] {
			continue
		}
		vidjeno[sd.Otisak] = true
		out = append(out, sd)
	}
	return out
}

// sadrzajiPoOtisku je sadrzajiPaketa po otisku.
func sadrzajiPoOtisku(versions []ledger.Version) map[string]CopSadrzaj {
	out := map[string]CopSadrzaj{}
	for _, sd := range sadrzajiPaketa(versions) {
		out[sd.Otisak] = sd
	}
	return out
}

// sadrzajVerzije je sadržaj koji navodi živi zapis izvornika.
func sadrzajVerzije(v ledger.Version) (CopSadrzaj, bool) {
	if !strings.HasSuffix(v.Entity, "_izvornici") || v.Archived {
		return CopSadrzaj{}, false
	}
	var iz struct {
		Otisak  string `json:"otisak"`
		Bajtova int    `json:"bajtova"`
		Vrsta   string `json:"vrsta"`
	}
	if json.Unmarshal(v.Payload, &iz) != nil || !jeOtisak(iz.Otisak) {
		return CopSadrzaj{}, false
	}
	if iz.Vrsta == "" {
		iz.Vrsta = "application/pdf"
	}
	return CopSadrzaj{Otisak: iz.Otisak, Vrsta: iz.Vrsta, Bajtova: iz.Bajtova, Kanal: v.Channel}, true
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

// citajManifestCopa čita manifest paketa kanala; paket karte ima svoje
// mjesto (Administracija › Karte)
func citajManifestCopa(mb []byte, m *CopManifest) error {
	if err := json.Unmarshal(mb, m); err != nil {
		return fmt.Errorf("manifest nije čitljiv: %w", err)
	}
	var vrsta struct{ Vrsta string }
	if json.Unmarshal(mb, &vrsta) == nil && vrsta.Vrsta == VrstaPaketaKarte {
		return fmt.Errorf("ovo je paket karte; učitava se u Administraciji › Karte")
	}
	return nil
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
	// Manifest se čita prvi i sam: potpis i izdavač provjeravaju se prije
	// nego što se raspakira ostatak paketa.
	p := &copPaket{}
	st, err := f.Stat()
	if err == nil {
		p, err = otvoriCop(f, st.Size())
	}
	rep.Manifest = p.m
	if err != nil {
		return rep, err
	}
	m := p.m
	javni, err := ProvjeriCopPotpis(m)
	if err != nil {
		return rep, err
	}
	rep.PotpisValjan = true
	rep.IzdavacClan, err = s.izdavacClan(ctx, m.Izdao, javni)
	if err != nil {
		return rep, err
	}
	if !rep.IzdavacClan {
		rep.Napomena = "čvor još nije u mreži, pa se članstvo izdavača nije moglo provjeriti"
	}
	versions, sadrzaji, err := p.zapisi()
	rep.Verzija = len(versions)
	if err == nil {
		err = p.provjeriSadrzaje(sadrzaji)
	}
	if err != nil {
		return rep, err
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
	ispravne := s.preskociNeispravne("uvoz", versions)
	wanted, odluke := s.zaUvoz(ispravne, filter)
	rep.Novih, err = s.uveziSRupama(ctx, wanted, s.promjenaUvoza(ctx, wanted, odluke, rupeIzPaketa(m, ispravne)))
	if err != nil {
		return rep, err
	}
	s.sadrzajiCopa(ctx, p, sadrzaji, wanted, &rep)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO cop_primljena (kljuc, izdao, izdanje, otisak, primljeno) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(kljuc, izdao) DO UPDATE SET izdanje = excluded.izdanje, otisak = excluded.otisak, primljeno = excluded.primljeno`,
		kljuc, m.Izdao, m.Izdanje, m.Otisak, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return rep, err
	}
	return rep, nil
}

// sadrzajiCopa upisuje uključene sadržaje paketa (paket je namjerno donesen),
// jedan po jedan, a ostale stavlja na popis želja. Uzima samo sadržaje koje
// navode primljeni zapisi (wanted), s vrstom i kanalom iz zapisa, ne s popisa.
func (s *Service) sadrzajiCopa(ctx context.Context, p *copPaket, sadrzaji []CopSadrzaj, wanted []ledger.Version, rep *CopIzvjestaj) {
	sp := s.spremisteSadrzaja()
	if sp == nil {
		return
	}
	navedeni := sadrzajiPoOtisku(wanted)
	for _, sd := range sadrzaji {
		zapis, ima := navedeni[sd.Otisak]
		switch {
		case !ima:
		case sd.Ukljucen:
			rep.SadrzajaUpisano += upisiSadrzajCopa(ctx, sp, p, zapis, rep.Manifest.Izdao)
		case !sp.Ima(ctx, sd.Otisak) && sp.Zeli(ctx, zapis.Otisak, zapis.Vrsta, zapis.Bajtova, "cop", zapis.Kanal) == nil:
			rep.SadrzajaZeljeno++
		}
	}
}

// upisiSadrzajCopa upisuje jedan uključeni sadržaj i vraća 1 kad je upisan.
// sd je sadržaj kako ga navodi zapis. Kanal ide kroz želju; veze sa zapisom
// upisuje primjena zapisa, pa sadržaj koji nijedan zapis ne drži ostaje
// siroče. Što se ne da upisati, ostaje među željama.
func upisiSadrzajCopa(ctx context.Context, sp *sadrzaj.Spremiste, p *copPaket, sd CopSadrzaj, izdao string) int {
	if sp.Zeli(ctx, sd.Otisak, sd.Vrsta, sd.Bajtova, "cop", sd.Kanal) != nil {
		return 0
	}
	b, err := p.procitajSadrzaj(sd.Otisak)
	if err != nil || sp.UpisiProvjereno(ctx, sd.Otisak, sd.Vrsta, b, "cop:"+izdao) != nil {
		return 0
	}
	return 1
}

// izdavacClan javlja je li paket potpisao član naše mreže koji je u
// manifestu naveden kao izdavač. Članstvo se traži po imenu izdavača, a ne
// samo po ključu: član koji potpiše paket pod tuđim imenom i s visokim
// izdanjem inače bi zauzeo mjesto tog čvora, pa bi se njegovi pravi paketi
// odbijali kao starija izdanja. Dok čvor nema mrežu, članstvo se ne može
// provjeriti; tada vraća false bez greške.
func (s *Service) izdavacClan(ctx context.Context, izdao string, javni ed25519.PublicKey) (bool, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return false, nil
	}
	if !s.trusted(javni, nil) {
		return false, fmt.Errorf("paket je potpisao čvor %s koji nije član naše mreže", izdao)
	}
	m, err := scanMembership(s.db.QueryRowContext(ctx, `SELECT `+stupciClanstva+` FROM memberships WHERE node_id = ?`, izdao))
	if err != nil || s.provjeriClanstvo(ctx, network.Public, m, javni) != nil {
		return false, fmt.Errorf("paket kaže da ga je izdao čvor %s, a potpisao ga je drugi član mreže, pa se ne ugrađuje", izdao)
	}
	return true, nil
}

// Potpisnik kaže što se zna o ključu kojim je paket potpisan
type Potpisnik int

const (
	// PotpisnikNeprovjeren: računalo još nije u mreži, pa ne zna čiji je ključ
	PotpisnikNeprovjeren Potpisnik = iota
	// PotpisnikClan: član mreže, pod svojim imenom
	PotpisnikClan
	// PotpisnikOvoRacunalo: ključ ovog računala
	PotpisnikOvoRacunalo
)

// PotpisnikPaketa javlja tko je potpisao paket koji kaže da ga je izdao
// čvor izdao: ovo računalo, član naše mreže ili, dok računalo još nije u
// mreži, nepoznat ključ. Pravilo je isto kao za .cop paket kanala: ključ
// izvan mreže i član koji potpisuje pod tuđim imenom su greška.
func (s *Service) PotpisnikPaketa(ctx context.Context, izdao string, javni ed25519.PublicKey) (Potpisnik, error) {
	if len(s.node.key) > 0 && bytes.Equal(javni, s.node.key.Public().(ed25519.PublicKey)) {
		return PotpisnikOvoRacunalo, nil
	}
	clan, err := s.izdavacClan(ctx, izdao, javni)
	if err != nil || !clan {
		return PotpisnikNeprovjeren, err
	}
	return PotpisnikClan, nil
}

// CopIme je naziv datoteke paketa: gocop-dnevnici-bp16-2026_v3.cop
func CopIme(m CopManifest) string {
	k := strings.NewReplacer("/", "-").Replace(m.Obuhvat.Kljuc())
	return fmt.Sprintf("gocop-%s_v%d.cop", k, m.Izdanje)
}
