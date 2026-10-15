package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gocop/internal/models"
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
		} else if !strings.Contains(err.Error(), "model nije pripremljen: ovaj čvor ne izdaje prognozu") {
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

// lazniModel zamijeni namještanje lanca i provjeru unatrag: namještanje
// spremi dva pojasa, a provjera zapiše pet promašaja ili vrati zadanu
// grešku. Obje provjere da pišu u dnevnik posla p, a provjera i da zapisuje.
func lazniModel(t *testing.T, p *poslovi.Posao, greskaProvjere error) {
	t.Helper()
	staroN, staroP, staroZ := namjestiLanac, provjeriUnatrag, zaboraviDnevneModele
	t.Cleanup(func() { namjestiLanac, provjeriUnatrag, zaboraviDnevneModele = staroN, staroP, staroZ })
	zaboraviDnevneModele = func() {}
	namjestiLanac = func(_, _ *sql.DB, o prognoza.OpcijeNamjestanja) (int, error) {
		if o.Dnevnik != io.Writer(p) || o.Korak == nil || o.Probno {
			t.Errorf("namještanje: %+v", o)
		}
		return 2, nil
	}
	provjeriUnatrag = func(_, _ *sql.DB, o prognoza.OpcijeProvjere) (int, error) {
		if o.Dnevnik != io.Writer(p) || !o.Zapisi {
			t.Errorf("provjera: dnevnik %v, zapisi %v", o.Dnevnik, o.Zapisi)
		}
		return 5, greskaProvjere
	}
}

// pripremi pokrene pripremu modela na krugu k i vrati završnu poruku i grešku
func pripremi(k *satniKrug, p *poslovi.Posao) (string, error) {
	priprema := pripremaModela(k.mreza, k.osvjezivac.Arhiva, k.prognoze, k.uvoznik, k.osvjezivac, k.objavi)
	err := priprema(context.Background(), p)
	return p.Stanje().Sazetak, err
}

// Priprema modela izda prognozu novim modelom odmah i to kaže u završnoj
// poruci, s riječima koje se slažu s brojem pojasa i promašaja.
func TestPripremaModelaIzdajeNovimModelom(t *testing.T) {
	k, objavljeno := izdavackiKrug(t)
	p := &poslovi.Posao{}
	lazniModel(t, p, nil)
	sazetak, err := pripremi(k, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(*objavljeno) != 1 {
		t.Fatalf("objavljeno %d izdanja, želim 1", len(*objavljeno))
	}
	zelim := "Namještena 2 pojasa, zapisano 5 promašaja; prognoza je izdana novim modelom za " +
		time.Unix((*objavljeno)[0].Sada*3600, 0).In(models.Zagreb).Format("2.1. u 15:04") + "."
	if sazetak != zelim {
		t.Errorf("poruka %q, želim %q", sazetak, zelim)
	}
}

// Svaki korak pripreme koji ne uspije zaustavi je i kaže što je zapelo;
// završne poruke o uspjehu tada nema, a prognoza se ne objavi.
func TestPripremaModelaKojaNijeUspjela(t *testing.T) {
	p := &poslovi.Posao{}
	lazniModel(t, p, errors.New("arhiva je prekratka za provjeru"))
	k, objavljeno := izdavackiKrug(t)
	if sazetak, err := pripremi(k, p); err == nil || err.Error() != "arhiva je prekratka za provjeru" || sazetak != "" {
		t.Errorf("provjera unatrag: %v, poruka %q", err, sazetak)
	}

	p = &poslovi.Posao{}
	lazniModel(t, p, nil)
	bezModela, _ := okolinaKruga(t, true) // prazna baza prognoza; objava ruši test
	if sazetak, err := pripremi(bezModela, p); err == nil ||
		!strings.HasPrefix(err.Error(), "model je spremljen, ali izdavanje nije uspjelo: ") || sazetak != "" {
		t.Errorf("izdavanje: %v, poruka %q", err, sazetak)
	}

	p = &poslovi.Posao{}
	lazniModel(t, p, nil)
	zabraniZapisIzdanja(t, k.prognoze)
	if sazetak, err := pripremi(k, p); err == nil ||
		!strings.HasPrefix(err.Error(), "model je spremljen, ali zapis prognoze nije uspio: ") || sazetak != "" {
		t.Errorf("zapis: %v, poruka %q", err, sazetak)
	}
	if len(*objavljeno) != 0 {
		t.Errorf("objavljeno %d izdanja, a priprema nije uspjela", len(*objavljeno))
	}
}

// Dok satni krug traje, priprema ne izdaje prognozu: novi model vrijedi od
// idućeg kruga.
func TestPripremaModelaDokKrugTraje(t *testing.T) {
	izdano, err := izdajNovimModelom(context.Background(), &poslovi.Posao{}, true, nil,
		func(context.Context, *prognoza.Ishod) { t.Error("objava dok krug traje") })
	if err != nil || izdano != "novi model vrijedi od idućeg satnog kruga, jer krug upravo traje" {
		t.Errorf("%q, %v", izdano, err)
	}
}
