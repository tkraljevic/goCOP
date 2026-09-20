package sadrzaj

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

// Sadržaj se sprema jednom bez obzira koliko ga zapisa navodi, čita se
// natrag isti, a krivi otisak ne ulazi.
func TestUpisPoOtisku(t *testing.T) {
	s, err := Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Zatvori()
	ctx := context.Background()
	pdf := []byte("%PDF-1.4\nprobni izvornik")
	o1, err := s.Upisi(ctx, "application/pdf", pdf, "", Veza{"prijave", "p1", "izvornik"})
	if err != nil {
		t.Fatal(err)
	}
	o2, err := s.Upisi(ctx, "application/pdf", pdf, "", Veza{"vodocuvarski_listovi", "l1", "izvornik"})
	if err != nil || o1 != o2 || o1 != Otisak(pdf) {
		t.Fatalf("drugi upis: %v %s %s", err, o1, o2)
	}
	st, _ := s.Stanje(ctx)
	if st.Sadrzaja != 1 || st.Bajtova != int64(len(pdf)) || st.Sirocadi != 0 {
		t.Errorf("stanje: %+v", st)
	}
	b, vrsta, err := s.Citaj(ctx, o1)
	if err != nil || !bytes.Equal(b, pdf) || vrsta != "application/pdf" {
		t.Errorf("čitanje: %v %q", err, vrsta)
	}
	if _, _, err := s.Citaj(ctx, "0000"); err != ErrNema {
		t.Errorf("nepostojeći: %v", err)
	}
	if err := s.UpisiProvjereno(ctx, o1, "application/pdf", []byte("drugo"), "cvor:x"); err != ErrOtisak {
		t.Errorf("krivi otisak prošao: %v", err)
	}
	if _, err := s.Upisi(ctx, "image/jpeg", nil, ""); err == nil {
		t.Error("prazan sadržaj prošao")
	}
}

// Sadržaj koji neki zapis drži ne može se ukloniti; kad ga nitko ne drži,
// siroče je i smije otići. Želja nestaje čim sadržaj stigne.
func TestVezeSirocadIZelje(t *testing.T) {
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Zatvori()
	ctx := context.Background()
	b := []byte("fotografija")
	o, _ := s.Upisi(ctx, "image/jpeg", b, "", Veza{"prijave", "p1", "slika"})
	if err := s.Ukloni(ctx, o); err == nil {
		t.Error("vezani sadržaj je uklonjen")
	}
	if err := s.Odvezi(ctx, "prijave", "p1"); err != nil {
		t.Fatal(err)
	}
	sir, _ := s.Sirocad(ctx)
	if len(sir) != 1 || sir[0] != o {
		t.Errorf("siročad: %v", sir)
	}
	if err := s.Ukloni(ctx, o); err != nil {
		t.Errorf("uklanjanje siročeta: %v", err)
	}
	if s.Ima(ctx, o) {
		t.Error("još ima")
	}
	// želja za sadržajem koji nemamo, pa stigne s drugog čvora
	if err := s.Zeli(ctx, o, "image/jpeg", len(b), "pretplata"); err != nil {
		t.Fatal(err)
	}
	z, _ := s.Zeljeni(ctx, 10)
	if len(z) != 1 || z[0].Otisak != o || z[0].Razlog != "pretplata" {
		t.Fatalf("željeni: %+v", z)
	}
	if err := s.UpisiProvjereno(ctx, o, "image/jpeg", b, "cvor:osijek", Veza{"prijave", "p2", "slika"}); err != nil {
		t.Fatal(err)
	}
	if z, _ := s.Zeljeni(ctx, 10); len(z) != 0 {
		t.Errorf("želja nije nestala: %+v", z)
	}
	if err := s.Zeli(ctx, o, "image/jpeg", len(b), "pretplata"); err != nil {
		t.Fatal(err)
	}
	if z, _ := s.Zeljeni(ctx, 10); len(z) != 0 {
		t.Error("želja za sadržajem koji imamo")
	}
}

// Dijelovi su određeni sadržajem: isti bajtovi daju iste dijelove, zadnji
// je kraći, a otisci dijelova se razlikuju od otiska cjeline.
func TestDijelovi(t *testing.T) {
	b := bytes.Repeat([]byte("x"), VelicinaDijela*2+5)
	d := Dijelovi(b)
	if len(d) != 3 || d[0].Bajtova != VelicinaDijela || d[2].Bajtova != 5 || d[2].Redni != 2 {
		t.Fatalf("dijelovi: %+v", d)
	}
	if d[0].Otisak != d[1].Otisak {
		t.Error("isti dijelovi, različit otisak")
	}
	if d2 := Dijelovi(b); d2[2].Otisak != d[2].Otisak {
		t.Error("dijelovi nisu određeni sadržajem")
	}
	if Dijelovi(nil) != nil {
		t.Error("prazan sadržaj ima dijelove")
	}
}
