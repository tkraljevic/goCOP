package peers

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gocop/internal/razmjena"
)

// Mreža je skupina čvorova koji si vjeruju, i ima vlastiti ključ. Članstvo
// je potpis mrežnog ključa nad ključem čvora; provjerava se na vratima
// svake razmjene. Dvije organizacije s istim programom imaju dva mrežna
// ključa i time ništa zajedničko: potvrda jedne ne vrijedi kod druge.
//
// Uparivanje (šest znamenki) i dalje dokazuje KOJI je čvor na drugoj
// strani; potvrda dokazuje da je JEDAN OD NAŠIH. Čvor uparen bez važeće
// potvrde je poznat, ali ne i pouzdan — razmjena mu se odbija dok ga
// nositelj mrežnog ključa ne primi.

// NetworkKeyFileName je privatni ključ mreže — postoji samo na čvorovima
// čiji vlasnici smiju primati članove. Nikad u bazi, nikad se ne sinkronizira.
const NetworkKeyFileName = "network-key"

// MembershipValidity je rok potvrde; obnavlja se ponovnim primanjem
const MembershipValidity = 365 * 24 * time.Hour

// EntityMemberships je naziv entiteta u knjizi verzija
const EntityMemberships = "memberships"

// Network je mreža kojoj čvor pripada, ako pripada
type Network struct {
	Name      string           `json:"name"`
	PublicKey string           `json:"public_key"`
	JoinedAt  time.Time        `json:"joined_at"`
	CanAdmit  bool             `json:"can_admit"`        // ovaj čvor smije primati članove
	DrziKljuc bool             `json:"drzi_kljuc"`       // ovaj čvor drži privatni ključ mreže
	Ovlast    *razmjena.Ovlast `json:"ovlast,omitempty"` // ovlast ovog čvora za primanje, kad je ima
}

// welcomePack je ono što se preda pri uparivanju: tko smo (mreža), moja
// potvrda (da me drugi može provjeriti) i, kad je mogu izdati, potvrda za
// drugoga — pa je član onog trena kad oba čovjeka potvrde kod.
type welcomePack struct {
	NetworkName string               `json:"network_name,omitempty"`
	NetworkKey  string               `json:"network_key,omitempty"`
	Mine        *razmjena.Membership `json:"mine,omitempty"`
	ForYou      *razmjena.Membership `json:"for_you,omitempty"`
}

// loadNetwork čita mrežu iz baze i, ako postoji, privatni ključ uz bazu
func (s *Service) loadNetwork() error {
	var name, pub string
	var joined time.Time
	err := s.db.QueryRow(`SELECT name, public_key, joined_at FROM network WHERE id = 1`).Scan(&name, &pub, &joined)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	pubKey, err := razmjena.ParsePublicKey(pub)
	if err != nil {
		return fmt.Errorf("javni ključ mreže u bazi je neispravan: %w", err)
	}

	key := razmjena.PublicNetwork(name, pubKey)
	if priv, err := razmjena.LoadKey(s.networkKeyPath()); err == nil {
		if !priv.Public().(ed25519.PublicKey).Equal(pubKey) {
			return fmt.Errorf("datoteka %s ne pripada mreži %q iz baze", s.networkKeyPath(), name)
		}
		key = razmjena.LoadNetworkKey(name, priv)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("ključ mreže: %w", err)
	}

	s.mu.Lock()
	s.network = &key
	s.joinedAt = joined
	s.mu.Unlock()
	return nil
}

func (s *Service) networkKeyPath() string {
	return filepath.Join(s.node.Dir, NetworkKeyFileName)
}

// NetworkInfo vraća mrežu čvora, ili nil kad čvor još nije ni u jednoj
func (s *Service) NetworkInfo() *Network {
	s.mu.Lock()
	if s.network == nil {
		s.mu.Unlock()
		return nil
	}
	n := &Network{
		Name:      s.network.Name,
		PublicKey: razmjena.PublicKeyString(s.network.Public),
		JoinedAt:  s.joinedAt,
		CanAdmit:  s.network.CanSign(),
		DrziKljuc: s.network.CanSign(),
	}
	s.mu.Unlock()
	if !n.DrziKljuc {
		if o := s.mojaOvlast(context.Background()); o != nil {
			n.CanAdmit, n.Ovlast = true, o
		}
	}
	return n
}

// CreateNetwork osniva mrežu: ovaj čvor dobiva privatni ključ mreže i
// postaje njezin prvi član. Radi se jednom, na jednom čvoru.
func (s *Service) CreateNetwork(ctx context.Context, name string) error {
	if s.NetworkInfo() != nil {
		return fmt.Errorf("ovaj čvor već pripada mreži %q", s.NetworkInfo().Name)
	}
	if name == "" {
		return fmt.Errorf("naziv mreže je obavezan")
	}

	// Datoteka ključa bez zapisa o mreži ostaje kad baza nastane iznova
	// (ili se mreža osnivala dok baza još nije bila ta). Takav ključ nije
	// nikoga primio u ovu bazu, pa ga osnivanje preuzme umjesto da odbije
	// ili ga pregazi: ključ se nikad ne briše sam.
	var key razmjena.NetworkKey
	if priv, err := razmjena.LoadKey(s.networkKeyPath()); err == nil {
		key = razmjena.LoadNetworkKey(name, priv)
	} else if errors.Is(err, os.ErrNotExist) {
		if key, err = razmjena.NewNetwork(name); err != nil {
			return err
		}
		if err := razmjena.SaveKey(s.networkKeyPath(), key.Private()); err != nil {
			return fmt.Errorf("ključ mreže se ne može zapisati: %w", err)
		}
	} else {
		return fmt.Errorf("zatečena datoteka ključa mreže se ne čita: %w", err)
	}

	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO network (id, name, public_key, joined_at, protokol) VALUES (1, ?, ?, ?, ?)`,
		name, razmjena.PublicKeyString(key.Public), now, s.protokol()); err != nil {
		return err
	}
	s.mu.Lock()
	s.network = &key
	s.joinedAt = now
	s.mu.Unlock()
	s.prekiniUparivanje()

	// osnivač je prvi član — vlastitim potpisom
	self, err := key.Admit(s.node.ID, s.node.key.Public().(ed25519.PublicKey), s.node.ID, ValjanostOsnivaca)
	if err != nil {
		return err
	}
	return s.nakonUpisaKljuca(ctx, s.saveMembership(ctx, self))
}

// joinNetwork prihvaća mrežu iz paketa dobrodošlice — samo kad čvor još
// nije ni u jednoj mreži i kad paket nosi potvrdu za baš ovaj čvor
func (s *Service) joinNetwork(ctx context.Context, pack welcomePack) error {
	if pack.NetworkKey == "" || pack.ForYou == nil {
		return fmt.Errorf("druga strana nije nositelj mrežnog ključa ni ovlašteni primatelj — čvor je uparen, ali nije primljen u mrežu; primiti ga mora netko tko drži ključ mreže ili ovlast za primanje")
	}
	pubKey, err := razmjena.ParsePublicKey(pack.NetworkKey)
	if err != nil {
		return fmt.Errorf("neispravan ključ mreže u paketu: %w", err)
	}
	myKey := s.node.key.Public().(ed25519.PublicKey)
	if err := pack.ForYou.Verify(pubKey, myKey, time.Now()); err != nil {
		return fmt.Errorf("potvrda članstva za ovaj čvor ne vrijedi: %w", err)
	}

	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO network (id, name, public_key, joined_at, protokol) VALUES (1, ?, ?, ?, ?)`,
		pack.NetworkName, pack.NetworkKey, now, s.protokol()); err != nil {
		return err
	}
	key := razmjena.PublicNetwork(pack.NetworkName, pubKey)
	s.mu.Lock()
	s.network = &key
	s.joinedAt = now
	s.mu.Unlock()

	return s.saveMembership(ctx, *pack.ForYou)
}

// myMembership vraća potvrdu ovog čvora, ako je ima
func (s *Service) myMembership(ctx context.Context) *razmjena.Membership {
	m, _ := s.getMembership(ctx, s.node.ID)
	return m
}

// stupciClanstva su stupci tablice memberships redom koji čita scanMembership
const stupciClanstva = `node_id, public_key, network, issued_by, issued_at, expires_at, signature, primatelj`

// scanMembership čita redak članstva; primatelj je ovlast potpisnika (JSON)
// kad članstvo nije potpisao ključ mreže
func scanMembership(sc interface{ Scan(...any) error }) (razmjena.Membership, error) {
	var m razmjena.Membership
	var primatelj string
	if err := sc.Scan(&m.DeviceID, &m.DeviceKey, &m.Network, &m.IssuedBy, &m.IssuedAt, &m.ExpiresAt, &m.Signature, &primatelj); err != nil {
		return m, err
	}
	if primatelj != "" {
		var o razmjena.Ovlast
		if err := json.Unmarshal([]byte(primatelj), &o); err != nil {
			return m, fmt.Errorf("ovlast primatelja u članstvu %s: %w", m.DeviceID, err)
		}
		m.Primatelj = &o
	}
	return m, nil
}

// primateljJSON je ovlast potpisnika za stupac primatelj; prazno kad je
// članstvo potpisao ključ mreže
func primateljJSON(m razmjena.Membership) string {
	if m.Primatelj == nil {
		return ""
	}
	b, _ := json.Marshal(m.Primatelj.UTC())
	return string(b)
}

func (s *Service) getMembership(ctx context.Context, nodeID string) (*razmjena.Membership, error) {
	m, err := scanMembership(s.db.QueryRowContext(ctx, `SELECT `+stupciClanstva+` FROM memberships WHERE node_id = ?`, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// saveMembership upisuje potvrdu i ostavlja verziju — članstva se
// sinkroniziraju, pa i čvor koji nije bio prisutan pri primanju sazna za
// novog člana
func (s *Service) saveMembership(ctx context.Context, m razmjena.Membership) error {
	return s.uTransakciji(ctx, func(tx *sql.Tx) error { return s.upisiClanstvo(ctx, tx, m) })
}

// upisiClanstvo je saveMembership u transakciji koju vodi pozivatelj
func (s *Service) upisiClanstvo(ctx context.Context, tx *sql.Tx, m razmjena.Membership) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memberships (node_id, public_key, network, issued_by, issued_at, expires_at, signature, created_at, primatelj)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			public_key = excluded.public_key, network = excluded.network, issued_by = excluded.issued_by,
			issued_at = excluded.issued_at, expires_at = excluded.expires_at, signature = excluded.signature,
			primatelj = excluded.primatelj
	`, m.DeviceID, m.DeviceKey, m.Network, m.IssuedBy, m.IssuedAt.UTC(), m.ExpiresAt.UTC(), m.Signature, time.Now().UTC(), primateljJSON(m)); err != nil {
		return err
	}
	_, err := s.rec.Record(ctx, tx, EntityMemberships, m.DeviceID, m)
	return err
}

// uTransakciji izvede fn u jednoj transakciji: sve ili ništa
func (s *Service) uTransakciji(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeMembership opoziva člana: potvrda se arhivira, uz zapis opoziva, i
// to putuje na sve čvorove; od tada mu nitko ne odgovara na razmjenu, ni
// kad potvrdu pokaže sam. Opoziva se svako valjano članstvo tog imena
// (zaOpoziv), ne red u tablici. Kad opoziva ključem mreže, uvijek opoziva i
// sve dotad izdane ovlasti za primanje ključeva tog imena, i one koje ovaj
// čvor ne zna, a time i članstva koja su ti ključevi potpisali; vraća te
// čvorove. Ključem mreže opoziva se i član čije je članstvo ovdje isteklo.
// Opoziv ključem mreže vrijedi za svako članstvo ključeva tog imena izdano
// do sada (trenuciOpozivaClanstva), i za ono koje ovaj čvor ne zna, a
// opoziv primatelja samo za članstvo koje je on izdao.
func (s *Service) RevokeMembership(ctx context.Context, nodeID string) ([]string, error) {
	defer s.javiPromjenuClanstva()
	if nodeID == s.node.ID {
		return nil, fmt.Errorf("čvor ne može opozvati sam sebe")
	}
	o, err := s.zaOpozivClana(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	var pogodjeni []string
	err = s.uTransakciji(ctx, func(tx *sql.Tx) error {
		if err := s.opozoviClanstvaTx(ctx, tx, nodeID, o); err != nil || !o.mrezni {
			return err
		}
		pogodjeni, err = s.opozoviOvlastiTx(ctx, tx, nodeID, o.kljucevi, o.ovlasti, o.priv)
		return err
	})
	return pogodjeni, err
}

// opozivClana je ono što opoziv člana zapisuje: valjana članstva tog imena,
// njegove ovlasti za primanje, ključeve čije se ovlasti opozivaju, ključ
// kojim se potpisuje (mrezni kad je to ključ mreže) i po ključu trenutak
// opoziva članstva (trenuciOpozivaClanstva)
type opozivClana struct {
	clanstva []razmjena.Membership
	ovlasti  []razmjena.Ovlast
	kljucevi []string
	priv     ed25519.PrivateKey
	mrezni   bool
	izdano   map[string]time.Time
}

// trenuciOpozivaClanstva su po ključu trenuci koje nosi opoziv članstva:
// za svaki ključ iz kljucevi sada, a za ključ valjanog članstva (clanstva)
// kasniji od tog i njegova izdavanja. Ključem mreže kljucevi su svi
// poznati ključevi tog imena, pa opoziv vrijedi i za članstvo koje je do
// sada izdalo drugo računalo, a ovo ga još ne zna. Kasniji trenutak uzima
// se samo iz članstava koja sada vrijede, jer samo za njih vrijedi granica
// izdavanja unaprijed (razmjena.Membership.Verify). Primatelj zadaje samo
// članstva koja je izdao, bez ključeva i bez sada.
func trenuciOpozivaClanstva(sada time.Time, kljucevi []string, clanstva []razmjena.Membership) map[string]time.Time {
	out := map[string]time.Time{}
	for _, k := range kljucevi {
		out[k] = sada
	}
	for _, m := range clanstva {
		if t, ima := out[m.DeviceKey]; !ima || m.IssuedAt.After(t) {
			out[m.DeviceKey] = m.IssuedAt
		}
	}
	return out
}

// zaOpozivClana skuplja što opoziv člana nodeID zapisuje. Ključevi za
// opoziv su iz svakog potpisanog članstva tog imena (i isteklog) i iz
// svake njegove ovlasti. Bez valjanog članstva, bez ovlasti i bez
// neopozvanog potpisanog članstva koje ovaj čvor smije opozvati, čvor nije
// član.
func (s *Service) zaOpozivClana(ctx context.Context, nodeID string) (opozivClana, error) {
	p, err := s.ucitajClanstva(ctx)
	if errors.Is(err, errNijeClan) {
		return opozivClana{}, fmt.Errorf("ovaj čvor nije ni u jednoj mreži")
	}
	if err != nil {
		return opozivClana{}, err
	}
	o := opozivClana{clanstva: p.zaOpoziv(ctx, nodeID)}
	if o.ovlasti, err = s.ovlastiCvora(ctx, nodeID); err != nil {
		return o, err
	}
	if o.priv, o.mrezni, err = s.potpisnikOpoziva(o.clanstva, len(o.ovlasti) > 0); err != nil {
		return o, err
	}
	potpisana := p.potpisanaImena(nodeID)
	if !o.imaSto(p.neopozvana(ctx, potpisana)) {
		return o, fmt.Errorf("čvor %s nije član", nodeID)
	}
	o.kljucevi = kljuceviZaOpoziv(append(slices.Clone(o.clanstva), potpisana...), o.ovlasti)
	sada, kljucevi := time.Time{}, []string(nil)
	if o.mrezni {
		sada, kljucevi = s.sadOpoziva(), o.kljucevi
	}
	o.izdano = trenuciOpozivaClanstva(sada, kljucevi, o.clanstva)
	return o, nil
}

// imaSto: opoziv ima što opozvati, valjano članstvo ili, ključem mreže,
// ovlast za primanje ili potpisano članstvo koje još nije opozvano
// (neopozvanih, npr. isteklo)
func (o opozivClana) imaSto(neopozvanih int) bool {
	return len(o.clanstva) > 0 || (o.mrezni && len(o.ovlasti)+neopozvanih > 0)
}

// potpisnikOpoziva je ključ kojim ovaj čvor smije opozvati sva zadana
// članstva (potpisnikOpozivaClanstva), i je li to ključ mreže. Jedno koje ne
// smije odbija cijeli opoziv: primatelj ne opoziva ime čije je drugo valjano
// članstvo izdao ključ mreže ili drugi primatelj.
func (s *Service) potpisnikOpoziva(clanstva []razmjena.Membership, imaOvlast bool) (ed25519.PrivateKey, bool, error) {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network != nil && network.CanSign() {
		return network.Private(), true, nil
	}
	var priv ed25519.PrivateKey
	for _, m := range clanstva {
		k, err := s.potpisnikOpozivaClanstva(m, imaOvlast)
		if err != nil {
			return nil, false, err
		}
		priv = k
	}
	return priv, false, nil
}

// potpisnikOpozivaClanstva je ključ ovog čvora kad je on primatelj koji je
// to članstvo izdao. Člana s ovlašću za primanje (imaOvlast, iz tablice ili
// knjige) opoziva samo ključ mreže, jer mu se opoziva i ovlast.
func (s *Service) potpisnikOpozivaClanstva(m razmjena.Membership, imaOvlast bool) (ed25519.PrivateKey, error) {
	switch {
	case imaOvlast:
		return nil, fmt.Errorf("čvor %s ima ovlast za primanje: opozvati ga može samo čvor koji drži ključ mreže", m.DeviceID)
	case m.Primatelj != nil && m.Primatelj.DeviceKey == s.node.PublicKey():
		return s.node.key, nil
	}
	return nil, fmt.Errorf("članstvo čvora %s opoziva nositelj ključa mreže ili primatelj koji ga je izdao", m.DeviceID)
}

// opozoviClanstvaTx miče članstvo računala nodeID (makniClanstva) i za
// svaki ključ iz o.izdano zapisuje jedan opoziv članstva s tim trenutkom,
// potpisan ključem o.priv (vrijedi i za potvrdu koju čvor pokaže sam)
func (s *Service) opozoviClanstvaTx(ctx context.Context, tx *sql.Tx, nodeID string, o opozivClana) error {
	if err := s.makniClanstva(ctx, tx, nodeID, o.clanstva); err != nil {
		return err
	}
	for _, k := range slices.Sorted(maps.Keys(o.izdano)) {
		if err := s.zapisiOpoziv(ctx, tx, o.priv, OpozivClanstva, nodeID, k, o.izdano[k]); err != nil {
			return err
		}
	}
	return nil
}

// makniClanstva briše red članstva računala nodeID s površine i arhivira ga
// u knjizi: sa svakim valjanim članstvom iz clanstva, a kad valjanih nema, s
// onim što je u redu stajalo. Bez reda i bez valjanog članstva nema što
// maknuti.
func (s *Service) makniClanstva(ctx context.Context, tx *sql.Tx, nodeID string, clanstva []razmjena.Membership) error {
	if len(clanstva) == 0 {
		red, err := scanMembership(tx.QueryRowContext(ctx, `SELECT `+stupciClanstva+` FROM memberships WHERE node_id = ?`, nodeID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		clanstva = []razmjena.Membership{red}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memberships WHERE node_id = ?`, nodeID); err != nil {
		return err
	}
	for _, m := range clanstva {
		if _, err := s.rec.Archive(ctx, tx, EntityMemberships, nodeID, m); err != nil {
			return err
		}
	}
	return nil
}

// Member je član za prikaz
type Member struct {
	razmjena.Membership
	Valid   bool   `json:"valid"`
	Problem string `json:"problem,omitempty"`
	IsSelf  bool   `json:"is_self"`
	// OvlastZaPrimanje je zadnja ovlast ovog imena da prima druge, kad je ima
	OvlastZaPrimanje *Primatelj `json:"ovlast_za_primanje,omitempty"`
	// OvlastDrugomKljucu: ovlast za primanje glasi na drugi ključ nego
	// članstvo (npr. računalo je ponovno instalirano)
	OvlastDrugomKljucu bool `json:"ovlast_drugom_kljucu,omitempty"`
	// Rok je stanje roka članstva (obnova.go)
	Rok Rok `json:"rok"`
	// BezClanstva: računalo ima ovlast za primanje, a ovo računalo ne zna
	// nijedno njegovo valjano članstvo (clanoviSamoSOvlascu)
	BezClanstva bool `json:"bez_clanstva,omitempty"`
}

// ListMembers vraća za svako poznato računalo članstvo koje za njega
// vrijedi (clanstvaZaPrikaz, tablica i knjiga), a kad takvog nema, red
// tablice s razlogom zbog kojeg ne vrijedi. Uz svako ide zadnja ovlast za
// primanje tog imena, i kad glasi na drugi ključ.
func (s *Service) ListMembers(ctx context.Context) ([]Member, error) {
	clanstva, err := s.clanstvaZaPrikaz(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	network := s.network
	s.mu.Unlock()

	svi, _ := s.ListOvlasti(ctx) // bez ovlasti popis članova i dalje vrijedi
	ovlasti := map[string]*Primatelj{}
	for i := range svi {
		ovlasti[svi[i].DeviceID] = &svi[i]
	}

	out := make([]Member, 0, len(clanstva))
	for _, m := range clanstva {
		member := Member{Membership: m, IsSelf: m.DeviceID == s.node.ID}
		if o := ovlasti[m.DeviceID]; o != nil {
			member.OvlastZaPrimanje, member.OvlastDrugomKljucu = o, o.DeviceKey != m.DeviceKey
		}
		member.Problem = s.problemClanstva(ctx, network, m)
		member.Valid = member.Problem == ""
		member.Rok = rokZa(m.ExpiresAt, time.Now(), true)
		out = append(out, member)
	}
	return append(out, s.clanoviSamoSOvlascu(clanstva, svi)...), nil
}

// clanstvaZaPrikaz su po imenu računala članstvo koje za njega vrijedi
// (svaIzabrana, iz tablice i knjige), a za ime bez takvog red tablice,
// poredana po izdavanju. Red tablice briše svaka arhivirana verzija, pa
// sam ne kaže tko je član.
func (s *Service) clanstvaZaPrikaz(ctx context.Context) ([]razmjena.Membership, error) {
	redovi, err := s.svaClanstva(ctx)
	if err != nil {
		return nil, err
	}
	p, err := s.ucitajClanstva(ctx)
	if errors.Is(err, errNijeClan) {
		return redovi, nil
	}
	if err != nil {
		return nil, err
	}
	izabrana := p.svaIzabrana(ctx)
	out := make([]razmjena.Membership, 0, len(izabrana)+len(redovi))
	for _, m := range izabrana {
		out = append(out, *m)
	}
	for _, m := range redovi {
		if izabrana[m.DeviceID] == nil {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b razmjena.Membership) int {
		return cmp.Or(a.IssuedAt.Compare(b.IssuedAt), cmp.Compare(a.DeviceID, b.DeviceID))
	})
	return out, nil
}

// clanoviSamoSOvlascu su za prikaz druga računala koja imaju ovlast za
// primanje (ListOvlasti, tablica i knjiga), a nisu u popisu članstava za
// prikaz: ovo računalo ne zna nijedno njihovo valjano članstvo ni red
// tablice. Ne vrijede kao članovi, ali se vide, pa im računalo s ključem
// mreže može oduzeti ovlast ili opozvati članstvo.
func (s *Service) clanoviSamoSOvlascu(clanstva []razmjena.Membership, ovlasti []Primatelj) []Member {
	uPopisu := map[string]bool{s.node.ID: true}
	for _, m := range clanstva {
		uPopisu[m.DeviceID] = true
	}
	var out []Member
	for i := range ovlasti {
		o := &ovlasti[i]
		if uPopisu[o.DeviceID] {
			continue
		}
		out = append(out, Member{
			Membership:       razmjena.Membership{Network: o.Network, DeviceID: o.DeviceID, DeviceKey: o.DeviceKey},
			Problem:          "na ovom računalu nema njegovog valjanog članstva",
			OvlastZaPrimanje: o, BezClanstva: true,
		})
	}
	return out
}

// svaClanstva su sva poznata članstva, najstarija prva; čitaju se do kraja
// prije ocjene, jer ocjena čita opozive iz iste baze
func (s *Service) svaClanstva(ctx context.Context) ([]razmjena.Membership, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+stupciClanstva+` FROM memberships ORDER BY issued_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var clanstva []razmjena.Membership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		clanstva = append(clanstva, m)
	}
	return clanstva, rows.Err()
}

// problemClanstva je razlog zbog kojeg članstvo ne vrijedi za našu mrežu,
// ili "" kad vrijedi
func (s *Service) problemClanstva(ctx context.Context, network *razmjena.NetworkKey, m razmjena.Membership) string {
	if network == nil {
		return "čvor nije ni u jednoj mreži"
	}
	pub, err := razmjena.ParsePublicKey(m.DeviceKey)
	if err != nil {
		return "neispravan ključ"
	}
	if err := s.provjeriClanstvo(ctx, network.Public, m, pub); err != nil {
		if errors.Is(err, razmjena.ErrExpired) {
			return fmt.Sprintf("članstvo je isteklo %s i pričuva je prošla; računalo treba ponovno primiti u mrežu", datumHR(m.ExpiresAt))
		}
		return err.Error()
	}
	return ""
}

// welcomeFor sastavlja paket dobrodošlice za uparenog čvora; potvrdu
// članstva izdaje samo kad je primi (ovlašten čovjek potvrđuje) i kad ovaj
// čvor smije primati (ključ mreže ili ovlast za primanje).
// Paket bez potvrde je isti kao onaj čvora bez ključa mreže, pa ga i stariji
// programi razumiju.
func (s *Service) welcomeFor(ctx context.Context, peerID string, peerKey ed25519.PublicKey, primi bool) *welcomePack {
	s.mu.Lock()
	network := s.network
	s.mu.Unlock()
	if network == nil {
		return nil
	}
	pack := &welcomePack{
		NetworkName: network.Name,
		NetworkKey:  razmjena.PublicKeyString(network.Public),
		Mine:        s.myMembership(ctx),
	}
	if primi {
		if m, err := s.izdajClanstvo(ctx, peerID, peerKey); err == nil {
			pack.ForYou = &m
		}
	}
	return pack
}

// acceptWelcome obrađuje paket druge strane nakon uspješnog uparivanja.
// Vraća je li drugi čvor sada član naše mreže, je li ga upravo primilo ovo
// računalo (Primljen), i poruku za ekran. Paired postavlja pozivatelj.
func (s *Service) acceptWelcome(ctx context.Context, raw json.RawMessage, peerID string, peerKey ed25519.PublicKey, given *welcomePack) (PairOutcome, error) {
	var theirs welcomePack
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &theirs); err != nil {
			return PairOutcome{}, fmt.Errorf("paket dobrodošlice se ne može pročitati: %w", err)
		}
	}

	s.mu.Lock()
	network := s.network
	s.mu.Unlock()

	// nismo ni u jednoj mreži: jedini način unutra je da nas druga strana primi
	if network == nil {
		if err := s.joinNetwork(ctx, theirs); err != nil {
			return PairOutcome{}, err
		}
		if theirs.Mine != nil {
			_ = s.saveMembership(ctx, *theirs.Mine) // potvrda druge strane samo ubrzava prvu razmjenu; stiže i razmjenom
		}
		return PairOutcome{Member: true, Message: fmt.Sprintf("Ovaj čvor je primljen u mrežu %q.", theirs.NetworkName)}, nil
	}

	// u mreži smo: druga strana je ili iste mreže, ili ju mi primamo, ili ništa
	ourKey := razmjena.PublicKeyString(network.Public)
	if theirs.NetworkKey != "" && theirs.NetworkKey != ourKey {
		return PairOutcome{}, fmt.Errorf("čvor %s pripada drugoj mreži (%q) — ne može biti član naše", peerID, theirs.NetworkName)
	}
	if theirs.Mine != nil && s.provjeriClanstvo(ctx, network.Public, *theirs.Mine, peerKey) == nil {
		_ = s.saveMembership(ctx, *theirs.Mine) // već je član; potvrda stiže i razmjenom
		return PairOutcome{Member: true, Message: "Čvor je već član naše mreže."}, nil
	}
	if given != nil && given.ForYou != nil {
		if err := s.saveMembership(ctx, *given.ForYou); err != nil {
			return PairOutcome{}, err
		}
		return PairOutcome{Member: true, Primljen: true, Message: fmt.Sprintf("Čvor %s je primljen u mrežu %q.", peerID, network.Name)}, nil
	}
	return PairOutcome{Message: fmt.Sprintf("Čvor %s je uparen, ali NIJE član mreže — primiti ga može samo nositelj mrežnog ključa ili ovlašteni primatelj.", peerID)}, nil
}
