package ledger

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// NajviseUnaprijed je koliko vrijeme u oznaci primljene verzije smije biti
// ispred sata ovog računala. Pet minuta je granica koju za razliku satova
// traži i prijava na domenu (Kerberos), pa računala u uredu nikad nisu toliko
// razmaknuta, a prijenosnik koji je tjednima bio izvan mreže obično nije
// više od minute ili dvije. Verzija s vremenom iza granice ne upisuje se, a
// pošiljatelj je šalje dok joj vrijeme ne dođe, jer se granica primatelja za
// nju nije pomaknula.
const NajviseUnaprijed = 5 * time.Minute

// Zašto se primljena verzija ne upisuje (ProvjeriPrimljenu). Tekst se
// čita iza riječi „verzija”, npr. „verzija … nema oznaku …”.
var (
	ErrBezOznake         = errors.New("nema oznaku, entitet ili identifikator zapisa")
	ErrOblikOznake       = errors.New("ima oznaku koja nije ispravnog oblika")
	ErrIzBuducnosti      = errors.New("ima vrijeme u oznaci predaleko ispred sata ovog računala")
	ErrVlastitaUnaprijed = errors.New("nosi ime ovog računala, a vrijeme u oznaci je ispred njegova sata")
	ErrOblikAutora       = errors.New("ima znak | u imenu autora ili kanala")
)

// IspravnaOznaka javlja je li oznaka verzije u obliku koji upisuje ovaj
// program (oznakaVerzije): UUIDv7 u kanonskom obliku, 36 znakova, mala
// slova, crtice na svojim mjestima, inačica 7 i varijanta RFC 9562. Na
// tom obliku počiva poredak verzija, pa i granica razmjene.
func IspravnaOznaka(id string) bool {
	if len(id) != 36 {
		return false
	}
	u, err := uuid.Parse(id)
	return err == nil && u.Version() == 7 && u.Variant() == uuid.RFC4122 && u.String() == id
}

// Neispravnost je razlog zbog kojeg knjiga verziju ne prima (Ispravna),
// ili nil. Znak | dijeli autora od kanala u ključu granice (FrontierKey),
// pa ga ni ime autora ni kanal ne smiju imati: imena računala i kanali koje
// program daje ga nemaju.
func Neispravnost(v Version) error {
	switch {
	case v.VersionID == "" || v.Entity == "" || v.EntityID == "":
		return ErrBezOznake
	case !IspravnaOznaka(v.VersionID):
		return ErrOblikOznake
	case strings.ContainsRune(v.NodeID, '|') || strings.ContainsRune(v.Channel, '|'):
		return ErrOblikAutora
	}
	return nil
}

// VrijemeOznake je trenutak nastanka zapisan u oznaci verzije (prvih 48
// bita, milisekunde od 1. 1. 1970. UTC); false kad oznaka nije ispravna
func VrijemeOznake(id string) (time.Time, bool) {
	if !IspravnaOznaka(id) {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(msOznake(uuid.MustParse(id)))).UTC(), true
}

// msOznake su milisekunde iz prvih šest bajtova oznake
func msOznake(u uuid.UUID) uint64 {
	var ms [8]byte
	copy(ms[2:], u[:6])
	return binary.BigEndian.Uint64(ms[:])
}

// zadnjaMs je vrijeme zadnje oznake koju je knjiga dala novoj verziji s
// današnjim vremenom (oznakaVerzije). Generator oznaka ne ide unatrag ni
// kad sat računala skoči unatrag, pa ni Sada ne ide.
var zadnjaMs atomic.Int64

// zapamtiOznaku pamti vrijeme nove oznake za Sada
func zapamtiOznaku(id uuid.UUID, err error) {
	if err != nil {
		return
	}
	ms := int64(msOznake(id))
	for {
		bilo := zadnjaMs.Load()
		if ms <= bilo || zadnjaMs.CompareAndSwap(bilo, ms) {
			return
		}
	}
}

// Sada je sat po kojem se provjerava vrijeme u primljenoj oznaci: sat
// računala, ili vrijeme zadnje oznake koju je ova knjiga dala, ako je ono
// kasnije. Primljena verzija tako nikad nije „iz budućnosti” u odnosu na
// verzije koje je ovo računalo samo upravo napisalo.
func Sada() time.Time {
	sada := time.Now()
	if zadnja := time.UnixMilli(zadnjaMs.Load()); zadnja.After(sada) {
		return zadnja
	}
	return sada
}

// ProvjeriPrimljenu javlja smije li verzija koja je stigla s drugog
// računala (razmjenom, arhivom ili COP paketom) u knjigu ovog računala:
// nil kad smije, inače razlog. Uz Ispravna traži i da vrijeme u oznaci nije
// više od NajviseUnaprijed ispred sata. Verzija s imenom ovog računala
// (ovajCvor) ne smije biti ni malo ispred sata, jer ju je napisalo samo ovo
// računalo, svojim satom. Starija takva verzija prolazi: tako se računalo
// koje je izgubilo bazu vrati na svoje zapise iz mreže ili iz arhive.
func ProvjeriPrimljenu(v Version, ovajCvor string, sada time.Time) error {
	if err := Neispravnost(v); err != nil {
		return err
	}
	t, _ := VrijemeOznake(v.VersionID)
	switch {
	case ovajCvor != "" && v.NodeID == ovajCvor && t.After(sada):
		return ErrVlastitaUnaprijed
	case t.After(sada.Add(NajviseUnaprijed)):
		return ErrIzBuducnosti
	}
	return nil
}

// globOznake je uzorak za GLOB koji propušta točno oznake u obliku
// IspravnaOznaka: mala heksadekadska slova, crtice na mjestu, inačica 7 i
// varijanta RFC 9562 (8, 9, a ili b)
var globOznake = func() string {
	h := "[0-9a-f]"
	return strings.Repeat(h, 8) + "-" + strings.Repeat(h, 4) + "-7" + strings.Repeat(h, 3) +
		"-[89ab]" + strings.Repeat(h, 3) + "-" + strings.Repeat(h, 12)
}()

// NeispravneUKnjizi su verzije u knjizi ovog računala koje razmjena danas
// ne bi primila: oznaka nije ispravnog oblika, vrijeme u njoj je više od
// NajviseUnaprijed ispred sada, ili ime autora ili kanal imaju znak |. Takve su mogle ući prije nego što je
// razmjena provjeravala oznaku. Vraća broj i do najvise primjera, redom
// oznake. Knjigu ne mijenja.
func (r *Recorder) NeispravneUKnjizi(ctx context.Context, sada time.Time, najvise int) (int, []Version, error) {
	granica := oznakaZa(sada.Add(NajviseUnaprijed))
	rows, err := r.db.QueryContext(ctx, `SELECT `+columns+` FROM record_versions
		WHERE version_id NOT GLOB ? OR version_id > ? OR instr(node_id, '|') > 0 OR instr(channel, '|') > 0
		ORDER BY version_id`, globOznake, granica)
	if err != nil {
		return 0, nil, fmt.Errorf("greška pri čitanju verzija: %w", err)
	}
	defer rows.Close()
	var broj int
	var primjeri []Version
	for rows.Next() {
		v, err := procitajVerziju(rows)
		if err != nil {
			return 0, nil, err
		}
		if ProvjeriPrimljenu(v, "", sada) == nil {
			continue
		}
		broj++
		if len(primjeri) < najvise {
			primjeri = append(primjeri, v)
		}
	}
	return broj, primjeri, rows.Err()
}

// oznakaZa je najveća oznaka s vremenom t: sve oznake s vremenom do t su
// leksikografski do nje
func oznakaZa(t time.Time) string {
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(max(t.UnixMilli(), 0)))
	return fmt.Sprintf("%x-%x-7fff-bfff-ffffffffffff", ms[2:6], ms[6:8])
}

// uvjetGranice je uvjet za verzije od kojih se slaže granica (Frontier), s
// argumentima: oznaka ispravnog oblika, vrijeme najviše NajviseUnaprijed
// ispred sada, autor i kanal bez znaka |. To su verzije koje bi razmjena
// danas primila (ProvjeriPrimljenu bez vlastitog imena). Verzija koja je u
// knjigu ušla prije te provjere tako ne pomiče granicu: ostaje u knjizi
// (knjiga se ne prepisuje), ali drugi čvorovi i dalje šalju ono što autor
// napiše poslije nje. Svi stupci su u indeksu idx_versions_channel.
func uvjetGranice(sada time.Time) (string, []any) {
	oblik, najvisa := OznakeGranice(sada)
	return `version_id GLOB ? AND version_id <= ? AND instr(node_id, '|') = 0 AND instr(channel, '|') = 0`,
		[]any{oblik, najvisa}
}

// najveceUGranici je podupit s najvećom verzijom po autoru i kanalu među
// verzijama granice (uvjetGranice), s argumentima. Sažimanje i
// prorjeđivanje čuvaju i nju, uz najveću verziju ključa: bez nje bi granica
// pala, a druga strana bi tu verziju slala natrag svakom razmjenom.
func najveceUGranici() (string, []any) {
	uvjet, args := uvjetGranice(Sada())
	return `(SELECT MAX(version_id) FROM record_versions WHERE ` + uvjet + ` GROUP BY node_id, channel)`, args
}

// OznakeGranice su uzorak za GLOB i najveća oznaka za upit koji, kao
// granica (uvjetGranice), uzima samo oznake ispravnog oblika s vremenom
// najviše NajviseUnaprijed ispred sada:
// version_id GLOB oblik AND version_id <= najvisa
func OznakeGranice(sada time.Time) (oblik, najvisa string) {
	return globOznake, oznakaZa(sada.Add(NajviseUnaprijed))
}
