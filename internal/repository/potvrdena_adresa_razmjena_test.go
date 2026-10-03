package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
)

// Potvrda adrese za PIN putuje s korisnikom: verzija nosi potvrđenu
// adresu, tko i kada, a primjena na drugom čvoru ih postavlja. Uklonjena
// potvrda ne oživi iz prethodne verzije, a zapis bez tih polja (iz
// 0.0.26-alfa) primjenjuje se bez greške i bez potvrde.

// poljaPotvrde su JSON polja potvrde u verziji korisnika
var poljaPotvrde = []string{"pin_adresa_potvrdena", "pin_adresa_potvrdio", "pin_adresa_potvrdena_kad"}

// tijeloKorisnika vraća polja zadnje verzije korisnika
func tijeloKorisnika(t *testing.T, n *cvor, id uuid.UUID) map[string]json.RawMessage {
	t.Helper()
	top, err := n.rec.Latest(ctxRaz, EntityUsers, id.String())
	nuzno(t, err)
	var m map[string]json.RawMessage
	nuzno(t, json.Unmarshal(top.Payload, &m))
	return m
}

func korisnikNa(t *testing.T, n *cvor, id uuid.UUID) *models.User {
	t.Helper()
	u, err := NewUserRepository(n.db, n.rec).GetUserByID(id)
	nuzno(t, err)
	if u == nil {
		t.Fatalf("korisnika %s nema na čvoru %s", id, n.ime)
	}
	return u
}

// (e) Potvrda je u verziji i primjena na drugom čvoru je postavlja;
// uklonjena potvrda putuje jednako.
func TestPotvrdaAdresePutujeRazmjenom(t *testing.T) {
	a, b := noviCvor(t, "ured"), noviCvor(t, "unraid")
	kad := time.Date(2026, 10, 3, 9, 15, 0, 0, time.UTC)
	u := &models.User{Username: "ivo", PasswordHash: "x", FullName: "Ivo Ivić", OrgType: models.OrgPravnaOsoba,
		Email: "ivo@bistra.hr", IsActive: true,
		PINAdresaPotvrdena: "ivo@bistra.hr", PINAdresuPotvrdio: "Uprava Upravić", PINAdresaPotvrdenaKad: &kad}
	repoA := NewUserRepository(a.db, a.rec)
	nuzno(t, repoA.CreateUser(u, nil))

	m := tijeloKorisnika(t, a, u.ID)
	if string(m["pin_adresa_potvrdena"]) != `"ivo@bistra.hr"` || string(m["pin_adresa_potvrdio"]) != `"Uprava Upravić"` || len(m["pin_adresa_potvrdena_kad"]) == 0 {
		t.Fatalf("verzija ne nosi potvrdu: %s", mapaUTekst(m))
	}

	posalji(t, a, b)
	na := korisnikNa(t, b, u.ID)
	if !na.PotvrdaAdreseVrijedi() || na.PINAdresuPotvrdio != "Uprava Upravić" || na.PINAdresaPotvrdenaKad == nil || !na.PINAdresaPotvrdenaKad.Equal(kad) {
		t.Fatalf("potvrda nije stigla: %q %q %v", na.PINAdresaPotvrdena, na.PINAdresuPotvrdio, na.PINAdresaPotvrdenaKad)
	}

	// uklonjena potvrda: polja nema u verziji i ne vraćaju se iz prethodne
	x := korisnikNa(t, a, u.ID)
	x.PINAdresaPotvrdena, x.PINAdresuPotvrdio, x.PINAdresaPotvrdenaKad = "", "", nil
	nuzno(t, repoA.UpdateUser(x))
	m = tijeloKorisnika(t, a, u.ID)
	for _, p := range poljaPotvrde {
		if _, ima := m[p]; ima {
			t.Fatalf("uklonjena potvrda oživjela je iz prethodne verzije (%s): %s", p, mapaUTekst(m))
		}
	}
	posalji(t, a, b)
	if na := korisnikNa(t, b, u.ID); na.PINAdresaPotvrdena != "" || na.PINAdresuPotvrdio != "" || na.PINAdresaPotvrdenaKad != nil {
		t.Fatalf("uklonjena potvrda nije stigla: %+v", na)
	}
}

// (f) Zapis bez polja potvrde, kakav piše 0.0.26-alfa, primjenjuje se bez
// greške i bez potvrde: na novom čvoru i preko računa koji ju je imao.
func TestStariZapisKorisnikaBezPotvrde(t *testing.T) {
	b := noviCvor(t, "unraid")
	id := uuid.Must(uuid.NewV7())
	kad := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	// verzija bez polja potvrde, kao iz programa koji ih ne zna
	stara := func(email string) ledger.Version {
		var m map[string]any
		tijelo, err := json.Marshal(versionOfUser(models.User{ID: id, Username: "ivo", PasswordHash: "x", FullName: "Ivo Ivić",
			OrgType: models.OrgPravnaOsoba, Email: email, IsActive: true, CreatedAt: kad, UpdatedAt: kad}))
		nuzno(t, err)
		nuzno(t, json.Unmarshal(tijelo, &m))
		for _, p := range poljaPotvrde {
			delete(m, p)
		}
		tijelo, err = json.Marshal(m)
		nuzno(t, err)
		return ledger.Version{VersionID: uuid.Must(uuid.NewV7()).String(), Entity: EntityUsers, EntityID: id.String(),
			NodeID: "stari", Payload: tijelo, CreatedAt: time.Now().UTC(), SchemaVersion: ledger.SchemaVersion}
	}
	primi(t, b, []ledger.Version{stara("ivo@bistra.hr")})
	if u := korisnikNa(t, b, id); u.PINAdresaPotvrdena != "" || u.PINAdresuPotvrdio != "" || u.PINAdresaPotvrdenaKad != nil || u.Email != "ivo@bistra.hr" {
		t.Fatalf("stari zapis na novom čvoru: %+v", u)
	}

	// račun s potvrdom pa noviji zapis bez nje: verzija je istina
	x := korisnikNa(t, b, id)
	x.PINAdresaPotvrdena, x.PINAdresuPotvrdio, x.PINAdresaPotvrdenaKad = "ivo@bistra.hr", "Uprava", &kad
	nuzno(t, NewUserRepository(b.db, b.rec).UpdateUser(x))
	if !korisnikNa(t, b, id).PotvrdaAdreseVrijedi() {
		t.Fatal("potvrda nije upisana")
	}
	primi(t, b, []ledger.Version{stara("ivo@bistra.hr")})
	if u := korisnikNa(t, b, id); u.PINAdresaPotvrdena != "" || u.PINAdresaPotvrdenaKad != nil {
		t.Fatalf("zapis bez potvrde ostavio je potvrdu: %+v", u)
	}
}

// korisnik026 je korisnik kako ga zna 0.0.26-alfa: bez polja potvrde
type korisnik026 struct {
	ID           uuid.UUID      `json:"id"`
	Username     string         `json:"username"`
	FullName     string         `json:"full_name"`
	OrgType      models.OrgType `json:"org_type"`
	Email        string         `json:"email"`
	IsActive     bool           `json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	PasswordHash string         `json:"password_hash"`
}

// Stari čvor koji uredi korisnika potvrdu ne poznaje, ali je prenosi u
// svoju verziju (knjiga prepisuje nepoznata polja), pa je novi čvor čuva.
func TestStariCvorCuvaPotvrdu(t *testing.T) {
	a, b := noviCvor(t, "ured"), noviCvor(t, "stari")
	kad := time.Date(2026, 10, 3, 9, 15, 0, 0, time.UTC)
	u := &models.User{Username: "ivo", PasswordHash: "x", FullName: "Ivo Ivić", OrgType: models.OrgPravnaOsoba,
		Email: "ivo@bistra.hr", IsActive: true,
		PINAdresaPotvrdena: "ivo@bistra.hr", PINAdresuPotvrdio: "Uprava", PINAdresaPotvrdenaKad: &kad}
	nuzno(t, NewUserRepository(a.db, a.rec).CreateUser(u, nil))
	posalji(t, a, b)

	// stari čvor zapiše izmjenu imena oblikom koji zna
	tx, err := b.db.Begin()
	nuzno(t, err)
	_, err = b.rec.Record(ctxRaz, tx, EntityUsers, u.ID.String(), korisnik026{ID: u.ID, Username: u.Username,
		FullName: "Ivo Ivić (stari čvor)", OrgType: u.OrgType, Email: u.Email, IsActive: true,
		CreatedAt: u.CreatedAt, UpdatedAt: time.Now().UTC(), PasswordHash: "x"})
	nuzno(t, err)
	nuzno(t, tx.Commit())
	m := tijeloKorisnika(t, b, u.ID)
	if string(m["pin_adresa_potvrdena"]) != `"ivo@bistra.hr"` || string(m["pin_adresa_potvrdio"]) != `"Uprava"` {
		t.Fatalf("stari čvor izgubio je potvrdu: %s", mapaUTekst(m))
	}

	posalji(t, b, a)
	na := korisnikNa(t, a, u.ID)
	if na.FullName != "Ivo Ivić (stari čvor)" || !na.PotvrdaAdreseVrijedi() || na.PINAdresuPotvrdio != "Uprava" {
		t.Fatalf("izmjena sa starog čvora: %q, potvrda %q %q", na.FullName, na.PINAdresaPotvrdena, na.PINAdresuPotvrdio)
	}
}

func mapaUTekst(m map[string]json.RawMessage) string {
	var b strings.Builder
	for k, v := range m {
		b.WriteString(k + "=" + string(v) + " ")
	}
	return b.String()
}
