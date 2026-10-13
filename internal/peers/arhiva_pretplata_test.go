package peers_test

// Paket arhive vodostaja putuje kao sadržaj u kanalu arhiva/područje/0, bez
// godine. Laptop s pravilom „sve vrste, BP 16, 2025.–2026.” do 0.0.38 nikad
// ga nije dobio, jer su godine pravila odbijale godinu 0 (Odluke
// (13. 10. 2026.)).

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/peers"
	"gocop/internal/sadrzaj"
)

const vrstaPaketaArhive = "application/vnd.gocop.arhiva" // kao cmd/gocop VrstaPaketa

// Dva čvora: ured drži pakete arhive dvaju područja, laptop prati sve vrste
// za BP 16 u dvije godine. Paket BP 16 stiže, paket BP 3 ne. Brisanje
// onoga što pretplata ne pokriva odnese stara očitanja, a paket ostaje.
func TestPaketArhiveStizeUzPraviloSveVrste(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ured := startNodeBezRegistra(t, ctx, "cop-osijek")
	laptop := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, ured, "Hrvatske vode")
	pair(t, ctx, ured, laptop)

	spUred, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "ured.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer spUred.Zatvori()
	spLaptop, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "laptop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer spLaptop.Zatvori()
	ured.svc.SetSpremiste(spUred)
	laptop.svc.SetSpremiste(spLaptop)
	ured.svc.SetWantsAll(true)

	pakete := map[string][]byte{
		"arhiva/16/0": []byte("izmišljeni paket letve Primjerovo, cijela povijest"),
		"arhiva/3/0":  []byte("izmišljeni paket letve Drugovo, cijela povijest"),
	}
	otisci := map[string]string{}
	for kanal, b := range pakete {
		o, err := spUred.Upisi(ctx, vrstaPaketaArhive, b, "ovdje", sadrzaj.Veza{Entitet: "arhiva_paket", EntitetID: kanal, Uloga: "paket", Kanal: kanal})
		if err != nil {
			t.Fatal(err)
		}
		otisci[kanal] = o
	}

	laptop.svc.SetWantsAll(false)
	if _, err := laptop.svc.AddSubscription(ctx, peers.Subscription{AreaID: 16, YearFrom: 2025, YearTo: 2026}); err != nil {
		t.Fatal(err)
	}
	if !laptop.svc.ZeliSadrzaj(ctx, "arhiva/16/0", vrstaPaketaArhive) {
		t.Fatal("laptop ne bi zatražio paket arhive svog područja")
	}
	if laptop.svc.ZeliSadrzaj(ctx, "arhiva/3/0", vrstaPaketaArhive) {
		t.Fatal("laptop bi zatražio paket arhive tuđeg područja")
	}
	// obje želje upisane kao da ih je zatražio: razmjena svejedno ograđuje
	// po pretplati, na obje strane
	for kanal, o := range otisci {
		if err := spLaptop.Zeli(ctx, o, vrstaPaketaArhive, len(pakete[kanal]), "arhiva", kanal); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := laptop.svc.SyncWith(ctx, ured.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
	if got, _, err := spLaptop.Citaj(ctx, otisci["arhiva/16/0"]); err != nil || string(got) != string(pakete["arhiva/16/0"]) {
		t.Fatalf("paket arhive BP 16 nije stigao: %v", err)
	}
	if spLaptop.Ima(ctx, otisci["arhiva/3/0"]) {
		t.Error("stigao je paket arhive BP 3, koji pretplata ne pokriva")
	}

	for _, k := range []string{"ocitanja/16/2026", "ocitanja/16/2024"} {
		if _, err := laptop.rec.RecordIn(ctx, laptop.db, k, "probni", "r-"+k, map[string]int{"cm": 120}); err != nil {
			t.Fatal(err)
		}
	}
	obrisano, err := laptop.svc.PurgeUnwanted(ctx)
	if err != nil || len(obrisano) != 1 || obrisano["ocitanja/16/2024"] != 1 {
		t.Fatalf("brisanje nepokrivenog: %v %v", obrisano, err)
	}
	if c, _ := laptop.rec.CountByChannel(ctx); c["ocitanja/16/2026"] != 1 {
		t.Errorf("obrisana su očitanja koja pretplata pokriva: %v", c)
	}
	if !spLaptop.Ima(ctx, otisci["arhiva/16/0"]) {
		t.Error("brisanje nepokrivenog odnijelo je paket arhive BP 16")
	}
}
