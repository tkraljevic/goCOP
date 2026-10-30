package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
)

// prenesiKorisnika radi što i razmjena: verzije računa s čvora a idu u
// knjigu čvora b, pa s nje na površinu
func prenesiKorisnika(t *testing.T, a, b *cvor, id uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	verzije, err := a.rec.History(ctx, repository.EntityUsers, id.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.rec.Apply(ctx, verzije); err != nil {
		t.Fatal(err)
	}
	if err := repository.ApplyVersions(ctx, b.db, b.rec, verzije); err != nil {
		t.Fatal(err)
	}
}

func sesijaVrijedi(t *testing.T, n *cvor, id uuid.UUID) bool {
	t.Helper()
	_, err := n.auth.AuthenticateSessionView(id)
	if err != nil && !errors.Is(err, service.ErrSessionExpired) {
		t.Fatalf("provjera sesije: %v", err)
	}
	return err == nil
}

// Lozinka promijenjena ili poništena na jednom računalu gasi prijave te
// osobe i na drugom, kad promjena stigne razmjenom. Prijava iz koje je
// osoba sama promijenila lozinku ostaje, a izmjena računa koja ne dira
// lozinku ne gasi ništa.
func TestPromjenaLozinkeGasiSesijeNaDrugomRacunalu(t *testing.T) {
	a, b := noviCvor(t, "cvor-a"), noviCvor(t, "cvor-b")
	u, err := a.repo.GetUserByUsername("aanic")
	nuzno(t, err)
	hash, err := a.auth.HashPassword("stara-lozinka-2026")
	nuzno(t, err)
	nuzno(t, a.repo.ChangePassword(u.ID, hash))
	prenesiKorisnika(t, a, b, u.ID)

	naA, _, err := a.auth.Login("aanic", "stara-lozinka-2026", "192.168.1.10", "ured")
	nuzno(t, err)
	naB, _, err := b.auth.Login("aanic", "stara-lozinka-2026", "198.51.100.7", "laptop")
	nuzno(t, err)

	// izmjena računa bez nove lozinke: sesija na B ostaje
	u, err = a.repo.GetUserByID(u.ID)
	nuzno(t, err)
	u.FullName = "Ana Anić-Horvat"
	nuzno(t, a.repo.UpdateUser(u))
	prenesiKorisnika(t, a, b, u.ID)
	if !sesijaVrijedi(t, b, naB.ID) {
		t.Fatal("izmjena imena ne smije ugasiti sesiju na drugom računalu")
	}

	nuzno(t, a.auth.ChangePassword(u.ID, "stara-lozinka-2026", "nova-lozinka-2026", naA.ID))
	if !sesijaVrijedi(t, a, naA.ID) {
		t.Fatal("prijava iz koje je lozinka promijenjena mora ostati")
	}
	if !sesijaVrijedi(t, b, naB.ID) {
		t.Fatal("prije razmjene B ne zna za novu lozinku")
	}
	prenesiKorisnika(t, a, b, u.ID)
	if sesijaVrijedi(t, b, naB.ID) {
		t.Fatal("sesija stare lozinke na drugom računalu i dalje vrijedi")
	}
	if s, err := repository.NewSessionRepository(b.db).GetSession(naB.ID); err != nil || s != nil {
		t.Errorf("sesija stare lozinke mora biti obrisana: %v %v", s, err)
	}
	if _, _, err := b.auth.AuthenticateSession(naB.ID); !errors.Is(err, service.ErrSessionExpired) {
		t.Errorf("AuthenticateSession: %v", err)
	}

	// nova lozinka na B otvara sesiju koja vrijedi; poništenje na A je gasi
	naB2, _, err := b.auth.Login("aanic", "nova-lozinka-2026", "198.51.100.7", "laptop")
	nuzno(t, err)
	if !sesijaVrijedi(t, b, naB2.ID) {
		t.Fatal("sesija nove lozinke ne vrijedi")
	}
	privremena, err := a.auth.HashPassword("privremena-2026")
	nuzno(t, err)
	nuzno(t, a.repo.ResetPassword(u.ID, privremena))
	prenesiKorisnika(t, a, b, u.ID)
	if sesijaVrijedi(t, b, naB2.ID) {
		t.Error("poništena lozinka mora ugasiti sesiju na drugom računalu")
	}
}

// Sesija bez otiska lozinke (otvorena prije nadogradnje) ne vrijedi, pa se
// osoba jednom ponovno prijavi
func TestSesijaBezOtiskaLozinke(t *testing.T) {
	n := noviCvor(t, "cvor-a")
	u, err := n.repo.GetUserByUsername("aanic")
	nuzno(t, err)
	id := uuid.New()
	_, err = n.db.Exec(`INSERT INTO sessions (id, user_id, ip_address, user_agent, expires_at, created_at)
		VALUES (?, ?, '', '', ?, ?)`, id.String(), u.ID.String(), time.Now().UTC().Add(time.Hour), time.Now().UTC())
	nuzno(t, err)
	if sesijaVrijedi(t, n, id) {
		t.Error("sesija bez otiska lozinke ne smije vrijediti")
	}
	if repository.OtisakLozinke("") == "" || repository.OtisakLozinke("a") == repository.OtisakLozinke("b") {
		t.Error("otisak lozinke")
	}
}

// Prijava zabilježena na drugom računalu s istom lozinkom i verzija računa
// starijeg oblika, bez sažetka lozinke, ne gase sesiju
func TestSesijaOstajeUzVerzijuBezNoveLozinke(t *testing.T) {
	ctx := context.Background()
	a, b := noviCvor(t, "cvor-a"), noviCvor(t, "cvor-b")
	u, err := a.repo.GetUserByUsername("aanic")
	nuzno(t, err)
	hash, err := a.auth.HashPassword("ista-lozinka-2026")
	nuzno(t, err)
	nuzno(t, a.repo.ChangePassword(u.ID, hash))
	prenesiKorisnika(t, a, b, u.ID)

	naA, _, err := a.auth.Login("aanic", "ista-lozinka-2026", "192.168.1.10", "ured")
	nuzno(t, err)
	_, _, err = b.auth.Login("aanic", "ista-lozinka-2026", "198.51.100.7", "laptop") // B bilježi prijavu
	nuzno(t, err)
	prenesiKorisnika(t, b, a, u.ID)
	if !sesijaVrijedi(t, a, naA.ID) {
		t.Fatal("prijava zabilježena na drugom računalu ne smije ugasiti sesiju")
	}

	// verzija starijeg oblika: bez sažetka lozinke, novija od svih
	verzije, err := b.rec.History(ctx, repository.EntityUsers, u.ID.String())
	nuzno(t, err)
	v := verzije[len(verzije)-1]
	var polja map[string]any
	nuzno(t, json.Unmarshal(v.Payload, &polja))
	delete(polja, "password_hash")
	polja["full_name"] = "Ana Anić (stari program)"
	v.Payload, err = json.Marshal(polja)
	nuzno(t, err)
	id, err := uuid.NewV7()
	nuzno(t, err)
	v.VersionID, v.NodeID, v.Supersedes = id.String(), "cvor-stari", verzije[len(verzije)-1].VersionID
	stara := []ledger.Version{v}
	_, err = a.rec.Apply(ctx, stara)
	nuzno(t, err)
	nuzno(t, repository.ApplyVersions(ctx, a.db, a.rec, stara))
	if g, err := a.repo.GetUserByID(u.ID); err != nil || g.FullName != "Ana Anić (stari program)" {
		t.Fatalf("verzija starijeg oblika nije primijenjena: %+v %v", g, err)
	}
	if !sesijaVrijedi(t, a, naA.ID) {
		t.Error("verzija bez sažetka lozinke ne smije ugasiti sesiju")
	}
}

// Sesija pamti otisak lozinke koju je prijava provjerila, ne one koja je u
// bazi kad se sesija otvori: lozinka promijenjena u međuvremenu gasi je
func TestSesijaPamtiProvjerenuLozinku(t *testing.T) {
	n := noviCvor(t, "cvor-a")
	x, err := n.repo.GetUserByUsername("aanic")
	nuzno(t, err)
	hash, err := n.auth.HashPassword("prva-lozinka-2026")
	nuzno(t, err)
	nuzno(t, n.repo.ChangePassword(x.ID, hash))
	u, err := n.auth.ProvjeriPrijavu("aanic", "prva-lozinka-2026")
	nuzno(t, err)
	nova, err := n.auth.HashPassword("druga-lozinka-2026")
	nuzno(t, err)
	nuzno(t, n.repo.ResetPassword(u.ID, nova)) // npr. stigla s drugog računala
	ses, err := n.auth.OtvoriSesiju(u, "192.168.1.10", "ured")
	nuzno(t, err)
	if sesijaVrijedi(t, n, ses.ID) {
		t.Error("sesija otvorena uz lozinku koja više ne vrijedi")
	}
	svjez, err := n.repo.GetUserByID(u.ID)
	nuzno(t, err)
	ses, err = n.auth.OtvoriSesiju(svjez, "192.168.1.10", "ured")
	nuzno(t, err)
	if !sesijaVrijedi(t, n, ses.ID) {
		t.Error("sesija uz trenutnu lozinku mora vrijediti")
	}
}

// Prijava na čekanju (PIN izvana) vrijedi samo uz lozinku koju je
// provjerila: kad nova lozinka stigne s drugog računala, točan PIN javlja
// da je prijava istekla
func TestPrijavaNaCekanjuNakonNoveLozinke(t *testing.T) {
	ctx := context.Background()
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	a := &cvor{db: o.baza, rec: ledger.New(o.baza, "cvor-a"), repo: o.repo, auth: o.auth}
	b := noviCvor(t, "cvor-b")
	prenesiKorisnika(t, a, b, o.osoba.ID)

	// kontrola: bez nove lozinke PIN otvara prijavu
	p, err := o.dk.ZapocniPrijavu(ctx, o.osoba, "203.0.113.9", "Firefox")
	nuzno(t, err)
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, o.posta.pin(t)); err != nil {
		t.Fatalf("točan PIN: %v", err)
	}

	o.pomakni(2 * time.Minute)
	p, err = o.dk.ZapocniPrijavu(ctx, o.osoba, "203.0.113.9", "Firefox")
	nuzno(t, err)
	pin := o.posta.pin(t)
	nova, err := b.auth.HashPassword("nova-lozinka-2026")
	nuzno(t, err)
	nuzno(t, b.repo.ResetPassword(o.osoba.ID, nova))
	prenesiKorisnika(t, b, a, o.osoba.ID)
	if _, err := o.dk.ProvjeriKod(ctx, p.Token, pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Fatalf("PIN nakon nove lozinke s drugog računala: %v", err)
	}
}

func nuzno(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// korisniciBezRacuna je imenik u kojem računa prijave više nema
type korisniciBezRacuna struct{ *repository.UserRepository }

func (korisniciBezRacuna) GetUserByID(uuid.UUID) (*models.User, error) { return nil, nil }

// Točan PIN za račun kojeg više nema javlja da je prijava istekla
func TestPrijavaNaCekanjuBezRacuna(t *testing.T) {
	ctx := context.Background()
	o := okolinaDrugogKoraka(t)
	o.ukljuci(t)
	token, pin := o.novaPrijava(t)
	bez := service.NewDrugiKorak(o.dkRepo, repository.NewRacuniSustavaRepository(o.baza), korisniciBezRacuna{o.repo},
		repository.NewAktiRepository(o.baza, ledger.New(o.baza, "cvor-a")))
	bez.SetKljuc([]byte(sjemeTesta))
	bez.SetSat(func() time.Time { return o.sad })
	if _, err := bez.ProvjeriKod(ctx, token, pin); !errors.Is(err, service.ErrDrugiKorakIstekao) {
		t.Fatalf("PIN za račun kojeg nema: %v", err)
	}
}
