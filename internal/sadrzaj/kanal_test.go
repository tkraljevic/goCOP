package sadrzaj

import (
	"context"
	"errors"
	"testing"
)

// Sadržaj bez kanala dobiva kanal svog zapisa, a kanal koji već ima
// zadržava. Veza se prepoznaje po zapisu i ulozi.
func TestKanalIVezaSadrzaja(t *testing.T) {
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	veza := Veza{"vodocuvarski_listovi", "l1", "prilog:p1", ""}
	o, err := s.Upisi(ctx, "image/jpeg", []byte("fotografija nasipa kod ustave"), "", veza)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		otisak, kanal string
		upisan        bool
	}{
		{o, "", false},
		{o, "vodocuvarski/16/2026", true},
		{o, "vodocuvarski/17/2026", false},
		{Otisak([]byte("nema je")), "vodocuvarski/16/2026", false},
	} {
		if upisan, err := s.PostaviKanal(ctx, p.otisak, p.kanal); upisan != p.upisan || err != nil {
			t.Errorf("kanal %q: upisan %v (%v), očekivano %v", p.kanal, upisan, err, p.upisan)
		}
	}
	if _, _, kanal, _ := s.CitajSKanalom(ctx, o); kanal != "vodocuvarski/16/2026" {
		t.Errorf("kanal sadržaja %q", kanal)
	}
	if !s.ImaVezu(ctx, o, veza) {
		t.Error("veza nije prepoznata")
	}
	for _, druga := range []Veza{{"vodocuvarski_listovi", "l2", "prilog:p1", ""}, {"vodocuvarski_listovi", "l1", "prilog:p2", ""}, {"prijave", "l1", "prilog:p1", ""}} {
		if s.ImaVezu(ctx, o, druga) {
			t.Errorf("prepoznata tuđa veza %+v", druga)
		}
	}
	if err := s.Zatvori(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PostaviKanal(ctx, o, "vodocuvarski/16/2026"); err == nil {
		t.Error("zatvoreno spremište upisuje kanal bez greške")
	}
	if s.ImaVezu(ctx, o, veza) {
		t.Error("zatvoreno spremište javlja vezu")
	}
}

// Brisanje kanala miče sadržaj i želje tog kanala, a sadržaj drugog kanala
// ostaje. Popis za dohvat javlja što je na njemu.
func TestBrisanjeKanalaIPopisZaDohvat(t *testing.T) {
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Zatvori() }()
	ctx := context.Background()
	trazena := Otisak([]byte("fotografija koja nije stigla"))
	if err := s.Zeli(ctx, trazena, "image/jpeg", 10, "pretplata", "vodocuvarski/16/2026"); err != nil {
		t.Fatal(err)
	}
	if !s.Zeljen(ctx, trazena) || s.Zeljen(ctx, Otisak([]byte("nije na popisu"))) {
		t.Error("popis za dohvat krivo javlja što je na njemu")
	}
	drugi := Veza{"vodocuvarski_listovi", "l2", "prilog:p2", "vodocuvarski/17/2026"}
	stigla, err := s.Upisi(ctx, "image/jpeg", []byte("fotografija iz drugog područja"), "cvor:ured", drugi)
	if err != nil {
		t.Fatal(err)
	}
	dosla := []byte("fotografija koja je stigla")
	if _, err := s.Upisi(ctx, "image/jpeg", dosla, "cvor:laptop", Veza{"vodocuvarski_listovi", "l1", "prilog:p3", "vodocuvarski/16/2026"}); err != nil {
		t.Fatal(err)
	}
	if b, err := s.ObrisiKanal(ctx, "", nil); b != (BrisanjeKanala{}) || err != nil {
		t.Errorf("zajednički kanal se ne briše: %+v %v", b, err)
	}
	if b, err := s.ObrisiKanal(ctx, "vodocuvarski/16/2026", nil); b != (BrisanjeKanala{Obrisano: 1, Bajtova: int64(len(dosla))}) || err != nil {
		t.Fatalf("obrisano %+v, %v", b, err)
	}
	if s.Ima(ctx, Otisak(dosla)) {
		t.Error("sadržaj obrisanog kanala je ostao")
	}
	if s.Zeljen(ctx, trazena) {
		t.Error("želja obrisanog kanala je ostala")
	}
	if !s.ImaVezu(ctx, stigla, drugi) || !s.Ima(ctx, stigla) {
		t.Error("brisanje kanala dira drugi kanal")
	}
	if err := s.Zatvori(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ObrisiKanal(ctx, "vodocuvarski/16/2026", nil); err == nil {
		t.Error("zatvoreno spremište briše kanal bez greške")
	}
}

// Brisanje kanala ne dira sadržaj nastao na ovom računalu ni njegove veze,
// jer ga možda još nijedno drugo računalo nema (Odluke (13. 10. 2026.)).
// Pregled kaže isto što brisanje učini, a sam ništa ne briše.
func TestBrisanjeKanalaOstavljaSadrzajNastaoOvdje(t *testing.T) {
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Zatvori() })
	ctx := context.Background()
	kanal := "vodocuvarski/16/2026"
	nasa := Veza{"vodocuvarski_listovi", "l-ivo", "prilog:p1", kanal}
	ovdje, err := s.Upisi(ctx, "image/jpeg", []byte("fotografija Ive Ivića s obilaska"), "", nasa)
	if err != nil {
		t.Fatal(err)
	}
	tudja := []byte("%PDF ovjera ureda")
	stigla, err := s.Upisi(ctx, "application/pdf", tudja, "cvor:ured", Veza{"vodocuvarski_izvornici", "l-pero", "izvornik", kanal})
	if err != nil {
		t.Fatal(err)
	}

	pregled, err := s.PregledKanala(ctx, kanal, nil)
	if want := (BrisanjeKanala{Obrisano: 1, Bajtova: int64(len(tudja)), Ostaje: 1}); pregled != want || err != nil {
		t.Fatalf("pregled %+v, %v", pregled, err)
	}
	if !s.Ima(ctx, stigla) {
		t.Fatal("pregled je obrisao sadržaj")
	}
	obrisano, err := s.ObrisiKanal(ctx, kanal, nil)
	if obrisano != pregled || err != nil {
		t.Fatalf("obrisano %+v, pregled %+v, %v", obrisano, pregled, err)
	}
	if s.Ima(ctx, stigla) || !s.Ima(ctx, ovdje) || !s.ImaVezu(ctx, ovdje, nasa) {
		t.Errorf("nakon brisanja: tuđi %v, naš %v, veza %v", s.Ima(ctx, stigla), s.Ima(ctx, ovdje), s.ImaVezu(ctx, ovdje, nasa))
	}
	if b, err := s.PregledKanala(ctx, kanal, nil); b != (BrisanjeKanala{Ostaje: 1}) || err != nil {
		t.Errorf("pregled nakon brisanja: %+v %v", b, err)
	}
}

// Kad se popis kanala ne može pročitati ili brisanje padne, ništa se ne
// briše i javlja se greška.
func TestBrisanjeKanalaKadSpremisteNeRadi(t *testing.T) {
	ctx := context.Background()
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Zatvori() })
	kanal := "prijave/16/2026"
	stigla, err := s.Upisi(ctx, "image/jpeg", []byte("fotografija prijave"), "cvor:ured", Veza{"prijave", "p-1", "slika:1", kanal})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Zeli(ctx, Otisak([]byte("fotografija koja nije stigla")), "image/jpeg", 9, "pretplata", kanal); err != nil {
		t.Fatal(err)
	}
	for _, tablica := range []string{"sadrzaj_veze", "sadrzaj", "sadrzaj_zeljen"} {
		if _, err := s.db.Exec(`CREATE TRIGGER kvar BEFORE DELETE ON ` + tablica + ` BEGIN SELECT RAISE(ABORT, 'namjerni kvar'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ObrisiKanal(ctx, kanal, nil); err == nil || !s.Ima(ctx, stigla) {
			t.Errorf("%s: brisanje koje padne: %v, sadržaj ostao %v", tablica, err, s.Ima(ctx, stigla))
		}
		if _, err := s.db.Exec(`DROP TRIGGER kvar`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE sadrzaj DROP COLUMN izvor`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PregledKanala(ctx, kanal, nil); err == nil {
		t.Error("pregled bez čitljivog spremišta")
	}
}

// Fotografija s ureda na listu koji ostaje na računalu ne briše se s
// kanalom, a PDF ureda koji nijednom zapisu koji ostaje ne treba briše se.
// Pregled kaže isto, a greška provjere zapisa javlja se i ništa ne briše.
func TestBrisanjeKanalaOstavljaSadrzajKojiTrebaZapisu(t *testing.T) {
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Zatvori() })
	ctx := context.Background()
	kanal := "vodocuvarski/16/2026"
	fotografija, err := s.Upisi(ctx, "image/jpeg", []byte("fotografija Pere Perića s obilaska"), "cvor:ured", Veza{"vodocuvarski_listovi", "l-pero", "prilog:p1", kanal})
	if err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF ovjera ureda")
	ovjera, err := s.Upisi(ctx, "application/pdf", pdf, "cvor:ured", Veza{"vodocuvarski_izvornici", "l-pero", "izvornik", kanal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upisi(ctx, "application/pdf", []byte("%PDF bez zapisa"), "cvor:ured", Veza{Kanal: kanal}); err != nil {
		t.Fatal(err)
	}
	var pitano []string
	treba := func(_ context.Context, otisak, entitet, id string) (bool, error) {
		pitano = append(pitano, entitet+"/"+id)
		return otisak == fotografija && entitet == "vodocuvarski_listovi" && id == "l-pero", nil
	}

	pregled, err := s.PregledKanala(ctx, kanal, treba)
	if want := (BrisanjeKanala{Obrisano: 2, Bajtova: int64(len(pdf) + len("%PDF bez zapisa")), Ostaje: 1}); pregled != want || err != nil {
		t.Fatalf("pregled %+v, %v", pregled, err)
	}
	if obrisano, err := s.ObrisiKanal(ctx, kanal, treba); obrisano != pregled || err != nil {
		t.Fatalf("obrisano %+v, pregled %+v, %v", obrisano, pregled, err)
	}
	if !s.Ima(ctx, fotografija) || s.Ima(ctx, ovjera) {
		t.Errorf("nakon brisanja: fotografija %v, ovjera %v", s.Ima(ctx, fotografija), s.Ima(ctx, ovjera))
	}
	for _, p := range pitano {
		if p == "/" {
			t.Error("pitano je za vezu bez zapisa")
		}
	}

	kvar := func(context.Context, string, string, string) (bool, error) { return false, errors.New("namjerni kvar") }
	if _, err := s.ObrisiKanal(ctx, kanal, kvar); err == nil || !s.Ima(ctx, fotografija) {
		t.Errorf("provjera zapisa koja padne: %v, fotografija ostala %v", err, s.Ima(ctx, fotografija))
	}
	if _, err := s.db.Exec(`ALTER TABLE sadrzaj_veze RENAME COLUMN entitet_id TO nema`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PregledKanala(ctx, kanal, treba); err == nil {
		t.Error("pregled bez čitljivih veza")
	}
}

// Kanali s tuđim sadržajem su kanali u kojima je ostao sadržaj s drugih
// čvorova; sadržaj nastao ovdje i sadržaj bez kanala ne broje se
func TestKanaliTudjegSadrzaja(t *testing.T) {
	s, err := Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Zatvori() })
	ctx := context.Background()
	for _, u := range []struct{ podaci, izvor, kanal string }{
		{"s ureda 2026.", "cvor:ured", "prijave/16/2026"},
		{"još s ureda 2026.", "cvor:ured", "prijave/16/2026"},
		{"nastalo ovdje", "", "prijave/16/2025"},
		{"bez kanala", "cvor:ured", ""},
	} {
		if _, err := s.Upisi(ctx, "image/jpeg", []byte(u.podaci), u.izvor, Veza{Kanal: u.kanal}); err != nil {
			t.Fatal(err)
		}
	}
	if k, err := s.KanaliTudjegSadrzaja(ctx); err != nil || len(k) != 1 || k[0] != "prijave/16/2026" {
		t.Errorf("kanali %v, %v", k, err)
	}
	if _, err := s.db.Exec(`ALTER TABLE sadrzaj DROP COLUMN izvor`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.KanaliTudjegSadrzaja(ctx); err == nil {
		t.Error("kanali bez čitljivog spremišta")
	}
}
