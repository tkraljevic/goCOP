package sadrzaj

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func otvoriNaDisku(t *testing.T) (*Spremiste, string) {
	t.Helper()
	put := filepath.Join(t.TempDir(), "sadrzaj.db")
	s, err := Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Zatvori() }) //nolint:errcheck // proba, zatvaranje nije predmet
	return s, put
}

// datotekaSadrzaja je datoteka sadržaja upisanog ovaj mjesec
func datotekaSadrzaja(t *testing.T, put, vrsta, otisak string) string {
	t.Helper()
	rel, err := noviPut(vrsta, otisak, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(MapaUz(put), filepath.FromSlash(rel))
}

// PDF je obična datoteka uz bazu, a baza drži samo popis: bez stupca s
// bajtovima. Brisanje miče i datoteku, a sadržaj kojemu je netko obrisao
// datoteku čvor više ne drži, pa ga opet traži.
func TestSadrzajJeDatotekaUzBazu(t *testing.T) {
	s, put := otvoriNaDisku(t)
	ctx := context.Background()
	pdf := []byte("%PDF-1.4 prijava s terena")
	otisak, err := s.Upisi(ctx, "application/pdf", pdf, "cvor:ured")
	if err != nil {
		t.Fatal(err)
	}
	na := datotekaSadrzaja(t, put, "application/pdf", otisak)
	if ocekivano := filepath.Join(MapaUz(put), "pdf", time.Now().UTC().Format("2006-01"), otisak+".pdf"); na != ocekivano {
		t.Errorf("mjesto %s, treba %s", na, ocekivano)
	}
	if b, err := os.ReadFile(na); err != nil || !bytes.Equal(b, pdf) {
		t.Fatalf("datoteka %s: %q, %v", na, b, err)
	}
	if ima, err := imaStupac(s.db, "sadrzaj", "podaci"); err != nil || ima {
		t.Errorf("stupac podaci u bazi: %v, %v", ima, err)
	}
	if b, vrsta, err := s.Citaj(ctx, otisak); err != nil || !bytes.Equal(b, pdf) || vrsta != "application/pdf" {
		t.Errorf("čitanje: %q %q %v", b, vrsta, err)
	}

	if err := os.Remove(na); err != nil {
		t.Fatal(err)
	}
	if s.Ima(ctx, otisak) {
		t.Error("bez datoteke čvor i dalje kaže da ima sadržaj")
	}
	if _, _, err := s.Citaj(ctx, otisak); !errors.Is(err, ErrNema) {
		t.Errorf("čitanje bez datoteke: %v, treba ErrNema", err)
	}
	if err := s.UpisiProvjereno(ctx, otisak, "application/pdf", pdf, "cvor:ured"); err != nil {
		t.Fatal(err)
	}
	if !s.Ima(ctx, otisak) {
		t.Error("ponovno primljen sadržaj nema datoteku")
	}

	if err := s.Otpusti(ctx, otisak); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(na); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("otpušten sadržaj ostavio je datoteku: %v", err)
	}
}

// Pospremanje paketa arhive miče datoteke paketa koji ne ostaju, a datoteka
// paketa koji ostaje ostaje
func TestPospremanjeMiceDatoteke(t *testing.T) {
	s, put := otvoriNaDisku(t)
	ctx := context.Background()
	stari, err := s.Upisi(ctx, "application/vnd.gocop.arhiva", []byte("paket 1"), "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	novi, err := s.Upisi(ctx, "application/vnd.gocop.arhiva", []byte("paket 2"), "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.PospremiVrstu(ctx, "application/vnd.gocop.arhiva", "arhiva", map[string]bool{novi: true})
	if err != nil || n != 1 {
		t.Fatalf("pospremljeno %d, %v; treba 1", n, err)
	}
	if _, err := os.Stat(datotekaSadrzaja(t, put, "application/vnd.gocop.arhiva", stari)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stari paket ostavio je datoteku: %v", err)
	}
	if _, err := os.Stat(datotekaSadrzaja(t, put, "application/vnd.gocop.arhiva", novi)); err != nil {
		t.Errorf("paket koji ostaje nema datoteku: %v", err)
	}
}

// Spremište iz vremena kad su bajtovi bili u bazi. Do seljenja se sadržaj
// čita iz baze i novi se može upisati; seljenje svaki sadržaj pretvori u
// datoteku, makne stupac i smanji bazu, a sadržaj se čita kao prije.
func TestSeljenjeIzBaze(t *testing.T) {
	put := filepath.Join(t.TempDir(), "sadrzaj.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	velik := bytes.Repeat([]byte("sken akta "), 100_000)
	otisak := Otisak(velik)
	for _, q := range []string{
		`CREATE TABLE sadrzaj (otisak TEXT PRIMARY KEY, vrsta TEXT NOT NULL, bajtova INTEGER NOT NULL,
			podaci BLOB NOT NULL, primljeno DATETIME NOT NULL, izvor TEXT NOT NULL DEFAULT 'ovdje', kanal TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE sadrzaj_zeljen (otisak TEXT PRIMARY KEY, vrsta TEXT NOT NULL, bajtova INTEGER NOT NULL,
			trazeno DATETIME, razlog TEXT NOT NULL, kanal TEXT NOT NULL DEFAULT '')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO sadrzaj (otisak, vrsta, bajtova, podaci, primljeno) VALUES (?, 'application/pdf', ?, ?, ?)`,
		otisak, len(velik), velik, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	prije, err := os.Stat(put)
	if err != nil {
		t.Fatal(err)
	}

	s, err := Otvori(put)
	if err != nil {
		t.Fatalf("staro spremište: %v", err)
	}
	t.Cleanup(func() { s.Zatvori() }) //nolint:errcheck // proba, zatvaranje nije predmet
	ctx := context.Background()
	if b, _, err := s.Citaj(ctx, otisak); err != nil || !bytes.Equal(b, velik) || !s.Ima(ctx, otisak) {
		t.Fatalf("prije seljenja: %d bajtova, %v", len(b), err)
	}
	nova := []byte("fotografija upisana prije seljenja")
	novi, err := s.Upisi(ctx, "image/jpeg", nova, "ovdje")
	if err != nil {
		t.Fatalf("upis u staro spremište: %v", err)
	}

	n, err := s.PreseliIzBaze(ctx)
	if err != nil || n != 1 {
		t.Fatalf("preseljeno %d, %v; treba 1", n, err)
	}
	if ima, err := imaStupac(s.db, "sadrzaj", "podaci"); err != nil || ima {
		t.Errorf("stupac podaci ostao je u bazi: %v, %v", ima, err)
	}
	for o, ocekivano := range map[string][]byte{otisak: velik, novi: nova} {
		if b, _, err := s.Citaj(ctx, o); err != nil || !bytes.Equal(b, ocekivano) {
			t.Errorf("nakon seljenja %s: %d bajtova, %v", o[:8], len(b), err)
		}
	}
	if _, err := os.Stat(datotekaSadrzaja(t, put, "application/pdf", otisak)); err != nil {
		t.Errorf("preseljeni PDF nije u mapi pdf/mjesec: %v", err)
	}
	poslije, err := os.Stat(put)
	if err != nil {
		t.Fatal(err)
	}
	if poslije.Size() >= prije.Size()/10 {
		t.Errorf("baza se nije smanjila: %d → %d bajtova", prije.Size(), poslije.Size())
	}
	if n, err := s.PreseliIzBaze(ctx); err != nil || n != 0 {
		t.Errorf("drugo seljenje: %d, %v", n, err)
	}
}

// Otisak stiže i s drugih čvorova, a od njega nastaje ime datoteke: ništa
// osim SHA-256 heksom ne smije postati ime, a mjesto ne smije izaći iz mape
// sadržaja. Svaka vrsta ima svoju mapu, a nepoznata ide u ostalo.
func TestOtisakNijePut(t *testing.T) {
	s, _ := otvoriNaDisku(t)
	for _, o := range []string{"", "../../gocop.db", "AB" + Otisak(nil)[2:], Otisak(nil) + "/x", "zz" + Otisak(nil)[2:]} {
		if ispravanOtisak(o) || otisakIzImena(o+".pdf") != "" {
			t.Errorf("%q prihvaćen kao otisak", o)
		}
		if _, err := noviPut("application/pdf", o, time.Now()); err == nil {
			t.Errorf("%q: mjesto bez greške", o)
		}
	}
	for _, rel := range []string{"", "../gocop.db", "/etc/passwd", "pdf/../../x"} {
		if err := s.dat.zapisi(rel, []byte("x")); err == nil {
			t.Errorf("%q: upis izvan mape bez greške", rel)
		}
		if s.dat.ima(rel) {
			t.Errorf("%q: ima datoteku", rel)
		}
		if _, err := s.dat.citaj(rel); !errors.Is(err, ErrNema) {
			t.Errorf("%q: čitanje %v", rel, err)
		}
		s.dat.obrisi(rel) // ne smije ništa obrisati ni pasti
	}
	o := Otisak([]byte("x"))
	studeni := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	for vrsta, ocekivano := range map[string]string{
		"application/pdf":              "pdf/2026-11/" + o + ".pdf",
		"image/jpeg":                   "slike/2026-11/" + o + ".jpg",
		"image/png":                    "slike/2026-11/" + o + ".png",
		"application/vnd.gocop.arhiva": "paketi-arhive/2026-11/" + o + ".cop",
		"text/plain":                   "ostalo/2026-11/" + o,
	} {
		if rel, err := noviPut(vrsta, o, studeni); err != nil || rel != ocekivano {
			t.Errorf("%s: %q, %v; treba %q", vrsta, rel, err, ocekivano)
		}
	}
}

// Datoteka bez retka (prekinut upis, neuspjelo brisanje na Windowsu) i
// ostatak privremene datoteke odlaze kad su stariji od sata; svježi višak i
// datoteka koju baza navodi ostaju
func TestPocistiDatoteke(t *testing.T) {
	s, put := otvoriNaDisku(t)
	ctx := context.Background()
	zivi, err := s.Upisi(ctx, "image/jpeg", []byte("fotografija"), "ovdje")
	if err != nil {
		t.Fatal(err)
	}
	staro := time.Now().Add(-2 * time.Hour)
	napravi := func(ime string, star bool) string {
		t.Helper()
		p := filepath.Join(MapaUz(put), "pdf", "2026-01", ime)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("višak"), 0o644); err != nil {
			t.Fatal(err)
		}
		if star {
			if err := os.Chtimes(p, staro, staro); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	sirotan := napravi(Otisak([]byte("siroce"))+".pdf", true)
	tudja := napravi("biljeska.txt", true)
	prekinut := napravi(".novo-123", true)
	svjez := napravi(Otisak([]byte("svjeze"))+".pdf", false)
	zivaDatoteka := datotekaSadrzaja(t, put, "image/jpeg", zivi)
	if err := os.Chtimes(zivaDatoteka, staro, staro); err != nil {
		t.Fatal(err)
	}

	n, err := s.PocistiDatoteke(ctx)
	if err != nil || n != 2 {
		t.Fatalf("počišćeno %d, %v; treba 2", n, err)
	}
	for _, p := range []string{sirotan, prekinut} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s ostao: %v", filepath.Base(p), err)
		}
	}
	for _, p := range []string{svjez, zivaDatoteka, tudja} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s obrisan: %v", filepath.Base(p), err)
		}
	}
}

// staroSpremiste otvara spremište sa stupcem podaci i u njemu zadane retke
// (otisak → bajtovi), kao prije seljenja
func staroSpremiste(t *testing.T, retci map[string][]byte) *Spremiste {
	t.Helper()
	put := filepath.Join(t.TempDir(), "sadrzaj.db")
	db, err := sql.Open("sqlite", put)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sadrzaj (otisak TEXT PRIMARY KEY, vrsta TEXT NOT NULL, bajtova INTEGER NOT NULL,
		podaci BLOB NOT NULL, primljeno DATETIME NOT NULL, izvor TEXT NOT NULL DEFAULT 'ovdje', kanal TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	for o, b := range retci {
		if _, err := db.Exec(`INSERT INTO sadrzaj (otisak, vrsta, bajtova, podaci, primljeno) VALUES (?, 'application/pdf', ?, ?, ?)`,
			o, len(b), b, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Otvori(put)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Zatvori() }) //nolint:errcheck // proba, zatvaranje nije predmet
	return s
}

// Seljenje preskače redak s otiskom koji ne može biti ime datoteke, sadržaj
// obrisan u međuvremenu i sadržaj upisan nanovo dok je seljenje trajalo (u
// stupcu su tada prazni bajtovi, a pravi su već u datoteci)
func TestSeljenjePreskace(t *testing.T) {
	ctx := context.Background()
	pdf := []byte("pravi PDF")
	s := staroSpremiste(t, map[string][]byte{"nije-otisak": []byte("x"), Otisak(pdf): pdf})
	if ok, err := s.preseliJedan(ctx, Otisak([]byte("nema ga"))); ok || err != nil {
		t.Errorf("obrisan: %v, %v", ok, err)
	}
	nova := []byte("upisano tijekom seljenja")
	if _, err := s.Upisi(ctx, "image/jpeg", nova, "ovdje"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.preseliJedan(ctx, Otisak(nova)); ok || err != nil {
		t.Errorf("upisan tijekom seljenja: %v, %v", ok, err)
	}
	if b, _, err := s.Citaj(ctx, Otisak(nova)); err != nil || !bytes.Equal(b, nova) {
		t.Errorf("prazan stupac pregazio je datoteku: %q, %v", b, err)
	}
	// isti PDF primljen opet prije seljenja: redak dobije mjesto, pa ga
	// seljenje preskoči
	if err := s.UpisiProvjereno(ctx, Otisak(pdf), "application/pdf", pdf, "cvor:ured"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PreseliIzBaze(ctx); err != nil || n != 0 {
		t.Errorf("preseljeno %d, %v; treba 0", n, err)
	}
	if b, err := os.ReadFile(datotekaSadrzaja(t, s.Put(), "application/pdf", Otisak(pdf))); err != nil || !bytes.Equal(b, pdf) {
		t.Errorf("datoteka ponovno primljenog PDF-a: %q, %v", b, err)
	}
}

// Greške baze pri seljenju javljaju se, a sadržaj ostaje u stupcu za
// sljedeći pokušaj
func TestSeljenjeGreske(t *testing.T) {
	ctx := context.Background()
	pdf := []byte("PDF")
	s := staroSpremiste(t, map[string][]byte{Otisak(pdf): pdf})
	if ok, err := s.preseliJedan(ctx, Otisak(pdf)); !ok || err != nil {
		t.Fatalf("jedan: %v, %v", ok, err)
	}
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreseliIzBaze(ctx); err == nil {
		t.Error("zatvorena baza: nema greške popisa")
	}
	if _, err := s.preseliJedan(ctx, Otisak(pdf)); err == nil {
		t.Error("zatvorena baza: nema greške čitanja")
	}
	if err := s.makniStupac(ctx); err == nil {
		t.Error("zatvorena baza: nema greške brisanja stupca")
	}
	if !s.navedeno(ctx, "pdf/2026-11/"+Otisak(pdf)+".pdf") {
		t.Error("kad se baza ne da pitati, datoteka mora ostati")
	}
	if s.Ima(ctx, Otisak(pdf)) {
		t.Error("kad se baza ne da pitati, sadržaja nema")
	}
	if _, err := s.mjesto(ctx, Otisak(pdf), "application/pdf", time.Now()); err == nil {
		t.Error("zatvorena baza: nema greške mjesta")
	}
	if _, err := s.Upisi(ctx, "application/pdf", []byte("novi PDF"), "ovdje"); err == nil {
		t.Error("zatvorena baza: upis bez greške")
	}
	if _, err := stupacUpita(ctx, s.db, `SELECT 1`); err == nil {
		t.Error("zatvorena baza: nema greške upita")
	}

	d, _ := otvoriNaDisku(t)
	if _, err := stupacUpita(ctx, d.db, `SELECT 'a', 'b'`); err == nil {
		t.Error("dva stupca: nema greške čitanja")
	}
	if err := d.makniStupac(ctx); err == nil {
		t.Error("spremište bez stupca podaci: nema greške")
	}
}

// Memorijsko spremište nema mapu pa nema što čistiti; mapa koje još nema
// nije greška, a nečitljiva mapa jest
func TestPocistiBezMape(t *testing.T) {
	ctx := context.Background()
	m, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Zatvori() //nolint:errcheck // proba, zatvaranje nije predmet
	if n, err := m.PocistiDatoteke(ctx); n != 0 || err != nil {
		t.Errorf("memorija: %d, %v", n, err)
	}
	s, put := otvoriNaDisku(t)
	if n, err := s.PocistiDatoteke(ctx); n != 0 || err != nil {
		t.Errorf("mapa još ne postoji: %d, %v", n, err)
	}
	zakljucana := filepath.Join(MapaUz(put), "cd")
	if err := os.MkdirAll(zakljucana, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(zakljucana, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(zakljucana, 0o755) }) //nolint:errcheck // samo da TempDir može obrisati mapu
	if _, err := s.PocistiDatoteke(ctx); err == nil {
		t.Error("nečitljiva mapa: nema greške")
	}
}

// Kad se datoteka ne da zapisati, ni redak se ne upiše: popis ne smije
// navoditi sadržaj kojeg nema. Neuspjela potvrda brisanja ne miče datoteke.
func TestUpisBezDatotekeNemaRetka(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sadrzaj"), []byte("datoteka umjesto mape"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Otvori(filepath.Join(dir, "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Zatvori() }) //nolint:errcheck // proba, zatvaranje nije predmet
	pdf := []byte("PDF bez mjesta")
	if _, err := s.Upisi(ctx, "application/pdf", pdf, "ovdje"); err == nil {
		t.Fatal("upis bez mape: nema greške")
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM sadrzaj`).Scan(&n); err != nil || n != 0 {
		t.Errorf("redak upisan bez datoteke: %d, %v", n, err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.potvrdiPaUkloni(ctx, tx, []string{Otisak(pdf)}); err == nil {
		t.Error("potvrda već potvrđene transakcije: nema greške")
	}
}
