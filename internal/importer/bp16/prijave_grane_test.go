package bp16

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
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

// Zaključava rubne slučajeve uvoza obavijesti s terena: područje kojeg
// nema, prazan naslov, voda zapisana kao objekt, slika koja se dobije tek
// smanjena, fotografija s položajem iz fotoaparata, sken kao izvornik kad
// PDF iz podataka nije moguć, zapis bez vremena upisa i greške baze.

// bazaPrijava diže bazu prijava i spremište sadržaja za slike i izvornike
func bazaPrijava(t *testing.T) (*sql.DB, *repository.PrijavaRepository) {
	t.Helper()
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "prijave.db"))
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
	return baza, prijaveNa(baza)
}

// prijaveNa je spremište prijava nad zadanom bazom
func prijaveNa(baza *sql.DB) *repository.PrijavaRepository {
	return repository.NewPrijavaRepository(baza, ledger.New(baza, "cvor-b"))
}

// jpegSPolozajem je JPEG s EXIF-om kakav zapiše telefon: vrijeme snimanja
// 5. 6. 2024. 11:43:48 i položaj 45°39'43,2" N, 18°46'24" E
func jpegSPolozajem(t *testing.T) []byte {
	t.Helper()
	var slika bytes.Buffer
	if err := jpeg.Encode(&slika, image.NewRGBA(image.Rect(0, 0, 16, 12)), nil); err != nil {
		t.Fatal(err)
	}
	be := func(v uint32) []byte { return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)} }
	be16 := func(v uint16) []byte { return []byte{byte(v >> 8), byte(v)} }
	stavka := func(tag, tip uint16, n uint32, v []byte) []byte {
		return append(append(append(be16(tag), be16(tip)...), be(n)...), v...)
	}
	rac := func(br, naz uint32) []byte { return append(be(br), be(naz)...) }
	// IFD0 (2 stavke) na 8, GPS (4 stavke) na 38, podaci od 92
	datum := []byte("2024:06:05 11:43:48\x00")
	lat := append(append(rac(45, 1), rac(39, 1)...), rac(4320, 100)...)
	lon := append(append(rac(18, 1), rac(46, 1)...), rac(2400, 100)...)
	ifd0 := append(be16(2), stavka(0x0132, 2, uint32(len(datum)), be(92))...)
	ifd0 = append(append(ifd0, stavka(0x8825, 4, 1, be(38))...), be(0)...)
	gps := be16(4)
	gps = append(gps, stavka(0x0001, 2, 2, []byte("N\x00\x00\x00"))...)
	gps = append(gps, stavka(0x0002, 5, 3, be(uint32(92+len(datum))))...)
	gps = append(gps, stavka(0x0003, 2, 2, []byte("E\x00\x00\x00"))...)
	gps = append(gps, stavka(0x0004, 5, 3, be(uint32(92+len(datum)+len(lat))))...)
	gps = append(gps, be(0)...)
	tiff := append(append([]byte{'M', 'M', 0, 42, 0, 0, 0, 8}, ifd0...), gps...)
	if len(tiff) != 92 {
		t.Fatalf("raspored TIFF-a: %d", len(tiff))
	}
	tiff = append(append(append(tiff, datum...), lat...), lon...)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	out := append([]byte{0xFF, 0xD8, 0xFF, 0xE1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}, app1...)
	return append(out, slika.Bytes()[2:]...)
}

func TestUvozObavijestiRubniSlucajevi(t *testing.T) {
	_, repo := bazaPrijava(t)
	ctx := context.Background()
	exif := jpegSPolozajem(t)
	png := probnaSlika(t, 20, 10)
	sken := []byte("%PDF-1.4\nsken potpisanog ispisa")
	var trazeno, zapisnik []string
	src := probniIzvor{zbirke: map[string][]json.RawMessage{
		"users": raw(`{"id":"u-1","first_name":"Marko","last_name":"Markić"}`, `"nije korisnik"`),
		"vode":  raw(`{"id":201,"voda":"Kanal B1"}`, `"nije voda"`),
		"obavijesti_sa_terena": raw(
			`{"id":20,"status":"objavljeno","date_created":"2024-07-01T08:00:00Z","user_created":"u-1","vodocuvarsko_podrucje":"Nepoznato","naslov":"Tuđe područje"}`,
			`{"id":21,"status":"objavljeno","date_created":"2024-07-02T08:00:00Z","user_created":"u-1","vodocuvarsko_podrucje":" karašica sektor ","naslov":"  ","naziv_vodotoka":{"voda":"Kanal C3"},"slika_1":"s-mala","slika_2":"s-nema","slika_3":"s-kriva","datoteka":"d-nije-pdf","lokacija":{"type":"Point","coordinates":[2.35,48.85]}}`,
			`{"id":22,"status":"arhivirano","date_created":"2024-07-03T08:00:00Z","user_created":"u-1","vodocuvarsko_podrucje":"KARAŠICA SEKTOR","naslov":"S fotografijom","naziv_vodotoka":"Kanal kao tekst","slika_1":"s-exif","datoteka":"d-pdf"}`,
			`{"id":23,"status":"skica","date_created":"","user_created":"u-1","vodocuvarsko_podrucje":"KARAŠICA SEKTOR","naslov":"Bez vremena upisa"}`,
			`{"id":24,"status":"objavljeno","date_created":"2024-07-04T08:00:00Z","user_created":"u-1","vodocuvarsko_podrucje":"KARAŠICA SEKTOR","naslov":"Sken se ne da dohvatiti","datoteka":"d-greska"}`,
		),
	}}
	deps := PrijaveDeps{
		Prijave:   repo,
		Korisnici: map[string]KorisnikUvoza{"Marko Markić": {ID: "g-1", Ime: "Marko Markić", Sektor: "B"}},
		Podrucja:  map[string]int{"KARAŠICA SEKTOR": 16},
		Sektor:    "B", Cvor: "cvor-b",
		Datoteka: func(_ context.Context, id, upit string) ([]byte, error) {
			trazeno = append(trazeno, id+"?"+upit)
			switch {
			case id == "s-mala" && upit != "":
				return png, nil
			case id == "s-kriva":
				return []byte("nije slika"), nil
			case id == "s-exif":
				return exif, nil
			case id == "d-nije-pdf":
				return []byte("<html>prijava</html>"), nil
			case id == "d-pdf":
				return sken, nil
			}
			return nil, errors.New("Directus odgovorio 404")
		},
		Log: func(f string, a ...any) { zapisnik = append(zapisnik, fmt.Sprintf(f, a...)) },
	}
	prije := time.Now()
	rep, err := RunPrijave(ctx, src, deps)
	poslije := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ukupno != 5 || rep.Upisano != 4 || rep.BezPodrucja != 1 || rep.Nacrta != 1 || rep.Skenova != 1 || rep.Slika != 2 {
		t.Fatalf("izvješće: %+v", rep)
	}
	if rep.Nepoznati["područje Nepoznato"] != 1 || rep.PoPodrucju["BP 16 ( karašica sektor )"] != 1 || rep.PoPodrucju["BP 16 (KARAŠICA SEKTOR)"] != 3 {
		t.Errorf("po području: %v, nepoznato %v", rep.PoPodrucju, rep.Nepoznati)
	}
	// smanjena slika traži se tek kad izvorne nema; sken se dohvaća jer
	// PDF iz podataka nije moguć
	if got := strings.Join(trazeno, " "); got != "s-mala? s-mala?width=1600&height=1600&fit=inside&quality=82&format=jpg s-nema? "+
		"s-nema?width=1600&height=1600&fit=inside&quality=82&format=jpg s-kriva? d-nije-pdf? s-exif? d-pdf? d-greska?" {
		t.Errorf("dohvaćeno: %s", got)
	}
	z := strings.Join(zapisnik, "\n")
	for _, poruka := range []string{
		"  obavijest 21: slika s-nema: Directus odgovorio 404",
		"  obavijest 21: slika s-kriva: slika nije čitljiv JPEG ili PNG",
		"  obavijest 21: datoteka d-nije-pdf nije PDF, preskačem",
		"  obavijest 24: datoteka d-greska: Directus odgovorio 404",
	} {
		if !strings.Contains(z, poruka) {
			t.Errorf("u zapisniku nema %q:\n%s", poruka, z)
		}
	}

	p21, _ := repo.PoIzvoru(ctx, "bp16:obavijesti_sa_terena:21")
	// točka izvan Hrvatske se ne uzima
	if p21 == nil || p21.Naslov != "(bez naslova)" || p21.Vodotok != "Kanal C3" || p21.AreaID != 16 || p21.Broj != 1 || p21.ImaKoordinate() ||
		len(p21.Slike) != 1 || p21.Slike[0].Sirina != 20 || p21.Slike[0].Snimljeno != nil || p21.Sken != "" {
		t.Fatalf("obavijest 21: %+v", p21)
	}
	if iz, _ := repo.Izvornik(ctx, p21.ID); iz != nil {
		t.Errorf("bez skena i bez PDF-a iz podataka nema izvornika: %+v", iz)
	}

	p22, _ := repo.PoIzvoru(ctx, "bp16:obavijesti_sa_terena:22")
	if p22 == nil || p22.Vodotok != "" || p22.Status != models.PrijavaRijesena || p22.Broj != 2 || len(p22.Slike) != 1 {
		t.Fatalf("obavijest 22: %+v", p22)
	}
	sl := p22.Slike[0]
	otisak := sha256.Sum256(exif)
	if sl.Snimljeno == nil || !sl.Snimljeno.Equal(time.Date(2024, 6, 5, 11, 43, 48, 0, time.Local)) || sl.IzvornoBajtova != len(exif) ||
		sl.Otisak != hex.EncodeToString(otisak[:]) || sl.Lat < 45.6619 || sl.Lat > 45.6621 {
		t.Errorf("podaci fotoaparata: %+v", sl)
	}
	// bez točke u evidenciji položaj prijave dolazi s fotografije
	if !p22.ImaKoordinate() || *p22.Latitude != sl.Lat || *p22.Longitude != sl.Lon {
		t.Errorf("položaj prijave: %v %v", p22.Latitude, p22.Longitude)
	}
	if iz, _ := repo.Izvornik(ctx, p22.ID); iz == nil || !bytes.Equal(iz.PDF, sken) {
		t.Errorf("sken mora biti izvornik: %+v", iz)
	}

	// bez vremena upisa nacrt dobiva trenutak uvoza i ostaje bez broja
	p23, _ := repo.PoIzvoru(ctx, "bp16:obavijesti_sa_terena:23")
	if p23 == nil || p23.Status != models.PrijavaNacrt || p23.Broj != 0 || p23.ObjavljenoAt != nil ||
		p23.CreatedAt.Before(prije.Truncate(time.Second)) || p23.CreatedAt.After(poslije) {
		t.Errorf("obavijest 23: %+v (uvoz od %v do %v)", p23, prije, poslije)
	}
	if p24, _ := repo.PoIzvoru(ctx, "bp16:obavijesti_sa_terena:24"); p24 == nil || p24.Broj != 3 || p24.Sken != "" {
		t.Errorf("obavijest 24: %+v", p24)
	}
}

// Greška izvora ili baze prekida uvoz s porukom koja kaže gdje je stao.
func TestUvozObavijestiGreske(t *testing.T) {
	ctx := context.Background()
	jedna := func() probniIzvor {
		return probniIzvor{zbirke: map[string][]json.RawMessage{
			"users": raw(`{"id":"u-1","first_name":"Marko","last_name":"Markić"}`),
			"obavijesti_sa_terena": raw(`{"id":30,"status":"objavljeno","date_created":"2024-07-02T08:00:00Z","user_created":"u-1",` +
				`"vodocuvarsko_podrucje":"KARAŠICA SEKTOR","naslov":"Probna","slika_1":"s1","datoteka":"d1"}`),
		}}
	}
	png := probnaSlika(t, 20, 10)
	deps := func(repo *repository.PrijavaRepository) PrijaveDeps {
		return PrijaveDeps{
			Prijave:   repo,
			Korisnici: map[string]KorisnikUvoza{"Marko Markić": {ID: "g-1", Ime: "Marko Markić"}},
			Podrucja:  map[string]int{"KARAŠICA SEKTOR": 16},
			Sektor:    "B",
			Datoteka: func(_ context.Context, id, _ string) ([]byte, error) {
				if id == "d1" {
					return []byte("%PDF-1.4\nsken"), nil
				}
				return png, nil
			},
			IzradiPDF:  func(*models.PrijavaSTerena, map[string][]byte) []byte { return []byte("%PDF-1.4\niz podataka") },
			SpremiSken: func(p *models.PrijavaSTerena, _ []byte) (string, error) { return p.ID + ".pdf", nil },
		}
	}

	// izvor puca prije ikakvog upisa, pa slučajevi mogu na predložak
	predlozak, repo := bazaPrijava(t)
	if _, err := RunPrijave(ctx, pokvareniIzvor{probniIzvor: jedna(), korisnici: errors.New("401")}, deps(repo)); err == nil || err.Error() != "korisnici: 401" {
		t.Errorf("korisnici: %v", err)
	}
	if _, err := RunPrijave(ctx, pokvareniIzvor{probniIzvor: jedna(), greske: map[string]error{"obavijesti_sa_terena": errors.New("403")}}, deps(repo)); err == nil || err.Error() != "403" {
		t.Errorf("zbirka: %v", err)
	}
	pokvarena := jedna()
	pokvarena.zbirke["obavijesti_sa_terena"] = raw(`{"id":"trideset"}`)
	if _, err := RunPrijave(ctx, pokvarena, deps(repo)); err == nil || !strings.HasPrefix(err.Error(), "obavijest: ") {
		t.Errorf("nečitljiva obavijest: %v", err)
	}
	// bez zbirke vode obavijest bi ostala bez vode, a ponovni uvoz je
	// preskače, pa uvoz stane prije upisa
	bezVoda := kopijaBaze(t, predlozak)
	rep, err := RunPrijave(ctx, pokvareniIzvor{probniIzvor: jedna(), greske: map[string]error{"vode": errors.New("Directus odgovorio 403")}}, deps(prijaveNa(bezVoda)))
	if err == nil || err.Error() != "vode: Directus odgovorio 403" || rep.Upisano != 0 {
		t.Errorf("zbirka vode: %+v, %v", rep, err)
	}

	// prenesena prijava koja se ne da pročitati: uvoz stane, a ne upiše je
	// ponovno pod novim brojem
	necitljiva := kopijaBaze(t, predlozak)
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("gocop/bp16:obavijesti_sa_terena:30")).String()
	sad := time.Date(2024, 7, 2, 8, 0, 0, 0, time.UTC)
	if _, err := necitljiva.Exec(`INSERT INTO prijave (id, user_id, sektor, area_id, broj, godina, status, podaci, created_at, updated_at)
		VALUES (?, 'g-1', 'B', 'šesnaest', 7, 2024, ?, ?, ?, ?)`, id, models.PrijavaObjavljena, `{"izvor":"bp16:obavijesti_sa_terena:30"}`, sad, sad); err != nil {
		t.Fatal(err)
	}
	rep, err = RunPrijave(ctx, jedna(), deps(prijaveNa(necitljiva)))
	if err == nil || !strings.HasPrefix(err.Error(), "obavijest 30: čitanje prenesene prijave: ") || rep.Upisano != 0 {
		t.Errorf("nečitljiva prenesena prijava: %+v, %v", rep, err)
	}
	var broj int
	var podrucje string
	if err := necitljiva.QueryRow(`SELECT broj, area_id FROM prijave WHERE id = ?`, id).Scan(&broj, &podrucje); err != nil || broj != 7 || podrucje != "šesnaest" {
		t.Errorf("prenesena prijava prepisana: broj %d, područje %q, %v", broj, podrucje, err)
	}
	d := deps(prijaveNa(kopijaBaze(t, predlozak)))
	d.SpremiSken = func(*models.PrijavaSTerena, []byte) (string, error) { return "", errors.New("mapa nije zapisiva") }
	if _, err := RunPrijave(ctx, jedna(), d); err == nil || err.Error() != "obavijest 30, sken: mapa nije zapisiva" {
		t.Errorf("sken: %v", err)
	}

	// zatvorena baza: uvoz stane već na provjeri prenesenog
	if _, err := RunPrijave(ctx, jedna(), deps(prijaveNa(zatvorenaBaza(t)))); err == nil ||
		!strings.HasPrefix(err.Error(), "obavijest 30: čitanje prenesene prijave: ") || !strings.Contains(err.Error(), "closed") {
		t.Errorf("zatvorena baza: %v", err)
	}

	// baza odbija upis: okidač glumi pun disk
	for _, s := range []struct {
		ime, okidac, poruka string
		skenSpremljen       bool
	}{
		{"prijava", `BEFORE INSERT ON prijave`, "obavijest 30: upis prijave: ", false},
		{"prijava sa skenom", `BEFORE INSERT ON prijave WHEN COALESCE(json_extract(NEW.podaci, '$.sken'), '') <> ''`, "obavijest 30: upis prijave: ", true},
		{"izvornik", `BEFORE INSERT ON prijave_izvornici`, "obavijest 30, izvornik: ", true},
	} {
		baza := kopijaBaze(t, predlozak)
		if _, err := baza.Exec(`CREATE TRIGGER pun_disk ` + s.okidac + ` BEGIN SELECT RAISE(ABORT, 'disk pun'); END`); err != nil {
			t.Fatal(err)
		}
		d := deps(prijaveNa(baza))
		spremljen := false
		d.SpremiSken = func(p *models.PrijavaSTerena, _ []byte) (string, error) { spremljen = true; return p.ID + ".pdf", nil }
		rep, err := RunPrijave(ctx, jedna(), d)
		if err == nil || !strings.HasPrefix(err.Error(), s.poruka) || !strings.Contains(err.Error(), "disk pun") || rep.Upisano != 0 || spremljen != s.skenSpremljen {
			t.Errorf("%s: %+v, %v, sken spremljen %v", s.ime, rep, err, spremljen)
		}
	}

	// bez spremišta sadržaja slika se ne može spremiti
	spremiste := repository.Spremiste()
	repository.SetSpremiste(nil)
	defer repository.SetSpremiste(spremiste)
	if _, err := RunPrijave(ctx, jedna(), deps(prijaveNa(kopijaBaze(t, predlozak)))); err == nil || !strings.HasPrefix(err.Error(), "obavijest 30, slika: ") {
		t.Errorf("bez spremišta: %v", err)
	}
}
