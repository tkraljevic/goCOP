package web

import (
	"archive/zip"
	"bytes"
	"context"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gocop/internal/db"
	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/service"
	webassets "gocop/web"
)

// rukovateljSektora je rukovatelj s registrom sektora iz predloška baze i
// dvije letve na profilu iste vode: Gornji Brod prati sektor A, Donji Brod
// sektor B, a sektor C nema letvi. Imena su izmišljena.
func rukovateljSektora(t *testing.T) *PrognozeHandler {
	t.Helper()
	h := rukovateljBaze(t, bazaPrognoza(t, time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC).Unix()/3600))
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "op.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { baza.Close() })
	rec := ledger.New(baza, "test")
	stanice := repository.NewStationRepository(baza, rec)
	ocitanja := repository.NewReadingRepository(baza, rec)
	korisnici := repository.NewUserRepository(baza, rec)
	h.stations = service.NewStationService(stanice, nil, nil)
	h.SetReadings(service.NewReadingService(ocitanja, stanice, nil, nil, nil))
	h.SetWatercourses(service.NewWatercourseService(repository.NewWatercourseRepository(baza, rec)))
	h.SetUsers(service.NewUserService(korisnici, service.NewAuthService(korisnici, repository.NewSessionRepository(baza)), service.NewSSEBroker()))
	// sektori i područja kao u registru, s izmišljenim imenima područja
	for _, s := range []struct{ id, ime string }{{"A", "Sektor A — Mura i gornja Drava"}, {"B", "Sektor B — Dunav i donja Drava"},
		{"C", "Sektor C — Gornja Sava"}, {"DIREKCIJA", "Direkcija, Zagreb"}} {
		if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email, level) VALUES (?, ?, '', '', '', '', '', 2)`, s.id, s.ime); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := baza.Exec(`INSERT INTO areas (id, sector_id, name, vgi_name) VALUES (20, 'A', 'Područje proba', 'V'), (34, 'B', 'Područje primjer', 'V'), (8, 'C', 'Područje treće', 'V')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	kota := 80.0
	danas := time.Now().In(models.Zagreb)
	for i, l := range []struct {
		kod, ime, rkm string
		sektori       []string
	}{{"belisce", "Gornji Brod", "rkm 50+000", []string{"A"}}, {"donji-miholjac", "Donji Brod", "rkm 40+000", []string{"B"}}} {
		st := &models.Station{ID: uuid.New(), Code: l.kod, Name: l.ime, Watercourse: "Brodska rijeka", ZeroDatum: &kota,
			Stationing: l.rkm, PrateSektori: l.sektori}
		if err := stanice.CreateStation(ctx, st); err != nil {
			t.Fatal(err)
		}
		kad := []time.Time{time.Date(danas.Year(), danas.Month(), danas.Day(), JutroSat, 0, 0, 0, models.Zagreb)}
		for d := 1; d <= NulaNajmanjeDana+1; d++ {
			kad = append(kad, time.Date(danas.Year(), danas.Month(), danas.Day()-d, 12, 0, 0, 0, models.Zagreb))
		}
		for _, k := range kad {
			cm := 100 + i
			if err := ocitanja.Create(ctx, &models.Reading{StationID: st.ID.String(), MeasuredAt: k.UTC(),
				LevelCm: &cm, Source: models.ReadingSourceManual}); err != nil {
				t.Fatal(err)
			}
		}
	}
	templatesFS, err := fs.Sub(webassets.Files, "templates")
	if err != nil {
		t.Fatal(err)
	}
	h.tmpl = template.Must(template.New("base.html").Funcs(templateFuncs()).ParseFS(templatesFS, DijeloviPredloska("prognoze.html")...))
	return h
}

// stranicaIzbora iscrtava /prognoze s upitom kao glavni administrator.
func stranicaIzbora(t *testing.T, h *PrognozeHandler, upit string) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/prognoze"+upit, nil)
	ctx := context.WithValue(r.Context(), contextKeyUser, &models.User{FullName: "Ivka Proba"})
	ctx = context.WithValue(ctx, contextKeyPerms, &models.UserPermissions{IsGlobalAdmin: true})
	w := httptest.NewRecorder()
	h.ShowPrognoze(w, r.WithContext(ctx))
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status %d", upit, w.Code)
	}
	return w.Body.String()
}

// Stranica kroz rukovatelja: čipovi sektora iz baze (prazni zasivljeni),
// vodotoci sektora, samo letve odabranog vodotoka, „Cijeli vodotok” kad
// vodotok teče i izvan sektora, izbornici za mobitel i izvoz s istim
// izborom. ?dan= i izbor idu zajedno.
func TestStranicaIzboraSektoraIVodotoka(t *testing.T) {
	if (&PrognozeHandler{}).uscaLetvi(context.Background(), nil) != nil {
		t.Error("bez registra vodotoka nema ni ušća")
	}
	h := rukovateljSektora(t)
	kartica := func(ime string) string { return ">" + ime + "</a></h3>" }
	html := stranicaIzbora(t, h, "?sektor=B&dan=2")
	imaSve(t, html, "sektor B", []string{
		`<nav class="prog-izbor" aria-label="Sektor i vodotok">`,
		`aria-current="true">B · Dunav i donja Drava</a>`,
		`href="/prognoze?dan=2&amp;sektor=A" class="uzd-cip prog-cip">A · Mura i gornja Drava</a>`,
		`title="Nijedna letva iz prognoze nije povezana s ovim sektorom." aria-disabled="true">C · Gornja Sava</span>`,
		"Vodotoci sektora B:",
		`href="/prognoze?dan=2&amp;sektor=B&amp;vodotok=brodska-rijeka" class="uzd-cip prog-cip prog-cip-vodotok" aria-current="true">Brodska rijeka</a>`,
		`<h2 class="prog-voda">Letve: Brodska rijeka, od uzvodne prema nizvodnoj</h2>`,
		`href="/prognoze.xlsx?sektor=B&amp;vodotok=brodska-rijeka&amp;t=`,
		`<select name="sektor" class="prog-polje-izbor">`, `<option value="B" selected>B · Dunav i donja Drava</option>`,
		` disabled>C · Gornja Sava (nema povezanih letvi)</option>`, `<input type="hidden" name="dan" value="2">`, `>Prikaži</button>`,
		"Uzdužnog profila nema", // u sektoru B je samo jedna letva te vode
		`>Cijeli vodotok</a>`, `<span class="uzd-dio-opis">dio u sektoru B</span>`,
		kartica("Donji Brod"),
	})
	if strings.Contains(html, kartica("Gornji Brod")) || strings.Contains(html, "DIREKCIJA") {
		t.Error("Gornji Brod nije u sektoru B, a direkcija nije sektor")
	}

	// cijeli vodotok: obje letve i profil, s poveznicom natrag na sektor
	html = stranicaIzbora(t, h, "?sektor=B&vodotok=brodska-rijeka&cijeli=1&dan=2")
	imaSve(t, html, "cijeli vodotok", []string{
		kartica("Gornji Brod"), kartica("Donji Brod"), "cijeli vodotok, kroz sve sektore",
		`href="/prognoze?dan=2&amp;sektor=B&amp;vodotok=brodska-rijeka" class="btn btn-link">Samo sektor B</a>`,
		`href="?cijeli=1&amp;dan=2&amp;sektor=B&amp;vodotok=brodska-rijeka#profil-brodska-rijeka" class="uzd-cip"`,
		"Uzdužni profil Brodske rijeke",
	})
	if strings.Contains(html, `class="kartica-gumb"`) {
		t.Error("stare kartice po vodi ne smiju ostati uz izbor")
	}

	// bez parametara (administrator bez dužnosti): prvi sektor s letvama;
	// nepoznat sektor i vodotok daju isto, bez greške
	for _, upit := range []string{"", "?sektor=Q&vodotok=nema", "?sektor=C"} {
		html = stranicaIzbora(t, h, upit)
		imaSve(t, html, "zadano "+upit, []string{`aria-current="true">A · Mura i gornja Drava</a>`, kartica("Gornji Brod")})
		if strings.Contains(html, kartica("Donji Brod")) {
			t.Errorf("%q: Donji Brod je u sektoru B", upit)
		}
	}
}

// xlsxTekst je XML izvoza, za traženje imena letvi: uz naslov samo listovi
// koji ga nose, bez njega sve.
func xlsxTekst(t *testing.T, b []byte, naslov ...string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var sve strings.Builder
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		x, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(naslov) == 0 || strings.Contains(string(x), naslov[0]) {
			sve.Write(x)
		}
	}
	return sve.String()
}

// Izvoz slijedi izbor: uz sektor i vodotok samo te letve; bez parametara
// cijelo izdanje, isto kao i prije izbora.
func TestIzvozSlijediIzbor(t *testing.T) {
	h := rukovateljSektora(t)
	sve := izvezi(h, "")
	if sve.Code != http.StatusOK {
		t.Fatalf("status %d", sve.Code)
	}
	txt := xlsxTekst(t, sve.Body.Bytes(), "PROGNOZA VODOSTAJA — Pritoke")
	if !strings.Contains(txt, "Gornji Brod") || !strings.Contains(txt, "Donji Brod") {
		t.Error("izvoz bez parametara mora nositi cijelo izdanje")
	}
	// list vode i list grafova; list „O prognozi” nosi sve postaje lanca
	b := izvezi(h, "?sektor=B&vodotok=brodska-rijeka")
	if txt := xlsxTekst(t, b.Body.Bytes(), "PROGNOZA VODOSTAJA — Pritoke"); strings.Contains(txt, "Gornji Brod") || !strings.Contains(txt, "Donji Brod") {
		t.Error("izvoz sektora B mora nositi samo Donji Brod")
	}
	cijeli := izvezi(h, "?sektor=B&vodotok=brodska-rijeka&cijeli=1")
	if txt := xlsxTekst(t, cijeli.Body.Bytes(), "PROGNOZA VODOSTAJA — Pritoke"); !strings.Contains(txt, "Gornji Brod") || !strings.Contains(txt, "Donji Brod") {
		t.Error("izvoz cijelog vodotoka nosi obje letve")
	}
	// Isti ulaz, isti izvoz: bez parametara knjiga rukovatelja s registrom
	// sektora (stranica uvijek slaže zadani izbor) jednaka je knjizi
	// rukovatelja bez registra, kakva je bila prije izbora. Sektori u
	// registru nemaju odjel ni centar, pa ni potpis nije drukčiji.
	bez := rukovateljSektora(t)
	bez.users = nil
	if a, c := xlsxTekst(t, sve.Body.Bytes()), xlsxTekst(t, izvezi(bez, "").Body.Bytes()); a != c {
		t.Error("bez parametara izvoz mora biti isti kao prije izbora")
	}
	if a, c := xlsxTekst(t, izvezi(bez, "").Body.Bytes()), xlsxTekst(t, izvezi(bez, "?sektor=B").Body.Bytes()); a != c {
		t.Error("bez registra sektora izbor ne smije mijenjati izvoz")
	}

	// Sažetak je pregled cijelog izdanja i u suženom izvozu; ime datoteke
	// kaže da je izvoz samo dio.
	const sazetak = "PROGNOZA VODOSTAJA — KRATKI PREGLED"
	if a, c := xlsxTekst(t, sve.Body.Bytes(), sazetak), xlsxTekst(t, b.Body.Bytes(), sazetak); a == "" || a != c || !strings.Contains(c, "Gornji Brod") {
		t.Error("sažetak suženog izvoza mora biti isti kao u izvozu cijelog izdanja")
	}
	for w, zeli := range map[*httptest.ResponseRecorder]string{sve: "h.xlsx", b: "h_B_brodska-rijeka.xlsx", cijeli: "h_brodska-rijeka_cijeli.xlsx"} {
		if d := w.Header().Get("Content-Disposition"); !strings.Contains(d, zeli+`"`) && !strings.HasSuffix(d, zeli) {
			t.Errorf("ime datoteke %q, želim kraj %q", d, zeli)
		}
	}
	if imeSuzenogIzvoza("prognoza.xlsx", nil) != "prognoza.xlsx" {
		t.Error("bez izbora ime ostaje")
	}
}

// Registar sa sektorima bez područja, ili registar koji se ne da pročitati:
// izbora nema, a stranica i izvoz nose sve letve kao prije izbora.
func TestIzborBezPodrucjaSektora(t *testing.T) {
	h := rukovateljSektora(t)
	baza, err := db.BazaIzPredloska(filepath.Join(t.TempDir(), "bez.db"))
	if err != nil {
		t.Fatal(err)
	}
	korisnici := repository.NewUserRepository(baza, ledger.New(baza, "test"))
	h.SetUsers(service.NewUserService(korisnici, service.NewAuthService(korisnici, repository.NewSessionRepository(baza)), service.NewSSEBroker()))
	if _, err := baza.Exec(`INSERT INTO sectors (id, name, vgo_name, center_cop, address, phone, email, level) VALUES ('B', 'Sektor B — Primjer', '', '', '', '', '', 2)`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/prognoze?sektor=B", nil)
	if h.izborPrognoze(r, nil, nil) != nil {
		t.Error("sektor bez područja nije izbor")
	}
	html := stranicaIzbora(t, h, "?sektor=B")
	imaSve(t, html, "bez područja", []string{">Gornji Brod</a></h3>", ">Donji Brod</a></h3>", `href="/prognoze.xlsx?t=`})
	if strings.Contains(html, `class="prog-izbor"`) {
		t.Error("bez područja nema čipova sektora")
	}
	if txt := xlsxTekst(t, izvezi(h, "?sektor=B").Body.Bytes(), "PROGNOZA VODOSTAJA — Pritoke"); !strings.Contains(txt, "Gornji Brod") || !strings.Contains(txt, "Donji Brod") {
		t.Error("bez izbora izvoz nosi cijelo izdanje")
	}
	baza.Close()
	if h.izborPrognoze(r, nil, nil) != nil {
		t.Error("nečitljiv registar sektora nije izbor")
	}
}
