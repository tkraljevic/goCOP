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

// uredSPaketima: ured drži pakete arhive po kanalu i prati sve, laptop je
// uparen s njim i ne prati ništa dok mu test ne doda pravilo
type uredSPaketima struct {
	ured, laptop *node
	spLaptop     *sadrzaj.Spremiste
	paketi       map[string][]byte // kanal → bajtovi paketa
	otisci       map[string]string // kanal → otisak paketa
}

func noviUredSPaketima(t *testing.T, ctx context.Context, paketi map[string][]byte) *uredSPaketima {
	t.Helper()
	u := &uredSPaketima{paketi: paketi, otisci: map[string]string{}}
	u.ured = startNodeBezRegistra(t, ctx, "cop-osijek")
	u.laptop = startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, u.ured, "Hrvatske vode")
	pair(t, ctx, u.ured, u.laptop)

	spUred, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "ured.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spUred.Zatvori() })
	u.spLaptop, err = sadrzaj.Otvori(filepath.Join(t.TempDir(), "laptop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = u.spLaptop.Zatvori() })
	u.ured.svc.SetSpremiste(spUred)
	u.laptop.svc.SetSpremiste(u.spLaptop)
	u.ured.svc.SetWantsAll(true)
	u.laptop.svc.SetWantsAll(false)

	for kanal, b := range paketi {
		o, err := spUred.Upisi(ctx, vrstaPaketaArhive, b, "ovdje", sadrzaj.Veza{Entitet: "arhiva_paket", EntitetID: kanal, Uloga: "paket", Kanal: kanal})
		if err != nil {
			t.Fatal(err)
		}
		u.otisci[kanal] = o
	}
	return u
}

// razmijeni upiše sve pakete kao želje laptopa, kao da ih je zatražio, pa
// laptop razmijeni s uredom: razmjena svejedno ograđuje po pretplati, na
// obje strane
func (u *uredSPaketima) razmijeni(t *testing.T, ctx context.Context) {
	t.Helper()
	for kanal, o := range u.otisci {
		if err := u.spLaptop.Zeli(ctx, o, vrstaPaketaArhive, len(u.paketi[kanal]), "arhiva", kanal); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := u.laptop.svc.SyncWith(ctx, u.ured.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
}

// stigao javlja je li paket kanala stigao laptopu cijel
func (u *uredSPaketima) stigao(ctx context.Context, kanal string) bool {
	got, _, err := u.spLaptop.Citaj(ctx, u.otisci[kanal])
	return err == nil && string(got) == string(u.paketi[kanal])
}

// Laptop prati sve vrste za BP 16 u dvije godine. Paket BP 16 stiže, paket
// BP 3 ne. Brisanje onoga što pretplata ne pokriva odnese stara očitanja, a
// paket ostaje.
func TestPaketArhiveStizeUzPraviloSveVrste(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	u := noviUredSPaketima(t, ctx, map[string][]byte{
		"arhiva/16/0": []byte("izmišljeni paket letve Primjerovo, cijela povijest"),
		"arhiva/3/0":  []byte("izmišljeni paket letve Drugovo, cijela povijest"),
	})
	laptop := u.laptop
	if _, err := laptop.svc.AddSubscription(ctx, peers.Subscription{AreaID: 16, YearFrom: 2025, YearTo: 2026}); err != nil {
		t.Fatal(err)
	}
	if !laptop.svc.ZeliSadrzaj(ctx, "arhiva/16/0", vrstaPaketaArhive) {
		t.Fatal("laptop ne bi zatražio paket arhive svog područja")
	}
	if laptop.svc.ZeliSadrzaj(ctx, "arhiva/3/0", vrstaPaketaArhive) {
		t.Fatal("laptop bi zatražio paket arhive tuđeg područja")
	}
	u.razmijeni(t, ctx)
	if !u.stigao(ctx, "arhiva/16/0") {
		t.Fatal("paket arhive BP 16 nije stigao")
	}
	if u.spLaptop.Ima(ctx, u.otisci["arhiva/3/0"]) {
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
	if !u.spLaptop.Ima(ctx, u.otisci["arhiva/16/0"]) {
		t.Error("brisanje nepokrivenog odnijelo je paket arhive BP 16")
	}
}

// Pravilo „sve vrste” za sva područja s godinom početka donosi cijelu
// arhivu: pakete svih područja te strane i meteorološke letve (područje 0).
// Razina „Samo pregledi” paket ne zaustavlja, jer su to podaci, a ne slike
// (Odluke (13. 10. 2026.)). Do 0.0.38 takvo pravilo nije donosilo nijedan
// paket.
func TestCijelaArhivaUzSveVrsteZaSvaPodrucja(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	u := noviUredSPaketima(t, ctx, map[string][]byte{
		"arhiva/16/0": []byte("izmišljeni paket letve Primjerovo"),
		"arhiva/3/0":  []byte("izmišljeni paket letve Drugovo"),
		"arhiva/0/0":  []byte("izmišljeni paket strane letve Prekogranično"),
	})
	if _, err := u.laptop.svc.AddSubscription(ctx, peers.Subscription{YearFrom: 2025, Razina: peers.RazinaPregled}); err != nil {
		t.Fatal(err)
	}
	u.razmijeni(t, ctx)
	for kanal := range u.paketi {
		if !u.stigao(ctx, kanal) {
			t.Errorf("paket %s nije stigao", kanal)
		}
	}
}
