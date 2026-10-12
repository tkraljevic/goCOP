package peers

// Kanali i sadržaj na jednom čvoru: redoslijed kanala na ekranu, koji se
// sadržaji traže od drugog čvora (prema razini pretplate i ogradi po
// razmjeni) i nazivi datoteka za izvoz.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gocop/internal/sadrzaj"
)

// Kanali idu po vrsti, pa po području, a unutar područja najnovija godina
// prva.
func TestRedoslijedKanala(t *testing.T) {
	kanali := []ChannelStat{
		{Channel: "prijave/16/2025", Kind: "prijave", AreaID: 16, Year: 2025},
		{Channel: "ocitanja/16/2024", Kind: "ocitanja", AreaID: 16, Year: 2024},
		{Channel: "prijave/3/2026", Kind: "prijave", AreaID: 3, Year: 2026},
		{Channel: "ocitanja/16/2026", Kind: "ocitanja", AreaID: 16, Year: 2026},
		{Channel: "dnevnici/16/2026", Kind: "dnevnici", AreaID: 16, Year: 2026},
		{Channel: "prijave/16/2026", Kind: "prijave", AreaID: 16, Year: 2026},
	}
	sortChannels(kanali)
	var redom []string
	for _, k := range kanali {
		redom = append(redom, k.Channel)
	}
	ocekivano := []string{"dnevnici/16/2026", "ocitanja/16/2026", "ocitanja/16/2024", "prijave/3/2026", "prijave/16/2026", "prijave/16/2025"}
	if !slices.Equal(redom, ocekivano) {
		t.Errorf("redoslijed %v, očekivano %v", redom, ocekivano)
	}
	sortChannels(nil) // prazan popis ostaje prazan
}

// Čvor traži samo sadržaj koji mu pretplata pokriva na svojoj razini, a
// jednom razmjenom ne više od ograde bajtova (prva stavka ide uvijek).
// Svaki traženi sadržaj bilježi pokušaj, da se sljedeći put krene od drugih.
func TestZeljeniSadrzajPremaPretplatiIOgradi(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if s.zeljeniOtisci(ctxRub, Wants{All: true}) != nil {
		t.Error("bez spremišta čvor traži sadržaj")
	}
	sp, err := sadrzaj.Otvori("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sp.Zatvori() })
	s.SetSpremiste(sp)

	tudji := sadrzaj.Otisak([]byte("izvornik dnevnika iz drugog područja"))
	prvi := sadrzaj.Otisak([]byte("prvi izvornik prijave, Pero Perić"))
	drugi := sadrzaj.Otisak([]byte("drugi izvornik prijave, Ana Anić"))
	velicina := najviseBajtovaPoRazmjeni/2 + 1
	for _, z := range []struct {
		otisak, kanal string
	}{
		{tudji, "dnevnici/3/2026"}, {prvi, "prijave/16/2026"}, {drugi, "prijave/16/2026"},
	} {
		if err := sp.Zeli(ctxRub, z.otisak, "application/pdf", velicina, "test", z.kanal); err != nil {
			t.Fatal(err)
		}
	}
	// tuđi ide prvi (još nije tražen), pa ga čvor mora preskočiti, ne zaustaviti se
	if err := sp.Trazeno(ctxRub, prvi); err != nil {
		t.Fatal(err)
	}
	if err := sp.Trazeno(ctxRub, drugi); err != nil {
		t.Fatal(err)
	}

	w := Wants{Rules: []Subscription{{Kind: "prijave", AreaID: 16, Razina: RazinaSve}}}
	trazeno := s.zeljeniOtisci(ctxRub, w)
	if len(trazeno) != 1 || (trazeno[0] != prvi && trazeno[0] != drugi) {
		t.Fatalf("traženo %v: očekivan jedan izvornik prijave (dva su zajedno preko ograde)", trazeno)
	}
	zelje, err := sp.Zeljeni(ctxRub, 10)
	if err != nil {
		t.Fatal(err)
	}
	if zelje[len(zelje)-1].Otisak != trazeno[0] {
		t.Error("traženi sadržaj nije zabilježen kao zadnji pokušaj")
	}

	// razina pregleda ne traži PDF
	if got := s.zeljeniOtisci(ctxRub, Wants{Rules: []Subscription{{Kind: "prijave", Razina: RazinaPregled}}}); len(got) != 0 {
		t.Errorf("pregled traži PDF: %v", got)
	}

	if err := sp.Zatvori(); err != nil {
		t.Fatal(err)
	}
	if got := s.zeljeniOtisci(ctxRub, Wants{All: true}); got != nil {
		t.Errorf("iz nečitljivog spremišta traženo %v", got)
	}
}

// Hoće li čvor dohvatiti sadržaj: uredski čvor sve, pretplata na razini
// pregleda samo slike, kanal bez pretplate ništa, a kad se pretplate ne
// mogu pročitati, ništa.
func TestZeliSadrzajPoPretplati(t *testing.T) {
	s := rubniCvor(t, "laptop-baranja")
	if _, err := s.AddSubscription(ctxRub, Subscription{Kind: "prijave", AreaID: 16, Razina: RazinaPregled}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		kanal, vrsta string
		zeli         bool
	}{
		{"prijave/16/2026", "image/jpeg", true},
		{"prijave/16/2026", "application/pdf", false},
		{"prijave/3/2026", "image/jpeg", false},
	} {
		if got := s.ZeliSadrzaj(ctxRub, p.kanal, p.vrsta); got != p.zeli {
			t.Errorf("%s %s: %v, očekivano %v", p.kanal, p.vrsta, got, p.zeli)
		}
	}
	s.SetWantsAll(true)
	if !s.ZeliSadrzaj(ctxRub, "prijave/3/2026", "application/pdf") {
		t.Error("uredski čvor ne želi sve")
	}
	s.SetWantsAll(false)
	pokvariTablicu(t, s, "subscriptions")
	if s.ZeliSadrzaj(ctxRub, "prijave/16/2026", "image/jpeg") {
		t.Error("bez čitljivih pretplata čvor želi sadržaj")
	}
}

// Naziv datoteke za izvoz kaže vrstu, područje i godinu; bez vrste je "sve".
// Privremena datoteka je u privremenoj mapi i uzima samo ime, ne putanju.
func TestNaziviDatotekaArhive(t *testing.T) {
	if n := ArchiveFileName("ocitanja", 16, 2024); n != "gocop-ocitanja-bp16-2024.db" {
		t.Errorf("s vrstom: %s", n)
	}
	if n := ArchiveFileName("", 3, 2026); n != "gocop-sve-bp3-2026.db" {
		t.Errorf("bez vrste: %s", n)
	}
	put := (&Service{}).TempArchivePath("../../izvan/gocop-sve-bp3-2026.db")
	if filepath.Dir(put) != filepath.Clean(os.TempDir()) || !strings.HasSuffix(put, "-gocop-sve-bp3-2026.db") {
		t.Errorf("privremeni put %s", put)
	}
}
