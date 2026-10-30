package peers

import (
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// potpisanoClanstvo je članstvo koje ključ mreže priv potpiše s
// proizvoljnim trenutkom izdavanja i isteka (kanonski oblik v1,
// razmjena.Membership.signedBytes)
func potpisanoClanstvo(t *testing.T, priv ed25519.PrivateKey, id string, kljuc ed25519.PublicKey, izdano, istek time.Time) razmjena.Membership {
	t.Helper()
	m := razmjena.Membership{Network: razmjena.PublicKeyString(priv.Public().(ed25519.PublicKey)), DeviceID: id,
		DeviceKey: razmjena.PublicKeyString(kljuc), IssuedBy: "racunalo-x", IssuedAt: izdano.UTC().Truncate(time.Second), ExpiresAt: istek.UTC().Truncate(time.Second)}
	b, err := json.Marshal(struct {
		V         int    `json:"v"`
		Network   string `json:"network"`
		DeviceID  string `json:"deviceId"`
		DeviceKey string `json:"deviceKey"`
		IssuedBy  string `json:"issuedBy"`
		IssuedAt  int64  `json:"issuedAt"`
		ExpiresAt int64  `json:"expiresAt"`
	}{1, m.Network, m.DeviceID, m.DeviceKey, m.IssuedBy, m.IssuedAt.Unix(), m.ExpiresAt.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b))
	return m
}

// potpisanaOvlast je ovlast za primanje koju ključ mreže priv potpiše s
// proizvoljnim trenutkom izdavanja i isteka (razmjena.Ovlast.signedBytes)
func potpisanaOvlast(t *testing.T, priv ed25519.PrivateKey, id string, kljuc ed25519.PublicKey, izdano, istek time.Time) razmjena.Ovlast {
	t.Helper()
	o := razmjena.Ovlast{Network: razmjena.PublicKeyString(priv.Public().(ed25519.PublicKey)), DeviceID: id,
		DeviceKey: razmjena.PublicKeyString(kljuc), IssuedBy: "racunalo-x", IssuedAt: izdano.UTC().Truncate(time.Second), ExpiresAt: istek.UTC().Truncate(time.Second)}
	b, err := json.Marshal(struct {
		V         string `json:"v"`
		Network   string `json:"network"`
		DeviceID  string `json:"deviceId"`
		DeviceKey string `json:"deviceKey"`
		IssuedBy  string `json:"issuedBy"`
		IssuedAt  int64  `json:"issuedAt"`
		ExpiresAt int64  `json:"expiresAt"`
	}{"ovlast/1", o.Network, o.DeviceID, o.DeviceKey, o.IssuedBy, o.IssuedAt.Unix(), o.ExpiresAt.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	o.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b))
	if err := o.Verify(priv.Public().(ed25519.PublicKey), o.IssuedAt); err != nil {
		t.Fatalf("ovlast iz testa ne prolazi provjeru: %v", err)
	}
	return o
}

// Ključem mreže opoziva se i član kojem je članstvo isteklo i prošlo
// pričuvu, a ovlast za primanje još vrijedi: opoziv opozove ovlast, pa
// članstvo koje bi si član njome sam potpisao ne vrijedi. Bez ovlasti takav
// član i dalje nije član.
func TestOpozivClanaSIsteklimClanstvom(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	sada := time.Now()
	izdano := sada.Add(-MembershipValidity - PricuvniRok - 5*24*time.Hour)
	mb := potpisanoClanstvo(t, a.network.Private(), b.node.ID, b.javni(), izdano, izdano.Add(MembershipValidity))
	if err := a.saveMembership(ctxRub, mb); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RevokeMembership(ctxRub, b.node.ID); err == nil || !strings.Contains(err.Error(), "nije član") {
		t.Errorf("opoziv isteklog člana bez ovlasti: %v", err)
	}

	o := potpisanaOvlast(t, a.network.Private(), b.node.ID, b.javni(), izdano, izdano.Add(OvlastValidity))
	if err := a.saveOvlast(ctxRub, o); err != nil {
		t.Fatal(err)
	}
	if !a.trusted(b.javni(), samopotpisan(t, b, o, b.node.ID, b.javni())) {
		t.Fatal("član ovlašću ne potpisuje sebi članstvo ni prije opoziva")
	}
	if _, err := a.RevokeMembership(ctxRub, b.node.ID); err != nil {
		t.Fatalf("opoziv člana s isteklim članstvom i valjanom ovlašću: %v", err)
	}
	if err := a.provjeriOvlast(ctxRub, a.network.Public, o); !errors.Is(err, ErrOpozvano) {
		t.Errorf("ovlast nije opozvana: %v", err)
	}
	if a.trusted(b.javni(), samopotpisan(t, b, o, b.node.ID, b.javni())) {
		t.Error("član prolazi s članstvom koje je sam potpisao nakon opoziva")
	}
	if o, err := a.getOvlast(ctxRub, b.node.ID); err != nil || o != nil {
		t.Errorf("opozvana ovlast ostala je u tablici: %+v, %v", o, err)
	}
}

// Ovlast koju je izdalo računalo sa satom koji ide naprijed ima trenutak
// izdavanja iza „sada” ovog računala; opoziv je svejedno pokriva, i
// opozivom člana i opozivom ovlasti
func TestOpozivPokrivaOvlastIzdanuUnaprijed(t *testing.T) {
	for ime, opozovi := range map[string]func(a *Service, id string) error{
		"opoziv člana":   func(a *Service, id string) error { _, err := a.RevokeMembership(ctxRub, id); return err },
		"opoziv ovlasti": func(a *Service, id string) error { _, err := a.OpozoviOvlast(ctxRub, id); return err },
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			b := rubniCvor(t, "ured-vukovar")
			primiIzravno(t, a, b)
			unaprijed := time.Now().Add(5 * time.Minute)
			o := potpisanaOvlast(t, a.network.Private(), b.node.ID, b.javni(), unaprijed, unaprijed.Add(OvlastValidity))
			primiVerziju(t, a, EntityOvlasti, b.node.ID, "racunalo-x", false, o)
			if err := opozovi(a, b.node.ID); err != nil {
				t.Fatal(err)
			}
			if !a.opozvano(ctxRub, OpozivOvlasti, o.DeviceKey, o.IssuedAt, a.network.Public) {
				t.Error("ovlast izdana unaprijed nije opozvana")
			}
		})
	}
	// kontrola: bez ovlasti iz budućnosti opoziv nosi trenutak opoziva
	sada := time.Now()
	if got := trenutakOpoziva(sada, []razmjena.Ovlast{{IssuedAt: sada.Add(-time.Hour)}}); !got.Equal(sada) {
		t.Errorf("opoziv nosi %v, a treba %v", got, sada)
	}
}

// Opoziv se čita samo pod oznakom koja počinje njegovom vrstom i ključem
// (kao što je oznaka opoziva izvedena), i samo kad mu sadržaj glasi na taj
// ključ: potpisan opoziv pod drugom oznakom zapisa ništa ne opoziva
func TestOpozivSeCitaPodSvojomOznakom(t *testing.T) {
	a := rubnaMreza(t, "cop-osijek")
	b := rubniCvor(t, "ured-vukovar")
	mb := primiIzravno(t, a, b)
	potpisi := func(kljuc string) Opoziv {
		return Opoziv{Vrsta: OpozivClanstva, NodeID: b.node.ID, PublicKey: kljuc, IssuedAt: mb.IssuedAt,
			OpozvanoAt: time.Now().UTC().Truncate(time.Second), Opozvao: a.node.ID}.Potpisi(a.network.Private())
	}
	uKnjigu := func(oznaka string, op Opoziv) {
		t.Helper()
		p, err := json.Marshal(op)
		if err != nil {
			t.Fatal(err)
		}
		v := ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: EntityOpozivi, EntityID: oznaka, NodeID: "racunalo-x",
			Payload: p, CreatedAt: time.Now().UTC(), SchemaVersion: 1}
		if _, err := a.rec.Apply(ctxRub, []ledger.Version{v}); err != nil {
			t.Fatal(err)
		}
	}
	op := potpisi(mb.DeviceKey)
	uKnjigu("drugi-zapis", op)
	tudji := potpisi(razmjena.PublicKeyString(izmisljeniKljuc(t)))
	uKnjigu(OpozivClanstva+":"+mb.DeviceKey+":"+tudji.ID, tudji)
	if a.opozvano(ctxRub, OpozivClanstva, mb.DeviceKey, mb.IssuedAt, a.network.Public) || !a.trusted(b.javni(), nil) {
		t.Error("opoziv pod oznakom drugog zapisa ili za drugi ključ opozvao je članstvo")
	}
	uKnjigu(op.ID, op)
	if !a.opozvano(ctxRub, OpozivClanstva, mb.DeviceKey, mb.IssuedAt, a.network.Public) || a.trusted(b.javni(), nil) {
		t.Error("opoziv pod svojom oznakom nije pročitan")
	}
}

// Opoziv člana ključem mreže uvijek miče red ovlasti tog računala, i kad u
// njemu stoji ovlast koja ne vrijedi (nepotpisana ili opozvana); poslije ga
// popis ovlasti ne pokazuje
func TestOpozivClanaMiceSvakiRedOvlasti(t *testing.T) {
	for ime, upisi := range map[string]func(t *testing.T, a, b *Service){
		"nepotpisana ovlast": func(t *testing.T, a, b *Service) {
			primiVerziju(t, a, EntityOvlasti, b.node.ID, "clan-m", false, razmjena.Ovlast{Network: a.NetworkInfo().PublicKey,
				DeviceID: b.node.ID, DeviceKey: b.node.PublicKey(), IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), Signature: "AAAA"})
		},
		"opozvana ovlast": func(t *testing.T, a, b *Service) {
			if _, err := a.IzdajOvlast(ctxRub, b.node.ID); err != nil {
				t.Fatal(err)
			}
			if err := a.uTransakciji(ctxRub, func(tx *sql.Tx) error {
				return a.zapisiOpoziv(ctxRub, tx, a.network.Private(), OpozivOvlasti, b.node.ID, b.node.PublicKey(), time.Now())
			}); err != nil {
				t.Fatal(err)
			}
			if o, err := a.getOvlast(ctxRub, b.node.ID); err != nil || o == nil {
				t.Fatalf("red opozvane ovlasti nije u tablici: %v", err)
			}
		},
	} {
		t.Run(ime, func(t *testing.T) {
			a := rubnaMreza(t, "cop-osijek")
			b := rubniCvor(t, "ured-vukovar")
			primiIzravno(t, a, b)
			upisi(t, a, b)
			if _, err := a.RevokeMembership(ctxRub, b.node.ID); err != nil {
				t.Fatal(err)
			}
			var n int
			if err := a.db.QueryRow(`SELECT count(*) FROM ovlasti WHERE node_id = ?`, b.node.ID).Scan(&n); err != nil || n != 0 {
				t.Errorf("redova ovlasti nakon opoziva: %d (%v)", n, err)
			}
			if err := a.db.QueryRow(`SELECT count(*) FROM record_versions WHERE entity = ? AND entity_id = ? AND archived = 1`, EntityOvlasti, b.node.ID).Scan(&n); err != nil || n != 1 {
				t.Errorf("spomenika ovlasti: %d (%v)", n, err)
			}
		})
	}
}
