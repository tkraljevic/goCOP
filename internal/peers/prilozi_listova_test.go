package peers_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// Fotografije uz dnevne listove vodočuvara putuju razmjenom kao i drugi
// sadržaj: lista prati njegov kanal, a fotografiju razina pretplate na taj
// kanal. Osobe su izmišljene: vodočuvari Ivo Ivić (BP 16) i Pero Perić
// (BP 17).

var (
	vodocuvarBP16 = "01900000-0000-7000-8000-0000000017b1"
	vodocuvarBP17 = "01900000-0000-7000-8000-0000000017b2"
	danObilaska   = time.Date(2026, time.October, 14, 0, 0, 0, 0, models.Zagreb)
)

// cvoroviSFotografijama diže laptop (A, prati sve) i ured (B), upari ih i
// svakom da njegovo spremište sadržaja. Spremište primjene je jedno za
// program, pa ga test postavlja prema čvoru koji upravo radi.
func cvoroviSFotografijama(t *testing.T, ctx context.Context) (a, b *node, spA, spB *sadrzaj.Spremiste) {
	t.Helper()
	a = startNodeBezRegistra(t, ctx, "laptop-baranja")
	b = startNodeBezRegistra(t, ctx, "cop-osijek")
	founder(t, ctx, a, "Hrvatske vode")
	pair(t, ctx, a, b)
	prijasnje := repository.Spremiste()
	t.Cleanup(func() { repository.SetSpremiste(prijasnje) })
	spA, spB = otvoriSpremiste(t), otvoriSpremiste(t)
	a.svc.SetSpremiste(spA)
	b.svc.SetSpremiste(spB)
	a.svc.SetWantsAll(true)
	return a, b, spA, spB
}

func otvoriSpremiste(t *testing.T) *sadrzaj.Spremiste {
	t.Helper()
	sp, err := sadrzaj.Otvori(filepath.Join(t.TempDir(), "sadrzaj.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sp.Zatvori() })
	return sp
}

// listSFotografijom upisuje na laptopu list vodočuvara s jednom
// fotografijom, kako to radi uvoz obilazaka, i vraća oznaku priloga i
// fotografiju
func listSFotografijom(t *testing.T, ctx context.Context, a *node, spA *sadrzaj.Spremiste, vodocuvar, ime string, area int) (string, []byte) {
	t.Helper()
	repository.SetSpremiste(spA)
	foto := []byte(fmt.Sprintf("\xff\xd8 obilazak BP %d, %s", area, ime))
	prilog := fmt.Sprintf("foto-bp%d", area)
	l := &models.VodocuvarskiList{UserID: vodocuvar, Ime: ime, Sektor: "B", AreaID: area, Datum: danObilaska,
		Rekonstrukcija: true, Prilike: "sunčano", Prilozi: []models.PrilogLista{{ID: prilog, Naziv: "fotografija 1",
			Vrsta: "image/jpeg", Bajtova: len(foto), Sadrzaj: sadrzaj.Otisak(foto)}}}
	repo := repository.NewVodocuvarRepository(a.db, a.rec)
	if err := repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePrilog(ctx, l.ID, prilog, foto); err != nil {
		t.Fatal(err)
	}
	return prilog, foto
}

// fotografijaNaCvoru čita fotografiju priloga na uredu; prazno kad je nema
func fotografijaNaCvoru(t *testing.T, ctx context.Context, b *node, spB *sadrzaj.Spremiste, prilog string) string {
	t.Helper()
	repository.SetSpremiste(spB)
	foto, err := repository.NewVodocuvarRepository(b.db, b.rec).Prilog(ctx, prilog)
	if err != nil {
		t.Fatal(err)
	}
	return string(foto)
}

// imaListVodocuvara javlja ima li ured list vodočuvara za dan obilaska
func imaListVodocuvara(t *testing.T, ctx context.Context, b *node, vodocuvar string) bool {
	t.Helper()
	l, err := repository.NewVodocuvarRepository(b.db, b.rec).ZaDan(ctx, vodocuvar, danObilaska)
	if err != nil {
		t.Fatal(err)
	}
	return l != nil
}

func razmijeni(t *testing.T, ctx context.Context, b, a *node) {
	t.Helper()
	if _, _, err := b.svc.SyncWith(ctx, a.id); err != nil {
		t.Fatalf("razmjena: %v", err)
	}
}

// List s fotografijom napisan na laptopu stiže u ured koji prati sve, s
// fotografijom, u istoj razmjeni.
func TestFotografijaListaStizeNaDrugiCvor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, b, spA, spB := cvoroviSFotografijama(t, ctx)
	prilog, foto := listSFotografijom(t, ctx, a, spA, vodocuvarBP16, "Ivo Ivić", 16)
	b.svc.SetWantsAll(true)

	repository.SetSpremiste(spB)
	razmijeni(t, ctx, b, a)
	if !imaListVodocuvara(t, ctx, b, vodocuvarBP16) {
		t.Fatal("ured nije primio list")
	}
	if got := fotografijaNaCvoru(t, ctx, b, spB, prilog); got != string(foto) {
		t.Fatalf("ured nema fotografiju lista: %q", got)
	}
	if _, _, kanal, _ := spB.CitajSKanalom(ctx, sadrzaj.Otisak(foto)); kanal != "vodocuvarski/16/2026" {
		t.Errorf("primljena fotografija u kanalu %q", kanal)
	}
}

// Ured je list primio prije popravka, bez veze i želje za fotografijom.
// Nakon koraka pri pokretanju fotografiju traži i dobije je sljedećom
// razmjenom.
func TestFotografijaListaPrimljenogPrijePopravka(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, b, spA, spB := cvoroviSFotografijama(t, ctx)
	prilog, foto := listSFotografijom(t, ctx, a, spA, vodocuvarBP16, "Ivo Ivić", 16)
	b.svc.SetWantsAll(true)

	repository.SetSpremiste(nil) // primjena bez spremišta ne bilježi fotografije, kao prije popravka
	razmijeni(t, ctx, b, a)
	if !imaListVodocuvara(t, ctx, b, vodocuvarBP16) || fotografijaNaCvoru(t, ctx, b, spB, prilog) != "" {
		t.Fatal("ured mora imati list bez fotografije")
	}
	razmijeni(t, ctx, b, a)
	if fotografijaNaCvoru(t, ctx, b, spB, prilog) != "" {
		t.Fatal("fotografija stigla bez želje")
	}

	p, err := repository.PopraviPrilogeListova(ctx, b.db)
	if err != nil || p != (repository.PopravakPriloga{Veza: 1, Zelja: 1}) {
		t.Fatalf("korak pri pokretanju: %+v %v", p, err)
	}
	razmijeni(t, ctx, b, a)
	if got := fotografijaNaCvoru(t, ctx, b, spB, prilog); got != string(foto) {
		t.Fatalf("ured nema fotografiju nakon popravka: %q", got)
	}
}

// Ured prati vodočuvarski dnevnik BP 16 samo kao kazalo: list dobije, a
// fotografiju ne traži. Kad pretplatu digne na sve, fotografija stigne
// sljedećom razmjenom.
func TestKazaloNeTraziFotografijuLista(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, b, spA, spB := cvoroviSFotografijama(t, ctx)
	prilog, foto := listSFotografijom(t, ctx, a, spA, vodocuvarBP16, "Ivo Ivić", 16)
	pravilo, err := b.svc.AddSubscription(ctx, peers.Subscription{Kind: "vodocuvarski", AreaID: 16, Razina: peers.RazinaKazalo})
	if err != nil {
		t.Fatal(err)
	}

	repository.SetSpremiste(spB)
	razmijeni(t, ctx, b, a)
	if !imaListVodocuvara(t, ctx, b, vodocuvarBP16) {
		t.Fatal("ured nije primio list")
	}
	if fotografijaNaCvoru(t, ctx, b, spB, prilog) != "" {
		t.Fatal("ured je dobio fotografiju iako prati samo kazalo")
	}
	if z, _ := spB.Zeljeni(ctx, 10); len(z) != 1 || z[0].Otisak != sadrzaj.Otisak(foto) || z[0].Trazeno != nil {
		t.Fatalf("fotografija mora biti na popisu, ali netražena: %+v", z)
	}

	if err := b.svc.RemoveSubscription(ctx, pravilo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.svc.AddSubscription(ctx, peers.Subscription{Kind: "vodocuvarski", AreaID: 16, Razina: peers.RazinaSve}); err != nil {
		t.Fatal(err)
	}
	razmijeni(t, ctx, b, a)
	if got := fotografijaNaCvoru(t, ctx, b, spB, prilog); got != string(foto) {
		t.Fatalf("fotografija nije stigla uz pretplatu na sve: %q", got)
	}
}

// Ured prati samo vodočuvarski dnevnik BP 17: dobije list i fotografiju
// Pere, a list Ive iz BP 16 i njegovu fotografiju ne traži.
func TestDrugoPodrucjeNeTraziFotografijuLista(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, b, spA, spB := cvoroviSFotografijama(t, ctx)
	prilog16, foto16 := listSFotografijom(t, ctx, a, spA, vodocuvarBP16, "Ivo Ivić", 16)
	prilog17, foto17 := listSFotografijom(t, ctx, a, spA, vodocuvarBP17, "Pero Perić", 17)
	if _, err := b.svc.AddSubscription(ctx, peers.Subscription{Kind: "vodocuvarski", AreaID: 17}); err != nil {
		t.Fatal(err)
	}

	repository.SetSpremiste(spB)
	razmijeni(t, ctx, b, a)
	if !imaListVodocuvara(t, ctx, b, vodocuvarBP17) || fotografijaNaCvoru(t, ctx, b, spB, prilog17) != string(foto17) {
		t.Fatal("ured nema list ili fotografiju iz BP 17")
	}
	if imaListVodocuvara(t, ctx, b, vodocuvarBP16) || fotografijaNaCvoru(t, ctx, b, spB, prilog16) != "" || spB.Ima(ctx, sadrzaj.Otisak(foto16)) {
		t.Fatal("ured je dobio list ili fotografiju iz BP 16, koji ne prati")
	}
	if z, _ := spB.Zeljeni(ctx, 10); len(z) != 0 {
		t.Errorf("na popisu za dohvat: %+v", z)
	}
}
