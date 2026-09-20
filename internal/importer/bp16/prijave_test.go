package bp16

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// probniIzvor daje zbirke iz memorije, kao Directus
type probniIzvor struct{ zbirke map[string][]json.RawMessage }

func (s probniIzvor) Items(_ context.Context, c string) ([]json.RawMessage, error) {
	return s.zbirke[c], nil
}
func (s probniIzvor) Users(context.Context) ([]json.RawMessage, error) { return s.zbirke["users"], nil }

func raw(v ...string) []json.RawMessage {
	var out []json.RawMessage
	for _, x := range v {
		out = append(out, json.RawMessage(x))
	}
	return out
}

// Obavijesti s terena iz Directusa postaju rekonstruirane prijave: vodočuvar
// po imenu, područje po vodočuvarskom sektoru, vrsta, voda, točka, slike
// smanjene, sken kao izvornik ili PDF iz podataka; ponovni uvoz preskače.
func TestUvozObavijestiSTerena(t *testing.T) {
	baza, err := db.OpenDB(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer baza.Close()
	if err := db.InitSchema(baza); err != nil {
		t.Fatal(err)
	}
	spremiste, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer spremiste.Zatvori()
	repository.SetSpremiste(spremiste)
	repo := repository.NewPrijavaRepository(baza, ledger.New(baza, "test"))
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1500))
	for y := 0; y < 1500; y += 3 {
		img.Set(y%2000, y, color.RGBA{200, 40, 40, 255})
	}
	var slika bytes.Buffer
	_ = png.Encode(&slika, img)
	src := probniIzvor{zbirke: map[string][]json.RawMessage{
		"users": raw(`{"id":"u-1","first_name":"Željko","last_name":"Brdar"}`, `{"id":"u-9","first_name":"Netko","last_name":"Nepoznat"}`),
		"vode":  raw(`{"id":201,"voda":"Kanal B1"}`),
		"obavijesti_sa_terena": raw(
			`{"id":10,"status":"objavljeno","date_created":"2024-04-23T08:15:00.000Z","user_created":"u-1","vodocuvarsko_podrucje":"DUNAVSKI SEKTOR - SJEVER","cuvarnica":"DARDA","vrsta_dokumenta":"PRIJAVA","naslov":"SMEĆE U KANALU KAMENAC","Konstrukcijski_element":"KANAL","opis":"Obilaskom kanala uočio sam smeće.","vaznost_objekta":"HRVATSKE VODE - JVD","naziv_vodotoka":201,"slika_1":"s1","slika_2":null,"slika_3":null,"datoteka":"d1","lokacija":{"type":"Point","coordinates":[18.76,45.65]}}`,
			`{"id":11,"status":"arhivirano","date_created":"2024-05-02T10:00:00.000Z","user_created":"u-1","vodocuvarsko_podrucje":"KARAŠICA SEKTOR","cuvarnica":"-","vrsta_dokumenta":"OBAVIJEST","naslov":"Ustava 3-12","opis":"Savijen nišač.","slika_1":"s1","datoteka":null,"lokacija":null}`,
			`{"id":12,"status":"skica","date_created":"2024-05-03T10:00:00.000Z","user_created":"u-1","vodocuvarsko_podrucje":"KARAŠICA SEKTOR","vrsta_dokumenta":"ZAHTJEV","naslov":"Nacrt","opis":"x"}`,
			`{"id":13,"status":"objavljeno","date_created":"2024-05-04T10:00:00.000Z","user_created":"u-9","vodocuvarsko_podrucje":"KARAŠICA SEKTOR","vrsta_dokumenta":"OBAVIJEST","naslov":"Tuđa","opis":"x"}`,
		),
	}}
	deps := PrijaveDeps{
		Prijave:   repo,
		Korisnici: map[string]KorisnikUvoza{"Željko Brdar": {ID: "g-1", Ime: "Željko Brdar", Sektor: "B"}},
		Podrucja:  map[string]int{"DUNAVSKI SEKTOR - SJEVER": 34, "KARAŠICA SEKTOR": 16},
		Sektor:    "B", Cvor: "cop-osijek",
		Datoteka: func(_ context.Context, id, upit string) ([]byte, error) {
			if id == "d1" {
				return []byte("%PDF-1.4\nsken potpisane prijave"), nil
			}
			return slika.Bytes(), nil
		},
		IzradiPDF: func(p *models.PrijavaSTerena, slike map[string][]byte) []byte {
			return []byte("%PDF-1.4\nrekonstrukcija " + p.Oznaka())
		},
	}
	// suhi prolaz ništa ne upisuje
	deps.DryRun = true
	rep, err := RunPrijave(context.Background(), src, deps)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Upisano != 3 || rep.BezKorisnika != 1 || rep.Nacrta != 1 || rep.Skenova != 1 || rep.Slika != 2 {
		t.Fatalf("suhi prolaz: %+v", rep)
	}
	if sve, _ := repo.List(context.Background(), repository.FiltarPrijava{}); len(sve) != 0 {
		t.Fatal("suhi prolaz upisao")
	}
	deps.DryRun = false
	if rep, err = RunPrijave(context.Background(), src, deps); err != nil || rep.Upisano != 3 {
		t.Fatalf("uvoz: %+v %v", rep, err)
	}
	sve, _ := repo.List(context.Background(), repository.FiltarPrijava{Sektor: "B"})
	if len(sve) != 3 {
		t.Fatalf("prijava: %d", len(sve))
	}
	var prijava, obavijest *models.PrijavaSTerena
	for i := range sve {
		switch sve[i].Naslov {
		case "SMEĆE U KANALU KAMENAC":
			prijava = &sve[i]
		case "Ustava 3-12":
			obavijest = &sve[i]
		}
	}
	if prijava == nil || prijava.Vrsta != models.PrijavaPrijava || prijava.AreaID != 34 || prijava.UserID != "g-1" || prijava.Vodotok != "Kanal B1" || prijava.Element != "KANAL" ||
		!prijava.ImaKoordinate() || *prijava.Latitude != 45.65 || prijava.Status != models.PrijavaObjavljena || prijava.Broj != 1 || prijava.Godina != 2024 || !prijava.Rekonstrukcija || prijava.Objekt != "čuvarnica Darda" {
		t.Fatalf("prijava: %+v", prijava)
	}
	if len(prijava.Slike) != 1 || prijava.Slike[0].Sirina != 1000 {
		t.Fatalf("slike prijave: %+v", prijava.Slike)
	}
	if iz, _ := repo.Izvornik(context.Background(), prijava.ID); iz == nil || !bytes.Contains(iz.PDF, []byte("rekonstrukcija B-T-1/2024")) || prijava.Sken != "" {
		t.Fatal("izvornik treba biti PDF iz podataka, sken se ne preuzima kad ga nema kamo spremiti")
	}
	if obavijest == nil || obavijest.Status != models.PrijavaArhivirana || obavijest.AreaID != 16 || obavijest.Broj != 2 || obavijest.Objekt != "" {
		t.Fatalf("obavijest: %+v", obavijest)
	}
	if iz, _ := repo.Izvornik(context.Background(), obavijest.ID); iz == nil || !bytes.Contains(iz.PDF, []byte("rekonstrukcija B-T-2/2024")) {
		t.Fatal("bez skena nema PDF-a iz podataka")
	}
	if b, _ := repo.Slika(context.Background(), obavijest.Slike[0].ID); len(b) == 0 {
		t.Fatal("slika nije spremljena")
	}
	// ponovni uvoz: sve već postoji
	if rep, _ = RunPrijave(context.Background(), src, deps); rep.Postoje != 3 || rep.Upisano != 0 {
		t.Fatalf("ponovni uvoz: %+v", rep)
	}
	// sa spremanjem skena na disk: izvornik je PDF iz podataka, sken prilog
	skenovi := map[string][]byte{}
	deps.SpremiSken = func(p *models.PrijavaSTerena, pdf []byte) (string, error) {
		skenovi[p.ID] = pdf
		return p.ID + ".pdf", nil
	}
	src.zbirke["obavijesti_sa_terena"] = raw(`{"id":15,"status":"objavljeno","date_created":"2024-06-02T10:00:00.000Z","user_created":"u-1","vodocuvarsko_podrucje":"KARAŠICA SEKTOR","vrsta_dokumenta":"PRIJAVA","naslov":"Sa skenom","opis":"x","datoteka":"d1"}`)
	if rep, _ = RunPrijave(context.Background(), src, deps); rep.Upisano != 1 || rep.Skenova != 1 {
		t.Fatalf("uvoz sa skenom na disk: %+v", rep)
	}
	// prvi prolaz je bio bez mjesta za sken, pa ga nije ni brojio
	if rep.Slika != 0 {
		t.Fatalf("slike u prolazu sa skenom: %+v", rep)
	}
	if p, _ := repo.PoIzvoru(context.Background(), "bp16:obavijesti_sa_terena:15"); p == nil || p.Sken != p.ID+".pdf" || len(skenovi[p.ID]) == 0 {
		t.Fatalf("sken na disku: %+v", p)
	} else if iz, _ := repo.Izvornik(context.Background(), p.ID); iz == nil || !bytes.Contains(iz.PDF, []byte("rekonstrukcija")) {
		t.Fatal("izvornik uz sken treba biti PDF iz podataka")
	}
	deps.SpremiSken = nil
	// s brisanjem slika odmah: slike se ne spremaju, PDF ih nosi
	deps.SlikeOdmah = true
	src.zbirke["obavijesti_sa_terena"] = raw(`{"id":14,"status":"objavljeno","date_created":"2024-06-01T10:00:00.000Z","user_created":"u-1","vodocuvarsko_podrucje":"KARAŠICA SEKTOR","vrsta_dokumenta":"IZVJEŠĆE","naslov":"Obilazak","opis":"x","slika_1":"s1"}`)
	if rep, _ = RunPrijave(context.Background(), src, deps); rep.Upisano != 1 {
		t.Fatalf("uvoz s brisanjem: %+v", rep)
	}
	p, _ := repo.PoIzvoru(context.Background(), "bp16:obavijesti_sa_terena:14")
	if p == nil || len(p.Slike) != 1 {
		t.Fatalf("izvješće: %+v", p)
	}
	if b, _ := repo.Slika(context.Background(), p.Slike[0].ID); len(b) != 0 {
		t.Error("slika spremljena iako se briše odmah")
	}
}
