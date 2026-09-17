package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
)

// Put vreće kroz obranu: prazne čekaju u skladištu, napune se, odu na
// dionicu, dio se ugradi a dio vrati. Stanje se nigdje ne upisuje — zbraja
// se iz prometa, pa mora pratiti svaki od tih koraka.
func TestMtsPutVrecaKrozObranu(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)

	vrece := "vrece-50x80"
	ima := func(oblik string) float64 {
		t.Helper()
		st, err := s.StanjeSkladista(ctx, sk.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, sv := range st {
			if sv.Vrsta.ID == vrece {
				return sv.PoOblicima[oblik]
			}
		}
		return 0
	}

	// zatečeno stanje: 100 000 praznih vreća
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: vrece,
		Oblik: models.OblikPrazno, Kolicina: 100000, Napomena: "preneseno iz tablice"}); err != nil {
		t.Fatal(err)
	}
	if ima(models.OblikPrazno) != 100000 {
		t.Fatalf("početno stanje: %v", ima(models.OblikPrazno))
	}

	// punjenje: 5 000 praznih postaje napunjeno; ukupan broj vreća se ne mijenja
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPunjenje, SkladisteID: sk.ID, VrstaID: vrece,
		Oblik: models.OblikPrazno, UOblik: models.OblikPunjeno, Kolicina: 5000, Nalozio: "voditelj COP-a"}); err != nil {
		t.Fatal(err)
	}
	if ima(models.OblikPrazno) != 95000 || ima(models.OblikPunjeno) != 5000 {
		t.Fatalf("poslije punjenja: prazno %v, punjeno %v", ima(models.OblikPrazno), ima(models.OblikPunjeno))
	}

	// izdavanje na dionicu: iz skladišta odlazi, na terenu se pojavljuje
	obrana := uuid.New().String()
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: vrece,
		Oblik: models.OblikPunjeno, Kolicina: 4000, SectionCode: "B.34.1", JournalID: obrana,
		Nalozio: "rukovoditelj dionice", Preuzeo: "vodočuvar Batina", Dokument: "OT-2026-14"}); err != nil {
		t.Fatal(err)
	}
	if ima(models.OblikPunjeno) != 1000 {
		t.Fatalf("poslije izdavanja u skladištu: %v", ima(models.OblikPunjeno))
	}
	naTerenu, err := s.NaTerenu(ctx, obrana, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(naTerenu) != 1 || naTerenu[0].SectionCode != "B.34.1" || naTerenu[0].Kolicina != 4000 {
		t.Fatalf("na terenu poslije izdavanja: %+v", naTerenu)
	}

	// ugradnja i povrat zatvaraju teren
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometUtrosak, SkladisteID: sk.ID, VrstaID: vrece,
		Oblik: models.OblikPunjeno, Kolicina: 3500, SectionCode: "B.34.1", JournalID: obrana, Preuzeo: "DVD Batina"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPovrat, SkladisteID: sk.ID, VrstaID: vrece,
		Oblik: models.OblikPunjeno, Kolicina: 500, SectionCode: "B.34.1", JournalID: obrana}); err != nil {
		t.Fatal(err)
	}
	if naTerenu, _ := s.NaTerenu(ctx, obrana, ""); len(naTerenu) != 0 {
		t.Errorf("teren nije zatvoren: %+v", naTerenu)
	}
	if ima(models.OblikPunjeno) != 1500 || ima(models.OblikPrazno) != 95000 {
		t.Errorf("na kraju: prazno %v, punjeno %v", ima(models.OblikPrazno), ima(models.OblikPunjeno))
	}

	// ne može se izdati više nego što stoji; poruka kaže koliko ima
	_, err = s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometIzdano, SkladisteID: sk.ID, VrstaID: vrece,
		Oblik: models.OblikPunjeno, Kolicina: 9000, SectionCode: "B.34.1", JournalID: obrana})
	if err == nil || !strings.Contains(err.Error(), "1500") {
		t.Errorf("izdavanje preko zalihe: %v", err)
	}
	// ni ugraditi više nego što je na terenu
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometUtrosak, SkladisteID: sk.ID, VrstaID: vrece,
		Oblik: models.OblikPunjeno, Kolicina: 10, SectionCode: "B.34.1", JournalID: obrana}); err == nil ||
		!strings.Contains(err.Error(), "na terenu") {
		t.Errorf("utrošak bez terena: %v", err)
	}

	// knjiga prometa pamti tko je naložio, tko preuzeo i po kojem papiru
	knjiga, err := s.Promet(ctx, repository.FiltarPrometa{SkladisteID: sk.ID})
	if err != nil {
		t.Fatal(err)
	}
	nasao := false
	for _, p := range knjiga {
		if p.Vrsta == models.PrometIzdano && p.Preuzeo == "vodočuvar Batina" && p.Dokument == "OT-2026-14" && p.Nalozio == "rukovoditelj dionice" {
			nasao = true
		}
	}
	if !nasao {
		t.Error("knjiga ne pamti nalog, preuzimatelja i dokument")
	}
}

// Godišnji popis: što je prebrojano postaje stanje, a razlika prema knjizi
// ostaje zapisana kao usklađenje — ne briše se povijest.
func TestMtsGodisnjiPopisUsklađuje(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	lopata := "lopata"
	dan := pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 0, -1)
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: sk.ID, VrstaID: lopata,
		Kolicina: 46, Datum: dan.AddDate(0, 0, -5)}); err != nil {
		t.Fatal(err)
	}
	p, err := s.PredlozakPopisa(ctx, sk.ID, dan)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "" || len(p.Stavke) == 0 {
		t.Fatalf("predložak: %d stavki, id %q", len(p.Stavke), p.ID)
	}
	// predložak nudi knjižno stanje kao polazno, i sve vrste iz kataloga
	nasao := false
	for i := range p.Stavke {
		if p.Stavke[i].VrstaID == lopata {
			if p.Stavke[i].Knjizno != 46 || p.Stavke[i].Utvrdjeno != 46 {
				t.Errorf("predložak za lopate: %+v", p.Stavke[i])
			}
			p.Stavke[i].Utvrdjeno = 44 // dvije nedostaju
			p.Stavke[i].Potrebno = 15  // treba nabaviti
			nasao = true
		}
	}
	if !nasao {
		t.Fatal("predložak nema lopate")
	}
	if err := s.SpremiPopis(ctx, u, uprava, p); err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.Zakljucen() {
		t.Fatalf("spremljeno: %+v", p)
	}
	// nezaključen popis ne mijenja stanje
	if k := kolicinaU(t, s, ctx, sk.ID, lopata); k != 46 {
		t.Errorf("stanje prije zaključenja: %v", k)
	}
	if err := s.ZakljuciPopis(ctx, u, uprava, p.ID); err != nil {
		t.Fatal(err)
	}
	if k := kolicinaU(t, s, ctx, sk.ID, lopata); k != 44 {
		t.Errorf("stanje poslije zaključenja: %v", k)
	}
	zatvoren, _ := s.Popis(ctx, p.ID)
	if !zatvoren.Zakljucen() || zatvoren.Razlika() != 1 {
		t.Errorf("zaključeni popis: zaključen %v, razlika %d", zatvoren.Zakljucen(), zatvoren.Razlika())
	}
	// usklađenje stoji u knjizi, s pozivom na popis
	knjiga, _ := s.Promet(ctx, repository.FiltarPrometa{SkladisteID: sk.ID})
	nasao = false
	for _, x := range knjiga {
		if x.Vrsta == models.PrometPopis && x.Kolicina == -2 && x.PopisID == p.ID {
			nasao = true
		}
	}
	if !nasao {
		t.Error("usklađenje nije proknjiženo")
	}
	// zaključeni se ne prepravlja, i drugi popis na isti dan ne prolazi
	if err := s.SpremiPopis(ctx, u, uprava, zatvoren); err == nil || !strings.Contains(err.Error(), "zaključeni") {
		t.Errorf("izmjena zaključenog: %v", err)
	}
	drugi := &models.Popis{SkladisteID: sk.ID, Dan: dan}
	if err := s.SpremiPopis(ctx, u, uprava, drugi); err == nil || !strings.Contains(err.Error(), "već postoji") {
		t.Errorf("dvostruki popis: %v", err)
	}
}

// Kad u jednom sektoru ponestane, mora se vidjeti gdje drugdje ima — i
// sredstvo se prenosi preko granice sektora, bez da negdje nestane.
func TestMtsPrijenosMedjuSektorima(t *testing.T) {
	s, osijek, ctx, u, uprava := pripremiMts(t)
	metkovic := &models.Skladiste{Sektor: "F", AreaID: 40, Naziv: "Skladište Metković", Aktivno: true}
	if err := s.SpremiSkladiste(ctx, &models.UserPermissions{IsGlobalAdmin: true}, metkovic); err != nil {
		t.Fatal(err)
	}
	vrece := "vrece-50x80"
	if _, err := s.Provedi(ctx, u, &models.UserPermissions{IsGlobalAdmin: true}, Zahvat{Vrsta: models.PrometPocetno,
		SkladisteID: metkovic.ID, VrstaID: vrece, Oblik: models.OblikPrazno, Kolicina: 80000}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPocetno, SkladisteID: osijek.ID, VrstaID: vrece,
		Oblik: models.OblikPrazno, Kolicina: 2000}); err != nil {
		t.Fatal(err)
	}

	gdje, err := s.GdjeIma(ctx, vrece)
	if err != nil {
		t.Fatal(err)
	}
	if len(gdje) != 2 || gdje[0].Skladiste.ID != metkovic.ID || gdje[0].Kolicina != 80000 {
		t.Fatalf("gdje ima: %+v", gdje)
	}
	if gdje[0].Skladiste.Sektor != "F" || gdje[1].Skladiste.Sektor != "B" {
		t.Errorf("sektori: %s, %s", gdje[0].Skladiste.Sektor, gdje[1].Skladiste.Sektor)
	}

	// Metković šalje 20 000 vreća u Osijek: iz jednog odlazi, u drugi dolazi
	if _, err := s.Provedi(ctx, u, &models.UserPermissions{IsGlobalAdmin: true}, Zahvat{Vrsta: models.PrometPrijenos,
		SkladisteID: metkovic.ID, NaSkladisteID: osijek.ID, VrstaID: vrece, Oblik: models.OblikPrazno, Kolicina: 20000,
		Nalozio: "Glavni centar", Preuzeo: "prijevoznik"}); err != nil {
		t.Fatal(err)
	}
	if k := kolicinaU(t, s, ctx, metkovic.ID, vrece); k != 60000 {
		t.Errorf("Metković poslije prijenosa: %v", k)
	}
	if k := kolicinaU(t, s, ctx, osijek.ID, vrece); k != 22000 {
		t.Errorf("Osijek poslije prijenosa: %v", k)
	}
	// zbroj svih skladišta se prijenosom ne mijenja
	svi, err := s.StanjeSektora(ctx, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sv := range svi {
		if sv.Vrsta.ID == vrece && sv.Ukupno != 82000 {
			t.Errorf("ukupno poslije prijenosa: %v", sv.Ukupno)
		}
	}
}

// Katalog dolazi napunjen propisanim popisom, a stanje nudi sve njegove
// retke — i one kojih nema, jer ih obrazac za Glavni centar traži.
func TestMtsKatalogIPrava(t *testing.T) {
	s, sk, ctx, u, uprava := pripremiMts(t)
	vrste, err := s.Vrste(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(vrste) != 66 {
		t.Errorf("katalog ima %d vrsta, a propisani popis 66", len(vrste))
	}
	if vrste[0].Grupa != models.GrupaOprema || vrste[0].Naziv != "Agregat za rasvjetu" {
		t.Errorf("prva vrsta: %+v", vrste[0])
	}
	stanje, err := s.StanjeSkladista(ctx, sk.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stanje) != len(vrste) {
		t.Errorf("stanje ima %d redaka, katalog %d", len(stanje), len(vrste))
	}

	// vodočuvar s dosegom na području ne upisuje promet; skladištar da
	vodocuvar := &models.UserPermissions{AllowedAreas: map[int]bool{34: true}}
	if _, err := s.Provedi(ctx, u, vodocuvar, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 5}); err == nil {
		t.Error("vodočuvar upisao promet")
	}
	bp := 34
	sektorB := "B"
	skladistar := &models.UserPermissions{AllowedAreas: map[int]bool{34: true},
		User: models.User{Duties: []models.Duty{{Role: models.RoleWarehouseKeeper, AreaID: &bp, SectorID: &sektorB, IsActive: true}}}}
	if _, err := s.Provedi(ctx, u, skladistar, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 5}); err != nil {
		t.Errorf("skladištar ne može upisati: %v", err)
	}
	drugiBP := 16
	tudjiSkladistar := &models.UserPermissions{User: models.User{Duties: []models.Duty{{Role: models.RoleWarehouseKeeper, AreaID: &drugiBP, SectorID: &sektorB, IsActive: true}}}}
	if _, err := s.Provedi(ctx, u, tudjiSkladistar, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 5}); err == nil {
		t.Error("skladištar drugog područja upisao promet")
	}

	// tuđi sektor ne upisuje promet
	tudji := &models.UserPermissions{AllowedSectors: map[string]bool{"C": true}}
	if _, err := s.Provedi(ctx, u, tudji, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "lopata", Kolicina: 5}); err == nil ||
		!strings.Contains(err.Error(), "uprava") {
		t.Errorf("tuđi sektor: %v", err)
	}
	// promet unaprijed ne prolazi
	sutra := pocetakDana(time.Now().In(models.Zagreb)).AddDate(0, 0, 1)
	if _, err := s.Provedi(ctx, u, uprava, Zahvat{Vrsta: models.PrometPrimka, SkladisteID: sk.ID, VrstaID: "lopata",
		Kolicina: 5, Datum: sutra}); err == nil || !strings.Contains(err.Error(), "unaprijed") {
		t.Errorf("promet unaprijed: %v", err)
	}
	// nova vrsta dobiva oznaku iz naziva
	nova := &models.VrstaSredstva{Grupa: models.GrupaAlat, Naziv: "Ćuskija velika", Jedinica: "kom", Aktivna: true, Redoslijed: 99}
	if err := s.SpremiVrstu(ctx, nova); err != nil {
		t.Fatal(err)
	}
	if nova.ID != "cuskija-velika" {
		t.Errorf("oznaka nove vrste: %q", nova.ID)
	}
}

// pripremiMts otvara bazu s katalogom, jednim sektorom, područjem i skladištem
func pripremiMts(t *testing.T) (*MtsService, *models.Skladiste, context.Context, *models.User, *models.UserPermissions) {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "mts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('F', 'Sektor F', 'VGO Split', 'COP Split')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (40, 'F', 'Neretva', 'VGI Neretva', 'Metković')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	repo := repository.NewMtsRepository(baza, rec)
	if err := repo.OsigurajKatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := NewMtsService(repo, repository.NewSectionRepository(baza, rec), nil)
	ctx := context.Background()
	u := &models.User{ID: uuid.New(), FullName: "Skladištar Osijek"}
	uprava := &models.UserPermissions{AdminSectors: map[string]bool{"B": true}, AllowedSectors: map[string]bool{"B": true}}
	sk := &models.Skladiste{Sektor: "B", AreaID: 34, Naziv: "Centralno skladište Osijek", Adresa: "Splavarska 2a, Osijek",
		Centralno: true, Aktivno: true}
	if err := s.SpremiSkladiste(ctx, uprava, sk); err != nil {
		t.Fatal(err)
	}
	return s, sk, ctx, u, uprava
}

// kolicinaU javlja koliko jedne vrste stoji u skladištu, preko svih oblika
func kolicinaU(t *testing.T, s *MtsService, ctx context.Context, skladisteID, vrstaID string) float64 {
	t.Helper()
	st, err := s.StanjeSkladista(ctx, skladisteID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sv := range st {
		if sv.Vrsta.ID == vrstaID {
			return sv.Ukupno
		}
	}
	return 0
}
