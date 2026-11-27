package peers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/razmjena"
)

// zapisKorisnika upiše zapis users i vrati njegovu verziju iz knjige
func zapisKorisnika(t *testing.T, s *Service, id string) ledger.Version {
	t.Helper()
	if _, err := s.rec.Record(ctxRub, s.db, "users", id, map[string]string{"id": id, "ime": "Ivana Kovač"}); err != nil {
		t.Fatal(err)
	}
	v, err := s.rec.Latest(ctxRub, "users", id)
	if err != nil {
		t.Fatal(err)
	}
	return *v
}

// novoRacunalo je računalo koje prava i mrežu prima strogo i već zna da
// autor potpisuje: ima njegovu potpisanu verziju iz drugog kanala, pa
// granica autora za zajednički kanal ostaje prazna
func novoRacunalo(t *testing.T, c, autor *Service, potpisana ledger.Version) {
	t.Helper()
	c.SetWantsAll(true)
	c.PostaviStrogePotpise("users")
	if n, err := c.primijeniPodBravom(ctxRub, []ledger.Version{potpisana}); err != nil || n != 1 {
		t.Fatalf("potpisana verzija autora: %d, %v", n, err)
	}
	if !c.novaProvjeraPotpisa(ctxRub, nil, true).autorPotpisuje(autor.node.ID) {
		t.Fatal("novo računalo ne zna da autor potpisuje")
	}
}

// Zapis prava napisan prije potpisa (0.0.69, bez vremena primanja) novo
// računalo razmjenom ne prima, jer autor već potpisuje. Kad ga autor
// naknadno potpiše (PotpisiVlastiteStare), isti zapis stigne potpisan i
// uđe u knjigu, a potpis te stare verzije zapiše se u dnevnik. Računalo
// koje je zapis već imalo bez potpisa dobije potpis dopunom, a zapis se ne
// mijenja (Odluke (27. 11. 2026.)).
func TestStariZapisPravaStizePotpisan(t *testing.T) {
	cv := clanoviMreze(t, false, "cop-osijek", "laptop-baranja", "novi-laptop", "laptop-vukovar")
	a, staro, kontrola, novi := cv[0], cv[1], cv[2], cv[3]
	a.SetWantsAll(true)
	a.PostaviStrogePotpise("users")
	staro.SetWantsAll(true)

	stari := zapisKorisnika(t, a, "u-staro")
	if _, err := a.db.Exec(`UPDATE record_versions SET primljeno = NULL WHERE version_id = ?`, stari.VersionID); err != nil {
		t.Fatal(err)
	}
	razgovaraj(t, staro, a) // računalo koje je zapis primilo dok nije bio potpisan
	if v := redakKnjige(t, staro, stari.VersionID); v == nil || v.Potpis != "" {
		t.Fatalf("stari zapis kod računala iz 0.0.69: %+v", v)
	}

	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
	upisiOcitanja(t, a, "ocitanja/16/2026", "o1")
	ocitanje, err := a.rec.Latest(ctxRub, "probni", "o1")
	if err != nil {
		t.Fatal(err)
	}
	novi1 := zapisKorisnika(t, a, "u-novo")
	vrijediPotpis(t, a, Protocol, novi1)

	// kontrola: bez naknadnog potpisa stari zapis ne uđe
	novoRacunalo(t, kontrola, a, *ocitanje)
	razgovaraj(t, kontrola, a)
	if redakKnjige(t, kontrola, novi1.VersionID) == nil {
		t.Fatal("noviji potpisani zapis nije stigao")
	}
	if redakKnjige(t, kontrola, stari.VersionID) != nil {
		t.Fatal("nepotpisan stari zapis ušao je kod novog računala; kontrola ne vrijedi")
	}

	var dnevnik bytes.Buffer
	stariDnevnik := log.Writer()
	log.SetOutput(&dnevnik)
	n, err := a.PotpisiVlastiteStare(ctxRub)
	log.SetOutput(stariDnevnik)
	if err != nil || n != 1 {
		t.Fatalf("naknadni potpis: %d, %v", n, err)
	}
	if !strings.Contains(dnevnik.String(), "users/u-staro, verzija "+stari.VersionID) {
		t.Errorf("potpis stare verzije nije u dnevniku: %q", dnevnik.String())
	}
	if n, err := a.PotpisiVlastiteStare(ctxRub); err != nil || n != 0 {
		t.Errorf("drugi poziv: %d, %v", n, err)
	}
	potpisan, err := a.rec.Verzije(ctxRub, []string{stari.VersionID})
	if err != nil || len(potpisan) != 1 {
		t.Fatal(err)
	}
	vrijediPotpis(t, a, Protocol, potpisan[0])

	novoRacunalo(t, novi, a, *ocitanje)
	razgovaraj(t, novi, a)
	if v, err := novi.rec.Verzije(ctxRub, []string{stari.VersionID}); err != nil || len(v) != 1 {
		t.Fatalf("potpisan stari zapis nije ušao kod novog računala: %v, %v", v, err)
	} else {
		vrijediPotpis(t, a, Protocol, v[0])
	}

	// računalo s nepotpisanom kopijom: ista verzija stigne potpisana
	if n, err := staro.primijeniPodBravom(ctxRub, potpisan); err != nil || n != 0 {
		t.Fatalf("ponovno primanje: %d, %v", n, err)
	}
	v, err := staro.rec.Verzije(ctxRub, []string{stari.VersionID})
	if err != nil || len(v) != 1 {
		t.Fatal(err)
	}
	vrijediPotpis(t, a, Protocol, v[0])
	if string(v[0].Payload) != string(stari.Payload) {
		t.Errorf("dopuna je promijenila zapis: %s, a bilo je %s", v[0].Payload, stari.Payload)
	}
}

// Verzija otprije 0.0.70 ostaje bez potpisa kad pod imenom ovog računala
// postoji članstvo s drugim ključem i valjanim potpisom ključa mreže: ne
// zna se koji ju je ključ napisao. Lažno članstvo, koje može napisati bilo
// koji član, potpis ne sprječava. Novija vlastita verzija bez potpisa se
// svejedno potpiše.
func TestStariZapisUzDrugiKljucOstaje(t *testing.T) {
	osnivac := rubnaMreza(t, "osnivac-mreze")
	a := rubniCvor(t, "cop-osijek")
	primiIzravno(t, osnivac, a)
	a.PostaviStrogePotpise("users")
	stari := zapisKorisnika(t, a, "u-staro")
	if _, err := a.db.Exec(`UPDATE record_versions SET primljeno = NULL WHERE version_id = ?`, stari.VersionID); err != nil {
		t.Fatal(err)
	}
	noviji := zapisKorisnika(t, a, "u-bez-kljuca") // npr. -ponisti-lozinku bez datoteke ključa
	drugiKljuc, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// lažno: pravo članstvo drugog računala, prepisano na ime a
	lazno, err := osnivac.izdajClanstvo(ctxRub, "laptop-baranja", drugiKljuc)
	if err != nil {
		t.Fatal(err)
	}
	lazno.DeviceID = a.node.ID
	if _, err := a.rec.Record(ctxRub, a.db, EntityMemberships, "lazno", lazno); err != nil {
		t.Fatal(err)
	}
	if drugi, err := a.drugiKljucPodImenom(ctxRub); err != nil || drugi {
		t.Fatalf("lažno članstvo broji se kao drugi ključ: %v, %v", drugi, err)
	}

	// pravo: ime a s drugim ključem, npr. prije ponovne instalacije
	pravo, err := osnivac.izdajClanstvo(ctxRub, a.node.ID, drugiKljuc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.rec.Record(ctxRub, a.db, EntityMemberships, "staro-clanstvo", pravo); err != nil {
		t.Fatal(err)
	}
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
	if n, err := a.PotpisiVlastiteStare(ctxRub); err != nil || n != 1 {
		t.Fatalf("potpisano %d, %v", n, err)
	}
	if v := redakKnjige(t, a, stari.VersionID); v == nil || v.Potpis != "" {
		t.Errorf("stara verzija potpisana uz drugi ključ pod istim imenom: %+v", v)
	}
	if v := redakKnjige(t, a, noviji.VersionID); v == nil || v.Potpis == "" {
		t.Errorf("novija vlastita verzija nije potpisana: %+v", v)
	}
}

// Računalo koje nije ni u jednoj mreži ne može provjeriti potpis članstva,
// pa kao drugi ključ broji i članstvo bez valjanog potpisa
func TestDrugiKljucBezMreze(t *testing.T) {
	c := rubniCvor(t, "ured-vukovar")
	if drugi, err := c.drugiKljucPodImenom(ctxRub); err != nil || drugi {
		t.Fatalf("bez članstava: %v, %v", drugi, err)
	}
	drugiKljuc, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := razmjena.Membership{DeviceID: c.node.ID, DeviceKey: razmjena.PublicKeyString(drugiKljuc)}
	if _, err := c.rec.Record(ctxRub, c.db, EntityMemberships, c.node.ID, m); err != nil {
		t.Fatal(err)
	}
	if drugi, err := c.drugiKljucPodImenom(ctxRub); err != nil || !drugi {
		t.Errorf("članstvo s drugim ključem bez mreže: %v, %v", drugi, err)
	}
}

// Bez strogih entiteta naknadni potpis ne radi ništa, ni kad knjiga ima
// potpisnika i nepotpisane vlastite verzije
func TestPotpisiVlastiteStareBezStrogih(t *testing.T) {
	a := rubniCvor(t, "cop-osijek")
	v := zapisKorisnika(t, a, "u-staro")
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
	if n, err := a.PotpisiVlastiteStare(ctxRub); err != nil || n != 0 {
		t.Fatalf("bez strogih entiteta: %d, %v", n, err)
	}
	if r := redakKnjige(t, a, v.VersionID); r == nil || r.Potpis != "" {
		t.Errorf("potpisano bez strogih entiteta: %+v", r)
	}
}

// Kad se članstva ne daju pročitati, ne zna se ima li pod ovim imenom drugi
// ključ: verzija otprije 0.0.70 ostaje bez potpisa, novija vlastita se
// potpiše, a greška čitanja se javi
func TestPotpisiVlastiteStareBezClanstava(t *testing.T) {
	a := rubniCvor(t, "cop-osijek")
	a.PostaviStrogePotpise("users")
	stari := zapisKorisnika(t, a, "u-staro")
	if _, err := a.db.Exec(`UPDATE record_versions SET primljeno = NULL WHERE version_id = ?`, stari.VersionID); err != nil {
		t.Fatal(err)
	}
	noviji := zapisKorisnika(t, a, "u-novo")
	if _, err := a.db.Exec(`DROP TABLE memberships`); err != nil {
		t.Fatal(err)
	}
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
	n, err := a.PotpisiVlastiteStare(ctxRub)
	if n != 1 || err == nil || !strings.Contains(err.Error(), "članstva ovog računala ne daju se pročitati") {
		t.Fatalf("potpisano %d, %v", n, err)
	}
	if v := redakKnjige(t, a, stari.VersionID); v == nil || v.Potpis != "" {
		t.Errorf("stara verzija potpisana bez članstava: %+v", v)
	}
	if v := redakKnjige(t, a, noviji.VersionID); v == nil || v.Potpis == "" {
		t.Errorf("novija vlastita verzija nije potpisana: %+v", v)
	}
}

// Dok brava upisa nije slobodna, a ctx istekne, naknadni potpis ne upiše
// ništa i vrati grešku ctx
func TestPotpisiVlastiteStareBezBrave(t *testing.T) {
	a := rubniCvor(t, "cop-osijek")
	a.PostaviStrogePotpise("users")
	v := zapisKorisnika(t, a, "u-novo")
	a.rec.PostaviPotpisnika(a.node.PotpisnikVerzija(Protocol))
	if err := a.uzmiBravu(ctxRub); err != nil {
		t.Fatal(err)
	}
	defer a.pustiBravu()
	ctx, cancel := context.WithTimeout(ctxRub, 20*time.Millisecond)
	defer cancel()
	if n, err := a.PotpisiVlastiteStare(ctx); n != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bez brave: %d, %v", n, err)
	}
	if r := redakKnjige(t, a, v.VersionID); r == nil || r.Potpis != "" {
		t.Errorf("potpisano bez brave: %+v", r)
	}
}

// Greška čitanja članstava vrati se iz drugiKljucPodImenom: u mreži, i bez
// mreže kad se ne da pročitati knjiga ili tablica članstava
func TestDrugiKljucGreskaCitanja(t *testing.T) {
	ugasen, cancel := context.WithCancel(ctxRub)
	cancel()
	for _, tc := range []struct {
		ime  string
		cvor func(t *testing.T) (*Service, context.Context)
	}{
		{"u mreži, bez tablice članstava", func(t *testing.T) (*Service, context.Context) {
			c := rubnaMreza(t, "osnivac-mreze")
			if _, err := c.db.Exec(`DROP TABLE memberships`); err != nil {
				t.Fatal(err)
			}
			return c, ctxRub
		}},
		{"bez mreže, bez tablice članstava", func(t *testing.T) (*Service, context.Context) {
			c := rubniCvor(t, "ured-vukovar")
			if _, err := c.db.Exec(`DROP TABLE memberships`); err != nil {
				t.Fatal(err)
			}
			return c, ctxRub
		}},
		{"bez mreže, knjiga se ne da pročitati", func(t *testing.T) (*Service, context.Context) {
			return rubniCvor(t, "ured-vukovar"), ugasen
		}},
	} {
		t.Run(tc.ime, func(t *testing.T) {
			c, ctx := tc.cvor(t)
			if drugi, err := c.drugiKljucPodImenom(ctx); err == nil || drugi {
				t.Fatalf("greška čitanja nije javljena: %v, %v", drugi, err)
			}
		})
	}
}
