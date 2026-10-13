package peers_test

import (
	"context"
	"testing"
	"time"

	"gocop/internal/ledger"
	"gocop/internal/models"
	"gocop/internal/peers"
	"gocop/internal/repository"
	"gocop/internal/sadrzaj"
)

// Odluke (13. 10. 2026.): brisanje s računala ne smije izgubiti ono što još
// nije otišlo u ured. Osobe su izmišljene: Ivo Ivić piše na laptopu, Pero
// Perić je list predao u uredu, oba u BP 16.

var vodocuvarPero = "01900000-0000-7000-8000-0000000017b3"

// listSFotografijomZa upisuje na čvoru list vodočuvara s jednom
// fotografijom i vraća fotografiju
func listSFotografijomZa(t *testing.T, ctx context.Context, n *node, sp *sadrzaj.Spremiste, vodocuvar, ime, prilog string) []byte {
	t.Helper()
	repository.SetSpremiste(sp)
	foto := []byte("\xff\xd8 obilazak BP 16, " + ime)
	l := &models.VodocuvarskiList{UserID: vodocuvar, Ime: ime, Sektor: "B", AreaID: 16, Datum: danObilaska,
		Prilike: "oblačno", Prilozi: []models.PrilogLista{{ID: prilog, Naziv: "fotografija 1",
			Vrsta: "image/jpeg", Bajtova: len(foto), Sadrzaj: sadrzaj.Otisak(foto)}}}
	repo := repository.NewVodocuvarRepository(n.db, n.rec)
	if err := repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePrilog(ctx, l.ID, prilog, foto); err != nil {
		t.Fatal(err)
	}
	return foto
}

// verzijaAutora broji verzije jednog autora u kanalu
func verzijaAutora(t *testing.T, n *node, kanal, autor string) int {
	t.Helper()
	var broj int
	if err := n.db.QueryRow(`SELECT count(*) FROM record_versions WHERE channel = ? AND node_id = ?`, kanal, autor).Scan(&broj); err != nil {
		t.Fatal(err)
	}
	return broj
}

// Laptop prati vodočuvarski dnevnik BP 16 i od ureda dobije Perin list s
// fotografijom. Bez veze Ivo na laptopu napiše svoj list s fotografijom, pa
// laptop makne praćenje i obriše BP 16 za 2026. Perin list i fotografija
// odlaze, Ivin list, njegove verzije i fotografija ostaju, a potvrda je
// prije brisanja rekla točno to. Razmjena nakon brisanja Ivin list i
// fotografiju odnese u ured, Perin list ne vrati na laptop i ne obriše ga
// u uredu. Kad laptop BP 16 opet prati, Perin list se vrati.
func TestBrisanjeKanalaNeGubiNastaloNaLaptopu(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ured := startNodeBezRegistra(t, ctx, "cop-osijek")
	laptop := startNodeBezRegistra(t, ctx, "laptop-baranja")
	founder(t, ctx, ured, "Hrvatske vode")
	pair(t, ctx, ured, laptop)
	prijasnje := repository.Spremiste()
	t.Cleanup(func() { repository.SetSpremiste(prijasnje) })
	spU, spL := otvoriSpremiste(t), otvoriSpremiste(t)
	ured.svc.SetSpremiste(spU)
	laptop.svc.SetSpremiste(spL)
	ured.svc.SetWantsAll(true)
	kanal := ledger.ChannelFor(ledger.ChannelVodocuvar, 16, 2026)

	peroFoto := listSFotografijomZa(t, ctx, ured, spU, vodocuvarPero, "Pero Perić", "foto-pero")
	bp16 := peers.Subscription{Kind: ledger.ChannelVodocuvar, AreaID: 16, Razina: peers.RazinaSve}
	pravilo, err := laptop.svc.AddSubscription(ctx, bp16)
	if err != nil {
		t.Fatal(err)
	}
	repository.SetSpremiste(spL)
	razmijeni(t, ctx, laptop, ured)
	if !imaListVodocuvara(t, ctx, laptop, vodocuvarPero) || fotografijaNaCvoru(t, ctx, laptop, spL, "foto-pero") != string(peroFoto) {
		t.Fatal("laptop nije dobio Perin list s fotografijom")
	}

	ivoFoto := listSFotografijomZa(t, ctx, laptop, spL, vodocuvarBP16, "Ivo Ivić", "foto-ivo")
	if err := laptop.svc.RemoveSubscription(ctx, pravilo.ID); err != nil {
		t.Fatal(err)
	}
	prije, err := laptop.rec.Frontier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	vlastitih, uredovih := verzijaAutora(t, laptop, kanal, laptop.id), verzijaAutora(t, laptop, kanal, ured.id)
	pregled, err := laptop.svc.PregledBrisanja(ctx, kanal)
	want := peers.Brisanje{Kanal: kanal, Zapisa: 1, Dokumenata: 1, Bajtova: int64(len(peroFoto)), Verzija: uredovih,
		OstajeZapisa: 1, OstajeDokumenata: 1, OstajeVerzija: vlastitih}
	if err != nil || pregled != want || vlastitih == 0 || uredovih == 0 {
		t.Fatalf("pregled %+v, očekivano %+v, %v", pregled, want, err)
	}
	obrisano, err := laptop.svc.PurgeChannel(ctx, kanal)
	if err != nil || obrisano != pregled {
		t.Fatalf("obrisano %+v, pregled %+v, %v", obrisano, pregled, err)
	}

	if !imaListVodocuvara(t, ctx, laptop, vodocuvarBP16) || fotografijaNaCvoru(t, ctx, laptop, spL, "foto-ivo") != string(ivoFoto) {
		t.Fatal("brisanje je odnijelo Ivin list ili fotografiju, koji još nisu u uredu")
	}
	if imaListVodocuvara(t, ctx, laptop, vodocuvarPero) || spL.Ima(ctx, sadrzaj.Otisak(peroFoto)) {
		t.Fatal("Perin list ili fotografija ostali su na laptopu")
	}
	poslije, err := laptop.rec.Frontier(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if verzijaAutora(t, laptop, kanal, laptop.id) != vlastitih || poslije[ledger.FrontierKey(laptop.id, kanal)] != prije[ledger.FrontierKey(laptop.id, kanal)] {
		t.Errorf("vlastite verzije kanala: %d prije, %d poslije; granica %v → %v", vlastitih, verzijaAutora(t, laptop, kanal, laptop.id), prije, poslije)
	}
	if _, ima := poslije[ledger.FrontierKey(ured.id, kanal)]; ima {
		t.Errorf("verzije ureda u kanalu ostale su na laptopu: %v", poslije)
	}

	repository.SetSpremiste(spU)
	razmijeni(t, ctx, laptop, ured)
	if !imaListVodocuvara(t, ctx, ured, vodocuvarBP16) || fotografijaNaCvoru(t, ctx, ured, spU, "foto-ivo") != string(ivoFoto) {
		t.Fatal("Ivin list ili fotografija nisu stigli u ured")
	}
	if !imaListVodocuvara(t, ctx, ured, vodocuvarPero) {
		t.Fatal("brisanje na laptopu obrisalo je Perin list u uredu")
	}
	if imaListVodocuvara(t, ctx, laptop, vodocuvarPero) || verzijaAutora(t, laptop, kanal, ured.id) != 0 {
		t.Fatal("razmjena je vratila obrisano na laptop koji ga ne prati")
	}
	if primljeno, poslano, err := laptop.svc.SyncWith(ctx, ured.id); err != nil || primljeno != 0 || poslano != 0 {
		t.Errorf("ponovljena razmjena: primljeno %d, poslano %d, %v", primljeno, poslano, err)
	}

	if _, err := laptop.svc.AddSubscription(ctx, bp16); err != nil {
		t.Fatal(err)
	}
	repository.SetSpremiste(spL)
	razmijeni(t, ctx, laptop, ured)
	if !imaListVodocuvara(t, ctx, laptop, vodocuvarPero) || fotografijaNaCvoru(t, ctx, laptop, spL, "foto-pero") != string(peroFoto) {
		t.Error("Perin list s fotografijom nije se vratio kad ga laptop opet prati")
	}
}
