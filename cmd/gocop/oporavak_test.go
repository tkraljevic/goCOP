package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"

	"golang.org/x/crypto/bcrypt"
)

// bazaZaOporavak je baza čvora s globalnim administratorom tkraljevic
func bazaZaOporavak(t *testing.T) string {
	t.Helper()
	put := filepath.Join(t.TempDir(), "gocop.db")
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("stara-lozinka"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	u := &models.User{Username: "tkraljevic", PasswordHash: string(hash), FullName: "Glavni Administrator",
		IsGlobalAdmin: true, IsActive: true, OrgType: models.OrgHrvatskeVode}
	if err := repository.NewUserRepository(baza, ledger.New(baza, "cvor")).CreateUser(u, nil); err != nil {
		t.Fatal(err)
	}
	return put
}

// dnevnikTesta hvata dnevnik naredbe dok test traje
func dnevnikTesta(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	stari := log.Writer()
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(stari) })
	return &b
}

// Naredba ispiše privremenu lozinku jednom, na izlaz, a u dnevnik samo trag
// bez lozinke; lozinka vrijedi za račun u bazi.
func TestNaredbaPonistiLozinku(t *testing.T) {
	put := bazaZaOporavak(t)
	dnevnik := dnevnikTesta(t)
	var izlaz bytes.Buffer
	if kod := ponistiLozinkuSKonzole(put, "cop-osijek-unraid", "TKraljevic", false, &izlaz); kod != 0 {
		t.Fatalf("izlazni kod %d, dnevnik: %s", kod, dnevnik)
	}
	m := regexp.MustCompile(`[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}`).FindAllString(izlaz.String(), -1)
	if len(m) != 1 {
		t.Fatalf("lozinka mora biti ispisana točno jednom: %q", izlaz.String())
	}
	lozinka := m[0]
	if strings.Contains(dnevnik.String(), lozinka) {
		t.Errorf("lozinka je u dnevniku: %s", dnevnik)
	}
	if !strings.Contains(dnevnik.String(), "tkraljevic") || !strings.Contains(dnevnik.String(), "konzole") {
		t.Errorf("dnevnik ne bilježi poništenje: %q", dnevnik)
	}
	if strings.Contains(izlaz.String(), "UPOZORENJE") || strings.Contains(izlaz.String(), "nije globalni") {
		t.Errorf("aktivan globalni administrator dobio je upozorenje: %s", izlaz.String())
	}
	if strings.Contains(izlaz.String(), "potpisni ključ") {
		t.Errorf("račun bez potpisnog ključa: %s", izlaz.String())
	}

	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	u, err := repository.NewUserRepository(baza, ledger.New(baza, "cvor")).GetUserByUsername("tkraljevic")
	if err != nil || u == nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(lozinka)) != nil || !u.MustChangePassword {
		t.Errorf("u bazi nije ispisana lozinka ili račun nije zaključan na promjenu: %+v", u)
	}
}

// Kriva putanja ne stvara praznu bazu, a nepostojeće ime ne ispisuje ništa;
// obje su izlazni kod različit od nule.
func TestNaredbaPonistiLozinkuGreske(t *testing.T) {
	dnevnik := dnevnikTesta(t)
	var izlaz bytes.Buffer
	nema := filepath.Join(t.TempDir(), "data", "gocop.db")
	if kod := ponistiLozinkuSKonzole(nema, "cvor", "tkraljevic", false, &izlaz); kod == 0 {
		t.Error("nepostojeća baza: izlazni kod 0")
	}
	if _, err := os.Stat(filepath.Dir(nema)); !os.IsNotExist(err) {
		t.Errorf("naredba je stvorila mapu ili bazu: %v", err)
	}

	put := bazaZaOporavak(t)
	if kod := ponistiLozinkuSKonzole(put, "cvor", "nepostojeci", false, &izlaz); kod == 0 {
		t.Error("nepostojeće ime: izlazni kod 0")
	}
	if izlaz.Len() != 0 {
		t.Errorf("greška je ispisala lozinku: %q", izlaz.String())
	}
	if !strings.Contains(dnevnik.String(), "nepostojeci") {
		t.Errorf("greška ne kaže koje ime: %q", dnevnik)
	}
}

// Baza starijeg izdanja (tablice postoje, najnoviji stupci ne) ne mijenja se
// i greška kaže da treba najprije pokrenuti čvor novim izdanjem
func TestNaredbaPonistiLozinkuStarijaBaza(t *testing.T) {
	dnevnik := dnevnikTesta(t)
	put := bazaZaOporavak(t)
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{"pin_adresa_potvrdena", "pin_adresa_potvrdio", "pin_adresa_potvrdena_kad"} {
		if _, err := baza.Exec(`ALTER TABLE users DROP COLUMN ` + st); err != nil {
			t.Fatal(err)
		}
	}
	var prije string
	if err := baza.QueryRow(`SELECT password_hash FROM users WHERE username = 'tkraljevic'`).Scan(&prije); err != nil {
		t.Fatal(err)
	}
	baza.Close()

	var izlaz bytes.Buffer
	if kod := ponistiLozinkuSKonzole(put, "cvor", "tkraljevic", false, &izlaz); kod == 0 {
		t.Fatal("baza starijeg izdanja: izlazni kod 0")
	}
	if !strings.Contains(dnevnik.String(), "starije izdanje") || izlaz.Len() != 0 {
		t.Errorf("greška ne kaže da je baza starijeg izdanja: %q (izlaz %q)", dnevnik, izlaz.String())
	}
	baza, err = db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	var poslije string
	if err := baza.QueryRow(`SELECT password_hash FROM users WHERE username = 'tkraljevic'`).Scan(&poslije); err != nil {
		t.Fatal(err)
	}
	if poslije != prije {
		t.Error("lozinka je promijenjena u bazi starijeg izdanja")
	}
}

// lozinkaIzIspisa vraća jedinu privremenu lozinku iz ispisa naredbe
func lozinkaIzIspisa(t *testing.T, izlaz string) string {
	t.Helper()
	m := regexp.MustCompile(`[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}`).FindAllString(izlaz, -1)
	if len(m) != 1 {
		t.Fatalf("lozinka mora biti ispisana točno jednom: %q", izlaz)
	}
	return m[0]
}

// Potpisni ključ zaključan starom lozinkom naredba uklanja i to kaže, da
// osoba zna napraviti novi u profilu.
func TestNaredbaPonistiLozinkuPotpisniKljuc(t *testing.T) {
	ctx := context.Background()
	put := bazaZaOporavak(t)
	dnevnikTesta(t)
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	rec := ledger.New(baza, "cvor")
	u, err := repository.NewUserRepository(baza, rec).GetUserByUsername("tkraljevic")
	if err != nil || u == nil {
		t.Fatal(err)
	}
	potpisi := repository.NewPotpisRepository(baza, rec)
	if err := potpisi.SaveKljuc(ctx, &models.PotpisniKljuc{UserID: u.ID.String(), Ime: u.FullName,
		Cert: []byte("cert"), Kljuc: []byte("kljuc"), Sol: []byte("sol"), Izdao: "cvor"}); err != nil {
		t.Fatal(err)
	}

	var izlaz bytes.Buffer
	if kod := ponistiLozinkuSKonzole(put, "cvor", "tkraljevic", false, &izlaz); kod != 0 {
		t.Fatalf("izlazni kod %d", kod)
	}
	lozinkaIzIspisa(t, izlaz.String())
	if !strings.Contains(izlaz.String(), "potpisni ključ") || !strings.Contains(izlaz.String(), "uklonjen") {
		t.Errorf("ispis ne kaže da je potpisni ključ uklonjen: %s", izlaz.String())
	}
	if k, err := potpisi.GetKljuc(ctx, u.ID.String()); err != nil || k != nil {
		t.Errorf("ključ ostao: %+v %v", k, err)
	}
}

// Ne uspije li uz -aktiviraj uključenje nakon poništene lozinke, naredba
// ispiše lozinku i upozori da je račun i dalje isključen; izlazni kod nije 0.
func TestNaredbaPonistiLozinkuUkljucenjeNeUspije(t *testing.T) {
	put := bazaZaOporavak(t)
	dnevnik := dnevnikTesta(t)
	baza, err := db.OpenDB(put)
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	hash, err := bcrypt.GenerateFromPassword([]byte("lozinka-ane"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	ana := &models.User{Username: "ana", PasswordHash: string(hash), FullName: "Ana", OrgType: models.OrgHrvatskeVode}
	if err := repository.NewUserRepository(baza, ledger.New(baza, "cvor")).CreateUser(ana, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := baza.Exec(`CREATE TRIGGER proba_ukljucenje BEFORE UPDATE OF is_active ON users
		WHEN NEW.is_active = 1 BEGIN SELECT RAISE(ABORT, 'disk je pun'); END`); err != nil {
		t.Fatal(err)
	}

	var izlaz bytes.Buffer
	if kod := ponistiLozinkuSKonzole(put, "cvor", "ana", true, &izlaz); kod == 0 {
		t.Error("neuspjelo uključenje: izlazni kod 0")
	}
	lozinkaIzIspisa(t, izlaz.String())
	if !strings.Contains(izlaz.String(), "UPOZORENJE: račun je isključen") || strings.Contains(izlaz.String(), "sada je uključen") {
		t.Errorf("ispis ne kaže da je račun ostao isključen: %s", izlaz.String())
	}
	if !strings.Contains(dnevnik.String(), "nije uključen") {
		t.Errorf("dnevnik ne kaže da račun nije uključen: %q", dnevnik)
	}
}

// Uključenje s konzole uz adresu koju ima drugi aktivni račun ispiše
// upozorenje s uputom
func TestIspisUpozoravaNaZauzetuAdresu(t *testing.T) {
	var w bytes.Buffer
	u := &models.User{Username: "ana2", FullName: "Ana Druga", IsActive: true}
	ispisiPonistenje(&w, &service.PonistenjeSKonzole{Korisnik: u, Lozinka: "privremena", Aktiviran: true, AdresaZauzeta: "ana@voda.hr"}, "cvor")
	if !strings.Contains(w.String(), "UPOZORENJE: adresu e-pošte ana@voda.hr ima i drugi aktivni račun") {
		t.Errorf("ispis ne upozorava na zauzetu adresu:\n%s", w.String())
	}
	w.Reset()
	ispisiPonistenje(&w, &service.PonistenjeSKonzole{Korisnik: u, Lozinka: "privremena", Aktiviran: true}, "cvor")
	if strings.Contains(w.String(), "drugi aktivni račun") {
		t.Errorf("ispis upozorava bez zauzete adrese:\n%s", w.String())
	}
}
