package bp16

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// Zaključava uvoz obilazaka terena iz stare evidencije Baranje u zadatke i
// rekonstruirane dnevne listove: tko je zadao i kome, vrijeme dana iz
// evidencije ili arhive, odgovor ili objašnjenje bez odgovora, ucrtani
// obuhvat, fotografije i ponovni uvoz.

// probnaSlika je mala PNG slika za fotografije obilaska
func probnaSlika(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.RGBA{30, 90, 160, 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// dnevnikObilazaka diže bazu s dnevnikom vodočuvara i spremištem sadržaja
// za fotografije
func dnevnikObilazaka(t *testing.T) *repository.VodocuvarRepository {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "obilasci.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	spremiste, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	prijasnje := repository.Spremiste()
	repository.SetSpremiste(spremiste)
	t.Cleanup(func() {
		repository.SetSpremiste(prijasnje)
		_ = spremiste.Zatvori()
	})
	return repository.NewVodocuvarRepository(baza, ledger.New(baza, "cvor-b"))
}

func oznakaZadatka(id int) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("gocop/bp16:evidencije_obilaska:%d", id))).String()
}

// izvorObilazaka je stara evidencija s dva vodočuvara i sedam obilazaka
func izvorObilazaka() probniIzvor {
	return probniIzvor{zbirke: map[string][]json.RawMessage{
		"users": raw(
			`{"id":"u-1","first_name":"Ana","last_name":"Anić"}`,
			`{"id":"u-2","first_name":" Ivo","last_name":"Ivić "}`,
			`{"id":"u-3","first_name":"Marko","last_name":"Marić"}`,
			`{"id":"u-9","first_name":"Netko","last_name":"Nepoznat"}`,
			`"nije korisnik"`,
		),
		"vrijeme_za_list": raw(
			`{"datum":"2025-05-10","vrijeme_opisno":" Sunčano,  18 °C ","vodostaji_opisno":"Batina\t310 cm"}`,
			`{"datum":"2025-05-10","vrijeme_opisno":"drugi zapis istog dana"}`,
			`{"datum":"","vrijeme_opisno":"bez datuma"}`,
			`{"datum":"2025-05-12","vrijeme_opisno":"","vodostaji_opisno":"samo vodostaji"}`,
			`"nečitljivo"`,
		),
		"evidencije_obilaska": raw(
			// redoslijed nije po datumu: uvoz ih slaže po datumu, pa po broju
			`{"id_obilasci":6,"status":"arhivirano","date_created":"jučer","user_created":"u-3","dodijeljeno":"u-2","datum":"2025-05-11","opis_aktivnosti":"Obilazak kanala","napomena":""}`,
			`{"id_obilasci":2,"status":"Arhivirano ","date_created":"2025-05-09T06:00:00Z","user_created":"u-3","dodijeljeno":"u-1","datum":"2025-05-10","opis_aktivnosti":"Pregled ustave","napomena":"  ","udaljenost":3,"lokacija":null}`,
			`{"id_obilasci":1,"status":"arhivirano","date_created":"2025-05-09T05:30:00Z","user_created":"u-3","dodijeljeno":"u-1","datum":"2025-05-10","vrijeme_od":"07:00:00","vrijeme_do":"09:30:00","opis_aktivnosti":"  Obilazak  nasipa\tDraž ","napomena":"Obiđeno,  bez oštećenja.","udaljenost":"12,5","lokacija":{"type":"LineString","coordinates":[[18.7,45.8],[18.8,45.9]]},"foto_1":"f1","foto_2":"f-nema","foto_3":"f-kriva"}`,
			`{"id_obilasci":3,"status":"u planu","user_created":"u-3","dodijeljeno":"u-1","datum":"2025-05-13","opis_aktivnosti":"Još nije obavljeno"}`,
			`{"id_obilasci":4,"status":"arhivirano","user_created":"u-3","dodijeljeno":"u-9","datum":"2025-05-10","opis_aktivnosti":"Tuđi obilazak"}`,
			`{"id_obilasci":5,"status":"arhivirano","user_created":"u-3","dodijeljeno":"u-1","datum":"10.05.2025.","opis_aktivnosti":"Krivi datum"}`,
			`{"id_obilasci":7,"status":"arhivirano","user_created":"u-3","dodijeljeno":"u-2","datum":"2025-05-12","opis_aktivnosti":"Obilazak crpne stanice","napomena":"Sve u redu."}`,
		),
	}}
}

// Arhivirani obilasci postaju zadaci na rekonstruiranim listovima: dva
// obilaska istog dana dijele list, vrijeme dana dolazi iz evidencije, a kad
// ga ondje nema iz arhive; obilazak u planu, nepoznat vodočuvar i krivi
// datum se ne upisuju. Suhi prolaz broji isto, a ne upisuje ništa; ponovni
// uvoz preskače upisano.
func TestUvozObilazaka(t *testing.T) {
	repo := dnevnikObilazaka(t)
	ctx := context.Background()
	slika := probnaSlika(t, 40, 30)
	var dohvaceno []string
	var zapisnik []string
	deps := ObilasciDeps{
		Vodocuvar: repo,
		Korisnici: map[string]KorisnikUvoza{
			"Ana Anić": {ID: "g-ana", Ime: "Ana Anić", AreaID: 16},
			"Ivo Ivić": {ID: "g-ivo", Ime: "Ivo Ivić", Sektor: "C"},
		},
		// nula ne mijenja područje iz zaduženja
		Podrucja: map[string]int{"Ana Anić": 0, "Ivo Ivić": 34},
		Sektor:   "B", Cvor: "cvor-b",
		Datoteka: func(_ context.Context, id, upit string) ([]byte, error) {
			dohvaceno = append(dohvaceno, id+"?"+upit)
			switch id {
			case "f1":
				return slika, nil
			case "f-kriva":
				return []byte("nije slika"), nil
			}
			return nil, errors.New("Directus odgovorio 404")
		},
		SamoArhivirane: true,
		Meteo: func(_ context.Context, dan time.Time) string {
			if dan.Day() == 11 {
				return "kiša, 12 °C · Open-Meteo, naknadno"
			}
			return ""
		},
		Log: func(f string, a ...any) { zapisnik = append(zapisnik, fmt.Sprintf(f, a...)) },
	}

	deps.DryRun = true
	rep, err := RunObilasci(ctx, izvorObilazaka(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || rep.Ukupno != 7 || rep.Upisano != 4 || rep.Preskoceno != 1 || rep.BezKorisnika != 1 || rep.Listova != 0 ||
		rep.SOdgovorom != 2 || rep.BezOdgovora != 2 || rep.Slika != 1 || rep.SObuhvatom != 1 || rep.VrijemeIzEvidencije != 0 {
		t.Fatalf("suhi prolaz: %+v", rep)
	}
	if rep.Nepoznati["Netko Nepoznat"] != 1 || rep.Nepoznati["datum 10.05.2025."] != 1 || len(rep.Nepoznati) != 2 {
		t.Errorf("nepoznati: %v", rep.Nepoznati)
	}
	for _, id := range []int{1, 2, 6, 7} {
		if z, _ := repo.ZadatakPoIzvoru(ctx, fmt.Sprintf("bp16:evidencije_obilaska:%d", id)); z != nil {
			t.Fatalf("suhi prolaz upisao zadatak %d", id)
		}
	}
	if l, _ := repo.ZaDan(ctx, "g-ana", time.Date(2025, 5, 10, 0, 0, 0, 0, models.Zagreb)); l != nil {
		t.Fatal("suhi prolaz upisao list")
	}
	// izvorne datoteke, bez Directusove pretvorbe
	if strings.Join(dohvaceno, " ") != "f1? f-nema? f-kriva?" {
		t.Errorf("dohvaćene fotografije: %v", dohvaceno)
	}
	if z := strings.Join(zapisnik, "\n"); !strings.Contains(z, "obilazak 1: fotografija f-nema: Directus odgovorio 404") ||
		!strings.Contains(z, "obilazak 1: fotografija f-kriva: slika nije čitljiv JPEG ili PNG") {
		t.Errorf("zapisnik:\n%s", z)
	}

	deps.DryRun = false
	rep, err = RunObilasci(ctx, izvorObilazaka(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DryRun || rep.Upisano != 4 || rep.Listova != 3 || rep.SOdgovorom != 2 || rep.BezOdgovora != 2 || rep.Slika != 1 ||
		rep.VrijemeIzEvidencije != 1 || rep.VrijemeIzArhive != 1 || rep.Postoje != 0 {
		t.Fatalf("uvoz: %+v", rep)
	}

	// Ana, 10. 5.: oba obilaska na jednom listu, vrijeme iz evidencije
	anin, err := repo.ZaDan(ctx, "g-ana", time.Date(2025, 5, 10, 0, 0, 0, 0, models.Zagreb))
	if err != nil || anin == nil {
		t.Fatalf("Anin list: %v", err)
	}
	if !anin.Rekonstrukcija || anin.Izvor != "bp16:evidencije_obilaska" || anin.Cvor != "cvor-b" || anin.Sektor != "B" || anin.AreaID != 16 ||
		anin.Ime != "Ana Anić" || anin.Prilike != "Sunčano, 18 °C" || anin.Ocitanja != "Batina 310 cm" {
		t.Errorf("Anin list: %+v", anin)
	}
	if len(anin.Zadaci) != 2 || anin.Zadaci[0].ID != oznakaZadatka(1) || anin.Zadaci[1].ID != oznakaZadatka(2) {
		t.Fatalf("zadaci na Aninom listu: %+v", anin.Zadaci)
	}
	if na := anin.Zadaci[0]; na.Tekst != "Obilazak nasipa Draž" || na.Zadao != "Marko Marić" || na.Status != models.ZadatakObavljen ||
		na.Obavljeno != "Obiđeno, bez oštećenja." || na.Od != "07:00" || na.Do != "09:30" || na.Udaljenost != 12.5 || !na.ImaObuhvat {
		t.Errorf("prvi zadatak na listu: %+v", na)
	}
	if na := anin.Zadaci[1]; na.Status != models.ZadatakBezOdgovora || na.Obavljeno != ObjasnjenjeBezOdgovora || na.Udaljenost != 3 || na.ImaObuhvat {
		t.Errorf("drugi zadatak na listu: %+v", na)
	}
	if len(anin.Prilozi) != 1 {
		t.Fatalf("prilozi: %+v", anin.Prilozi)
	}
	pr := anin.Prilozi[0]
	if pr.Naziv != "fotografija 1" || pr.Vrsta != "image/jpeg" || pr.Sirina != 40 || pr.Visina != 30 || pr.ZadatakID != oznakaZadatka(1) ||
		pr.ID != uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/bp16:evidencije_obilaska:1/f1")).String() {
		t.Errorf("prilog: %+v", pr)
	}
	if b, _ := repo.Prilog(ctx, pr.ID); len(b) != pr.Bajtova || sadrzaj.Otisak(b) != pr.Sadrzaj {
		t.Errorf("fotografija u spremištu: %d bajtova, opis %d", len(b), pr.Bajtova)
	}

	z, err := repo.ZadatakPoIzvoru(ctx, "bp16:evidencije_obilaska:1")
	if err != nil || z == nil {
		t.Fatalf("zadatak 1: %v", err)
	}
	dan := time.Date(2025, 5, 10, 0, 0, 0, 0, models.Zagreb)
	if z.ID != oznakaZadatka(1) || z.UserID != "g-ana" || z.Sektor != "B" || z.AreaID != 16 || z.ListID != anin.ID || z.Cvor != "cvor-b" ||
		z.Zadao != "Marko Marić" || z.ZadaoID != "" || !z.ZadanoAt.Equal(time.Date(2025, 5, 9, 5, 30, 0, 0, time.UTC)) || !z.Za.Equal(dan) ||
		z.ObavljenoAt == nil || !z.ObavljenoAt.Equal(dan) || z.Udaljenost != 12.5 || !strings.Contains(z.Obuhvat, `"LineString"`) {
		t.Errorf("zadatak 1: %+v", z)
	}

	// Ivo: područje iz popisa područja, sektor iz zaduženja; 11. 5. vrijeme
	// iz arhive, 12. 5. evidencija ima samo vodostaje, a arhiva ništa
	ivin, _ := repo.ZaDan(ctx, "g-ivo", time.Date(2025, 5, 11, 0, 0, 0, 0, models.Zagreb))
	if ivin == nil || ivin.Prilike != "kiša, 12 °C · Open-Meteo, naknadno" || ivin.Ocitanja != "" || ivin.AreaID != 34 || ivin.Sektor != "C" {
		t.Fatalf("Ivin list 11. 5.: %+v", ivin)
	}
	z6, _ := repo.ZadatakPoIzvoru(ctx, "bp16:evidencije_obilaska:6")
	// nečitljivo vrijeme zadavanja: zadano je na dan obilaska
	if z6 == nil || !z6.ZadanoAt.Equal(time.Date(2025, 5, 11, 0, 0, 0, 0, models.Zagreb)) || z6.Status != models.ZadatakBezOdgovora || z6.ObavljenoAt != nil {
		t.Errorf("zadatak 6: %+v", z6)
	}
	ivin12, _ := repo.ZaDan(ctx, "g-ivo", time.Date(2025, 5, 12, 0, 0, 0, 0, models.Zagreb))
	if ivin12 == nil || ivin12.Prilike != "" || ivin12.Ocitanja != "" || len(ivin12.Zadaci) != 1 || ivin12.Zadaci[0].Status != models.ZadatakObavljen {
		t.Errorf("Ivin list 12. 5.: %+v", ivin12)
	}
	for _, id := range []int{3, 4, 5} {
		if z, _ := repo.ZadatakPoIzvoru(ctx, fmt.Sprintf("bp16:evidencije_obilaska:%d", id)); z != nil {
			t.Errorf("obilazak %d ne smije biti upisan: %+v", id, z)
		}
	}

	// ponovni uvoz: sve upisano preskače, ostalo se broji kao prije; samo
	// fotografije koje nisu stigle traže se opet
	dohvaceno = nil
	rep, err = RunObilasci(ctx, izvorObilazaka(), deps)
	if err != nil || rep.Postoje != 4 || rep.Upisano != 0 || rep.Listova != 0 || rep.Preskoceno != 1 || rep.BezKorisnika != 1 || rep.Slika != 0 || rep.Dopunjeno != 0 {
		t.Fatalf("ponovni uvoz: %+v, %v", rep, err)
	}
	if strings.Join(dohvaceno, " ") != "f-nema? f-kriva?" {
		t.Errorf("ponovni uvoz dohvatio: %v", dohvaceno)
	}
	if l, _ := repo.ZaDan(ctx, "g-ana", dan); l == nil || len(l.Zadaci) != 2 || len(l.Prilozi) != 1 {
		t.Errorf("ponovni uvoz promijenio list: %+v", l)
	}

	// bez SamoArhivirane i obilazak u planu postaje zadatak
	deps.SamoArhivirane = false
	if rep, err = RunObilasci(ctx, izvorObilazaka(), deps); err != nil || rep.Upisano != 1 || rep.Preskoceno != 0 || rep.Listova != 1 {
		t.Fatalf("uvoz i onih u planu: %+v, %v", rep, err)
	}
	if z, _ := repo.ZadatakPoIzvoru(ctx, "bp16:evidencije_obilaska:3"); z == nil || z.Status != models.ZadatakBezOdgovora {
		t.Errorf("obilazak u planu: %+v", z)
	}
}

// samoZbirke je izvor bez korisnika, kao mapa bez directus_users
type samoZbirke map[string][]json.RawMessage

func (s samoZbirke) Items(_ context.Context, c string) ([]json.RawMessage, error) {
	if z, ima := s[c]; ima {
		return z, nil
	}
	return nil, fmt.Errorf("%s: nema zbirke", c)
}

// pokvareniIzvor javlja grešku za zadane zbirke i za korisnike
type pokvareniIzvor struct {
	probniIzvor
	greske    map[string]error
	korisnici error
}

func (s pokvareniIzvor) Items(ctx context.Context, c string) ([]json.RawMessage, error) {
	if err := s.greske[c]; err != nil {
		return nil, err
	}
	return s.probniIzvor.Items(ctx, c)
}

func (s pokvareniIzvor) Users(ctx context.Context) ([]json.RawMessage, error) {
	if s.korisnici != nil {
		return nil, s.korisnici
	}
	return s.probniIzvor.Users(ctx)
}

// lazniDnevnik je dnevnik vodočuvara u memoriji, s greškom na zadanom koraku
type lazniDnevnik struct {
	greska  string // "izvor", "zadan", "list", "zadatak", "prilog"
	listovi []models.VodocuvarskiList
	zadaci  []models.Zadatak
	prilozi []string
}

func (d *lazniDnevnik) ZadatakPoIzvoru(_ context.Context, izvor string) (*models.Zadatak, error) {
	if d.greska == "izvor" {
		return nil, errors.New("baza ne odgovara")
	}
	for i := range d.zadaci {
		if d.zadaci[i].Izvor == izvor {
			return &d.zadaci[i], nil
		}
	}
	return nil, nil
}

func (d *lazniDnevnik) SaveZadatak(_ context.Context, z *models.Zadatak) error {
	if d.greska == "zadatak" {
		return errors.New("disk pun")
	}
	d.zadaci = append(d.zadaci, *z)
	return nil
}

func (d *lazniDnevnik) ZaDan(_ context.Context, userID string, dan time.Time) (*models.VodocuvarskiList, error) {
	if d.greska == "zadan" {
		return nil, errors.New("baza zaključana")
	}
	for i := range d.listovi {
		if d.listovi[i].UserID == userID && d.listovi[i].Datum.Equal(dan) {
			l := d.listovi[i]
			return &l, nil
		}
	}
	return nil, nil
}

func (d *lazniDnevnik) Save(_ context.Context, l *models.VodocuvarskiList) error {
	if d.greska == "list" {
		return errors.New("list predan u međuvremenu")
	}
	if l.ID == "" {
		l.ID = models.IDListaVodocuvara(l.UserID, l.Datum)
		d.listovi = append(d.listovi, *l)
		return nil
	}
	for i := range d.listovi {
		if d.listovi[i].ID == l.ID {
			d.listovi[i] = *l
		}
	}
	return nil
}

func (d *lazniDnevnik) SavePrilog(_ context.Context, listID, prilogID string, _ []byte) error {
	if d.greska == "prilog" {
		return errors.New("nema spremišta")
	}
	d.prilozi = append(d.prilozi, listID+"/"+prilogID)
	return nil
}

// Greške izvora i dnevnika prekidaju uvoz s porukom koja kaže gdje je
// stao; nedostupno vrijeme dana samo se zapiše u zapisnik.
func TestUvozObilazakaGreske(t *testing.T) {
	ctx := context.Background()
	jedan := raw(`{"id_obilasci":1,"status":"arhivirano","user_created":"u-3","dodijeljeno":"u-1","datum":"2025-05-10","opis_aktivnosti":"Obilazak","foto_1":"f1"}`)
	izvor := func() probniIzvor {
		return probniIzvor{zbirke: map[string][]json.RawMessage{
			"users":               raw(`{"id":"u-1","first_name":"Ana","last_name":"Anić"}`),
			"evidencije_obilaska": jedan,
		}}
	}
	slika := probnaSlika(t, 8, 6)
	deps := func(d *lazniDnevnik) ObilasciDeps {
		return ObilasciDeps{
			Vodocuvar: d, Sektor: "B",
			Korisnici: map[string]KorisnikUvoza{"Ana Anić": {ID: "g-ana", Ime: "Ana Anić", AreaID: 16}},
			Datoteka:  func(context.Context, string, string) ([]byte, error) { return slika, nil },
		}
	}

	if _, err := RunObilasci(ctx, pokvareniIzvor{probniIzvor: izvor(), korisnici: errors.New("401")}, deps(&lazniDnevnik{})); err == nil || err.Error() != "korisnici: 401" {
		t.Errorf("korisnici: %v", err)
	}
	if _, err := RunObilasci(ctx, pokvareniIzvor{probniIzvor: izvor(), greske: map[string]error{"evidencije_obilaska": errors.New("Directus odgovorio 403")}}, deps(&lazniDnevnik{})); err == nil || err.Error() != "Directus odgovorio 403" {
		t.Errorf("zbirka obilazaka: %v", err)
	}
	pokvaren := izvor()
	pokvaren.zbirke["evidencije_obilaska"] = raw(`{"id_obilasci":"jedan"}`)
	if _, err := RunObilasci(ctx, pokvaren, deps(&lazniDnevnik{})); err == nil || !strings.HasPrefix(err.Error(), "obilazak: ") {
		t.Errorf("nečitljiv obilazak: %v", err)
	}

	// vrijeme dana nedostupno: zapiše se, a uvoz ide dalje; bez zapisnika ne puca
	var zapisnik []string
	d := &lazniDnevnik{}
	dp := deps(d)
	dp.Log = func(f string, a ...any) { zapisnik = append(zapisnik, fmt.Sprintf(f, a...)) }
	rep, err := RunObilasci(ctx, pokvareniIzvor{probniIzvor: izvor(), greske: map[string]error{"vrijeme_za_list": errors.New("zabranjeno")}}, dp)
	if err != nil || rep.Upisano != 1 || rep.Listova != 1 || len(d.zadaci) != 1 || len(d.prilozi) != 1 {
		t.Fatalf("bez vremena dana: %+v, %v", rep, err)
	}
	if len(zapisnik) != 1 || zapisnik[0] != "vrijeme za list: zabranjeno" {
		t.Errorf("zapisnik: %q", zapisnik)
	}
	if d.zadaci[0].ListID != d.listovi[0].ID || d.prilozi[0] != d.listovi[0].ID+"/"+d.listovi[0].Prilozi[0].ID {
		t.Errorf("zadatak i prilog nisu vezani na list: %+v %v", d.zadaci[0], d.prilozi)
	}

	for greska, poruka := range map[string]string{
		"izvor":   "obilazak 1: čitanje prenesenog zadatka: baza ne odgovara",
		"zadan":   "obilazak 1: list dana: baza zaključana",
		"list":    "obilazak 1: list: list predan u međuvremenu",
		"zadatak": "obilazak 1: zadatak: disk pun",
		"prilog":  "obilazak 1: prilog: nema spremišta",
	} {
		d := &lazniDnevnik{greska: greska}
		rep, err := RunObilasci(ctx, izvor(), deps(d))
		if err == nil || err.Error() != poruka {
			t.Errorf("%s: %v", greska, err)
		}
		if rep.Upisano != 0 {
			t.Errorf("%s: upisano %d", greska, rep.Upisano)
		}
	}

	// bez korisnika u izvoru i bez dohvata datoteka: vodočuvar se ne zna, a
	// fotografije se ne traže
	d = &lazniDnevnik{}
	dp = deps(d)
	dp.Datoteka = nil
	rep, err = RunObilasci(ctx, samoZbirke{"evidencije_obilaska": jedan, "vrijeme_za_list": nil}, dp)
	if err != nil || rep.BezKorisnika != 1 || rep.Nepoznati[""] != 1 || rep.Upisano != 0 {
		t.Errorf("bez korisnika: %+v, %v", rep, err)
	}
	dp.Korisnici[""] = KorisnikUvoza{ID: "g-bez", Ime: "Bez imena"}
	if rep, err = RunObilasci(ctx, samoZbirke{"evidencije_obilaska": jedan}, dp); err != nil || rep.Upisano != 1 || rep.Slika != 0 || len(d.prilozi) != 0 {
		t.Errorf("bez dohvata datoteka: %+v, %v", rep, err)
	}
	// sektor iz uvoza kad ga vodočuvar nema
	if len(d.zadaci) != 1 || d.zadaci[0].Sektor != "B" || d.listovi[0].Sektor != "B" || d.zadaci[0].Zadao != "" {
		t.Errorf("zadatak bez korisnika u izvoru: %+v", d.zadaci)
	}
}

// jedanObilazak je stara evidencija s jednim obilaskom Ane Anić 10. 5. 2025.
// i jednom fotografijom
func jedanObilazak() probniIzvor {
	return probniIzvor{zbirke: map[string][]json.RawMessage{
		"users": raw(`{"id":"u-1","first_name":"Ana","last_name":"Anić"}`),
		"evidencije_obilaska": raw(`{"id_obilasci":1,"status":"arhivirano","user_created":"u-1","dodijeljeno":"u-1",` +
			`"datum":"2025-05-10","opis_aktivnosti":"Obilazak nasipa","foto_1":"f1"}`),
	}}
}

// depsJednogObilaska su ovisnosti uvoza jednog obilaska nad zadanim dnevnikom
func depsJednogObilaska(t *testing.T, repo ObilasciRepo) ObilasciDeps {
	slika := probnaSlika(t, 8, 6)
	return ObilasciDeps{
		Vodocuvar: repo, Sektor: "B",
		Korisnici: map[string]KorisnikUvoza{"Ana Anić": {ID: "g-ana", Ime: "Ana Anić", AreaID: 16}},
		Datoteka:  func(context.Context, string, string) ([]byte, error) { return slika, nil },
	}
}

// Kad se preneseni zadatak ne da pročitati, uvoz stane. Inače bi isti
// obilazak upisao još jednom, preko zadatka koji je već prenesen.
func TestUvozObilazakaStajeKadZadatakNijeProcitan(t *testing.T) {
	ctx := context.Background()
	d := &lazniDnevnik{}
	deps := depsJednogObilaska(t, d)
	if rep, err := RunObilasci(ctx, jedanObilazak(), deps); err != nil || rep.Upisano != 1 {
		t.Fatalf("prvi uvoz: %+v, %v", rep, err)
	}
	d.greska = "izvor"
	rep, err := RunObilasci(ctx, jedanObilazak(), deps)
	if err == nil || err.Error() != "obilazak 1: čitanje prenesenog zadatka: baza ne odgovara" || rep.Upisano != 0 || rep.Postoje != 0 {
		t.Fatalf("ponovni uvoz: %+v, %v", rep, err)
	}
	if len(d.zadaci) != 1 || len(d.listovi) != 1 || len(d.listovi[0].Zadaci) != 1 || len(d.listovi[0].Prilozi) != 1 {
		t.Errorf("obilazak upisan još jednom: %d zadataka, list %+v", len(d.zadaci), d.listovi)
	}
}

// Fotografija koja se pri uvozu nije dala spremiti stiže ponovnim uvozom.
// List i zadatak upisuju se tek nakon fotografija, pa ponovni uvoz obilazak
// ne preskače, a list ne dobiva isti zadatak ni istu fotografiju dvaput.
func TestPonovniUvozDovrsavaObilazakBezFotografije(t *testing.T) {
	repo := dnevnikObilazaka(t)
	ctx := context.Background()
	deps := depsJednogObilaska(t, repo)
	dan := time.Date(2025, 5, 10, 0, 0, 0, 0, models.Zagreb)

	spremiste := repository.Spremiste()
	repository.SetSpremiste(nil)
	_, err := RunObilasci(ctx, jedanObilazak(), deps)
	repository.SetSpremiste(spremiste)
	if err == nil || !strings.HasPrefix(err.Error(), "obilazak 1: prilog: ") {
		t.Fatalf("uvoz bez spremišta: %v", err)
	}
	if z, err := repo.ZadatakPoIzvoru(ctx, "bp16:evidencije_obilaska:1"); err != nil || z != nil {
		t.Fatalf("zadatak upisan prije fotografije: %+v, %v", z, err)
	}
	if l, err := repo.ZaDan(ctx, "g-ana", dan); err != nil || l != nil {
		t.Fatalf("list upisan prije fotografije: %+v, %v", l, err)
	}

	rep, err := RunObilasci(ctx, jedanObilazak(), deps)
	if err != nil || rep.Upisano != 1 || rep.Postoje != 0 || rep.Listova != 1 || rep.Slika != 1 {
		t.Fatalf("ponovni uvoz: %+v, %v", rep, err)
	}
	l, err := repo.ZaDan(ctx, "g-ana", dan)
	if err != nil || l == nil || len(l.Zadaci) != 1 || len(l.Prilozi) != 1 {
		t.Fatalf("list nakon ponovnog uvoza: %+v, %v", l, err)
	}
	if b, _ := repo.Prilog(ctx, l.Prilozi[0].ID); len(b) == 0 || sadrzaj.Otisak(b) != l.Prilozi[0].Sadrzaj {
		t.Errorf("fotografija nije u spremištu: %d bajtova", len(b))
	}
	if z, _ := repo.ZadatakPoIzvoru(ctx, "bp16:evidencije_obilaska:1"); z == nil || z.ListID != l.ID {
		t.Errorf("zadatak nakon ponovnog uvoza: %+v", z)
	}

	// dovršen obilazak treći uvoz preskače
	if rep, err := RunObilasci(ctx, jedanObilazak(), deps); err != nil || rep.Postoje != 1 || rep.Upisano != 0 {
		t.Errorf("treći uvoz: %+v, %v", rep, err)
	}
}

// Kad Directus pri ponovnom uvozu ne da fotografiju koja se prvi put nije
// dala spremiti, na listu ne ostaje njezin opis bez slike.
func TestPonovniUvozBezFotografijeNeOstavljaOpis(t *testing.T) {
	repo := dnevnikObilazaka(t)
	ctx := context.Background()
	deps := depsJednogObilaska(t, repo)
	dan := time.Date(2025, 5, 10, 0, 0, 0, 0, models.Zagreb)

	spremiste := repository.Spremiste()
	repository.SetSpremiste(nil)
	_, err := RunObilasci(ctx, jedanObilazak(), deps)
	repository.SetSpremiste(spremiste)
	if err == nil || !strings.HasPrefix(err.Error(), "obilazak 1: prilog: ") {
		t.Fatalf("uvoz bez spremišta: %v", err)
	}

	deps.Datoteka = func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("Directus odgovorio 404")
	}
	rep, err := RunObilasci(ctx, jedanObilazak(), deps)
	if err != nil || rep.Upisano != 1 || rep.Slika != 0 {
		t.Fatalf("ponovni uvoz bez fotografije: %+v, %v", rep, err)
	}
	l, err := repo.ZaDan(ctx, "g-ana", dan)
	if err != nil || l == nil || len(l.Zadaci) != 1 {
		t.Fatalf("list nakon ponovnog uvoza: %+v, %v", l, err)
	}
	for _, pr := range l.Prilozi {
		if b, _ := repo.Prilog(ctx, pr.ID); len(b) == 0 {
			t.Errorf("na listu je opis fotografije bez slike: %+v", pr)
		}
	}
}

// Zadatak koji se nije dao upisati ponovni uvoz upiše, a list zadrži jedan
// zadatak i jednu fotografiju, iako ih je prvi uvoz već stavio na list.
func TestPonovniUvozNeUdvostrucujeZadatakNaListu(t *testing.T) {
	ctx := context.Background()
	d := &lazniDnevnik{greska: "zadatak"}
	deps := depsJednogObilaska(t, d)
	if _, err := RunObilasci(ctx, jedanObilazak(), deps); err == nil || err.Error() != "obilazak 1: zadatak: disk pun" {
		t.Fatalf("prvi uvoz: %v", err)
	}
	d.greska = ""
	rep, err := RunObilasci(ctx, jedanObilazak(), deps)
	if err != nil || rep.Upisano != 1 || rep.Listova != 0 {
		t.Fatalf("ponovni uvoz: %+v, %v", rep, err)
	}
	if len(d.zadaci) != 1 || len(d.listovi) != 1 || len(d.listovi[0].Zadaci) != 1 || len(d.listovi[0].Prilozi) != 1 {
		t.Errorf("nakon ponovnog uvoza: %d zadataka, list %+v", len(d.zadaci), d.listovi)
	}
	if d.zadaci[0].ListID != d.listovi[0].ID {
		t.Errorf("zadatak nije vezan na list: %+v", d.zadaci[0])
	}
}

// dvaObilaska su obilazak 1 s dvije fotografije i obilazak 2 bez njih, oba
// Ane Anić 10. 5. 2025.
func dvaObilaska() probniIzvor {
	return probniIzvor{zbirke: map[string][]json.RawMessage{
		"users": raw(`{"id":"u-1","first_name":"Ana","last_name":"Anić"}`),
		"evidencije_obilaska": raw(
			`{"id_obilasci":1,"status":"arhivirano","user_created":"u-1","dodijeljeno":"u-1","datum":"2025-05-10","opis_aktivnosti":"Obilazak nasipa","foto_1":"f1","foto_2":"f2"}`,
			`{"id_obilasci":2,"status":"arhivirano","user_created":"u-1","dodijeljeno":"u-1","datum":"2025-05-10","opis_aktivnosti":"Obilazak kanala"}`,
		),
	}}
}

// Fotografija koju Directus nije dao stiže ponovnim uvozom: obilazak se ne
// preskače dok mu fotografija nedostaje, a dobije samo ono što mu nedostaje.
func TestPonovniUvozDohvacaFotografijuObilaska(t *testing.T) {
	repo := dnevnikObilazaka(t)
	ctx := context.Background()
	slika := probnaSlika(t, 8, 6)
	nema := map[string]bool{"f2": true}
	var dohvaceno, zapisnik []string
	deps := depsJednogObilaska(t, repo)
	deps.Datoteka = func(_ context.Context, id, _ string) ([]byte, error) {
		dohvaceno = append(dohvaceno, id)
		if nema[id] {
			return nil, errors.New("Directus odgovorio 404")
		}
		return slika, nil
	}
	deps.Log = func(f string, a ...any) { zapisnik = append(zapisnik, fmt.Sprintf(f, a...)) }
	dan := time.Date(2025, 5, 10, 0, 0, 0, 0, models.Zagreb)

	if rep, err := RunObilasci(ctx, dvaObilaska(), deps); err != nil || rep.Upisano != 2 || rep.Slika != 1 {
		t.Fatalf("prvi uvoz: %+v, %v", rep, err)
	}
	prije, _ := repo.ZadatakPoIzvoru(ctx, "bp16:evidencije_obilaska:1")

	nema, dohvaceno, zapisnik = nil, nil, nil
	rep, err := RunObilasci(ctx, dvaObilaska(), deps)
	if err != nil || rep.Postoje != 2 || rep.Dopunjeno != 1 || rep.Slika != 1 || rep.Upisano != 0 || rep.Listova != 0 {
		t.Fatalf("ponovni uvoz: %+v, %v", rep, err)
	}
	if strings.Join(dohvaceno, " ") != "f2" {
		t.Errorf("ponovni uvoz dohvatio: %v", dohvaceno)
	}
	l, err := repo.ZaDan(ctx, "g-ana", dan)
	if err != nil || l == nil || len(l.Zadaci) != 2 || len(l.Prilozi) != 2 {
		t.Fatalf("list nakon ponovnog uvoza: %+v, %v", l, err)
	}
	pr := l.Prilozi[1]
	if pr.Naziv != "fotografija 2" || pr.ZadatakID != oznakaZadatka(1) ||
		pr.ID != uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/bp16:evidencije_obilaska:1/f2")).String() {
		t.Errorf("dodana fotografija: %+v", pr)
	}
	if b, _ := repo.Prilog(ctx, pr.ID); len(b) == 0 || sadrzaj.Otisak(b) != pr.Sadrzaj {
		t.Errorf("dodana fotografija nije u spremištu: %d bajtova", len(b))
	}
	if z, _ := repo.ZadatakPoIzvoru(ctx, "bp16:evidencije_obilaska:1"); z == nil || !z.UpdatedAt.Equal(prije.UpdatedAt) || z.ListID != l.ID {
		t.Errorf("zadatak se promijenio: %+v, prije %+v", z, prije)
	}
	if z := strings.Join(zapisnik, "\n"); z != "  obilazak 1: dodana je 1 fotografija koja ranije nije stigla" {
		t.Errorf("zapisnik:\n%s", z)
	}

	// kad su sve fotografije stigle, ništa se više ne traži
	dohvaceno = nil
	if rep, err := RunObilasci(ctx, dvaObilaska(), deps); err != nil || rep.Postoje != 2 || rep.Dopunjeno != 0 || len(dohvaceno) != 0 {
		t.Errorf("treći uvoz: %+v, %v, dohvaćeno %v", rep, err, dohvaceno)
	}
}

// Greška dnevnika pri dopuni prekida uvoz i kaže gdje je stao, a list ne
// dobije opis fotografije koja nije spremljena. Suhi prolaz, uvoz bez
// dohvata datoteka i obilazak bez fotografija list ne čitaju.
func TestDopunaObilaskaGreske(t *testing.T) {
	ctx := context.Background()
	prenesen := func(t *testing.T) (*lazniDnevnik, ObilasciDeps) {
		d := &lazniDnevnik{}
		deps := depsJednogObilaska(t, d)
		slika := probnaSlika(t, 8, 6)
		deps.Datoteka = func(context.Context, string, string) ([]byte, error) {
			return nil, errors.New("Directus odgovorio 404")
		}
		if rep, err := RunObilasci(ctx, jedanObilazak(), deps); err != nil || rep.Upisano != 1 || len(d.listovi) != 1 || len(d.listovi[0].Prilozi) != 0 {
			t.Fatalf("prvi uvoz: %+v, %v", rep, err)
		}
		deps.Datoteka = func(context.Context, string, string) ([]byte, error) { return slika, nil }
		return d, deps
	}
	for greska, poruka := range map[string]string{
		"zadan":  "obilazak 1: list dana: baza zaključana",
		"prilog": "obilazak 1: prilog: nema spremišta",
		"list":   "obilazak 1: list: list predan u međuvremenu",
	} {
		d, deps := prenesen(t)
		d.greska = greska
		rep, err := RunObilasci(ctx, jedanObilazak(), deps)
		if err == nil || err.Error() != poruka || rep.Dopunjeno != 0 {
			t.Errorf("%s: %+v, %v", greska, rep, err)
		}
		if len(d.listovi[0].Prilozi) != 0 {
			t.Errorf("%s: na listu je opis fotografije: %+v", greska, d.listovi[0].Prilozi)
		}
	}

	// list obilaska više ne postoji: nema na što dodati
	d, deps := prenesen(t)
	d.listovi = nil
	if rep, err := RunObilasci(ctx, jedanObilazak(), deps); err != nil || rep.Postoje != 1 || rep.Dopunjeno != 0 || len(d.prilozi) != 0 {
		t.Errorf("bez lista: %+v, %v", rep, err)
	}

	// suhi prolaz, bez dohvata datoteka i bez fotografija list se ne čita
	bezFotografija := jedanObilazak()
	bezFotografija.zbirke["evidencije_obilaska"] = raw(`{"id_obilasci":1,"status":"arhivirano","user_created":"u-1","dodijeljeno":"u-1","datum":"2025-05-10","opis_aktivnosti":"Obilazak nasipa"}`)
	for ime, izmijeni := range map[string]func(*ObilasciDeps) probniIzvor{
		"suhi prolaz":     func(d *ObilasciDeps) probniIzvor { d.DryRun = true; return jedanObilazak() },
		"bez datoteka":    func(d *ObilasciDeps) probniIzvor { d.Datoteka = nil; return jedanObilazak() },
		"bez fotografija": func(*ObilasciDeps) probniIzvor { return bezFotografija },
	} {
		d, deps := prenesen(t)
		d.greska = "zadan"
		src := izmijeni(&deps)
		if rep, err := RunObilasci(ctx, src, deps); err != nil || rep.Postoje != 1 || rep.Dopunjeno != 0 {
			t.Errorf("%s: %+v, %v", ime, rep, err)
		}
	}
}
