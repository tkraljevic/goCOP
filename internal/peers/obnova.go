package peers

// Rokovi i obnova članstva (G9a u planu WEB synca; Odluke (18. 10. 2026.) i
// (23. 10. 2026.)). Nijedan rok ne smije tiho raspasti mrežu:
//
//   - 30 dana prije isteka računalo samo traži obnovu, i to samo uz uspješnu
//     razmjenu, pa računalo koje se ne koristi samo istekne;
//   - zahtjev za obnovu je zapis u knjizi, potpisan ključem računala, i
//     putuje i preko posrednika do računala koje smije primati; ono izda
//     novo članstvo s istim imenom i ključem, ako članstvo nije opozvano;
//   - obnovu se može tražiti u svako doba (gumb Obnovi članstvo), a računalo
//     koje prima može obnoviti svakog člana odmah (gumb Obnovi);
//   - ovlast za primanje obnavlja samo računalo s ključem mreže, isto na
//     zahtjev, a računalo s ključem mreže samo sebi obnavlja članstvo;
//   - 30 dana nakon isteka članstvo još vrijedi za razmjenu (pričuva), ali
//     računalo u pričuvi ne prima i ne obnavlja druge (izdajClanstvo).

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"time"

	"gocop/internal/models"
	"gocop/internal/razmjena"
)

const (
	// PricuvniRok: toliko nakon isteka članstvo još vrijedi za razmjenu,
	// uz upozorenje na pločici
	PricuvniRok = 30 * 24 * time.Hour
	// RokUpozorenja: toliko prije isteka pločica upozorava, a računalo samo
	// traži obnovu
	RokUpozorenja = 30 * 24 * time.Hour
	// ValjanostOsnivaca je rok članstva koje računalo s ključem mreže
	// izdaje sebi
	ValjanostOsnivaca = 10 * MembershipValidity
	// EntityZahtjeviObnove je zahtjev za obnovu članstva; samo u knjizi,
	// bez tablice
	EntityZahtjeviObnove = "zahtjevi_obnove"
	// oznakaObnove je oznaka formata potpisanog zahtjeva; ostaje ista i uz
	// drugo ime protokola
	oznakaObnove = "obnova/1"
	// razmakObnove: računalo traži obnovu najviše jednom na dan, a članstvo
	// i ovlast obnavljaju se samo kad obnova rok produlji barem za dan
	razmakObnove = 24 * time.Hour
)

// Stanja roka članstva ili ovlasti
const (
	RokVrijedi = "vrijedi"
	RokIstjece = "istjece" // ističe za manje od 30 dana
	RokPricuva = "pricuva" // isteklo je, a razmjena još ide
	RokIsteklo = "isteklo" // pričuva je prošla (za ovlast: istekla je)
)

// Rok je rok članstva ili ovlasti za prikaz
type Rok struct {
	Do     time.Time `json:"do"`
	Stanje string    `json:"stanje"`
	ovlast bool      // rok ovlasti: bez pričuve, i „istekla”
}

// rokZa je stanje roka do u trenutku sada; članstvo ima i pričuvu, a
// ovlast je nema
func rokZa(do, sada time.Time, clanstvo bool) Rok {
	r := Rok{Do: do, Stanje: RokVrijedi, ovlast: !clanstvo}
	switch {
	case do.IsZero():
	case !sada.Before(do) && clanstvo && sada.Before(do.Add(PricuvniRok)):
		r.Stanje = RokPricuva
	case !sada.Before(do):
		r.Stanje = RokIsteklo
	case do.Sub(sada) < RokUpozorenja:
		r.Stanje = RokIstjece
	}
	return r
}

// TraziPaznju javlja ističe li rok uskoro ili je istekao
func (r Rok) TraziPaznju() bool { return r.Stanje != RokVrijedi }

// Oznaka je izgled roka na stranici: zeleno, žuto kad ističe, crveno kad
// je isteklo
func (r Rok) Oznaka() string {
	switch r.Stanje {
	case RokIstjece:
		return "badge-warning"
	case RokPricuva, RokIsteklo:
		return "badge-danger"
	}
	return "badge-active"
}

// datumHR je dan hrvatski, npr. 3. 10. 2026.
func datumHR(t time.Time) string { return t.In(models.Zagreb).Format("2. 1. 2006.") }

// danaDo je broj punih dana od sada do t, barem 1
func danaDo(t, sada time.Time) int { return max(1, int(t.Sub(sada).Hours()/24)) }

// Opis je rok riječima, za Postavke
func (r Rok) Opis() string {
	sada := time.Now()
	switch r.Stanje {
	case RokPricuva:
		return fmt.Sprintf("isteklo je %s; razmjena radi još do %s", datumHR(r.Do), datumHR(r.Do.Add(PricuvniRok)))
	case RokIsteklo:
		if r.ovlast {
			return "istekla je " + datumHR(r.Do)
		}
		return "isteklo je " + datumHR(r.Do)
	}
	if r.Do.IsZero() {
		return "bez roka"
	}
	d := danaDo(r.Do, sada)
	return fmt.Sprintf("vrijedi do %s (još %d %s)", datumHR(r.Do), d, models.UzBrojHR(d, "dan", "dana", "dana"))
}

// uPricuvi javlja je li greška provjere samo istek članstva m za koji još
// traje pričuva; tada članstvo vrijedi za razmjenu
func uPricuvi(m razmjena.Membership, err error, sada time.Time) bool {
	return errors.Is(err, razmjena.ErrExpired) && sada.Before(m.ExpiresAt.Add(PricuvniRok))
}

// ZahtjevObnove je zahtjev računala za obnovu članstva, a uz Ovlast i
// ovlasti za primanje. Potpisan je ključem računala, pa ga ne može
// podmetnuti drugi član.
type ZahtjevObnove struct {
	NodeID    string    `json:"nodeId"`
	PublicKey string    `json:"publicKey"`
	Network   string    `json:"network"`
	Kad       time.Time `json:"kad"`
	Ovlast    bool      `json:"ovlast,omitempty"`
	Potpis    string    `json:"potpis"`
}

// potpisaniZahtjev je ono što pokriva potpis zahtjeva za obnovu
type potpisaniZahtjev struct {
	V         string `json:"v"`
	NodeID    string `json:"nodeId"`
	PublicKey string `json:"publicKey"`
	Network   string `json:"network"`
	Kad       int64  `json:"kad"`
	Ovlast    bool   `json:"ovlast"`
}

func (z ZahtjevObnove) signedBytes() []byte {
	p := potpisaniZahtjev{oznakaObnove, z.NodeID, z.PublicKey, z.Network, z.Kad.Unix(), z.Ovlast}
	b, _ := json.Marshal(p) // polja su tekst, broj i istina, pa Marshal ne pada
	return b
}

// potpisan javlja je li zahtjev potpisao ključ iz njega
func (z ZahtjevObnove) potpisan() bool {
	pub, err := razmjena.ParsePublicKey(z.PublicKey)
	if err != nil {
		return false
	}
	potpis, err := base64.StdEncoding.DecodeString(z.Potpis)
	return err == nil && ed25519.Verify(pub, z.signedBytes(), potpis)
}

// ZatraziObnovu je gumb Obnovi članstvo: računalo s ključem mreže odmah
// obnovi sebi članstvo, a drugo upiše zahtjev koji ide uz razmjenu do
// računala koje prima. Vraća novo članstvo kad je obnovljeno odmah.
func (s *Service) ZatraziObnovu(ctx context.Context) (*razmjena.Membership, error) {
	n := s.NetworkInfo()
	if n == nil {
		return nil, fmt.Errorf("ovo računalo nije ni u jednoj mreži")
	}
	if n.DrziKljuc {
		m, err := s.obnoviSebe(ctx)
		return &m, err
	}
	z := ZahtjevObnove{NodeID: s.node.ID, PublicKey: s.node.PublicKey(), Network: n.PublicKey,
		Kad: time.Now().UTC().Truncate(time.Second), Ovlast: s.ovlastZaObnovu(ctx, s.node.ID, s.node.PublicKey()) != nil}
	z.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(s.node.key, z.signedBytes()))
	return nil, s.uTransakciji(ctx, func(tx *sql.Tx) error {
		_, err := s.rec.Record(ctx, tx, EntityZahtjeviObnove, s.node.ID, z)
		return err
	})
}

// obnoviSebe: računalo s ključem mreže izda sebi novo članstvo
func (s *Service) obnoviSebe(ctx context.Context) (razmjena.Membership, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return razmjena.Membership{}, fmt.Errorf("ovo računalo nije ni u jednoj mreži")
	}
	m, err := network.Admit(s.node.ID, s.node.key.Public().(ed25519.PublicKey), s.node.ID, ValjanostOsnivaca)
	if err != nil {
		return m, fmt.Errorf("samo računalo s ključem mreže obnavlja sebi članstvo")
	}
	return m, s.saveMembership(ctx, m)
}

// ObnoviClana je gumb Obnovi uz člana na računalu koje prima: novo članstvo
// s istim imenom i ključem, ako članstvo još vrijedi ili je u pričuvi
func (s *Service) ObnoviClana(ctx context.Context, nodeID string) (razmjena.Membership, error) {
	if nodeID == s.node.ID {
		if n := s.NetworkInfo(); n != nil && n.DrziKljuc {
			return s.obnoviSebe(ctx)
		}
	}
	m, err := s.izabranoClanstvo(ctx, nodeID)
	if err != nil {
		return razmjena.Membership{}, fmt.Errorf("članstvo računala %s ne vrijedi ni u pričuvi (isteklo je prije više od 30 dana ili je opozvano), pa ga treba ponovno primiti u mrežu", nodeID)
	}
	return s.obnovi(ctx, *m)
}

// errNeProduzuje: novo članstvo ne bi produljilo rok barem za dan (obnovljeno
// je nedavno, ili ovlast ovog računala ističe prije)
var errNeProduzuje = errors.New("obnova ne bi produljila članstvo")

// obnovi izda novo članstvo s imenom i ključem iz m i upiše ga, kad
// produljuje rok barem za dan
func (s *Service) obnovi(ctx context.Context, m razmjena.Membership) (razmjena.Membership, error) {
	kljuc, _ := razmjena.ParsePublicKey(m.DeviceKey) // m je izabrano, pa se ključ da pročitati
	novo, err := s.izdajClanstvo(ctx, m.DeviceID, kljuc)
	if err != nil {
		return razmjena.Membership{}, err
	}
	if !novo.ExpiresAt.After(m.ExpiresAt.Add(razmakObnove)) {
		return razmjena.Membership{}, fmt.Errorf("%w: članstvo računala %s vrijedi do %s, a novo bi vrijedilo do %s", errNeProduzuje, m.DeviceID, datumHR(m.ExpiresAt), datumHR(novo.ExpiresAt))
	}
	return novo, s.saveMembership(ctx, novo)
}

// ovlastZaObnovu je ovlast računala nodeID s ključem kljuc koju potpisuje
// ključ mreže i koja nije opozvana, i kad je istekla; inače nil
func (s *Service) ovlastZaObnovu(ctx context.Context, nodeID, kljuc string) *razmjena.Ovlast {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	o, err := s.getOvlast(ctx, nodeID)
	if network == nil || err != nil || o == nil || o.DeviceKey != kljuc {
		return nil
	}
	// provjera u trenutku izdavanja: potpis, bez roka
	if o.Verify(network.Public, o.IssuedAt) != nil || s.opozvano(ctx, OpozivOvlasti, o.DeviceKey, o.IssuedAt, network.Public) {
		return nil
	}
	return o
}

// zahtjeviObnove je zadnji potpisan zahtjev za obnovu u našoj mreži po
// računalu, iz knjige. Red tablice nema: zahtjev bi mogao prepisati bilo
// koji član, a u knjizi ostaju svi.
func (s *Service) zahtjeviObnove(ctx context.Context, mreza string) (map[string]ZahtjevObnove, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM record_versions WHERE entity = ? AND archived = 0 ORDER BY version_id`, EntityZahtjeviObnove)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ZahtjevObnove{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var z ZahtjevObnove
		if json.Unmarshal([]byte(payload), &z) == nil && z.Network == mreza && z.potpisan() {
			out[z.NodeID] = z
		}
	}
	return out, rows.Err()
}

// obnovaUzRazmjenu ide nakon svake uspješne razmjene: računalo kojem rok
// ističe zatraži obnovu, a računalo koje prima obradi zahtjeve koji su
// stigli. Dvije razmjene odjednom ne obrađuju iste zahtjeve.
func (s *Service) obnovaUzRazmjenu(ctx context.Context, o syncOutcome) {
	if o.err != nil || !s.obnovaTece.TryLock() {
		return
	}
	defer s.obnovaTece.Unlock()
	s.mozdaZatraziObnovu(ctx)
	s.obradiZahtjeveObnove(ctx)
}

// mozdaZatraziObnovu: kad vlastitom članstvu ili ovlasti ostane manje od
// 30 dana ili je članstvo u pričuvi, računalo s ključem mreže obnovi sebe,
// a drugo zatraži obnovu, najviše jednom na dan
func (s *Service) mozdaZatraziObnovu(ctx context.Context) {
	n := s.NetworkInfo()
	if n == nil || !s.trebaObnova(ctx, n.DrziKljuc) || s.nedavnoZatrazeno(ctx, n) {
		return
	}
	if _, err := s.ZatraziObnovu(ctx); err != nil {
		log.Printf("razmjena: obnova članstva nije zatražena: %v", err)
	}
}

// trebaObnova: vlastitom članstvu ostaje manje od 30 dana ili je u
// pričuvi, ili, bez ključa mreže, vlastitoj ovlasti ostaje manje od 30 dana
func (s *Service) trebaObnova(ctx context.Context, kljucMreze bool) bool {
	sada := time.Now()
	if m, err := s.izabranoClanstvo(ctx, s.node.ID); err == nil && rokZa(m.ExpiresAt, sada, true).TraziPaznju() {
		return true
	}
	o := s.ovlastZaObnovu(ctx, s.node.ID, s.node.PublicKey())
	return !kljucMreze && o != nil && rokZa(o.ExpiresAt, sada, false).TraziPaznju()
}

// nedavnoZatrazeno: računalo bez ključa mreže već je tražilo obnovu u
// zadnjih dan, ili se njegovi zahtjevi ne daju pročitati
func (s *Service) nedavnoZatrazeno(ctx context.Context, n *Network) bool {
	if n.DrziKljuc {
		return false
	}
	zahtjevi, err := s.zahtjeviObnove(ctx, n.PublicKey)
	z, ima := zahtjevi[s.node.ID]
	return err != nil || (ima && time.Since(z.Kad) < razmakObnove)
}

// obradiZahtjeveObnove: računalo koje smije primati obnovi članstvo svakom
// računalu čiji je zahtjev stigao, a računalo s ključem mreže i ovlast
func (s *Service) obradiZahtjeveObnove(ctx context.Context) {
	n := s.NetworkInfo()
	if n == nil || !n.CanAdmit {
		return
	}
	zahtjevi, err := s.zahtjeviObnove(ctx, n.PublicKey)
	if err != nil {
		log.Printf("razmjena: zahtjevi za obnovu se ne daju pročitati: %v", err)
		return
	}
	for _, id := range slices.Sorted(maps.Keys(zahtjevi)) {
		if z := zahtjevi[id]; id != s.node.ID && !s.obradjen(ctx, z) {
			s.obnoviNaZahtjev(ctx, z, n.DrziKljuc)
		}
	}
}

// obnoviNaZahtjev obnovi članstvo (i ovlast, uz ključ mreže) računala iz
// zahtjeva i zapamti da je zahtjev obrađen. Opozvanom ili isteklom članu,
// ili zahtjevu s drugim ključem, se ne obnavlja, a zahtjev je time obrađen.
// Kad obnova ne uspije, greška ide u dnevnik jednom, a zahtjev se obrađuje
// iznova uz sljedeću razmjenu.
func (s *Service) obnoviNaZahtjev(ctx context.Context, z ZahtjevObnove, kljucMreze bool) {
	m, err := s.izabranoClanstvo(ctx, z.NodeID)
	if err != nil && !errors.Is(err, errNijeClan) {
		s.javiObnovu(z, "članstva", err)
		return
	}
	if err == nil && m.DeviceKey == z.PublicKey && !s.obnoviIzZahtjeva(ctx, z, *m, kljucMreze) {
		return
	}
	s.zabiljeziObradu(ctx, z)
}

// obnoviIzZahtjeva obnovi članstvo m i, uz ključ mreže i kad zahtjev to
// traži, ovlast; javlja je li prošlo (i kad obnova roka ne bi produljila)
func (s *Service) obnoviIzZahtjeva(ctx context.Context, z ZahtjevObnove, m razmjena.Membership, kljucMreze bool) bool {
	if _, err := s.obnovi(ctx, m); neuspjelaObnova(err) {
		s.javiObnovu(z, "članstva", err)
		return false
	}
	o := s.ovlastZaObnovu(ctx, z.NodeID, z.PublicKey)
	if !z.Ovlast || !kljucMreze || o == nil {
		return true
	}
	if err := s.obnoviOvlast(ctx, *o); neuspjelaObnova(err) {
		s.javiObnovu(z, "ovlasti za primanje", err)
		return false
	}
	return true
}

// neuspjelaObnova: greška obnove, osim obnove koja rok ne bi produljila
func neuspjelaObnova(err error) bool { return err != nil && !errors.Is(err, errNeProduzuje) }

// obnoviOvlast izda novu ovlast za primanje kad produljuje rok barem za dan
func (s *Service) obnoviOvlast(ctx context.Context, o razmjena.Ovlast) error {
	if !time.Now().Add(OvlastValidity).After(o.ExpiresAt.Add(razmakObnove)) {
		return errNeProduzuje
	}
	_, err := s.IzdajOvlast(ctx, o.DeviceID)
	return err
}

// obradjen javlja je li ovo računalo već obradilo baš taj zahtjev (zadnji
// obrađeni zahtjev računala pamti se lokalno, ne putuje)
func (s *Service) obradjen(ctx context.Context, z ZahtjevObnove) bool {
	var potpis string
	err := s.db.QueryRowContext(ctx, `SELECT potpis FROM obnova_obradjena WHERE node_id = ?`, z.NodeID).Scan(&potpis)
	return err == nil && potpis == z.Potpis
}

// zabiljeziObradu pamti da je zahtjev obrađen; kad ne uspije, zahtjev se
// obradi iznova, a obnova koja ne produljuje rok ništa ne upisuje
func (s *Service) zabiljeziObradu(ctx context.Context, z ZahtjevObnove) {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO obnova_obradjena (node_id, potpis, kad) VALUES (?, ?, ?)
		ON CONFLICT (node_id) DO UPDATE SET potpis = excluded.potpis, kad = excluded.kad`, z.NodeID, z.Potpis, time.Now().UTC()); err != nil {
		log.Printf("razmjena: obrađeni zahtjev za obnovu nije zapamćen: %v", err)
	}
}

// javiObnovu zapisuje u dnevnik, jednom po zahtjevu i onome što se
// obnavlja (u genitivu: članstva, ovlasti za primanje), da obnova nije
// uspjela
func (s *Service) javiObnovu(z ZahtjevObnove, cega string, err error) {
	if _, vec := s.javljenaObnova.LoadOrStore(z.Potpis+"\x00"+cega, true); !vec {
		log.Printf("razmjena: obnova %s računala %s na zahtjev od %s nije uspjela: %v", cega, z.NodeID, datumHR(z.Kad), err)
	}
}
