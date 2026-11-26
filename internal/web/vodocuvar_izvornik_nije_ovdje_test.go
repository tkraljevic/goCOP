package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
	"gocop/internal/service"
)

// Predan list čiji je izvornik stigao razmjenom samo kao zapis (PDF ovo
// računalo po pretplati ne drži ili ga još čeka): stranica to kaže, PDF lista
// se ne crta iznova pod imenom izvornika, a ovjera se odbija prije upisa, da
// novi izvornik ne zamijeni potpisani na svim računalima
func TestListKojemuIzvornikNijeOvdje(t *testing.T) {
	o := novaOkolinaDnevnika(t)
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	prijasnje := repository.Spremiste()
	repository.SetSpremiste(sp)
	t.Cleanup(func() {
		repository.SetSpremiste(prijasnje)
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	o.mux.HandleFunc("GET /vodocuvar/{id}/list.pdf", o.vodH.IzvoziPDF)
	o.mux.HandleFunc("POST /vodocuvar/{id}/radnja", o.vodH.HandleRadnja)

	ctx := context.Background()
	// jedan list po vodočuvaru i danu: svaki list u testu ima svoj dan
	predan := func(pomak, broj int, prilozi ...models.PrilogLista) *models.VodocuvarskiList {
		t.Helper()
		dan, err := time.ParseInLocation("2006-01-02", danZaTest(pomak), models.Zagreb)
		if err != nil {
			t.Fatal(err)
		}
		kad := time.Now()
		l := &models.VodocuvarskiList{UserID: o.vodocuvar.ID.String(), Ime: "Pero Perić", Sektor: "P", AreaID: 1, Datum: dan, Broj: broj,
			Od: "07:00", Do: "15:00", Opis: "obilazak nasipa", PredanoAt: &kad, Prilozi: prilozi}
		if err := o.repo.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	// zapis izvornika kakav stigne razmjenom: otisak, a PDF-a u spremištu nema
	zapisIzvornika := func(l *models.VodocuvarskiList, otisak string) {
		t.Helper()
		if _, err := o.baza.Exec(`INSERT INTO vodocuvarski_izvornici (list_id, otisak, bajtova, vrsta, sazetak, updated_at)
			VALUES (?, ?, 1234, 'application/pdf', 'sazetak-predaje', ?)`, l.ID, otisak, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	greska := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		u, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		return u.Query().Get("error")
	}

	l := predan(0, 7, models.PrilogLista{ID: "slika-nasipa", Vrsta: "image/png"})
	pdfPredaje := PDFListaSPrilozima(l, models.Terms(), nil, models.OtisciLista{}, PrilogListaPDF{})
	otisak := sadrzaj.Otisak(pdfPredaje)
	zapisIzvornika(l, otisak)

	stranica := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String()
	for _, x := range []string{"Izvornik lista, PDF kakav je predan, nije na ovom računalu", "Ovjera se upisuje u izvornik", "Fotografija nije na ovom računalu."} {
		if !strings.Contains(stranica, x) {
			t.Errorf("stranica lista nema %q", x)
		}
	}
	for _, x := range []string{"potpisnici nemaju ključ", "Ovjeri list", `value="ovjeri"`, "/list.pdf", "nosi je ispis lista"} {
		if strings.Contains(stranica, x) {
			t.Errorf("stranica lista kojemu izvornik nije ovdje ima %q", x)
		}
	}
	// vodočuvar ne ovjerava, pa mu stranica ne spominje ovjeru
	if s := o.zovi(t, o.vodocuvar, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String(); !strings.Contains(s, "nije na ovom računalu, pa se ovdje ne mogu provjeriti potpisi") || strings.Contains(s, "Ovjera se upisuje") {
		t.Error("vodočuvar: stranica ne kaže da izvornik nije ovdje ili mu spominje ovjeru")
	}

	w := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+l.ID+"/list.pdf", nil)
	if w.Code != http.StatusSeeOther || strings.HasPrefix(w.Body.String(), "%PDF") || greska(w) != porukaPDFNijeOvdje {
		t.Errorf("PDF lista kojemu izvornik nije ovdje: %d, greška %q", w.Code, greska(w))
	}
	if cd := w.Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("odbijen PDF ima ime datoteke %q", cd)
	}

	w = o.zovi(t, o.rukovodi, http.MethodPost, "/vodocuvar/"+l.ID+"/radnja", url.Values{"radnja": {"ovjeri"}})
	if w.Code != http.StatusSeeOther || greska(w) != service.ErrIzvornikNijeOvdje.Error() {
		t.Errorf("ovjera lista kojemu izvornik nije ovdje: %d, greška %q", w.Code, greska(w))
	}
	// tko list ne smije ovjeriti dobiva svoju poruku, ne uputu o izvorniku
	if g := greska(o.zovi(t, o.vodocuvar, http.MethodPost, "/vodocuvar/"+l.ID+"/radnja", url.Values{"radnja": {"ovjeri"}})); g == "" || g == service.ErrIzvornikNijeOvdje.Error() {
		t.Errorf("vodočuvar ovjerava vlastiti list: greška %q", g)
	}
	if nakon, err := o.repo.Get(ctx, l.ID); err != nil || nakon == nil || nakon.Potvrden() {
		t.Errorf("list je ovjeren iako izvornik nije ovdje (%v)", err)
	}
	if iz, err := o.repo.GetIzvornik(ctx, l.ID); err != nil || iz == nil || iz.Otisak != otisak || iz.Sazetak != "sazetak-predaje" {
		t.Errorf("izvornik predaje je promijenjen: %+v, %v", iz, err)
	}

	// druga obrana: izvornikOvjere ni sam ne crta novi izvornik umjesto onog
	// koji nije ovdje
	kad := time.Now()
	ovjeren := *l
	ovjeren.PotvrdenoAt, ovjeren.Potvrdio, ovjeren.PotvrdioID = &kad, o.rukovodi.FullName, o.rukovodi.ID.String()
	rukovodi, err := o.users.GetUserByID(o.rukovodi.ID)
	if err != nil {
		t.Fatal(err)
	}
	perms := models.NewUserPermissions(*rukovodi)
	if err := o.vodH.izvornikOvjere(ctx, o.vod, perms, &ovjeren, nil); err != service.ErrIzvornikNijeOvdje {
		t.Errorf("izvornikOvjere bez izvornika ovdje: %v", err)
	}
	if iz, err := o.repo.GetIzvornik(ctx, l.ID); err != nil || iz == nil || iz.Otisak != otisak {
		t.Error("izvornikOvjere je zamijenio izvornik koji nije ovdje")
	}

	// PDF predaje stigne razmjenom pod istim otiskom: ovjera se dopisuje na
	// njegove bajtove, a PDF lista je baš taj izvornik
	if err := sp.UpisiProvjereno(ctx, otisak, "application/pdf", pdfPredaje, "razmjena"); err != nil {
		t.Fatal(err)
	}
	if s := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+l.ID, nil).Body.String(); !strings.Contains(s, "Ovjeri list") || strings.Contains(s, "nije na ovom računalu, pa se ovdje") {
		t.Error("list s izvornikom ovdje: nema gumba ovjere ili piše da izvornik nije ovdje")
	}
	if w := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+l.ID+"/list.pdf", nil); w.Code != http.StatusOK || w.Body.String() != string(pdfPredaje) {
		t.Errorf("PDF lista s izvornikom ovdje: %d, nije izvornik predaje", w.Code)
	}
	if g := greska(o.zovi(t, o.rukovodi, http.MethodPost, "/vodocuvar/"+l.ID+"/radnja", url.Values{"radnja": {"ovjeri"}})); g != "" {
		t.Errorf("ovjera lista s izvornikom ovdje: %q", g)
	}
	if nakon, err := o.repo.Get(ctx, l.ID); err != nil || nakon == nil || !nakon.Potvrden() {
		t.Error("list s izvornikom ovdje nije ovjeren")
	}
	if iz, err := o.repo.GetIzvornik(ctx, l.ID); err != nil || iz == nil || !bytes.HasPrefix(iz.PDF, pdfPredaje) || len(iz.PDF) == len(pdfPredaje) {
		t.Error("ovjera nije dopisana na bajtove izvornika predaje")
	}

	// kontrola: predan list bez ikakvog izvornika crta se iznova, kao i prije
	bez := predan(-1, 6)
	if w := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+bez.ID+"/list.pdf", nil); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("predan list bez izvornika: %d, nije PDF", w.Code)
	} else if w.Body.String() == string(pdfPredaje) {
		t.Error("predan list bez izvornika dobio je tuđi izvornik")
	}

	// izvornik se ne da pročitati: PDF se ne crta, ovjera se ne upisuje
	if _, err := o.baza.Exec(`DROP TABLE vodocuvarski_izvornici`); err != nil {
		t.Fatal(err)
	}
	if w := o.zovi(t, o.rukovodi, http.MethodGet, "/vodocuvar/"+bez.ID+"/list.pdf", nil); w.Code != http.StatusInternalServerError || strings.HasPrefix(w.Body.String(), "%PDF") {
		t.Errorf("PDF lista kad se izvornik ne da pročitati: %d", w.Code)
	}
	if g := greska(o.zovi(t, o.rukovodi, http.MethodPost, "/vodocuvar/"+bez.ID+"/radnja", url.Values{"radnja": {"ovjeri"}})); !strings.HasPrefix(g, "ovjera nije upisana: izvornik lista nije se dao pročitati") {
		t.Errorf("ovjera kad se izvornik ne da pročitati: %q", g)
	}
	if nakon, err := o.repo.Get(ctx, bez.ID); err != nil || nakon == nil || nakon.Potvrden() {
		t.Error("list je ovjeren iako se izvornik nije dao pročitati")
	}
}
