package service

import (
	"context"
	"database/sql"
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

// zidPrijava je čvor s prijavama s terena za provjeru zida: Marko Markić,
// vodočuvar na BP 34 (Sektor B), piše prijave
type zidPrijava struct {
	ctx      context.Context
	baza     *sql.DB
	rec      *ledger.Recorder
	repo     *repository.PrijavaRepository
	prijave  *PrijavaService
	zid      *ZidService
	marko    *models.User
	journals *repository.JournalRepository
}

func noviZidPrijava(t *testing.T) *zidPrijava {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "zid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('B', 'Sektor B', 'VGO Osijek', 'COP Osijek')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (34, 'B', 'Drava i Dunav', 'COP', 'Osijek')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	userRepo := repository.NewUserRepository(baza, rec)
	users := NewUserService(userRepo, NewAuthService(userRepo, repository.NewSessionRepository(baza)), NewSSEBroker())
	repo := repository.NewPrijavaRepository(baza, rec)
	vodocuvar := NewVodocuvarService(repository.NewVodocuvarRepository(baza, rec), users, "test")
	journals := repository.NewJournalRepository(baza, rec)
	return &zidPrijava{ctx: context.Background(), baza: baza, rec: rec, repo: repo, prijave: NewPrijavaService(repo, users, vodocuvar, "test"),
		zid: NewZidService(rec, journals, repository.NewSectionRepository(baza, rec), nil, userRepo, nil, nil), journals: journals,
		marko: &models.User{ID: uuid.New(), FullName: "Marko Markić"}}
}

// nova sprema prijavu kako je vodočuvar sastavi i, kad je broj zadan, objavi je
func (z *zidPrijava) nova(t *testing.T, vrsta, naslov string, broj int) *models.PrijavaSTerena {
	t.Helper()
	p := &models.PrijavaSTerena{UserID: z.marko.ID.String(), Ime: z.marko.FullName, Sektor: "B", AreaID: 34, Vrsta: vrsta,
		Naslov: naslov, Opis: "Zatečeno pri obilasku.", Datum: time.Now().In(models.Zagreb), Status: models.PrijavaNacrt, Vodotok: "Dunav"}
	z.spremi(t, p)
	if broj > 0 {
		sad := time.Now()
		p.Broj, p.Godina, p.Status, p.ObjavljenoAt = broj, sad.In(models.Zagreb).Year(), models.PrijavaObjavljena, &sad
		z.spremi(t, p)
	}
	return p
}

func (z *zidPrijava) spremi(t *testing.T, p *models.PrijavaSTerena) {
	t.Helper()
	if err := z.repo.Save(z.ctx, p); err != nil {
		t.Fatal(err)
	}
}

// prijaveNaZidu su redci modula prijava koje osoba vidi, kao „naslov|tekst|tko”
func (z *zidPrijava) prijaveNaZidu(t *testing.T, perms *models.UserPermissions, f FiltarZida) []Dogadjaj {
	t.Helper()
	f.Limit = 50
	dog, _, err := z.zid.Zadnji(z.ctx, perms, f)
	if err != nil {
		t.Fatal(err)
	}
	var out []Dogadjaj
	for _, d := range dog {
		if d.Modul == ModulPrijave {
			out = append(out, d)
		}
	}
	return out
}

func naslovi(dog []Dogadjaj) string {
	var s []string
	for _, d := range dog {
		s = append(s, d.Naslov+"|"+d.Tekst+"|"+d.Tko)
	}
	return strings.Join(s, "\n")
}

// Prijave s terena na zidu događanja (Odluke (8. 10. 2026.), pitanje 20):
// zaseban gumb „Prijave s terena”, sve četiri vrste, „Prijava” istaknuta.
// Na zid idu objava i rješenje, svaki jednom (urudžba nije novi redak);
// nacrt nikad, ni autoru; prijava prenesena iz BP16 nikad. Redak vidi samo
// tko smije čitati tu prijavu, po istom pravilu kao Dokumentacija →
// Prijave s terena, a ne po pravilu zida: drugi vodočuvar istog područja
// ne vidi ništa, iako zid sektora vidi.
func TestZidPrijaveSTerena(t *testing.T) {
	z := noviZidPrijava(t)
	if ModulNaziv(ModulPrijave) != "Prijave s terena" || ModulIkona(ModulPrijave) != "map-pin" {
		t.Errorf("gumb zida: %q, %q", ModulNaziv(ModulPrijave), ModulIkona(ModulPrijave))
	}

	z.nova(t, models.PrijavaObavijest, "Nacrt koji nije objavljen", 0)
	z.nova(t, models.PrijavaObavijest, "Otpad u kanalu", 41)
	pr := z.nova(t, models.PrijavaPrijava, "Procjeđivanje na nasipu", 42)
	z.nova(t, models.PrijavaIzvjesce, "Stanje rampe", 43)
	z.nova(t, models.PrijavaZahtjev, "Košnja pokosa", 44)
	// urudžba, pa rješenje, pa ispravak urudžbe: jedan redak rješenja
	pr.Klasa = "325-01/26-01/1"
	z.spremi(t, pr)
	sad := time.Now()
	pr.Status, pr.RijesioID, pr.Rijesio, pr.RijesenoAt = models.PrijavaRijesena, uuid.NewString(), "Ivo Ivić", &sad
	z.spremi(t, pr)
	pr.Urbroj = "2158-26-1"
	z.spremi(t, pr)
	// rekonstrukcija iz ranije evidencije: objavljena i riješena, ali ne na zid
	staro := time.Date(2024, 6, 5, 11, 0, 0, 0, models.Zagreb)
	z.spremi(t, &models.PrijavaSTerena{UserID: z.marko.ID.String(), Ime: z.marko.FullName, Sektor: "B", AreaID: 34, Vrsta: models.PrijavaPrijava,
		Naslov: "Prenesena iz BP16", Opis: "Iz ranije evidencije.", Datum: staro, Broj: 7, Godina: 2024, Status: models.PrijavaRijesena,
		ObjavljenoAt: &staro, Rekonstrukcija: true, Izvor: "bp16:7"})

	// bez pravila čitanja prijava nitko ne vidi nijednu, ni uprava organizacije
	ana := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Ana Anić", IsGlobalAdmin: true}}
	if dog := z.prijaveNaZidu(t, ana, FiltarZida{}); len(dog) != 0 {
		t.Fatalf("zid bez pravila čitanja prijava pokazuje:\n%s", naslovi(dog))
	}
	z.zid.SetVidiPrijavu(z.prijave.SmijeVidjeti)

	ocekivano := []string{
		"Riješena prijava s terena|B-T-42/" + godina(sad) + ": Procjeđivanje na nasipu|Ivo Ivić",
		"Zahtjev s terena B-T-44/" + godina(sad) + "|Košnja pokosa · Dunav|Marko Markić",
		"Izvješće s terena B-T-43/" + godina(sad) + "|Stanje rampe · Dunav|Marko Markić",
		"Prijava s terena B-T-42/" + godina(sad) + "|Procjeđivanje na nasipu · Dunav|Marko Markić",
		"Obavijest s terena B-T-41/" + godina(sad) + "|Otpad u kanalu · Dunav|Marko Markić",
	}
	marko := &models.UserPermissions{User: *z.marko, AllowedSectors: map[string]bool{"B": true}, AllowedAreas: map[int]bool{34: true}}
	podrucje34 := 34
	ivo := &models.UserPermissions{AdminAreas: map[int]bool{34: true}, User: models.User{ID: uuid.New(), FullName: "Ivo Ivić",
		Duties: []models.Duty{{Role: models.RoleAreaLeader, ScopeType: models.ScopeArea, AreaID: &podrucje34, IsActive: true}}}}
	for ime, perms := range map[string]*models.UserPermissions{"uprava organizacije": ana, "autor": marko, "rukovoditelj područja": ivo} {
		for _, modul := range []string{"", ModulPrijave} {
			dog := z.prijaveNaZidu(t, perms, FiltarZida{Modul: modul})
			if got := naslovi(dog); got != strings.Join(ocekivano, "\n") {
				t.Errorf("%s, modul %q:\n%s\nočekivano:\n%s", ime, modul, got, strings.Join(ocekivano, "\n"))
				continue
			}
			for _, d := range dog {
				if d.Sektor != "B" || d.AreaID != 34 || !strings.HasPrefix(d.Link, "/prijave/") {
					t.Errorf("%s: redak %+v", ime, d)
				}
				if d.Vazno != strings.HasPrefix(d.Naslov, "Prijava s terena") {
					t.Errorf("%s: istaknuta smije biti samo vrsta Prijava: %+v", ime, d)
				}
			}
		}
	}

	// Pero Perić, vodočuvar istog područja: zid sektora vidi, tuđe prijave ne
	pero := &models.UserPermissions{AllowedSectors: map[string]bool{"B": true}, AllowedAreas: map[int]bool{34: true},
		User: models.User{ID: uuid.New(), FullName: "Pero Perić",
			Duties: []models.Duty{{Role: models.RoleWaterGuard, ScopeType: models.ScopeArea, AreaID: &podrucje34, IsActive: true}}}}
	if dog := z.prijaveNaZidu(t, pero, FiltarZida{}); len(dog) != 0 {
		t.Errorf("drugi vodočuvar vidi tuđe prijave na zidu:\n%s", naslovi(dog))
	}
	if !z.zid.vidi(pero, Dogadjaj{Sektor: "B"}, z.zid.podrucjaPoSektoru()) {
		t.Error("pravilo zida: vodočuvar područja vidi zid svog sektora")
	}

	// filtar modula na zidu: samo prijave
	if dog, _, _ := z.zid.Zadnji(z.ctx, ana, FiltarZida{Modul: ModulPrijave, Limit: 50}); len(dog) != len(ocekivano) {
		t.Errorf("filtar Prijave s terena: %d redaka, očekivano %d", len(dog), len(ocekivano))
	}
}

func godina(t time.Time) string { return t.In(models.Zagreb).Format("2006") }

// Redak objave i redak rješenja određuje povijest prijave: verzija u kojoj
// je prijava postala objavljena, odnosno riješena, prema verziji koju je
// zamijenila. Kad je starija povijest sažeta, događaj nosi najstarija
// verzija koja je ostala, a verziju čija je prethodnica sažeta uspoređuje
// se s prvom starijom verzijom koja je ostala. Objavu ipak ne nosi verzija
// koja je urudžbirana ili riješena: to se upisuje tek na objavljenu
// prijavu, pa je verzija objave sažeta i redak objave nestaje s njom
// (Odluke (8. 10. 2026.), pitanje 20; prije te dorade objavu je nosila i
// takva verzija). Kad je verzija s događajem obrisana s oglasne ploče (iza
// nje ostaje starija verzija istog čvora u istom kanalu, koju bi sažimanje
// obrisalo), događaja više nema.
func TestDogadjajiPrijave(t *testing.T) {
	v := func(id, prethodna, payload string, arhivirana bool) ledger.Version {
		return ledger.Version{VersionID: id, Supersedes: prethodna, Entity: repository.EntityPrijave, Payload: []byte(payload), Archived: arhivirana}
	}
	// vc je verzija s čvorom i kanalom
	vc := func(id, prethodna, payload, cvor, kanal string) ledger.Version {
		return ledger.Version{VersionID: id, Supersedes: prethodna, Entity: repository.EntityPrijave, Payload: []byte(payload), NodeID: cvor, Channel: kanal}
	}
	kanal := "prijave/34/2026"
	objavljeno := `"objavljeno_at":"2026-10-08T08:00:00Z"`
	nacrt := v("1", "", `{"status":"NACRT"}`, false)
	for _, k := range []struct {
		ime              string
		povijest         []ledger.Version // najnovija prva, kao rec.History
		objava, rjesenje string
	}{
		{"prazna povijest", nil, "", ""},
		{"nacrt", []ledger.Version{nacrt}, "", ""},
		{"objava i urudžba", []ledger.Version{
			v("3", "2", `{"status":"OBJAVLJENA","klasa":"1",`+objavljeno+`}`, false),
			v("2", "1", `{"status":"OBJAVLJENA",`+objavljeno+`}`, false),
			nacrt}, "2", ""},
		{"rješenje pa urudžba", []ledger.Version{
			v("4", "3", `{"status":"RIJESENA","urbroj":"1",`+objavljeno+`}`, false),
			v("3", "2", `{"status":"RIJESENA",`+objavljeno+`}`, false),
			v("2", "1", `{"status":"OBJAVLJENA",`+objavljeno+`}`, false),
			nacrt}, "2", "3"},
		{"objavljena već u prvoj verziji", []ledger.Version{
			v("2", "1", `{"status":"OBJAVLJENA","klasa":"1",`+objavljeno+`}`, false),
			v("1", "", `{"status":"OBJAVLJENA",`+objavljeno+`}`, false)}, "1", ""},
		{"sažeta povijest: ostalo samo rješenje", []ledger.Version{v("9", "8", `{"status":"RIJESENA",`+objavljeno+`}`, false)}, "", "9"},
		{"sažeta povijest: ostala samo urudžba", []ledger.Version{v("9", "8", `{"status":"OBJAVLJENA","klasa":"1",`+objavljeno+`}`, false)}, "", ""},
		{"sažeta objava, ostali nacrt i urudžba", []ledger.Version{
			vc("3", "2", `{"status":"OBJAVLJENA","urbroj":"1",`+objavljeno+`}`, "ured", kanal),
			vc("1", "", `{"status":"NACRT"}`, "ured", "prijave/34/2025")}, "", ""},
		{"sažeta povijest, poslije urudžba", []ledger.Version{
			v("3", "2", `{"status":"OBJAVLJENA","klasa":"1",`+objavljeno+`}`, false),
			v("2", "1", `{"status":"OBJAVLJENA",`+objavljeno+`}`, false)}, "2", ""},
		{"objava obrisana s ploče", []ledger.Version{
			v("3", "2", `{"status":"OBJAVLJENA","klasa":"1",`+objavljeno+`}`, false),
			nacrt}, "", ""},
		{"rješenje obrisano s ploče", []ledger.Version{
			v("5", "4", `{"status":"RIJESENA","urbroj":"1",`+objavljeno+`}`, false),
			v("3", "2", `{"status":"OBJAVLJENA","klasa":"1",`+objavljeno+`}`, false),
			v("2", "1", `{"status":"OBJAVLJENA",`+objavljeno+`}`, false),
			nacrt}, "2", ""},
		{"sažeto na dva čvora: objava s terena, urbroj u uredu", []ledger.Version{
			vc("u2", "r", `{"status":"RIJESENA","urbroj":"1",`+objavljeno+`}`, "ured", kanal),
			vc("p", "d", `{"status":"OBJAVLJENA",`+objavljeno+`}`, "teren", kanal)}, "p", "u2"},
		{"sažeto na dva čvora: riješena već prije praznine", []ledger.Version{
			vc("u2", "g", `{"status":"RIJESENA","urbroj":"1",`+objavljeno+`}`, "ured", kanal),
			vc("r", "x", `{"status":"RIJESENA",`+objavljeno+`}`, "teren", kanal)}, "", "r"},
		{"obrisano s ploče, poslije sažeto: kasnija verzija ga ne ponavlja", []ledger.Version{
			vc("u2", "g", `{"status":"RIJESENA","urbroj":"1",`+objavljeno+`}`, "ured", kanal),
			vc("r", "x", `{"status":"RIJESENA",`+objavljeno+`}`, "teren", kanal),
			vc("d", "", `{"status":"NACRT"}`, "teren", kanal)}, "", ""},
		{"starija verzija istog čvora u drugom kanalu ne znači brisanje", []ledger.Version{
			vc("3", "2", `{"status":"OBJAVLJENA",`+objavljeno+`}`, "ured", kanal),
			vc("1", "", `{"status":"NACRT"}`, "ured", "prijave/34/2025")}, "3", ""},
		{"obrisano s ploče: starija verzija istog čvora i kanala", []ledger.Version{
			vc("u2", "r", `{"status":"RIJESENA","urbroj":"1",`+objavljeno+`}`, "ured", kanal),
			vc("u1", "p", `{"status":"OBJAVLJENA","klasa":"1",`+objavljeno+`}`, "ured", kanal),
			vc("p", "d", `{"status":"OBJAVLJENA",`+objavljeno+`}`, "teren", kanal),
			vc("d", "", `{"status":"NACRT"}`, "teren", kanal)}, "p", ""},
		{"prethodnica se ne može pročitati", []ledger.Version{
			v("2", "1", `{"status":"OBJAVLJENA",`+objavljeno+`}`, false),
			v("1", "", `nije json`, false)}, "2", ""},
		{"arhivirana i neispravna verzija se preskaču", []ledger.Version{
			v("3", "2", `{"status":"OBJAVLJENA",`+objavljeno+`}`, true),
			v("2", "", `nije json`, false)}, "", ""},
	} {
		objava, rjesenje := dogadjajiPrijave(k.povijest)
		if objava != k.objava || rjesenje != k.rjesenje {
			t.Errorf("%s: objava %q, rješenje %q; očekivano %q, %q", k.ime, objava, rjesenje, k.objava, k.rjesenje)
		}
	}
}

// Redak prijave obrisan s oglasne ploče na čvoru na kojem su upisane i
// njezine ostale verzije ostaje obrisan na tom čvoru: brisanje uklanja
// verziju s događajem, pa urudžba ili kasnija verzija ne smiju postati novi
// redak objave ili rješenja s kasnijim vremenom. Što brisanje radi kad su
// prijavu pisala dva čvora zaključava TestZidBrisanjePrijaveSPloceNaDvaCvora.
func TestZidObrisanRedakPrijaveNeVracaSe(t *testing.T) {
	z := noviZidPrijava(t)
	z.zid.SetVidiPrijavu(z.prijave.SmijeVidjeti)
	ana := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Ana Anić", IsGlobalAdmin: true}}
	redak := func(naslov string) Dogadjaj {
		t.Helper()
		for _, d := range z.prijaveNaZidu(t, ana, FiltarZida{}) {
			if strings.HasPrefix(d.Naslov, naslov) {
				return d
			}
		}
		t.Fatalf("na zidu nema retka %q:\n%s", naslov, naslovi(z.prijaveNaZidu(t, ana, FiltarZida{})))
		return Dogadjaj{}
	}

	pr := z.nova(t, models.PrijavaPrijava, "Procjeđivanje na nasipu", 42)
	pr.Klasa = "325-01/26-01/1"
	z.spremi(t, pr)
	if err := z.zid.ObrisiDogadjaj(z.ctx, ana, redak("Prijava s terena").VersionID); err != nil {
		t.Fatal(err)
	}
	if dog := z.prijaveNaZidu(t, ana, FiltarZida{}); len(dog) != 0 {
		t.Fatalf("obrisana objava se vratila na zid:\n%s", naslovi(dog))
	}

	// rješenje pa urbroj: obrisano rješenje se ne vraća s vremenom urbroja
	sad := time.Now()
	pr.Status, pr.RijesioID, pr.Rijesio, pr.RijesenoAt = models.PrijavaRijesena, uuid.NewString(), "Ivo Ivić", &sad
	z.spremi(t, pr)
	pr.Urbroj = "2158-26-1"
	z.spremi(t, pr)
	if err := z.zid.ObrisiDogadjaj(z.ctx, ana, redak("Riješena").VersionID); err != nil {
		t.Fatal(err)
	}
	if dog := z.prijaveNaZidu(t, ana, FiltarZida{}); len(dog) != 0 {
		t.Fatalf("obrisano rješenje se vratilo na zid:\n%s", naslovi(dog))
	}
}

// Redak rješenja nosi vrstu: riješeni zahtjev nije „riješena prijava”,
// jer je „Prijava” zasebna, istaknuta vrsta (Odluke (8. 10. 2026.),
// pitanje 20)
func TestZidRjesenjeNosiVrstuPrijave(t *testing.T) {
	z := noviZidPrijava(t)
	z.zid.SetVidiPrijavu(z.prijave.SmijeVidjeti)
	ana := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Ana Anić", IsGlobalAdmin: true}}
	ocekivano := map[string]string{
		models.PrijavaObavijest: "Riješena obavijest s terena",
		models.PrijavaPrijava:   "Riješena prijava s terena",
		models.PrijavaIzvjesce:  "Riješeno izvješće s terena",
		models.PrijavaZahtjev:   "Riješen zahtjev s terena",
	}
	for i, vrsta := range models.VrstePrijava {
		p := z.nova(t, vrsta, "Košnja pokosa", 50+i)
		sad := time.Now()
		p.Status, p.RijesioID, p.Rijesio, p.RijesenoAt = models.PrijavaRijesena, uuid.NewString(), "Ivo Ivić", &sad
		z.spremi(t, p)
		dog := z.prijaveNaZidu(t, ana, FiltarZida{})
		if len(dog) == 0 || dog[0].Naslov != ocekivano[vrsta] || dog[0].Tekst != p.Oznaka()+": Košnja pokosa" || dog[0].Vazno {
			t.Errorf("%s: rješenje na zidu\n%s\nočekivan naslov %q", vrsta, naslovi(dog), ocekivano[vrsta])
		}
	}
	if got := naslovRjesenja("NEPOZNATA"); got != "Riješena prijava s terena" {
		t.Errorf("nepoznata vrsta: %q", got)
	}
}

// Sažimanje knjige (Održavanje baze) briše starije verzije, a zid ima samo
// ono što je u knjizi ostalo. Prijava kojoj je ostala samo verzija objave
// zadržava redak objave. Prijavi kojoj su objava i rješenje sažeti u jednu
// verziju ostaje samo redak rješenja, s vremenom te verzije: redak objave,
// s isticanjem vrste „Prijava”, nestaje, jer jedna verzija daje jedan
// redak (Odluke (8. 10. 2026.), pitanje 20).
func TestZidPrijavaNakonSazimanjaKnjige(t *testing.T) {
	z := noviZidPrijava(t)
	z.zid.SetVidiPrijavu(z.prijave.SmijeVidjeti)
	ana := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Ana Anić", IsGlobalAdmin: true}}

	z.nova(t, models.PrijavaObavijest, "Otpad u kanalu", 41)
	pr := z.nova(t, models.PrijavaPrijava, "Procjeđivanje na nasipu", 42)
	sad := time.Now()
	pr.Status, pr.RijesioID, pr.Rijesio, pr.RijesenoAt = models.PrijavaRijesena, uuid.NewString(), "Ivo Ivić", &sad
	z.spremi(t, pr)
	pr.Urbroj = "2158-26-1"
	z.spremi(t, pr)
	if n, err := z.rec.Compact(z.ctx, time.Now().Add(time.Hour)); err != nil || n == 0 {
		t.Fatalf("sažimanje: %d, %v", n, err)
	}

	dog := z.prijaveNaZidu(t, ana, FiltarZida{})
	ocekivano := "Riješena prijava s terena|" + pr.Oznaka() + ": Procjeđivanje na nasipu|Ivo Ivić\n" +
		"Obavijest s terena B-T-41/" + godina(sad) + "|Otpad u kanalu · Dunav|Marko Markić"
	if got := naslovi(dog); got != ocekivano {
		t.Fatalf("zid nakon sažimanja:\n%s\nočekivano:\n%s", got, ocekivano)
	}
	zadnja, err := z.rec.Latest(z.ctx, repository.EntityPrijave, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dog[0].VersionID != zadnja.VersionID || !dog[0].Kad.Equal(zadnja.CreatedAt) || dog[0].Vazno {
		t.Errorf("rješenje nakon sažimanja: %+v, zadnja verzija %s u %v", dog[0], zadnja.VersionID, zadnja.CreatedAt)
	}
}

// Prijava objavljena na terenskom čvoru, a urudžbirana i riješena na čvoru
// ovog zida, pa sažimanje knjige: sažimanje zadržava zadnju verziju svakog
// čvora u kanalu, pa objava s terena ostaje bez nacrta, a urbroj bez
// rješenja. Zid i dalje pokazuje rješenje kao zatvaranje predmeta i objavu
// (Odluke (8. 10. 2026.), pitanje 20); praznina u povijesti je sažimanje, a
// ne brisanje s oglasne ploče, jer iza nje ne ostaje starija verzija istog
// čvora u istom kanalu.
func TestZidPrijavaSazetaNaDvaCvora(t *testing.T) {
	z := noviZidPrijava(t)
	z.zid.SetVidiPrijavu(z.prijave.SmijeVidjeti)
	ana := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Ana Anić", IsGlobalAdmin: true}}
	teren := repository.NewPrijavaRepository(z.baza, ledger.New(z.baza, "teren"))

	pr := &models.PrijavaSTerena{UserID: z.marko.ID.String(), Ime: z.marko.FullName, Sektor: "B", AreaID: 34, Vrsta: models.PrijavaPrijava,
		Naslov: "Procjeđivanje na nasipu", Opis: "Zatečeno pri obilasku.", Datum: time.Now().In(models.Zagreb), Status: models.PrijavaNacrt, Vodotok: "Dunav"}
	if err := teren.Save(z.ctx, pr); err != nil {
		t.Fatal(err)
	}
	sad := time.Now()
	pr.Broj, pr.Godina, pr.Status, pr.ObjavljenoAt = 42, sad.In(models.Zagreb).Year(), models.PrijavaObjavljena, &sad
	if err := teren.Save(z.ctx, pr); err != nil {
		t.Fatal(err)
	}
	pr.Klasa = "325-01/26-01/1"
	z.spremi(t, pr)
	pr.Status, pr.RijesioID, pr.Rijesio, pr.RijesenoAt = models.PrijavaRijesena, uuid.NewString(), "Ivo Ivić", &sad
	z.spremi(t, pr)
	pr.Urbroj = "2158-26-1"
	z.spremi(t, pr)

	ocekivano := "Riješena prijava s terena|" + pr.Oznaka() + ": Procjeđivanje na nasipu|Ivo Ivić\n" +
		"Prijava s terena " + pr.Oznaka() + "|Procjeđivanje na nasipu · Dunav|Marko Markić"
	if got := naslovi(z.prijaveNaZidu(t, ana, FiltarZida{})); got != ocekivano {
		t.Fatalf("zid prije sažimanja:\n%s\nočekivano:\n%s", got, ocekivano)
	}
	if n, err := z.rec.Compact(z.ctx, time.Now().Add(time.Hour)); err != nil || n != 3 {
		t.Fatalf("sažimanje: %d, %v; očekivano 3 obrisane verzije (nacrt, urudžba, rješenje)", n, err)
	}
	dog := z.prijaveNaZidu(t, ana, FiltarZida{})
	if got := naslovi(dog); got != ocekivano {
		t.Fatalf("zid nakon sažimanja:\n%s\nočekivano:\n%s", got, ocekivano)
	}
	zadnja, err := z.rec.Latest(z.ctx, repository.EntityPrijave, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dog[0].VersionID != zadnja.VersionID || dog[0].Cvor != "test" || dog[0].Vazno {
		t.Errorf("rješenje nosi urbroj %s: %+v", zadnja.VersionID, dog[0])
	}
	if dog[1].Cvor != "teren" || !dog[1].Vazno {
		t.Errorf("objava ostaje verzija s terena, istaknuta: %+v", dog[1])
	}
}

// razmijeni šalje čvoru do ono što od čvora od još nema, kao razmjena
// čvorova, i vraća broj primljenih verzija
func razmijeni(t *testing.T, ctx context.Context, od, do *ledger.Recorder) int {
	t.Helper()
	granica, err := do.Frontier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	paket, err := od.Delta(ctx, granica, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	n, err := do.Apply(ctx, paket)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Brisanje s oglasne ploče briše verziju iz knjige samo na ovom čvoru
// (Administracija › Opcije: „Briše samo na ovom čvoru”), i za redak prijave
// s terena kao za svaki drugi redak. Test zaključava sadašnje ponašanje, a
// ne obećava trajno brisanje:
//   - prijavu objavi vodočuvar na terenskom čvoru, urudžbira je ured: kad
//     ured obriše redak objave, redak nestaje. Brisanje tuđe verzije ne
//     razlikuje se od sažimanja knjige (iza obrisane objave ne ostaje
//     starija verzija ureda), a urudžba nije objava, pa redak objave ne
//     nosi ni ona (Odluke (8. 10. 2026.), pitanje 20; prije te dorade
//     nosila ga je, s vremenom urudžbe);
//   - obrisana verzija koja je zadnji upis svog čvora u kanalu vraća se
//     sljedećom razmjenom, pa je redak opet objava s terena;
//   - kad je sve verzije upisao ured, obrisani redak ne vraća ni razmjena.
func TestZidBrisanjePrijaveSPloceNaDvaCvora(t *testing.T) {
	z := noviZidPrijava(t) // ured: čvor ovog zida
	z.zid.SetVidiPrijavu(z.prijave.SmijeVidjeti)
	ana := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Ana Anić", IsGlobalAdmin: true}}
	bazaT, err := db.OpenDB(filepath.Join(t.TempDir(), "teren.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bazaT.Close() })
	if err := db.InitSchema(bazaT); err != nil {
		t.Fatal(err)
	}
	recT := ledger.New(bazaT, "teren")
	teren := repository.NewPrijavaRepository(bazaT, recT)

	pr := &models.PrijavaSTerena{UserID: z.marko.ID.String(), Ime: z.marko.FullName, Sektor: "B", AreaID: 34, Vrsta: models.PrijavaPrijava,
		Naslov: "Procjeđivanje na nasipu", Opis: "Zatečeno pri obilasku.", Datum: time.Now().In(models.Zagreb), Status: models.PrijavaNacrt, Vodotok: "Dunav"}
	if err := teren.Save(z.ctx, pr); err != nil {
		t.Fatal(err)
	}
	sad := time.Now()
	pr.Broj, pr.Godina, pr.Status, pr.ObjavljenoAt = 42, sad.In(models.Zagreb).Year(), models.PrijavaObjavljena, &sad
	if err := teren.Save(z.ctx, pr); err != nil {
		t.Fatal(err)
	}
	if n := razmijeni(t, z.ctx, recT, z.rec); n != 2 {
		t.Fatalf("razmjena s terena: %d verzija, očekivane 2", n)
	}
	objava, err := z.rec.Latest(z.ctx, repository.EntityPrijave, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	pr.Klasa = "325-01/26-01/1"
	z.spremi(t, pr) // urudžba u uredu
	urudzba, err := z.rec.Latest(z.ctx, repository.EntityPrijave, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	redakObjave := func(korak, verzija string) {
		t.Helper()
		dog := z.prijaveNaZidu(t, ana, FiltarZida{})
		if len(dog) != 1 || !strings.HasPrefix(dog[0].Naslov, "Prijava s terena") || dog[0].VersionID != verzija {
			t.Fatalf("%s: očekivan redak objave u verziji %s:\n%s", korak, verzija, naslovi(dog))
		}
	}
	redakObjave("prije brisanja", objava.VersionID)

	if err := z.zid.ObrisiDogadjaj(z.ctx, ana, objava.VersionID); err != nil {
		t.Fatal(err)
	}
	if dog := z.prijaveNaZidu(t, ana, FiltarZida{}); len(dog) != 0 {
		t.Fatalf("nakon brisanja tuđe objave redak nosi verzija %s (urudžba %s):\n%s", dog[0].VersionID, urudzba.VersionID, naslovi(dog))
	}
	if n := razmijeni(t, z.ctx, recT, z.rec); n != 1 {
		t.Fatalf("razmjena nakon brisanja: %d verzija, očekivana obrisana objava", n)
	}
	redakObjave("nakon razmjene", objava.VersionID)

	// prijava koju je cijelu upisao ured: brisanje vrijedi i nakon razmjene
	druga := z.nova(t, models.PrijavaObavijest, "Otpad u kanalu", 43)
	druga.Klasa = "325-01/26-01/2"
	z.spremi(t, druga)
	razmijeni(t, z.ctx, z.rec, recT)
	objavaDruge := ""
	for _, d := range z.prijaveNaZidu(t, ana, FiltarZida{}) {
		if strings.HasPrefix(d.Naslov, "Obavijest s terena") {
			objavaDruge = d.VersionID
		}
	}
	if err := z.zid.ObrisiDogadjaj(z.ctx, ana, objavaDruge); err != nil {
		t.Fatal(err)
	}
	if n := razmijeni(t, z.ctx, recT, z.rec); n != 0 {
		t.Errorf("razmjena je vratila %d verzija", n)
	}
	redakObjave("obrisana obavijest ureda", objava.VersionID)
}

// Prijava objavljena prije deset dana i urudžbirana danas, pa sažimanje
// knjige s granicom od jučer: sažimanje briše verziju objave, a ostaje
// urudžba. Urudžba se upisuje tek na objavljenu prijavu, pa njezina
// verzija nije objava: redak objave nestaje sa svojom verzijom, kao i
// drugi redci zida čija je verzija sažeta, a ne vraća se na vrh zida,
// istaknut, s vremenom urudžbe, kao da je prijava tek objavljena
// (Odluke (8. 10. 2026.), pitanje 20). Ni Događanja za dan urudžbe ne
// pokazuju objavu.
func TestZidSazetaObjavaNeVracaSeSVremenomUrudzbe(t *testing.T) {
	z := noviZidPrijava(t)
	z.zid.SetVidiPrijavu(z.prijave.SmijeVidjeti)
	ana := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Ana Anić", IsGlobalAdmin: true}}

	pr := z.nova(t, models.PrijavaPrijava, "Procjeđivanje na nasipu", 42)
	objavljeno := time.Now().AddDate(0, 0, -10).Truncate(time.Second)
	if _, err := z.baza.Exec(`UPDATE record_versions SET created_at = ? WHERE entity = ? AND entity_id = ?`,
		objavljeno.UTC(), repository.EntityPrijave, pr.ID); err != nil {
		t.Fatal(err)
	}
	druga := z.nova(t, models.PrijavaObavijest, "Otpad u kanalu", 43)
	pr.Klasa = "325-01/26-01/1"
	z.spremi(t, pr) // urudžba danas

	obavijest := "Obavijest s terena " + druga.Oznaka() + "|Otpad u kanalu · Dunav|Marko Markić"
	objava := "Prijava s terena " + pr.Oznaka() + "|Procjeđivanje na nasipu · Dunav|Marko Markić"
	dog := z.prijaveNaZidu(t, ana, FiltarZida{})
	if got := naslovi(dog); got != obavijest+"\n"+objava {
		t.Fatalf("zid prije sažimanja:\n%s", got)
	}
	if !dog[1].Kad.Equal(objavljeno) || !dog[1].Vazno {
		t.Fatalf("objava prije sažimanja: %+v, objavljena %v", dog[1], objavljeno)
	}

	if n, err := z.rec.Compact(z.ctx, time.Now().Add(-24*time.Hour)); err != nil || n != 2 {
		t.Fatalf("sažimanje: %d, %v; očekivane 2 obrisane verzije (nacrt i objava prijave %s)", n, err, pr.Oznaka())
	}
	if got := naslovi(z.prijaveNaZidu(t, ana, FiltarZida{})); got != obavijest {
		t.Errorf("zid nakon sažimanja:\n%s\nočekivano:\n%s", got, obavijest)
	}
	danas := FiltarZida{Od: time.Now().Add(-time.Hour), Do: time.Now().Add(time.Hour)}
	if got := naslovi(z.prijaveNaZidu(t, ana, danas)); got != obavijest {
		t.Errorf("Događanja za dan urudžbe nakon sažimanja:\n%s\nočekivano:\n%s", got, obavijest)
	}
}

// Nacrt prenesen iz BP16 koji autor dovrši i objavi u goCOP-u: objava mu
// daje novi broj i upis na dnevni list, ali prijava zadržava oznaku
// rekonstrukcije (tako je i prije odluke, a po njoj je i stranica i PDF
// zovu prenesenom iz ranije evidencije). Zid zato ni njezinu objavu ni
// rješenje ne pokazuje nikome: prijave prenesene iz BP16 ne idu na zid
// (Odluke (8. 10. 2026.), pitanje 20), a zid ih prepoznaje samo po toj
// oznaki, kao i ostatak programa. Ista povijest bez oznake na objavi daje
// oba retka, pa kad objava u goCOP-u bude skidala oznaku, zid se ne mijenja;
// tada se mijenja prvi dio ovog testa.
func TestZidNacrtIzBP16ObjavljenUGoCOPu(t *testing.T) {
	z := noviZidPrijava(t)
	z.zid.SetVidiPrijavu(z.prijave.SmijeVidjeti)
	b, bp := "B", 34
	z.marko.Username, z.marko.IsActive = "mmarkic", true
	if err := repository.NewUserRepository(z.baza, z.rec).CreateUser(z.marko, &models.Duty{Title: "Vodočuvar", Role: models.RoleWaterGuard,
		ScopeType: models.ScopeArea, SectorID: &b, AreaID: &bp, IsPrimary: true}); err != nil {
		t.Fatal(err)
	}
	ana := &models.UserPermissions{IsGlobalAdmin: true, User: models.User{ID: uuid.New(), FullName: "Ana Anić", IsGlobalAdmin: true}}
	autor := &models.UserPermissions{User: *z.marko, AllowedSectors: map[string]bool{"B": true}, AllowedAreas: map[int]bool{34: true}}
	nacrtIzBP16 := func(naslov, izvor string) *models.PrijavaSTerena {
		p := &models.PrijavaSTerena{UserID: z.marko.ID.String(), Ime: z.marko.FullName, Sektor: "B", AreaID: 34, Vrsta: models.PrijavaPrijava,
			Naslov: naslov, Opis: "Zatečeno pri obilasku.", Datum: time.Now().In(models.Zagreb), Status: models.PrijavaNacrt,
			Rekonstrukcija: true, Izvor: izvor}
		z.spremi(t, p)
		return p
	}
	rijesi := func(p *models.PrijavaSTerena) {
		sad := time.Now()
		p.Status, p.RijesioID, p.Rijesio, p.RijesenoAt = models.PrijavaRijesena, uuid.NewString(), "Ivo Ivić", &sad
		z.spremi(t, p)
	}

	// objava kroz goCOP, pa rješenje: oznaka ostaje, zid ne pokazuje ništa
	p := nacrtIzBP16("Procjeđivanje na nasipu", "bp16:obavijesti_sa_terena:9")
	if _, _, err := z.prijave.Objavi(z.ctx, z.marko, p.ID, func(*models.PrijavaSTerena) ([]byte, error) {
		return []byte("%PDF-1.7 probni izvornik"), nil
	}); err != nil {
		t.Fatal(err)
	}
	objavljena, err := z.repo.Get(z.ctx, p.ID)
	if err != nil || objavljena == nil || !objavljena.Objavljena() || objavljena.Broj == 0 || objavljena.ListID == "" {
		t.Fatalf("objava nacrta iz BP16: %+v, %v", objavljena, err)
	}
	if !objavljena.Rekonstrukcija {
		t.Fatalf("objava u goCOP-u skida oznaku rekonstrukcije: zid sad treba pokazati objavu i rješenje, promijeni ovaj test")
	}
	rijesi(objavljena)
	for ime, perms := range map[string]*models.UserPermissions{"uprava organizacije": ana, "autor": autor} {
		if dog := z.prijaveNaZidu(t, perms, FiltarZida{}); len(dog) != 0 {
			t.Errorf("%s: prijava s oznakom rekonstrukcije na zidu:\n%s", ime, naslovi(dog))
		}
	}

	// ista povijest, a objava bez oznake: zid pokazuje objavu i rješenje
	q := nacrtIzBP16("Odron pokosa", "bp16:obavijesti_sa_terena:10")
	sad := time.Now()
	q.Rekonstrukcija, q.Status, q.Broj, q.Godina, q.ObjavljenoAt = false, models.PrijavaObjavljena, 61, sad.In(models.Zagreb).Year(), &sad
	z.spremi(t, q)
	rijesi(q)
	ocekivano := "Riješena prijava s terena|" + q.Oznaka() + ": Odron pokosa|Ivo Ivić\n" +
		"Prijava s terena " + q.Oznaka() + "|Odron pokosa|Marko Markić"
	if got := naslovi(z.prijaveNaZidu(t, autor, FiltarZida{})); got != ocekivano {
		t.Errorf("objava bez oznake rekonstrukcije:\n%s\nočekivano:\n%s", got, ocekivano)
	}
}
