package service_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/razmjena"
	"gocop/internal/service"
)

// Što ide uz ovjeren akt: elektronički potpis ključem čvora, sken
// vlastoručnog potpisa, žig sektora i izvornik PDF-a.

// kljucCvora je ključ čvora iz zadanog sjemena, da testovi budu ponovljivi
func kljucCvora(sjeme byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{sjeme}, ed25519.SeedSize))
}

// Elektronički potpis akta vrijedi dok je ovjereni sadržaj isti. Akt bez
// potpisa ili bez ključa čvora nema potpis; neispravan ključ, neispravan
// potpis, potpis drugim ključem i izmjena nakon ovjere ne vrijede.
func TestProvjeraPotpisaAkta(t *testing.T) {
	kljuc := kljucCvora(7)
	kad := time.Date(2026, 10, 5, 8, 0, 0, 0, models.Zagreb)
	potpisan := func(izmijeni func(a *models.Akt)) *models.Akt {
		a := &models.Akt{ID: "akt-p-1", Sektor: "P", AreaID: 1, Broj: 3, Godina: 2026, Radnja: models.AktUspostava,
			Stupanj: models.PhasePrep, StationName: "Primjerovo", Vrijedi: kad, Status: models.AktOvjeren,
			OvjerioID: "pperic", Ovjerio: "Pero Perić", OvjerenoAt: &kad}
		a.KljucCvora = razmjena.PublicKeyString(kljuc.Public().(ed25519.PublicKey))
		a.Potpis = base64.StdEncoding.EncodeToString(ed25519.Sign(kljuc, a.PorukaPotpisa()))
		if izmijeni != nil {
			izmijeni(a)
		}
		return a
	}
	drugi := kljucCvora(8)
	for _, x := range []struct {
		opis string
		akt  *models.Akt
		zeli string
	}{
		{"bez akta", nil, service.PotpisNema},
		{"ispravan potpis", potpisan(nil), service.PotpisVrijedi},
		{"bez potpisa", potpisan(func(a *models.Akt) { a.Potpis = "" }), service.PotpisNema},
		{"bez ključa čvora", potpisan(func(a *models.Akt) { a.KljucCvora = "" }), service.PotpisNema},
		{"izmijenjen ovjeritelj", potpisan(func(a *models.Akt) { a.Ovjerio = "Ivo Ivić" }), service.PotpisNeVrijedi},
		{"izmijenjen stupanj", potpisan(func(a *models.Akt) { a.Stupanj = models.PhaseRegular }), service.PotpisNeVrijedi},
		{"ključ nije base64", potpisan(func(a *models.Akt) { a.KljucCvora = "nije ključ!" }), service.PotpisNeVrijedi},
		{"ključ krive duljine", potpisan(func(a *models.Akt) { a.KljucCvora = base64.StdEncoding.EncodeToString([]byte("kratko")) }), service.PotpisNeVrijedi},
		{"potpis nije base64", potpisan(func(a *models.Akt) { a.Potpis = "nije potpis!" }), service.PotpisNeVrijedi},
		{"ključ drugog čvora", potpisan(func(a *models.Akt) {
			a.KljucCvora = razmjena.PublicKeyString(drugi.Public().(ed25519.PublicKey))
		}), service.PotpisNeVrijedi},
	} {
		if got := service.ProvjeriPotpis(x.akt); got != x.zeli {
			t.Errorf("%s: %s, želi %s", x.opis, got, x.zeli)
		}
	}
}

// Sken vlastoručnog potpisa sprema samo prijavljena osoba, šifriran ključem
// čvora: bez ključa se ne sprema, a čvor s drugim ključem ga ne otključava.
// Sken je PNG ili JPEG od barem 120×40 točaka i najviše 1 MB. Obrisan sken
// se više ne nalazi, a kad se ne da pročitati, akt ide bez njega.
func TestSkenPotpisaSifriranKljucemCvora(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	id := o.rukovod.ID.String()
	slika := slikaPNG(t, 300, 100)

	if err := o.akti.SpremiPotpisSliku(ctx, nil, slika); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("bez osobe: %v", err)
	}
	if err := o.akti.SpremiPotpisSliku(ctx, o.rukovod, slika); err == nil ||
		err.Error() != "ključ čvora nije učitan; sken se ne može sigurno spremiti" {
		t.Fatalf("bez ključa čvora: %v", err)
	}
	if p := o.akti.PotpisSlika(ctx, id); p != nil {
		t.Fatalf("sken spremljen bez ključa: %+v", p)
	}

	o.akti.SetKljuc(kljucCvora(1))
	for _, x := range []struct {
		opis   string
		slika  []byte
		greska string
	}{
		{"bez slike", nil, "odaberite sliku potpisa"},
		{"veća od 1 MB", make([]byte, 1<<20+1), "slika potpisa je prevelika"},
		{"nije slika", []byte("%PDF-1.7"), "potpis mora biti slika PNG ili JPEG"},
		{"preuska", slikaPNG(t, 119, 100), "slika potpisa je premala (119×100)"},
		{"preniska", slikaPNG(t, 300, 39), "slika potpisa je premala (300×39)"},
		{"previše točaka", pngSMjerama(t, 60000, 1000), "slika potpisa ima previše točaka (60000×1000"},
	} {
		if err := o.akti.SpremiPotpisSliku(ctx, o.rukovod, x.slika); err == nil || !strings.Contains(err.Error(), x.greska) {
			t.Errorf("%s: %v, želi %q", x.opis, err, x.greska)
		}
	}
	if p := o.akti.PotpisSlika(ctx, id); p != nil {
		t.Fatalf("odbijen sken je spremljen: %+v", p)
	}

	// JPEG se u PDF stavlja kakav jest, pa i fotografija potpisa s telefona
	// prolazi; PNG do 4 milijuna točaka također
	for _, dobra := range [][]byte{jpegSMjerama(t, 4000, 3000), pngSMjerama(t, 2000, 2000)} {
		if err := o.akti.SpremiPotpisSliku(ctx, o.rukovod, dobra); err != nil {
			t.Errorf("dopušten sken: %v", err)
		}
	}
	if err := o.akti.SpremiPotpisSliku(ctx, o.rukovod, slika); err != nil {
		t.Fatal(err)
	}
	p := o.akti.PotpisSlika(ctx, id)
	if p == nil || p.Mime != "image/png" || !bytes.Equal(p.Slika, slika) {
		t.Fatalf("otključan sken: %+v", p)
	}
	if u, err := repoAkata(o).GetPotpisSlika(ctx, id); err != nil || u == nil || bytes.Contains(u.Slika, slika[:16]) {
		t.Fatalf("sken u bazi nije šifriran: %v", err)
	}

	// isti zapis na čvoru s drugim ključem, ili bez ključa, ne otključava se
	drugiCvor := service.NewAktService(repoAkata(o), nil, nil, nil, nil, nil, nil, "drugi")
	if p := drugiCvor.PotpisSlika(ctx, id); p != nil {
		t.Errorf("čvor bez ključa otključao je sken")
	}
	drugiCvor.SetKljuc(kljucCvora(2))
	if p := drugiCvor.PotpisSlika(ctx, id); p != nil {
		t.Errorf("čvor s drugim ključem otključao je sken")
	}

	if err := o.akti.ObrisiPotpisSliku(ctx, nil); !errors.Is(err, service.ErrUnauthorized) {
		t.Errorf("brisanje bez osobe: %v", err)
	}
	if o.akti.PotpisSlika(ctx, id) == nil {
		t.Fatal("odbijeno brisanje obrisalo je sken")
	}
	if err := o.akti.ObrisiPotpisSliku(ctx, o.rukovod); err != nil {
		t.Fatal(err)
	}
	if p := o.akti.PotpisSlika(ctx, id); p != nil {
		t.Fatalf("sken nije obrisan: %+v", p)
	}

	if err := o.akti.SpremiPotpisSliku(ctx, o.rukovod, slika); err != nil {
		t.Fatal(err)
	}
	pokvariTablicu(t, o, "potpisi_slike")
	if p := o.akti.PotpisSlika(ctx, id); p != nil {
		t.Errorf("nečitljiv sken: %+v", p)
	}
}

// Otisci na PDF-u akta: žig sektora ide na svaki akt, a sken potpisa samo
// na ovjeren akt, i to sken onoga tko ga je ovjerio.
func TestOtisciAkta(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	o.akti.SetKljuc(kljucCvora(1))
	if err := o.akti.SpremiZig(ctx, upravaSektoraP, o.rukovod, "P", slikaPNG(t, 300, 300)); err != nil {
		t.Fatal(err)
	}
	if err := o.akti.SpremiPotpisSliku(ctx, o.rukovod, slikaPNG(t, 300, 100)); err != nil {
		t.Fatal(err)
	}
	ovjeren := ovjerenAkt(t, o, o.rukovod.ID.String())
	nacrt := *ovjeren
	nacrt.Status = models.AktNacrt
	bezOvjeritelja := *ovjeren
	bezOvjeritelja.OvjerioID = ""
	tudji := *ovjeren
	tudji.OvjerioID = o.vodocuv.ID.String()
	for _, x := range []struct {
		opis   string
		akt    *models.Akt
		potpis bool
	}{
		{"nacrt", &nacrt, false},
		{"ovjeren", ovjeren, true},
		{"ovjeren bez ovjeritelja", &bezOvjeritelja, false},
		{"ovjeritelj bez skena", &tudji, false},
	} {
		ot := o.akti.OtisciAkta(ctx, x.akt)
		if ot.Zig == nil || ot.Zig.Sektor != "P" {
			t.Errorf("%s: bez žiga sektora: %+v", x.opis, ot.Zig)
		}
		if (ot.Potpis != nil) != x.potpis {
			t.Errorf("%s: potpis %+v, želi potpis %v", x.opis, ot.Potpis, x.potpis)
		}
		if ot.Potpis != nil && ot.Potpis.UserID != o.rukovod.ID.String() {
			t.Errorf("%s: tuđi potpis %s", x.opis, ot.Potpis.UserID)
		}
	}
}

// Izvornik ovjerenog akta (PDF s otiscima) sprema se jednom: nacrt i prazan
// PDF se ne spremaju, a zatečeni izvornik (npr. sken ručno potpisanog akta)
// ostaje. Greška čitanja izvornika se vraća.
func TestIzvornikOvjerenogAktaSpremaSeJednom(t *testing.T) {
	o := novaOkolinaAkta(t)
	ctx := context.Background()
	ovjeren := ovjerenAkt(t, o, o.rukovod.ID.String())
	nacrt := *ovjeren
	nacrt.Status = models.AktNacrt
	prvi, drugi := []byte("%PDF-1.7 prvi izvornik"), []byte("%PDF-1.7 drugi izvornik")

	if err := o.akti.SpremiIzvornikPDF(ctx, &nacrt, prvi); err != nil {
		t.Fatal(err)
	}
	if err := o.akti.SpremiIzvornikPDF(ctx, ovjeren, nil); err != nil {
		t.Fatal(err)
	}
	if pdf, err := o.akti.Izvornik(ctx, ovjeren.ID); err != nil || pdf != nil {
		t.Fatalf("izvornik nacrta ili praznog PDF-a je spremljen: %q %v", pdf, err)
	}
	if err := o.akti.SpremiIzvornikPDF(ctx, ovjeren, prvi); err != nil {
		t.Fatal(err)
	}
	if err := o.akti.SpremiIzvornikPDF(ctx, ovjeren, drugi); err != nil {
		t.Fatal(err)
	}
	if pdf, err := o.akti.Izvornik(ctx, ovjeren.ID); err != nil || !bytes.Equal(pdf, prvi) {
		t.Fatalf("izvornik: %q %v, želi prvi", pdf, err)
	}

	pokvariTablicu(t, o, "akti_izvornici")
	if err := o.akti.SpremiIzvornikPDF(ctx, ovjeren, drugi); err == nil {
		t.Error("greška čitanja izvornika nije vraćena pri spremanju")
	}
	if _, err := o.akti.Izvornik(ctx, ovjeren.ID); err == nil {
		t.Error("greška čitanja izvornika nije vraćena")
	}
}
