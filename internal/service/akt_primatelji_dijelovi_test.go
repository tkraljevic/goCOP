package service

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gocop/internal/models"
)

// Dijelovi slaganja popisa primatelja (Odluke (8. 10. 2026.), pitanje 19)

func naziviPrimatelja(p []models.AktPrimatelj) string {
	var out []string
	for _, x := range p {
		out = append(out, x.Naziv)
	}
	return strings.Join(out, "; ")
}

// Službe jedne županije: redom vrste pa redoslijeda, prevencija i 112 kao
// podstavke civilne zaštite, služba uz općinu samo za ugroženu općinu, a
// vatrogasci i stožer (prije izvanrednog stanja) ne idu na akt
func TestSluzbeZupanijeNaAktu(t *testing.T) {
	sluzbe := []models.Sluzba{
		{Vrsta: models.SluzbaPolicija, Naziv: "PU druga", Redoslijed: 2},
		{Vrsta: models.SluzbaCentar112, Naziv: "Centar 112"},
		{Vrsta: models.SluzbaPolicija, Naziv: "PU prva", Redoslijed: 1},
		{Vrsta: models.SluzbaVatrogasci, Naziv: "Vatrogasna zajednica"},
		{Vrsta: models.SluzbaStozerCZ, Naziv: "Stožer CZ"},
		{Vrsta: models.SluzbaPolicijskaPost, Naziv: "PP ugrožene općine", MunicipalityID: 8},
		{Vrsta: models.SluzbaPolicijskaPost, Naziv: "PP druge općine", MunicipalityID: 7},
		{Vrsta: models.SluzbaCivilnaZastita, Naziv: "Civilna zaštita"},
	}
	p := &slaganjePrimatelja{vidjeno: map[string]bool{}}
	dodajSluzbeZupanije(sluzbe, models.PhasePrep, map[int]bool{8: true}, p)
	if got, want := naziviPrimatelja(p.popis), "Civilna zaštita; – Centar 112; PU prva; PU druga; PP ugrožene općine"; got != want {
		t.Errorf("službe pripremnog stanja:\n%s\nželjeno:\n%s", got, want)
	}
	p = &slaganjePrimatelja{vidjeno: map[string]bool{}}
	dodajSluzbeZupanije(sluzbe, models.PhaseState, map[int]bool{8: true}, p)
	if got := naziviPrimatelja(p.popis); !strings.Contains(got, "Stožer CZ") || strings.Contains(got, "Vatrogasna") {
		t.Errorf("službe izvanrednog stanja: %s", got)
	}
	// bez civilne zaštite na popisu 112 nije podstavka
	p = &slaganjePrimatelja{vidjeno: map[string]bool{}}
	dodajSluzbeZupanije([]models.Sluzba{{Vrsta: models.SluzbaCentar112, Naziv: "Centar 112"}}, models.PhasePrep, nil, p)
	if got := naziviPrimatelja(p.popis); got != "Centar 112" {
		t.Errorf("112 bez civilne zaštite: %s", got)
	}
}

// Primatelj bez naziva se ne dodaje, a isti naziv i adresa samo jednom
func TestDodajPrimateljaJednom(t *testing.T) {
	p := &slaganjePrimatelja{vidjeno: map[string]bool{}}
	p.dodaj(models.SkupinaOsobe, "  ", "x@primjer.hr")
	p.dodaj(models.SkupinaOsobe, "Pero Perić", "pero@primjer.hr")
	p.dodaj(models.SkupinaOsobe, "PERO PERIĆ ", "pero@primjer.hr")
	p.dodaj(models.SkupinaOsobe, "Pero Perić", "")
	if got := naziviPrimatelja(p.popis); got != "Pero Perić; Pero Perić" {
		t.Errorf("popis: %s", got)
	}
}

// Osoba na popisu: titula uz ime, funkcija iz dužnosti ili, bez nje, iz
// uloge, svaka funkcija jednom
func TestOsobaNaPopisuPrimatelja(t *testing.T) {
	ana := noviPrimateljOsoba(models.SectionOfficer{FullName: "Ana Anić", Title: "dipl. ing. građ.", Email: "ana@primjer.hr"})
	if got := ana.naslov(); got != "Ana Anić, dipl. ing. građ." {
		t.Errorf("osoba bez funkcije: %q", got)
	}
	ana.dodajFunkciju(models.SectionOfficer{DutyTitle: "Rukovoditeljica BP 2"})
	ana.dodajFunkciju(models.SectionOfficer{RoleLabel: "Rukovoditelj dionice"})
	ana.dodajFunkciju(models.SectionOfficer{DutyTitle: "Rukovoditeljica BP 2"})
	ana.dodajFunkciju(models.SectionOfficer{})
	if got := ana.naslov(); got != "Ana Anić, dipl. ing. građ., Rukovoditeljica BP 2; Rukovoditelj dionice" || ana.email != "ana@primjer.hr" {
		t.Errorf("osoba s funkcijama: %q <%s>", got, ana.email)
	}
	if got := noviPrimateljOsoba(models.SectionOfficer{FullName: "Ivo Ivić"}).naslov(); got != "Ivo Ivić" {
		t.Errorf("osoba bez titule: %q", got)
	}
}

// Potvrda uz upozorenje o popisu: bez upozorenja ne treba, bez potvrde se
// pita, a potvrda se bilježi na osobu
func TestPotvrdaPopisaPrimatelja(t *testing.T) {
	u := &models.User{ID: uuid.New(), FullName: "Iva Ivić"}
	a := &models.Akt{}
	if err := potvrdiPrimatelje(a, u, nil, false, "Pitanje: ", "x"); err != nil || len(a.Potvrde) != 0 {
		t.Fatalf("bez upozorenja: %v %+v", err, a.Potvrde)
	}
	err := potvrdiPrimatelje(a, u, []string{"prvo", "drugo"}, false, "Pitanje: ", "x")
	if _, ok := err.(*TrebaPotvrduError); !ok || !strings.HasPrefix(err.Error(), "Pitanje: prvo; drugo") || len(a.Potvrde) != 0 {
		t.Fatalf("bez potvrde: %v %+v", err, a.Potvrde)
	}
	if err := potvrdiPrimatelje(a, u, []string{"prvo"}, true, "Pitanje: ", "Uvod: "); err != nil || !a.ImaPotvrdu(models.PotvrdaPrimatelji) ||
		a.Potvrde[0].Tekst != "Uvod: prvo" || a.Potvrde[0].Tko != "Iva Ivić" || a.Potvrde[0].TkoID != u.ID.String() || a.Potvrde[0].Kad.IsZero() {
		t.Fatalf("potvrda: %v %+v", err, a.Potvrde)
	}
}

// Sken: PDF ostaje kakav jest, slika postaje PDF, ništa i nečitljivo se odbija
func TestPDFSkena(t *testing.T) {
	if _, err := pdfSkena(nil, "Akt"); err == nil || !strings.Contains(err.Error(), "odaberite sken") {
		t.Errorf("prazan sken: %v", err)
	}
	pdf := []byte("%PDF-1.4\n% sken\n%%EOF\n")
	if got, err := pdfSkena(pdf, "Akt"); err != nil || !bytes.Equal(got, pdf) {
		t.Errorf("PDF: %v", err)
	}
	var slika bytes.Buffer
	if err := png.Encode(&slika, image.NewGray(image.Rect(0, 0, 20, 30))); err != nil {
		t.Fatal(err)
	}
	if got, err := pdfSkena(slika.Bytes(), "Akt"); err != nil || !bytes.HasPrefix(got, []byte("%PDF")) {
		t.Errorf("slika: %v", err)
	}
	if _, err := pdfSkena([]byte("nije ni PDF ni slika"), "Akt"); err == nil || err.Error() != "sken mora biti PDF" {
		t.Errorf("nečitljiv sken: %v", err)
	}
	// PNG sken kojem bi za čitanje trebalo previše memorije dobiva poruku
	// zašto nije primljen
	golem := slika.Bytes()
	binary.BigEndian.PutUint32(golem[16:], 30000)
	binary.BigEndian.PutUint32(golem[20:], 30000)
	binary.BigEndian.PutUint32(golem[29:], crc32.ChecksumIEEE(golem[12:29]))
	if _, err := pdfSkena(golem, "Akt"); err == nil || !strings.Contains(err.Error(), "sken je prevelik (previše točaka)") {
		t.Errorf("prevelik sken: %v", err)
	}
}

// Tekst potvrde kaže ono što se dogodilo: otisak koji se ne slaže ne broji
// razliku prema nacrtu (osoba je mogla vidjeti drukčiji popis ili samo
// drugo upozorenje), a sken kojem je jedini razlog razlika prema ispisu ne
// kaže da popis nije potpun
func TestTekstoviPotvrdePrimatelja(t *testing.T) {
	u := &models.User{ID: uuid.New(), FullName: "Iva Ivić"}
	err := primateljiZaOvjeru(u, &models.Akt{}, PotvrdaOvjere{Otisak: "vidjela"}, PregledPrimatelja{Otisak: "sada"})
	if err == nil || strings.Contains(err.Error(), "dodano 0") || !strings.Contains(err.Error(), "otkad je stranica otvorena") {
		t.Errorf("promijenjen otisak: %v", err)
	}
	err = primateljiZaOvjeru(u, &models.Akt{}, PotvrdaOvjere{}, PregledPrimatelja{Upozorenja: []string{"teritorij dionice B.1 nije pročitan"}})
	if err == nil || !strings.HasPrefix(err.Error(), "Popis primatelja nije potpun: teritorij dionice B.1") {
		t.Errorf("ovjera uz upozorenje: %v", err)
	}
	err = primateljiSkena(u, &models.Akt{}, PotvrdaOvjere{}, PregledPrimatelja{Dodani: []models.AktPrimatelj{{Naziv: "Ravnateljstvo civilne zaštite"}}})
	if err == nil || strings.Contains(err.Error(), "nije potpun") || !strings.Contains(err.Error(), "popis složen sada bio bi drukčiji od onoga na ispisu (dodano 1, uklonjeno 0)") {
		t.Errorf("sken s popisom koji bi sada bio drukčiji: %v", err)
	}
	err = primateljiSkena(u, &models.Akt{UpozorenjaPrimatelja: []string{"dionice bez upisanog teritorija: B.1"}}, PotvrdaOvjere{}, PregledPrimatelja{})
	if err == nil || !strings.Contains(err.Error(), "nije potpun") || !strings.Contains(err.Error(), "bez upisanog teritorija: B.1") {
		t.Errorf("sken uz upozorenje nacrta: %v", err)
	}
}

// Sken pita i za upozorenje uz popis složen sada kad ga nacrt ne nosi;
// upozorenje koje nosi i nacrt navodi se jednom
func TestSkenPitaZaUpozorenjeSlozenoSada(t *testing.T) {
	u := &models.User{ID: uuid.New(), FullName: "Iva Ivić"}
	sada := PregledPrimatelja{Upozorenja: []string{"dionice bez upisanog teritorija: B.1", "registar primatelja nije pročitan"}}
	err := primateljiSkena(u, &models.Akt{}, PotvrdaOvjere{}, sada)
	if err == nil || !strings.HasPrefix(err.Error(), "Popis primatelja s ispisa nije potpun: dionice bez upisanog teritorija: B.1; registar primatelja nije pročitan.") {
		t.Errorf("sken nacrta bez upozorenja: %v", err)
	}
	a := &models.Akt{UpozorenjaPrimatelja: []string{"dionice bez upisanog teritorija: B.1"}}
	if err := primateljiSkena(u, a, PotvrdaOvjere{Primatelji: true}, sada); err != nil || len(a.Potvrde) != 1 ||
		a.Potvrde[0].Tekst != "Sken ovjeren s popisom primatelja s ispisa, uz upozorenje: dionice bez upisanog teritorija: B.1; registar primatelja nije pročitan" {
		t.Errorf("potvrda: %v %+v", err, a.Potvrde)
	}
	if got := spojiUpozorenja(nil, nil); len(got) != 0 {
		t.Errorf("bez upozorenja: %v", got)
	}
}
