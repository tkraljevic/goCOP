package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// Kiša po međuslivovima uz izvoz ranijeg izdanja je ona koju je to izdanje
// imalo, s njegovim satom za dnevnu prognozu; bez ranijeg izdanja izdavač
// uzima živu kišu, a primatelj onu iz zadnjeg izdanja.
func TestKisaZaStanjeRanijegIzdanja(t *testing.T) {
	pb, err := prognoza.Otvori(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	const staro, novo = int64(497500), int64(497600)
	for sat, mm := range map[int64]float64{staro: 4, novo: 9} {
		if err := prognoza.SpremiIzdane(pb, []prognoza.Izdana{{Letva: "gornja", Velicina: "vodostaj", Izdano: sat, Ciljni: sat + 1,
			Vrijednost: 100, Dolje: 99, Gore: 101, Model: prognoza.ModelLanac}}); err != nil {
			t.Fatal(err)
		}
		if _, err := prognoza.SpremiZapisIzdanja(pb, prognoza.ZapisIzdanja{Izdano: sat,
			Kisa: map[string]prognoza.DnevniNiz{prognoza.OborinaKljuc("A"): {0: mm}}}); err != nil {
			t.Fatal(err)
		}
	}
	ziva := func() (map[string]prognoza.DnevniNiz, error) {
		return map[string]prognoza.DnevniNiz{prognoza.OborinaKljuc("A"): {0: 30}}, nil
	}
	ranije := func(sat int64) context.Context {
		iz := time.Unix(sat*3600, 0)
		return web.UzRanijeIzdanje(context.Background(), web.RanijeIzdanje{Trazeno: iz, Izdano: iz})
	}
	kljuc := prognoza.OborinaKljuc("A")
	for _, izdaje := range []bool{true, false} {
		kisa, sat, err := kisaZaStanje(ranije(staro), pb, ziva, izdaje)
		if err != nil || sat != staro || kisa[kljuc][0] != 4 {
			t.Errorf("ranije izdanje (izdaje %v): %v %d %v", izdaje, kisa, sat, err)
		}
	}
	if kisa, sat, err := kisaZaStanje(context.Background(), pb, ziva, true); err != nil || sat != novo || kisa[kljuc][0] != 30 {
		t.Errorf("izdavač: %v %d %v", kisa, sat, err)
	}
	if kisa, sat, err := kisaZaStanje(context.Background(), pb, ziva, false); err != nil || sat != novo || kisa[kljuc][0] != 9 {
		t.Errorf("primatelj: %v %d %v", kisa, sat, err)
	}
	// Izdanje bez zapisane kiše ne smije reći da kiše nije bilo.
	if _, _, err := kisaZaStanje(ranije(staro-1), pb, ziva, true); err == nil {
		t.Error("izdanje bez zapisane kiše bez greške")
	}
}
