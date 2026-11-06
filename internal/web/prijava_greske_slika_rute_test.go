package web

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// sadrzajZaPrijave otvara spremište sadržaja za test i vraća ga
func sadrzajZaPrijave(t *testing.T) *sadrzaj.Spremiste {
	t.Helper()
	prije := repository.Spremiste()
	t.Cleanup(func() { repository.SetSpremiste(prije) })
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sp.Zatvori(); err != nil {
			t.Error(err)
		}
	})
	repository.SetSpremiste(sp)
	return sp
}

func pngZaPrijavu(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// obrazacSaSlikom šalje obrazac prijave sa slikom, kao preglednik; ime datoteke
// i vrsta su ono što preglednik tvrdi
func (o *okolinaPrijaveGreske) obrazacSaSlikom(sesija, opis, imeDatoteke, tvrdnja string, slika []byte) *httptest.ResponseRecorder {
	o.t.Helper()
	tijelo, vrsta := o.tijeloObrasca(opis, imeDatoteke, tvrdnja, slika)
	r := izravno(httptest.NewRequest(http.MethodPost, "/prijava-greske", tijelo))
	r.Header.Set("Content-Type", vrsta)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: sesija})
	w := httptest.NewRecorder()
	o.srv.ServeHTTP(w, r)
	return w
}

// tijeloObrasca sastavlja obrazac prijave sa slikom i vraća tijelo i njegovu vrstu
func (o *okolinaPrijaveGreske) tijeloObrasca(opis, imeDatoteke, tvrdnja string, slika []byte) (*bytes.Buffer, string) {
	o.t.Helper()
	var tijelo bytes.Buffer
	m := multipart.NewWriter(&tijelo)
	for ime, vrijednost := range map[string]string{"vrsta": "greska", "opis": opis, "stranica": "/karta", "naziv_stranice": "Karta"} {
		if err := m.WriteField(ime, vrijednost); err != nil {
			o.t.Fatal(err)
		}
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="slika"; filename="`+imeDatoteke+`"`)
	h.Set("Content-Type", tvrdnja)
	dio, err := m.CreatePart(h)
	if err != nil {
		o.t.Fatal(err)
	}
	if _, err := dio.Write(slika); err != nil {
		o.t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		o.t.Fatal(err)
	}
	return &tijelo, m.FormDataContentType()
}

func (o *okolinaPrijaveGreske) zadnjaPrijava(userID string) models.PrijavaGreske {
	o.t.Helper()
	moje, err := o.repo.PoKorisniku(context.Background(), userID, 1)
	if err != nil || len(moje) != 1 {
		o.t.Fatalf("%d %v", len(moje), err)
	}
	return moje[0]
}

// Slika stigne s obrascem, vide je administrator i osoba koja ju je poslala, a
// nitko drugi; poslužuje se kao slika, bez mogućnosti izvršavanja
func TestPrijavaGreskeSlikaKrozRutu(t *testing.T) {
	sadrzajZaPrijave(t)
	o := novaOkolinaPrijaveGreske(t)
	slika := pngZaPrijavu(t)
	// preglednik tvrdi JPEG, a vrijedi ono što je u bajtovima
	w := o.obrazacSaSlikom(o.mara, "Karta je prazna.", "zaslon.jpg", "image/jpeg", slika)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	p := o.zadnjaPrijava(o.maraID)
	if p.SlikaVrsta != models.SlikaPrijavePNG || p.SlikaBajtova != len(slika) || p.SlikaOtisak != sadrzaj.Otisak(slika) {
		t.Fatalf("opis slike: %+v", p)
	}
	put := "/prijava-greske/" + p.ID + "/slika"

	for ime, sesija := range map[string]string{"osoba koja je poslala": o.mara, "administrator": o.ivo} {
		w := o.zahtjev(http.MethodGet, put, sesija, nil, nil)
		if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), slika) {
			t.Errorf("%s: %d", ime, w.Code)
		}
		if w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" ||
			!strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline") ||
			!strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("%s: zaglavlja %v", ime, w.Header())
		}
	}

	// neprijavljeni nikad ne dobije sliku
	if w := o.zahtjev(http.MethodGet, put, "", nil, nil); w.Code == http.StatusOK {
		t.Errorf("neprijavljeni: %d", w.Code)
	}

	// administratorova prijava sa slikom nije za običnu osobu
	if w := o.obrazacSaSlikom(o.ivo, "Moja prijava.", "a.png", "image/png", slika); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	sve, err := o.repo.Popis(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range sve {
		if q.UserID == o.maraID {
			continue
		}
		if w := o.zahtjev(http.MethodGet, "/prijava-greske/"+q.ID+"/slika", o.mara, nil, nil); w.Code != http.StatusNotFound {
			t.Errorf("tuđa prijava: %d", w.Code)
		}
	}
	// nepostojeća prijava i prijava bez slike
	if w := o.zahtjev(http.MethodGet, "/prijava-greske/nema-je/slika", o.ivo, nil, nil); w.Code != http.StatusNotFound {
		t.Errorf("nema je: %d", w.Code)
	}
	if w := o.zahtjev(http.MethodPost, "/prijava-greske", o.mara, obrazacPrijave("Bez slike."), map[string]string{"Sec-Fetch-Site": "same-origin"}); w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
	if w := o.zahtjev(http.MethodGet, "/prijava-greske/"+o.zadnjaPrijava(o.maraID).ID+"/slika", o.mara, nil, nil); w.Code != http.StatusNotFound {
		t.Errorf("bez slike: %d", w.Code)
	}
}

// Datoteka koja nije slika i prevelika slika odbijaju se rečenicom za osobu, a
// prijava se ne sprema
func TestPrijavaGreskeLosaSlika(t *testing.T) {
	sadrzajZaPrijave(t)
	o := novaOkolinaPrijaveGreske(t)
	for _, s := range []struct {
		ime     string
		slika   []byte
		status  int
		poruka  string
		tvrdnja string
	}{
		{"SVG sa skriptom pod imenom slike", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), http.StatusBadRequest, "mora biti PNG, JPEG ili WebP", "image/png"},
		{"HTML", []byte("<html></html>"), http.StatusBadRequest, "mora biti PNG, JPEG ili WebP", "image/png"},
		{"preko granice slike", make([]byte, models.NajvecaSlikaPrijave+1), http.StatusRequestEntityTooLarge, "Najviše je 2 MB", "image/png"},
		{"preko granice tijela", make([]byte, models.NajvecaSlikaPrijave+2<<20), http.StatusRequestEntityTooLarge, "", "image/png"},
	} {
		w := o.obrazacSaSlikom(o.mara, "Karta je prazna.", "zaslon.png", s.tvrdnja, s.slika)
		if w.Code != s.status || !strings.Contains(w.Body.String(), s.poruka) {
			t.Errorf("%s: %d %s", s.ime, w.Code, w.Body.String())
		}
	}
	if moje, err := o.repo.PoKorisniku(context.Background(), o.maraID, 5); err != nil || len(moje) != 0 {
		t.Errorf("spremljeno: %d %v", len(moje), err)
	}
	// i odgovor koji je obrazac dobio u JSON-u kaže isto
	w := o.obrazacSaSlikom(o.mara, "Karta je prazna.", "zaslon.png", "image/png", []byte("nije slika"))
	var d odgovorPrijave
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || d.Success || d.Error == "" {
		t.Errorf("%+v %v", d, err)
	}
	// prazna datoteka znači da slike nema
	if w := o.obrazacSaSlikom(o.mara, "Karta je prazna.", "", "application/octet-stream", nil); w.Code != http.StatusOK {
		t.Errorf("prazna: %d %s", w.Code, w.Body.String())
	}
}

// Otvorena prijava pokazuje sliku, a dok slika još nije stigla razmjenom kaže
// to; Moje prijave nose poveznicu, a poslana poruka piše uz podatke programa
func TestPrijavaGreskeSlikaNaStranicama(t *testing.T) {
	sp := sadrzajZaPrijave(t)
	o := novaOkolinaPrijaveGreske(t)
	slika := pngZaPrijavu(t)
	if w := o.obrazacSaSlikom(o.mara, "Karta je prazna.", "zaslon.png", "image/png", slika); w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
	p := o.zadnjaPrijava(o.maraID)
	put := putPopisaPrijava + "/" + p.ID
	w := o.zahtjev(http.MethodGet, put, o.ivo, nil, nil)
	tijelo := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(tijelo, `src="/prijava-greske/`+p.ID+`/slika"`) || !strings.Contains(tijelo, "Otvori sliku zaslona") {
		t.Fatalf("%d: slike nema na stranici", w.Code)
	}
	if strings.Contains(tijelo, "E-pošta administratoru") {
		t.Error("poruka nije poslana")
	}

	if err := o.repo.ZabiljeziPoruku(context.Background(), p.ID, time.Date(2026, 11, 6, 16, 42, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := sp.Odvezi(context.Background(), "prijave_gresaka", p.ID); err != nil {
		t.Fatal(err)
	}
	if err := sp.Ukloni(context.Background(), p.SlikaOtisak); err != nil {
		t.Fatal(err)
	}
	tijelo = o.zahtjev(http.MethodGet, put, o.ivo, nil, nil).Body.String()
	if strings.Contains(tijelo, "/slika") || !strings.Contains(tijelo, "još nije stigla na ovo računalo") {
		t.Error("slika koja još nije stigla")
	}
	if !strings.Contains(tijelo, "E-pošta administratoru poslana je 6. 11. 2026. u 17:42.") {
		t.Error("poslana poruka")
	}
	if w := o.zahtjev(http.MethodGet, "/prijava-greske/"+p.ID+"/slika", o.ivo, nil, nil); w.Code != http.StatusNotFound {
		t.Errorf("slika koje nema: %d", w.Code)
	}
	// Moje prijave isto: dok slika nije stigla, kaže to umjesto poveznice koja ne vodi nikamo
	moje := o.zahtjev(http.MethodGet, "/profile", o.mara, nil, nil).Body.String()
	if strings.Contains(moje, `href="/prijava-greske/`+p.ID+`/slika"`) || !strings.Contains(moje, "još nije stigla na ovo računalo") {
		t.Error("Moje prijave: slika koja još nije stigla")
	}
}

// Kad je rok prošao, stranice kažu da je slika uklonjena, a prijava i tekst ostaju.
// Slika se ne da otvoriti ni dok bajtovi još nisu obrisani.
func TestUklonjenaSlikaNaStranicama(t *testing.T) {
	sp := sadrzajZaPrijave(t)
	o := novaOkolinaPrijaveGreske(t)
	if w := o.obrazacSaSlikom(o.mara, "Karta je prazna.", "zaslon.png", "image/png", pngZaPrijavu(t)); w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
	p := o.zadnjaPrijava(o.maraID)
	put := putPopisaPrijava + "/" + p.ID
	slika := "/prijava-greske/" + p.ID + "/slika"
	const recenica = "Slika zaslona je uklonjena jer je prošlo 30 dana otkako je prijava riješena."
	if strings.Contains(o.zahtjev(http.MethodGet, put, o.ivo, nil, nil).Body.String(), recenica) {
		t.Error("slika nije uklonjena")
	}
	if _, err := o.baza.Exec(`UPDATE prijave_gresaka SET stanje = ?, updated_at = ? WHERE id = ?`,
		models.StanjePrijaveRijeseno, time.Now().Add(-models.RokSlikePrijave-time.Hour).UTC(), p.ID); err != nil {
		t.Fatal(err)
	}
	provjeri := func(kada string) {
		t.Helper()
		tijelo := o.zahtjev(http.MethodGet, put, o.ivo, nil, nil).Body.String()
		if !strings.Contains(tijelo, recenica) || strings.Contains(tijelo, slika) || strings.Contains(tijelo, "još nije stigla") || !strings.Contains(tijelo, "Karta je prazna.") {
			t.Errorf("%s: otvorena prijava", kada)
		}
		moje := o.zahtjev(http.MethodGet, "/profile", o.mara, nil, nil).Body.String()
		if !strings.Contains(moje, recenica) || strings.Contains(moje, slika) || !strings.Contains(moje, "Karta je prazna.") {
			t.Errorf("%s: Moje prijave", kada)
		}
		for ime, sesija := range map[string]string{"osoba": o.mara, "administrator": o.ivo} {
			if w := o.zahtjev(http.MethodGet, slika, sesija, nil, nil); w.Code != http.StatusNotFound {
				t.Errorf("%s: %s dobije sliku: %d", kada, ime, w.Code)
			}
		}
	}
	provjeri("bajtovi još postoje")
	if !sp.Ima(context.Background(), p.SlikaOtisak) {
		t.Fatal("bajtovi su već nestali")
	}
	if _, err := o.repo.UkloniIstekleSlike(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if sp.Ima(context.Background(), p.SlikaOtisak) {
		t.Error("bajtovi su ostali")
	}
	provjeri("bajtovi obrisani")
}

// Slika od točno 2 MB stigne kroz cijeli obrazac
func TestPrijavaGreskeSlikaNaGranici(t *testing.T) {
	sadrzajZaPrijave(t)
	o := novaOkolinaPrijaveGreske(t)
	mala := pngZaPrijavu(t)
	dodatak := models.NajvecaSlikaPrijave - len(mala) - 12
	blok := binary.BigEndian.AppendUint32(nil, uint32(dodatak))
	blok = append(blok, "tEXt"...)
	blok = append(blok, make([]byte, dodatak)...)
	blok = binary.BigEndian.AppendUint32(blok, crc32.ChecksumIEEE(blok[4:]))
	slika := append(append(append([]byte(nil), mala[:33]...), blok...), mala[33:]...)
	if w := o.obrazacSaSlikom(o.mara, "Karta je prazna.", "zaslon.png", "image/png", slika); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if p := o.zadnjaPrijava(o.maraID); p.SlikaBajtova != models.NajvecaSlikaPrijave {
		t.Errorf("%d", p.SlikaBajtova)
	}
}

// Moje prijave nose poveznicu kad je slika na ovom računalu
func TestMojePrijavePoveznicaNaSliku(t *testing.T) {
	sadrzajZaPrijave(t)
	o := novaOkolinaPrijaveGreske(t)
	if w := o.obrazacSaSlikom(o.mara, "Karta je prazna.", "zaslon.png", "image/png", pngZaPrijavu(t)); w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
	moje := o.zahtjev(http.MethodGet, "/profile", o.mara, nil, nil).Body.String()
	if !strings.Contains(moje, `href="/prijava-greske/`+o.zadnjaPrijava(o.maraID).ID+`/slika"`) || strings.Contains(moje, "još nije stigla na ovo računalo") {
		t.Error("Moje prijave bez poveznice na sliku")
	}
}

// Preko pravog poslužitelja, kroz sve slojeve: slika veća od granice u memoriji
// ide u privremenu datoteku, koja ne smije ostati ni kad je slika primljena ni kad
// je odbijena
func TestPrijavaGreskeNeOstavljaPrivremeneDatoteke(t *testing.T) {
	sadrzajZaPrijave(t)
	o := novaOkolinaPrijaveGreske(t)
	mapa := t.TempDir()
	t.Setenv("TMPDIR", mapa)
	srv := httptest.NewServer(o.srv)
	defer srv.Close()

	velika := image.NewRGBA(image.Rect(0, 0, 512, 512))
	izvor := rand.New(rand.NewPCG(1, 2))
	for i := range velika.Pix {
		velika.Pix[i] = byte(izvor.IntN(256))
	}
	var slika bytes.Buffer
	if err := png.Encode(&slika, velika); err != nil {
		t.Fatal(err)
	}
	if slika.Len() <= najvecaPrijavaUMemoriji {
		t.Fatalf("slika stane u memoriju: %d", slika.Len())
	}
	for _, s := range []struct {
		ime    string
		slika  []byte
		status int
	}{
		{"primljena", slika.Bytes(), http.StatusOK},
		{"odbijena", make([]byte, slika.Len()), http.StatusBadRequest},
	} {
		tijelo, vrsta := o.tijeloObrasca("Karta je prazna: "+s.ime, "zaslon.png", "image/png", s.slika)
		r, err := http.NewRequest(http.MethodPost, srv.URL+"/prijava-greske", tijelo)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", vrsta)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.AddCookie(&http.Cookie{Name: imeKolacicaSesije, Value: o.mara})
		odgovor, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_ = odgovor.Body.Close()
		if odgovor.StatusCode != s.status {
			t.Fatalf("%s: %d", s.ime, odgovor.StatusCode)
		}
		ostalo, err := filepath.Glob(filepath.Join(mapa, "multipart-*"))
		if err != nil || len(ostalo) != 0 {
			t.Errorf("%s: ostale su privremene datoteke %v %v", s.ime, ostalo, err)
		}
	}
}

// Obrazac nudi sliku samo u tri vrste, a bez servisa slika nema
func TestPrijavaGreskeObrazacISlikaBezServisa(t *testing.T) {
	o := novaOkolinaPrijaveGreske(t)
	tijelo := o.zahtjev(http.MethodGet, "/profile", o.mara, nil, nil).Body.String()
	if !strings.Contains(tijelo, `accept="image/png,image/jpeg,image/webp"`) || !strings.Contains(tijelo, "Dodaj sliku zaslona (po želji)") {
		t.Error("obrazac bez slike")
	}
	o.server.SetPrijaveGresaka(nil)
	if w := o.zahtjev(http.MethodGet, "/prijava-greske/x/slika", o.mara, nil, nil); w.Code != http.StatusNotFound {
		t.Errorf("%d", w.Code)
	}
}

// Datoteka koja se ne da otvoriti nije greška osobe: odgovor kaže da prijava nije
// spremljena, a tekst ostaje u obrascu
func TestSlikaIzObrascaSeNeDaOtvoriti(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/prijava-greske", nil)
	r.MultipartForm = &multipart.Form{File: map[string][]*multipart.FileHeader{"slika": {{Filename: "a.png", Size: 1}}}}
	_, err := slikaIzObrasca(r)
	if err == nil {
		t.Fatal("datoteke nema")
	}
	w := httptest.NewRecorder()
	odbijPrijavu(w, err)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Tekst je ostao ovdje") {
		t.Errorf("%d %s", w.Code, w.Body.String())
	}
}
