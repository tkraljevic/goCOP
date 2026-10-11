package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/poslovi"
	"gocop/internal/prognoza"
	"gocop/internal/web"
)

// Odluke (8. 10. 2026.), pitanje 15: priprema modela baca naučene dnevne
// modele, pa ih idući krug uči iznova iz arhive, a ne tek iza ponoći — i kad
// namještanje lanca padne. Čvor koji prognozu ne izdaje ne priprema ništa.
func TestPripremaModelaUciDnevneIznova(t *testing.T) {
	stari := zaboraviDnevneModele
	t.Cleanup(func() { zaboraviDnevneModele = stari })
	zaboravljeno := 0
	zaboraviDnevneModele = func() { zaboravljeno++ }

	for _, izdaje := range []bool{false, true} {
		zaboravljeno = 0
		k, uvoznik := okolinaKruga(t, izdaje)
		priprema := pripremaModela(k.mreza, k.osvjezivac.Arhiva, k.prognoze, uvoznik, k.osvjezivac,
			func(context.Context, *prognoza.Ishod) { t.Error("objava bez namještenog modela") })
		// Arhiva je prazna, pa namještanje pada; dnevni modeli su već bačeni.
		err := priprema(context.Background(), &poslovi.Posao{})
		if err == nil {
			t.Fatalf("izdaje=%v: priprema iz prazne arhive prošla", izdaje)
		}
		want := 0
		if izdaje {
			want = 1
		} else if !strings.Contains(err.Error(), "čvor koji izdaje") {
			t.Errorf("čvor koji ne izdaje: %v", err)
		}
		if zaboravljeno != want {
			t.Errorf("izdaje=%v: dnevni modeli bačeni %d puta, želim %d", izdaje, zaboravljeno, want)
		}
	}
}

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
