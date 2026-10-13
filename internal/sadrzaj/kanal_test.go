package sadrzaj

import (
	"context"
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
	if n, b, err := s.ObrisiKanal(ctx, ""); n != 0 || b != 0 || err != nil {
		t.Errorf("zajednički kanal se ne briše: %d %d %v", n, b, err)
	}
	if n, b, err := s.ObrisiKanal(ctx, "vodocuvarski/16/2026"); n != 1 || b != int64(len(dosla)) || err != nil {
		t.Fatalf("obrisano %d (%d B), %v", n, b, err)
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
	if _, _, err := s.ObrisiKanal(ctx, "vodocuvarski/16/2026"); err == nil {
		t.Error("zatvoreno spremište briše kanal bez greške")
	}
}
