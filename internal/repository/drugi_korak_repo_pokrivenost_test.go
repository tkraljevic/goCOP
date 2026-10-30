package repository

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
)

// Drugi korak prijave izvana na razini baze: prijave na čekanju, zapamćena
// računala i kodovi. Vrijeme je uvijek zadano, servis ga ovdje ne daje.

var sadDrugogKoraka = time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)

func bazaDrugogKoraka(t *testing.T) (*sql.DB, *DrugiKorakRepository) {
	t.Helper()
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "drugi.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	return baza, NewDrugiKorakRepository(baza)
}

// osobaDrugogKoraka upisuje račun s adresom; aktivan određuje is_active
func osobaDrugogKoraka(t *testing.T, baza *sql.DB, korisnik, adresa string, aktivan bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := baza.Exec(`INSERT INTO users (id, username, password_hash, full_name, org_type, email, is_active, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, id.String(), korisnik, "x", "Probna "+korisnik, "HRVATSKE_VODE", adresa, aktivan,
		sadDrugogKoraka, sadDrugogKoraka); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPrijavaNaCekanjuUpisCitanjeIPokusaji(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ana@primjer.hr", true)

	if p, err := repo.Prijava(ctx, []byte("nema")); err != nil || p != nil {
		t.Fatalf("nepostojeća prijava: %v, %v", p, err)
	}

	stara := &PrijavaNaCekanju{ID: []byte("stara"), UserID: ana, ExpiresAt: sadDrugogKoraka.Add(10 * time.Minute)}
	nuzno(t, repo.ZapocniPrijavu(ctx, stara))
	if stara.CreatedAt.IsZero() {
		t.Error("vrijeme nastanka nije popunjeno")
	}
	slanje := sadDrugogKoraka.Add(-time.Minute)
	nova := &PrijavaNaCekanju{ID: []byte("nova"), UserID: ana, PinHash: []byte("pin"), PoslanoNa: "a**@primjer.hr",
		IPAddress: "10.0.0.1", UserAgent: "Preglednik", PosljednjeSlanje: &slanje,
		ExpiresAt: sadDrugogKoraka.Add(10 * time.Minute), CreatedAt: sadDrugogKoraka, Razlog: "zajednicka"}
	nuzno(t, repo.ZapocniPrijavu(ctx, nova))

	// nova prijava iste osobe briše staru
	if p, err := repo.Prijava(ctx, []byte("stara")); err != nil || p != nil {
		t.Fatalf("stara prijava je ostala: %v, %v", p, err)
	}
	p, err := repo.Prijava(ctx, []byte("nova"))
	nuzno(t, err)
	if p == nil || p.UserID != ana || !bytes.Equal(p.PinHash, []byte("pin")) || p.PoslanoNa != "a**@primjer.hr" ||
		p.IPAddress != "10.0.0.1" || p.UserAgent != "Preglednik" || p.Razlog != "zajednicka" {
		t.Fatalf("pročitana prijava: %+v", p)
	}
	if p.PosljednjeSlanje == nil || !p.PosljednjeSlanje.Equal(slanje) || !p.ExpiresAt.Equal(nova.ExpiresAt) || !p.CreatedAt.Equal(sadDrugogKoraka) {
		t.Errorf("vremena prijave: slanje %v, istek %v, nastanak %v", p.PosljednjeSlanje, p.ExpiresAt, p.CreatedAt)
	}

	// tri pokušaja pa kraj; redni broj raste
	for i := 1; i <= 3; i++ {
		n, ok, err := repo.RezervirajPokusaj(ctx, []byte("nova"), 3)
		nuzno(t, err)
		if !ok || n != i {
			t.Fatalf("%d. pokušaj: %d, %v", i, n, ok)
		}
	}
	if n, ok, err := repo.RezervirajPokusaj(ctx, []byte("nova"), 3); err != nil || ok || n != 0 {
		t.Errorf("pokušaj preko granice: %d, %v, %v", n, ok, err)
	}
	if _, ok, err := repo.RezervirajPokusaj(ctx, []byte("nema"), 3); err != nil || ok {
		t.Errorf("pokušaj bez prijave: %v, %v", ok, err)
	}
}

func TestPrijavaNaCekanjuBezPINaIPonovnoSlanje(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ana@primjer.hr", true)
	nuzno(t, repo.ZapocniPrijavu(ctx, &PrijavaNaCekanju{ID: []byte("p"), UserID: ana, PinHash: []byte{},
		ExpiresAt: sadDrugogKoraka.Add(time.Hour), CreatedAt: sadDrugogKoraka}))

	p, err := repo.Prijava(ctx, []byte("p"))
	nuzno(t, err)
	if p.PinHash != nil || p.PosljednjeSlanje != nil {
		t.Errorf("bez PIN-a: sažetak %v, slanje %v", p.PinHash, p.PosljednjeSlanje)
	}

	nuzno(t, repo.PostaviRazlog(ctx, []byte("p"), "bez-adrese"))
	p, _ = repo.Prijava(ctx, []byte("p"))
	if p.Razlog != "bez-adrese" {
		t.Errorf("razlog: %q", p.Razlog)
	}

	kad := sadDrugogKoraka.Add(2 * time.Minute)
	nuzno(t, repo.PostaviPIN(ctx, []byte("p"), []byte("novi-pin"), "a**@primjer.hr", kad))
	p, _ = repo.Prijava(ctx, []byte("p"))
	if !bytes.Equal(p.PinHash, []byte("novi-pin")) || p.PoslanoNa != "a**@primjer.hr" || p.Razlog != "" ||
		p.PosljednjeSlanje == nil || !p.PosljednjeSlanje.Equal(kad) {
		t.Errorf("nakon ponovnog slanja: %+v", p)
	}
}

func TestBrisanjePrijavaNaCekanju(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ana@primjer.hr", true)
	ivo := osobaDrugogKoraka(t, baza, "ivo", "ivo@primjer.hr", true)
	istek := sadDrugogKoraka.Add(time.Hour)
	nuzno(t, repo.ZapocniPrijavu(ctx, &PrijavaNaCekanju{ID: []byte("a"), UserID: ana, ExpiresAt: istek}))
	nuzno(t, repo.ZapocniPrijavu(ctx, &PrijavaNaCekanju{ID: []byte("i"), UserID: ivo, ExpiresAt: istek}))

	// od dva brisanja iste prijave samo prvo javlja da ju je bilo
	if ok, err := repo.ObrisiPrijavu(ctx, []byte("a")); err != nil || !ok {
		t.Fatalf("prvo brisanje: %v, %v", ok, err)
	}
	if ok, err := repo.ObrisiPrijavu(ctx, []byte("a")); err != nil || ok {
		t.Errorf("drugo brisanje: %v, %v", ok, err)
	}

	nuzno(t, repo.ObrisiPrijaveKorisnika(ctx, ivo))
	if p, _ := repo.Prijava(ctx, []byte("i")); p != nil {
		t.Error("prijava osobe nije obrisana")
	}
}

func TestZapamcenaRacunala(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ana@primjer.hr", true)
	ivo := osobaDrugogKoraka(t, baza, "ivo", "ivo@primjer.hr", true)

	if x, err := repo.Racunalo(ctx, []byte("t1"), ana); err != nil || x != nil {
		t.Fatalf("nepostojeće računalo: %v, %v", x, err)
	}

	prvo := &ZapamcenoRacunalo{UserID: ana, TokenHash: []byte("t1"), LozinkaOtisak: []byte("o1"), UserAgent: "Ured",
		IPPrvi: "10.0.0.1", IPZadnji: "10.0.0.1", CreatedAt: sadDrugogKoraka, ExpiresAt: sadDrugogKoraka.Add(30 * 24 * time.Hour)}
	nuzno(t, repo.SpremiRacunalo(ctx, prvo))
	if prvo.ID == "" {
		t.Fatal("računalo nije dobilo oznaku")
	}
	// isti token iste osobe zamjenjuje stari redak
	zamjena := &ZapamcenoRacunalo{UserID: ana, TokenHash: []byte("t1"), LozinkaOtisak: []byte("o2"), UserAgent: "Ured",
		CreatedAt: sadDrugogKoraka.Add(time.Hour), ExpiresAt: sadDrugogKoraka.Add(60 * 24 * time.Hour)}
	nuzno(t, repo.SpremiRacunalo(ctx, zamjena))
	kuca := &ZapamcenoRacunalo{UserID: ana, TokenHash: []byte("t2"), LozinkaOtisak: []byte("o2"), UserAgent: "Kuća",
		CreatedAt: sadDrugogKoraka.Add(2 * time.Hour), ExpiresAt: sadDrugogKoraka.Add(30 * 24 * time.Hour)}
	nuzno(t, repo.SpremiRacunalo(ctx, kuca))
	// isti token na zajedničkom računalu vrijedi i za drugu osobu
	ivino := &ZapamcenoRacunalo{UserID: ivo, TokenHash: []byte("t1"), LozinkaOtisak: []byte("o3")}
	nuzno(t, repo.SpremiRacunalo(ctx, ivino))
	if ivino.CreatedAt.IsZero() {
		t.Error("vrijeme pamćenja nije popunjeno")
	}

	x, err := repo.Racunalo(ctx, []byte("t1"), ana)
	nuzno(t, err)
	if x == nil || x.ID != zamjena.ID || !bytes.Equal(x.LozinkaOtisak, []byte("o2")) || x.LastUsedAt != nil ||
		!x.ExpiresAt.Equal(zamjena.ExpiresAt) || x.UserID != ana {
		t.Fatalf("zamijenjeno računalo: %+v", x)
	}

	kad := sadDrugogKoraka.Add(3 * time.Hour)
	nuzno(t, repo.OznaciKoristenje(ctx, x.ID, "10.0.0.9", kad))
	x, _ = repo.Racunalo(ctx, []byte("t1"), ana)
	if x.LastUsedAt == nil || !x.LastUsedAt.Equal(kad) || x.IPZadnji != "10.0.0.9" || !x.ExpiresAt.Equal(zamjena.ExpiresAt) {
		t.Errorf("korištenje: zadnje %v, ip %q, rok %v", x.LastUsedAt, x.IPZadnji, x.ExpiresAt)
	}

	sva, err := repo.Racunala(ctx, ana)
	nuzno(t, err)
	if len(sva) != 2 || sva[0].UserAgent != "Kuća" || sva[1].UserAgent != "Ured" {
		t.Fatalf("računala osobe, najnovije prvo: %+v", sva)
	}

	// tuđe računalo se ne briše
	if ok, err := repo.ObrisiRacunalo(ctx, ana, ivino.ID); err != nil || ok {
		t.Errorf("brisanje tuđeg: %v, %v", ok, err)
	}
	if ok, err := repo.ObrisiRacunalo(ctx, ana, kuca.ID); err != nil || !ok {
		t.Errorf("brisanje svojeg: %v, %v", ok, err)
	}
	nuzno(t, repo.ObrisiRacunaloPoTokenu(ctx, ivo, []byte("t1")))
	if x, _ := repo.Racunalo(ctx, []byte("t1"), ivo); x != nil {
		t.Error("računalo po tokenu nije zaboravljeno")
	}
	if x, _ := repo.Racunalo(ctx, []byte("t1"), ana); x == nil {
		t.Error("brisanje po tokenu jedne osobe odnijelo je i drugu")
	}

	nuzno(t, repo.SpremiRacunalo(ctx, &ZapamcenoRacunalo{UserID: ana, TokenHash: []byte("t3"), LozinkaOtisak: []byte("o")}))
	if n, err := repo.ObrisiRacunalaKorisnika(ctx, ana); err != nil || n != 2 {
		t.Errorf("sva računala osobe: %d, %v", n, err)
	}
	if sva, _ := repo.Racunala(ctx, ana); len(sva) != 0 {
		t.Errorf("ostala računala: %d", len(sva))
	}
}

func TestRezervniKodovi(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ana@primjer.hr", true)

	if n, kad, err := repo.StanjeRezervnih(ctx, ana); err != nil || n != 0 || kad != nil {
		t.Fatalf("bez niza: %d, %v, %v", n, kad, err)
	}

	stari := []KodPrijave{{Oznaka: "r1", KodHash: []byte("s1"), CreatedAt: sadDrugogKoraka}}
	nuzno(t, repo.ZamijeniRezervne(ctx, ana, stari))
	novi := []KodPrijave{
		{Oznaka: "r1", KodHash: []byte("n1"), CreatedAt: sadDrugogKoraka.Add(time.Hour)},
		{Oznaka: "r2", KodHash: []byte("n2"), CreatedAt: sadDrugogKoraka.Add(time.Hour)},
		{Oznaka: "r3", KodHash: []byte("n3"), CreatedAt: sadDrugogKoraka.Add(2 * time.Hour)},
	}
	nuzno(t, repo.ZamijeniRezervne(ctx, ana, novi))
	if novi[0].UserID != ana || novi[0].Vrsta != KodRezervni || novi[2].ID == "" {
		t.Errorf("popunjeni kod: %+v", novi[2])
	}

	// oznaka se traži bez obzira na velika slova; stari niz je nestao
	k, err := repo.Kod(ctx, ana, "r1")
	nuzno(t, err)
	if k == nil || !bytes.Equal(k.KodHash, []byte("n1")) || k.Oznaka != "R1" || k.Vrsta != KodRezervni || k.UserID != ana ||
		k.ExpiresAt != nil || k.UsedAt != nil {
		t.Fatalf("kod R1: %+v", k)
	}

	if ok, err := repo.IskoristiKod(ctx, k.ID, sadDrugogKoraka.Add(2*time.Hour)); err != nil || !ok {
		t.Fatalf("prvo korištenje: %v, %v", ok, err)
	}
	if ok, err := repo.IskoristiKod(ctx, k.ID, sadDrugogKoraka.Add(3*time.Hour)); err != nil || ok {
		t.Errorf("drugo korištenje istog koda: %v, %v", ok, err)
	}
	if k, _ := repo.Kod(ctx, ana, "R1"); k != nil {
		t.Errorf("iskorišteni kod se još nudi: %+v", k)
	}

	n, kad, err := repo.StanjeRezervnih(ctx, ana)
	nuzno(t, err)
	// vrijeme niza je vrijeme najnovijeg koda
	if n != 2 || kad == nil || !kad.Equal(sadDrugogKoraka.Add(2*time.Hour)) {
		t.Errorf("stanje niza: ostalo %d, napravljen %v", n, kad)
	}
}

// Kod bez zadanog vremena nastanka dobije ga pri upisu.
func TestRezervniKodBezVremenaNastanka(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ana@primjer.hr", true)
	kodovi := []KodPrijave{{Oznaka: "R1", KodHash: []byte("r")}}
	nuzno(t, repo.ZamijeniRezervne(ctx, ana, kodovi))
	if kodovi[0].CreatedAt.IsZero() || kodovi[0].ID == "" {
		t.Fatalf("popunjeni kod: %+v", kodovi[0])
	}
	if n, kad, err := repo.StanjeRezervnih(ctx, ana); err != nil || n != 1 || kad == nil || !kad.Equal(kodovi[0].CreatedAt) {
		t.Errorf("stanje niza: %d, %v, %v", n, kad, err)
	}
}

func TestPrivremeniKodVrijediSamoZadnji(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ana@primjer.hr", true)
	nuzno(t, repo.ZamijeniRezervne(ctx, ana, []KodPrijave{{Oznaka: "R1", KodHash: []byte("r"), CreatedAt: sadDrugogKoraka}}))

	istek := sadDrugogKoraka.Add(24 * time.Hour)
	prvi := &KodPrijave{UserID: ana, Oznaka: "p", KodHash: []byte("p1"), Izdao: "admin", CreatedAt: sadDrugogKoraka, ExpiresAt: &istek}
	nuzno(t, repo.DodajPrivremeni(ctx, prvi))
	drugi := &KodPrijave{UserID: ana, Oznaka: "p", KodHash: []byte("p2"), Izdao: "admin", CreatedAt: sadDrugogKoraka.Add(time.Minute), ExpiresAt: &istek}
	nuzno(t, repo.DodajPrivremeni(ctx, drugi))
	if drugi.Vrsta != KodPrivremeni {
		t.Errorf("vrsta: %q", drugi.Vrsta)
	}

	k, err := repo.Kod(ctx, ana, "P")
	nuzno(t, err)
	if k == nil || !bytes.Equal(k.KodHash, []byte("p2")) || k.Izdao != "admin" || k.ExpiresAt == nil || !k.ExpiresAt.Equal(istek) {
		t.Fatalf("privremeni kod: %+v", k)
	}

	nuzno(t, repo.ObrisiPrivremene(ctx, ana))
	if k, _ := repo.Kod(ctx, ana, "P"); k != nil {
		t.Error("privremeni kod nije obrisan")
	}
	if n, _, _ := repo.StanjeRezervnih(ctx, ana); n != 1 {
		t.Errorf("brisanje privremenih odnijelo je rezervne: ostalo %d", n)
	}
}

func TestAktivnihSAdresom(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ured@primjer.hr", true)
	osobaDrugogKoraka(t, baza, "ivo", "  URED@primjer.hr ", true)
	osobaDrugogKoraka(t, baza, "eva", "ured@primjer.hr", false)
	osobaDrugogKoraka(t, baza, "luka", "luka@primjer.hr", true)

	// sebe ne broji, neaktivne ne broji, velika slova i razmaci ne smetaju
	if n, err := repo.AktivnihSAdresom(ctx, "Ured@Primjer.hr", ana); err != nil || n != 1 {
		t.Errorf("drugih s adresom: %d, %v", n, err)
	}
	if n, err := repo.AktivnihSAdresom(ctx, "nitko@primjer.hr", ana); err != nil || n != 0 {
		t.Errorf("nepoznata adresa: %d, %v", n, err)
	}
}

func TestOcistiIstekleDrugogKoraka(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ana@primjer.hr", true)
	ivo := osobaDrugogKoraka(t, baza, "ivo", "ivo@primjer.hr", true)
	eva := osobaDrugogKoraka(t, baza, "eva", "eva@primjer.hr", true)
	prije, poslije := sadDrugogKoraka.Add(-time.Hour), sadDrugogKoraka.Add(time.Hour)
	naRubu := sadDrugogKoraka

	nuzno(t, repo.ZapocniPrijavu(ctx, &PrijavaNaCekanju{ID: []byte("istekla"), UserID: ana, ExpiresAt: prije}))
	nuzno(t, repo.ZapocniPrijavu(ctx, &PrijavaNaCekanju{ID: []byte("vrijedi"), UserID: ivo, ExpiresAt: poslije}))
	nuzno(t, repo.SpremiRacunalo(ctx, &ZapamcenoRacunalo{UserID: ana, TokenHash: []byte("a"), LozinkaOtisak: []byte("o"), ExpiresAt: prije}))
	nuzno(t, repo.SpremiRacunalo(ctx, &ZapamcenoRacunalo{UserID: ana, TokenHash: []byte("b"), LozinkaOtisak: []byte("o"), ExpiresAt: poslije}))
	nuzno(t, repo.DodajPrivremeni(ctx, &KodPrijave{UserID: ana, Oznaka: "P", KodHash: []byte("p"), ExpiresAt: &prije}))
	nuzno(t, repo.DodajPrivremeni(ctx, &KodPrijave{UserID: ivo, Oznaka: "P", KodHash: []byte("p"), ExpiresAt: &poslije}))
	// rezervni kod s rokom u prošlosti ostaje: briše ga samo novi niz
	nuzno(t, repo.ZamijeniRezervne(ctx, ana, []KodPrijave{{Oznaka: "R1", KodHash: []byte("r"), ExpiresAt: &prije}}))
	// rok koji istječe baš u trenutku čišćenja više ne vrijedi
	nuzno(t, repo.ZapocniPrijavu(ctx, &PrijavaNaCekanju{ID: []byte("na-rubu"), UserID: eva, ExpiresAt: naRubu}))
	nuzno(t, repo.SpremiRacunalo(ctx, &ZapamcenoRacunalo{UserID: ivo, TokenHash: []byte("c"), LozinkaOtisak: []byte("o"), ExpiresAt: naRubu}))
	nuzno(t, repo.DodajPrivremeni(ctx, &KodPrijave{UserID: eva, Oznaka: "P", KodHash: []byte("p"), ExpiresAt: &naRubu}))

	n, err := repo.OcistiIstekle(ctx, sadDrugogKoraka)
	nuzno(t, err)
	if n != 6 {
		t.Errorf("obrisano %d, očekivano 6", n)
	}
	if p, _ := repo.Prijava(ctx, []byte("na-rubu")); p != nil {
		t.Error("ostala je prijava kojoj je rok istekao u trenutku čišćenja")
	}
	if sva, _ := repo.Racunala(ctx, ivo); len(sva) != 0 {
		t.Errorf("ostalo je računalo kojem je rok istekao u trenutku čišćenja: %+v", sva)
	}
	if k, _ := repo.Kod(ctx, eva, "P"); k != nil {
		t.Errorf("ostao je privremeni kod kojem je rok istekao u trenutku čišćenja: %+v", k)
	}
	if p, _ := repo.Prijava(ctx, []byte("vrijedi")); p == nil {
		t.Error("obrisana je prijava koja vrijedi")
	}
	if sva, _ := repo.Racunala(ctx, ana); len(sva) != 1 || string(sva[0].TokenHash) != "b" {
		t.Errorf("ostala računala: %+v", sva)
	}
	if k, _ := repo.Kod(ctx, ivo, "P"); k == nil {
		t.Error("obrisan je privremeni kod koji vrijedi")
	}
	if k, _ := repo.Kod(ctx, ana, "R1"); k == nil {
		t.Error("obrisan je rezervni kod")
	}
}

func TestDrugiKorakZatvorenaBaza(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	id := uuid.New()
	baza.Close()

	if err := repo.ZapocniPrijavu(ctx, &PrijavaNaCekanju{ID: []byte("p"), UserID: id}); err == nil {
		t.Error("ZapocniPrijavu bez baze")
	}
	if _, err := repo.Prijava(ctx, []byte("p")); err == nil {
		t.Error("Prijava bez baze")
	}
	if _, _, err := repo.RezervirajPokusaj(ctx, []byte("p"), 5); err == nil {
		t.Error("RezervirajPokusaj bez baze")
	}
	if _, err := repo.ObrisiPrijavu(ctx, []byte("p")); err == nil {
		t.Error("ObrisiPrijavu bez baze")
	}
	if err := repo.SpremiRacunalo(ctx, &ZapamcenoRacunalo{UserID: id}); err == nil {
		t.Error("SpremiRacunalo bez baze")
	}
	if _, err := repo.Racunala(ctx, id); err == nil {
		t.Error("Racunala bez baze")
	}
	if _, err := repo.ObrisiRacunalo(ctx, id, "x"); err == nil {
		t.Error("ObrisiRacunalo bez baze")
	}
	if _, err := repo.ObrisiRacunalaKorisnika(ctx, id); err == nil {
		t.Error("ObrisiRacunalaKorisnika bez baze")
	}
	if err := repo.ZamijeniRezervne(ctx, id, nil); err == nil {
		t.Error("ZamijeniRezervne bez baze")
	}
	if err := repo.DodajPrivremeni(ctx, &KodPrijave{UserID: id}); err == nil {
		t.Error("DodajPrivremeni bez baze")
	}
	if _, err := repo.Kod(ctx, id, "P"); err == nil {
		t.Error("Kod bez baze")
	}
	if _, err := repo.IskoristiKod(ctx, "x", sadDrugogKoraka); err == nil {
		t.Error("IskoristiKod bez baze")
	}
	if _, _, err := repo.StanjeRezervnih(ctx, id); err == nil {
		t.Error("StanjeRezervnih bez baze")
	}
	if _, err := repo.OcistiIstekle(ctx, sadDrugogKoraka); err == nil {
		t.Error("OcistiIstekle bez baze")
	}
}

// Upis koji baza odbije (osoba ne postoji) vraća grešku.
func TestDrugiKorakUpisNepostojeceOsobe(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	nitko := uuid.New()
	var fk int
	nuzno(t, baza.QueryRow(`PRAGMA foreign_keys`).Scan(&fk))
	if fk != 1 {
		t.Skip("strani ključevi nisu uključeni")
	}
	if err := repo.ZapocniPrijavu(ctx, &PrijavaNaCekanju{ID: []byte("p"), UserID: nitko, ExpiresAt: sadDrugogKoraka}); err == nil {
		t.Error("prijava za nepostojeću osobu je spremljena")
	}
	if err := repo.SpremiRacunalo(ctx, &ZapamcenoRacunalo{UserID: nitko, TokenHash: []byte("t"), LozinkaOtisak: []byte("o")}); err == nil {
		t.Error("računalo za nepostojeću osobu je spremljeno")
	}
	if err := repo.ZamijeniRezervne(ctx, nitko, []KodPrijave{{Oznaka: "R1", KodHash: []byte("r")}}); err == nil {
		t.Error("rezervni kod za nepostojeću osobu je spremljen")
	}
	if err := repo.DodajPrivremeni(ctx, &KodPrijave{UserID: nitko, Oznaka: "P", KodHash: []byte("p")}); err == nil {
		t.Error("privremeni kod za nepostojeću osobu je spremljen")
	}
}

// Kad upis novog niza ili privremenog koda pukne na pola, stari kodovi
// ostaju kakvi su bili.
func TestZamjenaKodovaBezPolaPosla(t *testing.T) {
	baza, repo := bazaDrugogKoraka(t)
	ctx := context.Background()
	ana := osobaDrugogKoraka(t, baza, "ana", "ana@primjer.hr", true)
	istek := sadDrugogKoraka.Add(24 * time.Hour)
	nuzno(t, repo.ZamijeniRezervne(ctx, ana, []KodPrijave{
		{ID: "stari-1", Oznaka: "R1", KodHash: []byte("s1"), CreatedAt: sadDrugogKoraka},
		{ID: "stari-2", Oznaka: "R2", KodHash: []byte("s2"), CreatedAt: sadDrugogKoraka},
	}))
	nuzno(t, repo.DodajPrivremeni(ctx, &KodPrijave{ID: "priv", UserID: ana, Oznaka: "P", KodHash: []byte("p1"), CreatedAt: sadDrugogKoraka, ExpiresAt: &istek}))

	// drugi kod novog niza ima isti ID kao prvi: upis puca nakon brisanja starog niza
	if err := repo.ZamijeniRezervne(ctx, ana, []KodPrijave{
		{ID: "novi", Oznaka: "R1", KodHash: []byte("n1"), CreatedAt: sadDrugogKoraka.Add(time.Hour)},
		{ID: "novi", Oznaka: "R2", KodHash: []byte("n2"), CreatedAt: sadDrugogKoraka.Add(time.Hour)},
	}); err == nil {
		t.Fatal("niz s dva ista ID-a je spremljen")
	}
	if n, kad, err := repo.StanjeRezervnih(ctx, ana); err != nil || n != 2 || kad == nil || !kad.Equal(sadDrugogKoraka) {
		t.Errorf("stari niz nakon neuspjele zamjene: %d, %v, %v", n, kad, err)
	}
	if k, _ := repo.Kod(ctx, ana, "R1"); k == nil || !bytes.Equal(k.KodHash, []byte("s1")) {
		t.Errorf("kod R1 nakon neuspjele zamjene: %+v", k)
	}

	// novi privremeni s ID-om rezervnog koda ne smije odnijeti stari privremeni
	if err := repo.DodajPrivremeni(ctx, &KodPrijave{ID: "stari-1", UserID: ana, Oznaka: "P", KodHash: []byte("p2"), CreatedAt: sadDrugogKoraka, ExpiresAt: &istek}); err == nil {
		t.Fatal("privremeni kod s postojećim ID-om je spremljen")
	}
	if k, _ := repo.Kod(ctx, ana, "P"); k == nil || !bytes.Equal(k.KodHash, []byte("p1")) {
		t.Errorf("privremeni kod nakon neuspjelog upisa: %+v", k)
	}
}
