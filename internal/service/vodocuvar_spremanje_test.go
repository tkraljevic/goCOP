package service

import (
	"context"
	"database/sql"
	"errors"
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

// Testovi VodocuvarService.Spremi: tko vodi dnevnik, radno vrijeme,
// predan, prenesen i arhiviran list, redni broj u knjizi, zadaci na listu i
// vodostaji upisani pri predaji. Podaci su izmišljeni: sektor P, područja 1
// i 2, letva Primjerovo i vodočuvar Pero Perić (pperic).

type okolinaVodocuvara struct {
	baza     *sql.DB
	vs       *VodocuvarService
	repo     *repository.VodocuvarRepository
	readings *repository.ReadingRepository
	letva    *models.Station
}

func novaOkolinaVodocuvara(t *testing.T) *okolinaVodocuvara {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "vodocuvar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO sectors (id, name, vgo_name, center_cop) VALUES ('P', 'Sektor P', 'VGO Primjerovo', 'COP Primjerovo')`,
		`INSERT INTO areas (id, sector_id, name, vgi_name, subcenter) VALUES (1, 'P', 'Mali sliv Primjerica', 'VGI Primjerica', ''),
			(2, 'P', 'Mali sliv Probni', 'VGI Probni', '')`,
	} {
		if _, err := baza.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rec := ledger.New(baza, "test")
	o := &okolinaVodocuvara{baza: baza, repo: repository.NewVodocuvarRepository(baza, rec), readings: repository.NewReadingRepository(baza, rec)}
	o.vs = NewVodocuvarService(o.repo, nil, "cvor-probni")
	// bez registra organizacije i klijenta prilike ostaju prazne, bez mreže
	o.vs.SetWeather(nil)
	o.letva = &models.Station{ID: uuid.New(), Code: "primjerovo", Name: "Primjerovo"}
	if err := repository.NewStationRepository(baza, rec).CreateStation(context.Background(), o.letva); err != nil {
		t.Fatal(err)
	}
	return o
}

// vdPperic je Pero Perić s danim zaduženjima
func vdPperic(duznosti ...models.Duty) *models.User {
	return &models.User{ID: uuid.New(), Username: "pperic", FullName: "Pero Perić", IsActive: true, Duties: duznosti}
}

// vdDuznost je aktivno zaduženje na području sektora P
func vdDuznost(uloga models.Role, podrucje int, primarna bool) models.Duty {
	sektor := "P"
	return models.Duty{Title: "Probno zaduženje", Role: uloga, ScopeType: models.ScopeArea, SectorID: &sektor, AreaID: &podrucje,
		IsActive: true, IsPrimary: primarna}
}

// vdVodocuvar je Pero Perić, vodočuvar na području 1
func vdVodocuvar() *models.User {
	return vdPperic(vdDuznost(models.RoleWaterGuard, 1, true))
}

// vdDan je dan u tekućoj godini; dnevnik budući dan ne odbija, pa testovi
// ne ovise o tome koji je danas dan
func vdDan(mjesec time.Month, dan int) time.Time {
	return time.Date(time.Now().In(models.Zagreb).Year(), mjesec, dan, 0, 0, 0, 0, models.Zagreb)
}

func vdUnos(opis string) UnosLista {
	return UnosLista{Od: "07:00", Do: "15:00", Opis: opis}
}

// vdZadatak zadaje otvoren zadatak vodočuvaru za dan
func (o *okolinaVodocuvara) vdZadatak(t *testing.T, u *models.User, tekst string, za time.Time) *models.Zadatak {
	t.Helper()
	z := &models.Zadatak{UserID: u.ID.String(), Sektor: "P", AreaID: 1, Tekst: tekst, Zadao: "Pero Perić", ZadanoAt: za.AddDate(0, 0, -1),
		Za: za, Status: models.ZadatakOtvoren}
	if err := o.repo.SaveZadatak(context.Background(), z); err != nil {
		t.Fatal(err)
	}
	return z
}

func (o *okolinaVodocuvara) vdZadatakIzBaze(t *testing.T, id string) *models.Zadatak {
	t.Helper()
	z, err := o.repo.GetZadatak(context.Background(), id)
	if err != nil || z == nil {
		t.Fatalf("zadatak %s: %v %v", id, z, err)
	}
	return z
}

func (o *okolinaVodocuvara) vdListIzBaze(t *testing.T, u *models.User, dan time.Time) *models.VodocuvarskiList {
	t.Helper()
	l, err := o.repo.ZaDan(context.Background(), u.ID.String(), dan)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func vdNaListu(l *models.VodocuvarskiList, id string) *models.ZadatakNaListu {
	for i := range l.Zadaci {
		if l.Zadaci[i].ID == id {
			return &l.Zadaci[i]
		}
	}
	return nil
}

func vdGreska(t *testing.T, err error, sadrzi string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sadrzi) {
		t.Fatalf("očekivana greška s %q, dobiveno %v", sadrzi, err)
	}
}

// Dnevnik vodi samo tko ima aktivno zaduženje vodočuvara; rukovoditelj,
// strojar i bivši vodočuvar ga nemaju, a bez osobe nema ni upisa.
func TestDnevnikVodiSamoVodocuvar(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	dan := vdDan(time.March, 10)

	if _, err := o.vs.Spremi(ctx, nil, dan, vdUnos(""), false); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("bez osobe: %v", err)
	}
	bivsi := vdDuznost(models.RoleWaterGuard, 1, true)
	bivsi.IsActive = false
	for naziv, u := range map[string]*models.User{
		"rukovoditelj područja": vdPperic(vdDuznost(models.RoleAreaLeader, 1, true)),
		"strojar":               vdPperic(vdDuznost(models.RoleMachinist, 1, true)),
		"bivši vodočuvar":       vdPperic(bivsi),
		"bez zaduženja":         vdPperic(),
	} {
		if _, err := o.vs.Spremi(ctx, u, dan, vdUnos(""), false); !errors.Is(err, ErrNijeVodocuvar) {
			t.Errorf("%s: %v", naziv, err)
		}
		if VodiDnevnik(u) {
			t.Errorf("%s vodi dnevnik", naziv)
		}
	}
	if VodiDnevnik(nil) {
		t.Error("nitko vodi dnevnik")
	}
	if n, _ := o.repo.List(ctx, repository.FiltarListova{}); len(n) != 0 {
		t.Errorf("odbijeni upisi ostavili su %d listova", len(n))
	}
}

// List nosi sektor i područje zaduženja vodočuvara: glavnog ako ga ima,
// inače prvog aktivnog. Zaduženja drugih uloga ne računaju se.
func TestListNosiPodrucjeZaduzenjaVodocuvara(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()

	glavno := vdPperic(vdDuznost(models.RoleAreaLeader, 1, true), vdDuznost(models.RoleWaterGuard, 1, false), vdDuznost(models.RoleWaterGuard, 2, true))
	l, err := o.vs.Spremi(ctx, glavno, vdDan(time.March, 10), vdUnos(""), false)
	if err != nil {
		t.Fatal(err)
	}
	if l.Sektor != "P" || l.AreaID != 2 {
		t.Errorf("glavno zaduženje: %s/%d, očekivano P/2", l.Sektor, l.AreaID)
	}
	if l.Ime != "Pero Perić" || l.UserID != glavno.ID.String() || l.Cvor != "cvor-probni" {
		t.Errorf("list: ime %q, osoba %s, čvor %q", l.Ime, l.UserID, l.Cvor)
	}

	neaktivno := vdDuznost(models.RoleWaterGuard, 2, true)
	neaktivno.IsActive = false
	prvo := vdPperic(neaktivno, vdDuznost(models.RoleWaterGuard, 1, false), vdDuznost(models.RoleWaterGuard, 2, false))
	l, err = o.vs.Spremi(ctx, prvo, vdDan(time.March, 10), vdUnos(""), false)
	if err != nil {
		t.Fatal(err)
	}
	if l.AreaID != 1 {
		t.Errorf("bez aktivnog glavnog zaduženja list je na području %d, očekivano prvo aktivno (1)", l.AreaID)
	}
}

// Radno vrijeme upisuje se kao sati i minute. Pogrešan upis odbija cijeli
// list, a razmaci se brišu iz svih polja.
func TestRadnoVrijemeNaListu(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)

	for _, k := range []struct{ od, do string }{
		{"", "15:00"}, {"07:00", ""}, {"7", "15:00"}, {"07:00", "15h"}, {"24:00", "15:00"}, {"07:60", "15:00"}, {"07:00:00", "15:00"},
	} {
		unos := vdUnos("obilazak nasipa")
		unos.Od, unos.Do = k.od, k.do
		_, err := o.vs.Spremi(ctx, u, dan, unos, false)
		vdGreska(t, err, "radno vrijeme upišite kao sate i minute")
	}
	if l := o.vdListIzBaze(t, u, dan); l != nil {
		t.Fatalf("odbijeni upis spremio je list %+v", l)
	}

	l, err := o.vs.Spremi(ctx, u, dan, UnosLista{Od: " 06:30 ", Do: "14:30\t", Prilike: "  sunčano ", Naredbe: " pregled ustave ",
		Opis: "\nobilazak nasipa\n", Zapazanja: "  procjedna voda kod rkm 12  "}, false)
	if err != nil {
		t.Fatal(err)
	}
	if l.Od != "06:30" || l.Do != "14:30" || l.Prilike != "sunčano" || l.Naredbe != "pregled ustave" ||
		l.Opis != "obilazak nasipa" || l.Zapazanja != "procjedna voda kod rkm 12" {
		t.Errorf("polja nisu očišćena: %+v", l)
	}
	if l.Sati() != 8 {
		t.Errorf("sati: %v", l.Sati())
	}
	// jednoznamenkasti sat prolazi i sprema se kako je upisan
	unos := vdUnos("")
	unos.Od = "7:00"
	l, err = o.vs.Spremi(ctx, u, dan, unos, false)
	if err != nil {
		t.Fatal(err)
	}
	if l.Od != "7:00" {
		t.Errorf("od: %q", l.Od)
	}
	// noćni rad: svršetak prije početka znači rad preko ponoći
	unos = UnosLista{Od: "22:00", Do: "06:00"}
	if l, err = o.vs.Spremi(ctx, u, dan, unos, false); err != nil {
		t.Fatal(err)
	}
	if l.Sati() != 8 {
		t.Errorf("noćni rad: %v sati", l.Sati())
	}
}

// Nov list dobiva radno vrijeme iz postavki organizacije, a bez postavki
// 07:30–15:30. Upis ga ipak mora imati.
func TestPripremaListaRadnoVrijeme(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()

	l, err := o.vs.Pripremi(ctx, u, vdDan(time.March, 10))
	if err != nil {
		t.Fatal(err)
	}
	if l.Od != "07:30" || l.Do != "15:30" || l.ID != "" || l.Broj != 0 {
		t.Errorf("zadani list: %+v", l)
	}
	o.vs.SetRadnoVrijeme(func(context.Context) (string, string) { return "06:00", "" })
	if l, _ = o.vs.Pripremi(ctx, u, vdDan(time.March, 10)); l.Od != "07:30" || l.Do != "15:30" {
		t.Errorf("napola postavljeno radno vrijeme: %s–%s", l.Od, l.Do)
	}
	o.vs.SetRadnoVrijeme(func(context.Context) (string, string) { return "06:00", "14:00" })
	if l, _ = o.vs.Pripremi(ctx, u, vdDan(time.March, 10)); l.Od != "06:00" || l.Do != "14:00" {
		t.Errorf("radno vrijeme organizacije: %s–%s", l.Od, l.Do)
	}
	// Spremi ne nasljeđuje radno vrijeme iz pripreme: prazan upis se odbija
	_, err = o.vs.Spremi(ctx, u, vdDan(time.March, 10), UnosLista{Opis: "obilazak"}, false)
	vdGreska(t, err, "radno vrijeme")
	// pripremljen list nije spremljen
	if l := o.vdListIzBaze(t, u, vdDan(time.March, 10)); l != nil {
		t.Errorf("priprema je spremila list")
	}
	if _, err := o.vs.Pripremi(ctx, vdPperic(), vdDan(time.March, 10)); !errors.Is(err, ErrNijeVodocuvar) {
		t.Errorf("priprema bez zaduženja: %v", err)
	}
}

// Svaki list nosi redni broj od prvog spremanja, kao stranica u knjizi:
// izmjena ga ne mijenja, nova stranica dobiva sljedeći, a knjiga svake
// godine počinje od 1. Broj prati redoslijed upisa, ne datum.
func TestRedniBrojLista(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	spremi := func(dan time.Time) *models.VodocuvarskiList {
		t.Helper()
		l, err := o.vs.Spremi(ctx, u, dan, vdUnos(""), false)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	if l := spremi(vdDan(time.March, 10)); l.Broj != 1 || l.ID == "" {
		t.Errorf("prvi list: broj %d, id %q", l.Broj, l.ID)
	}
	prvi := spremi(vdDan(time.March, 10))
	if prvi.Broj != 1 {
		t.Errorf("izmjena prvog lista promijenila je broj u %d", prvi.Broj)
	}
	if l := spremi(vdDan(time.March, 12)); l.Broj != 2 {
		t.Errorf("drugi list: %d", l.Broj)
	}
	// raniji dan upisan kasnije dobiva sljedeći broj
	if l := spremi(vdDan(time.March, 11)); l.Broj != 3 {
		t.Errorf("list od 11. 3. upisan treći: broj %d", l.Broj)
	}
	// drugi vodočuvar ima svoju knjigu
	drugi := vdVodocuvar()
	if l, err := o.vs.Spremi(ctx, drugi, vdDan(time.March, 10), vdUnos(""), false); err != nil || l.Broj != 1 {
		t.Errorf("knjiga drugog vodočuvara: %v %v", l, err)
	}
	// iduće godine knjiga počinje ispočetka
	iduce := vdDan(time.March, 10).AddDate(1, 0, 0)
	if l := spremi(iduce); l.Broj != 1 {
		t.Errorf("iduća godina: broj %d", l.Broj)
	}
	listovi, err := o.vs.Moji(ctx, u, vdDan(time.March, 10).Year())
	if err != nil || len(listovi) != 3 {
		t.Fatalf("listovi u godini: %d %v", len(listovi), err)
	}
}

// Predan list više se ne mijenja; ni prenesen iz ranije evidencije, ni
// list iz arhivirane knjige prošle godine.
func TestZakljucenListSeNeMijenja(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)

	predan, err := o.vs.Spremi(ctx, u, dan, vdUnos("obilazak nasipa"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !predan.Predan() || predan.PredanoAt.After(time.Now()) {
		t.Fatalf("list nije predan: %+v", predan.PredanoAt)
	}
	_, err = o.vs.Spremi(ctx, u, dan, vdUnos("drugi opis"), false)
	vdGreska(t, err, "je predan i više se ne mijenja")
	_, err = o.vs.Spremi(ctx, u, dan, vdUnos("drugi opis"), true)
	vdGreska(t, err, "je predan i više se ne mijenja")
	if l := o.vdListIzBaze(t, u, dan); l.Opis != "obilazak nasipa" || !l.PredanoAt.Equal(*predan.PredanoAt) {
		t.Errorf("predan list promijenjen: %q %v", l.Opis, l.PredanoAt)
	}

	prenesen := &models.VodocuvarskiList{UserID: u.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: vdDan(time.March, 11),
		Opis: "obilazak iz stare evidencije", Rekonstrukcija: true, Izvor: "probni-zapis-1"}
	if err := o.repo.Save(ctx, prenesen); err != nil {
		t.Fatal(err)
	}
	_, err = o.vs.Spremi(ctx, u, vdDan(time.March, 11), vdUnos("novi opis"), false)
	vdGreska(t, err, "prenesen je iz ranije evidencije i ne mijenja se")
	if l := o.vdListIzBaze(t, u, vdDan(time.March, 11)); l.Opis != "obilazak iz stare evidencije" || l.Predan() {
		t.Errorf("prenesen list promijenjen: %+v", l)
	}

	lani := vdDan(time.March, 10).AddDate(-1, 0, 0)
	_, err = o.vs.Spremi(ctx, u, lani, vdUnos("kasni upis"), false)
	vdGreska(t, err, "je arhivirana istekom godine")
	if l := o.vdListIzBaze(t, u, lani); l != nil {
		t.Errorf("upis u arhiviranu knjigu: %+v", l)
	}
	// i 31. prosinca prošle godine je arhiviran, a 1. siječnja nije
	_, err = o.vs.Spremi(ctx, u, vdDan(time.January, 1).Add(-time.Minute), vdUnos(""), false)
	vdGreska(t, err, "arhivirana")
	if _, err := o.vs.Spremi(ctx, u, vdDan(time.January, 1), vdUnos(""), false); err != nil {
		t.Errorf("1. siječnja: %v", err)
	}
}

// Predaja traži opis rada. Odbijena predaja ne sprema ništa: ni nov list,
// ni izmjene postojećeg nacrta.
func TestPredajaTraziOpis(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)

	_, err := o.vs.Spremi(ctx, u, dan, vdUnos("   "), true)
	vdGreska(t, err, "prije predaje upišite opis radnih aktivnosti")
	if l := o.vdListIzBaze(t, u, dan); l != nil {
		t.Fatalf("odbijena predaja spremila je list %+v", l)
	}

	if _, err := o.vs.Spremi(ctx, u, dan, UnosLista{Od: "07:00", Do: "15:00", Zapazanja: "nacrt"}, false); err != nil {
		t.Fatal(err)
	}
	_, err = o.vs.Spremi(ctx, u, dan, UnosLista{Od: "08:00", Do: "16:00", Zapazanja: "izmjena"}, true)
	vdGreska(t, err, "opis radnih aktivnosti")
	l := o.vdListIzBaze(t, u, dan)
	if l.Predan() || l.Zapazanja != "nacrt" || l.Od != "07:00" {
		t.Errorf("odbijena predaja promijenila je nacrt: %+v", l)
	}
	// nacrt bez opisa smije se spremiti
	if l, err := o.vs.Spremi(ctx, u, dan, vdUnos(""), false); err != nil || l.Predan() {
		t.Errorf("nacrt bez opisa: %v", err)
	}
	if l, err := o.vs.Spremi(ctx, u, dan, vdUnos("obilazak nasipa"), true); err != nil || !l.Predan() || l.Broj != 1 {
		t.Errorf("predaja s opisom: %v %v", l, err)
	}
}

// Otvoreni zadaci rukovoditelja stoje na listu od dana za koji su zadani.
// Na nacrtu vodočuvar označava stanje, a u evidenciji zadataka zadatak se
// zaključuje tek predajom lista.
func TestZadaciNaListuIPredaja(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)

	ustava := o.vdZadatak(t, u, "pregledati ustavu", dan)
	nasip := o.vdZadatak(t, u, "obići nasip", dan)
	propust := o.vdZadatak(t, u, "očistiti propust", dan)
	kasniji := o.vdZadatak(t, u, "pregled crpne stanice", dan.AddDate(0, 0, 2))
	tudji := o.vdZadatak(t, vdVodocuvar(), "tuđi zadatak", dan)

	l, err := o.vs.Spremi(ctx, u, dan, vdUnos(""), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Zadaci) != 3 || vdNaListu(l, kasniji.ID) != nil || vdNaListu(l, tudji.ID) != nil {
		t.Fatalf("zadaci na listu: %+v", l.Zadaci)
	}
	for _, z := range l.Zadaci {
		if z.Status != models.ZadatakOtvoren || z.Zadao != "Pero Perić" {
			t.Errorf("zadatak na novom listu: %+v", z)
		}
	}

	// obavljen i odbačen zadatak traže upis što je napravljeno ili zašto nije
	for _, status := range []string{models.ZadatakObavljen, models.ZadatakOdbacen} {
		unos := vdUnos("")
		unos.Zadaci = map[string]UnosZadatka{ustava.ID: {Status: status, Obavljeno: "  "}}
		_, err := o.vs.Spremi(ctx, u, dan, unos, false)
		vdGreska(t, err, "uz zadatak „pregledati ustavu” upišite što je napravljeno ili zašto nije")
	}

	unos := vdUnos("obilazak područja")
	unos.Zadaci = map[string]UnosZadatka{
		ustava.ID:      {Status: models.ZadatakObavljen, Obavljeno: " ustava pregledana "},
		nasip.ID:       {Status: models.ZadatakOdbacen, Obavljeno: "nasip pod vodom"},
		propust.ID:     {Status: "NEPOZNATO", Obavljeno: "nije stiglo"},
		"nema-zadatka": {Status: models.ZadatakObavljen, Obavljeno: "ne postoji"},
		kasniji.ID:     {Status: models.ZadatakObavljen, Obavljeno: "unaprijed"},
	}
	l, err = o.vs.Spremi(ctx, u, dan, unos, false)
	if err != nil {
		t.Fatal(err)
	}
	if z := vdNaListu(l, ustava.ID); z.Status != models.ZadatakObavljen || z.Obavljeno != "ustava pregledana" {
		t.Errorf("obavljen: %+v", z)
	}
	if z := vdNaListu(l, nasip.ID); z.Status != models.ZadatakOdbacen || z.Obavljeno != "nasip pod vodom" {
		t.Errorf("odbačen: %+v", z)
	}
	// nepoznato stanje znači da zadatak ostaje otvoren, s upisanim razlogom
	if z := vdNaListu(l, propust.ID); z.Status != models.ZadatakOtvoren || z.Obavljeno != "nije stiglo" {
		t.Errorf("nepoznato stanje: %+v", z)
	}
	// zadatak kojeg nema na listu ne dolazi s obrasca
	if len(l.Zadaci) != 3 || vdNaListu(l, kasniji.ID) != nil {
		t.Errorf("zadaci nakon upisa: %+v", l.Zadaci)
	}
	// nacrt ne zaključuje zadatke u evidenciji
	if z := o.vdZadatakIzBaze(t, ustava.ID); z.Status != models.ZadatakOtvoren || z.ListID != "" {
		t.Errorf("nacrt je zaključio zadatak: %+v", z)
	}
	// ponovno spremanje bez obrasca zadataka čuva označeno stanje
	l, err = o.vs.Spremi(ctx, u, dan, vdUnos("obilazak područja"), false)
	if err != nil {
		t.Fatal(err)
	}
	if z := vdNaListu(l, ustava.ID); z == nil || z.Status != models.ZadatakObavljen {
		t.Errorf("stanje zadatka izgubljeno: %+v", l.Zadaci)
	}

	l, err = o.vs.Spremi(ctx, u, dan, unos, true)
	if err != nil {
		t.Fatal(err)
	}
	z := o.vdZadatakIzBaze(t, ustava.ID)
	if z.Status != models.ZadatakObavljen || z.Obavljeno != "ustava pregledana" || z.ListID != l.ID || z.ObavljenoAt == nil {
		t.Errorf("obavljen zadatak u evidenciji: %+v", z)
	}
	if z := o.vdZadatakIzBaze(t, nasip.ID); z.Status != models.ZadatakOdbacen || z.ListID != l.ID {
		t.Errorf("odbačen zadatak u evidenciji: %+v", z)
	}
	// otvoren zadatak ostaje otvoren i prenosi se na sljedeći list
	if z := o.vdZadatakIzBaze(t, propust.ID); z.Status != models.ZadatakOtvoren || z.ListID != "" || z.Obavljeno != "" {
		t.Errorf("otvoren zadatak u evidenciji: %+v", z)
	}
	sutra, err := o.vs.Pripremi(ctx, u, dan.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(sutra.Zadaci) != 1 || sutra.Zadaci[0].ID != propust.ID || sutra.Zadaci[0].Obavljeno != "" {
		t.Errorf("sljedeći list: %+v", sutra.Zadaci)
	}
	prekosutra, _ := o.vs.Pripremi(ctx, u, dan.AddDate(0, 0, 2))
	if len(prekosutra.Zadaci) != 2 || vdNaListu(prekosutra, kasniji.ID) == nil {
		t.Errorf("planirani zadatak na svom danu: %+v", prekosutra.Zadaci)
	}
}

// Predaja ne prolazi dok neobavljeni zadatak nema obrazloženje.
func TestPredajaTraziObrazlozenjeNeobavljenogZadatka(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)
	z := o.vdZadatak(t, u, "pregledati ustavu", dan)

	_, err := o.vs.Spremi(ctx, u, dan, vdUnos("obilazak"), true)
	vdGreska(t, err, "zadatak „pregledati ustavu” nije obavljen: prije predaje obrazložite zašto")
	unos := vdUnos("obilazak")
	unos.Zadaci = map[string]UnosZadatka{z.ID: {Status: models.ZadatakOtvoren, Obavljeno: " \t "}}
	_, err = o.vs.Spremi(ctx, u, dan, unos, true)
	vdGreska(t, err, "nije obavljen")
	if l := o.vdListIzBaze(t, u, dan); l != nil {
		t.Fatalf("odbijena predaja spremila je list")
	}
	unos.Zadaci[z.ID] = UnosZadatka{Status: models.ZadatakOtvoren, Obavljeno: "nije bilo vremena"}
	l, err := o.vs.Spremi(ctx, u, dan, unos, true)
	if err != nil {
		t.Fatal(err)
	}
	if nl := vdNaListu(l, z.ID); nl.Status != models.ZadatakOtvoren || nl.Obavljeno != "nije bilo vremena" {
		t.Errorf("obrazloženje na listu: %+v", nl)
	}
	if z := o.vdZadatakIzBaze(t, z.ID); !z.Otvoren() || z.ListID != "" {
		t.Errorf("neobavljen zadatak zaključen: %+v", z)
	}
}

// Zadatak zaključen na listu ostaje na tom listu i kad ga je u evidenciji
// već zaključio drugi list; evidenciju tada ne mijenja.
func TestZadatakZakljucenNaDvaLista(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	prvi, drugi := vdDan(time.March, 10), vdDan(time.March, 11)
	z := o.vdZadatak(t, u, "pregledati ustavu", prvi)

	// nacrt prvog dana: obavljen
	unos := vdUnos("obilazak")
	unos.Zadaci = map[string]UnosZadatka{z.ID: {Status: models.ZadatakObavljen, Obavljeno: "pregledana"}}
	if _, err := o.vs.Spremi(ctx, u, prvi, unos, false); err != nil {
		t.Fatal(err)
	}
	// drugi dan isti zadatak stoji otvoren (evidencija ga još drži otvorenim)
	// i predajom se odbacuje
	unos.Zadaci = map[string]UnosZadatka{z.ID: {Status: models.ZadatakOdbacen, Obavljeno: "ustava srušena"}}
	l2, err := o.vs.Spremi(ctx, u, drugi, unos, true)
	if err != nil {
		t.Fatal(err)
	}
	// predaja prvog lista: zadatak ostaje na njemu obavljen, a evidencija
	// ostaje kako ju je zaključio drugi list
	l1, err := o.vs.Spremi(ctx, u, prvi, vdUnos("obilazak"), true)
	if err != nil {
		t.Fatal(err)
	}
	if nl := vdNaListu(l1, z.ID); nl == nil || nl.Status != models.ZadatakObavljen || nl.Obavljeno != "pregledana" {
		t.Errorf("zadatak na prvom listu: %+v", l1.Zadaci)
	}
	if ev := o.vdZadatakIzBaze(t, z.ID); ev.Status != models.ZadatakOdbacen || ev.ListID != l2.ID || ev.Obavljeno != "ustava srušena" {
		t.Errorf("evidencija: %+v", ev)
	}
}

// Vodostaji koje je vodočuvar taj dan upisao ulaze na list pri predaji;
// nacrt ih ne osvježava.
func TestOcitanjaNaListuPriPredaji(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)
	upisi := func(kad time.Time, cm *int, korisnik string) {
		t.Helper()
		if err := o.readings.Create(ctx, &models.Reading{ID: uuid.New(), StationID: o.letva.ID.String(), MeasuredAt: kad.UTC(), LevelCm: cm,
			UserID: korisnik, Source: models.ReadingSourceManual}); err != nil {
			t.Fatal(err)
		}
	}
	cm := func(v int) *int { return &v }

	l, err := o.vs.Spremi(ctx, u, dan, vdUnos(""), false)
	if err != nil {
		t.Fatal(err)
	}
	if l.Ocitanja != "" {
		t.Errorf("očitanja bez upisa: %q", l.Ocitanja)
	}
	upisi(dan.Add(8*time.Hour+15*time.Minute), cm(250), u.ID.String())
	upisi(dan.Add(23*time.Hour+50*time.Minute), cm(-30), u.ID.String())
	upisi(dan.Add(12*time.Hour), nil, u.ID.String())                        // bez vodostaja
	upisi(dan.Add(9*time.Hour), cm(400), uuid.NewString())                  // tuđe
	upisi(dan.Add(-10*time.Minute), cm(100), u.ID.String())                 // prethodni dan
	upisi(dan.AddDate(0, 0, 1).Add(10*time.Minute), cm(110), u.ID.String()) // sljedeći dan

	if l, err = o.vs.Spremi(ctx, u, dan, vdUnos(""), false); err != nil || l.Ocitanja != "" {
		t.Errorf("nacrt je osvježio očitanja: %q %v", l.Ocitanja, err)
	}
	l, err = o.vs.Spremi(ctx, u, dan, vdUnos("očitanje letve"), true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Primjerovo: +250 cm (08:15)\nPrimjerovo: -30 cm (23:50)"; l.Ocitanja != want {
		t.Errorf("očitanja na listu:\n%q\nočekivano\n%q", l.Ocitanja, want)
	}
	if b := o.vdListIzBaze(t, u, dan); b.Ocitanja != l.Ocitanja {
		t.Errorf("spremljena očitanja: %q", b.Ocitanja)
	}
}

// Kad upis lista u bazu ne uspije, Spremi vraća grešku i list ne postoji.
func TestNeuspjeliUpisLista(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)
	if _, err := o.baza.Exec(`CREATE TRIGGER vd_bez_upisa BEFORE INSERT ON vodocuvarski_listovi BEGIN SELECT RAISE(ABORT, 'upis zabranjen'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := o.vs.Spremi(ctx, u, dan, vdUnos("obilazak"), true)
	vdGreska(t, err, "upis zabranjen")
	if l := o.vdListIzBaze(t, u, dan); l != nil {
		t.Errorf("list postoji: %+v", l)
	}
}

// Predaja nije jedna transakcija: list se spremi kao predan prije nego što
// se zadaci zaključe u evidenciji. Ako zaključivanje ne uspije, Spremi
// javlja grešku, a list ostaje predan i zadatak otvoren. Test bilježi
// zatečeno ponašanje (vidi „Sumnjivo ponašanje” u opisu PR-a).
func TestPredajaBezZakljucenogZadatka(t *testing.T) {
	o := novaOkolinaVodocuvara(t)
	ctx := context.Background()
	u := vdVodocuvar()
	dan := vdDan(time.March, 10)
	z := o.vdZadatak(t, u, "pregledati ustavu", dan)
	if _, err := o.baza.Exec(`CREATE TRIGGER vd_bez_zakljucivanja BEFORE UPDATE ON vodocuvarski_zadaci BEGIN SELECT RAISE(ABORT, 'zadatak zaključan'); END`); err != nil {
		t.Fatal(err)
	}
	unos := vdUnos("obilazak")
	unos.Zadaci = map[string]UnosZadatka{z.ID: {Status: models.ZadatakObavljen, Obavljeno: "pregledana"}}
	_, err := o.vs.Spremi(ctx, u, dan, unos, true)
	vdGreska(t, err, "zadatak zaključan")

	l := o.vdListIzBaze(t, u, dan)
	if l == nil || !l.Predan() {
		t.Fatalf("list nakon greške: %+v", l)
	}
	if nl := vdNaListu(l, z.ID); nl == nil || nl.Status != models.ZadatakObavljen {
		t.Errorf("zadatak na predanom listu: %+v", l.Zadaci)
	}
	if ev := o.vdZadatakIzBaze(t, z.ID); !ev.Otvoren() || ev.ListID != "" {
		t.Errorf("evidencija: %+v", ev)
	}
	// ponovni pokušaj više ne ide: list je predan
	if _, err := o.vs.Spremi(ctx, u, dan, unos, true); err == nil || !strings.Contains(err.Error(), "predan") {
		t.Errorf("ponovna predaja: %v", err)
	}
	if _, err := o.baza.Exec(`DROP TRIGGER vd_bez_zakljucivanja`); err != nil {
		t.Fatal(err)
	}
	// zadatak se zato pojavljuje i na sljedećem listu
	sutra, err := o.vs.Pripremi(ctx, u, dan.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if vdNaListu(sutra, z.ID) == nil {
		t.Errorf("zadatak nije na sljedećem listu: %+v", sutra.Zadaci)
	}
}
